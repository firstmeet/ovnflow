package ovsdbjson

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
)

// Endpoint is an OVSDB connection target parsed from the common OVSDB address
// syntax, such as tcp:127.0.0.1:6641 or unix:/var/run/openvswitch/db.sock.
type Endpoint struct {
	Network string
	Address string
}

// ParseEndpoint converts an OVSDB-style endpoint into a net.Dial network and
// address. The integration harness supports tcp and unix endpoints. Windows WSL
// tests should use tcp endpoints.
func ParseEndpoint(raw string) (Endpoint, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Endpoint{}, errors.New("endpoint is empty")
	}

	switch {
	case strings.HasPrefix(raw, "tcp:"):
		address := strings.TrimPrefix(raw, "tcp:")
		if address == "" {
			return Endpoint{}, fmt.Errorf("tcp endpoint %q is missing host:port", raw)
		}
		if _, _, err := net.SplitHostPort(address); err != nil {
			return Endpoint{}, fmt.Errorf("tcp endpoint %q must be host:port: %w", raw, err)
		}
		return Endpoint{Network: "tcp", Address: address}, nil
	case strings.HasPrefix(raw, "unix:"):
		address := strings.TrimPrefix(raw, "unix:")
		if address == "" {
			return Endpoint{}, fmt.Errorf("unix endpoint %q is missing a socket path", raw)
		}
		return Endpoint{Network: "unix", Address: address}, nil
	case strings.HasPrefix(raw, "ssl:"):
		return Endpoint{}, fmt.Errorf("ssl endpoint %q is not supported by the integration harness yet", raw)
	default:
		return Endpoint{}, fmt.Errorf("unsupported OVSDB endpoint %q; use tcp:host:port", raw)
	}
}

// Client is a tiny JSON-RPC client for integration tests. It intentionally
// implements only the OVSDB calls needed by the test harness.
type Client struct {
	conn net.Conn
	enc  *json.Encoder
	dec  *json.Decoder

	mu     sync.Mutex
	nextID int64
}

// Dial opens an OVSDB connection.
func Dial(ctx context.Context, rawEndpoint string) (*Client, error) {
	endpoint, err := ParseEndpoint(rawEndpoint)
	if err != nil {
		return nil, err
	}

	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, endpoint.Network, endpoint.Address)
	if err != nil {
		return nil, err
	}

	return &Client{
		conn:   conn,
		enc:    json.NewEncoder(conn),
		dec:    json.NewDecoder(conn),
		nextID: 1,
	}, nil
}

// Close closes the underlying connection.
func (c *Client) Close() error {
	return c.conn.Close()
}

// Call sends one JSON-RPC request and decodes the matching response.
func (c *Client) Call(ctx context.Context, method string, params any, result any) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return err
	}

	id := c.nextID
	c.nextID++

	if deadline, ok := ctx.Deadline(); ok {
		if err := c.conn.SetDeadline(deadline); err != nil {
			return err
		}
		defer c.conn.SetDeadline(time.Time{})
	}

	req := rpcRequest{
		Method: method,
		Params: params,
		ID:     id,
	}
	if err := c.enc.Encode(req); err != nil {
		return err
	}

	for {
		var resp rpcResponse
		if err := c.dec.Decode(&resp); err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			return err
		}
		if resp.ID == nil || *resp.ID != id {
			continue
		}
		if !isJSONNull(resp.Error) {
			return fmt.Errorf("ovsdb rpc %s failed: %s", method, string(resp.Error))
		}
		if result == nil {
			return nil
		}
		if isJSONNull(resp.Result) {
			return nil
		}
		if err := json.Unmarshal(resp.Result, result); err != nil {
			return fmt.Errorf("decode ovsdb rpc %s result: %w", method, err)
		}
		return nil
	}
}

// ListDatabases calls the OVSDB list_dbs method.
func (c *Client) ListDatabases(ctx context.Context) ([]string, error) {
	var result []string
	if err := c.Call(ctx, "list_dbs", []any{}, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// Transact executes raw OVSDB transaction operations against database.
func (c *Client) Transact(ctx context.Context, database string, ops ...map[string]any) ([]OperationResult, error) {
	params := make([]any, 0, len(ops)+1)
	params = append(params, database)
	for _, op := range ops {
		params = append(params, op)
	}

	var result []OperationResult
	if err := c.Call(ctx, "transact", params, &result); err != nil {
		return nil, err
	}
	for i, opResult := range result {
		if opResult.Error != "" {
			return result, OperationError{
				Index:   i,
				Reason:  opResult.Error,
				Details: opResult.Details,
			}
		}
	}
	return result, nil
}

// Monitor starts an OVSDB monitor and returns the initial table updates.
func (c *Client) Monitor(ctx context.Context, database, monitorID string, requests map[string]any) (TableUpdates, error) {
	var result TableUpdates
	if err := c.Call(ctx, "monitor", []any{database, monitorID, requests}, &result); err != nil {
		return nil, err
	}
	return result, nil
}

type rpcRequest struct {
	Method string `json:"method"`
	Params any    `json:"params"`
	ID     int64  `json:"id"`
}

type rpcResponse struct {
	Result json.RawMessage `json:"result"`
	Error  json.RawMessage `json:"error"`
	ID     *int64          `json:"id"`
}

// OperationResult is the common JSON shape returned by OVSDB transact
// operations.
type OperationResult struct {
	Count   int                          `json:"count,omitempty"`
	UUID    []string                     `json:"uuid,omitempty"`
	Rows    []map[string]json.RawMessage `json:"rows,omitempty"`
	Error   string                       `json:"error,omitempty"`
	Details string                       `json:"details,omitempty"`
}

// OperationError reports the first failing operation in a transaction result.
type OperationError struct {
	Index   int
	Reason  string
	Details string
}

func (e OperationError) Error() string {
	if e.Details == "" {
		return fmt.Sprintf("ovsdb operation %d failed: %s", e.Index, e.Reason)
	}
	return fmt.Sprintf("ovsdb operation %d failed: %s: %s", e.Index, e.Reason, e.Details)
}

// TableUpdates is the result shape returned by OVSDB monitor.
type TableUpdates map[string]map[string]RowUpdate

// RowUpdate contains old and new row values for a monitored row.
type RowUpdate struct {
	Old map[string]json.RawMessage `json:"old,omitempty"`
	New map[string]json.RawMessage `json:"new,omitempty"`
}

func isJSONNull(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return true
	}
	return string(raw) == "null"
}

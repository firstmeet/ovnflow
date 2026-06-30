package ovnflow

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	ovsclient "github.com/ovn-kubernetes/libovsdb/client"
)

const defaultReconnectBackoff = 50 * time.Millisecond

// Config configures the three OVSDB connections used by ovnflow.
type Config struct {
	OVSAddr   string
	OVNNBAddr string
	OVNSBAddr string
	OpenFlow  OpenFlowConfig
	SDWAN     SDWANBackend
}

// ConfigFromEnv loads the SDK configuration from the same environment
// variables used by the Windows + WSL integration tests.
func ConfigFromEnv() Config {
	cfg := LoadIntegrationConfigFromEnv()
	return Config{
		OVSAddr:   cfg.OVSAddr,
		OVNNBAddr: cfg.OVNNBAddr,
		OVNSBAddr: cfg.OVNSBAddr,
		OpenFlow:  OpenFlowConfig{Endpoint: cfg.OpenFlowAddr},
	}
}

// Client is the public SDK entrypoint.
type Client struct {
	nb  *dbClient
	sb  *dbClient
	ovs *dbClient
	of  OpenFlowConfig

	sdwanMu sync.Mutex
	sdwan   SDWANBackend

	closeOnce sync.Once
}

type dbClient struct {
	mu           sync.RWMutex
	reconnectMu  sync.Mutex
	database     string
	address      string
	raw          ovsclient.Client
	executor     executor
	schema       *SchemaRegistry
	reconnect    func(context.Context) error
	retryBackoff time.Duration

	watchesMu sync.Mutex
	watches   *watchManager
}

// Connect creates and connects all configured OVN/OVS clients.
func Connect(ctx context.Context, cfg Config) (*Client, error) {
	nb, err := connectDB(ctx, dbOVNNorthbound, cfg.OVNNBAddr)
	if err != nil {
		return nil, err
	}
	sb, err := connectDB(ctx, dbOVNSouthbound, cfg.OVNSBAddr)
	if err != nil {
		nb.close()
		return nil, err
	}
	ovs, err := connectDB(ctx, dbOpenVSwitch, cfg.OVSAddr)
	if err != nil {
		nb.close()
		sb.close()
		return nil, err
	}
	return &Client{nb: nb, sb: sb, ovs: ovs, of: cfg.OpenFlow, sdwan: cfg.SDWAN}, nil
}

func connectDB(ctx context.Context, database, address string) (*dbClient, error) {
	address = strings.TrimSpace(address)
	if address == "" {
		return nil, wrap(ErrorValidation, database, "", "connect", "", "endpoint is required", nil)
	}

	raw, err := newOVSDBRawClient(database, address)
	if err != nil {
		return nil, err
	}
	if err := raw.Connect(ctx); err != nil {
		return nil, classifyContext(err, database, "", "connect", "")
	}
	if err := validateDatabaseSchema(raw.Schema(), requiredSchema(database)); err != nil {
		raw.Close()
		return nil, wrap(ErrorInvalidSchema, database, "", "schema", "", "", err)
	}
	d := &dbClient{database: database, address: address, raw: raw, executor: raw, schema: newSchemaRegistry(database, raw.Schema()), retryBackoff: defaultReconnectBackoff}
	d.reconnect = d.defaultReconnect
	d.watches = newWatchManager(d)
	return d, nil
}

func newOVSDBRawClient(database, address string) (ovsclient.Client, error) {
	opts, err := ovsdbEndpointOptions(address)
	if err != nil {
		return nil, wrap(ErrorValidation, database, "", "connect", "", "invalid endpoint list", err)
	}

	switch database {
	case dbOVNNorthbound:
		dbModel, modelErr := nbDBModel()
		if modelErr != nil {
			return nil, wrap(ErrorInvalidSchema, database, "", "model", "", "", modelErr)
		}
		return ovsclient.NewOVSDBClient(dbModel, opts...)
	case dbOVNSouthbound:
		dbModel, modelErr := sbDBModel()
		if modelErr != nil {
			return nil, wrap(ErrorInvalidSchema, database, "", "model", "", "", modelErr)
		}
		return ovsclient.NewOVSDBClient(dbModel, opts...)
	case dbOpenVSwitch:
		dbModel, modelErr := ovsDBModel()
		if modelErr != nil {
			return nil, wrap(ErrorInvalidSchema, database, "", "model", "", "", modelErr)
		}
		return ovsclient.NewOVSDBClient(dbModel, opts...)
	default:
		return nil, wrap(ErrorValidation, database, "", "connect", "", "unsupported database", nil)
	}
}

func (d *dbClient) defaultReconnect(ctx context.Context) error {
	raw, err := newOVSDBRawClient(d.database, d.address)
	if err != nil {
		return err
	}
	if err := raw.Connect(ctx); err != nil {
		raw.Close()
		return classifyContext(err, d.database, "", "connect", "")
	}
	if err := validateDatabaseSchema(raw.Schema(), requiredSchema(d.database)); err != nil {
		raw.Close()
		return wrap(ErrorInvalidSchema, d.database, "", "schema", "", "", err)
	}
	old := d.swapRaw(raw)
	if old != nil {
		old.Close()
	}
	return nil
}

func (d *dbClient) currentExecutor() executor {
	if d == nil {
		return nil
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	if d.executor != nil {
		return d.executor
	}
	return d.raw
}

func (d *dbClient) swapRaw(raw ovsclient.Client) ovsclient.Client {
	if d == nil {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	old := d.raw
	d.raw = raw
	d.executor = raw
	return old
}

func (d *dbClient) rawClient() ovsclient.Client {
	if d == nil {
		return nil
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.raw
}

func (d *dbClient) reconnectAfterDisconnect(ctx context.Context, table, op, object string) error {
	if d == nil {
		return wrap(ErrorUnavailable, "", table, op, object, "database client is nil", nil)
	}
	if d.reconnect == nil {
		return wrap(ErrorUnavailable, d.database, table, op, object, "database reconnect is not configured", nil)
	}
	if d.retryBackoff > 0 {
		timer := time.NewTimer(d.retryBackoff)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return classifyContext(ctx.Err(), d.database, table, op, object)
		}
	}
	d.reconnectMu.Lock()
	defer d.reconnectMu.Unlock()
	if err := d.reconnect(ctx); err != nil {
		if KindOf(err) != "" {
			return err
		}
		return classifyTransactError(err, d.database, table, "reconnect", object)
	}
	return nil
}

func ovsdbEndpointOptions(address string) ([]ovsclient.Option, error) {
	endpoints, err := splitEndpointList(address)
	if err != nil {
		return nil, err
	}
	opts := make([]ovsclient.Option, 0, len(endpoints))
	for _, endpoint := range endpoints {
		opts = append(opts, ovsclient.WithEndpoint(endpoint))
	}
	return opts, nil
}

func splitEndpointList(address string) ([]string, error) {
	parts := strings.Split(strings.TrimSpace(address), ",")
	endpoints := make([]string, 0, len(parts))
	for _, part := range parts {
		endpoint := strings.TrimSpace(part)
		if endpoint == "" {
			return nil, errors.New("endpoint must not be empty")
		}
		endpoints = append(endpoints, endpoint)
	}
	return endpoints, nil
}

func (d *dbClient) close() {
	if d == nil {
		return
	}
	d.watchesMu.Lock()
	watches := d.watches
	d.watchesMu.Unlock()
	if watches != nil {
		watches.close()
	}
	d.mu.Lock()
	raw := d.raw
	d.raw = nil
	d.executor = nil
	d.mu.Unlock()
	if raw != nil {
		raw.Close()
	}
}

// Close closes all underlying OVSDB connections.
func (c *Client) Close() {
	if c == nil {
		return
	}
	c.closeOnce.Do(func() {
		c.nb.close()
		c.sb.close()
		c.ovs.close()
	})
}

// RawNB returns the underlying libovsdb client for OVN Northbound.
func (c *Client) RawNB() ovsclient.Client {
	return c.nb.rawClient()
}

// RawSB returns the underlying libovsdb client for OVN Southbound.
func (c *Client) RawSB() ovsclient.Client {
	return c.sb.rawClient()
}

// RawOVS returns the underlying libovsdb client for Open_vSwitch.
func (c *Client) RawOVS() ovsclient.Client {
	return c.ovs.rawClient()
}

// OVN returns OVN Northbound and Southbound APIs.
func (c *Client) OVN() OVN {
	return OVN{client: c}
}

// LocalOVS returns the local Open_vSwitch API.
func (c *Client) LocalOVS() *OVSClient {
	return &OVSClient{db: c.ovs}
}

// OpenFlow returns native OpenFlow APIs for local Open_vSwitch bridges.
func (c *Client) OpenFlow() *OpenFlowClient {
	return &OpenFlowClient{ovs: c.LocalOVS(), config: c.of, dialer: openFlowNetDialer{}}
}

// UseSDWANBackend sets the backend used by Client.SDWAN.
func (c *Client) UseSDWANBackend(backend SDWANBackend) *Client {
	if c == nil {
		return c
	}
	c.sdwanMu.Lock()
	defer c.sdwanMu.Unlock()
	c.sdwan = backend
	return c
}

// OVN groups OVN APIs.
type OVN struct {
	client *Client
}

func (o OVN) NB() *NBClient {
	return &NBClient{db: o.client.nb}
}

func (o OVN) SB() *SBClient {
	return &SBClient{db: o.client.sb}
}

package ovnflow

import (
	"context"
	"sync"

	ovsclient "github.com/ovn-kubernetes/libovsdb/client"
)

// Config configures the three OVSDB connections used by ovnflow.
type Config struct {
	OVSAddr   string
	OVNNBAddr string
	OVNSBAddr string
}

// ConfigFromEnv loads the SDK configuration from the same environment
// variables used by the Windows + WSL integration tests.
func ConfigFromEnv() Config {
	cfg := LoadIntegrationConfigFromEnv()
	return Config{
		OVSAddr:   cfg.OVSAddr,
		OVNNBAddr: cfg.OVNNBAddr,
		OVNSBAddr: cfg.OVNSBAddr,
	}
}

// Client is the public SDK entrypoint.
type Client struct {
	nb  *dbClient
	sb  *dbClient
	ovs *dbClient

	closeOnce sync.Once
}

type dbClient struct {
	database string
	address  string
	raw      ovsclient.Client
	executor executor
	schema   *SchemaRegistry

	watchesMu sync.Mutex
	watches  *watchManager
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
	return &Client{nb: nb, sb: sb, ovs: ovs}, nil
}

func connectDB(ctx context.Context, database, address string) (*dbClient, error) {
	if address == "" {
		return nil, wrap(ErrorValidation, database, "", "connect", "", "endpoint is required", nil)
	}

	var (
		raw ovsclient.Client
		err error
	)
	switch database {
	case dbOVNNorthbound:
		dbModel, modelErr := nbDBModel()
		if modelErr != nil {
			return nil, wrap(ErrorInvalidSchema, database, "", "model", "", "", modelErr)
		}
		raw, err = ovsclient.NewOVSDBClient(dbModel, ovsclient.WithEndpoint(address))
	case dbOVNSouthbound:
		dbModel, modelErr := sbDBModel()
		if modelErr != nil {
			return nil, wrap(ErrorInvalidSchema, database, "", "model", "", "", modelErr)
		}
		raw, err = ovsclient.NewOVSDBClient(dbModel, ovsclient.WithEndpoint(address))
	case dbOpenVSwitch:
		dbModel, modelErr := ovsDBModel()
		if modelErr != nil {
			return nil, wrap(ErrorInvalidSchema, database, "", "model", "", "", modelErr)
		}
		raw, err = ovsclient.NewOVSDBClient(dbModel, ovsclient.WithEndpoint(address))
	}
	if err != nil {
		return nil, wrap(ErrorInvalidSchema, database, "", "client", "", "", err)
	}
	if err := raw.Connect(ctx); err != nil {
		return nil, classifyContext(err, database, "", "connect", "")
	}
	if err := validateDatabaseSchema(raw.Schema(), requiredSchema(database)); err != nil {
		raw.Close()
		return nil, wrap(ErrorInvalidSchema, database, "", "schema", "", "", err)
	}
	if _, err := raw.MonitorAll(ctx); err != nil {
		raw.Close()
		return nil, classifyContext(err, database, "", "monitor", "")
	}
	d := &dbClient{database: database, address: address, raw: raw, executor: raw, schema: newSchemaRegistry(database, raw.Schema())}
	d.watches = newWatchManager(d)
	return d, nil
}

func (d *dbClient) close() {
	if d != nil && d.raw != nil {
		d.raw.Close()
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
	return c.nb.raw
}

// RawSB returns the underlying libovsdb client for OVN Southbound.
func (c *Client) RawSB() ovsclient.Client {
	return c.sb.raw
}

// RawOVS returns the underlying libovsdb client for Open_vSwitch.
func (c *Client) RawOVS() ovsclient.Client {
	return c.ovs.raw
}

// OVN returns OVN Northbound and Southbound APIs.
func (c *Client) OVN() OVN {
	return OVN{client: c}
}

// LocalOVS returns the local Open_vSwitch API.
func (c *Client) LocalOVS() *OVSClient {
	return &OVSClient{db: c.ovs}
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

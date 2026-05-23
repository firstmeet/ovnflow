//go:build integration

package ovnflow

import (
	"context"
	"os"
	"testing"
	"time"

	libovsdb "github.com/ovn-kubernetes/libovsdb/ovsdb"
	"github.com/firstmeet/ovnflow/internal/ovsdbjson"
)

const (
	// EnvV1SchemaChecks enables read-only schema checks for the v1.0 NB, SB,
	// and OVS surfaces.
	EnvV1SchemaChecks = "OVNFLOW_V1_SCHEMA_CHECKS"

	// EnvV1MutationChecks enables broad v1.0 lifecycle tests that mutate
	// OVN/OVS rows beyond the always-on integration scenarios.
	EnvV1MutationChecks = "OVNFLOW_V1_MUTATION_CHECKS"

	// EnvV02SchemaChecks is kept as a compatibility alias for existing CI and
	// local scripts.
	EnvV02SchemaChecks = "OVNFLOW_V02_SCHEMA_CHECKS"

	// EnvV02MutationChecks is kept as a compatibility alias for existing CI and
	// local scripts.
	EnvV02MutationChecks = "OVNFLOW_V02_MUTATION_CHECKS"
)

type integrationSchemaCheck struct {
	table   string
	columns []string
}

var v1NorthboundSchemaPlan = []integrationSchemaCheck{
	{table: "Logical_Router", columns: []string{"name", "ports", "static_routes", "nat", "options", "external_ids"}},
	{table: "Logical_Router_Port", columns: []string{"name", "mac", "networks", "options", "external_ids"}},
	{table: "ACL", columns: []string{"priority", "direction", "match", "action", "external_ids"}},
	{table: "NAT", columns: []string{"type", "logical_ip", "external_ip", "external_ids"}},
	{table: "Load_Balancer", columns: []string{"name", "vips", "protocol", "external_ids"}},
	{table: "DHCP_Options", columns: []string{"cidr", "options", "external_ids"}},
	{table: "DNS", columns: []string{"records", "external_ids"}},
	{table: "QoS", columns: []string{"priority", "direction", "match", "action", "bandwidth", "external_ids"}},
	{table: "Meter", columns: []string{"name", "unit", "bands", "external_ids"}},
	{table: "Meter_Band", columns: []string{"action", "rate", "external_ids"}},
	{table: "Port_Group", columns: []string{"name", "ports", "acls", "external_ids"}},
	{table: "Address_Set", columns: []string{"name", "addresses", "external_ids"}},
	{table: "Gateway_Chassis", columns: []string{"name", "chassis_name", "priority", "external_ids"}},
	{table: "HA_Chassis", columns: []string{"chassis_name", "priority", "external_ids"}},
	{table: "HA_Chassis_Group", columns: []string{"name", "ha_chassis", "external_ids"}},
	{table: "BFD", columns: []string{"logical_port", "dst_ip", "status", "external_ids"}},
}

var v1SouthboundSchemaPlan = []integrationSchemaCheck{
	{table: "Chassis", columns: []string{"name", "hostname", "external_ids"}},
	{table: "Port_Binding", columns: []string{"logical_port", "chassis", "datapath", "mac", "external_ids"}},
	{table: "Datapath_Binding", columns: []string{"tunnel_key", "external_ids"}},
	{table: "Logical_Flow", columns: []string{"pipeline", "table_id", "match", "actions", "external_ids"}},
	{table: "MAC_Binding", columns: []string{"logical_port", "ip", "mac", "datapath"}},
	{table: "FDB", columns: []string{"mac", "dp_key", "port_key"}},
	{table: "Multicast_Group", columns: []string{"datapath", "tunnel_key", "ports"}},
	{table: "Service_Monitor", columns: []string{"logical_port", "ip", "protocol", "port", "status"}},
	{table: "RBAC_Role", columns: []string{"name", "permissions"}},
	{table: "RBAC_Permission", columns: []string{"table", "authorization", "insert_delete", "update"}},
	{table: "Meter", columns: []string{"name", "unit", "bands"}},
	{table: "Meter_Band", columns: []string{"action", "rate"}},
	{table: "DNS", columns: []string{"records", "datapaths", "external_ids"}},
	{table: "BFD", columns: []string{"logical_port", "dst_ip", "status", "external_ids"}},
}

var v1OpenVSwitchSchemaPlan = []integrationSchemaCheck{
	{table: "Open_vSwitch", columns: []string{"bridges", "manager_options", "ssl", "external_ids"}},
	{table: "Bridge", columns: []string{"name", "ports", "controller", "mirrors", "external_ids"}},
	{table: "Port", columns: []string{"name", "interfaces", "external_ids"}},
	{table: "Interface", columns: []string{"name", "type", "options", "external_ids"}},
	{table: "Controller", columns: []string{"target", "external_ids"}},
	{table: "Manager", columns: []string{"target", "external_ids"}},
	{table: "Mirror", columns: []string{"name", "select_src_port", "select_dst_port", "output_port"}},
	{table: "QoS", columns: []string{"type", "queues", "external_ids"}},
	{table: "Queue", columns: []string{"external_ids", "other_config"}},
	{table: "Flow_Table", columns: []string{"name", "external_ids"}},
	{table: "NetFlow", columns: []string{"targets", "engine_type", "engine_id", "active_timeout"}},
	{table: "sFlow", columns: []string{"agent", "targets", "header", "sampling", "polling"}},
	{table: "IPFIX", columns: []string{"targets", "sampling", "external_ids"}},
	{table: "SSL", columns: []string{"private_key", "certificate", "ca_cert", "bootstrap_ca_cert"}},
	{table: "AutoAttach", columns: []string{"system_name", "system_description", "mappings"}},
}

func TestIntegrationV1SchemaReadiness(t *testing.T) {
	requireAnyEnvOptIn(t, "read-only v1.0 schema checks", EnvV1SchemaChecks, EnvV02SchemaChecks)

	cfg := requireIntegrationConfig(t)
	checks := []struct {
		name     string
		address  string
		database string
		required []integrationSchemaCheck
	}{
		{name: "Northbound", address: cfg.OVNNBAddr, database: nbDatabase, required: v1NorthboundSchemaPlan},
		{name: "Southbound", address: cfg.OVNSBAddr, database: sbDatabase, required: v1SouthboundSchemaPlan},
		{name: "Open_vSwitch", address: cfg.OVSAddr, database: ovsDatabase, required: v1OpenVSwitchSchemaPlan},
	}

	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			client := dialOVSDBOrSkip(t, check.address)
			t.Cleanup(func() {
				_ = client.Close()
			})
			schema := getIntegrationSchema(t, client, check.database)
			assertSchemaReadiness(t, schema, check.required)
		})
	}
}

func TestIntegrationV1MutationScenariosAreEnvGated(t *testing.T) {
	requireAnyEnvOptIn(t, "v1.0 mutation acceptance", EnvV1MutationChecks, EnvV02MutationChecks)

	cfg := requireIntegrationConfig(t)
	sdk := connectSDKOrSkip(t, cfg)
	t.Cleanup(sdk.Close)

	rawNB := dialOVSDBOrSkip(t, cfg.OVNNBAddr)
	t.Cleanup(func() { _ = rawNB.Close() })
	rawOVS := dialOVSDBOrSkip(t, cfg.OVSAddr)
	t.Cleanup(func() { _ = rawOVS.Close() })

	suffix := uniqueSuffix()
	ctx := testContext(t)

	t.Run("northbound L3 policy service lifecycle", func(t *testing.T) {
		prefix := cfg.ResourcePrefix + "v1-" + suffix + "-"
		lrName := prefix + "lr"
		lrpName := prefix + "lrp"
		aclMatch := "outport == \"" + prefix + "vm\""
		natLogicalIP := "10.210.0.0/24"
		lbName := prefix + "lb"
		dhcpCIDR := "10.210.0.0/24"
		dnsName := prefix + "dns"
		qosMatch := "ip4.src == 10.210.0.10"
		addressSetName := prefix + "as"

		cleanupV1Northbound(ctx, t, sdk, rawNB, lrName, lrpName, aclMatch, natLogicalIP, lbName, dhcpCIDR, dnsName, qosMatch, addressSetName)
		t.Cleanup(func() {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			cleanupV1Northbound(cleanupCtx, t, sdk, rawNB, lrName, lrpName, aclMatch, natLogicalIP, lbName, dhcpCIDR, dnsName, qosMatch, addressSetName)
		})

		must(t, sdk.OVN().NB().LogicalRouter(lrName).Ensure().WithExternalID(testMarkerKey, testMarkerValue).Execute(ctx), "ensure logical router")
		must(t, sdk.OVN().NB().LogicalRouter(lrName).Ensure().WithExternalID(testMarkerKey, testMarkerValue).Execute(ctx), "repeat ensure logical router")
		must(t, sdk.OVN().NB().LogicalRouterPort(lrpName).Ensure().WithMAC("00:00:5e:00:53:01").WithNetwork("10.210.0.1/24").WithExternalID(testMarkerKey, testMarkerValue).Execute(ctx), "ensure router port")
		must(t, sdk.OVN().NB().ACLByMatch("to-lport", 1001, aclMatch).Ensure().WithAction("allow").WithExternalID(testMarkerKey, testMarkerValue).Execute(ctx), "ensure acl")
		must(t, sdk.OVN().NB().NATByLogicalIP("snat", natLogicalIP).Ensure().WithExternalIP("192.0.2.210").WithExternalID(testMarkerKey, testMarkerValue).Execute(ctx), "ensure nat")
		must(t, sdk.OVN().NB().LoadBalancer(lbName).Ensure().WithVIP("192.0.2.211:80", "10.210.0.10:80").WithExternalID(testMarkerKey, testMarkerValue).Execute(ctx), "ensure load balancer")
		must(t, sdk.OVN().NB().DHCPOptions(dhcpCIDR).Ensure().WithOption("router", "10.210.0.1").WithExternalID(testMarkerKey, testMarkerValue).Execute(ctx), "ensure dhcp options")
		must(t, sdk.OVN().NB().DNS(dnsName).Ensure().WithRecord("vm.ovnflow.test", "10.210.0.10").WithExternalID(testMarkerKey, testMarkerValue).Execute(ctx), "ensure dns")
		must(t, sdk.OVN().NB().QoSByMatch("from-lport", 100, qosMatch).Ensure().WithRate(1000).WithExternalID(testMarkerKey, testMarkerValue).Execute(ctx), "ensure qos")
		must(t, sdk.OVN().NB().AddressSet(addressSetName).Ensure().WithAddress("10.210.0.10").WithExternalID(testMarkerKey, testMarkerValue).Execute(ctx), "ensure address set")

		if _, err := sdk.OVN().NB().GetLogicalRouter(ctx, lrName); err != nil {
			t.Fatalf("get logical router: %v", err)
		}
		if rows := selectRows(t, rawNB, nbDatabase, "Logical_Router", nameWhere(lrName), []string{"name"}); len(rows) != 1 {
			t.Fatalf("Logical_Router rows = %d, want 1", len(rows))
		}
	})

	t.Run("southbound typed reads and watch cancel", func(t *testing.T) {
		if _, err := sdk.OVN().SB().ListChassis(ctx); err != nil {
			t.Fatalf("list chassis: %v", err)
		}
		if _, err := sdk.OVN().SB().ListPortBindings(ctx); err != nil {
			t.Fatalf("list port bindings: %v", err)
		}
		if _, err := sdk.OVN().SB().ListLogicalFlows(ctx); err != nil {
			t.Fatalf("list logical flows: %v", err)
		}
		watchCtx, cancel := context.WithCancel(context.Background())
		events, errs := sdk.OVN().SB().WatchPortBindings(watchCtx)
		cancel()
		select {
		case <-events:
		case err := <-errs:
			if err != nil && !IsKind(err, ErrorCanceled) {
				t.Fatalf("watch error = %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("watch did not stop after cancel")
		}
	})

	t.Run("ovs extended table lifecycle", func(t *testing.T) {
		managerTarget := "ptcp:" + suffix + ":127.0.0.1"
		qosName := cfg.ResourcePrefix + "qos-" + suffix
		queueName := cfg.ResourcePrefix + "queue-" + suffix
		cleanupV1OVS(ctx, t, sdk, rawOVS, managerTarget, qosName, queueName)
		t.Cleanup(func() {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			cleanupV1OVS(cleanupCtx, t, sdk, rawOVS, managerTarget, qosName, queueName)
		})

		must(t, sdk.LocalOVS().Manager(managerTarget).Ensure().WithTarget(managerTarget).WithExternalID(testMarkerKey, testMarkerValue).Execute(ctx), "ensure manager")
		must(t, sdk.LocalOVS().QoS(qosName).Ensure().WithQoSType("linux-htb").WithExternalID(testMarkerKey, testMarkerValue).Execute(ctx), "ensure qos")
		must(t, sdk.LocalOVS().Queue(queueName).Ensure().WithQueueOtherConfig("max-rate", "1000000").WithExternalID(testMarkerKey, testMarkerValue).Execute(ctx), "ensure queue")

		if rows := selectRows(t, rawOVS, ovsDatabase, "Manager", []any{ovsdbjson.Condition("target", "==", managerTarget)}, []string{"target"}); len(rows) != 1 {
			t.Fatalf("Manager rows = %d, want 1", len(rows))
		}
	})
}

func requireEnvOptIn(t *testing.T, name, purpose string) {
	t.Helper()
	if !parseEnvBool(os.Getenv(name)) {
		t.Skipf("%s disabled; set %s=1 to run it", purpose, name)
	}
}

func requireAnyEnvOptIn(t *testing.T, purpose string, names ...string) {
	t.Helper()
	for _, name := range names {
		if parseEnvBool(os.Getenv(name)) {
			return
		}
	}
	t.Skipf("%s disabled; set one of %v to 1", purpose, names)
}

func getIntegrationSchema(t *testing.T, client *ovsdbjson.Client, database string) libovsdb.DatabaseSchema {
	t.Helper()
	var schema libovsdb.DatabaseSchema
	if err := client.Call(testContext(t), "get_schema", []any{database}, &schema); err != nil {
		t.Fatalf("get_schema %s: %v", database, err)
	}
	return schema
}

func assertSchemaReadiness(t *testing.T, schema libovsdb.DatabaseSchema, required []integrationSchemaCheck) {
	t.Helper()
	for _, required := range required {
		t.Run(required.table, func(t *testing.T) {
			table := schema.Table(required.table)
			if table == nil {
				t.Fatalf("schema %s is missing table %s", schema.Name, required.table)
			}
			for _, column := range required.columns {
				if table.Column(column) == nil {
					t.Fatalf("schema %s is missing column %s.%s", schema.Name, required.table, column)
				}
			}
		})
	}
}

func cleanupV1Northbound(ctx context.Context, t *testing.T, sdk *Client, raw *ovsdbjson.Client, lrName, lrpName, aclMatch, natLogicalIP, lbName, dhcpCIDR, dnsName, qosMatch, addressSetName string) {
	t.Helper()
	_ = sdk.OVN().NB().LogicalRouter(lrName).Delete().Execute(ctx)
	_ = sdk.OVN().NB().LogicalRouterPort(lrpName).Delete().Execute(ctx)
	_ = sdk.OVN().NB().ACLByMatch("to-lport", 1001, aclMatch).Delete().Execute(ctx)
	_ = sdk.OVN().NB().NATByLogicalIP("snat", natLogicalIP).Delete().Execute(ctx)
	_ = sdk.OVN().NB().LoadBalancer(lbName).Delete().Execute(ctx)
	_ = sdk.OVN().NB().DHCPOptions(dhcpCIDR).Delete().Execute(ctx)
	_ = sdk.OVN().NB().DNS(dnsName).Delete().Execute(ctx)
	_ = sdk.OVN().NB().QoSByMatch("from-lport", 100, qosMatch).Delete().Execute(ctx)
	_ = sdk.OVN().NB().AddressSet(addressSetName).Delete().Execute(ctx)
	_, err := raw.Transact(ctx, nbDatabase,
		map[string]any{"op": "delete", "table": "Logical_Router", "where": nameWhere(lrName)},
		map[string]any{"op": "delete", "table": "Logical_Router_Port", "where": nameWhere(lrpName)},
		map[string]any{"op": "delete", "table": "Load_Balancer", "where": nameWhere(lbName)},
		map[string]any{"op": "delete", "table": "Address_Set", "where": nameWhere(addressSetName)},
	)
	if err != nil {
		t.Logf("fallback cleanup v1.0 northbound: %v", err)
	}
}

func cleanupV1OVS(ctx context.Context, t *testing.T, sdk *Client, raw *ovsdbjson.Client, managerTarget, qosName, queueName string) {
	t.Helper()
	_ = sdk.LocalOVS().Manager(managerTarget).Delete().Execute(ctx)
	_ = sdk.LocalOVS().QoS(qosName).Delete().Execute(ctx)
	_ = sdk.LocalOVS().Queue(queueName).Delete().Execute(ctx)
	_, err := raw.Transact(ctx, ovsDatabase,
		map[string]any{"op": "delete", "table": "Manager", "where": []any{ovsdbjson.Condition("target", "==", managerTarget)}},
		map[string]any{"op": "delete", "table": "QoS", "where": []any{ovsdbjson.Condition("external_ids", "includes", ovsdbjson.Map(map[string]string{"name": qosName}))}},
		map[string]any{"op": "delete", "table": "Queue", "where": []any{ovsdbjson.Condition("external_ids", "includes", ovsdbjson.Map(map[string]string{"name": queueName}))}},
	)
	if err != nil {
		t.Logf("fallback cleanup v1.0 OVS: %v", err)
	}
}

func must(t *testing.T, err error, action string) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", action, err)
	}
}

//go:build integration

package ovnflow

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/firstmeet/ovnflow/internal/ovsdbjson"
)

const (
	ovsDatabase = "Open_vSwitch"
	nbDatabase  = "OVN_Northbound"
	sbDatabase  = "OVN_Southbound"

	testMarkerKey   = "ovnflow.integration"
	testMarkerValue = "true"
)

func TestIntegrationEndpointsExposeExpectedDatabases(t *testing.T) {
	cfg := requireIntegrationConfig(t)

	checks := []struct {
		name     string
		address  string
		database string
	}{
		{name: "Open_vSwitch", address: cfg.OVSAddr, database: ovsDatabase},
		{name: "OVN Northbound", address: cfg.OVNNBAddr, database: nbDatabase},
		{name: "OVN Southbound", address: cfg.OVNSBAddr, database: sbDatabase},
	}

	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			client := dialOVSDBOrSkip(t, check.address)
			t.Cleanup(func() {
				_ = client.Close()
			})
			requireDatabase(t, client, check.database)
		})
	}
}

func TestIntegrationNorthboundLogicalSwitchPortLifecycle(t *testing.T) {
	cfg := requireIntegrationConfig(t)
	sdk := connectSDKOrSkip(t, cfg)
	t.Cleanup(sdk.Close)

	raw := dialOVSDBOrSkip(t, cfg.OVNNBAddr)
	t.Cleanup(func() {
		_ = raw.Close()
	})
	requireDatabase(t, raw, nbDatabase)

	suffix := uniqueSuffix()
	lsName := cfg.ResourcePrefix + "ls-" + suffix
	lspName := cfg.ResourcePrefix + "lsp-" + suffix

	ctx := testContext(t)
	cleanupNorthbound(ctx, t, raw, lsName, lspName)
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cleanupNorthbound(cleanupCtx, t, raw, lsName, lspName)
	})

	err := sdk.OVN().NB().
		LogicalSwitch(lsName).
		Ensure().
		WithSubnet("192.168.1.0/24").
		WithExternalID(testMarkerKey, testMarkerValue).
		AddPort(lspName).
		WithMac("00:11:22:33:44:55").
		WithIP("192.168.1.10").
		WithExternalID(testMarkerKey, testMarkerValue).
		Execute(ctx)
	if err != nil {
		t.Fatalf("SDK create logical switch and port: %v", err)
	}
	err = sdk.OVN().NB().
		LogicalSwitch(lsName).
		Ensure().
		WithSubnet("192.168.1.0/24").
		AddPort(lspName).
		WithMac("00:11:22:33:44:55").
		WithIP("192.168.1.10").
		Execute(ctx)
	if err != nil {
		t.Fatalf("SDK repeated ensure logical switch and port: %v", err)
	}

	lsRows := selectRows(t, raw, nbDatabase, "Logical_Switch", nameWhere(lsName), []string{"name", "ports", "external_ids"})
	if len(lsRows) != 1 {
		t.Fatalf("Logical_Switch rows = %d, want 1", len(lsRows))
	}
	if rowString(t, lsRows[0], "name") != lsName {
		t.Fatalf("Logical_Switch name mismatch")
	}
	ls, err := sdk.OVN().NB().GetLogicalSwitch(ctx, lsName)
	if err != nil {
		t.Fatalf("SDK get logical switch: %v", err)
	}
	if ls.Name != lsName || len(ls.Ports) != 1 {
		t.Fatalf("SDK logical switch = %#v, want one attached port", ls)
	}

	lspRows := selectRows(t, raw, nbDatabase, "Logical_Switch_Port", nameWhere(lspName), []string{"_uuid", "name", "addresses", "external_ids"})
	if len(lspRows) != 1 {
		t.Fatalf("Logical_Switch_Port rows = %d, want 1", len(lspRows))
	}
	if !rowUUIDSetContains(t, lsRows[0], "ports", rowUUIDMust(t, lspRows[0], "_uuid")) {
		t.Fatalf("Logical_Switch ports does not reference Logical_Switch_Port")
	}

	monitorClient := dialOVSDBOrSkip(t, cfg.OVNNBAddr)
	t.Cleanup(func() {
		_ = monitorClient.Close()
	})
	updates, err := monitorClient.Monitor(testContext(t), nbDatabase, "ovnflow-it-"+suffix, map[string]any{
		"Logical_Switch": map[string]any{
			"columns": []string{"name"},
			"select": map[string]bool{
				"initial": true,
				"insert":  true,
				"delete":  true,
				"modify":  true,
			},
		},
	})
	if err != nil {
		t.Fatalf("monitor Logical_Switch: %v", err)
	}
	if !updatesContainName(updates, "Logical_Switch", lsName) {
		t.Fatalf("monitor initial update did not contain logical switch %q", lsName)
	}

	cleanupErr := sdk.OVN().NB().LogicalSwitch(lsName).Delete().AddPort(lspName).Execute(ctx)
	if cleanupErr != nil {
		t.Fatalf("SDK cleanup logical switch and port: %v", cleanupErr)
	}
	lsRows = selectRows(t, raw, nbDatabase, "Logical_Switch", nameWhere(lsName), []string{"name"})
	if len(lsRows) != 0 {
		t.Fatalf("Logical_Switch rows after cleanup = %d, want 0", len(lsRows))
	}
	lspRows = selectRows(t, raw, nbDatabase, "Logical_Switch_Port", nameWhere(lspName), []string{"name"})
	if len(lspRows) != 0 {
		t.Fatalf("Logical_Switch_Port rows after cleanup = %d, want 0", len(lspRows))
	}
}

func TestIntegrationOVSBridgePortLifecycle(t *testing.T) {
	cfg := requireIntegrationConfig(t)
	sdk := connectSDKOrSkip(t, cfg)
	t.Cleanup(sdk.Close)

	raw := dialOVSDBOrSkip(t, cfg.OVSAddr)
	t.Cleanup(func() {
		_ = raw.Close()
	})
	requireDatabase(t, raw, ovsDatabase)

	suffix := uniqueSuffix()
	bridgeName := cfg.BridgeName + "-" + suffix
	portName := cfg.ResourcePrefix + "port-" + suffix
	ifaceName := cfg.ResourcePrefix + "iface-" + suffix

	ctx := testContext(t)
	requireSafeBridgeTarget(t, raw, bridgeName)
	cleanupOVS(ctx, t, raw, bridgeName, portName, ifaceName)
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cleanupOVS(cleanupCtx, t, raw, bridgeName, portName, ifaceName)
	})

	err := sdk.LocalOVS().
		Bridge(bridgeName).
		AddPort(portName).
		WithInterfaceName(ifaceName).
		WithInterfaceType("internal").
		WithExternalID(testMarkerKey, testMarkerValue).
		Execute(ctx)
	if err != nil {
		t.Fatalf("SDK create OVS bridge/port/interface: %v", err)
	}
	err = sdk.LocalOVS().
		Bridge(bridgeName).
		AddPort(portName).
		WithInterfaceName(ifaceName).
		WithInterfaceType("internal").
		WithExternalID(testMarkerKey, testMarkerValue).
		Execute(ctx)
	if err != nil {
		t.Fatalf("SDK repeated ensure OVS bridge/port/interface: %v", err)
	}

	bridgeRows := selectRows(t, raw, ovsDatabase, "Bridge", nameWhere(bridgeName), []string{"_uuid", "name", "ports", "external_ids"})
	if len(bridgeRows) != 1 {
		t.Fatalf("Bridge rows = %d, want 1", len(bridgeRows))
	}
	if rowString(t, bridgeRows[0], "name") != bridgeName {
		t.Fatalf("Bridge name mismatch")
	}

	portRows := selectRows(t, raw, ovsDatabase, "Port", nameWhere(portName), []string{"_uuid", "name", "interfaces", "external_ids"})
	if len(portRows) != 1 {
		t.Fatalf("Port rows = %d, want 1", len(portRows))
	}
	if !rowUUIDSetContains(t, bridgeRows[0], "ports", rowUUIDMust(t, portRows[0], "_uuid")) {
		t.Fatalf("Bridge ports does not reference Port")
	}

	ifaceRows := selectRows(t, raw, ovsDatabase, "Interface", nameWhere(ifaceName), []string{"_uuid", "name", "type", "external_ids"})
	if len(ifaceRows) != 1 {
		t.Fatalf("Interface rows = %d, want 1", len(ifaceRows))
	}
	if got := rowString(t, ifaceRows[0], "type"); got != "internal" {
		t.Fatalf("Interface type = %q, want internal", got)
	}
	if !rowUUIDSetContains(t, portRows[0], "interfaces", rowUUIDMust(t, ifaceRows[0], "_uuid")) {
		t.Fatalf("Port interfaces does not reference Interface")
	}

	monitorClient := dialOVSDBOrSkip(t, cfg.OVSAddr)
	t.Cleanup(func() {
		_ = monitorClient.Close()
	})
	updates, err := monitorClient.Monitor(testContext(t), ovsDatabase, "ovnflow-it-"+suffix, map[string]any{
		"Bridge": map[string]any{
			"columns": []string{"name"},
			"select": map[string]bool{
				"initial": true,
				"insert":  true,
				"delete":  true,
				"modify":  true,
			},
		},
	})
	if err != nil {
		t.Fatalf("monitor Bridge: %v", err)
	}
	if !updatesContainName(updates, "Bridge", bridgeName) {
		t.Fatalf("monitor initial update did not contain bridge %q", bridgeName)
	}

	if err := sdk.LocalOVS().Bridge(bridgeName).Delete().Execute(ctx); err != nil {
		t.Fatalf("SDK cleanup OVS bridge: %v", err)
	}
	bridgeRows = selectRows(t, raw, ovsDatabase, "Bridge", nameWhere(bridgeName), []string{"name"})
	if len(bridgeRows) != 0 {
		t.Fatalf("Bridge rows after cleanup = %d, want 0", len(bridgeRows))
	}
	portRows = selectRows(t, raw, ovsDatabase, "Port", nameWhere(portName), []string{"name"})
	if len(portRows) != 0 {
		t.Fatalf("Port rows after bridge cleanup = %d, want 0", len(portRows))
	}
	ifaceRows = selectRows(t, raw, ovsDatabase, "Interface", nameWhere(ifaceName), []string{"name"})
	if len(ifaceRows) != 0 {
		t.Fatalf("Interface rows after bridge cleanup = %d, want 0", len(ifaceRows))
	}
}

func TestIntegrationOVSDeletePortRemovesCustomInterface(t *testing.T) {
	cfg := requireIntegrationConfig(t)
	sdk := connectSDKOrSkip(t, cfg)
	t.Cleanup(sdk.Close)

	raw := dialOVSDBOrSkip(t, cfg.OVSAddr)
	t.Cleanup(func() {
		_ = raw.Close()
	})

	suffix := uniqueSuffix()
	bridgeName := cfg.BridgeName + "-delport-" + suffix
	portName := cfg.ResourcePrefix + "port-del-" + suffix
	ifaceName := cfg.ResourcePrefix + "iface-del-" + suffix

	ctx := testContext(t)
	requireSafeBridgeTarget(t, raw, bridgeName)
	cleanupOVS(ctx, t, raw, bridgeName, portName, ifaceName)
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cleanupOVS(cleanupCtx, t, raw, bridgeName, portName, ifaceName)
	})

	if err := sdk.LocalOVS().
		Bridge(bridgeName).
		AddPort(portName).
		WithInterfaceName(ifaceName).
		WithInterfaceType("internal").
		WithExternalID(testMarkerKey, testMarkerValue).
		Execute(ctx); err != nil {
		t.Fatalf("SDK create OVS bridge/port/interface: %v", err)
	}
	if err := sdk.LocalOVS().Bridge(bridgeName).DeletePort(portName).Execute(ctx); err != nil {
		t.Fatalf("SDK delete OVS port: %v", err)
	}

	if rows := selectRows(t, raw, ovsDatabase, "Bridge", nameWhere(bridgeName), []string{"name"}); len(rows) != 1 {
		t.Fatalf("Bridge rows after delete port = %d, want 1", len(rows))
	}
	if rows := selectRows(t, raw, ovsDatabase, "Port", nameWhere(portName), []string{"name"}); len(rows) != 0 {
		t.Fatalf("Port rows after delete port = %d, want 0", len(rows))
	}
	if rows := selectRows(t, raw, ovsDatabase, "Interface", nameWhere(ifaceName), []string{"name"}); len(rows) != 0 {
		t.Fatalf("Interface rows after delete port = %d, want 0", len(rows))
	}
}

func TestIntegrationSouthboundSDKQueryAndWatch(t *testing.T) {
	cfg := requireIntegrationConfig(t)
	sdk := connectSDKOrSkip(t, cfg)
	t.Cleanup(sdk.Close)

	ctx := testContext(t)
	if _, err := sdk.OVN().SB().ListChassis(ctx); err != nil {
		t.Fatalf("SDK list SB chassis: %v", err)
	}
	if _, err := sdk.OVN().SB().ListPortBindings(ctx); err != nil {
		t.Fatalf("SDK list SB port bindings: %v", err)
	}
	if _, err := sdk.OVN().SB().ListDatapaths(ctx); err != nil {
		t.Fatalf("SDK list SB datapaths: %v", err)
	}

	watchCtx, cancel := context.WithCancel(context.Background())
	events, errs := sdk.OVN().SB().WatchPortBindings(watchCtx)
	cancel()
	select {
	case _, ok := <-events:
		if ok {
			// Initial events are allowed if the cache had rows.
		}
	case err := <-errs:
		if err != nil && !IsKind(err, ErrorCanceled) {
			t.Fatalf("watch error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("watch did not stop after context cancel")
	}
}

func requireIntegrationConfig(t *testing.T) IntegrationConfig {
	t.Helper()
	cfg := LoadIntegrationConfigFromEnv()
	if missing := cfg.MissingEndpoints(); len(missing) > 0 {
		skipOrFailIntegration(t, "integration endpoints are not configured: missing %s. Example: $env:%s=\"tcp:172.27.192.120:6640\"; $env:%s=\"tcp:172.27.192.120:6641\"; $env:%s=\"tcp:172.27.192.120:6642\"",
			strings.Join(missing, ", "), EnvOVSAddr, EnvOVNNBAddr, EnvOVNSBAddr)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("invalid integration configuration: %v", err)
	}
	return cfg
}

func dialOVSDBOrSkip(t *testing.T, address string) *ovsdbjson.Client {
	t.Helper()
	if _, err := ovsdbjson.ParseEndpoint(address); err != nil {
		t.Fatalf("invalid OVSDB endpoint %q: %v", address, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	client, err := ovsdbjson.Dial(ctx, address)
	if err != nil {
		skipOrFailIntegration(t, "OVSDB endpoint %s is not reachable: %v. %s", address, err, integrationTroubleshootingHint())
	}
	return client
}

func connectSDKOrSkip(t *testing.T, cfg IntegrationConfig) *Client {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, err := Connect(ctx, Config{
		OVSAddr:   cfg.OVSAddr,
		OVNNBAddr: cfg.OVNNBAddr,
		OVNSBAddr: cfg.OVNSBAddr,
	})
	if err != nil {
		skipOrFailIntegration(t, "SDK could not connect to WSL OVN/OVS endpoints: %v. %s", err, integrationTroubleshootingHint())
	}
	return client
}

func skipOrFailIntegration(t *testing.T, format string, args ...any) {
	t.Helper()
	if LoadIntegrationConfigFromEnv().ShouldRequireEndpoints() {
		t.Fatalf(format, args...)
	}
	t.Skipf(format, args...)
}

func integrationTroubleshootingHint() string {
	return "Check WSL with `ss -lntp | grep -E '6640|6641|6642'` and expose services with `ovs-vsctl set-manager ptcp:6640:0.0.0.0`, `ovn-nbctl set-connection ptcp:6641:0.0.0.0`, `ovn-sbctl set-connection ptcp:6642:0.0.0.0`."
}

func requireDatabase(t *testing.T, client *ovsdbjson.Client, expected string) {
	t.Helper()
	ctx := testContext(t)
	databases, err := client.ListDatabases(ctx)
	if err != nil {
		t.Fatalf("list OVSDB databases: %v", err)
	}
	for _, database := range databases {
		if database == expected {
			return
		}
	}
	t.Fatalf("endpoint databases = %v, want %s", databases, expected)
}

func selectRows(t *testing.T, client *ovsdbjson.Client, database, table string, where []any, columns []string) []map[string]json.RawMessage {
	t.Helper()
	op := map[string]any{
		"op":    "select",
		"table": table,
		"where": where,
	}
	if len(columns) > 0 {
		op["columns"] = columns
	}

	result, err := client.Transact(testContext(t), database, op)
	if err != nil {
		t.Fatalf("select %s: %v", table, err)
	}
	if len(result) != 1 {
		t.Fatalf("select %s returned %d operation results, want 1", table, len(result))
	}
	return result[0].Rows
}

func cleanupNorthbound(ctx context.Context, t *testing.T, client *ovsdbjson.Client, lsName, lspName string) {
	t.Helper()
	_, err := client.Transact(ctx, nbDatabase,
		map[string]any{
			"op":    "delete",
			"table": "Logical_Switch",
			"where": nameWhere(lsName),
		},
		map[string]any{
			"op":    "delete",
			"table": "Logical_Switch_Port",
			"where": nameWhere(lspName),
		},
	)
	if err != nil {
		t.Logf("cleanup northbound rows: %v", err)
	}
}

func requireSafeBridgeTarget(t *testing.T, client *ovsdbjson.Client, bridgeName string) {
	t.Helper()
	rows := selectRows(t, client, ovsDatabase, "Bridge", nameWhere(bridgeName), []string{"name", "external_ids"})
	if len(rows) == 0 {
		return
	}
	if len(rows) > 1 {
		t.Fatalf("Bridge %q has %d rows, want at most 1", bridgeName, len(rows))
	}

	externalIDs := rowStringMap(t, rows[0], "external_ids")
	if externalIDs[testMarkerKey] != testMarkerValue {
		t.Skipf("Bridge %q already exists and is not marked as an ovnflow integration-test bridge; set %s to a dedicated bridge name", bridgeName, EnvTestBridge)
	}
}

func cleanupOVS(ctx context.Context, t *testing.T, client *ovsdbjson.Client, bridgeName, portName, ifaceName string) {
	t.Helper()
	bridgeRows := selectRows(t, client, ovsDatabase, "Bridge", nameWhere(bridgeName), []string{"_uuid", "ports", "external_ids"})
	var portUUIDs []string
	if len(bridgeRows) > 0 {
		externalIDs := rowStringMap(t, bridgeRows[0], "external_ids")
		if externalIDs[testMarkerKey] == testMarkerValue {
			if bridgeUUID, ok := rowUUID(t, bridgeRows[0], "_uuid"); ok {
				portUUIDs = rowUUIDSetValuesFromColumn(t, bridgeRows[0], "ports")
				_, err := client.Transact(ctx, ovsDatabase,
					map[string]any{
						"op":    "mutate",
						"table": "Open_vSwitch",
						"where": []any{},
						"mutations": []any{
							[]any{"bridges", "delete", ovsdbjson.Set(ovsdbjson.UUID(bridgeUUID))},
						},
					},
					map[string]any{
						"op":    "delete",
						"table": "Bridge",
						"where": nameWhere(bridgeName),
					},
				)
				if err != nil {
					t.Logf("cleanup OVS bridge %q: %v", bridgeName, err)
				}
			}
		}
	}

	var ifaceUUIDs []string
	for _, portUUID := range portUUIDs {
		rows := selectRows(t, client, ovsDatabase, "Port", uuidWhere(portUUID), []string{"interfaces"})
		for _, row := range rows {
			ifaceUUIDs = append(ifaceUUIDs, rowUUIDSetValuesFromColumn(t, row, "interfaces")...)
		}
	}

	ops := []map[string]any{
		map[string]any{
			"op":    "delete",
			"table": "Port",
			"where": nameWhere(portName),
		},
		map[string]any{
			"op":    "delete",
			"table": "Interface",
			"where": nameWhere(ifaceName),
		},
	}
	for _, portUUID := range uniqueStrings(portUUIDs) {
		ops = append(ops, map[string]any{
			"op":    "delete",
			"table": "Port",
			"where": uuidWhere(portUUID),
		})
	}
	for _, ifaceUUID := range uniqueStrings(ifaceUUIDs) {
		ops = append(ops, map[string]any{
			"op":    "delete",
			"table": "Interface",
			"where": uuidWhere(ifaceUUID),
		})
	}
	_, err := client.Transact(ctx, ovsDatabase, ops...)
	if err != nil {
		t.Logf("cleanup OVS port/interface: %v", err)
	}
}

func nameWhere(name string) []any {
	return []any{ovsdbjson.Condition("name", "==", name)}
}

func uuidWhere(id string) []any {
	return []any{ovsdbjson.Condition("_uuid", "==", ovsdbjson.UUID(id))}
}

func rowString(t *testing.T, row map[string]json.RawMessage, column string) string {
	t.Helper()
	raw, ok := row[column]
	if !ok {
		t.Fatalf("row is missing column %q", column)
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("decode %s string: %v", column, err)
	}
	return value
}

func rowInt(t *testing.T, row map[string]json.RawMessage, column string) int {
	t.Helper()
	raw, ok := row[column]
	if !ok {
		t.Fatalf("row is missing column %q", column)
	}
	var value int
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("decode %s int: %v", column, err)
	}
	return value
}

func rowUUID(t *testing.T, row map[string]json.RawMessage, column string) (string, bool) {
	t.Helper()
	raw, ok := row[column]
	if !ok {
		return "", false
	}
	var value []string
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("decode %s uuid: %v", column, err)
	}
	if len(value) != 2 || (value[0] != "uuid" && value[0] != "named-uuid") {
		t.Fatalf("column %s is not an OVSDB uuid: %s", column, string(raw))
	}
	return value[1], true
}

func rowUUIDMust(t *testing.T, row map[string]json.RawMessage, column string) string {
	t.Helper()
	value, ok := rowUUID(t, row, column)
	if !ok {
		t.Fatalf("row is missing UUID column %q", column)
	}
	return value
}

func rowUUIDOptional(t *testing.T, row map[string]json.RawMessage, column string) string {
	t.Helper()
	raw, ok := row[column]
	if !ok {
		return ""
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("decode %s optional uuid: %v", column, err)
	}
	values := ovsdbUUIDSetValues(t, column, value)
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func rowUUIDSetContains(t *testing.T, row map[string]json.RawMessage, column, want string) bool {
	t.Helper()
	for _, got := range rowUUIDSetValuesFromColumn(t, row, column) {
		if got == want {
			return true
		}
	}
	return false
}

func rowUUIDSetValuesFromColumn(t *testing.T, row map[string]json.RawMessage, column string) []string {
	t.Helper()
	raw, ok := row[column]
	if !ok {
		return nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("decode %s uuid set: %v", column, err)
	}
	return ovsdbUUIDSetValues(t, column, value)
}

func ovsdbUUIDSetValues(t *testing.T, column string, value any) []string {
	t.Helper()
	if values, ok := value.([]any); ok {
		if len(values) == 0 {
			return nil
		}
		if len(values) > 0 {
			if first, ok := values[0].([]any); ok && len(first) == 2 && first[0] == "uuid" {
				out := make([]string, 0, len(values))
				for _, item := range values {
					uuidValue, ok := item.([]any)
					if !ok || len(uuidValue) != 2 || (uuidValue[0] != "uuid" && uuidValue[0] != "named-uuid") {
						t.Fatalf("column %s contains non-uuid set value: %v", column, item)
					}
					id, ok := uuidValue[1].(string)
					if !ok {
						t.Fatalf("column %s UUID value is not string: %v", column, item)
					}
					out = append(out, id)
				}
				return out
			}
		}
	}
	items := []any{value}
	if outer, ok := value.([]any); ok && len(outer) == 2 && outer[0] == "set" {
		if setItems, ok := outer[1].([]any); ok {
			items = setItems
		}
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		uuidValue, ok := item.([]any)
		if !ok || len(uuidValue) != 2 || (uuidValue[0] != "uuid" && uuidValue[0] != "named-uuid") {
			t.Fatalf("column %s contains non-uuid set value: %v", column, item)
		}
		id, ok := uuidValue[1].(string)
		if !ok {
			t.Fatalf("column %s UUID value is not string: %v", column, item)
		}
		out = append(out, id)
	}
	return out
}

func rowStringSetValuesFromColumn(t *testing.T, row map[string]json.RawMessage, column string) []string {
	t.Helper()
	raw, ok := row[column]
	if !ok {
		return nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("decode %s string set: %v", column, err)
	}
	items := []any{value}
	if outer, ok := value.([]any); ok {
		if len(outer) == 0 {
			return nil
		}
		if len(outer) == 2 && outer[0] == "set" {
			setItems, ok := outer[1].([]any)
			if !ok {
				t.Fatalf("column %s has invalid set shape: %v", column, value)
			}
			items = setItems
		}
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		s, ok := item.(string)
		if !ok {
			t.Fatalf("column %s contains non-string set value: %v", column, item)
		}
		out = append(out, s)
	}
	return out
}

func rowStringMap(t *testing.T, row map[string]json.RawMessage, column string) map[string]string {
	t.Helper()
	raw, ok := row[column]
	if !ok {
		return map[string]string{}
	}

	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("decode %s map: %v", column, err)
	}
	result := map[string]string{}
	outer, ok := value.([]any)
	if !ok || len(outer) != 2 || outer[0] != "map" {
		t.Fatalf("column %s is not an OVSDB map: %s", column, string(raw))
	}
	pairs, ok := outer[1].([]any)
	if !ok {
		t.Fatalf("column %s has invalid OVSDB map pairs: %s", column, string(raw))
	}
	for _, pairValue := range pairs {
		pair, ok := pairValue.([]any)
		if !ok || len(pair) != 2 {
			t.Fatalf("column %s has invalid OVSDB map pair: %v", column, pairValue)
		}
		key, keyOK := pair[0].(string)
		val, valOK := pair[1].(string)
		if !keyOK || !valOK {
			t.Fatalf("column %s map pair is not string:string: %v", column, pair)
		}
		result[key] = val
	}
	return result
}

func rowIntMap(t *testing.T, row map[string]json.RawMessage, column string) map[string]int {
	t.Helper()
	raw, ok := row[column]
	if !ok {
		return map[string]int{}
	}

	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("decode %s map: %v", column, err)
	}
	result := map[string]int{}
	outer, ok := value.([]any)
	if !ok || len(outer) != 2 || outer[0] != "map" {
		t.Fatalf("column %s is not an OVSDB map: %s", column, string(raw))
	}
	pairs, ok := outer[1].([]any)
	if !ok {
		t.Fatalf("column %s has invalid OVSDB map pairs: %s", column, string(raw))
	}
	for _, pairValue := range pairs {
		pair, ok := pairValue.([]any)
		if !ok || len(pair) != 2 {
			t.Fatalf("column %s has invalid OVSDB map pair: %v", column, pairValue)
		}
		key, keyOK := pair[0].(string)
		val, valOK := pair[1].(float64)
		if !keyOK || !valOK {
			t.Fatalf("column %s map pair is not string:int: %v", column, pair)
		}
		result[key] = int(val)
	}
	return result
}

func updatesContainName(updates ovsdbjson.TableUpdates, table, name string) bool {
	for _, update := range updates[table] {
		if update.New == nil {
			continue
		}
		raw, ok := update.New["name"]
		if !ok {
			continue
		}
		var got string
		if err := json.Unmarshal(raw, &got); err == nil && got == name {
			return true
		}
	}
	return false
}

func testContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func uniqueSuffix() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

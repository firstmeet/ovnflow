//go:build integration

package ovnflow

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/firstmeet/ovnflow/internal/ovsdbjson"
)

var v2NorthboundSchemaPlan = []integrationSchemaCheck{
	{table: tableLogicalSwitch, columns: []string{colName, colPorts, colExternalIDs, colOtherConfig}},
	{table: tableLogicalSwitchPort, columns: []string{colName, colAddresses, colExternalIDs, colOptions}},
	{table: tableDNS, columns: []string{colRecords, colExternalIDs}},
	{table: tableACL, columns: []string{colPriority, colDirection, colMatch, colAction, colExternalIDs}},
	{table: tablePortGroup, columns: []string{colName, colACLs, colExternalIDs}},
}

var v2OpenVSwitchSchemaPlan = []integrationSchemaCheck{
	{table: tableBridge, columns: []string{colName, colPorts, colExternalIDs}},
	{table: tablePort, columns: []string{colName, colInterfaces, colExternalIDs}},
	{table: tableInterface, columns: []string{colName, colType, colExternalIDs}},
}

func TestIntegrationV2ReadinessAreEnvGated(t *testing.T) {
	if !EnvGateEnabled(os.Getenv(EnvV2SchemaChecks)) {
		t.Skip(EnvV2SchemaChecks + " not enabled")
	}
	cfg := requireIntegrationConfig(t)
	if cfg.OVNNBAddr == "" || cfg.OVNSBAddr == "" || cfg.OVSAddr == "" {
		t.Fatalf("%s requires OVN/OVS endpoints in integration env", EnvV2SchemaChecks)
	}
	rawNB := dialOVSDBOrSkip(t, cfg.OVNNBAddr)
	t.Cleanup(func() { _ = rawNB.Close() })
	schema := getIntegrationSchema(t, rawNB, nbDatabase)
	assertSchemaReadiness(t, schema, v2NorthboundSchemaPlan)

	rawOVS := dialOVSDBOrSkip(t, cfg.OVSAddr)
	t.Cleanup(func() { _ = rawOVS.Close() })
	ovsSchema := getIntegrationSchema(t, rawOVS, ovsDatabase)
	assertSchemaReadiness(t, ovsSchema, v2OpenVSwitchSchemaPlan)
}

func TestIntegrationV2MutationGateIsEnvGated(t *testing.T) {
	if !EnvGateEnabled(os.Getenv(EnvV2MutationChecks)) {
		t.Skip(EnvV2MutationChecks + " not enabled")
	}
	cfg := requireIntegrationConfig(t)
	if cfg.OVNNBAddr == "" || cfg.OVSAddr == "" {
		t.Fatalf("%s requires OVN NB and OVS endpoints in integration env", EnvV2MutationChecks)
	}
	sdk := connectSDKOrSkip(t, cfg)
	t.Cleanup(sdk.Close)
	rawNB := dialOVSDBOrSkip(t, cfg.OVNNBAddr)
	t.Cleanup(func() { _ = rawNB.Close() })
	rawOVS := dialOVSDBOrSkip(t, cfg.OVSAddr)
	t.Cleanup(func() { _ = rawOVS.Close() })

	suffix := uniqueSuffix()
	prefix := cfg.ResourcePrefix + "v2-" + suffix + "-"
	resources := v2Resources{
		virtualNetwork: prefix + "net",
		dns:            prefix + "dns",
		attachment:     prefix + "att",
		policy:         prefix + "policy",
		bridge:         cfg.BridgeName + "-v2-" + suffix,
		ovsPort:        prefix + "port",
		ovsInterface:   prefix + "iface",
	}
	ctx := testContext(t)
	requireSafeBridgeTarget(t, rawOVS, resources.bridge)
	cleanupV2(ctx, t, sdk, rawNB, rawOVS, resources)
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cleanupV2(cleanupCtx, t, sdk, rawNB, rawOVS, resources)
	})

	must(t, sdk.LocalOVS().Bridge(resources.bridge).Ensure().WithExternalID(testMarkerKey, testMarkerValue).Execute(ctx), "ensure v2 local OVS bridge")

	must(t, sdk.OVN().NB().VirtualNetwork(resources.virtualNetwork).
		Ensure().
		WithCIDR("10.230.0.0/24").
		WithOwner("project", "v2-"+suffix).
		WithLabel("suite", "v2").
		WithDNS(resources.dns, func(d *LogicalSwitchDNSBuilder) {
			d.AddRecord("api.v2.ovnflow.test", "10.230.0.10", "10.230.0.11")
		}).
		Execute(ctx), "ensure virtual network")
	must(t, sdk.OVN().NB().VirtualNetwork(resources.virtualNetwork).
		Ensure().
		WithCIDR("10.230.0.0/24").
		WithOwner("project", "v2-"+suffix).
		WithDNS(resources.dns, func(d *LogicalSwitchDNSBuilder) {
			d.AddRecord("api.v2.ovnflow.test", "10.230.0.11", "10.230.0.10")
		}).
		Execute(ctx), "repeat ensure virtual network")

	must(t, sdk.WorkloadAttachment(resources.attachment).
		Ensure().
		OnNetwork(resources.virtualNetwork).
		WithWorkload("vm-"+suffix).
		WithInterface("eth0").
		WithMAC("00:16:3e:23:00:10").
		WithIP("10.230.0.10").
		WithOwner("project", "v2-"+suffix).
		SyncLocalOVS(resources.bridge).
		WithOVSPort(resources.ovsPort).
		WithOVSInterface(resources.ovsInterface).
		WithOVSInterfaceType("internal").
		Execute(ctx), "ensure workload attachment")

	must(t, sdk.WorkloadAttachment(resources.attachment).
		Ensure().
		OnNetwork(resources.virtualNetwork).
		WithWorkload("vm-"+suffix).
		WithInterface("eth0").
		WithMAC("00:16:3e:23:00:10").
		WithIP("10.230.0.10").
		WithOwner("project", "v2-"+suffix).
		SyncLocalOVS(resources.bridge).
		WithOVSPort(resources.ovsPort).
		WithOVSInterface(resources.ovsInterface).
		WithOVSInterfaceType("internal").
		Execute(ctx), "repeat ensure workload attachment with local OVS")

	must(t, sdk.OVN().NB().SecurityPolicy(resources.policy).
		Ensure().
		ForSubject(resources.policy).
		WithOwner("project", "v2-"+suffix).
		AddRule(SecurityRule{Name: "allow-web", Action: "allow", Protocol: "tcp", CIDRs: []string{"10.230.0.0/24"}, Ports: []int{80}}).
		Execute(ctx), "ensure security policy")

	assertV2Readback(t, rawNB, resources, "v2-"+suffix)
	assertV2LocalOVSReadback(t, rawOVS, resources, "v2-"+suffix)
	must(t, sdk.WorkloadAttachment(resources.attachment).DetachLocalOVS(ctx), "detach workload local OVS")
	assertV2LocalOVSDetached(t, rawOVS, resources)
	cleanupV2(ctx, t, sdk, rawNB, rawOVS, resources)
	assertV2Cleanup(t, rawNB, rawOVS, resources)
}

func TestIntegrationLinuxRouterGateIsEnvGated(t *testing.T) {
	if !EnvGateEnabled(os.Getenv(EnvLinuxRouterChecks)) {
		t.Skip(EnvLinuxRouterChecks + " not enabled")
	}
	if !ValidNATBackend(os.Getenv(EnvLinuxRouterNATBackend)) {
		t.Fatalf("invalid %s value %q", EnvLinuxRouterNATBackend, os.Getenv(EnvLinuxRouterNATBackend))
	}
}

type v2Resources struct {
	virtualNetwork string
	dns            string
	attachment     string
	policy         string
	bridge         string
	ovsPort        string
	ovsInterface   string
}

func cleanupV2(ctx context.Context, t *testing.T, sdk *Client, rawNB, rawOVS *ovsdbjson.Client, resources v2Resources) {
	t.Helper()
	if rawOVS != nil {
		cleanupOVS(ctx, t, rawOVS, resources.bridge, resources.ovsPort, resources.ovsInterface)
	}
	_ = sdk.OVN().NB().PortGroup(resources.policy).Delete().Execute(ctx)
	_ = sdk.OVN().NB().DNS(resources.dns).Delete().Execute(ctx)
	_ = sdk.OVN().NB().LogicalSwitch(resources.virtualNetwork).Delete().Execute(ctx)
	_, err := rawNB.Transact(ctx, nbDatabase,
		map[string]any{"op": "delete", "table": tableACL, "where": externalIDWhere(ExternalIDNameKey, resources.policy)},
		map[string]any{"op": "delete", "table": tablePortGroup, "where": nameWhere(resources.policy)},
		map[string]any{"op": "delete", "table": tableDNS, "where": externalIDWhere(dnsNameExternalID, resources.dns)},
		map[string]any{"op": "delete", "table": tableLogicalSwitch, "where": nameWhere(resources.virtualNetwork)},
		map[string]any{"op": "delete", "table": tableLogicalSwitchPort, "where": nameWhere(resources.attachment)},
	)
	if err != nil {
		t.Logf("fallback cleanup v2 northbound: %v", err)
	}
}

func assertV2Readback(t *testing.T, raw *ovsdbjson.Client, resources v2Resources, ownerName string) {
	t.Helper()
	ls := requireOneRow(t, raw, nbDatabase, tableLogicalSwitch, nameWhere(resources.virtualNetwork), []string{colName, colExternalIDs})
	requireStringMapValue(t, ls, colExternalIDs, ExternalIDKindKey, "VirtualNetwork")
	requireStringMapValue(t, ls, colExternalIDs, ExternalIDOwnerNameKey, ownerName)

	dns := requireOneRow(t, raw, nbDatabase, tableDNS, externalIDWhere(dnsNameExternalID, resources.dns), []string{colRecords, colExternalIDs})
	requireStringMapValue(t, dns, colExternalIDs, ExternalIDKindKey, "LogicalSwitchDNS")
	records := rowStringMap(t, dns, colRecords)
	if got := records["api.v2.ovnflow.test"]; got != "10.230.0.10 10.230.0.11" {
		t.Fatalf("v2 DNS record = %q", got)
	}

	lsp := requireOneRow(t, raw, nbDatabase, tableLogicalSwitchPort, nameWhere(resources.attachment), []string{colName, colAddresses, colExternalIDs})
	requireStringMapValue(t, lsp, colExternalIDs, ExternalIDKindKey, "WorkloadAttachment")
	requireStringMapValue(t, lsp, colExternalIDs, ExternalIDPrefix+"workload", "vm-"+trimV2OwnerSuffix(ownerName))

	pg := requireOneRow(t, raw, nbDatabase, tablePortGroup, nameWhere(resources.policy), []string{colName, colACLs, colExternalIDs})
	requireStringMapValue(t, pg, colExternalIDs, ExternalIDKindKey, "SecurityPolicy")
	acls := rowStringSet(t, pg, colACLs)
	if len(acls) == 0 {
		t.Fatalf("v2 policy has no ACL refs: %s", string(pg[colACLs]))
	}
}

func assertV2LocalOVSReadback(t *testing.T, raw *ovsdbjson.Client, resources v2Resources, ownerName string) {
	t.Helper()
	bridge := requireOneRow(t, raw, ovsDatabase, tableBridge, nameWhere(resources.bridge), []string{colUUID, colName, colPorts, colExternalIDs})
	requireStringMapValue(t, bridge, colExternalIDs, testMarkerKey, testMarkerValue)

	port := requireOneRow(t, raw, ovsDatabase, tablePort, nameWhere(resources.ovsPort), []string{colUUID, colName, colInterfaces, colExternalIDs})
	portUUID := rowUUIDMust(t, port, colUUID)
	if !rowUUIDSetContains(t, bridge, colPorts, portUUID) {
		t.Fatalf("v2 local bridge %q does not reference port %q", resources.bridge, resources.ovsPort)
	}
	requireStringMapValue(t, port, colExternalIDs, ExternalIDKindKey, "WorkloadAttachment")
	requireStringMapValue(t, port, colExternalIDs, ExternalIDNameKey, resources.attachment)
	requireStringMapValue(t, port, colExternalIDs, ExternalIDOwnerNameKey, ownerName)
	requireStringMapValue(t, port, colExternalIDs, ExternalIDPrefix+"network", resources.virtualNetwork)
	requireStringMapValue(t, port, colExternalIDs, ExternalIDPrefix+"workload", "vm-"+trimV2OwnerSuffix(ownerName))

	iface := requireOneRow(t, raw, ovsDatabase, tableInterface, nameWhere(resources.ovsInterface), []string{colUUID, colName, colType, colExternalIDs})
	ifaceUUID := rowUUIDMust(t, iface, colUUID)
	if !rowUUIDSetContains(t, port, colInterfaces, ifaceUUID) {
		t.Fatalf("v2 local port %q does not reference interface %q", resources.ovsPort, resources.ovsInterface)
	}
	requireString(t, iface, colType, "internal")
	requireStringMapValue(t, iface, colExternalIDs, "iface-id", resources.attachment)
	requireStringMapValue(t, iface, colExternalIDs, ExternalIDKindKey, "WorkloadAttachment")
	requireStringMapValue(t, iface, colExternalIDs, ExternalIDNameKey, resources.attachment)
	requireStringMapValue(t, iface, colExternalIDs, ExternalIDOwnerNameKey, ownerName)
}

func assertV2LocalOVSDetached(t *testing.T, raw *ovsdbjson.Client, resources v2Resources) {
	t.Helper()
	if rows := selectRows(t, raw, ovsDatabase, tableBridge, nameWhere(resources.bridge), []string{colName}); len(rows) != 1 {
		t.Fatalf("v2 local bridge rows after detach = %d, want 1", len(rows))
	}
	if rows := selectRows(t, raw, ovsDatabase, tablePort, nameWhere(resources.ovsPort), []string{colName}); len(rows) != 0 {
		t.Fatalf("v2 local OVS Port rows after detach = %d, want 0", len(rows))
	}
	if rows := selectRows(t, raw, ovsDatabase, tableInterface, nameWhere(resources.ovsInterface), []string{colName}); len(rows) != 0 {
		t.Fatalf("v2 local OVS Interface rows after detach = %d, want 0", len(rows))
	}
}

func assertV2Cleanup(t *testing.T, rawNB, rawOVS *ovsdbjson.Client, resources v2Resources) {
	t.Helper()
	checks := []struct {
		table   string
		where   []any
		columns []string
	}{
		{table: tableLogicalSwitch, where: nameWhere(resources.virtualNetwork), columns: []string{colName}},
		{table: tableLogicalSwitchPort, where: nameWhere(resources.attachment), columns: []string{colName}},
		{table: tableDNS, where: externalIDWhere(dnsNameExternalID, resources.dns), columns: []string{colExternalIDs}},
		{table: tablePortGroup, where: nameWhere(resources.policy), columns: []string{colName}},
		{table: tableACL, where: externalIDWhere(ExternalIDNameKey, resources.policy), columns: []string{colExternalIDs}},
	}
	for _, check := range checks {
		if rows := selectRows(t, rawNB, nbDatabase, check.table, check.where, check.columns); len(rows) != 0 {
			t.Fatalf("expected %s cleanup, found %d rows", check.table, len(rows))
		}
	}
	if rawOVS != nil {
		if rows := selectRows(t, rawOVS, ovsDatabase, tableBridge, nameWhere(resources.bridge), []string{colName}); len(rows) != 0 {
			t.Fatalf("expected v2 local OVS bridge cleanup, found %d rows", len(rows))
		}
		if rows := selectRows(t, rawOVS, ovsDatabase, tablePort, nameWhere(resources.ovsPort), []string{colName}); len(rows) != 0 {
			t.Fatalf("expected v2 local OVS port cleanup, found %d rows", len(rows))
		}
		if rows := selectRows(t, rawOVS, ovsDatabase, tableInterface, nameWhere(resources.ovsInterface), []string{colName}); len(rows) != 0 {
			t.Fatalf("expected v2 local OVS interface cleanup, found %d rows", len(rows))
		}
	}
}

func rowStringSet(t *testing.T, row map[string]json.RawMessage, column string) []string {
	t.Helper()
	var raw any
	if err := json.Unmarshal(row[column], &raw); err != nil {
		t.Fatalf("decode %s: %v", column, err)
	}
	if values, ok := raw.([]any); ok && len(values) == 2 {
		if tag, ok := values[0].(string); ok && tag == "uuid" {
			if value, ok := values[1].(string); ok {
				return []string{value}
			}
		}
		if tag, ok := values[0].(string); ok && tag == "set" {
			if items, ok := values[1].([]any); ok {
				out := make([]string, 0, len(items))
				for _, item := range items {
					if uuid, ok := item.([]any); ok && len(uuid) == 2 {
						if value, ok := uuid[1].(string); ok {
							out = append(out, value)
						}
					}
				}
				return out
			}
		}
	}
	return nil
}

func trimV2OwnerSuffix(owner string) string {
	return strings.TrimPrefix(owner, "v2-")
}

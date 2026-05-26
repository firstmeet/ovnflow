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

	suffix := uniqueSuffix()
	prefix := cfg.ResourcePrefix + "v2-" + suffix + "-"
	resources := v2Resources{
		virtualNetwork: prefix + "net",
		dns:            prefix + "dns",
		attachment:     prefix + "att",
		policy:         prefix + "policy",
	}
	ctx := testContext(t)
	cleanupV2(ctx, t, sdk, rawNB, resources)
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cleanupV2(cleanupCtx, t, sdk, rawNB, resources)
	})

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

	must(t, sdk.OVN().NB().WorkloadAttachment(resources.attachment).
		Ensure().
		OnNetwork(resources.virtualNetwork).
		WithWorkload("vm-"+suffix).
		WithInterface("eth0").
		WithMAC("00:16:3e:23:00:10").
		WithIP("10.230.0.10").
		WithOwner("project", "v2-"+suffix).
		Execute(ctx), "ensure workload attachment")

	must(t, sdk.OVN().NB().SecurityPolicy(resources.policy).
		Ensure().
		ForSubject(resources.policy).
		WithOwner("project", "v2-"+suffix).
		AddRule(SecurityRule{Name: "allow-web", Action: "allow", Protocol: "tcp", CIDRs: []string{"10.230.0.0/24"}, Ports: []int{80}}).
		Execute(ctx), "ensure security policy")

	assertV2Readback(t, rawNB, resources, "v2-"+suffix)
	cleanupV2(ctx, t, sdk, rawNB, resources)
	assertV2Cleanup(t, rawNB, resources)
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
}

func cleanupV2(ctx context.Context, t *testing.T, sdk *Client, raw *ovsdbjson.Client, resources v2Resources) {
	t.Helper()
	_ = sdk.OVN().NB().PortGroup(resources.policy).Delete().Execute(ctx)
	_ = sdk.OVN().NB().DNS(resources.dns).Delete().Execute(ctx)
	_ = sdk.OVN().NB().LogicalSwitch(resources.virtualNetwork).Delete().Execute(ctx)
	_, err := raw.Transact(ctx, nbDatabase,
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

func assertV2Cleanup(t *testing.T, raw *ovsdbjson.Client, resources v2Resources) {
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
		if rows := selectRows(t, raw, nbDatabase, check.table, check.where, check.columns); len(rows) != 0 {
			t.Fatalf("expected %s cleanup, found %d rows", check.table, len(rows))
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

package ovnflow

import (
	"context"
	"testing"

	libovsdb "github.com/ovn-kubernetes/libovsdb/ovsdb"
)

func TestOwnershipAuditCleanV2Resources(t *testing.T) {
	nb := testNBDBClient(t)
	nb.executor = &nbRecordingExecutor{results: []libovsdb.OperationResult{
		{Rows: []libovsdb.Row{{
			colUUID:        uuidValue("ls-uuid"),
			colName:        "net-a",
			colPorts:       []string{"lsp-workload", "lsp-localnet"},
			colExternalIDs: ovsMap(testOwnedExternalIDs("VirtualNetwork", "net-a")),
		}}},
		{Rows: []libovsdb.Row{
			{
				colUUID:        uuidValue("lsp-workload"),
				colName:        "att-a",
				colExternalIDs: ovsMap(mergeStringMaps(testOwnedExternalIDs("WorkloadAttachment", "att-a"), map[string]string{ExternalIDPrefix + "network": "net-a"})),
			},
			{
				colUUID: uuidValue("lsp-localnet"),
				colName: "ln-public",
				colType: "localnet",
				colOptions: ovsMap(map[string]string{
					"network_name": "physnet1",
				}),
				colExternalIDs: ovsMap(mergeStringMaps(testOwnedExternalIDs("ProviderNetwork", "public"), map[string]string{
					ExternalIDPrefix + "logical-switch":   "net-a",
					ExternalIDPrefix + "physical-network": "physnet1",
					ExternalIDPrefix + "bridge":           "br-ex",
				})),
			},
		}},
		{Rows: nil},
		{Rows: []libovsdb.Row{{
			colUUID:        uuidValue("pg-uuid"),
			colName:        "allow-web",
			colACLs:        []string{"acl-uuid"},
			colExternalIDs: ovsMap(testOwnedExternalIDs("SecurityPolicy", "allow-web")),
		}}},
		{Rows: []libovsdb.Row{{
			colUUID:        uuidValue("acl-uuid"),
			colExternalIDs: ovsMap(testOwnedExternalIDs("SecurityPolicy", "allow-web")),
		}}},
	}}
	ovs := testOVSDBClient(t)
	ovs.executor = &recordingExecutor{results: []libovsdb.OperationResult{
		{Rows: []libovsdb.Row{{
			colUUID:    uuidValue("ovs-root"),
			colBridges: []string{"br-uuid"},
			colExternalIDs: ovsMap(map[string]string{
				ovsBridgeMappingsKey:                       "physnet1:br-ex",
				providerNetworkMappingOwnerKey("physnet1"): "public",
			}),
		}}},
		{Rows: []libovsdb.Row{{
			colUUID:  uuidValue("br-uuid"),
			colName:  "br-ex",
			colPorts: []string{"port-uuid"},
		}}},
		{Rows: []libovsdb.Row{{
			colUUID:        uuidValue("port-uuid"),
			colName:        "vnet0",
			colInterfaces:  []string{"iface-uuid"},
			colExternalIDs: ovsMap(testOwnedExternalIDs("WorkloadAttachment", "att-a")),
		}}},
		{Rows: []libovsdb.Row{{
			colUUID:        uuidValue("iface-uuid"),
			colName:        "vnet0",
			colExternalIDs: ovsMap(mergeStringMaps(testOwnedExternalIDs("WorkloadAttachment", "att-a"), map[string]string{"iface-id": "att-a"})),
		}}},
	}}

	report, err := (&Client{nb: nb, ovs: ovs}).Diagnostics().AuditOwnership(context.Background(), OwnershipAuditOptions{})
	if err != nil {
		t.Fatalf("AuditOwnership returned error: %v", err)
	}
	if report.Summary.Findings != 0 {
		t.Fatalf("findings = %#v, want none", report.Findings)
	}
	if report.Summary.OwnedResources != 7 {
		t.Fatalf("owned resources = %d, want 7: %#v", report.Summary.OwnedResources, report.Resources)
	}
}

func TestOwnershipAuditReportsOrphans(t *testing.T) {
	nb := testNBDBClient(t)
	nb.executor = &nbRecordingExecutor{results: []libovsdb.OperationResult{
		{Rows: []libovsdb.Row{{
			colUUID:        uuidValue("ls-uuid"),
			colName:        "net-a",
			colPorts:       []string{"missing-lsp"},
			colExternalIDs: ovsMap(testOwnedExternalIDs("VirtualNetwork", "net-a")),
		}}},
		{Rows: []libovsdb.Row{
			{
				colUUID:        uuidValue("lsp-workload"),
				colName:        "att-a",
				colExternalIDs: ovsMap(mergeStringMaps(testOwnedExternalIDs("WorkloadAttachment", "att-a"), map[string]string{ExternalIDPrefix + "network": "missing-net"})),
			},
			{
				colUUID: uuidValue("lsp-localnet"),
				colName: "ln-public",
				colType: "localnet",
				colOptions: ovsMap(map[string]string{
					"network_name": "physnet1",
				}),
				colExternalIDs: ovsMap(mergeStringMaps(testOwnedExternalIDs("ProviderNetwork", "public"), map[string]string{
					ExternalIDPrefix + "logical-switch":   "net-a",
					ExternalIDPrefix + "physical-network": "physnet1",
					ExternalIDPrefix + "bridge":           "missing-br",
				})),
			},
		}},
		{Rows: nil},
		{Rows: []libovsdb.Row{{
			colUUID:        uuidValue("pg-uuid"),
			colName:        "allow-web",
			colACLs:        []string{"missing-acl"},
			colExternalIDs: ovsMap(testOwnedExternalIDs("SecurityPolicy", "allow-web")),
		}}},
		{Rows: nil},
	}}
	ovs := testOVSDBClient(t)
	ovs.executor = &recordingExecutor{results: []libovsdb.OperationResult{
		{Rows: []libovsdb.Row{{
			colUUID: uuidValue("ovs-root"),
			colExternalIDs: ovsMap(map[string]string{
				ovsBridgeMappingsKey:                       "physnet1:missing-br",
				providerNetworkMappingOwnerKey("physnet1"): "public",
			}),
		}}},
		{Rows: nil},
		{Rows: []libovsdb.Row{{
			colUUID:        uuidValue("port-uuid"),
			colName:        "vnet0",
			colInterfaces:  []string{"missing-iface"},
			colExternalIDs: ovsMap(testOwnedExternalIDs("WorkloadAttachment", "att-a")),
		}}},
		{Rows: nil},
	}}

	report, err := (&Client{nb: nb, ovs: ovs}).Diagnostics().AuditOwnership(context.Background(), OwnershipAuditOptions{})
	if err != nil {
		t.Fatalf("AuditOwnership returned error: %v", err)
	}
	for _, code := range []string{
		"missing_logical_switch_port",
		"provider_network_bridge_missing",
		"workload_network_missing",
		"workload_interface_missing",
		"security_policy_acl_missing",
		"ovs_interface_missing",
	} {
		if !ownershipAuditHasFinding(report, code) {
			t.Fatalf("missing finding %q in %#v", code, report.Findings)
		}
	}
	if report.Summary.Errors == 0 {
		t.Fatalf("summary = %#v, want errors", report.Summary)
	}
}

func testOwnedExternalIDs(kind, name string) map[string]string {
	return map[string]string{
		ExternalIDManagedByKey:  "ovnflow",
		ExternalIDAPIVersionKey: "v2",
		ExternalIDKindKey:       kind,
		ExternalIDNameKey:       name,
		ExternalIDOwnerKindKey:  "project",
		ExternalIDOwnerNameKey:  "alpha",
	}
}

func ownershipAuditHasFinding(report *OwnershipAuditReport, code string) bool {
	for _, finding := range report.Findings {
		if finding.Code == code {
			return true
		}
	}
	return false
}

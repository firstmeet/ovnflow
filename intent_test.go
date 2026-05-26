package ovnflow

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	libovsdb "github.com/ovn-kubernetes/libovsdb/ovsdb"
)

func TestOwnerExternalIDsEncodeReservedLabels(t *testing.T) {
	ids, err := (OwnerRef{Kind: "project", Name: "alpha"}).ExternalIDs(Labels{"team/name": "net"})
	if err != nil {
		t.Fatalf("ExternalIDs returned error: %v", err)
	}
	key := ExternalIDLabelKey("team/name")
	if ids[ExternalIDManagedByKey] != "ovnflow" || ids[ExternalIDOwnerKindKey] != "project" || ids[key] != "net" {
		t.Fatalf("external ids = %#v", ids)
	}
	decoded, ok := DecodeExternalIDLabelKey(key)
	if !ok || decoded != "team/name" {
		t.Fatalf("DecodeExternalIDLabelKey = %q, %v", decoded, ok)
	}
	if !IsReservedExternalIDKey(key) {
		t.Fatalf("%q should be reserved", key)
	}
}

func TestIntentValidationFailures(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{name: "virtual network cidr", err: (VirtualNetwork{Name: "net", CIDRs: []string{"bad"}}).Validate()},
		{name: "dns ip", err: (LogicalSwitchDNS{Records: []DNSRecord{{Domain: "svc.local", IPs: []string{"bad"}}}}).Validate()},
		{name: "workload mac", err: (WorkloadAttachment{Name: "att", Network: "net", MAC: "bad"}).Validate()},
		{name: "workload owner", err: (WorkloadAttachment{Name: "att", Network: "net", Owner: OwnerRef{Kind: "project"}}).Validate()},
		{name: "security cidr", err: (SecurityPolicy{Name: "pol", Rules: []SecurityRule{{Action: "allow", CIDRs: []string{"bad"}}}}).Validate()},
		{name: "security owner", err: (SecurityPolicy{Name: "pol", Owner: OwnerRef{Kind: "project"}}).Validate()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !IsKind(tt.err, ErrorValidation) {
				t.Fatalf("error kind = %q for %v, want validation", KindOf(tt.err), tt.err)
			}
		})
	}
}

func TestLogicalSwitchDNSAllowsMultipleIPsPerDomain(t *testing.T) {
	dns := LogicalSwitchDNS{Records: []DNSRecord{
		{Domain: "api.service", IPs: []string{"10.0.0.2"}},
		{Domain: "api.service", IPs: []string{"10.0.0.3", "10.0.0.1"}},
	}}
	got := dns.RecordMap()["api.service"]
	want := []string{"10.0.0.1", "10.0.0.2", "10.0.0.3"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("record ips = %#v, want %#v", got, want)
	}
}

func TestVirtualNetworkNoopPlanAndReconcile(t *testing.T) {
	builder := (&NBClient{}).VirtualNetwork("net-a").
		Ensure().
		WithCIDR("10.0.0.0/24").
		WithGateway("10.0.0.1").
		WithDNS("net-a", func(d *LogicalSwitchDNSBuilder) {
			d.AddRecord("api.service", "10.0.0.2", "10.0.0.3")
		})
	dryRun, err := builder.DryRun(context.Background())
	if err != nil {
		t.Fatalf("DryRun returned error: %v", err)
	}
	if len(dryRun.Plan.Operations) != 1 || dryRun.Plan.Operations[0].Resource != "VirtualNetwork" {
		t.Fatalf("dry run plan = %#v", dryRun.Plan)
	}
	if len(dryRun.Diff.Changes) != 1 || dryRun.Diff.Changes[0].Path != "/" {
		t.Fatalf("dry run diff = %#v, want create diff", dryRun.Diff)
	}
	reconciled, err := builder.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}
	if reconciled.Applied {
		t.Fatalf("Reconcile applied real changes in interface-freeze stub")
	}
}

func TestAttachmentAndPolicyBuildersAreSymmetric(t *testing.T) {
	ctx := context.Background()
	attachment := (&NBClient{}).WorkloadAttachment("att-a").
		Ensure().
		OnNetwork("net-a").
		WithInterface("eth0").
		WithMAC("00:16:3e:11:22:33").
		WithIP("10.0.0.10")
	if _, err := attachment.DryRun(ctx); err != nil {
		t.Fatalf("attachment DryRun returned error: %v", err)
	}
	if reconciled, err := attachment.Reconcile(ctx); err != nil || reconciled.Applied {
		t.Fatalf("attachment Reconcile = %#v, %v", reconciled, err)
	}
	if err := attachment.Execute(ctx); err != nil {
		t.Fatalf("attachment Execute returned error: %v", err)
	}

	policy := (&NBClient{}).SecurityPolicy("allow-web").
		Ensure().
		ForSubject("net-a").
		AddRule(SecurityRule{Action: "allow", CIDRs: []string{"10.0.0.0/24"}, Ports: []int{80}})
	if _, err := policy.DryRun(ctx); err != nil {
		t.Fatalf("policy DryRun returned error: %v", err)
	}
	if reconciled, err := policy.Reconcile(ctx); err != nil || reconciled.Applied {
		t.Fatalf("policy Reconcile = %#v, %v", reconciled, err)
	}
	if err := policy.Execute(ctx); err != nil {
		t.Fatalf("policy Execute returned error: %v", err)
	}
}

func TestTypedErrorsIncludeV2Kinds(t *testing.T) {
	err := &Error{Kind: ErrorAmbiguous}
	if !errors.Is(err, ErrAmbiguous) {
		t.Fatalf("errors.Is did not match ambiguous")
	}
}

func TestV2IntentRequiresOwnerForRealMutation(t *testing.T) {
	db := testNBDBClient(t)
	db.executor = &nbRecordingExecutor{}
	_, err := (&NBClient{db: db}).VirtualNetwork("net-a").Ensure().WithCIDR("10.0.0.0/24").Reconcile(context.Background())
	if !IsKind(err, ErrorOwnershipViolation) {
		t.Fatalf("Reconcile error = %v, want ownership violation", err)
	}
}

func TestV2IntentApplyRequiresBackend(t *testing.T) {
	err := (&NBClient{}).VirtualNetwork("net-a").Apply(context.Background(), VirtualNetwork{Name: "net-a"})
	if !IsKind(err, ErrorBackendUnavailable) {
		t.Fatalf("Apply error = %v, want backend unavailable", err)
	}
}

func TestVirtualNetworkReconcileWritesLogicalSwitchAndDNS(t *testing.T) {
	rec := &nbRecordingExecutor{
		results: []libovsdb.OperationResult{
			{Rows: nil},
			{Count: 1},
			{Rows: nil},
			{Count: 1},
		},
	}
	db := testNBDBClient(t)
	db.executor = rec
	nb := &NBClient{db: db}

	result, err := nb.VirtualNetwork("net-a").
		Ensure().
		WithCIDR("10.0.0.0/24").
		WithOwner("project", "alpha").
		WithLabel("env", "test").
		WithDNS("net-a-dns", func(d *LogicalSwitchDNSBuilder) {
			d.AddRecord("api.service", "10.0.0.3", "10.0.0.2")
		}).
		Reconcile(context.Background())
	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}
	if !result.Applied {
		t.Fatalf("Reconcile did not report applied")
	}
	if len(result.Diff.Changes) != 1 || result.Diff.Changes[0].Path != "/" {
		t.Fatalf("Reconcile diff = %#v, want create diff", result.Diff)
	}
	lsOp := findRecordedOp(rec.ops, libovsdb.OperationInsert, tableLogicalSwitch)
	if lsOp == nil {
		t.Fatalf("missing Logical_Switch insert in ops: %#v", rec.ops)
	}
	externalIDs := ovsMapStrings(t, lsOp.Row[colExternalIDs])
	if externalIDs[ExternalIDKindKey] != "VirtualNetwork" || externalIDs[ExternalIDOwnerNameKey] != "alpha" {
		t.Fatalf("logical switch external_ids = %#v", externalIDs)
	}
	dnsOp := findRecordedOp(rec.ops, libovsdb.OperationInsert, tableDNS)
	if dnsOp == nil {
		t.Fatalf("missing DNS insert in ops: %#v", rec.ops)
	}
	records := ovsMapStrings(t, dnsOp.Row[colRecords])
	if records["api.service"] != "10.0.0.2 10.0.0.3" {
		t.Fatalf("DNS records = %#v", records)
	}
}

func TestWorkloadAttachmentReconcileWritesLogicalSwitchPort(t *testing.T) {
	rec := &nbRecordingExecutor{
		results: []libovsdb.OperationResult{
			{Rows: []libovsdb.Row{{colUUID: uuidValue("ls-uuid"), colPorts: ovsSet()}}},
			{Rows: nil},
			{Count: 1},
			{Count: 1},
		},
	}
	db := testNBDBClient(t)
	db.executor = rec
	nb := &NBClient{db: db}

	result, err := nb.WorkloadAttachment("att-a").
		Ensure().
		OnNetwork("net-a").
		WithWorkload("vm-a").
		WithInterface("eth0").
		WithMAC("00:16:3e:11:22:33").
		WithIP("10.0.0.10").
		WithOwner("project", "alpha").
		Reconcile(context.Background())
	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}
	if !result.Applied {
		t.Fatalf("Reconcile did not report applied")
	}
	lspOp := findRecordedOp(rec.ops, libovsdb.OperationInsert, tableLogicalSwitchPort)
	if lspOp == nil {
		t.Fatalf("missing Logical_Switch_Port insert in ops: %#v", rec.ops)
	}
	if lspOp.Row[colName] != "att-a" {
		t.Fatalf("lsp row = %#v", lspOp.Row)
	}
	externalIDs := ovsMapStrings(t, lspOp.Row[colExternalIDs])
	if externalIDs[ExternalIDKindKey] != "WorkloadAttachment" || externalIDs[ExternalIDPrefix+"workload"] != "vm-a" {
		t.Fatalf("lsp external_ids = %#v", externalIDs)
	}
}

func TestSecurityPolicyReconcileWritesPortGroupAndInlineACL(t *testing.T) {
	rec := &nbRecordingExecutor{
		results: []libovsdb.OperationResult{
			{Rows: nil},
			{Rows: nil},
			{Count: 1},
			{Count: 1},
		},
	}
	db := testNBDBClient(t)
	db.executor = rec
	nb := &NBClient{db: db}

	result, err := nb.SecurityPolicy("allow-web").
		Ensure().
		ForSubject("pg-web").
		WithOwner("project", "alpha").
		AddRule(SecurityRule{Name: "web", Action: "allow", Protocol: "tcp", CIDRs: []string{"10.0.0.0/24"}, Ports: []int{80}}).
		Reconcile(context.Background())
	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}
	if !result.Applied {
		t.Fatalf("Reconcile did not report applied")
	}
	aclOp := findRecordedOp(rec.ops, libovsdb.OperationInsert, tableACL)
	if aclOp == nil {
		t.Fatalf("missing ACL insert in ops: %#v", rec.ops)
	}
	if aclOp.Row[colAction] != "allow" {
		t.Fatalf("acl row = %#v", aclOp.Row)
	}
	if match := aclOp.Row[colMatch].(string); match == "" || !strings.Contains(match, "tcp.dst == 80") {
		t.Fatalf("acl match = %q", match)
	}
	pgOp := findRecordedOp(rec.ops, libovsdb.OperationInsert, tablePortGroup)
	if pgOp == nil {
		t.Fatalf("missing Port_Group insert in ops: %#v", rec.ops)
	}
}

func TestVirtualNetworkGetReadsLogicalSwitchIntentMetadata(t *testing.T) {
	db := testNBDBClient(t)
	db.executor = &nbRecordingExecutor{results: []libovsdb.OperationResult{{Rows: []libovsdb.Row{{
		colUUID:        uuidValue("ls-uuid"),
		colName:        "net-a",
		colPorts:       ovsSet(),
		colOtherConfig: ovsMap(map[string]string{"subnet": "10.0.0.0/24", "gateway": "10.0.0.1"}),
		colExternalIDs: ovsMap(map[string]string{
			ExternalIDOwnerKindKey:    "project",
			ExternalIDOwnerNameKey:    "alpha",
			ExternalIDLabelKey("env"): "test",
		}),
	}}}}}
	got, err := (&NBClient{db: db}).VirtualNetwork("net-a").Get(context.Background())
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if got.Name != "net-a" || got.CIDRs[0] != "10.0.0.0/24" || got.Gateway != "10.0.0.1" || got.Owner.Name != "alpha" || got.Labels["env"] != "test" {
		t.Fatalf("virtual network = %#v", got)
	}
}

func TestVirtualNetworkDryRunDiffsExistingState(t *testing.T) {
	db := testNBDBClient(t)
	db.executor = &nbRecordingExecutor{results: []libovsdb.OperationResult{{Rows: []libovsdb.Row{{
		colUUID:        uuidValue("ls-uuid"),
		colName:        "net-a",
		colPorts:       ovsSet(),
		colOtherConfig: ovsMap(map[string]string{"subnet": "10.0.0.0/24"}),
		colExternalIDs: ovsMap(map[string]string{
			ExternalIDOwnerKindKey:    "project",
			ExternalIDOwnerNameKey:    "alpha",
			ExternalIDLabelKey("env"): "dev",
		}),
	}}}}}
	dryRun, err := (&NBClient{db: db}).VirtualNetwork("net-a").
		Ensure().
		WithCIDR("10.0.1.0/24").
		WithGateway("10.0.1.1").
		WithOwner("project", "alpha").
		WithLabel("env", "prod").
		DryRun(context.Background())
	if err != nil {
		t.Fatalf("DryRun returned error: %v", err)
	}
	if len(dryRun.Diff.Changes) != 3 {
		t.Fatalf("diff changes = %#v, want cidrs gateway labels", dryRun.Diff.Changes)
	}
}

func TestVirtualNetworkReconcileNoopDoesNotApply(t *testing.T) {
	db := testNBDBClient(t)
	db.executor = &nbRecordingExecutor{results: []libovsdb.OperationResult{{Rows: []libovsdb.Row{{
		colUUID:        uuidValue("ls-uuid"),
		colName:        "net-a",
		colPorts:       ovsSet(),
		colOtherConfig: ovsMap(map[string]string{"subnet": "10.0.0.0/24"}),
		colExternalIDs: ovsMap(map[string]string{
			ExternalIDOwnerKindKey: "project",
			ExternalIDOwnerNameKey: "alpha",
		}),
	}}}}}
	result, err := (&NBClient{db: db}).VirtualNetwork("net-a").
		Ensure().
		WithCIDR("10.0.0.0/24").
		WithOwner("project", "alpha").
		Reconcile(context.Background())
	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}
	if result.Applied || !result.Diff.Empty() {
		t.Fatalf("Reconcile result = %#v, want no-op", result)
	}
}

func TestVirtualNetworkPatchReadsMutatesAndApplies(t *testing.T) {
	db := testNBDBClient(t)
	rec := &nbRecordingExecutor{results: []libovsdb.OperationResult{{Rows: []libovsdb.Row{{
		colUUID:        uuidValue("ls-uuid"),
		colName:        "net-a",
		colPorts:       ovsSet(),
		colOtherConfig: ovsMap(map[string]string{"subnet": "10.0.0.0/24"}),
		colExternalIDs: ovsMap(map[string]string{
			ExternalIDOwnerKindKey:    "project",
			ExternalIDOwnerNameKey:    "alpha",
			ExternalIDLabelKey("env"): "dev",
		}),
	}}}, {Rows: []libovsdb.Row{{
		colUUID:        uuidValue("ls-uuid"),
		colName:        "net-a",
		colPorts:       ovsSet(),
		colOtherConfig: ovsMap(map[string]string{"subnet": "10.0.0.0/24"}),
		colExternalIDs: ovsMap(map[string]string{
			ExternalIDOwnerKindKey:    "project",
			ExternalIDOwnerNameKey:    "alpha",
			ExternalIDLabelKey("env"): "dev",
		}),
	}}}, {Rows: []libovsdb.Row{{
		colUUID:        uuidValue("ls-uuid"),
		colName:        "net-a",
		colPorts:       ovsSet(),
		colOtherConfig: ovsMap(map[string]string{"subnet": "10.0.0.0/24"}),
		colExternalIDs: ovsMap(map[string]string{
			ExternalIDOwnerKindKey:    "project",
			ExternalIDOwnerNameKey:    "alpha",
			ExternalIDLabelKey("env"): "dev",
		}),
	}}}, {Count: 1}}}
	db.executor = rec
	gateway := "10.0.1.1"
	got, err := (&NBClient{db: db}).VirtualNetwork("net-a").Patch(context.Background(), VirtualNetworkPatch{
		AddCIDRs:     []string{"10.0.1.0/24"},
		RemoveCIDRs:  []string{"10.0.0.0/24"},
		Gateway:      &gateway,
		Labels:       Labels{"env": "prod"},
		RemoveLabels: []string{"missing"},
	})
	if err != nil {
		t.Fatalf("Patch returned error: %v", err)
	}
	if got.CIDRs[0] != "10.0.1.0/24" || got.Gateway != gateway || got.Labels["env"] != "prod" {
		t.Fatalf("patched network = %#v", got)
	}
	if op := findRecordedOp(rec.ops, libovsdb.OperationMutate, tableLogicalSwitch); op == nil {
		t.Fatalf("missing logical switch mutate after patch: %#v", rec.ops)
	}
}

func TestLogicalSwitchDNSGetRestoresMultipleIPs(t *testing.T) {
	db := testNBDBClient(t)
	db.executor = &nbRecordingExecutor{results: []libovsdb.OperationResult{{Rows: []libovsdb.Row{{
		colUUID: uuidValue("dns-uuid"),
		colRecords: ovsMap(map[string]string{
			"api.service": "10.0.0.2 10.0.0.3",
		}),
		colExternalIDs: ovsMap(map[string]string{
			dnsNameExternalID:      "net-a-dns",
			ExternalIDOwnerKindKey: "project",
			ExternalIDOwnerNameKey: "alpha",
		}),
	}}}}}
	got, err := (&NBClient{db: db}).LogicalSwitchDNS("net-a-dns").Get(context.Background())
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if got.Name != "net-a-dns" || len(got.Records) != 1 || !reflect.DeepEqual(got.Records[0].IPs, []string{"10.0.0.2", "10.0.0.3"}) {
		t.Fatalf("logical switch dns = %#v", got)
	}
}

func TestLogicalSwitchDNSPatchAddsAndRemovesRecords(t *testing.T) {
	db := testNBDBClient(t)
	rec := &nbRecordingExecutor{results: []libovsdb.OperationResult{{Rows: []libovsdb.Row{{
		colUUID: uuidValue("dns-uuid"),
		colRecords: ovsMap(map[string]string{
			"api.service": "10.0.0.2",
			"old.service": "10.0.0.9",
		}),
		colExternalIDs: ovsMap(map[string]string{
			dnsNameExternalID:      "net-a-dns",
			ExternalIDOwnerKindKey: "project",
			ExternalIDOwnerNameKey: "alpha",
		}),
	}}}, {Rows: []libovsdb.Row{{
		colUUID: uuidValue("dns-uuid"),
		colRecords: ovsMap(map[string]string{
			"api.service": "10.0.0.2",
			"old.service": "10.0.0.9",
		}),
		colExternalIDs: ovsMap(map[string]string{
			dnsNameExternalID:      "net-a-dns",
			ExternalIDOwnerKindKey: "project",
			ExternalIDOwnerNameKey: "alpha",
		}),
	}}}, {Rows: []libovsdb.Row{{
		colUUID: uuidValue("dns-uuid"),
		colRecords: ovsMap(map[string]string{
			"api.service": "10.0.0.2",
			"old.service": "10.0.0.9",
		}),
		colExternalIDs: ovsMap(map[string]string{
			dnsNameExternalID:      "net-a-dns",
			ExternalIDOwnerKindKey: "project",
			ExternalIDOwnerNameKey: "alpha",
		}),
	}}}, {Count: 1}, {Rows: []libovsdb.Row{{
		colUUID: uuidValue("dns-uuid"),
	}}}, {Count: 1}}}
	db.executor = rec
	got, err := (&NBClient{db: db}).LogicalSwitchDNS("net-a-dns").Patch(context.Background(), LogicalSwitchDNSPatch{
		AddRecords:    []DNSRecord{{Domain: "api.service", IPs: []string{"10.0.0.3"}}, {Domain: "db.service", IPs: []string{"10.0.0.4"}}},
		RemoveDomains: []string{"old.service"},
	})
	if err != nil {
		t.Fatalf("Patch returned error: %v", err)
	}
	records := got.RecordMap()
	if !reflect.DeepEqual(records["api.service"], []string{"10.0.0.2", "10.0.0.3"}) || len(records["old.service"]) != 0 {
		t.Fatalf("patched DNS records = %#v", records)
	}
	if findRecordedOp(rec.ops, libovsdb.OperationMutate, tableDNS) == nil {
		t.Fatalf("missing DNS mutate after patch: %#v", rec.ops)
	}
	if !hasRecordedMutation(rec.ops, tableDNS, colRecords, libovsdb.MutateOperationDelete) {
		t.Fatalf("DNS patch ops = %#v, want records delete mutation", rec.ops)
	}
}

func TestWorkloadAttachmentGetReadsLogicalSwitchPort(t *testing.T) {
	db := testNBDBClient(t)
	db.executor = &nbRecordingExecutor{results: []libovsdb.OperationResult{{Rows: []libovsdb.Row{{
		colUUID:      uuidValue("lsp-uuid"),
		colName:      "att-a",
		colAddresses: stringSet([]string{"00:16:3e:11:22:33 10.0.0.10"}),
		colOptions:   ovsMap(map[string]string{}),
		colExternalIDs: ovsMap(map[string]string{
			ExternalIDOwnerKindKey:         "project",
			ExternalIDOwnerNameKey:         "alpha",
			ExternalIDPrefix + "workload":  "vm-a",
			ExternalIDPrefix + "interface": "eth0",
			ExternalIDPrefix + "network":   "net-a",
		}),
	}}}}}
	got, err := (&NBClient{db: db}).WorkloadAttachment("att-a").Get(context.Background())
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if got.Name != "att-a" || got.Network != "net-a" || got.Workload != "vm-a" || got.InterfaceName != "eth0" || got.MAC != "00:16:3e:11:22:33" || got.IPs[0] != "10.0.0.10" {
		t.Fatalf("workload attachment = %#v", got)
	}
}

func TestWorkloadAttachmentPatchUpdatesIPsAndMetadata(t *testing.T) {
	db := testNBDBClient(t)
	rec := &nbRecordingExecutor{results: []libovsdb.OperationResult{{Rows: []libovsdb.Row{{
		colUUID:      uuidValue("lsp-uuid"),
		colName:      "att-a",
		colAddresses: stringSet([]string{"00:16:3e:11:22:33 10.0.0.10"}),
		colExternalIDs: ovsMap(map[string]string{
			ExternalIDOwnerKindKey:        "project",
			ExternalIDOwnerNameKey:        "alpha",
			ExternalIDPrefix + "network":  "net-a",
			ExternalIDPrefix + "workload": "vm-a",
		}),
	}}}, {Rows: []libovsdb.Row{{
		colUUID:      uuidValue("lsp-uuid"),
		colName:      "att-a",
		colAddresses: stringSet([]string{"00:16:3e:11:22:33 10.0.0.10"}),
		colExternalIDs: ovsMap(map[string]string{
			ExternalIDOwnerKindKey:        "project",
			ExternalIDOwnerNameKey:        "alpha",
			ExternalIDPrefix + "network":  "net-a",
			ExternalIDPrefix + "workload": "vm-a",
		}),
	}}}, {Rows: []libovsdb.Row{{
		colUUID:  uuidValue("ls-uuid"),
		colName:  "net-a",
		colPorts: ovsSet(),
	}}}, {Rows: []libovsdb.Row{{
		colUUID:      uuidValue("lsp-uuid"),
		colName:      "att-a",
		colAddresses: stringSet([]string{"00:16:3e:11:22:33 10.0.0.10"}),
		colExternalIDs: ovsMap(map[string]string{
			ExternalIDOwnerKindKey:        "project",
			ExternalIDOwnerNameKey:        "alpha",
			ExternalIDPrefix + "network":  "net-a",
			ExternalIDPrefix + "workload": "vm-a",
		}),
	}}}, {Count: 1}}}
	db.executor = rec
	iface := "eth1"
	got, err := (&NBClient{db: db}).WorkloadAttachment("att-a").Patch(context.Background(), WorkloadAttachmentPatch{
		InterfaceName: &iface,
		AddIPs:        []string{"10.0.0.11"},
		RemoveIPs:     []string{"10.0.0.10"},
	})
	if err != nil {
		t.Fatalf("Patch returned error: %v", err)
	}
	if got.InterfaceName != "eth1" || !reflect.DeepEqual(got.IPs, []string{"10.0.0.11"}) {
		t.Fatalf("patched attachment = %#v", got)
	}
	if op := findRecordedOp(rec.ops, libovsdb.OperationMutate, tableLogicalSwitchPort); op == nil {
		t.Fatalf("missing logical switch port mutate after patch: %#v", rec.ops)
	}
}

func TestWorkloadAttachmentSyncLocalOVSWritesPortInterfaceMetadata(t *testing.T) {
	nbDB := testNBDBClient(t)
	nbRec := &nbRecordingExecutor{results: []libovsdb.OperationResult{
		{Rows: nil},
		{Rows: []libovsdb.Row{{colUUID: uuidValue("ls-uuid"), colPorts: ovsSet()}}},
		{Rows: nil},
		{Count: 1},
		{Count: 1},
	}}
	nbDB.executor = nbRec
	ovsDB := testOVSDBClient(t)
	ovsDB.schema.schema.Tables[tableInterface].Columns[colOptions] = columnSchemaFromJSON(t, `{"type":{"key":"string","value":"string","min":0,"max":"unlimited"}}`)
	bridgeRow := libovsdb.Row{colUUID: uuidValue("br-uuid"), colName: "br-int", colPorts: ovsSet()}
	ovsRec := &recordingExecutor{results: []libovsdb.OperationResult{
		{Rows: []libovsdb.Row{bridgeRow}},
		{Rows: nil},
		{Rows: nil},
		{Rows: []libovsdb.Row{bridgeRow}},
		{Rows: nil},
		{Rows: nil},
		{Rows: []libovsdb.Row{bridgeRow}},
		{Rows: nil},
		{Count: 1},
		{Count: 1},
		{Count: 1},
	}}
	ovsDB.executor = ovsRec
	ref := &WorkloadAttachmentRef{client: &NBClient{db: nbDB}, ovs: &OVSClient{db: ovsDB}, name: "att-a"}

	result, err := ref.Ensure().
		OnNetwork("net-a").
		WithWorkload("vm-a").
		WithInterface("eth0").
		WithMAC("00:16:3e:11:22:33").
		WithIP("10.0.0.10").
		WithOwner("project", "alpha").
		SyncLocalOVS("br-int").
		WithOVSPort("vnet0").
		WithOVSInterface("tap0").
		WithOVSInterfaceType("internal").
		WithOVSOption("mtu_request", "1450").
		Reconcile(context.Background())
	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}
	if !result.Applied {
		t.Fatalf("Reconcile did not report applied")
	}
	portOp := findRecordedOp(ovsRec.ops, libovsdb.OperationInsert, tablePort)
	if portOp == nil {
		t.Fatalf("missing OVS Port insert: %#v", ovsRec.ops)
	}
	portIDs := ovsMapStrings(t, portOp.Row[colExternalIDs])
	if portIDs[ExternalIDKindKey] != "WorkloadAttachment" || portIDs[ExternalIDNameKey] != "att-a" {
		t.Fatalf("port external_ids = %#v", portIDs)
	}
	if portIDs[ExternalIDOwnerNameKey] != "alpha" || portIDs[ExternalIDPrefix+"network"] != "net-a" || portIDs[ExternalIDPrefix+"workload"] != "vm-a" {
		t.Fatalf("port external_ids missing workload metadata: %#v", portIDs)
	}
	ifaceOp := findRecordedOp(ovsRec.ops, libovsdb.OperationInsert, tableInterface)
	if ifaceOp == nil {
		t.Fatalf("missing OVS Interface insert: %#v", ovsRec.ops)
	}
	if ifaceOp.Row[colName] != "tap0" {
		t.Fatalf("interface row = %#v, want name tap0", ifaceOp.Row)
	}
	if ifaceOp.Row[colType] != "internal" {
		t.Fatalf("interface row = %#v, want type internal", ifaceOp.Row)
	}
	options := ovsMapStrings(t, ifaceOp.Row[colOptions])
	if options["mtu_request"] != "1450" {
		t.Fatalf("interface options = %#v", options)
	}
	ifaceIDs := ovsMapStrings(t, ifaceOp.Row[colExternalIDs])
	if ifaceIDs["iface-id"] != "att-a" || ifaceIDs[ExternalIDKindKey] != "WorkloadAttachment" || ifaceIDs[ExternalIDNameKey] != "att-a" {
		t.Fatalf("interface external_ids = %#v", ifaceIDs)
	}
}

func TestWorkloadAttachmentSyncLocalOVSRejectsUnownedPort(t *testing.T) {
	nbDB := testNBDBClient(t)
	nbRec := &nbRecordingExecutor{}
	nbDB.executor = nbRec
	ovsDB := testOVSDBClient(t)
	ovsRec := &recordingExecutor{results: []libovsdb.OperationResult{
		{Rows: []libovsdb.Row{{colUUID: uuidValue("br-uuid"), colName: "br-int", colPorts: ovsSet()}}},
		{Rows: []libovsdb.Row{{colUUID: uuidValue("port-uuid"), colName: "vnet0", colInterfaces: ovsSet()}}},
	}}
	ovsDB.executor = ovsRec
	ref := &WorkloadAttachmentRef{client: &NBClient{db: nbDB}, ovs: &OVSClient{db: ovsDB}, name: "att-a"}

	_, err := ref.Ensure().
		OnNetwork("net-a").
		WithOwner("project", "alpha").
		SyncLocalOVS("br-int").
		WithOVSPort("vnet0").
		Reconcile(context.Background())
	if !IsKind(err, ErrorOwnershipViolation) {
		t.Fatalf("Reconcile error = %v, want ownership violation", err)
	}
	if len(nbRec.ops) != 0 {
		t.Fatalf("NB ops = %#v, want none before local OVS ownership passes", nbRec.ops)
	}
}

func TestWorkloadAttachmentSyncLocalOVSRequiresBackendBeforeNBWrite(t *testing.T) {
	nbDB := testNBDBClient(t)
	nbRec := &nbRecordingExecutor{}
	nbDB.executor = nbRec
	ref := &WorkloadAttachmentRef{client: &NBClient{db: nbDB}, name: "att-a"}

	_, err := ref.Ensure().
		OnNetwork("net-a").
		SyncLocalOVS("br-int").
		Reconcile(context.Background())
	if !IsKind(err, ErrorBackendUnavailable) {
		t.Fatalf("Reconcile error = %v, want backend unavailable", err)
	}
	if len(nbRec.ops) != 0 {
		t.Fatalf("NB ops = %#v, want none without local OVS backend", nbRec.ops)
	}
}

func TestWorkloadAttachmentLocalOVSDefaultsUseAttachmentName(t *testing.T) {
	builder := (&WorkloadAttachmentRef{name: "lp-vm-a-eth0"}).
		Ensure().
		OnNetwork("net-a").
		WithInterface("eth0").
		SyncLocalOVS("br-int")

	desired := normalizeWorkloadAttachment(builder.spec)
	if desired.LocalOVS.PortName != "lp-vm-a-eth0" || desired.LocalOVS.InterfaceName != "lp-vm-a-eth0" {
		t.Fatalf("local OVS defaults = %#v, want attachment name for port/interface", desired.LocalOVS)
	}
}

func TestWorkloadAttachmentSyncLocalOVSRejectsUnownedInterface(t *testing.T) {
	nbDB := testNBDBClient(t)
	nbRec := &nbRecordingExecutor{}
	nbDB.executor = nbRec
	ovsDB := testOVSDBClient(t)
	ovsRec := &recordingExecutor{results: []libovsdb.OperationResult{
		{Rows: []libovsdb.Row{{colUUID: uuidValue("br-uuid"), colName: "br-int", colPorts: ovsSet()}}},
		{Rows: nil},
		{Rows: []libovsdb.Row{{
			colUUID:        uuidValue("iface-uuid"),
			colName:        "tap0",
			colExternalIDs: ovsMap(map[string]string{"iface-id": "other-att"}),
		}}},
	}}
	ovsDB.executor = ovsRec
	ref := &WorkloadAttachmentRef{client: &NBClient{db: nbDB}, ovs: &OVSClient{db: ovsDB}, name: "att-a"}

	_, err := ref.Ensure().
		OnNetwork("net-a").
		SyncLocalOVS("br-int").
		WithOVSInterface("tap0").
		Reconcile(context.Background())
	if !IsKind(err, ErrorOwnershipViolation) {
		t.Fatalf("Reconcile error = %v, want ownership violation", err)
	}
	if len(nbRec.ops) != 0 {
		t.Fatalf("NB ops = %#v, want none before local OVS interface ownership passes", nbRec.ops)
	}
}

func TestSecurityPolicyGetReadsPortGroupMetadata(t *testing.T) {
	db := testNBDBClient(t)
	db.executor = &nbRecordingExecutor{results: []libovsdb.OperationResult{{Rows: []libovsdb.Row{{
		colUUID: uuidValue("pg-uuid"),
		colName: "allow-web",
		colACLs: ovsSet(uuidValue("acl-uuid")),
		colExternalIDs: ovsMap(map[string]string{
			ExternalIDOwnerKindKey:       "project",
			ExternalIDOwnerNameKey:       "alpha",
			ExternalIDLabelKey("env"):    "test",
			ExternalIDPrefix + "subject": "pg-web",
		}),
	}}}, {Rows: []libovsdb.Row{{
		colUUID:      uuidValue("acl-uuid"),
		colPriority:  1001,
		colDirection: "to-lport",
		colMatch:     "outport == @pg-web && tcp && ip4.src == 10.0.0.0/24 && tcp.dst == 80",
		colAction:    "allow",
		colExternalIDs: ovsMap(map[string]string{
			ExternalIDKindKey:              "SecurityPolicy",
			ExternalIDNameKey:              "allow-web",
			ExternalIDPrefix + "subject":   "pg-web",
			ExternalIDPrefix + "rule-name": "web",
		}),
	}}}}}
	got, err := (&NBClient{db: db}).SecurityPolicy("allow-web").Get(context.Background())
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if got.Name != "allow-web" || got.Subject != "pg-web" || got.Owner.Name != "alpha" || got.Labels["env"] != "test" || len(got.Rules) != 1 || got.Rules[0].Name != "web" {
		t.Fatalf("security policy = %#v", got)
	}
}

func TestSecurityPolicyPatchAddsAndRemovesRules(t *testing.T) {
	db := testNBDBClient(t)
	rec := &nbRecordingExecutor{results: []libovsdb.OperationResult{{Rows: []libovsdb.Row{{
		colUUID: uuidValue("pg-uuid"),
		colName: "allow-web",
		colACLs: ovsSet(),
		colExternalIDs: ovsMap(map[string]string{
			ExternalIDOwnerKindKey:       "project",
			ExternalIDOwnerNameKey:       "alpha",
			ExternalIDPrefix + "subject": "pg-web",
		}),
	}}}, {Rows: []libovsdb.Row{{
		colUUID: uuidValue("pg-uuid"),
		colName: "allow-web",
		colACLs: ovsSet(),
		colExternalIDs: ovsMap(map[string]string{
			ExternalIDOwnerKindKey:       "project",
			ExternalIDOwnerNameKey:       "alpha",
			ExternalIDPrefix + "subject": "pg-web",
		}),
	}}}, {Rows: []libovsdb.Row{{
		colUUID: uuidValue("pg-uuid"),
		colName: "allow-web",
		colACLs: ovsSet(),
		colExternalIDs: ovsMap(map[string]string{
			ExternalIDOwnerKindKey:       "project",
			ExternalIDOwnerNameKey:       "alpha",
			ExternalIDPrefix + "subject": "pg-web",
		}),
	}}}, {Rows: []libovsdb.Row{{
		colUUID: uuidValue("pg-uuid"),
	}}}, {Count: 1}, {Count: 1}}}
	db.executor = rec
	got, err := (&NBClient{db: db}).SecurityPolicy("allow-web").Patch(context.Background(), SecurityPolicyPatch{
		AddRules: []SecurityRule{{Name: "ssh", Action: "allow", Protocol: "tcp", CIDRs: []string{"10.0.0.0/24"}, Ports: []int{22}}},
	})
	if err != nil {
		t.Fatalf("Patch returned error: %v", err)
	}
	if len(got.Rules) != 1 || got.Rules[0].Name != "ssh" {
		t.Fatalf("patched policy = %#v", got)
	}
	if op := findRecordedOp(rec.ops, libovsdb.OperationInsert, tableACL); op == nil {
		t.Fatalf("missing ACL insert after policy patch: %#v", rec.ops)
	}
}

func TestV2IntentDeleteUsesUnderlyingResources(t *testing.T) {
	rec := &nbRecordingExecutor{results: []libovsdb.OperationResult{{Rows: []libovsdb.Row{{
		colUUID:  uuidValue("lsp-uuid"),
		colName:  "att-a",
		colPorts: ovsSet(),
	}}}, {Count: 1}}}
	db := testNBDBClient(t)
	db.executor = rec
	if err := (&NBClient{db: db}).WorkloadAttachment("att-a").Delete(context.Background()); err != nil {
		t.Fatalf("Delete returned error: %v", err)
	}
	if op := findRecordedOp(rec.ops, libovsdb.OperationDelete, tableLogicalSwitchPort); op == nil {
		t.Fatalf("missing Logical_Switch_Port delete: %#v", rec.ops)
	}
}

func ovsMapStrings(t *testing.T, value any) map[string]string {
	t.Helper()
	switch typed := value.(type) {
	case libovsdb.OvsMap:
		out := map[string]string{}
		for key, value := range typed.GoMap {
			out[anyString(key)] = anyString(value)
		}
		return out
	default:
		t.Fatalf("value %T is not ovs map: %#v", value, value)
		return nil
	}
}

func hasMutation(mutations []libovsdb.Mutation, column string, mutator libovsdb.Mutator) bool {
	for _, mutation := range mutations {
		if mutation.Column == column && mutation.Mutator == mutator {
			return true
		}
	}
	return false
}

func hasRecordedMutation(ops []libovsdb.Operation, table, column string, mutator libovsdb.Mutator) bool {
	for _, op := range ops {
		if op.Op == libovsdb.OperationMutate && op.Table == table && hasMutation(op.Mutations, column, mutator) {
			return true
		}
	}
	return false
}

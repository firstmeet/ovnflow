package ovnflow

import (
	"context"
	"reflect"
	"sync"
	"testing"

	libovsdb "github.com/ovn-kubernetes/libovsdb/ovsdb"
)

func TestNetworkServiceVIPStringConversion(t *testing.T) {
	vip := ServiceVIP{
		Address: "192.0.2.10",
		Port:    443,
		Backends: []ServiceBackend{
			{Address: "10.0.0.3", Port: 8443},
			{Address: "10.0.0.2", Port: 8443},
		},
	}
	if got := vip.String(); got != "192.0.2.10:443" {
		t.Fatalf("VIP string = %q, want 192.0.2.10:443", got)
	}
	if got := vip.BackendsString(); got != "10.0.0.2:8443,10.0.0.3:8443" {
		t.Fatalf("backend string = %q", got)
	}

	ipv6 := ServiceVIP{Address: "2001:db8::10", Port: 443, Backends: []ServiceBackend{{Address: "2001:db8::20", Port: 8443}}}
	if got := ipv6.String(); got != "[2001:db8::10]:443" {
		t.Fatalf("IPv6 VIP string = %q", got)
	}
	if got := ipv6.BackendsString(); got != "[2001:db8::20]:8443" {
		t.Fatalf("IPv6 backend string = %q", got)
	}
}

func TestNetworkServiceValidationFailures(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{name: "name", err: (NetworkService{}).Validate()},
		{name: "protocol", err: (NetworkService{Name: "svc", Protocol: "icmp", VIPs: []ServiceVIP{{Address: "192.0.2.10", Port: 80, Backends: []ServiceBackend{{Address: "10.0.0.2", Port: 80}}}}}).Validate()},
		{name: "vip address", err: (NetworkService{Name: "svc", VIPs: []ServiceVIP{{Address: "bad", Port: 80, Backends: []ServiceBackend{{Address: "10.0.0.2", Port: 80}}}}}).Validate()},
		{name: "vip port", err: (NetworkService{Name: "svc", VIPs: []ServiceVIP{{Address: "192.0.2.10", Port: 0, Backends: []ServiceBackend{{Address: "10.0.0.2", Port: 80}}}}}).Validate()},
		{name: "backend address", err: (NetworkService{Name: "svc", VIPs: []ServiceVIP{{Address: "192.0.2.10", Port: 80, Backends: []ServiceBackend{{Address: "bad", Port: 80}}}}}).Validate()},
		{name: "backend port", err: (NetworkService{Name: "svc", VIPs: []ServiceVIP{{Address: "192.0.2.10", Port: 80, Backends: []ServiceBackend{{Address: "10.0.0.2", Port: 65536}}}}}).Validate()},
		{name: "owner", err: (NetworkService{Name: "svc", Owner: OwnerRef{Kind: "project"}, VIPs: []ServiceVIP{{Address: "192.0.2.10", Port: 80, Backends: []ServiceBackend{{Address: "10.0.0.2", Port: 80}}}}}).Validate()},
		{name: "label", err: (NetworkService{Name: "svc", Labels: Labels{"": "bad"}, VIPs: []ServiceVIP{{Address: "192.0.2.10", Port: 80, Backends: []ServiceBackend{{Address: "10.0.0.2", Port: 80}}}}}).Validate()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !IsKind(tt.err, ErrorValidation) {
				t.Fatalf("error kind = %q for %v, want validation", KindOf(tt.err), tt.err)
			}
		})
	}
}

func TestNetworkServicePlanDryRunAndStubReconcile(t *testing.T) {
	builder := (&NBClient{}).NetworkService("svc-web").
		Ensure().
		WithProtocol("TCP").
		WithOwner("app", "web").
		WithLabel("team/name", "platform").
		WithVIP("192.0.2.10", 80,
			ServiceBackend{Address: "10.0.0.2", Port: 8080},
			ServiceBackend{Address: "10.0.0.3", Port: 8080},
		)

	dryRun, err := builder.DryRun(context.Background())
	if err != nil {
		t.Fatalf("DryRun returned error: %v", err)
	}
	if len(dryRun.Plan.Operations) != 1 || dryRun.Plan.Operations[0].Resource != networkServiceKind {
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
		t.Fatalf("Reconcile applied real changes in stub")
	}
}

func TestNetworkServiceReconcileUsesLoadBalancerBuilderExternalIDs(t *testing.T) {
	db := testServiceNBDBClient(t)
	rec := &serviceRecordingExecutor{results: []libovsdb.OperationResult{
		{Rows: nil},
		{Rows: nil},
		{},
		{Count: 1},
	}}
	db.executor = rec

	err := (&NBClient{db: db}).NetworkService("svc-web").Ensure().
		WithProtocol("tcp").
		AttachToRouter("lr0").
		WithOwner("app", "web").
		WithLabel("team/name", "platform").
		WithVIP("192.0.2.10", 80,
			ServiceBackend{Address: "10.0.0.2", Port: 8080},
			ServiceBackend{Address: "10.0.0.3", Port: 8080},
		).
		Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if len(rec.ops) != 6 {
		t.Fatalf("ops = %d, want ownership/diff/stale/ensure selects plus insert/router attach: %#v", len(rec.ops), rec.ops)
	}
	insert := rec.ops[4]
	if insert.Op != libovsdb.OperationInsert || insert.Table != tableLoadBalancer {
		t.Fatalf("insert op = %#v, want Load_Balancer insert", insert)
	}
	if got := rowStringMapValue(insert.Row, colVIPs); !reflect.DeepEqual(got, map[string]string{"192.0.2.10:80": "10.0.0.2:8080,10.0.0.3:8080"}) {
		t.Fatalf("vips = %#v", got)
	}
	if got := rowStringValue(insert.Row, colProtocol); got != "tcp" {
		t.Fatalf("protocol = %q, want tcp", got)
	}
	externalIDs := rowStringMapValue(insert.Row, colExternalIDs)
	if externalIDs[ExternalIDKindKey] != networkServiceKind || externalIDs[ExternalIDNameKey] != "svc-web" {
		t.Fatalf("external IDs missing intent markers: %#v", externalIDs)
	}
	if externalIDs[ExternalIDOwnerKindKey] != "app" || externalIDs[ExternalIDOwnerNameKey] != "web" {
		t.Fatalf("external IDs missing owner: %#v", externalIDs)
	}
	if got := externalIDs[ExternalIDLabelKey("team/name")]; got != "platform" {
		t.Fatalf("encoded label = %q, want platform", got)
	}
	if rec.ops[5].Op != libovsdb.OperationMutate || rec.ops[5].Table != tableLogicalRouter {
		t.Fatalf("router attach op = %#v", rec.ops[5])
	}
}

func TestNetworkServiceDryRunDiffsExistingLoadBalancer(t *testing.T) {
	db := testServiceNBDBClient(t)
	ownerIDs, err := intentExternalIDs(networkServiceKind, "svc-web", OwnerRef{Kind: "app", Name: "web"}, Labels{"team": "platform"})
	if err != nil {
		t.Fatalf("intentExternalIDs returned error: %v", err)
	}
	row := libovsdb.Row{
		colName:        "svc-web",
		colVIPs:        ovsMap(map[string]string{"192.0.2.10:80": "10.0.0.2:8080"}),
		colProtocol:    "tcp",
		colExternalIDs: ovsMap(ownerIDs),
	}
	db.executor = &serviceRecordingExecutor{results: []libovsdb.OperationResult{{Rows: []libovsdb.Row{row}}}}

	dryRun, err := (&NBClient{db: db}).NetworkService("svc-web").Ensure().
		WithProtocol("tcp").
		WithOwner("app", "web").
		WithLabel("team", "platform").
		WithVIP("192.0.2.10", 80,
			ServiceBackend{Address: "10.0.0.2", Port: 8080},
			ServiceBackend{Address: "10.0.0.3", Port: 8080},
		).
		DryRun(context.Background())
	if err != nil {
		t.Fatalf("DryRun returned error: %v", err)
	}
	if len(dryRun.Diff.Changes) != 1 || dryRun.Diff.Changes[0].Path != "vips" {
		t.Fatalf("dry run diff = %#v, want vips change", dryRun.Diff)
	}
}

func TestNetworkServiceReconcileRejectsForeignLoadBalancer(t *testing.T) {
	db := testServiceNBDBClient(t)
	db.executor = &serviceRecordingExecutor{results: []libovsdb.OperationResult{{Rows: []libovsdb.Row{{
		colUUID:        "lb-uuid",
		colName:        "svc-web",
		colExternalIDs: ovsMap(map[string]string{"owner": "someone-else"}),
	}}}}}

	err := (&NBClient{db: db}).NetworkService("svc-web").Ensure().
		WithOwner("app", "web").
		WithVIP("192.0.2.10", 80, ServiceBackend{Address: "10.0.0.2", Port: 8080}).
		Execute(context.Background())
	if !IsKind(err, ErrorOwnershipViolation) {
		t.Fatalf("Execute foreign LB error = %v, want ownership violation", err)
	}
}

func TestNetworkServiceReconcileRemovesStaleVIPs(t *testing.T) {
	db := testServiceNBDBClient(t)
	ownerIDs, err := intentExternalIDs(networkServiceKind, "svc-web", OwnerRef{Kind: "app", Name: "web"}, nil)
	if err != nil {
		t.Fatalf("intentExternalIDs returned error: %v", err)
	}
	existingRow := libovsdb.Row{
		colUUID:        "lb-uuid",
		colName:        "svc-web",
		colVIPs:        ovsMap(map[string]string{"192.0.2.10:80": "10.0.0.2:8080", "192.0.2.20:80": "10.0.0.9:8080"}),
		colProtocol:    "tcp",
		colExternalIDs: ovsMap(ownerIDs),
	}
	rec := &serviceRecordingExecutor{results: []libovsdb.OperationResult{
		{Rows: []libovsdb.Row{existingRow}},
		{Rows: []libovsdb.Row{existingRow}},
		{Rows: []libovsdb.Row{existingRow}},
		{Rows: []libovsdb.Row{{colUUID: "lb-uuid"}}},
		{Count: 1},
		{Rows: []libovsdb.Row{{colUUID: "lb-uuid"}}},
		{Count: 1},
	}}
	db.executor = rec

	err = (&NBClient{db: db}).NetworkService("svc-web").Ensure().
		WithProtocol("tcp").
		WithOwner("app", "web").
		WithVIP("192.0.2.10", 80, ServiceBackend{Address: "10.0.0.2", Port: 8080}).
		Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	var removedStale bool
	for _, op := range rec.ops {
		if op.Op != libovsdb.OperationMutate || op.Table != tableLoadBalancer {
			continue
		}
		for _, mutation := range op.Mutations {
			if mutation.Column == colVIPs && mutation.Mutator == libovsdb.MutateOperationDelete {
				removedStale = true
			}
		}
	}
	if !removedStale {
		t.Fatalf("ops did not delete stale VIP: %#v", rec.ops)
	}
}

func TestNetworkServiceDeleteRequiresV2Ownership(t *testing.T) {
	db := testServiceNBDBClient(t)
	db.executor = &serviceRecordingExecutor{results: []libovsdb.OperationResult{{Rows: []libovsdb.Row{{
		colUUID:        "lb-uuid",
		colName:        "svc-web",
		colExternalIDs: ovsMap(map[string]string{ExternalIDKindKey: networkServiceKind, ExternalIDNameKey: "svc-web"}),
	}}}}}

	err := (&NBClient{db: db}).NetworkService("svc-web").Delete().Execute(context.Background())
	if !IsKind(err, ErrorOwnershipViolation) {
		t.Fatalf("Delete weak marker error = %v, want ownership violation", err)
	}
}

type serviceRecordingExecutor struct {
	mu      sync.Mutex
	ops     []libovsdb.Operation
	results []libovsdb.OperationResult
}

func (r *serviceRecordingExecutor) Transact(_ context.Context, ops ...libovsdb.Operation) ([]libovsdb.OperationResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ops = append(r.ops, ops...)
	if r.results != nil {
		if len(r.results) < len(ops) {
			out := append([]libovsdb.OperationResult{}, r.results...)
			r.results = nil
			for len(out) < len(ops) {
				out = append(out, libovsdb.OperationResult{Count: 1})
			}
			return out, nil
		}
		out := append([]libovsdb.OperationResult{}, r.results[:len(ops)]...)
		r.results = r.results[len(ops):]
		return out, nil
	}
	return []libovsdb.OperationResult{{Count: 1}}, nil
}

func (r *serviceRecordingExecutor) List(context.Context, any) error {
	return nil
}

func testServiceNBDBClient(t *testing.T) *dbClient {
	t.Helper()
	return &dbClient{
		database: dbOVNNorthbound,
		schema:   newSchemaRegistry(dbOVNNorthbound, serviceDatabaseSchemaWithColumns(dbOVNNorthbound, requiredSchema(dbOVNNorthbound))),
	}
}

func serviceDatabaseSchemaWithColumns(name string, required map[string][]string) libovsdb.DatabaseSchema {
	schema := libovsdb.DatabaseSchema{
		Name:   name,
		Tables: map[string]libovsdb.TableSchema{},
	}
	for table, columns := range required {
		tableSchema := libovsdb.TableSchema{Columns: map[string]*libovsdb.ColumnSchema{}}
		for _, column := range columns {
			tableSchema.Columns[column] = &libovsdb.ColumnSchema{}
		}
		schema.Tables[table] = tableSchema
	}
	return schema
}

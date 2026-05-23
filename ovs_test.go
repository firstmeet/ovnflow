package ovnflow

import (
	"context"
	"encoding/json"
	"testing"

	libovsdb "github.com/ovn-kubernetes/libovsdb/ovsdb"
)

func TestOVSBridgeControllerUsesSingularControllerColumn(t *testing.T) {
	builder := (&OVSClient{db: testOVSDBClient(t)}).
		Bridge("br-test").
		Ensure().
		WithControllerTarget("tcp:127.0.0.1:6653")

	controllerUUIDs, controllerOps := builder.controllerOps()
	op := builder.insertBridgeOp(controllerUUIDs)

	if len(controllerOps) != 1 {
		t.Fatalf("controller ops = %d, want 1", len(controllerOps))
	}
	if _, ok := op.Row[colController]; !ok {
		t.Fatalf("bridge insert row missing %q: %#v", colController, op.Row)
	}
	if _, ok := op.Row[colControllers]; ok {
		t.Fatalf("bridge insert row used invalid %q column: %#v", colControllers, op.Row)
	}
}

func TestOVSBridgeReferencedRowsIncludesUUIDRefs(t *testing.T) {
	netflow := "netflow-uuid"
	sflow := "sflow-uuid"
	ipfix := "ipfix-uuid"
	autoAttach := "auto-uuid"

	refs := ovsBridgeReferencedRows(OVSBridge{
		Controllers: []string{"controller-uuid"},
		Mirrors:     []string{"mirror-uuid"},
		NetFlow:     &netflow,
		SFlow:       &sflow,
		IPFIX:       &ipfix,
		AutoAttach:  &autoAttach,
		FlowTables:  map[int]string{0: "flow-table-uuid"},
	})

	tests := map[string]string{
		tableController: "controller-uuid",
		tableMirror:     "mirror-uuid",
		tableNetFlow:    "netflow-uuid",
		tableSFlow:      "sflow-uuid",
		tableIPFIX:      "ipfix-uuid",
		tableAutoAttach: "auto-uuid",
		tableFlowTable:  "flow-table-uuid",
	}
	for table, uuid := range tests {
		if !containsString(refs[table], uuid) {
			t.Fatalf("refs[%s] = %v, want %s", table, refs[table], uuid)
		}
	}
}

func TestOVSBridgeEnsureNewBridgeWithPortAndExternalIDsDoesNotPanic(t *testing.T) {
	db := testOVSDBClient(t)
	rec := &recordingExecutor{
		results: []libovsdb.OperationResult{
			{Rows: nil},
			{Rows: nil},
			{Rows: []libovsdb.Row{{colUUID: uuidValue("root-uuid")}}},
			{Count: 1},
			{Count: 1},
			{Count: 1},
			{Count: 1},
		},
	}
	db.executor = rec

	err := (&OVSClient{db: db}).Bridge("br-test").Ensure().
		WithExternalID("owner", "test").
		AddPort("p0").
		WithInterfaceType("internal").
		Execute(context.Background())
	if err != nil {
		t.Fatalf("Ensure() = %v", err)
	}
	if len(rec.ops) != 7 {
		t.Fatalf("ops = %d, want select bridge/select port/select root/insert iface/insert port/insert bridge/mutate root as recorded batches: %#v", len(rec.ops), rec.ops)
	}
	foundBridgeInsert := false
	for _, op := range rec.ops {
		if op.Op == libovsdb.OperationInsert && op.Table == tableBridge {
			foundBridgeInsert = true
			if got := rowStringMapValue(op.Row, colExternalIDs)["owner"]; got != "test" {
				t.Fatalf("bridge insert external_ids.owner = %q, want test: %#v", got, op.Row)
			}
		}
		if op.Op == libovsdb.OperationMutate && op.Table == tableBridge {
			t.Fatalf("unexpected Bridge mutate for newly inserted bridge: %#v", op)
		}
	}
	if !foundBridgeInsert {
		t.Fatal("missing Bridge insert operation")
	}
}

func TestOVSDeleteUnreferencesRowsByUUID(t *testing.T) {
	op := ovsUnreferenceUUIDOp(tableBridge, colController, "br-uuid", "controller-uuid")
	if op.Op != libovsdb.OperationMutate || op.Table != tableBridge {
		t.Fatalf("op = %#v, want mutate Bridge", op)
	}
	if len(op.Where) != 1 || op.Where[0].Column != colUUID {
		t.Fatalf("where = %#v, want UUID condition", op.Where)
	}
	if len(op.Mutations) != 1 || op.Mutations[0].Column != colController {
		t.Fatalf("mutations = %#v, want controller UUID delete", op.Mutations)
	}
}

func TestOVSTableDeleteSelectsThenDeletesByUUID(t *testing.T) {
	db := testOVSDBClient(t)
	rec := &recordingExecutor{
		results: []libovsdb.OperationResult{
			{Rows: []libovsdb.Row{{colUUID: uuidValue("controller-uuid")}}},
			{Count: 1},
		},
	}
	db.executor = rec

	err := (&OVSClient{db: db}).Controller("tcp:127.0.0.1:6653").Delete().Execute(context.Background())
	if err != nil {
		t.Fatalf("Delete() = %v", err)
	}
	if len(rec.ops) != 2 {
		t.Fatalf("recorded ops = %d, want select/delete: %#v", len(rec.ops), rec.ops)
	}
	if rec.ops[1].Op != libovsdb.OperationDelete || rec.ops[1].Table != tableController {
		t.Fatalf("delete op = %#v", rec.ops[1])
	}
	if len(rec.ops[1].Where) != 1 || rec.ops[1].Where[0].Column != colUUID {
		t.Fatalf("delete where = %#v, want UUID where", rec.ops[1].Where)
	}
}

func TestOVSTableDeleteDoesNotRequireUnreferenceMutationsToAffectRows(t *testing.T) {
	db := testOVSDBClient(t)
	db.schema.schema.Tables[tableBridge].Columns[colController] = columnSchemaFromJSON(t, `{"type":{"key":{"type":"uuid","refTable":"Controller"},"min":0,"max":"unlimited"}}`)
	rec := &recordingExecutor{
		results: []libovsdb.OperationResult{
			{Rows: []libovsdb.Row{{colUUID: uuidValue("controller-uuid")}}},
			{Rows: []libovsdb.Row{{colUUID: uuidValue("br-uuid")}}},
			{Count: 0},
			{Count: 1},
		},
	}
	db.executor = rec

	err := (&OVSClient{db: db}).Controller("tcp:127.0.0.1:6653").Delete().Execute(context.Background())
	if err != nil {
		t.Fatalf("Delete() = %v, want nil when only unreference mutation is no-op", err)
	}
	if len(rec.ops) != 4 {
		t.Fatalf("ops = %d, want select target/select refs/mutate/delete: %#v", len(rec.ops), rec.ops)
	}
	if rec.ops[2].Op != libovsdb.OperationMutate || rec.ops[3].Op != libovsdb.OperationDelete {
		t.Fatalf("ops = %#v, want mutate cleanup followed by target delete", rec.ops)
	}
}

func columnSchemaFromJSON(t *testing.T, raw string) *libovsdb.ColumnSchema {
	t.Helper()
	var schema libovsdb.ColumnSchema
	if err := json.Unmarshal([]byte(raw), &schema); err != nil {
		t.Fatalf("decode column schema: %v", err)
	}
	return &schema
}

func TestOVSNamedExternalIDEnsureWritesIdentity(t *testing.T) {
	db := testOVSDBClient(t)
	rec := &recordingExecutor{
		results: []libovsdb.OperationResult{
			{Rows: nil},
			{UUID: uuidValue("qos-uuid")},
		},
	}
	db.executor = rec

	err := (&OVSClient{db: db}).QoS("qos0").Ensure().WithQoSType("linux-htb").Execute(context.Background())
	if err != nil {
		t.Fatalf("Ensure() = %v", err)
	}
	if len(rec.ops) != 2 {
		t.Fatalf("ops = %d, want select/insert: %#v", len(rec.ops), rec.ops)
	}
	externalIDs := rowStringMapValue(rec.ops[1].Row, colExternalIDs)
	if externalIDs["name"] != "qos0" {
		t.Fatalf("insert external_ids = %#v, want name=qos0", rec.ops[1].Row[colExternalIDs])
	}
}

func testOVSDBClient(t *testing.T) *dbClient {
	t.Helper()
	required := requiredSchema(dbOpenVSwitch)
	required[tableController] = []string{colTarget, colExternalIDs, colOtherConfig}
	return &dbClient{
		database: dbOpenVSwitch,
		executor: &recordingExecutor{},
		schema:   newSchemaRegistry(dbOpenVSwitch, databaseSchemaWithColumns(dbOpenVSwitch, required)),
	}
}

type recordingExecutor struct {
	ops     []libovsdb.Operation
	results []libovsdb.OperationResult
}

func (r *recordingExecutor) Transact(_ context.Context, ops ...libovsdb.Operation) ([]libovsdb.OperationResult, error) {
	r.ops = append(r.ops, ops...)
	if len(r.results) > 0 {
		n := len(ops)
		if n > len(r.results) {
			n = len(r.results)
		}
		out := append([]libovsdb.OperationResult{}, r.results[:n]...)
		r.results = r.results[n:]
		for len(out) < len(ops) {
			out = append(out, libovsdb.OperationResult{Count: 1})
		}
		return out, nil
	}
	return []libovsdb.OperationResult{{Count: 1}}, nil
}

func (r *recordingExecutor) List(context.Context, any) error {
	return nil
}

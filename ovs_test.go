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
	op := ovsUnreferenceUUIDSetOp(tableBridge, colController, "br-uuid", "controller-uuid")
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

func TestOVSTableDeleteReportsScalarStrongReferenceConflict(t *testing.T) {
	db := testOVSDBClient(t)
	db.schema.schema.Tables[tableBridge].Columns[colNetFlow] = columnSchemaFromJSON(t, `{"type":{"key":{"type":"uuid","refTable":"NetFlow"}}}`)
	db.schema.schema.Tables[tableNetFlow].Columns[colExternalIDs] = columnSchemaFromJSON(t, `{"type":{"key":"string","value":"string","min":0,"max":"unlimited"}}`)
	rec := &recordingExecutor{
		results: []libovsdb.OperationResult{
			{Rows: []libovsdb.Row{{colUUID: uuidValue("netflow-uuid")}}},
			{Rows: []libovsdb.Row{{colUUID: uuidValue("br-uuid")}}},
		},
	}
	db.executor = rec

	err := (&OVSClient{db: db}).NetFlow("nf0").Delete().Execute(context.Background())
	if !IsKind(err, ErrorConflict) {
		t.Fatalf("Delete() = %v, want ErrorConflict for scalar strong reference", err)
	}
	for _, op := range rec.ops {
		if op.Op == libovsdb.OperationMutate && op.Table == tableBridge && len(op.Mutations) > 0 && op.Mutations[0].Column == colNetFlow {
			t.Fatalf("unexpected scalar UUID mutate: %#v; ops=%#v", op, rec.ops)
		}
	}
}

func TestOVSMapUUIDReferenceCleanupSupportsJSONUUIDValues(t *testing.T) {
	keys := ovsMapDeleteKeysForUUID([]any{
		"map",
		[]any{
			[]any{"selected", []any{"uuid", "port-uuid"}},
			[]any{"other", []any{"uuid", "other-uuid"}},
		},
	}, "port-uuid", false, true)
	if len(keys) != 1 || keys[0] != "selected" {
		t.Fatalf("delete keys = %#v, want selected", keys)
	}
}

func TestOVSMapUUIDReferenceCleanupSupportsKeyReferences(t *testing.T) {
	keys := ovsMapDeleteKeysForUUID([]any{
		"map",
		[]any{
			[]any{[]any{"uuid", "queue-uuid"}, []any{"uuid", "port-uuid"}},
			[]any{[]any{"uuid", "other-uuid"}, []any{"uuid", "port-uuid"}},
		},
	}, "queue-uuid", true, false)
	if len(keys) != 1 {
		t.Fatalf("delete keys = %#v, want one key", keys)
	}
	if key, ok := keys[0].(libovsdb.UUID); !ok || key.GoUUID != "queue-uuid" {
		t.Fatalf("delete key = %#v, want queue UUID", keys[0])
	}
}

func TestOVSBridgeDeleteDoesNotCascadeSharedConfigRows(t *testing.T) {
	db := testOVSDBClient(t)
	rec := &recordingExecutor{
		results: []libovsdb.OperationResult{
			{Rows: []libovsdb.Row{{
				colUUID:       uuidValue("br-uuid"),
				colName:       "br-test",
				colPorts:      uuidSet("port-uuid"),
				colController: uuidSet("controller-uuid"),
				colMirrors:    uuidSet("mirror-uuid"),
			}}},
			{Rows: []libovsdb.Row{{colUUID: uuidValue("root-uuid")}}},
			{Rows: []libovsdb.Row{{
				colUUID:       uuidValue("port-uuid"),
				colName:       "p0",
				colInterfaces: uuidSet("iface-uuid"),
				colQoS:        uuidValue("qos-uuid"),
			}}},
			{Count: 1},
			{Count: 1},
			{Count: 1},
			{Count: 1},
		},
	}
	db.executor = rec

	err := (&OVSClient{db: db}).Bridge("br-test").Delete().Execute(context.Background())
	if err != nil {
		t.Fatalf("Delete() = %v", err)
	}
	for _, op := range rec.ops {
		if op.Op == libovsdb.OperationDelete {
			switch op.Table {
			case tableController, tableMirror, tableQoS, tableNetFlow, tableSFlow, tableIPFIX, tableFlowTable, tableAutoAttach:
				t.Fatalf("unexpected cascade delete of shared table %s: %#v", op.Table, rec.ops)
			}
		}
	}
}

func TestOVSBridgeDeleteKeepsPortReferencedByAnotherBridge(t *testing.T) {
	db := testOVSDBClient(t)
	rec := &recordingExecutor{
		results: []libovsdb.OperationResult{
			{Rows: []libovsdb.Row{{
				colUUID:  uuidValue("br-uuid"),
				colName:  "br-test",
				colPorts: uuidSet("shared-port-uuid"),
			}}},
			{Rows: []libovsdb.Row{{colUUID: uuidValue("root-uuid")}}},
			{Rows: []libovsdb.Row{{
				colUUID:       uuidValue("shared-port-uuid"),
				colName:       "p0",
				colInterfaces: uuidSet("iface-uuid"),
			}}},
			{Rows: []libovsdb.Row{
				{colUUID: uuidValue("br-uuid"), colName: "br-test", colPorts: uuidSet("shared-port-uuid")},
				{colUUID: uuidValue("br-other-uuid"), colName: "br-other", colPorts: uuidSet("shared-port-uuid")},
			}},
			{Rows: []libovsdb.Row{{
				colUUID:       uuidValue("shared-port-uuid"),
				colName:       "p0",
				colInterfaces: uuidSet("iface-uuid"),
			}}},
			{Count: 1},
			{Count: 1},
		},
	}
	db.executor = rec

	err := (&OVSClient{db: db}).Bridge("br-test").Delete().Execute(context.Background())
	if err != nil {
		t.Fatalf("Delete() = %v", err)
	}
	for _, op := range rec.ops {
		if op.Op == libovsdb.OperationDelete && (op.Table == tablePort || op.Table == tableInterface) {
			t.Fatalf("unexpected delete of shared port/interface: %#v; ops=%#v", op, rec.ops)
		}
	}
}

func TestOVSDeletePortKeepsInterfaceReferencedByAnotherPort(t *testing.T) {
	db := testOVSDBClient(t)
	rec := &recordingExecutor{
		results: []libovsdb.OperationResult{
			{Rows: []libovsdb.Row{{
				colUUID:  uuidValue("br-uuid"),
				colName:  "br-test",
				colPorts: uuidSet("port-uuid"),
			}}},
			{Rows: []libovsdb.Row{{
				colUUID:       uuidValue("port-uuid"),
				colName:       "p0",
				colInterfaces: uuidSet("shared-iface-uuid"),
			}}},
			{Rows: []libovsdb.Row{
				{colUUID: uuidValue("port-uuid"), colName: "p0", colInterfaces: uuidSet("shared-iface-uuid")},
				{colUUID: uuidValue("other-port-uuid"), colName: "p1", colInterfaces: uuidSet("shared-iface-uuid")},
			}},
			{Count: 1},
			{Count: 1},
		},
	}
	db.executor = rec

	err := (&OVSClient{db: db}).Bridge("br-test").DeletePort("p0").Execute(context.Background())
	if err != nil {
		t.Fatalf("DeletePort() = %v", err)
	}
	for _, op := range rec.ops {
		if op.Op == libovsdb.OperationDelete && op.Table == tableInterface {
			t.Fatalf("unexpected delete of shared interface: %#v; ops=%#v", op, rec.ops)
		}
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
	db.schema.schema.Tables[tableQoS].Columns[colExternalIDs] = &libovsdb.ColumnSchema{}
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

func TestOVSManagerEnsureReferencesRootManagerOptions(t *testing.T) {
	db := testOVSDBClient(t)
	db.schema.schema.Tables[tableOpenVSwitch].Columns[colManagerOptions] = columnSchemaFromJSON(t, `{"type":{"key":{"type":"uuid","refTable":"Manager"},"min":0,"max":"unlimited"}}`)
	rec := &recordingExecutor{
		results: []libovsdb.OperationResult{
			{Rows: nil},
			{Rows: []libovsdb.Row{{colUUID: uuidValue("root-uuid")}}},
			{UUID: uuidValue("manager-uuid")},
			{Count: 1},
		},
	}
	db.executor = rec

	err := (&OVSClient{db: db}).Manager("ptcp:6640:127.0.0.1").
		Ensure().
		WithExternalID("owner", "test").
		Execute(context.Background())
	if err != nil {
		t.Fatalf("Ensure() = %v", err)
	}
	if len(rec.ops) != 4 {
		t.Fatalf("ops = %d, want select manager/select root/insert/mutate root: %#v", len(rec.ops), rec.ops)
	}
	if rec.ops[2].Op != libovsdb.OperationInsert || rec.ops[2].Table != tableManager || rec.ops[2].UUIDName == "" {
		t.Fatalf("manager insert op = %#v", rec.ops[2])
	}
	if rec.ops[3].Op != libovsdb.OperationMutate || rec.ops[3].Table != tableOpenVSwitch {
		t.Fatalf("root reference op = %#v, want Open_vSwitch mutate", rec.ops[3])
	}
	if len(rec.ops[3].Where) != 1 || rec.ops[3].Where[0].Column != colUUID {
		t.Fatalf("root where = %#v, want root UUID", rec.ops[3].Where)
	}
	if len(rec.ops[3].Mutations) != 1 || rec.ops[3].Mutations[0].Column != colManagerOptions {
		t.Fatalf("root mutations = %#v, want manager_options", rec.ops[3].Mutations)
	}
}

func TestOVSManagerEnsureRepairsMissingRootReference(t *testing.T) {
	db := testOVSDBClient(t)
	db.schema.schema.Tables[tableOpenVSwitch].Columns[colManagerOptions] = columnSchemaFromJSON(t, `{"type":{"key":{"type":"uuid","refTable":"Manager"},"min":0,"max":"unlimited"}}`)
	rec := &recordingExecutor{
		results: []libovsdb.OperationResult{
			{Rows: []libovsdb.Row{{colUUID: uuidValue("manager-uuid")}}},
			{Rows: []libovsdb.Row{{colUUID: uuidValue("root-uuid")}}},
			{Count: 1},
			{Count: 1},
		},
	}
	db.executor = rec

	err := (&OVSClient{db: db}).Manager("ptcp:6640:127.0.0.1").
		Ensure().
		WithExternalID("owner", "test").
		Execute(context.Background())
	if err != nil {
		t.Fatalf("Ensure() = %v", err)
	}
	if len(rec.ops) != 4 {
		t.Fatalf("ops = %d, want select manager/select root/mutate root/mutate manager: %#v", len(rec.ops), rec.ops)
	}
	if rec.ops[2].Op != libovsdb.OperationMutate || rec.ops[2].Table != tableOpenVSwitch {
		t.Fatalf("root repair op = %#v", rec.ops[2])
	}
	if len(rec.ops[2].Mutations) != 1 || rec.ops[2].Mutations[0].Column != colManagerOptions {
		t.Fatalf("root repair mutations = %#v, want manager_options", rec.ops[2].Mutations)
	}
	if rec.ops[3].Op != libovsdb.OperationMutate || rec.ops[3].Table != tableManager {
		t.Fatalf("manager update op = %#v", rec.ops[3])
	}
}

func TestOVSExtendedTableHelpersSelectExpectedIdentities(t *testing.T) {
	tests := []struct {
		name       string
		ref        func(*OVSClient) *TableRef
		wantTable  string
		wantColumn string
		wantValue  any
	}{
		{name: "controller", ref: func(o *OVSClient) *TableRef { return o.Controller("tcp:127.0.0.1:6653") }, wantTable: tableController, wantColumn: colTarget, wantValue: "tcp:127.0.0.1:6653"},
		{name: "manager", ref: func(o *OVSClient) *TableRef { return o.Manager("ptcp:6640:127.0.0.1") }, wantTable: tableManager, wantColumn: colTarget, wantValue: "ptcp:6640:127.0.0.1"},
		{name: "mirror", ref: func(o *OVSClient) *TableRef { return o.Mirror("m0") }, wantTable: tableMirror, wantColumn: colName, wantValue: "m0"},
		{name: "flow table", ref: func(o *OVSClient) *TableRef { return o.FlowTable("ft0") }, wantTable: tableFlowTable, wantColumn: colName, wantValue: "ft0"},
		{name: "auto attach", ref: func(o *OVSClient) *TableRef { return o.AutoAttach("system0") }, wantTable: tableAutoAttach, wantColumn: colSystemName, wantValue: "system0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := testOVSDBClient(t)
			db.schema.schema.Tables[tt.wantTable].Columns[colExternalIDs] = &libovsdb.ColumnSchema{}
			rec := &recordingExecutor{}
			db.executor = rec

			_, err := tt.ref(&OVSClient{db: db}).Get(context.Background())
			if err != nil && !IsKind(err, ErrorNotFound) {
				t.Fatalf("Get() = %v", err)
			}
			if len(rec.ops) != 1 {
				t.Fatalf("ops = %d, want one select: %#v", len(rec.ops), rec.ops)
			}
			op := rec.ops[0]
			if op.Op != libovsdb.OperationSelect || op.Table != tt.wantTable {
				t.Fatalf("op = %#v, want select %s", op, tt.wantTable)
			}
			if len(op.Where) != 1 || op.Where[0].Column != tt.wantColumn || op.Where[0].Value != tt.wantValue {
				t.Fatalf("where = %#v, want %s == %v", op.Where, tt.wantColumn, tt.wantValue)
			}
		})
	}
}

func TestOVSNamedExternalIDHelpersUseIncludesCondition(t *testing.T) {
	tests := []struct {
		name      string
		ref       func(*OVSClient) *TableRef
		wantTable string
	}{
		{name: "qos", ref: func(o *OVSClient) *TableRef { return o.QoS("qos0") }, wantTable: tableQoS},
		{name: "queue", ref: func(o *OVSClient) *TableRef { return o.Queue("queue0") }, wantTable: tableQueue},
		{name: "netflow", ref: func(o *OVSClient) *TableRef { return o.NetFlow("nf0") }, wantTable: tableNetFlow},
		{name: "sflow", ref: func(o *OVSClient) *TableRef { return o.SFlow("sf0") }, wantTable: tableSFlow},
		{name: "ipfix", ref: func(o *OVSClient) *TableRef { return o.IPFIX("ipfix0") }, wantTable: tableIPFIX},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := testOVSDBClient(t)
			db.schema.schema.Tables[tt.wantTable].Columns[colExternalIDs] = &libovsdb.ColumnSchema{}
			rec := &recordingExecutor{}
			db.executor = rec

			_, err := tt.ref(&OVSClient{db: db}).Get(context.Background())
			if err != nil && !IsKind(err, ErrorNotFound) {
				t.Fatalf("Get() = %v", err)
			}
			if len(rec.ops) != 1 {
				t.Fatalf("ops = %d, want one select: %#v", len(rec.ops), rec.ops)
			}
			op := rec.ops[0]
			if op.Op != libovsdb.OperationSelect || op.Table != tt.wantTable {
				t.Fatalf("op = %#v, want select %s", op, tt.wantTable)
			}
			if len(op.Where) != 1 || op.Where[0].Column != colExternalIDs || op.Where[0].Function != libovsdb.ConditionIncludes {
				t.Fatalf("where = %#v, want external_ids includes", op.Where)
			}
		})
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

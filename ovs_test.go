package ovnflow

import (
	"context"
	"encoding/json"
	"testing"

	libovsdb "github.com/ovn-kubernetes/libovsdb/ovsdb"
)

func TestOVSBridgeControllerUsesSingularControllerColumn(t *testing.T) {
	db := testOVSDBClient(t)
	rec := &recordingExecutor{
		results: []libovsdb.OperationResult{
			{Rows: nil},
		},
	}
	db.executor = rec
	builder := (&OVSClient{db: db}).
		Bridge("br-test").
		Ensure().
		WithControllerTarget("tcp:127.0.0.1:6653")

	controllerUUIDs, controllerOps, err := builder.controllerOps(context.Background())
	if err != nil {
		t.Fatalf("controllerOps() = %v", err)
	}
	op := builder.insertBridgeOp(controllerUUIDs, nil, nil)

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

func TestOVSBridgeControllerEnsureReusesExistingController(t *testing.T) {
	db := testOVSDBClient(t)
	rec := &recordingExecutor{
		results: []libovsdb.OperationResult{
			{Rows: []libovsdb.Row{{
				colUUID:   uuidValue("controller-uuid"),
				colTarget: "tcp:127.0.0.1:6653",
			}}},
		},
	}
	db.executor = rec
	builder := (&OVSClient{db: db}).
		Bridge("br-test").
		Ensure().
		WithControllerTarget("tcp:127.0.0.1:6653")

	controllerUUIDs, controllerOps, err := builder.controllerOps(context.Background())
	if err != nil {
		t.Fatalf("controllerOps() = %v", err)
	}
	if len(controllerOps) != 0 {
		t.Fatalf("controller ops = %d, want no insert for existing controller: %#v", len(controllerOps), controllerOps)
	}
	if got, want := controllerUUIDs, []string{"controller-uuid"}; !equalStringSlices(got, want) {
		t.Fatalf("controller UUIDs = %#v, want %#v", got, want)
	}
	if len(rec.ops) != 1 || rec.ops[0].Op != libovsdb.OperationSelect || rec.ops[0].Table != tableController {
		t.Fatalf("recorded ops = %#v, want controller select", rec.ops)
	}
}

func TestOVSBridgeEnsureWithExistingControllerDoesNotInsertDuplicate(t *testing.T) {
	db := testOVSDBClient(t)
	db.schema.schema.Tables[tableBridge].Columns[colController] = columnSchemaFromJSON(t, `{"type":{"key":{"type":"uuid","refTable":"Controller"},"min":0,"max":"unlimited"}}`)
	rec := &recordingExecutor{
		results: []libovsdb.OperationResult{
			{Rows: []libovsdb.Row{{colUUID: uuidValue("bridge-uuid"), colName: "br-test"}}},
			{Rows: []libovsdb.Row{{colUUID: uuidValue("controller-uuid"), colTarget: "tcp:127.0.0.1:6653"}}},
			{Count: 1},
		},
	}
	db.executor = rec

	err := (&OVSClient{db: db}).Bridge("br-test").Ensure().
		WithControllerTarget("tcp:127.0.0.1:6653").
		Execute(context.Background())
	if err != nil {
		t.Fatalf("Ensure() = %v", err)
	}
	var controllerInserts int
	var bridgeControllerMutates int
	for _, op := range rec.ops {
		if op.Op == libovsdb.OperationInsert && op.Table == tableController {
			controllerInserts++
		}
		if op.Op == libovsdb.OperationMutate && op.Table == tableBridge {
			for _, mutation := range op.Mutations {
				if mutation.Column == colController && mutation.Mutator == libovsdb.MutateOperationInsert {
					bridgeControllerMutates++
					if got := rowUUIDSliceValue(libovsdb.Row{colController: mutation.Value}, colController); !equalStringSlices(got, []string{"controller-uuid"}) {
						t.Fatalf("controller mutation value = %#v, want controller-uuid", mutation.Value)
					}
				}
			}
		}
	}
	if controllerInserts != 0 {
		t.Fatalf("controller inserts = %d, want 0: %#v", controllerInserts, rec.ops)
	}
	if bridgeControllerMutates != 1 {
		t.Fatalf("bridge controller mutates = %d, want 1: %#v", bridgeControllerMutates, rec.ops)
	}
}

func TestOVSBridgeControllerEnsureReusesExistingAndInsertsMissing(t *testing.T) {
	db := testOVSDBClient(t)
	rec := &recordingExecutor{
		results: []libovsdb.OperationResult{
			{Rows: []libovsdb.Row{{colUUID: uuidValue("existing-controller-uuid"), colTarget: "tcp:127.0.0.1:6653"}}},
			{Rows: nil},
		},
	}
	db.executor = rec
	builder := (&OVSClient{db: db}).
		Bridge("br-test").
		Ensure().
		WithControllerTarget("tcp:127.0.0.1:6653").
		WithControllerTarget("tcp:127.0.0.1:6654")

	controllerUUIDs, controllerOps, err := builder.controllerOps(context.Background())
	if err != nil {
		t.Fatalf("controllerOps() = %v", err)
	}
	if len(controllerUUIDs) != 2 {
		t.Fatalf("controller UUIDs = %#v, want existing plus named UUID", controllerUUIDs)
	}
	if controllerUUIDs[0] != "existing-controller-uuid" {
		t.Fatalf("first controller UUID = %q, want existing-controller-uuid", controllerUUIDs[0])
	}
	if controllerUUIDs[1] == "" || controllerUUIDs[1] == "existing-controller-uuid" {
		t.Fatalf("second controller UUID = %q, want new named UUID", controllerUUIDs[1])
	}
	if len(controllerOps) != 1 || controllerOps[0].Op != libovsdb.OperationInsert || controllerOps[0].Table != tableController {
		t.Fatalf("controller ops = %#v, want one controller insert", controllerOps)
	}
	if got := rowStringValue(controllerOps[0].Row, colTarget); got != "tcp:127.0.0.1:6654" {
		t.Fatalf("insert controller target = %q, want missing target", got)
	}
	op := builder.insertBridgeOp(controllerUUIDs, nil, nil)
	if got := rowUUIDSliceValue(op.Row, colController); !equalStringSlices(got, controllerUUIDs) {
		t.Fatalf("bridge controller UUID set = %#v, want %#v", got, controllerUUIDs)
	}
}

func TestOVSBridgeControllerEnsureDeduplicatesTargets(t *testing.T) {
	db := testOVSDBClient(t)
	rec := &recordingExecutor{
		results: []libovsdb.OperationResult{
			{Rows: nil},
		},
	}
	db.executor = rec
	builder := (&OVSClient{db: db}).
		Bridge("br-test").
		Ensure().
		WithControllerTarget("tcp:127.0.0.1:6653").
		WithControllerTarget("tcp:127.0.0.1:6653")

	controllerUUIDs, controllerOps, err := builder.controllerOps(context.Background())
	if err != nil {
		t.Fatalf("controllerOps() = %v", err)
	}
	if len(rec.ops) != 1 {
		t.Fatalf("controller selects = %d, want one deduplicated select: %#v", len(rec.ops), rec.ops)
	}
	if len(controllerUUIDs) != 1 || len(controllerOps) != 1 {
		t.Fatalf("controller UUIDs/ops = %#v/%#v, want one UUID and one insert", controllerUUIDs, controllerOps)
	}
}

func TestOVSBridgeEnsureCreatesAdvancedConfigRowsAndReferences(t *testing.T) {
	db := testOVSDBClient(t)
	db.schema.schema.Tables[tableBridge].Columns[colMirrors] = columnSchemaFromJSON(t, `{"type":{"key":{"type":"uuid","refTable":"Mirror"},"min":0,"max":"unlimited"}}`)
	db.schema.schema.Tables[tableBridge].Columns[colFlowTables] = columnSchemaFromJSON(t, `{"type":{"key":{"type":"integer","minInteger":0,"maxInteger":254},"value":{"type":"uuid","refTable":"Flow_Table"},"min":0,"max":"unlimited"}}`)
	db.schema.schema.Tables[tableBridge].Columns[colNetFlow] = columnSchemaFromJSON(t, `{"type":{"key":{"type":"uuid","refTable":"NetFlow"},"min":0}}`)
	db.schema.schema.Tables[tableBridge].Columns[colSFlow] = columnSchemaFromJSON(t, `{"type":{"key":{"type":"uuid","refTable":"sFlow"},"min":0}}`)
	db.schema.schema.Tables[tableBridge].Columns[colIPFIX] = columnSchemaFromJSON(t, `{"type":{"key":{"type":"uuid","refTable":"IPFIX"},"min":0}}`)
	db.schema.schema.Tables[tableBridge].Columns[colAutoAttach] = columnSchemaFromJSON(t, `{"type":{"key":{"type":"uuid","refTable":"AutoAttach"},"min":0}}`)
	db.schema.schema.Tables[tableMirror].Columns[colSelectAll] = columnSchemaFromJSON(t, `{"type":"boolean"}`)
	db.schema.schema.Tables[tableMirror].Columns[colExternalIDs] = columnSchemaFromJSON(t, `{"type":{"key":"string","value":"string","min":0,"max":"unlimited"}}`)
	db.schema.schema.Tables[tableFlowTable].Columns[colExternalIDs] = columnSchemaFromJSON(t, `{"type":{"key":"string","value":"string","min":0,"max":"unlimited"}}`)
	db.schema.schema.Tables[tableNetFlow].Columns[colExternalIDs] = columnSchemaFromJSON(t, `{"type":{"key":"string","value":"string","min":0,"max":"unlimited"}}`)
	db.schema.schema.Tables[tableSFlow].Columns[colExternalIDs] = columnSchemaFromJSON(t, `{"type":{"key":"string","value":"string","min":0,"max":"unlimited"}}`)
	db.schema.schema.Tables[tableIPFIX].Columns[colExternalIDs] = columnSchemaFromJSON(t, `{"type":{"key":"string","value":"string","min":0,"max":"unlimited"}}`)
	rec := &recordingExecutor{
		results: []libovsdb.OperationResult{
			{Rows: nil},
			{Rows: nil},
			{Rows: nil},
			{Rows: nil},
			{Rows: nil},
			{Rows: nil},
			{Rows: nil},
			{Rows: []libovsdb.Row{{colUUID: uuidValue("root-uuid")}}},
			{Count: 1},
			{Count: 1},
			{Count: 1},
			{Count: 1},
			{Count: 1},
			{Count: 1},
			{Count: 1},
			{Count: 1},
		},
	}
	db.executor = rec

	err := (&OVSClient{db: db}).Bridge("br-test").Ensure().
		WithMirror("mirror0", func(mirror *TableBuilder) {
			mirror.WithMirrorSelectAll().WithExternalID("owner", "test")
		}).
		WithFlowTable(0, "ft0", func(flowTable *TableBuilder) {
			flowTable.WithExternalID("owner", "test")
		}).
		WithNetFlow("nf0", func(netflow *TableBuilder) {
			netflow.WithSamplingTarget("127.0.0.1:2055").WithExternalID("owner", "test")
		}).
		WithSFlow("sf0", func(sflow *TableBuilder) {
			sflow.WithSamplingTarget("127.0.0.1:6343").WithExternalID("owner", "test")
		}).
		WithIPFIX("ipfix0", func(ipfix *TableBuilder) {
			ipfix.WithSamplingTarget("127.0.0.1:4739").WithExternalID("owner", "test")
		}).
		WithAutoAttach("aa0", func(autoAttach *TableBuilder) {
			autoAttach.WithColumn(colSystemDescription, "integration").WithColumn(colMappings, ovsIntMap(map[int]int{100: 200}))
		}).
		Execute(context.Background())
	if err != nil {
		t.Fatalf("Ensure() = %v", err)
	}
	bridgeInsert := findRecordedOp(rec.ops, libovsdb.OperationInsert, tableBridge)
	if bridgeInsert == nil {
		t.Fatalf("missing Bridge insert: %#v", rec.ops)
	}
	if len(rowUUIDSliceValue(bridgeInsert.Row, colMirrors)) != 1 {
		t.Fatalf("bridge mirrors = %#v, want one named UUID", bridgeInsert.Row[colMirrors])
	}
	if got := rowIntUUIDMapValue(bridgeInsert.Row, colFlowTables); len(got) != 1 || got[0] == "" {
		t.Fatalf("bridge flow_tables = %#v, want key 0 named UUID", bridgeInsert.Row[colFlowTables])
	}
	if got := rowUUIDSliceValue(bridgeInsert.Row, colNetFlow); len(got) != 1 {
		t.Fatalf("bridge netflow = %#v, want one named UUID", bridgeInsert.Row[colNetFlow])
	}
	if got := rowUUIDSliceValue(bridgeInsert.Row, colSFlow); len(got) != 1 {
		t.Fatalf("bridge sflow = %#v, want one named UUID", bridgeInsert.Row[colSFlow])
	}
	if got := rowUUIDSliceValue(bridgeInsert.Row, colIPFIX); len(got) != 1 {
		t.Fatalf("bridge ipfix = %#v, want one named UUID", bridgeInsert.Row[colIPFIX])
	}
	if got := rowUUIDSliceValue(bridgeInsert.Row, colAutoAttach); len(got) != 1 {
		t.Fatalf("bridge auto_attach = %#v, want one named UUID", bridgeInsert.Row[colAutoAttach])
	}
	if findRecordedOp(rec.ops, libovsdb.OperationInsert, tableMirror) == nil ||
		findRecordedOp(rec.ops, libovsdb.OperationInsert, tableFlowTable) == nil ||
		findRecordedOp(rec.ops, libovsdb.OperationInsert, tableNetFlow) == nil ||
		findRecordedOp(rec.ops, libovsdb.OperationInsert, tableSFlow) == nil ||
		findRecordedOp(rec.ops, libovsdb.OperationInsert, tableIPFIX) == nil ||
		findRecordedOp(rec.ops, libovsdb.OperationInsert, tableAutoAttach) == nil {
		t.Fatalf("missing advanced config inserts: %#v", rec.ops)
	}
	bridgeIndex := recordedOpIndex(rec.ops, libovsdb.OperationInsert, tableBridge)
	for _, table := range []string{tableMirror, tableFlowTable, tableNetFlow, tableSFlow, tableIPFIX, tableAutoAttach} {
		configIndex := recordedOpIndex(rec.ops, libovsdb.OperationInsert, table)
		if configIndex < 0 || bridgeIndex < 0 || configIndex > bridgeIndex {
			t.Fatalf("%s insert index = %d, Bridge insert index = %d, want config insert before Bridge insert: %#v", table, configIndex, bridgeIndex, rec.ops)
		}
	}
}

func TestOVSBridgeEnsureNewBridgeMergesMultipleAdvancedSetAndMapReferences(t *testing.T) {
	db := testOVSDBClient(t)
	db.schema.schema.Tables[tableBridge].Columns[colMirrors] = columnSchemaFromJSON(t, `{"type":{"key":{"type":"uuid","refTable":"Mirror"},"min":0,"max":"unlimited"}}`)
	db.schema.schema.Tables[tableBridge].Columns[colFlowTables] = columnSchemaFromJSON(t, `{"type":{"key":{"type":"integer","minInteger":0,"maxInteger":254},"value":{"type":"uuid","refTable":"Flow_Table"},"min":0,"max":"unlimited"}}`)
	db.schema.schema.Tables[tableMirror].Columns[colExternalIDs] = columnSchemaFromJSON(t, `{"type":{"key":"string","value":"string","min":0,"max":"unlimited"}}`)
	db.schema.schema.Tables[tableFlowTable].Columns[colExternalIDs] = columnSchemaFromJSON(t, `{"type":{"key":"string","value":"string","min":0,"max":"unlimited"}}`)
	rec := &recordingExecutor{
		results: []libovsdb.OperationResult{
			{Rows: nil},
			{Rows: nil},
			{Rows: nil},
			{Rows: nil},
			{Rows: nil},
			{Rows: []libovsdb.Row{{colUUID: uuidValue("root-uuid")}}},
			{Count: 1},
			{Count: 1},
			{Count: 1},
			{Count: 1},
			{Count: 1},
			{Count: 1},
		},
	}
	db.executor = rec

	err := (&OVSClient{db: db}).Bridge("br-test").Ensure().
		WithMirror("mirror0", nil).
		WithMirror("mirror1", nil).
		WithFlowTable(0, "ft0", nil).
		WithFlowTable(1, "ft1", nil).
		Execute(context.Background())
	if err != nil {
		t.Fatalf("Ensure() = %v", err)
	}
	bridgeInsert := findRecordedOp(rec.ops, libovsdb.OperationInsert, tableBridge)
	if bridgeInsert == nil {
		t.Fatalf("missing Bridge insert: %#v", rec.ops)
	}
	if got := rowUUIDSliceValue(bridgeInsert.Row, colMirrors); len(got) != 2 {
		t.Fatalf("bridge mirrors = %#v, want two named UUIDs", bridgeInsert.Row[colMirrors])
	}
	if got := rowIntUUIDMapValue(bridgeInsert.Row, colFlowTables); len(got) != 2 || got[0] == "" || got[1] == "" {
		t.Fatalf("bridge flow_tables = %#v, want keys 0 and 1 named UUIDs", bridgeInsert.Row[colFlowTables])
	}
}

func TestOVSBridgeEnsureSkipsUnsupportedAdvancedConfigReferences(t *testing.T) {
	db := testOVSDBClient(t)
	delete(db.schema.schema.Tables, tableMirror)
	rec := &recordingExecutor{
		results: []libovsdb.OperationResult{
			{Rows: nil},
			{Rows: []libovsdb.Row{{colUUID: uuidValue("root-uuid")}}},
			{Count: 1},
			{Count: 1},
		},
	}
	db.executor = rec

	err := (&OVSClient{db: db}).Bridge("br-test").Ensure().
		WithMirror("mirror0", func(mirror *TableBuilder) {
			mirror.WithMirrorSelectAll()
		}).
		WithNetFlow("nf0", func(netflow *TableBuilder) {
			netflow.WithSamplingTarget("127.0.0.1:2055")
		}).
		Execute(context.Background())
	if err != nil {
		t.Fatalf("Ensure() = %v", err)
	}
	if findRecordedOp(rec.ops, libovsdb.OperationInsert, tableMirror) != nil ||
		findRecordedOp(rec.ops, libovsdb.OperationInsert, tableNetFlow) != nil {
		t.Fatalf("unsupported advanced config rows should not be inserted: %#v", rec.ops)
	}
	bridgeInsert := findRecordedOp(rec.ops, libovsdb.OperationInsert, tableBridge)
	if bridgeInsert == nil {
		t.Fatalf("missing Bridge insert: %#v", rec.ops)
	}
	if _, ok := bridgeInsert.Row[colMirrors]; ok {
		t.Fatalf("bridge insert unexpectedly contains mirrors: %#v", bridgeInsert.Row)
	}
	if _, ok := bridgeInsert.Row[colNetFlow]; ok {
		t.Fatalf("bridge insert unexpectedly contains netflow: %#v", bridgeInsert.Row)
	}
}

func TestOVSBridgeEnsureReportsInvalidSchemaForUnsupportedAdvancedConfigColumns(t *testing.T) {
	db := testOVSDBClient(t)
	db.schema.schema.Tables[tableBridge].Columns[colMirrors] = columnSchemaFromJSON(t, `{"type":{"key":{"type":"uuid","refTable":"Mirror"},"min":0,"max":"unlimited"}}`)
	rec := &recordingExecutor{
		results: []libovsdb.OperationResult{
			{Rows: nil},
		},
	}
	db.executor = rec

	err := (&OVSClient{db: db}).Bridge("br-test").Ensure().
		WithMirror("mirror0", func(mirror *TableBuilder) {
			mirror.WithColumn("unsupported_column", true)
		}).
		Execute(context.Background())
	if !IsKind(err, ErrorInvalidSchema) {
		t.Fatalf("Ensure() = %v, want ErrorInvalidSchema", err)
	}
	if findRecordedOp(rec.ops, libovsdb.OperationInsert, tableBridge) != nil ||
		findRecordedOp(rec.ops, libovsdb.OperationInsert, tableMirror) != nil {
		t.Fatalf("invalid schema should fail before writes: %#v", rec.ops)
	}
}

func TestOVSBridgeEnsureExistingAdvancedConfigMutatesMapAndSetWithoutUpdateOverwrite(t *testing.T) {
	db := testOVSDBClient(t)
	db.schema.schema.Tables[tableBridge].Columns[colMirrors] = columnSchemaFromJSON(t, `{"type":{"key":{"type":"uuid","refTable":"Mirror"},"min":0,"max":"unlimited"}}`)
	db.schema.schema.Tables[tableMirror].Columns[colExternalIDs] = columnSchemaFromJSON(t, `{"type":{"key":"string","value":"string","min":0,"max":"unlimited"}}`)
	db.schema.schema.Tables[tableMirror].Columns[colSelectSrcPort] = columnSchemaFromJSON(t, `{"type":{"key":{"type":"uuid","refTable":"Port"},"min":0,"max":"unlimited"}}`)
	rec := &recordingExecutor{
		results: []libovsdb.OperationResult{
			{Rows: []libovsdb.Row{{colUUID: uuidValue("bridge-uuid"), colName: "br-test"}}},
			{Rows: []libovsdb.Row{{colUUID: uuidValue("mirror-uuid")}}},
			{Count: 1},
			{Count: 1},
		},
	}
	db.executor = rec

	err := (&OVSClient{db: db}).Bridge("br-test").Ensure().
		WithMirror("mirror0", func(mirror *TableBuilder) {
			mirror.WithExternalID("owner", "test").
				MutateUUIDSet(colSelectSrcPort, "port-uuid")
		}).
		Execute(context.Background())
	if err != nil {
		t.Fatalf("Ensure() = %v", err)
	}
	for _, op := range rec.ops {
		if op.Op == libovsdb.OperationUpdate && op.Table == tableMirror {
			if _, ok := op.Row[colExternalIDs]; ok {
				t.Fatalf("Mirror update overwrites external_ids instead of mutating: %#v", rec.ops)
			}
			if _, ok := op.Row[colSelectSrcPort]; ok {
				t.Fatalf("Mirror update overwrites select_src_port instead of mutating: %#v", rec.ops)
			}
		}
	}
	var mirrorMutates int
	for _, op := range rec.ops {
		if op.Op != libovsdb.OperationMutate || op.Table != tableMirror {
			continue
		}
		for _, mutation := range op.Mutations {
			if mutation.Column == colExternalIDs || mutation.Column == colSelectSrcPort {
				mirrorMutates++
			}
		}
	}
	if mirrorMutates != 2 {
		t.Fatalf("Mirror mutate count = %d, want external_ids and select_src_port mutates: %#v", mirrorMutates, rec.ops)
	}
}

func TestOVSBridgeEnsureUpdatesScalarAdvancedConfigReferencesOnExistingBridge(t *testing.T) {
	db := testOVSDBClient(t)
	db.schema.schema.Tables[tableBridge].Columns[colMirrors] = columnSchemaFromJSON(t, `{"type":{"key":{"type":"uuid","refTable":"Mirror"},"min":0,"max":"unlimited"}}`)
	db.schema.schema.Tables[tableBridge].Columns[colFlowTables] = columnSchemaFromJSON(t, `{"type":{"key":{"type":"integer","minInteger":0,"maxInteger":254},"value":{"type":"uuid","refTable":"Flow_Table"},"min":0,"max":"unlimited"}}`)
	db.schema.schema.Tables[tableBridge].Columns[colNetFlow] = columnSchemaFromJSON(t, `{"type":{"key":{"type":"uuid","refTable":"NetFlow"},"min":0}}`)
	db.schema.schema.Tables[tableBridge].Columns[colSFlow] = columnSchemaFromJSON(t, `{"type":{"key":{"type":"uuid","refTable":"sFlow"},"min":0}}`)
	db.schema.schema.Tables[tableBridge].Columns[colIPFIX] = columnSchemaFromJSON(t, `{"type":{"key":{"type":"uuid","refTable":"IPFIX"},"min":0}}`)
	db.schema.schema.Tables[tableBridge].Columns[colAutoAttach] = columnSchemaFromJSON(t, `{"type":{"key":{"type":"uuid","refTable":"AutoAttach"},"min":0}}`)
	db.schema.schema.Tables[tableMirror].Columns[colExternalIDs] = columnSchemaFromJSON(t, `{"type":{"key":"string","value":"string","min":0,"max":"unlimited"}}`)
	db.schema.schema.Tables[tableFlowTable].Columns[colExternalIDs] = columnSchemaFromJSON(t, `{"type":{"key":"string","value":"string","min":0,"max":"unlimited"}}`)
	db.schema.schema.Tables[tableNetFlow].Columns[colExternalIDs] = columnSchemaFromJSON(t, `{"type":{"key":"string","value":"string","min":0,"max":"unlimited"}}`)
	db.schema.schema.Tables[tableSFlow].Columns[colExternalIDs] = columnSchemaFromJSON(t, `{"type":{"key":"string","value":"string","min":0,"max":"unlimited"}}`)
	db.schema.schema.Tables[tableIPFIX].Columns[colExternalIDs] = columnSchemaFromJSON(t, `{"type":{"key":"string","value":"string","min":0,"max":"unlimited"}}`)
	rec := &recordingExecutor{
		results: []libovsdb.OperationResult{
			{Rows: []libovsdb.Row{{colUUID: uuidValue("bridge-uuid"), colName: "br-test"}}},
			{Rows: []libovsdb.Row{{colUUID: uuidValue("mirror-uuid")}}},
			{Rows: []libovsdb.Row{{colUUID: uuidValue("flow-table-uuid")}}},
			{Rows: []libovsdb.Row{{colUUID: uuidValue("netflow-uuid")}}},
			{Rows: []libovsdb.Row{{colUUID: uuidValue("sflow-uuid")}}},
			{Rows: []libovsdb.Row{{colUUID: uuidValue("ipfix-uuid")}}},
			{Rows: []libovsdb.Row{{colUUID: uuidValue("auto-attach-uuid")}}},
			{Count: 1},
			{Count: 1},
			{Count: 1},
			{Count: 1},
			{Count: 1},
			{Count: 1},
			{Count: 1},
			{Count: 1},
			{Count: 1},
			{Count: 1},
			{Count: 1},
			{Count: 1},
		},
	}
	db.executor = rec

	err := (&OVSClient{db: db}).Bridge("br-test").Ensure().
		WithMirror("mirror0", func(mirror *TableBuilder) {
			mirror.WithExternalID("owner", "test")
		}).
		WithFlowTable(0, "ft0", func(flowTable *TableBuilder) {
			flowTable.WithExternalID("owner", "test")
		}).
		WithNetFlow("nf0", func(netflow *TableBuilder) {
			netflow.WithExternalID("owner", "test")
		}).
		WithSFlow("sf0", func(sflow *TableBuilder) {
			sflow.WithExternalID("owner", "test")
		}).
		WithIPFIX("ipfix0", func(ipfix *TableBuilder) {
			ipfix.WithExternalID("owner", "test")
		}).
		WithAutoAttach("aa0", nil).
		Execute(context.Background())
	if err != nil {
		t.Fatalf("Ensure() = %v", err)
	}
	for _, column := range []string{colNetFlow, colSFlow, colIPFIX, colAutoAttach} {
		op := findBridgeReferenceOp(rec.ops, column)
		if op == nil {
			t.Fatalf("missing Bridge reference op for %s: %#v", column, rec.ops)
		}
		if op.Op != libovsdb.OperationUpdate {
			t.Fatalf("Bridge %s reference op = %s, want update: %#v", column, op.Op, op)
		}
		if rowUUIDSliceValue(op.Row, column) == nil {
			t.Fatalf("Bridge %s update row missing UUID: %#v", column, op.Row)
		}
	}
	for _, column := range []string{colMirrors, colFlowTables} {
		op := findBridgeReferenceOp(rec.ops, column)
		if op == nil {
			t.Fatalf("missing Bridge reference op for %s: %#v", column, rec.ops)
		}
		if op.Op != libovsdb.OperationMutate {
			t.Fatalf("Bridge %s reference op = %s, want mutate: %#v", column, op.Op, op)
		}
	}
}

func TestOVSBridgeEnsureCreatesReferencedRowsBeforeBridgeInsert(t *testing.T) {
	db := testOVSDBClient(t)
	db.schema.schema.Tables[tableBridge].Columns[colController] = columnSchemaFromJSON(t, `{"type":{"key":{"type":"uuid","refTable":"Controller"},"min":0,"max":"unlimited"}}`)
	db.schema.schema.Tables[tableBridge].Columns[colMirrors] = columnSchemaFromJSON(t, `{"type":{"key":{"type":"uuid","refTable":"Mirror"},"min":0,"max":"unlimited"}}`)
	db.schema.schema.Tables[tableBridge].Columns[colFlowTables] = columnSchemaFromJSON(t, `{"type":{"key":{"type":"integer","minInteger":0,"maxInteger":254},"value":{"type":"uuid","refTable":"Flow_Table"},"min":0,"max":"unlimited"}}`)
	db.schema.schema.Tables[tableBridge].Columns[colNetFlow] = columnSchemaFromJSON(t, `{"type":{"key":{"type":"uuid","refTable":"NetFlow"},"min":0}}`)
	db.schema.schema.Tables[tableMirror].Columns[colSelectAll] = columnSchemaFromJSON(t, `{"type":"boolean"}`)
	db.schema.schema.Tables[tableMirror].Columns[colExternalIDs] = columnSchemaFromJSON(t, `{"type":{"key":"string","value":"string","min":0,"max":"unlimited"}}`)
	db.schema.schema.Tables[tableFlowTable].Columns[colExternalIDs] = columnSchemaFromJSON(t, `{"type":{"key":"string","value":"string","min":0,"max":"unlimited"}}`)
	db.schema.schema.Tables[tableNetFlow].Columns[colExternalIDs] = columnSchemaFromJSON(t, `{"type":{"key":"string","value":"string","min":0,"max":"unlimited"}}`)
	rec := &recordingExecutor{
		results: []libovsdb.OperationResult{
			{Rows: nil},
			{Rows: nil},
			{Rows: nil},
			{Rows: nil},
			{Rows: nil},
			{Rows: nil},
			{Rows: []libovsdb.Row{{colUUID: uuidValue("root-uuid")}}},
			{Count: 1},
			{Count: 1},
			{Count: 1},
			{Count: 1},
			{Count: 1},
			{Count: 1},
			{Count: 1},
			{Count: 1},
		},
	}
	db.executor = rec

	err := (&OVSClient{db: db}).Bridge("br-test").Ensure().
		WithControllerTarget("tcp:127.0.0.1:6653").
		WithMirror("mirror0", func(mirror *TableBuilder) {
			mirror.WithMirrorSelectAll()
		}).
		WithFlowTable(0, "ft0", nil).
		WithNetFlow("nf0", func(netflow *TableBuilder) {
			netflow.WithSamplingTarget("127.0.0.1:2055")
		}).
		AddPort("p0").
		WithInterfaceType("internal").
		Execute(context.Background())
	if err != nil {
		t.Fatalf("Ensure() = %v", err)
	}
	bridgeIndex := recordedOpIndex(rec.ops, libovsdb.OperationInsert, tableBridge)
	if bridgeIndex < 0 {
		t.Fatalf("missing Bridge insert: %#v", rec.ops)
	}
	for _, table := range []string{tableInterface, tablePort, tableController, tableMirror, tableFlowTable, tableNetFlow} {
		index := recordedOpIndex(rec.ops, libovsdb.OperationInsert, table)
		if index < 0 {
			t.Fatalf("missing %s insert: %#v", table, rec.ops)
		}
		if index > bridgeIndex {
			t.Fatalf("%s insert index = %d, Bridge insert index = %d, want referenced row before Bridge insert: %#v", table, index, bridgeIndex, rec.ops)
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
			{Rows: []libovsdb.Row{{
				colUUID:  uuidValue("br-uuid"),
				colName:  "br-test",
				colPorts: uuidSet("port-uuid"),
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

func TestOVSDeletePortKeepsPortReferencedByAnotherBridge(t *testing.T) {
	db := testOVSDBClient(t)
	rec := &recordingExecutor{
		results: []libovsdb.OperationResult{
			{Rows: []libovsdb.Row{{
				colUUID:  uuidValue("br-uuid"),
				colName:  "br-test",
				colPorts: uuidSet("shared-port-uuid"),
			}}},
			{Rows: []libovsdb.Row{{
				colUUID:       uuidValue("shared-port-uuid"),
				colName:       "p0",
				colInterfaces: uuidSet("iface-uuid"),
			}}},
			{Rows: []libovsdb.Row{
				{colUUID: uuidValue("br-uuid"), colName: "br-test", colPorts: uuidSet("shared-port-uuid")},
				{colUUID: uuidValue("br-other-uuid"), colName: "br-other", colPorts: uuidSet("shared-port-uuid")},
			}},
			{Count: 1},
		},
	}
	db.executor = rec

	err := (&OVSClient{db: db}).Bridge("br-test").DeletePort("p0").Execute(context.Background())
	if err != nil {
		t.Fatalf("DeletePort() = %v", err)
	}
	for _, op := range rec.ops {
		if op.Op == libovsdb.OperationDelete && (op.Table == tablePort || op.Table == tableInterface) {
			t.Fatalf("unexpected delete of shared port/interface: %#v; ops=%#v", op, rec.ops)
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

func TestOVSManagerEnsureDuplicateFallbackRepairsRootReference(t *testing.T) {
	db := testOVSDBClient(t)
	db.schema.schema.Tables[tableOpenVSwitch].Columns[colManagerOptions] = columnSchemaFromJSON(t, `{"type":{"key":{"type":"uuid","refTable":"Manager"},"min":0,"max":"unlimited"}}`)
	rec := &recordingExecutor{
		results: []libovsdb.OperationResult{
			{Rows: nil},
			{Rows: []libovsdb.Row{{colUUID: uuidValue("root-uuid")}}},
			{Error: "constraint violation", Details: "duplicate manager"},
			{Count: 0},
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
	var rootRepairs int
	for _, op := range rec.ops {
		if op.Op == libovsdb.OperationMutate && op.Table == tableOpenVSwitch {
			for _, mutation := range op.Mutations {
				if mutation.Column == colManagerOptions {
					rootRepairs++
				}
			}
		}
	}
	if rootRepairs < 2 {
		t.Fatalf("root manager_options repairs = %d, want insert attempt and fallback repair: %#v", rootRepairs, rec.ops)
	}
	if last := rec.ops[len(rec.ops)-1]; last.Op != libovsdb.OperationMutate || last.Table != tableManager {
		t.Fatalf("last op = %#v, want manager update after fallback root repair", last)
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
		if err := checkOperationResults(out, dbOpenVSwitch, "", "", ""); err != nil {
			return out, err
		}
		return out, nil
	}
	return []libovsdb.OperationResult{{Count: 1}}, nil
}

func (r *recordingExecutor) List(context.Context, any) error {
	return nil
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func recordedOpIndex(ops []libovsdb.Operation, op, table string) int {
	for i := range ops {
		if ops[i].Op == op && ops[i].Table == table {
			return i
		}
	}
	return -1
}

func findBridgeReferenceOp(ops []libovsdb.Operation, column string) *libovsdb.Operation {
	for i := range ops {
		if ops[i].Table != tableBridge {
			continue
		}
		if ops[i].Op == libovsdb.OperationUpdate {
			if _, ok := ops[i].Row[column]; ok {
				return &ops[i]
			}
		}
		if ops[i].Op == libovsdb.OperationMutate {
			for _, mutation := range ops[i].Mutations {
				if mutation.Column == column {
					return &ops[i]
				}
			}
		}
	}
	return nil
}

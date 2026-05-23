package ovnflow

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"

	libovsdb "github.com/ovn-kubernetes/libovsdb/ovsdb"
)

func TestNBNorthboundRuntimeSchemaCanCoverRequestedTables(t *testing.T) {
	required := requiredSchema(dbOVNNorthbound)
	wantTables := []string{
		tableLogicalRouter,
		tableLogicalRouterPort,
		tableACL,
		tableNAT,
		tableLoadBalancer,
		tableDHCPOptions,
		tableDNS,
		tableQoS,
		tableMeter,
		tableMeterBand,
		tablePortGroup,
		tableAddressSet,
		tableGatewayChassis,
		tableHAChassis,
		tableHAChassisGroup,
		tableBFD,
	}
	for _, table := range wantTables {
		if _, ok := required[table]; !ok {
			t.Fatalf("requiredSchema(%s) missing %s", dbOVNNorthbound, table)
		}
	}
}

func TestNBNorthboundBuildersEmitHandwrittenOperations(t *testing.T) {
	tests := []struct {
		name      string
		run       func(*NBClient) error
		wantTable string
		wantOp    string
	}{
		{
			name: "logical router",
			run: func(nb *NBClient) error {
				return nb.LogicalRouter("lr0").Create().WithOption("chassis", "gw").Execute(context.Background())
			},
			wantTable: tableLogicalRouter,
			wantOp:    libovsdb.OperationInsert,
		},
		{
			name: "logical router port",
			run: func(nb *NBClient) error {
				return nb.LogicalRouterPort("lrp0").Create().WithMAC("00:11:22:33:44:55").WithNetwork("192.0.2.1/24").Execute(context.Background())
			},
			wantTable: tableLogicalRouterPort,
			wantOp:    libovsdb.OperationInsert,
		},
		{
			name: "acl",
			run: func(nb *NBClient) error {
				return nb.ACLByMatch("to-lport", 1001, "ip4").Create().WithAction("allow").Execute(context.Background())
			},
			wantTable: tableACL,
			wantOp:    libovsdb.OperationInsert,
		},
		{
			name: "nat",
			run: func(nb *NBClient) error {
				return nb.NATByLogicalIP("snat", "10.0.0.0/24").Create().WithExternalIP("203.0.113.10").Execute(context.Background())
			},
			wantTable: tableNAT,
			wantOp:    libovsdb.OperationInsert,
		},
		{
			name: "load balancer",
			run: func(nb *NBClient) error {
				return nb.LoadBalancer("lb0").Create().WithVIP("198.51.100.10:80", "10.0.0.2:80").Execute(context.Background())
			},
			wantTable: tableLoadBalancer,
			wantOp:    libovsdb.OperationInsert,
		},
		{
			name: "dhcp options",
			run: func(nb *NBClient) error {
				return nb.DHCPOptions("10.0.0.0/24").Create().WithOption("router", "10.0.0.1").Execute(context.Background())
			},
			wantTable: tableDHCPOptions,
			wantOp:    libovsdb.OperationInsert,
		},
		{
			name: "dns",
			run: func(nb *NBClient) error {
				return nb.DNS("dns0").Create().WithRecord("host.local", "10.0.0.2").Execute(context.Background())
			},
			wantTable: tableDNS,
			wantOp:    libovsdb.OperationInsert,
		},
		{
			name: "qos",
			run: func(nb *NBClient) error {
				return nb.QoSByMatch("from-lport", 100, "ip").Create().WithRate(1000).Execute(context.Background())
			},
			wantTable: tableQoS,
			wantOp:    libovsdb.OperationInsert,
		},
		{
			name: "meter",
			run: func(nb *NBClient) error {
				return nb.Meter("meter0").Create().WithUnit("kbps").WithBandUUID("band-uuid").Execute(context.Background())
			},
			wantTable: tableMeter,
			wantOp:    libovsdb.OperationInsert,
		},
		{
			name: "meter band",
			run: func(nb *NBClient) error {
				return nb.MeterBand("band0").Create().WithRate(100).Execute(context.Background())
			},
			wantTable: tableMeterBand,
			wantOp:    libovsdb.OperationInsert,
		},
		{
			name: "port group",
			run: func(nb *NBClient) error {
				return nb.PortGroup("pg0").Create().WithACLUUID("acl-uuid").Execute(context.Background())
			},
			wantTable: tablePortGroup,
			wantOp:    libovsdb.OperationInsert,
		},
		{
			name: "address set",
			run: func(nb *NBClient) error {
				return nb.AddressSet("as0").Create().WithAddress("10.0.0.1").Execute(context.Background())
			},
			wantTable: tableAddressSet,
			wantOp:    libovsdb.OperationInsert,
		},
		{
			name: "gateway chassis",
			run: func(nb *NBClient) error {
				return nb.GatewayChassis("gwc0").Create().WithChassisName("chassis0").WithPriority(10).Execute(context.Background())
			},
			wantTable: tableGatewayChassis,
			wantOp:    libovsdb.OperationInsert,
		},
		{
			name: "ha chassis",
			run: func(nb *NBClient) error {
				return nb.HAChassis("chassis0").Create().WithPriority(10).Execute(context.Background())
			},
			wantTable: tableHAChassis,
			wantOp:    libovsdb.OperationInsert,
		},
		{
			name: "ha chassis group",
			run: func(nb *NBClient) error {
				return nb.HAChassisGroup("hag0").Create().WithHAChassisUUID("hac-uuid").Execute(context.Background())
			},
			wantTable: tableHAChassisGroup,
			wantOp:    libovsdb.OperationInsert,
		},
		{
			name: "bfd",
			run: func(nb *NBClient) error {
				return nb.BFD("lrp0", "192.0.2.2").Create().WithMinTx(100).Execute(context.Background())
			},
			wantTable: tableBFD,
			wantOp:    libovsdb.OperationInsert,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := testNBDBClient(t)
			rec := &nbRecordingExecutor{}
			db.executor = rec
			if err := tt.run(&NBClient{db: db}); err != nil {
				t.Fatalf("builder Execute() = %v", err)
			}
			if len(rec.ops) != 1 {
				t.Fatalf("recorded ops = %d, want 1: %#v", len(rec.ops), rec.ops)
			}
			if rec.ops[0].Table != tt.wantTable || rec.ops[0].Op != tt.wantOp {
				t.Fatalf("op = %s %s, want %s %s", rec.ops[0].Op, rec.ops[0].Table, tt.wantOp, tt.wantTable)
			}
			if len(rec.ops[0].Row) == 0 {
				t.Fatalf("insert row is empty for %s", tt.wantTable)
			}
		})
	}
}

func TestNATBuilderDoesNotWriteUnsetOptionalStringColumns(t *testing.T) {
	db := testNBDBClient(t)
	rec := &nbRecordingExecutor{}
	db.executor = rec

	err := (&NBClient{db: db}).NATByLogicalIP("snat", "10.0.0.0/24").
		Create().
		WithExternalIP("203.0.113.10").
		Execute(context.Background())
	if err != nil {
		t.Fatalf("Create() = %v", err)
	}
	if len(rec.ops) != 1 {
		t.Fatalf("ops = %d, want insert: %#v", len(rec.ops), rec.ops)
	}
	if _, ok := rec.ops[0].Row[colExternalPortRange]; ok {
		t.Fatalf("row contains unset external_port_range: %#v", rec.ops[0].Row)
	}
	if _, ok := rec.ops[0].Row[colMatch]; ok {
		t.Fatalf("row contains unset match: %#v", rec.ops[0].Row)
	}
}

func TestNATBuilderAllowsExplicitEmptyOptionalStringColumns(t *testing.T) {
	db := testNBDBClient(t)
	db.schema.schema.Tables[tableNAT].Columns[colExternalPortRange] = columnSchemaFromJSON(t, `{"type":"string"}`)
	db.schema.schema.Tables[tableNAT].Columns[colMatch] = columnSchemaFromJSON(t, `{"type":"string"}`)
	rec := &nbRecordingExecutor{}
	db.executor = rec

	err := (&NBClient{db: db}).NATByLogicalIP("snat", "10.0.0.0/24").
		Ensure().
		WithExternalIP("203.0.113.10").
		WithExternalPortRange("").
		WithMatch("").
		Execute(context.Background())
	if err != nil {
		t.Fatalf("Ensure() = %v", err)
	}
	if len(rec.ops) < 2 {
		t.Fatalf("ops = %d, want select/insert-or-update: %#v", len(rec.ops), rec.ops)
	}
	row := libovsdb.Row(nil)
	for i := len(rec.ops) - 1; i >= 0; i-- {
		if len(rec.ops[i].Row) > 0 {
			row = rec.ops[i].Row
			break
		}
	}
	if row == nil {
		t.Fatalf("ops do not contain a row operation: %#v", rec.ops)
	}
	if got, ok := row[colExternalPortRange]; !ok || got != "" {
		t.Fatalf("external_port_range = %#v, present %v; want explicit empty string", got, ok)
	}
	if got, ok := row[colMatch]; !ok || got != "" {
		t.Fatalf("match = %#v, present %v; want explicit empty string", got, ok)
	}
}

func TestNBNorthboundEnsureMutatesExistingRows(t *testing.T) {
	db := testNBDBClient(t)
	rec := &nbRecordingExecutor{
		results: []libovsdb.OperationResult{
			{Rows: []libovsdb.Row{{colUUID: uuidValue("lr-uuid")}}},
			{Count: 1},
			{Count: 1},
		},
	}
	db.executor = rec

	err := (&NBClient{db: db}).LogicalRouter("lr0").Ensure().
		WithOption("always_learn_from_arp_request", "true").
		WithExternalID("owner", "test").
		Execute(context.Background())
	if err != nil {
		t.Fatalf("Ensure() = %v", err)
	}
	if len(rec.ops) != 2 {
		t.Fatalf("ops = %d, want select/mutate: %#v", len(rec.ops), rec.ops)
	}
	if got, want := []string{rec.ops[0].Op, rec.ops[1].Op}, []string{libovsdb.OperationSelect, libovsdb.OperationMutate}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ops = %#v, want %#v", got, want)
	}
}

func TestTableRefListHonorsWhereConditions(t *testing.T) {
	db := testNBDBClient(t)
	rec := &nbRecordingExecutor{
		results: []libovsdb.OperationResult{{Rows: []libovsdb.Row{{colUUID: uuidValue("lr-uuid"), colName: "lr0"}}}},
	}
	db.executor = rec

	rows, err := (&NBClient{db: db}).Table(tableLogicalRouter).
		WhereCondition(colExternalIDs, libovsdb.ConditionIncludes, ovsMap(map[string]string{"owner": "test"})).
		List(context.Background())
	if err != nil {
		t.Fatalf("List() = %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if len(rec.ops) != 1 || len(rec.ops[0].Where) != 1 {
		t.Fatalf("select where = %#v, want one explicit condition", rec.ops)
	}
	if rec.ops[0].Where[0].Column != colExternalIDs {
		t.Fatalf("where column = %q, want external_ids", rec.ops[0].Where[0].Column)
	}
}

func TestTableRefDeleteSelectsThenDeletesByUUID(t *testing.T) {
	db := testNBDBClient(t)
	rec := &nbRecordingExecutor{
		results: []libovsdb.OperationResult{
			{Rows: []libovsdb.Row{{colUUID: uuidValue("lr-uuid")}}},
			{Count: 1},
		},
	}
	db.executor = rec

	err := (&NBClient{db: db}).TableBy(tableLogicalRouter, colName, "lr0").Delete().Execute(context.Background())
	if err != nil {
		t.Fatalf("Delete() = %v", err)
	}
	if len(rec.ops) != 2 {
		t.Fatalf("ops = %d, want select/delete: %#v", len(rec.ops), rec.ops)
	}
	if rec.ops[1].Op != libovsdb.OperationDelete || rec.ops[1].Table != tableLogicalRouter {
		t.Fatalf("delete op = %#v", rec.ops[1])
	}
	if len(rec.ops[1].Where) != 1 || rec.ops[1].Where[0].Column != colUUID {
		t.Fatalf("delete where = %#v, want UUID where", rec.ops[1].Where)
	}
}

func TestNBDeleteUnreferencesOnlyMatchingUUIDReferrers(t *testing.T) {
	db := testNBDBClient(t)
	db.schema.schema.Tables[tableLogicalRouter].Columns[colPorts] = columnSchemaFromJSON(t, `{"type":{"key":{"type":"uuid","refTable":"Logical_Router_Port"},"min":0,"max":"unlimited"}}`)
	rec := &nbRecordingExecutor{
		results: []libovsdb.OperationResult{
			{Rows: []libovsdb.Row{{colUUID: uuidValue("lrp-uuid")}}},
			{Rows: []libovsdb.Row{{colUUID: uuidValue("lr-uuid")}}},
			{Count: 1},
			{Count: 1},
		},
	}
	db.executor = rec

	err := (&NBClient{db: db}).LogicalRouterPort("lrp0").Delete().Execute(context.Background())
	if err != nil {
		t.Fatalf("Delete() = %v", err)
	}
	if len(rec.ops) != 4 {
		t.Fatalf("ops = %d, want select target/select refs/mutate ref/delete target: %#v", len(rec.ops), rec.ops)
	}
	if rec.ops[2].Op != libovsdb.OperationMutate || rec.ops[2].Table != tableLogicalRouter {
		t.Fatalf("ref cleanup op = %#v, want Logical_Router mutate", rec.ops[2])
	}
	if len(rec.ops[2].Where) != 1 || rec.ops[2].Where[0].Column != colUUID {
		t.Fatalf("ref cleanup where = %#v, want UUID-specific referrer", rec.ops[2].Where)
	}
	if rec.ops[3].Op != libovsdb.OperationDelete || rec.ops[3].Where[0].Column != colUUID {
		t.Fatalf("target delete op = %#v, want UUID delete", rec.ops[3])
	}
}

func TestNBDeleteUnreferencesMapValuedUUIDRefsByKey(t *testing.T) {
	db := testNBDBClient(t)
	db.schema.schema.Tables[tableLogicalRouter].Columns["ref_map"] = columnSchemaFromJSON(t, `{"type":{"key":"string","value":{"type":"uuid","refTable":"Logical_Router_Port"},"min":0,"max":"unlimited"}}`)
	rec := &nbRecordingExecutor{
		results: []libovsdb.OperationResult{
			{Rows: []libovsdb.Row{{colUUID: uuidValue("lrp-uuid")}}},
			{Rows: []libovsdb.Row{
				{colUUID: uuidValue("lr-uuid"), "ref_map": ovsMap(map[string]string{"match": "lrp-uuid", "other": "other-uuid"})},
				{colUUID: uuidValue("lr-other-uuid"), "ref_map": ovsMap(map[string]string{"other": "other-uuid"})},
			}},
			{Count: 1},
			{Count: 1},
		},
	}
	db.executor = rec

	err := (&NBClient{db: db}).LogicalRouterPort("lrp0").Delete().Execute(context.Background())
	if err != nil {
		t.Fatalf("Delete() = %v", err)
	}
	if len(rec.ops) != 4 {
		t.Fatalf("ops = %d, want select target/select map refs/mutate matching ref/delete target: %#v", len(rec.ops), rec.ops)
	}
	if rec.ops[2].Op != libovsdb.OperationMutate || rec.ops[2].Table != tableLogicalRouter {
		t.Fatalf("map cleanup op = %#v, want Logical_Router mutate", rec.ops[2])
	}
	if len(rec.ops[2].Mutations) != 1 || rec.ops[2].Mutations[0].Column != "ref_map" {
		t.Fatalf("map cleanup mutations = %#v, want ref_map delete", rec.ops[2].Mutations)
	}
	if len(rec.ops[2].Where) != 1 || rec.ops[2].Where[0].Column != colUUID {
		t.Fatalf("map cleanup where = %#v, want UUID-specific referrer", rec.ops[2].Where)
	}
}

func TestTableRefEnsureHandlesConcurrentInsertRace(t *testing.T) {
	db := testNBDBClient(t)
	rec := &nbRecordingExecutor{
		errs: []error{
			nil,
			wrap(ErrorAlreadyExists, dbOVNNorthbound, tableLogicalRouter, "ensure", "lr0", "duplicate name", errors.New("duplicate")),
			nil,
		},
		results: []libovsdb.OperationResult{
			{Rows: nil},
			{},
			{Count: 1},
		},
	}
	db.executor = rec

	err := (&NBClient{db: db}).TableBy(tableLogicalRouter, colName, "lr0").
		Ensure().
		WithExternalID("owner", "test").
		Execute(context.Background())
	if err != nil {
		t.Fatalf("Ensure() = %v, want nil after duplicate fallback update", err)
	}
	if len(rec.ops) != 3 {
		t.Fatalf("ops = %d, want select/insert/update: %#v", len(rec.ops), rec.ops)
	}
	if got, want := []string{rec.ops[0].Op, rec.ops[1].Op, rec.ops[2].Op}, []string{libovsdb.OperationSelect, libovsdb.OperationInsert, libovsdb.OperationMutate}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ops = %#v, want %#v", got, want)
	}
}

func TestNBV01LogicalSwitchAPIStillReturnsBuilder(t *testing.T) {
	builder := (&NBClient{}).LogicalSwitch("ls0").Create().
		WithSubnet("192.0.2.0/24").
		WithExternalID("owner", "test")
	if builder.name != "ls0" || builder.mode != nbModeCreate || builder.subnet != "192.0.2.0/24" {
		t.Fatalf("logical switch builder changed shape: %#v", builder)
	}
}

func testNBDBClient(t *testing.T) *dbClient {
	t.Helper()
	return &dbClient{
		database: dbOVNNorthbound,
		schema:   newSchemaRegistry(dbOVNNorthbound, databaseSchemaWithColumns(dbOVNNorthbound, requiredSchema(dbOVNNorthbound))),
	}
}

type nbRecordingExecutor struct {
	mu      sync.Mutex
	ops     []libovsdb.Operation
	results []libovsdb.OperationResult
	errs    []error
}

func (r *nbRecordingExecutor) Transact(_ context.Context, ops ...libovsdb.Operation) ([]libovsdb.OperationResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ops = append(r.ops, ops...)
	var err error
	if len(r.errs) > 0 {
		err = r.errs[0]
		r.errs = r.errs[1:]
	}
	if r.results != nil {
		if len(r.results) < len(ops) {
			out := append([]libovsdb.OperationResult{}, r.results...)
			r.results = nil
			for len(out) < len(ops) {
				out = append(out, libovsdb.OperationResult{Count: 1})
			}
			return out, err
		}
		out := append([]libovsdb.OperationResult{}, r.results[:len(ops)]...)
		r.results = r.results[len(ops):]
		return out, err
	}
	return []libovsdb.OperationResult{{Count: 1}}, err
}

func (r *nbRecordingExecutor) List(context.Context, any) error {
	return nil
}

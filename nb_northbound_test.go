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

func TestMeterEnsureCreatesInlineBandInSameTransaction(t *testing.T) {
	db := testNBDBClient(t)
	rec := &nbRecordingExecutor{
		results: []libovsdb.OperationResult{
			{Rows: nil},
			{},
			{},
		},
	}
	db.executor = rec

	err := (&NBClient{db: db}).Meter("meter0").Ensure().
		WithUnit("kbps").
		WithNamedBand("band0", "drop", 100).
		WithExternalID("owner", "test").
		Execute(context.Background())
	if err != nil {
		t.Fatalf("Ensure() = %v", err)
	}
	bandOp := findRecordedOp(rec.ops, libovsdb.OperationInsert, tableMeterBand)
	if bandOp == nil {
		t.Fatalf("ops missing Meter_Band insert: %#v", rec.ops)
	}
	if got := rowStringMapValue(bandOp.Row, colExternalIDs)[dnsNameExternalID]; got != "band0" {
		t.Fatalf("band external_ids[%q] = %q, want band0: %#v", dnsNameExternalID, got, bandOp.Row)
	}
	if got := rowStringMapValue(bandOp.Row, colExternalIDs)["owner"]; got != "test" {
		t.Fatalf("band inherited external_ids[owner] = %q, want test: %#v", got, bandOp.Row)
	}
	meterOp := findRecordedOp(rec.ops, libovsdb.OperationInsert, tableMeter)
	if meterOp == nil {
		t.Fatalf("ops missing Meter insert: %#v", rec.ops)
	}
	if len(rowUUIDSliceValue(meterOp.Row, colBands)) != 1 {
		t.Fatalf("meter bands = %#v, want one inline band reference", meterOp.Row[colBands])
	}
}

func TestPortGroupEnsureCreatesInlineACLInSameTransaction(t *testing.T) {
	db := testNBDBClient(t)
	rec := &nbRecordingExecutor{
		results: []libovsdb.OperationResult{
			{Rows: nil},
			{},
			{},
		},
	}
	db.executor = rec

	err := (&NBClient{db: db}).PortGroup("pg0").Ensure().
		WithACL("to-lport", 1001, "outport == \"vm0\"", "allow").
		WithExternalID("owner", "test").
		Execute(context.Background())
	if err != nil {
		t.Fatalf("Ensure() = %v", err)
	}
	aclOp := findRecordedOp(rec.ops, libovsdb.OperationInsert, tableACL)
	if aclOp == nil {
		t.Fatalf("ops missing ACL insert: %#v", rec.ops)
	}
	if got := rowStringMapValue(aclOp.Row, colExternalIDs)["owner"]; got != "test" {
		t.Fatalf("ACL inherited external_ids[owner] = %q, want test: %#v", got, aclOp.Row)
	}
	portGroupOp := findRecordedOp(rec.ops, libovsdb.OperationInsert, tablePortGroup)
	if portGroupOp == nil {
		t.Fatalf("ops missing Port_Group insert: %#v", rec.ops)
	}
	if len(rowUUIDSliceValue(portGroupOp.Row, colACLs)) != 1 {
		t.Fatalf("port group ACLs = %#v, want one inline ACL reference", portGroupOp.Row[colACLs])
	}
}

func TestLogicalRouterPortEnsureCreatesInlineGatewayAndHAInSameTransaction(t *testing.T) {
	db := testNBDBClient(t)
	db.schema.schema.Tables[tableLogicalRouterPort].Columns[colGatewayChassis] = columnSchemaFromJSON(t, `{"type":{"key":{"type":"uuid","refTable":"Gateway_Chassis"},"min":0,"max":"unlimited"}}`)
	db.schema.schema.Tables[tableLogicalRouterPort].Columns[colHAChassisGroup] = columnSchemaFromJSON(t, `{"type":{"key":{"type":"uuid","refTable":"HA_Chassis_Group"},"min":0}}`)
	rec := &nbRecordingExecutor{
		results: []libovsdb.OperationResult{
			{Rows: nil},
			{},
			{},
			{},
			{},
		},
	}
	db.executor = rec

	err := (&NBClient{db: db}).LogicalRouterPort("lrp0").Ensure().
		AttachToRouter("lr0").
		WithMAC("00:00:5e:00:53:01").
		WithNetwork("10.0.0.1/24").
		WithGatewayChassis("gwc0", "gw0", 20).
		WithHAChassisGroup("hag0").
		WithHAChassis("ha0", 30).
		WithExternalID("owner", "test").
		Execute(context.Background())
	if err != nil {
		t.Fatalf("Ensure() = %v", err)
	}
	gatewayOp := findRecordedOp(rec.ops, libovsdb.OperationInsert, tableGatewayChassis)
	if gatewayOp == nil {
		t.Fatalf("ops missing Gateway_Chassis insert: %#v", rec.ops)
	}
	haOp := findRecordedOp(rec.ops, libovsdb.OperationInsert, tableHAChassis)
	if haOp == nil {
		t.Fatalf("ops missing HA_Chassis insert: %#v", rec.ops)
	}
	haGroupOp := findRecordedOp(rec.ops, libovsdb.OperationInsert, tableHAChassisGroup)
	if haGroupOp == nil {
		t.Fatalf("ops missing HA_Chassis_Group insert: %#v", rec.ops)
	}
	lrpOp := findRecordedOp(rec.ops, libovsdb.OperationInsert, tableLogicalRouterPort)
	if lrpOp == nil {
		t.Fatalf("ops missing Logical_Router_Port insert: %#v", rec.ops)
	}
	routerAttachOp := findRecordedOp(rec.ops, libovsdb.OperationMutate, tableLogicalRouter)
	if routerAttachOp == nil {
		t.Fatalf("ops missing Logical_Router attach mutate: %#v", rec.ops)
	}
	if got := rowStringMapValue(gatewayOp.Row, colExternalIDs)["owner"]; got != "test" {
		t.Fatalf("gateway inherited external_ids[owner] = %q, want test", got)
	}
	if got := rowStringMapValue(haOp.Row, colExternalIDs)["owner"]; got != "test" {
		t.Fatalf("ha chassis inherited external_ids[owner] = %q, want test", got)
	}
	if got := rowStringMapValue(haGroupOp.Row, colExternalIDs)["owner"]; got != "test" {
		t.Fatalf("ha group inherited external_ids[owner] = %q, want test", got)
	}
	if len(rowUUIDSliceValue(haGroupOp.Row, colHAChassis)) != 1 {
		t.Fatalf("ha group refs = %#v, want one HA_Chassis reference", haGroupOp.Row[colHAChassis])
	}
	if len(rowUUIDSliceValue(lrpOp.Row, colGatewayChassis)) != 1 {
		t.Fatalf("lrp gateway refs = %#v, want one Gateway_Chassis reference", lrpOp.Row[colGatewayChassis])
	}
	if rowOptionalUUIDValue(lrpOp.Row, colHAChassisGroup) == nil {
		t.Fatalf("lrp ha_chassis_group = %#v, want inline HA group reference", lrpOp.Row[colHAChassisGroup])
	}
	if len(routerAttachOp.Where) != 1 || routerAttachOp.Where[0].Column != colName {
		t.Fatalf("router attach where = %#v, want router name", routerAttachOp.Where)
	}
}

func TestNATEnsureAttachesToRouterInSameTransaction(t *testing.T) {
	db := testNBDBClient(t)
	rec := &nbRecordingExecutor{
		results: []libovsdb.OperationResult{
			{Rows: nil},
			{},
			{Count: 1},
		},
	}
	db.executor = rec

	err := (&NBClient{db: db}).NATByLogicalIP("snat", "10.0.0.0/24").Ensure().
		AttachToRouter("lr0").
		WithExternalIP("192.0.2.10").
		WithExternalID("owner", "test").
		Execute(context.Background())
	if err != nil {
		t.Fatalf("Ensure() = %v", err)
	}
	if len(rec.ops) != 3 {
		t.Fatalf("ops = %d, want select/insert nat/router attach: %#v", len(rec.ops), rec.ops)
	}
	if rec.ops[1].Op != libovsdb.OperationInsert || rec.ops[1].Table != tableNAT {
		t.Fatalf("nat op = %#v, want NAT insert", rec.ops[1])
	}
	if rec.ops[2].Op != libovsdb.OperationMutate || rec.ops[2].Table != tableLogicalRouter {
		t.Fatalf("router attach op = %#v, want Logical_Router mutate", rec.ops[2])
	}
	if len(rec.ops[2].Mutations) != 1 || rec.ops[2].Mutations[0].Column != colNAT {
		t.Fatalf("router attach mutations = %#v, want nat insert", rec.ops[2].Mutations)
	}
}

func TestLoadBalancerEnsureAttachesToRouterInSameTransaction(t *testing.T) {
	db := testNBDBClient(t)
	rec := &nbRecordingExecutor{
		results: []libovsdb.OperationResult{
			{Rows: nil},
			{},
			{Count: 1},
		},
	}
	db.executor = rec

	err := (&NBClient{db: db}).LoadBalancer("lb0").Ensure().
		AttachToRouter("lr0").
		WithVIP("192.0.2.20:80", "10.0.0.20:80").
		WithExternalID("owner", "test").
		Execute(context.Background())
	if err != nil {
		t.Fatalf("Ensure() = %v", err)
	}
	if len(rec.ops) != 3 {
		t.Fatalf("ops = %d, want select/insert lb/router attach: %#v", len(rec.ops), rec.ops)
	}
	if rec.ops[1].Op != libovsdb.OperationInsert || rec.ops[1].Table != tableLoadBalancer {
		t.Fatalf("load balancer op = %#v, want Load_Balancer insert", rec.ops[1])
	}
	if rec.ops[2].Op != libovsdb.OperationMutate || rec.ops[2].Table != tableLogicalRouter {
		t.Fatalf("router attach op = %#v, want Logical_Router mutate", rec.ops[2])
	}
	if len(rec.ops[2].Mutations) != 1 || rec.ops[2].Mutations[0].Column != colLoadBalancer {
		t.Fatalf("router attach mutations = %#v, want load_balancer insert", rec.ops[2].Mutations)
	}
}

func TestQoSEnsureAttachesToSwitchInSameTransaction(t *testing.T) {
	db := testNBDBClient(t)
	db.schema.schema.Tables[tableLogicalSwitch].Columns[colQoSRules] = columnSchemaFromJSON(t, `{"type":{"key":{"type":"uuid","refTable":"QoS"},"min":0,"max":"unlimited"}}`)
	rec := &nbRecordingExecutor{
		results: []libovsdb.OperationResult{
			{Rows: nil},
			{},
			{Count: 1},
		},
	}
	db.executor = rec

	err := (&NBClient{db: db}).QoSByMatch("from-lport", 100, "ip4").Ensure().
		AttachToSwitch("ls0").
		WithRate(1000).
		WithExternalID("owner", "test").
		Execute(context.Background())
	if err != nil {
		t.Fatalf("Ensure() = %v", err)
	}
	if len(rec.ops) != 3 {
		t.Fatalf("ops = %d, want select/insert qos/switch attach: %#v", len(rec.ops), rec.ops)
	}
	if rec.ops[1].Op != libovsdb.OperationInsert || rec.ops[1].Table != tableQoS {
		t.Fatalf("qos op = %#v, want QoS insert", rec.ops[1])
	}
	if rec.ops[2].Op != libovsdb.OperationMutate || rec.ops[2].Table != tableLogicalSwitch {
		t.Fatalf("switch attach op = %#v, want Logical_Switch mutate", rec.ops[2])
	}
	if len(rec.ops[2].Mutations) != 1 || rec.ops[2].Mutations[0].Column != colQoSRules {
		t.Fatalf("switch attach mutations = %#v, want qos_rules insert", rec.ops[2].Mutations)
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

func TestNBDeleteReportsScalarStrongReferenceConflict(t *testing.T) {
	db := testNBDBClient(t)
	const scalarRefColumn = "scalar_ref"
	db.schema.schema.Tables[tableLogicalRouter].Columns[scalarRefColumn] = columnSchemaFromJSON(t, `{"type":{"key":{"type":"uuid","refTable":"Logical_Router_Port"}}}`)
	rec := &nbRecordingExecutor{
		results: []libovsdb.OperationResult{
			{Rows: []libovsdb.Row{{colUUID: uuidValue("lrp-uuid")}}},
			{Rows: []libovsdb.Row{{colUUID: uuidValue("lr-uuid")}}},
		},
	}
	db.executor = rec

	err := (&NBClient{db: db}).TableBy(tableLogicalRouterPort, colName, "lrp0").Delete().Execute(context.Background())
	if !IsKind(err, ErrorConflict) {
		t.Fatalf("Delete() = %v, want ErrorConflict for scalar strong reference", err)
	}
	for _, op := range rec.ops {
		if op.Op == libovsdb.OperationMutate && op.Table == tableLogicalRouter && len(op.Mutations) > 0 && op.Mutations[0].Column == scalarRefColumn {
			t.Fatalf("unexpected scalar UUID mutate: %#v; ops=%#v", op, rec.ops)
		}
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

func findRecordedOp(ops []libovsdb.Operation, op, table string) *libovsdb.Operation {
	for i := range ops {
		if ops[i].Op == op && ops[i].Table == table {
			return &ops[i]
		}
	}
	return nil
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

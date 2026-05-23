package ovnflow

import (
	"context"
	"testing"

	libovsdb "github.com/ovn-kubernetes/libovsdb/ovsdb"
)

func TestSBRuntimeTableHelpersSelectExpectedIdentities(t *testing.T) {
	tests := []struct {
		name       string
		ref        func(*SBClient) *TableRef
		wantTable  string
		wantColumn string
		wantValue  any
	}{
		{name: "chassis", ref: func(s *SBClient) *TableRef { return s.Chassis("ch0") }, wantTable: tableChassis, wantColumn: colName, wantValue: "ch0"},
		{name: "encap", ref: func(s *SBClient) *TableRef { return s.Encap("192.0.2.10") }, wantTable: tableEncap, wantColumn: colIP, wantValue: "192.0.2.10"},
		{name: "port binding", ref: func(s *SBClient) *TableRef { return s.PortBinding("lp0") }, wantTable: tablePortBinding, wantColumn: colLogicalPort, wantValue: "lp0"},
		{name: "mac binding", ref: func(s *SBClient) *TableRef { return s.MACBinding("lp0") }, wantTable: tableMACBinding, wantColumn: colLogicalPort, wantValue: "lp0"},
		{name: "fdb", ref: func(s *SBClient) *TableRef { return s.FDB("00:11:22:33:44:55") }, wantTable: tableFDB, wantColumn: colMAC, wantValue: "00:11:22:33:44:55"},
		{name: "service monitor", ref: func(s *SBClient) *TableRef { return s.ServiceMonitor("lp0") }, wantTable: tableServiceMonitor, wantColumn: colLogicalPort, wantValue: "lp0"},
		{name: "rbac role", ref: func(s *SBClient) *TableRef { return s.RBACRole("role0") }, wantTable: tableRBACRole, wantColumn: colName, wantValue: "role0"},
		{name: "rbac permission", ref: func(s *SBClient) *TableRef { return s.RBACPermission("perm-uuid") }, wantTable: tableRBACPermission, wantColumn: colUUID, wantValue: uuidValue("perm-uuid")},
		{name: "meter", ref: func(s *SBClient) *TableRef { return s.Meter("meter0") }, wantTable: tableMeter, wantColumn: colName, wantValue: "meter0"},
		{name: "bfd", ref: func(s *SBClient) *TableRef { return s.BFD("lp0") }, wantTable: tableBFD, wantColumn: colLogicalPort, wantValue: "lp0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := testSBDBClient(t)
			rec := &nbRecordingExecutor{}
			db.executor = rec

			_, err := tt.ref(&SBClient{db: db}).Get(context.Background())
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

func TestSBTypedReadsUseRuntimeTableRef(t *testing.T) {
	db := testSBDBClient(t)
	rec := &nbRecordingExecutor{}
	db.executor = rec

	rows, err := (&SBClient{db: db}).ListChassis(context.Background())
	if err != nil {
		t.Fatalf("ListChassis() = %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("ListChassis() rows = %d, want 0", len(rows))
	}
	if len(rec.ops) != 1 || rec.ops[0].Op != libovsdb.OperationSelect || rec.ops[0].Table != tableChassis {
		t.Fatalf("ops = %#v, want one Chassis select", rec.ops)
	}

	rec.ops = nil
	_, err = (&SBClient{db: db}).GetPortBinding(context.Background(), "lp0")
	if !IsKind(err, ErrorNotFound) {
		t.Fatalf("GetPortBinding() = %v, want ErrorNotFound", err)
	}
	if len(rec.ops) != 1 || len(rec.ops[0].Where) != 1 || rec.ops[0].Where[0].Column != colLogicalPort {
		t.Fatalf("ops = %#v, want logical_port select", rec.ops)
	}
}

func testSBDBClient(t *testing.T) *dbClient {
	t.Helper()
	required := requiredSchema(dbOVNSouthbound)
	required[tableEncap] = []string{colIP, colType}
	required[tableSBGlobal] = []string{}
	required[tableConnection] = []string{colTarget}
	required[tableSSL] = []string{}
	return &dbClient{
		database: dbOVNSouthbound,
		schema:   newSchemaRegistry(dbOVNSouthbound, databaseSchemaWithColumns(dbOVNSouthbound, required)),
	}
}

package ovnflow

import (
	"context"
	"testing"

	libovsdb "github.com/ovn-kubernetes/libovsdb/ovsdb"
)

func TestDoctorNilClientReportsUnavailable(t *testing.T) {
	report, err := ((*Client)(nil)).Doctor().Run(context.Background())
	if err != nil {
		t.Fatalf("Doctor.Run returned error: %v", err)
	}
	if len(report.Findings) != 1 || report.Findings[0].Code != "client_unavailable" || report.Findings[0].Severity != DoctorError {
		t.Fatalf("findings = %#v", report.Findings)
	}

	report, err = ((*Doctor)(nil)).Run(context.Background())
	if err != nil {
		t.Fatalf("nil Doctor.Run returned error: %v", err)
	}
	if len(report.Findings) != 1 || report.Findings[0].Code != "client_unavailable" {
		t.Fatalf("nil doctor findings = %#v", report.Findings)
	}
}

func TestDoctorCollectsOVSDBSummary(t *testing.T) {
	ctx := context.Background()
	nb := testNBDBClient(t)
	nb.executor = &nbRecordingExecutor{results: []libovsdb.OperationResult{
		{Rows: []libovsdb.Row{{colUUID: uuidValue("ls1")}}},
		{Rows: []libovsdb.Row{{colUUID: uuidValue("lsp1")}, {colUUID: uuidValue("lsp2")}}},
		{Rows: []libovsdb.Row{}},
		{Rows: []libovsdb.Row{}},
		{Rows: []libovsdb.Row{}},
		{Rows: []libovsdb.Row{}},
		{Rows: []libovsdb.Row{}},
		{Rows: []libovsdb.Row{}},
		{Rows: []libovsdb.Row{}},
		{Rows: []libovsdb.Row{{colUUID: uuidValue("localnet"), colType: "localnet"}}},
	}}
	sb := testSBDBClient(t)
	sb.executor = &nbRecordingExecutor{results: []libovsdb.OperationResult{
		{Rows: []libovsdb.Row{{colUUID: uuidValue("ch0")}}},
		{Rows: []libovsdb.Row{{colUUID: uuidValue("dp0")}}},
		{Rows: []libovsdb.Row{{colUUID: uuidValue("pb0")}, {colUUID: uuidValue("pb1")}}},
		{Rows: []libovsdb.Row{}},
		{Rows: []libovsdb.Row{{colUUID: uuidValue("pb0"), colLogicalPort: "lp0", colChassis: uuidValue("ch0")}, {colUUID: uuidValue("pb1"), colLogicalPort: "lp1"}}},
	}}
	ovs := testOVSDBClient(t)
	ovs.executor = &recordingExecutor{results: []libovsdb.OperationResult{
		{Rows: []libovsdb.Row{{colUUID: uuidValue("br0")}}},
		{Rows: []libovsdb.Row{{colUUID: uuidValue("port0")}}},
		{Rows: []libovsdb.Row{{colUUID: uuidValue("iface0")}}},
		{Rows: []libovsdb.Row{}},
		{Rows: []libovsdb.Row{}},
		{Rows: []libovsdb.Row{{colUUID: uuidValue("ovs0"), colExternalIDs: ovsMap(map[string]string{ovsBridgeMappingsKey: "physnet:br-ex"})}}},
	}}

	report, err := (&Client{nb: nb, sb: sb, ovs: ovs}).Doctor().Run(ctx)
	if err != nil {
		t.Fatalf("Doctor.Run returned error: %v", err)
	}
	if report.Northbound.LogicalSwitches != 1 || report.Northbound.LogicalSwitchPorts != 2 || report.Northbound.ProviderLocalnetPorts != 1 {
		t.Fatalf("northbound report = %#v", report.Northbound)
	}
	if report.Southbound.Chassis != 1 || report.Southbound.PortBindings != 2 || report.Southbound.BoundPortBindings != 1 || report.Southbound.UnboundPortBindings != 1 {
		t.Fatalf("southbound report = %#v", report.Southbound)
	}
	if report.OVS.Bridges != 1 || report.OVS.Ports != 1 || report.OVS.Interfaces != 1 || report.OVS.BridgeMappings["physnet"] != "br-ex" {
		t.Fatalf("ovs report = %#v", report.OVS)
	}
	if !report.Databases[dbOVNNorthbound].Connected || !report.Databases[dbOVNSouthbound].Connected || !report.Databases[dbOpenVSwitch].Connected {
		t.Fatalf("database reports = %#v", report.Databases)
	}
}

func TestDiagnosticsDoctorOptionsControlHeavyChecks(t *testing.T) {
	sb := testSBDBClient(t)
	rec := &nbRecordingExecutor{results: []libovsdb.OperationResult{
		{Rows: []libovsdb.Row{}},
		{Rows: []libovsdb.Row{}},
		{Rows: []libovsdb.Row{}},
		{Rows: []libovsdb.Row{}},
		{Rows: []libovsdb.Row{{colUUID: uuidValue("flow0")}}},
	}}
	sb.executor = rec
	report, err := (&Client{sb: sb}).Diagnostics().Doctor(context.Background(), DoctorOptions{
		Checks:       []DiagnosticCheck{DiagnosticCheckCounts},
		IncludeHeavy: true,
	})
	if err != nil {
		t.Fatalf("Doctor returned error: %v", err)
	}
	if report.Southbound.LogicalFlows != 1 {
		t.Fatalf("logical flow count = %d, want 1", report.Southbound.LogicalFlows)
	}
	if len(rec.ops) != 5 || rec.ops[4].Table != tableLogicalFlow {
		t.Fatalf("ops = %#v, want logical flow counted only when heavy is enabled", rec.ops)
	}
}

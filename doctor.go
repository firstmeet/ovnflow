package ovnflow

import (
	"context"
	"errors"
	"fmt"
	"sort"
)

type Doctor struct {
	client *Client
}

type Diagnostics struct {
	client *Client
}

type DoctorSeverity string

type DiagnosticStatus string

type DiagnosticCheck string

const (
	DoctorInfo    DoctorSeverity = "info"
	DoctorWarning DoctorSeverity = "warning"
	DoctorError   DoctorSeverity = "error"

	DiagnosticPass DiagnosticStatus = "pass"
	DiagnosticWarn DiagnosticStatus = "warn"
	DiagnosticFail DiagnosticStatus = "fail"
	DiagnosticSkip DiagnosticStatus = "skip"

	DiagnosticCheckConnections   DiagnosticCheck = "connections"
	DiagnosticCheckSchema        DiagnosticCheck = "schema"
	DiagnosticCheckCounts        DiagnosticCheck = "counts"
	DiagnosticCheckBindings      DiagnosticCheck = "bindings"
	DiagnosticCheckBridgeMapping DiagnosticCheck = "bridge_mappings"
	DiagnosticCheckHeavyTables   DiagnosticCheck = "heavy_tables"
)

type DoctorOptions struct {
	Checks       []DiagnosticCheck
	IncludeHeavy bool
}

type DoctorReport struct {
	Databases  map[string]DoctorDatabaseReport `json:"databases"`
	TableRows  map[string]map[string]int       `json:"table_rows"`
	Checks     []DiagnosticCheckResult         `json:"checks"`
	Northbound DoctorNorthboundReport          `json:"northbound"`
	Southbound DoctorSouthboundReport          `json:"southbound"`
	OVS        DoctorOVSReport                 `json:"ovs"`
	Findings   []DoctorFinding                 `json:"findings"`
}

type DiagnosticCheckResult struct {
	Name    DiagnosticCheck  `json:"name"`
	Status  DiagnosticStatus `json:"status"`
	Message string           `json:"message,omitempty"`
}

type DoctorDatabaseReport struct {
	Name          string `json:"name"`
	Address       string `json:"address,omitempty"`
	SchemaVersion string `json:"schema_version,omitempty"`
	Connected     bool   `json:"connected"`
	Error         string `json:"error,omitempty"`
}

type DoctorNorthboundReport struct {
	LogicalSwitches       int `json:"logical_switches"`
	LogicalSwitchPorts    int `json:"logical_switch_ports"`
	LogicalRouters        int `json:"logical_routers"`
	LogicalRouterPorts    int `json:"logical_router_ports"`
	ACLs                  int `json:"acls"`
	NATs                  int `json:"nats"`
	LoadBalancers         int `json:"load_balancers"`
	DNSRecords            int `json:"dns_records"`
	DHCPOptions           int `json:"dhcp_options"`
	ProviderLocalnetPorts int `json:"provider_localnet_ports"`
}

type DoctorSouthboundReport struct {
	Chassis             int `json:"chassis"`
	Datapaths           int `json:"datapaths"`
	PortBindings        int `json:"port_bindings"`
	BoundPortBindings   int `json:"bound_port_bindings"`
	UnboundPortBindings int `json:"unbound_port_bindings"`
	LogicalFlows        int `json:"logical_flows"`
	ServiceMonitors     int `json:"service_monitors"`
}

type DoctorOVSReport struct {
	Bridges        int               `json:"bridges"`
	Ports          int               `json:"ports"`
	Interfaces     int               `json:"interfaces"`
	BridgeMappings map[string]string `json:"bridge_mappings,omitempty"`
	Managers       int               `json:"managers"`
	Controllers    int               `json:"controllers"`
}

type DoctorFinding struct {
	Severity  DoctorSeverity    `json:"severity"`
	Component string            `json:"component"`
	Code      string            `json:"code"`
	Message   string            `json:"message"`
	Details   map[string]string `json:"details,omitempty"`
}

func (c *Client) Doctor() *Doctor {
	return &Doctor{client: c}
}

func (c *Client) Diagnostics() *Diagnostics {
	return &Diagnostics{client: c}
}

func (d *Doctor) Run(ctx context.Context) (*DoctorReport, error) {
	var client *Client
	if d != nil {
		client = d.client
	}
	return (&Diagnostics{client: client}).Doctor(ctx, DoctorOptions{})
}

func (d *Diagnostics) Doctor(ctx context.Context, opts DoctorOptions) (*DoctorReport, error) {
	report := &DoctorReport{
		Databases: map[string]DoctorDatabaseReport{},
		TableRows: map[string]map[string]int{},
	}
	if d == nil || d.client == nil {
		report.addFinding(DoctorError, "client", "client_unavailable", "ovnflow client is nil", nil)
		report.addCheck(DiagnosticCheckConnections, DiagnosticFail, "client is nil")
		return report, nil
	}
	checks := normalizeDoctorChecks(opts)
	if err := report.inspectNorthbound(ctx, d.client.nb, checks); err != nil {
		return report, err
	}
	if err := report.inspectSouthbound(ctx, d.client.sb, checks); err != nil {
		return report, err
	}
	if err := report.inspectOVS(ctx, d.client.ovs, checks); err != nil {
		return report, err
	}
	report.finalizeChecks(checks)
	return report, nil
}

func (r *DoctorReport) inspectNorthbound(ctx context.Context, db *dbClient, checks map[DiagnosticCheck]bool) error {
	if !r.inspectDatabase(dbOVNNorthbound, db) {
		return nil
	}
	tables := []string{
		tableLogicalSwitch,
		tableLogicalSwitchPort,
		tableLogicalRouter,
		tableLogicalRouterPort,
		tableACL,
		tableNAT,
		tableLoadBalancer,
		tableDNS,
		tableDHCPOptions,
	}
	counts := map[string]int{}
	if checks[DiagnosticCheckCounts] {
		var err error
		counts, err = r.countTables(ctx, db, tables...)
		if err != nil {
			return err
		}
	}
	r.Northbound = DoctorNorthboundReport{
		LogicalSwitches:    counts[tableLogicalSwitch],
		LogicalSwitchPorts: counts[tableLogicalSwitchPort],
		LogicalRouters:     counts[tableLogicalRouter],
		LogicalRouterPorts: counts[tableLogicalRouterPort],
		ACLs:               counts[tableACL],
		NATs:               counts[tableNAT],
		LoadBalancers:      counts[tableLoadBalancer],
		DNSRecords:         counts[tableDNS],
		DHCPOptions:        counts[tableDHCPOptions],
	}
	if checks[DiagnosticCheckBridgeMapping] {
		rows, err := newTableRef(db, tableLogicalSwitchPort, "", "").Where(colType, "localnet").List(ctx)
		if err == nil {
			r.Northbound.ProviderLocalnetPorts = len(rows)
		} else if !IsKind(err, ErrorInvalidSchema) {
			r.addFinding(DoctorWarning, dbOVNNorthbound, "localnet_probe_failed", "could not inspect localnet ports", map[string]string{"error": err.Error()})
		}
	}
	if r.Northbound.LogicalSwitches == 0 {
		r.addFinding(DoctorInfo, dbOVNNorthbound, "no_logical_switches", "no logical switches were found", nil)
	}
	return nil
}

func (r *DoctorReport) inspectSouthbound(ctx context.Context, db *dbClient, checks map[DiagnosticCheck]bool) error {
	if !r.inspectDatabase(dbOVNSouthbound, db) {
		return nil
	}
	tables := []string{
		tableChassis,
		tableDatapathBinding,
		tablePortBinding,
		tableServiceMonitor,
	}
	if checks[DiagnosticCheckHeavyTables] {
		tables = append(tables, tableLogicalFlow)
	}
	counts := map[string]int{}
	if checks[DiagnosticCheckCounts] {
		var err error
		counts, err = r.countTables(ctx, db, tables...)
		if err != nil {
			return err
		}
	}
	r.Southbound = DoctorSouthboundReport{
		Chassis:         counts[tableChassis],
		Datapaths:       counts[tableDatapathBinding],
		PortBindings:    counts[tablePortBinding],
		LogicalFlows:    counts[tableLogicalFlow],
		ServiceMonitors: counts[tableServiceMonitor],
	}
	if checks[DiagnosticCheckBindings] {
		rows, err := (&SBClient{db: db}).ListPortBindings(ctx)
		if err == nil {
			for _, row := range rows {
				if row.Chassis == nil || *row.Chassis == "" {
					r.Southbound.UnboundPortBindings++
				} else {
					r.Southbound.BoundPortBindings++
				}
			}
		} else if !IsKind(err, ErrorInvalidSchema) {
			r.addFinding(DoctorWarning, dbOVNSouthbound, "port_binding_probe_failed", "could not inspect port binding chassis state", map[string]string{"error": err.Error()})
		}
	}
	if r.Southbound.Chassis == 0 {
		r.addFinding(DoctorWarning, dbOVNSouthbound, "no_chassis", "no southbound chassis rows were found", nil)
	}
	if r.Southbound.PortBindings > 0 && r.Southbound.BoundPortBindings == 0 {
		r.addFinding(DoctorWarning, dbOVNSouthbound, "all_ports_unbound", "port bindings exist but none are bound to chassis", nil)
	}
	return nil
}

func (r *DoctorReport) inspectOVS(ctx context.Context, db *dbClient, checks map[DiagnosticCheck]bool) error {
	if !r.inspectDatabase(dbOpenVSwitch, db) {
		return nil
	}
	tables := []string{tableBridge, tablePort, tableInterface, tableManager, tableController}
	counts := map[string]int{}
	if checks[DiagnosticCheckCounts] {
		var err error
		counts, err = r.countTables(ctx, db, tables...)
		if err != nil {
			return err
		}
	}
	r.OVS = DoctorOVSReport{
		Bridges:     counts[tableBridge],
		Ports:       counts[tablePort],
		Interfaces:  counts[tableInterface],
		Managers:    counts[tableManager],
		Controllers: counts[tableController],
	}
	if checks[DiagnosticCheckBridgeMapping] {
		mappings, err := (&OVSClient{db: db}).GetBridgeMappings(ctx)
		if err != nil {
			if !IsKind(err, ErrorNotFound) && !IsKind(err, ErrorInvalidSchema) {
				r.addFinding(DoctorWarning, dbOpenVSwitch, "bridge_mapping_probe_failed", "could not inspect OVN bridge mappings", map[string]string{"error": err.Error()})
			}
		} else {
			r.OVS.BridgeMappings = mappings
		}
	}
	if r.OVS.Bridges == 0 {
		r.addFinding(DoctorWarning, dbOpenVSwitch, "no_bridges", "no OVS bridges were found", nil)
	}
	return nil
}

func normalizeDoctorChecks(opts DoctorOptions) map[DiagnosticCheck]bool {
	checks := map[DiagnosticCheck]bool{}
	selected := opts.Checks
	if len(selected) == 0 {
		selected = []DiagnosticCheck{
			DiagnosticCheckConnections,
			DiagnosticCheckSchema,
			DiagnosticCheckCounts,
			DiagnosticCheckBindings,
			DiagnosticCheckBridgeMapping,
		}
	}
	for _, check := range selected {
		checks[check] = true
	}
	if opts.IncludeHeavy {
		checks[DiagnosticCheckHeavyTables] = true
	}
	return checks
}

func (r *DoctorReport) inspectDatabase(name string, db *dbClient) bool {
	report := DoctorDatabaseReport{Name: name}
	if db == nil {
		report.Error = "database client is nil"
		r.Databases[name] = report
		r.addFinding(DoctorError, name, "database_unavailable", report.Error, nil)
		return false
	}
	report.Address = db.address
	if db.schema != nil {
		report.SchemaVersion = db.schema.Version()
	}
	if db.executor == nil && db.raw == nil {
		report.Error = "database executor is nil"
		r.Databases[name] = report
		r.addFinding(DoctorError, name, "database_unavailable", report.Error, nil)
		return false
	}
	report.Connected = true
	r.Databases[name] = report
	return true
}

func (r *DoctorReport) countTables(ctx context.Context, db *dbClient, tables ...string) (map[string]int, error) {
	out := map[string]int{}
	if db == nil {
		return out, nil
	}
	if r.TableRows[db.database] == nil {
		r.TableRows[db.database] = map[string]int{}
	}
	for _, table := range tables {
		if db.schema == nil || !db.schema.HasTable(table) {
			r.addFinding(DoctorWarning, db.database, "table_missing", fmt.Sprintf("table %s is not present in schema", table), map[string]string{"table": table})
			continue
		}
		rows, err := newTableRef(db, table, "", "").selectRows(ctx, nil, []string{colUUID})
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return out, err
			}
			r.addFinding(DoctorWarning, db.database, "table_count_failed", fmt.Sprintf("could not count %s", table), map[string]string{"table": table, "error": err.Error()})
			continue
		}
		out[table] = len(rows)
		r.TableRows[db.database][table] = len(rows)
	}
	return out, nil
}

func (r *DoctorReport) addFinding(severity DoctorSeverity, component, code, message string, details map[string]string) {
	r.Findings = append(r.Findings, DoctorFinding{
		Severity:  severity,
		Component: component,
		Code:      code,
		Message:   message,
		Details:   cloneStringMap(details),
	})
	sort.SliceStable(r.Findings, func(i, j int) bool {
		if r.Findings[i].Severity != r.Findings[j].Severity {
			return severityRank(r.Findings[i].Severity) > severityRank(r.Findings[j].Severity)
		}
		if r.Findings[i].Component != r.Findings[j].Component {
			return r.Findings[i].Component < r.Findings[j].Component
		}
		return r.Findings[i].Code < r.Findings[j].Code
	})
}

func (r *DoctorReport) addCheck(name DiagnosticCheck, status DiagnosticStatus, message string) {
	for i := range r.Checks {
		if r.Checks[i].Name == name {
			if diagnosticStatusRank(status) > diagnosticStatusRank(r.Checks[i].Status) {
				r.Checks[i].Status = status
				r.Checks[i].Message = message
			}
			return
		}
	}
	r.Checks = append(r.Checks, DiagnosticCheckResult{Name: name, Status: status, Message: message})
}

func (r *DoctorReport) finalizeChecks(checks map[DiagnosticCheck]bool) {
	for check := range checks {
		r.addCheck(check, DiagnosticPass, "")
	}
	for _, finding := range r.Findings {
		status := DiagnosticWarn
		if finding.Severity == DoctorError {
			status = DiagnosticFail
		}
		switch finding.Code {
		case "database_unavailable", "client_unavailable":
			r.addCheck(DiagnosticCheckConnections, status, finding.Message)
		case "table_missing":
			r.addCheck(DiagnosticCheckSchema, status, finding.Message)
		case "table_count_failed":
			r.addCheck(DiagnosticCheckCounts, status, finding.Message)
		case "port_binding_probe_failed", "all_ports_unbound", "no_chassis":
			r.addCheck(DiagnosticCheckBindings, status, finding.Message)
		case "bridge_mapping_probe_failed":
			r.addCheck(DiagnosticCheckBridgeMapping, status, finding.Message)
		}
	}
	sort.Slice(r.Checks, func(i, j int) bool { return r.Checks[i].Name < r.Checks[j].Name })
}

func diagnosticStatusRank(status DiagnosticStatus) int {
	switch status {
	case DiagnosticFail:
		return 3
	case DiagnosticWarn:
		return 2
	case DiagnosticSkip:
		return 1
	default:
		return 0
	}
}

func severityRank(severity DoctorSeverity) int {
	switch severity {
	case DoctorError:
		return 3
	case DoctorWarning:
		return 2
	default:
		return 1
	}
}

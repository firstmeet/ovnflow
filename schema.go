package ovnflow

import (
	"fmt"
	"sort"

	libovsdb "github.com/ovn-kubernetes/libovsdb/ovsdb"
)

// SchemaRegistry captures the runtime capabilities advertised by an OVN/OVS
// database. Builders use it to fail fast for required columns and to skip
// optional version-specific columns.
type SchemaRegistry struct {
	database string
	schema   libovsdb.DatabaseSchema
}

func newSchemaRegistry(database string, schema libovsdb.DatabaseSchema) *SchemaRegistry {
	return &SchemaRegistry{database: database, schema: schema}
}

// Database returns the OVSDB database name represented by the registry.
func (s *SchemaRegistry) Database() string {
	if s == nil {
		return ""
	}
	return s.database
}

// Version returns the runtime OVSDB schema version.
func (s *SchemaRegistry) Version() string {
	if s == nil {
		return ""
	}
	return s.schema.Version
}

// HasTable reports whether the runtime schema contains table.
func (s *SchemaRegistry) HasTable(table string) bool {
	return s != nil && s.schema.Table(table) != nil
}

// HasColumn reports whether table.column exists in the runtime schema.
func (s *SchemaRegistry) HasColumn(table, column string) bool {
	tableSchema := s.table(table)
	return tableSchema != nil && tableSchema.Column(column) != nil
}

// Columns returns the schema columns for table, including _uuid first.
func (s *SchemaRegistry) Columns(table string) []string {
	tableSchema := s.table(table)
	if tableSchema == nil {
		return nil
	}
	columns := make([]string, 0, len(tableSchema.Columns)+1)
	columns = append(columns, colUUID)
	for column := range tableSchema.Columns {
		columns = append(columns, column)
	}
	sort.Strings(columns[1:])
	return columns
}

// RequireTable returns ErrorInvalidSchema when table is unavailable.
func (s *SchemaRegistry) RequireTable(table string) error {
	if s.HasTable(table) {
		return nil
	}
	return wrap(ErrorInvalidSchema, s.Database(), table, "schema", "", fmt.Sprintf("required table %s is missing", table), nil)
}

// RequireColumns returns ErrorInvalidSchema when any required column is missing.
func (s *SchemaRegistry) RequireColumns(table string, columns ...string) error {
	if err := s.RequireTable(table); err != nil {
		return err
	}
	for _, column := range columns {
		if column == "" {
			continue
		}
		if !s.HasColumn(table, column) {
			return wrap(ErrorInvalidSchema, s.Database(), table, "schema", "", fmt.Sprintf("required column %s.%s is missing", table, column), nil)
		}
	}
	return nil
}

func (s *SchemaRegistry) RequireConditionColumns(table string, conditions ...libovsdb.Condition) error {
	for _, condition := range conditions {
		if err := s.RequireColumns(table, condition.Column); err != nil {
			return err
		}
	}
	return nil
}

func (s *SchemaRegistry) ReferenceColumns(table, refTable string) []string {
	tableSchema := s.table(table)
	if tableSchema == nil || refTable == "" {
		return nil
	}
	var columns []string
	for name, column := range tableSchema.Columns {
		if columnReferencesTable(column, refTable) {
			columns = append(columns, name)
		}
	}
	sort.Strings(columns)
	return columns
}

func (s *SchemaRegistry) existingColumns(table string, columns ...string) []string {
	out := make([]string, 0, len(columns))
	for _, column := range columns {
		if s.HasColumn(table, column) {
			out = append(out, column)
		}
	}
	return out
}

func (s *SchemaRegistry) table(table string) *libovsdb.TableSchema {
	if s == nil {
		return nil
	}
	return s.schema.Table(table)
}

func (s *SchemaRegistry) column(table, column string) *libovsdb.ColumnSchema {
	tableSchema := s.table(table)
	if tableSchema == nil {
		return nil
	}
	return tableSchema.Column(column)
}

func columnReferencesTable(column *libovsdb.ColumnSchema, refTable string) bool {
	if column == nil || column.TypeObj == nil || column.TypeObj.Key == nil {
		return false
	}
	if baseReferencesTable(column.TypeObj.Key, refTable) {
		return true
	}
	return column.TypeObj.Value != nil && baseReferencesTable(column.TypeObj.Value, refTable)
}

func baseReferencesTable(base *libovsdb.BaseType, refTable string) bool {
	if base == nil {
		return false
	}
	table, err := base.RefTable()
	return err == nil && table == refTable
}

func requiredSchema(database string) map[string][]string {
	switch database {
	case dbOVNNorthbound:
		return map[string][]string{
			tableLogicalSwitch:     {colName, colPorts, colExternalIDs, colOtherConfig},
			tableLogicalSwitchPort: {colName, colAddresses, colExternalIDs, colOptions, colType},
			tableLogicalRouter:     {colName, colPorts, colStaticRoutes, colNAT, colLoadBalancer, colOptions, colExternalIDs},
			tableLogicalRouterPort: {colName, colMAC, colNetworks, colOptions, colExternalIDs},
			tableACL:               {colPriority, colDirection, colMatch, colAction, colExternalIDs},
			tableNAT:               {colType, colLogicalIP, colExternalIP, colExternalIDs},
			tableLoadBalancer:      {colName, colVIPs, colProtocol, colExternalIDs},
			tableDHCPOptions:       {colCIDR, colOptions, colExternalIDs},
			tableDNS:               {colRecords, colExternalIDs},
			tableQoS:               {colPriority, colDirection, colMatch, colAction, colBandwidth, colExternalIDs},
			tableMeter:             {colName, colUnit, colBands, colExternalIDs},
			tableMeterBand:         {colAction, colRate, colExternalIDs},
			tablePortGroup:         {colName, colPorts, colACLs, colExternalIDs},
			tableAddressSet:        {colName, colAddresses, colExternalIDs},
			tableGatewayChassis:    {colName, colChassisName, colPriority, colExternalIDs},
			tableHAChassis:         {colChassisName, colPriority, colExternalIDs},
			tableHAChassisGroup:    {colName, colHAChassis, colExternalIDs},
			tableBFD:               {colLogicalPort, colDstIP, colStatus, colExternalIDs},
		}
	case dbOVNSouthbound:
		return map[string][]string{
			tableChassis:         {colName},
			tablePortBinding:     {colLogicalPort, colDatapath},
			tableDatapathBinding: {colTunnelKey},
			tableLogicalFlow:     {"pipeline", "table_id", colMatch, "actions"},
			tableMACBinding:      {colLogicalPort, colIP, colMAC, colDatapath},
			tableFDB:             {colMAC, colDPKey, colPortKey},
			tableMulticastGroup:  {colDatapath, colTunnelKey, colPorts},
			tableServiceMonitor:  {colIP, colProtocol, colPort, colLogicalPort},
			tableRBACRole:        {colName, "permissions"},
			tableRBACPermission:  {"table", "authorization", "insert_delete", "update"},
			tableMeter:           {colName, colUnit, colBands},
			tableMeterBand:       {colAction, colRate, colBurstSize},
			tableDNS:             {colRecords, "datapaths"},
			tableBFD:             {colLogicalPort, colDstIP},
		}
	case dbOpenVSwitch:
		return map[string][]string{
			tableOpenVSwitch: {colBridges, colExternalIDs},
			tableBridge:      {colName, colPorts},
			tablePort:        {colName, colInterfaces},
			tableInterface:   {colName, colType},
			tableController:  {colTarget, colExternalIDs, colOtherConfig},
			tableManager:     {colTarget, colExternalIDs, colOtherConfig},
			tableMirror:      {colName, colSelectSrcPort, colSelectDstPort, colOutputPort},
			tableQoS:         {colType, colQueues},
			tableQueue:       {},
			tableFlowTable:   {colName},
			tableNetFlow:     {colTargets, colEngineType, colEngineID, colActiveTimeout},
			tableSFlow:       {colAgent, colTargets, colHeader, colSampling, colPolling},
			tableIPFIX:       {colTargets, colSampling},
			tableSSL:         {colPrivateKey, colCertificate, colCACert, colBootstrapCACert},
			tableAutoAttach:  {colSystemName, colSystemDescription, colMappings},
		}
	default:
		return nil
	}
}

func validateDatabaseSchema(schema libovsdb.DatabaseSchema, required map[string][]string) error {
	if len(required) == 0 {
		return nil
	}
	for table, columns := range required {
		tableSchema := schema.Table(table)
		if tableSchema == nil {
			return fmt.Errorf("required table %s is missing from schema %s", table, schema.Name)
		}
		for _, column := range columns {
			if tableSchema.Column(column) == nil {
				return fmt.Errorf("required column %s.%s is missing from schema %s", table, column, schema.Name)
			}
		}
	}
	return nil
}

package ovnflow

import (
	"reflect"
	"strings"
	"testing"

	libovsdb "github.com/ovn-kubernetes/libovsdb/ovsdb"
)

func TestValidateDatabaseSchemaAcceptsRequiredColumns(t *testing.T) {
	required := requiredSchema(dbOVNNorthbound)
	schema := databaseSchemaWithColumns(dbOVNNorthbound, required)

	if err := validateDatabaseSchema(schema, required); err != nil {
		t.Fatalf("validateDatabaseSchema() = %v, want nil", err)
	}
}

func TestValidateDatabaseSchemaRejectsMissingTable(t *testing.T) {
	required := requiredSchema(dbOVNNorthbound)
	schema := databaseSchemaWithColumns(dbOVNNorthbound, required)
	delete(schema.Tables, tableLogicalSwitchPort)

	err := validateDatabaseSchema(schema, required)
	if err == nil {
		t.Fatal("validateDatabaseSchema() succeeded, want missing table error")
	}
	if !strings.Contains(err.Error(), tableLogicalSwitchPort) {
		t.Fatalf("validateDatabaseSchema() = %v, want table name in error", err)
	}
}

func TestValidateDatabaseSchemaRejectsMissingColumn(t *testing.T) {
	required := requiredSchema(dbOpenVSwitch)
	schema := databaseSchemaWithColumns(dbOpenVSwitch, required)
	delete(schema.Tables[tableInterface].Columns, colType)

	err := validateDatabaseSchema(schema, required)
	if err == nil {
		t.Fatal("validateDatabaseSchema() succeeded, want missing column error")
	}
	if !strings.Contains(err.Error(), tableInterface+"."+colType) {
		t.Fatalf("validateDatabaseSchema() = %v, want table.column in error", err)
	}
}

func TestRequiredSchemaDocumentsV01Surface(t *testing.T) {
	tests := []struct {
		database string
		tables   []string
	}{
		{database: dbOVNNorthbound, tables: []string{tableLogicalSwitch, tableLogicalSwitchPort}},
		{database: dbOVNSouthbound, tables: []string{tableChassis, tablePortBinding, tableDatapathBinding}},
		{database: dbOpenVSwitch, tables: []string{tableOpenVSwitch, tableBridge, tablePort, tableInterface}},
	}

	for _, tt := range tests {
		t.Run(tt.database, func(t *testing.T) {
			required := requiredSchema(tt.database)
			for _, table := range tt.tables {
				if _, ok := required[table]; !ok {
					t.Fatalf("requiredSchema(%q) missing table %q", tt.database, table)
				}
			}
		})
	}
}

func TestSchemaRegistryReportsRuntimeCapabilities(t *testing.T) {
	required := map[string][]string{
		tableLogicalSwitch: {colPorts, colName, colExternalIDs},
	}
	schema := databaseSchemaWithColumns(dbOVNNorthbound, required)
	schema.Version = "20.30.0"

	registry := newSchemaRegistry(dbOVNNorthbound, schema)
	if registry.Database() != dbOVNNorthbound {
		t.Fatalf("Database() = %q, want %q", registry.Database(), dbOVNNorthbound)
	}
	if registry.Version() != "20.30.0" {
		t.Fatalf("Version() = %q, want 20.30.0", registry.Version())
	}
	if !registry.HasTable(tableLogicalSwitch) {
		t.Fatalf("HasTable(%q) = false, want true", tableLogicalSwitch)
	}
	if !registry.HasColumn(tableLogicalSwitch, colPorts) {
		t.Fatalf("HasColumn(%q, %q) = false, want true", tableLogicalSwitch, colPorts)
	}
	if got, want := registry.Columns(tableLogicalSwitch), []string{colUUID, colExternalIDs, colName, colPorts}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Columns() = %#v, want %#v", got, want)
	}
}

func TestSchemaRegistryRequireHelpers(t *testing.T) {
	registry := newSchemaRegistry(dbOpenVSwitch, databaseSchemaWithColumns(dbOpenVSwitch, map[string][]string{
		tableBridge: {colName},
	}))

	if err := registry.RequireTable(tableBridge); err != nil {
		t.Fatalf("RequireTable() = %v, want nil", err)
	}
	if err := registry.RequireColumns(tableBridge, colName, ""); err != nil {
		t.Fatalf("RequireColumns() = %v, want nil", err)
	}
	if err := registry.RequireTable(tablePort); !IsKind(err, ErrorInvalidSchema) {
		t.Fatalf("missing table kind = %q for %v, want %q", KindOf(err), err, ErrorInvalidSchema)
	}
	if err := registry.RequireColumns(tableBridge, colPorts); !IsKind(err, ErrorInvalidSchema) {
		t.Fatalf("missing column kind = %q for %v, want %q", KindOf(err), err, ErrorInvalidSchema)
	}
}

func databaseSchemaWithColumns(name string, required map[string][]string) libovsdb.DatabaseSchema {
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

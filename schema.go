package ovnflow

import (
	"fmt"

	libovsdb "github.com/ovn-kubernetes/libovsdb/ovsdb"
)

func requiredSchema(database string) map[string][]string {
	switch database {
	case dbOVNNorthbound:
		return map[string][]string{
			tableLogicalSwitch:     {colName, colPorts, colExternalIDs, colOtherConfig},
			tableLogicalSwitchPort: {colName, colAddresses, colExternalIDs, colOptions, colType},
		}
	case dbOVNSouthbound:
		return map[string][]string{
			tableChassis:         {colName, "hostname", colExternalIDs},
			tablePortBinding:     {colLogicalPort, colChassis, colDatapath, "mac", colExternalIDs},
			tableDatapathBinding: {"tunnel_key", colExternalIDs},
		}
	case dbOpenVSwitch:
		return map[string][]string{
			tableOpenVSwitch: {colBridges, colExternalIDs},
			tableBridge:      {colName, colPorts, colExternalIDs},
			tablePort:        {colName, colInterfaces, colExternalIDs},
			tableInterface:   {colName, colType, colExternalIDs},
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

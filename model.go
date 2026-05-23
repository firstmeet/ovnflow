package ovnflow

import libmodel "github.com/ovn-kubernetes/libovsdb/model"

const (
	dbOVNNorthbound = "OVN_Northbound"
	dbOVNSouthbound = "OVN_Southbound"
	dbOpenVSwitch   = "Open_vSwitch"

	tableLogicalSwitch     = "Logical_Switch"
	tableLogicalSwitchPort = "Logical_Switch_Port"
	tableChassis           = "Chassis"
	tablePortBinding       = "Port_Binding"
	tableDatapathBinding   = "Datapath_Binding"
	tableOpenVSwitch       = "Open_vSwitch"
	tableBridge            = "Bridge"
	tablePort              = "Port"
	tableInterface         = "Interface"

	colUUID        = "_uuid"
	colName        = "name"
	colPorts       = "ports"
	colAddresses   = "addresses"
	colExternalIDs = "external_ids"
	colOtherConfig = "other_config"
	colOptions     = "options"
	colInterfaces  = "interfaces"
	colType        = "type"
	colBridges     = "bridges"
	colDatapath    = "datapath"
	colChassis     = "chassis"
	colLogicalPort = "logical_port"
)

// LogicalSwitch is the v0.1 OVN Northbound Logical_Switch model.
type LogicalSwitch struct {
	UUID        string            `ovsdb:"_uuid"`
	Name        string            `ovsdb:"name"`
	Ports       []string          `ovsdb:"ports"`
	ExternalIDs map[string]string `ovsdb:"external_ids"`
	OtherConfig map[string]string `ovsdb:"other_config"`
}

// LogicalSwitchPort is the v0.1 OVN Northbound Logical_Switch_Port model.
type LogicalSwitchPort struct {
	UUID        string            `ovsdb:"_uuid"`
	Name        string            `ovsdb:"name"`
	Addresses   []string          `ovsdb:"addresses"`
	ExternalIDs map[string]string `ovsdb:"external_ids"`
	Options     map[string]string `ovsdb:"options"`
	Type        string            `ovsdb:"type"`
}

// SBChassis is a minimal OVN Southbound Chassis model.
type SBChassis struct {
	UUID        string            `ovsdb:"_uuid"`
	Name        string            `ovsdb:"name"`
	Hostname    string            `ovsdb:"hostname"`
	ExternalIDs map[string]string `ovsdb:"external_ids"`
}

// SBPortBinding is a minimal OVN Southbound Port_Binding model.
type SBPortBinding struct {
	UUID        string            `ovsdb:"_uuid"`
	LogicalPort string            `ovsdb:"logical_port"`
	Chassis     *string           `ovsdb:"chassis"`
	Datapath    string            `ovsdb:"datapath"`
	MAC         []string          `ovsdb:"mac"`
	ExternalIDs map[string]string `ovsdb:"external_ids"`
}

// SBDatapathBinding is a minimal OVN Southbound Datapath_Binding model.
type SBDatapathBinding struct {
	UUID        string            `ovsdb:"_uuid"`
	TunnelKey   int               `ovsdb:"tunnel_key"`
	ExternalIDs map[string]string `ovsdb:"external_ids"`
}

// OpenVSwitch is the Open_vSwitch root table model.
type OpenVSwitch struct {
	UUID        string            `ovsdb:"_uuid"`
	Bridges     []string          `ovsdb:"bridges"`
	ExternalIDs map[string]string `ovsdb:"external_ids"`
}

// OVSBridge is the Open_vSwitch Bridge model.
type OVSBridge struct {
	UUID        string            `ovsdb:"_uuid"`
	Name        string            `ovsdb:"name"`
	Ports       []string          `ovsdb:"ports"`
	ExternalIDs map[string]string `ovsdb:"external_ids"`
}

// OVSPort is the Open_vSwitch Port model.
type OVSPort struct {
	UUID        string            `ovsdb:"_uuid"`
	Name        string            `ovsdb:"name"`
	Interfaces  []string          `ovsdb:"interfaces"`
	ExternalIDs map[string]string `ovsdb:"external_ids"`
}

// OVSInterface is the Open_vSwitch Interface model.
type OVSInterface struct {
	UUID        string            `ovsdb:"_uuid"`
	Name        string            `ovsdb:"name"`
	Type        string            `ovsdb:"type"`
	ExternalIDs map[string]string `ovsdb:"external_ids"`
}

func nbDBModel() (libmodel.ClientDBModel, error) {
	db, err := libmodel.NewClientDBModel(dbOVNNorthbound, map[string]libmodel.Model{
		tableLogicalSwitch:     &LogicalSwitch{},
		tableLogicalSwitchPort: &LogicalSwitchPort{},
	})
	if err != nil {
		return db, err
	}
	db.SetIndexes(map[string][]libmodel.ClientIndex{
		tableLogicalSwitch: {
			{Columns: []libmodel.ColumnKey{{Column: colName}}},
		},
		tableLogicalSwitchPort: {
			{Columns: []libmodel.ColumnKey{{Column: colName}}},
		},
	})
	return db, nil
}

func sbDBModel() (libmodel.ClientDBModel, error) {
	db, err := libmodel.NewClientDBModel(dbOVNSouthbound, map[string]libmodel.Model{
		tableChassis:         &SBChassis{},
		tablePortBinding:     &SBPortBinding{},
		tableDatapathBinding: &SBDatapathBinding{},
	})
	if err != nil {
		return db, err
	}
	db.SetIndexes(map[string][]libmodel.ClientIndex{
		tableChassis: {
			{Columns: []libmodel.ColumnKey{{Column: colName}}},
		},
		tablePortBinding: {
			{Columns: []libmodel.ColumnKey{{Column: colLogicalPort}}},
		},
	})
	return db, nil
}

func ovsDBModel() (libmodel.ClientDBModel, error) {
	db, err := libmodel.NewClientDBModel(dbOpenVSwitch, map[string]libmodel.Model{
		tableOpenVSwitch: &OpenVSwitch{},
		tableBridge:      &OVSBridge{},
		tablePort:        &OVSPort{},
		tableInterface:   &OVSInterface{},
	})
	if err != nil {
		return db, err
	}
	db.SetIndexes(map[string][]libmodel.ClientIndex{
		tableBridge: {
			{Columns: []libmodel.ColumnKey{{Column: colName}}},
		},
		tablePort: {
			{Columns: []libmodel.ColumnKey{{Column: colName}}},
		},
		tableInterface: {
			{Columns: []libmodel.ColumnKey{{Column: colName}}},
		},
	})
	return db, nil
}

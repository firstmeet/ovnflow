package ovnflow

import libmodel "github.com/ovn-kubernetes/libovsdb/model"

const (
	dbOVNNorthbound = "OVN_Northbound"
	dbOVNSouthbound = "OVN_Southbound"
	dbOpenVSwitch   = "Open_vSwitch"

	tableLogicalSwitch     = "Logical_Switch"
	tableLogicalSwitchPort = "Logical_Switch_Port"
	tableLogicalRouter     = "Logical_Router"
	tableLogicalRouterPort = "Logical_Router_Port"
	tableACL               = "ACL"
	tableNAT               = "NAT"
	tableLoadBalancer      = "Load_Balancer"
	tableDHCPOptions       = "DHCP_Options"
	tableDNS               = "DNS"
	tableQoS               = "QoS"
	tableMeter             = "Meter"
	tableMeterBand         = "Meter_Band"
	tablePortGroup         = "Port_Group"
	tableAddressSet        = "Address_Set"
	tableGatewayChassis    = "Gateway_Chassis"
	tableHAChassis         = "HA_Chassis"
	tableHAChassisGroup    = "HA_Chassis_Group"
	tableBFD               = "BFD"
	tableNBGlobal          = "NB_Global"
	tableSBGlobal          = "SB_Global"
	tableConnection        = "Connection"
	tableSSL               = "SSL"
	tableForwardingGroup   = "Forwarding_Group"
	tableChassis           = "Chassis"
	tableEncap             = "Encap"
	tablePortBinding       = "Port_Binding"
	tableDatapathBinding   = "Datapath_Binding"
	tableLogicalFlow       = "Logical_Flow"
	tableMACBinding        = "MAC_Binding"
	tableFDB               = "FDB"
	tableMulticastGroup    = "Multicast_Group"
	tableServiceMonitor    = "Service_Monitor"
	tableRBACRole          = "RBAC_Role"
	tableRBACPermission    = "RBAC_Permission"
	tableOpenVSwitch       = "Open_vSwitch"
	tableBridge            = "Bridge"
	tablePort              = "Port"
	tableInterface         = "Interface"
	tableController        = "Controller"
	tableManager           = "Manager"
	tableMirror            = "Mirror"
	tableQueue             = "Queue"
	tableFlowTable         = "Flow_Table"
	tableNetFlow           = "NetFlow"
	tableSFlow             = "sFlow"
	tableIPFIX             = "IPFIX"
	tableAutoAttach        = "AutoAttach"

	colUUID              = "_uuid"
	colName              = "name"
	colPorts             = "ports"
	colAddresses         = "addresses"
	colExternalIDs       = "external_ids"
	colOtherConfig       = "other_config"
	colOptions           = "options"
	colInterfaces        = "interfaces"
	colType              = "type"
	colBridges           = "bridges"
	colDatapath          = "datapath"
	colChassis           = "chassis"
	colLogicalPort       = "logical_port"
	colTunnelKey         = "tunnel_key"
	colIP                = "ip"
	colMAC               = "mac"
	colProtocol          = "protocol"
	colStatus            = "status"
	colPort              = "port"
	colPortKey           = "port_key"
	colDPKey             = "dp_key"
	colDstIP             = "dst_ip"
	colSrcPort           = "src_port"
	colDisc              = "disc"
	colNetworks          = "networks"
	colPriority          = "priority"
	colDirection         = "direction"
	colMatch             = "match"
	colAction            = "action"
	colTier              = "tier"
	colUUIDs             = "uuids"
	colRules             = "rules"
	colAddresses2        = "addresses"
	colDNSRecords        = "records"
	colRecords           = "records"
	colLoadBalancer      = "load_balancer"
	colLoadBalancers     = "load_balancer"
	colNAT               = "nat"
	colStaticRoutes      = "static_routes"
	colPolicies          = "policies"
	colGatewayChassis    = "gateway_chassis"
	colHAChassisGroup    = "ha_chassis_group"
	colTarget            = "target"
	colController        = "controller"
	colControllers       = "controllers"
	colManagerOptions    = "manager_options"
	colManagers          = "managers"
	colMirrors           = "mirrors"
	colQoS               = "qos"
	colQoSRules          = "qos_rules"
	colQueues            = "queues"
	colFlowTables        = "flow_tables"
	colAutoAttach        = "auto_attach"
	colNetFlow           = "netflow"
	colSFlow             = "sflow"
	colIPFIX             = "ipfix"
	colSSL               = "ssl"
	colFailMode          = "fail_mode"
	colDatapathType      = "datapath_type"
	colSelectAll         = "select_all"
	colSelectSrcPort     = "select_src_port"
	colSelectDstPort     = "select_dst_port"
	colOutputPort        = "output_port"
	colTargets           = "targets"
	colEngineType        = "engine_type"
	colEngineID          = "engine_id"
	colActiveTimeout     = "active_timeout"
	colAgent             = "agent"
	colHeader            = "header"
	colSampling          = "sampling"
	colPolling           = "polling"
	colSystemName        = "system_name"
	colSystemDescription = "system_description"
	colMappings          = "mappings"
	colPrivateKey        = "private_key"
	colCertificate       = "certificate"
	colCACert            = "ca_cert"
	colBootstrapCACert   = "bootstrap_ca_cert"
	colACLs              = "acls"
	colAllowedExtIPs     = "allowed_ext_ips"
	colBandwidth         = "bandwidth"
	colBands             = "bands"
	colBurstSize         = "burst_size"
	colChassisName       = "chassis_name"
	colCIDR              = "cidr"
	colDetectMult        = "detect_mult"
	colEnabled           = "enabled"
	colExemptedExtIPs    = "exempted_ext_ips"
	colExternalIP        = "external_ip"
	colExternalMAC       = "external_mac"
	colExternalPortRange = "external_port_range"
	colFair              = "fair"
	colGatewayPort       = "gateway_port"
	colHAChassis         = "ha_chassis"
	colIPPortMappings    = "ip_port_mappings"
	colIPv6Prefix        = "ipv6_prefix"
	colIPv6RAConfigs     = "ipv6_ra_configs"
	colLabel             = "label"
	colLog               = "log"
	colLogicalIP         = "logical_ip"
	colMeter             = "meter"
	colMinRx             = "min_rx"
	colMinTx             = "min_tx"
	colPeer              = "peer"
	colRate              = "rate"
	colSelectionFields   = "selection_fields"
	colSeverity          = "severity"
	colUnit              = "unit"
	colVIPs              = "vips"
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

// LogicalRouter is an OVN Northbound Logical_Router model.
type LogicalRouter struct {
	UUID          string            `ovsdb:"_uuid"`
	Name          string            `ovsdb:"name"`
	Ports         []string          `ovsdb:"ports"`
	StaticRoutes  []string          `ovsdb:"static_routes"`
	NAT           []string          `ovsdb:"nat"`
	LoadBalancers []string          `ovsdb:"load_balancer"`
	Options       map[string]string `ovsdb:"options"`
	ExternalIDs   map[string]string `ovsdb:"external_ids"`
}

// LogicalRouterPort is an OVN Northbound Logical_Router_Port model.
type LogicalRouterPort struct {
	UUID           string            `ovsdb:"_uuid"`
	Name           string            `ovsdb:"name"`
	MAC            string            `ovsdb:"mac"`
	Networks       []string          `ovsdb:"networks"`
	GatewayChassis []string          `ovsdb:"gateway_chassis"`
	HAChassisGroup *string           `ovsdb:"ha_chassis_group"`
	Peer           *string           `ovsdb:"peer"`
	Enabled        *bool             `ovsdb:"enabled"`
	IPv6Prefix     []string          `ovsdb:"ipv6_prefix"`
	IPv6RAConfigs  map[string]string `ovsdb:"ipv6_ra_configs"`
	Options        map[string]string `ovsdb:"options"`
	ExternalIDs    map[string]string `ovsdb:"external_ids"`
}

// ACL is an OVN Northbound ACL model.
type ACL struct {
	UUID        string            `ovsdb:"_uuid"`
	Name        *string           `ovsdb:"name"`
	Priority    int               `ovsdb:"priority"`
	Direction   string            `ovsdb:"direction"`
	Match       string            `ovsdb:"match"`
	Action      string            `ovsdb:"action"`
	Log         bool              `ovsdb:"log"`
	Meter       *string           `ovsdb:"meter"`
	Severity    *string           `ovsdb:"severity"`
	Label       int               `ovsdb:"label"`
	Tier        int               `ovsdb:"tier"`
	Options     map[string]string `ovsdb:"options"`
	ExternalIDs map[string]string `ovsdb:"external_ids"`
}

// NAT is an OVN Northbound NAT model.
type NAT struct {
	UUID              string            `ovsdb:"_uuid"`
	Type              string            `ovsdb:"type"`
	LogicalIP         string            `ovsdb:"logical_ip"`
	ExternalIP        string            `ovsdb:"external_ip"`
	LogicalPort       *string           `ovsdb:"logical_port"`
	ExternalMAC       *string           `ovsdb:"external_mac"`
	ExternalPortRange string            `ovsdb:"external_port_range"`
	GatewayPort       *string           `ovsdb:"gateway_port"`
	AllowedExtIPs     *string           `ovsdb:"allowed_ext_ips"`
	ExemptedExtIPs    *string           `ovsdb:"exempted_ext_ips"`
	Match             string            `ovsdb:"match"`
	Priority          int               `ovsdb:"priority"`
	Options           map[string]string `ovsdb:"options"`
	ExternalIDs       map[string]string `ovsdb:"external_ids"`
}

// LoadBalancer is an OVN Northbound Load_Balancer model.
type LoadBalancer struct {
	UUID            string            `ovsdb:"_uuid"`
	Name            string            `ovsdb:"name"`
	VIPs            map[string]string `ovsdb:"vips"`
	Protocol        *string           `ovsdb:"protocol"`
	SelectionFields []string          `ovsdb:"selection_fields"`
	IPPortMappings  map[string]string `ovsdb:"ip_port_mappings"`
	HealthCheck     []string          `ovsdb:"health_check"`
	Options         map[string]string `ovsdb:"options"`
	ExternalIDs     map[string]string `ovsdb:"external_ids"`
}

// DHCPOptions is an OVN Northbound DHCP_Options model.
type DHCPOptions struct {
	UUID        string            `ovsdb:"_uuid"`
	CIDR        string            `ovsdb:"cidr"`
	Options     map[string]string `ovsdb:"options"`
	ExternalIDs map[string]string `ovsdb:"external_ids"`
}

// DNS is an OVN Northbound DNS model.
type DNS struct {
	UUID        string            `ovsdb:"_uuid"`
	Records     map[string]string `ovsdb:"records"`
	Options     map[string]string `ovsdb:"options"`
	ExternalIDs map[string]string `ovsdb:"external_ids"`
}

// QoS is an OVN Northbound QoS model.
type QoS struct {
	UUID        string            `ovsdb:"_uuid"`
	Priority    int               `ovsdb:"priority"`
	Direction   string            `ovsdb:"direction"`
	Match       string            `ovsdb:"match"`
	Action      map[string]int    `ovsdb:"action"`
	Bandwidth   map[string]int    `ovsdb:"bandwidth"`
	ExternalIDs map[string]string `ovsdb:"external_ids"`
}

// Meter is an OVN Northbound Meter model.
type Meter struct {
	UUID        string            `ovsdb:"_uuid"`
	Name        string            `ovsdb:"name"`
	Unit        string            `ovsdb:"unit"`
	Bands       []string          `ovsdb:"bands"`
	Fair        *bool             `ovsdb:"fair"`
	ExternalIDs map[string]string `ovsdb:"external_ids"`
}

// MeterBand is an OVN Northbound Meter_Band model.
type MeterBand struct {
	UUID        string            `ovsdb:"_uuid"`
	Action      string            `ovsdb:"action"`
	Rate        int               `ovsdb:"rate"`
	BurstSize   int               `ovsdb:"burst_size"`
	ExternalIDs map[string]string `ovsdb:"external_ids"`
}

// PortGroup is an OVN Northbound Port_Group model.
type PortGroup struct {
	UUID        string            `ovsdb:"_uuid"`
	Name        string            `ovsdb:"name"`
	Ports       []string          `ovsdb:"ports"`
	ACLs        []string          `ovsdb:"acls"`
	ExternalIDs map[string]string `ovsdb:"external_ids"`
}

// AddressSet is an OVN Northbound Address_Set model.
type AddressSet struct {
	UUID        string            `ovsdb:"_uuid"`
	Name        string            `ovsdb:"name"`
	Addresses   []string          `ovsdb:"addresses"`
	ExternalIDs map[string]string `ovsdb:"external_ids"`
}

// GatewayChassis is an OVN Northbound Gateway_Chassis model.
type GatewayChassis struct {
	UUID        string            `ovsdb:"_uuid"`
	Name        string            `ovsdb:"name"`
	ChassisName string            `ovsdb:"chassis_name"`
	Priority    int               `ovsdb:"priority"`
	Options     map[string]string `ovsdb:"options"`
	ExternalIDs map[string]string `ovsdb:"external_ids"`
}

// HAChassis is an OVN Northbound HA_Chassis model.
type HAChassis struct {
	UUID        string            `ovsdb:"_uuid"`
	ChassisName string            `ovsdb:"chassis_name"`
	Priority    int               `ovsdb:"priority"`
	ExternalIDs map[string]string `ovsdb:"external_ids"`
}

// HAChassisGroup is an OVN Northbound HA_Chassis_Group model.
type HAChassisGroup struct {
	UUID        string            `ovsdb:"_uuid"`
	Name        string            `ovsdb:"name"`
	HAChassis   []string          `ovsdb:"ha_chassis"`
	ExternalIDs map[string]string `ovsdb:"external_ids"`
}

// BFD is an OVN Northbound BFD model.
type BFD struct {
	UUID        string            `ovsdb:"_uuid"`
	LogicalPort string            `ovsdb:"logical_port"`
	DstIP       string            `ovsdb:"dst_ip"`
	MinTx       *int              `ovsdb:"min_tx"`
	MinRx       *int              `ovsdb:"min_rx"`
	DetectMult  *int              `ovsdb:"detect_mult"`
	Status      *string           `ovsdb:"status"`
	Options     map[string]string `ovsdb:"options"`
	ExternalIDs map[string]string `ovsdb:"external_ids"`
}

// SBChassis is a minimal OVN Southbound Chassis model.
type SBChassis struct {
	UUID        string            `ovsdb:"_uuid"`
	Name        string            `ovsdb:"name"`
	Hostname    string            `ovsdb:"hostname"`
	ExternalIDs map[string]string `ovsdb:"external_ids"`
	Encaps      []string          `ovsdb:"encaps"`
	NbCfg       int               `ovsdb:"nb_cfg"`
	OtherConfig map[string]string `ovsdb:"other_config"`
}

// SBPortBinding is a minimal OVN Southbound Port_Binding model.
type SBPortBinding struct {
	UUID           string            `ovsdb:"_uuid"`
	LogicalPort    string            `ovsdb:"logical_port"`
	Type           string            `ovsdb:"type"`
	Chassis        *string           `ovsdb:"chassis"`
	Datapath       string            `ovsdb:"datapath"`
	TunnelKey      int               `ovsdb:"tunnel_key"`
	ParentPort     *string           `ovsdb:"parent_port"`
	Tag            *int              `ovsdb:"tag"`
	VirtualParent  *string           `ovsdb:"virtual_parent"`
	Encap          *string           `ovsdb:"encap"`
	GatewayChassis []string          `ovsdb:"gateway_chassis"`
	HAChassisGroup *string           `ovsdb:"ha_chassis_group"`
	MAC            []string          `ovsdb:"mac"`
	NatAddresses   []string          `ovsdb:"nat_addresses"`
	Up             *bool             `ovsdb:"up"`
	Options        map[string]string `ovsdb:"options"`
	ExternalIDs    map[string]string `ovsdb:"external_ids"`
}

// SBDatapathBinding is a minimal OVN Southbound Datapath_Binding model.
type SBDatapathBinding struct {
	UUID          string            `ovsdb:"_uuid"`
	TunnelKey     int               `ovsdb:"tunnel_key"`
	LoadBalancers []string          `ovsdb:"load_balancers"`
	ExternalIDs   map[string]string `ovsdb:"external_ids"`
}

// SBLogicalFlow is an OVN Southbound Logical_Flow model.
type SBLogicalFlow struct {
	UUID            string            `ovsdb:"_uuid"`
	LogicalDatapath *string           `ovsdb:"logical_datapath"`
	LogicalDPGroup  *string           `ovsdb:"logical_dp_group"`
	Pipeline        string            `ovsdb:"pipeline"`
	TableID         int               `ovsdb:"table_id"`
	Priority        int               `ovsdb:"priority"`
	Match           string            `ovsdb:"match"`
	Actions         string            `ovsdb:"actions"`
	ExternalIDs     map[string]string `ovsdb:"external_ids"`
}

// SBMACBinding is an OVN Southbound MAC_Binding model.
type SBMACBinding struct {
	UUID        string `ovsdb:"_uuid"`
	LogicalPort string `ovsdb:"logical_port"`
	IP          string `ovsdb:"ip"`
	MAC         string `ovsdb:"mac"`
	Datapath    string `ovsdb:"datapath"`
}

// SBFDB is an OVN Southbound FDB model.
type SBFDB struct {
	UUID    string `ovsdb:"_uuid"`
	MAC     string `ovsdb:"mac"`
	DPKey   int    `ovsdb:"dp_key"`
	PortKey int    `ovsdb:"port_key"`
}

// SBMulticastGroup is an OVN Southbound Multicast_Group model.
type SBMulticastGroup struct {
	UUID      string   `ovsdb:"_uuid"`
	Datapath  string   `ovsdb:"datapath"`
	Name      string   `ovsdb:"name"`
	TunnelKey int      `ovsdb:"tunnel_key"`
	Ports     []string `ovsdb:"ports"`
}

// SBServiceMonitor is an OVN Southbound Service_Monitor model.
type SBServiceMonitor struct {
	UUID        string            `ovsdb:"_uuid"`
	IP          string            `ovsdb:"ip"`
	Protocol    *string           `ovsdb:"protocol"`
	Port        int               `ovsdb:"port"`
	LogicalPort string            `ovsdb:"logical_port"`
	SrcMAC      string            `ovsdb:"src_mac"`
	SrcIP       string            `ovsdb:"src_ip"`
	Status      *string           `ovsdb:"status"`
	Options     map[string]string `ovsdb:"options"`
	ExternalIDs map[string]string `ovsdb:"external_ids"`
}

// SBRBACRole is an OVN Southbound RBAC_Role model.
type SBRBACRole struct {
	UUID        string            `ovsdb:"_uuid"`
	Name        string            `ovsdb:"name"`
	Permissions map[string]string `ovsdb:"permissions"`
}

// SBRBACPermission is an OVN Southbound RBAC_Permission model.
type SBRBACPermission struct {
	UUID          string   `ovsdb:"_uuid"`
	Table         string   `ovsdb:"table"`
	Authorization []string `ovsdb:"authorization"`
	InsertDelete  bool     `ovsdb:"insert_delete"`
	Update        []string `ovsdb:"update"`
}

// SBMeter is an OVN Southbound Meter model.
type SBMeter struct {
	UUID  string   `ovsdb:"_uuid"`
	Name  string   `ovsdb:"name"`
	Unit  string   `ovsdb:"unit"`
	Bands []string `ovsdb:"bands"`
}

// SBMeterBand is an OVN Southbound Meter_Band model.
type SBMeterBand struct {
	UUID      string `ovsdb:"_uuid"`
	Action    string `ovsdb:"action"`
	Rate      int    `ovsdb:"rate"`
	BurstSize int    `ovsdb:"burst_size"`
}

// SBDNS is an OVN Southbound DNS model.
type SBDNS struct {
	UUID        string            `ovsdb:"_uuid"`
	Records     map[string]string `ovsdb:"records"`
	Datapaths   []string          `ovsdb:"datapaths"`
	ExternalIDs map[string]string `ovsdb:"external_ids"`
}

// SBBFD is an OVN Southbound BFD model.
type SBBFD struct {
	UUID        string            `ovsdb:"_uuid"`
	SrcPort     int               `ovsdb:"src_port"`
	Disc        int               `ovsdb:"disc"`
	LogicalPort string            `ovsdb:"logical_port"`
	DstIP       string            `ovsdb:"dst_ip"`
	MinTx       int               `ovsdb:"min_tx"`
	MinRx       int               `ovsdb:"min_rx"`
	DetectMult  int               `ovsdb:"detect_mult"`
	Status      string            `ovsdb:"status"`
	ExternalIDs map[string]string `ovsdb:"external_ids"`
	Options     map[string]string `ovsdb:"options"`
}

// OpenVSwitch is the Open_vSwitch root table model.
type OpenVSwitch struct {
	UUID        string            `ovsdb:"_uuid"`
	Bridges     []string          `ovsdb:"bridges"`
	Managers    []string          `ovsdb:"manager_options"`
	SSL         *string           `ovsdb:"ssl"`
	ExternalIDs map[string]string `ovsdb:"external_ids"`
	OtherConfig map[string]string `ovsdb:"other_config"`
}

// OVSBridge is the Open_vSwitch Bridge model.
type OVSBridge struct {
	UUID         string            `ovsdb:"_uuid"`
	Name         string            `ovsdb:"name"`
	Ports        []string          `ovsdb:"ports"`
	Controllers  []string          `ovsdb:"controller"`
	Mirrors      []string          `ovsdb:"mirrors"`
	NetFlow      *string           `ovsdb:"netflow"`
	SFlow        *string           `ovsdb:"sflow"`
	IPFIX        *string           `ovsdb:"ipfix"`
	FlowTables   map[int]string    `ovsdb:"flow_tables"`
	AutoAttach   *string           `ovsdb:"auto_attach"`
	FailMode     *string           `ovsdb:"fail_mode"`
	DatapathType string            `ovsdb:"datapath_type"`
	ExternalIDs  map[string]string `ovsdb:"external_ids"`
	OtherConfig  map[string]string `ovsdb:"other_config"`
}

// OVSPort is the Open_vSwitch Port model.
type OVSPort struct {
	UUID        string            `ovsdb:"_uuid"`
	Name        string            `ovsdb:"name"`
	Interfaces  []string          `ovsdb:"interfaces"`
	QoS         *string           `ovsdb:"qos"`
	ExternalIDs map[string]string `ovsdb:"external_ids"`
	OtherConfig map[string]string `ovsdb:"other_config"`
}

// OVSInterface is the Open_vSwitch Interface model.
type OVSInterface struct {
	UUID        string            `ovsdb:"_uuid"`
	Name        string            `ovsdb:"name"`
	Type        string            `ovsdb:"type"`
	Options     map[string]string `ovsdb:"options"`
	ExternalIDs map[string]string `ovsdb:"external_ids"`
	OtherConfig map[string]string `ovsdb:"other_config"`
}

// OVSController is the Open_vSwitch Controller model.
type OVSController struct {
	UUID        string            `ovsdb:"_uuid"`
	Target      string            `ovsdb:"target"`
	ExternalIDs map[string]string `ovsdb:"external_ids"`
	OtherConfig map[string]string `ovsdb:"other_config"`
}

// OVSManager is the Open_vSwitch Manager model.
type OVSManager struct {
	UUID        string            `ovsdb:"_uuid"`
	Target      string            `ovsdb:"target"`
	ExternalIDs map[string]string `ovsdb:"external_ids"`
	OtherConfig map[string]string `ovsdb:"other_config"`
}

// OVSMirror is the Open_vSwitch Mirror model.
type OVSMirror struct {
	UUID          string            `ovsdb:"_uuid"`
	Name          string            `ovsdb:"name"`
	SelectAll     bool              `ovsdb:"select_all"`
	SelectSrcPort []string          `ovsdb:"select_src_port"`
	SelectDstPort []string          `ovsdb:"select_dst_port"`
	OutputPort    *string           `ovsdb:"output_port"`
	ExternalIDs   map[string]string `ovsdb:"external_ids"`
}

// OVSQoS is the Open_vSwitch QoS model.
type OVSQoS struct {
	UUID        string            `ovsdb:"_uuid"`
	Type        string            `ovsdb:"type"`
	Queues      map[int]string    `ovsdb:"queues"`
	ExternalIDs map[string]string `ovsdb:"external_ids"`
	OtherConfig map[string]string `ovsdb:"other_config"`
}

// OVSQueue is the Open_vSwitch Queue model.
type OVSQueue struct {
	UUID        string            `ovsdb:"_uuid"`
	ExternalIDs map[string]string `ovsdb:"external_ids"`
	OtherConfig map[string]string `ovsdb:"other_config"`
}

// OVSFlowTable is the Open_vSwitch Flow_Table model.
type OVSFlowTable struct {
	UUID        string            `ovsdb:"_uuid"`
	Name        string            `ovsdb:"name"`
	ExternalIDs map[string]string `ovsdb:"external_ids"`
}

// OVSNetFlow is the Open_vSwitch NetFlow model.
type OVSNetFlow struct {
	UUID          string            `ovsdb:"_uuid"`
	Targets       []string          `ovsdb:"targets"`
	EngineType    int               `ovsdb:"engine_type"`
	EngineID      int               `ovsdb:"engine_id"`
	ActiveTimeout int               `ovsdb:"active_timeout"`
	ExternalIDs   map[string]string `ovsdb:"external_ids"`
}

// OVSSFlow is the Open_vSwitch sFlow model.
type OVSSFlow struct {
	UUID        string            `ovsdb:"_uuid"`
	Agent       string            `ovsdb:"agent"`
	Targets     []string          `ovsdb:"targets"`
	Header      int               `ovsdb:"header"`
	Sampling    int               `ovsdb:"sampling"`
	Polling     int               `ovsdb:"polling"`
	ExternalIDs map[string]string `ovsdb:"external_ids"`
}

// OVSIPFIX is the Open_vSwitch IPFIX model.
type OVSIPFIX struct {
	UUID        string            `ovsdb:"_uuid"`
	Targets     []string          `ovsdb:"targets"`
	Sampling    int               `ovsdb:"sampling"`
	ExternalIDs map[string]string `ovsdb:"external_ids"`
	OtherConfig map[string]string `ovsdb:"other_config"`
}

// OVSSSL is the Open_vSwitch SSL model.
type OVSSSL struct {
	UUID            string            `ovsdb:"_uuid"`
	PrivateKey      string            `ovsdb:"private_key"`
	Certificate     string            `ovsdb:"certificate"`
	CACert          string            `ovsdb:"ca_cert"`
	BootstrapCACert bool              `ovsdb:"bootstrap_ca_cert"`
	ExternalIDs     map[string]string `ovsdb:"external_ids"`
}

// OVSAutoAttach is the Open_vSwitch AutoAttach model.
type OVSAutoAttach struct {
	UUID              string            `ovsdb:"_uuid"`
	SystemName        string            `ovsdb:"system_name"`
	SystemDescription string            `ovsdb:"system_description"`
	Mappings          map[int]int       `ovsdb:"mappings"`
	ExternalIDs       map[string]string `ovsdb:"external_ids"`
}

func nbDBModel() (libmodel.ClientDBModel, error) {
	db, err := libmodel.NewClientDBModel(dbOVNNorthbound, map[string]libmodel.Model{
		tableLogicalSwitch:     &LogicalSwitch{},
		tableLogicalSwitchPort: &LogicalSwitchPort{},
		tableLogicalRouter:     &LogicalRouter{},
		tableLogicalRouterPort: &LogicalRouterPort{},
		tableACL:               &ACL{},
		tableNAT:               &NAT{},
		tableLoadBalancer:      &LoadBalancer{},
		tableDHCPOptions:       &DHCPOptions{},
		tableDNS:               &DNS{},
		tableQoS:               &QoS{},
		tableMeter:             &Meter{},
		tableMeterBand:         &MeterBand{},
		tablePortGroup:         &PortGroup{},
		tableAddressSet:        &AddressSet{},
		tableGatewayChassis:    &GatewayChassis{},
		tableHAChassis:         &HAChassis{},
		tableHAChassisGroup:    &HAChassisGroup{},
		tableBFD:               &BFD{},
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
		tableLogicalRouter: {
			{Columns: []libmodel.ColumnKey{{Column: colName}}},
		},
		tableLogicalRouterPort: {
			{Columns: []libmodel.ColumnKey{{Column: colName}}},
		},
		tableLoadBalancer: {
			{Columns: []libmodel.ColumnKey{{Column: colName}}},
		},
		tableDHCPOptions: {
			{Columns: []libmodel.ColumnKey{{Column: colCIDR}}},
		},
		tableMeter: {
			{Columns: []libmodel.ColumnKey{{Column: colName}}},
		},
		tablePortGroup: {
			{Columns: []libmodel.ColumnKey{{Column: colName}}},
		},
		tableAddressSet: {
			{Columns: []libmodel.ColumnKey{{Column: colName}}},
		},
		tableGatewayChassis: {
			{Columns: []libmodel.ColumnKey{{Column: colName}}},
		},
		tableHAChassis: {
			{Columns: []libmodel.ColumnKey{{Column: colChassisName}}},
		},
		tableHAChassisGroup: {
			{Columns: []libmodel.ColumnKey{{Column: colName}}},
		},
		tableBFD: {
			{Columns: []libmodel.ColumnKey{{Column: colLogicalPort}, {Column: colDstIP}}},
		},
	})
	return db, nil
}

func sbDBModel() (libmodel.ClientDBModel, error) {
	db, err := libmodel.NewClientDBModel(dbOVNSouthbound, map[string]libmodel.Model{
		tableChassis:         &SBChassis{},
		tablePortBinding:     &SBPortBinding{},
		tableDatapathBinding: &SBDatapathBinding{},
		tableLogicalFlow:     &SBLogicalFlow{},
		tableMACBinding:      &SBMACBinding{},
		tableFDB:             &SBFDB{},
		tableMulticastGroup:  &SBMulticastGroup{},
		tableServiceMonitor:  &SBServiceMonitor{},
		tableRBACRole:        &SBRBACRole{},
		tableRBACPermission:  &SBRBACPermission{},
		tableMeter:           &SBMeter{},
		tableMeterBand:       &SBMeterBand{},
		tableDNS:             &SBDNS{},
		tableBFD:             &SBBFD{},
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
		tableDatapathBinding: {
			{Columns: []libmodel.ColumnKey{{Column: colTunnelKey}}},
		},
		tableMACBinding: {
			{Columns: []libmodel.ColumnKey{{Column: colLogicalPort}, {Column: colIP}}},
		},
		tableFDB: {
			{Columns: []libmodel.ColumnKey{{Column: colMAC}, {Column: colDPKey}}},
		},
		tableMulticastGroup: {
			{Columns: []libmodel.ColumnKey{{Column: colDatapath}, {Column: colTunnelKey}}},
			{Columns: []libmodel.ColumnKey{{Column: colDatapath}, {Column: colName}}},
		},
		tableServiceMonitor: {
			{Columns: []libmodel.ColumnKey{{Column: colLogicalPort}, {Column: colIP}, {Column: colPort}, {Column: colProtocol}}},
		},
		tableRBACRole: {
			{Columns: []libmodel.ColumnKey{{Column: colName}}},
		},
		tableMeter: {
			{Columns: []libmodel.ColumnKey{{Column: colName}}},
		},
		tableBFD: {
			{Columns: []libmodel.ColumnKey{{Column: colLogicalPort}, {Column: colDstIP}, {Column: colSrcPort}, {Column: colDisc}}},
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
		tableController:  &OVSController{},
		tableManager:     &OVSManager{},
		tableMirror:      &OVSMirror{},
		tableQoS:         &OVSQoS{},
		tableQueue:       &OVSQueue{},
		tableFlowTable:   &OVSFlowTable{},
		tableNetFlow:     &OVSNetFlow{},
		tableSFlow:       &OVSSFlow{},
		tableIPFIX:       &OVSIPFIX{},
		tableSSL:         &OVSSSL{},
		tableAutoAttach:  &OVSAutoAttach{},
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
		tableController: {
			{Columns: []libmodel.ColumnKey{{Column: colTarget}}},
		},
		tableManager: {
			{Columns: []libmodel.ColumnKey{{Column: colTarget}}},
		},
		tableMirror: {
			{Columns: []libmodel.ColumnKey{{Column: colName}}},
		},
		tableFlowTable: {
			{Columns: []libmodel.ColumnKey{{Column: colName}}},
		},
		tableAutoAttach: {
			{Columns: []libmodel.ColumnKey{{Column: colSystemName}}},
		},
	})
	return db, nil
}

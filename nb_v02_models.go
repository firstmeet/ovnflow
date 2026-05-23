package ovnflow

// LogicalRouterRef identifies one Logical_Router row by name.
type LogicalRouterRef struct {
	client *NBClient
	name   string
}

// LogicalRouterBuilder builds Logical_Router operations.
type LogicalRouterBuilder struct {
	once          useOnce
	client        *NBClient
	name          string
	mode          nbMode
	ports         []string
	nat           []string
	loadBalancers []string
	options       map[string]string
	externalIDs   map[string]string
}

// LogicalRouterPortRef identifies one Logical_Router_Port row by name.
type LogicalRouterPortRef struct {
	client *NBClient
	name   string
}

// LogicalRouterPortBuilder builds Logical_Router_Port operations.
type LogicalRouterPortBuilder struct {
	once           useOnce
	client         *NBClient
	name           string
	mode           nbMode
	mac            string
	networks       []string
	gatewayChassis []string
	haChassisGroup string
	peer           string
	enabled        *bool
	ipv6Prefixes   []string
	ipv6RAConfigs  map[string]string
	options        map[string]string
	externalIDs    map[string]string
}

// ACLRef identifies an ACL row by direction, priority, match, and optional name.
type ACLRef struct {
	client    *NBClient
	name      string
	direction string
	priority  int
	match     string
}

// ACLBuilder builds ACL operations.
type ACLBuilder struct {
	once        useOnce
	client      *NBClient
	name        string
	direction   string
	priority    int
	match       string
	mode        nbMode
	action      string
	log         *bool
	meter       string
	severity    string
	label       *int
	tier        *int
	options     map[string]string
	externalIDs map[string]string
}

// NATRef identifies a NAT row by type and logical_ip.
type NATRef struct {
	client    *NBClient
	name      string
	kind      string
	logicalIP string
}

// NATBuilder builds NAT operations.
type NATBuilder struct {
	once              useOnce
	client            *NBClient
	name              string
	kind              string
	logicalIP         string
	mode              nbMode
	externalIP        string
	logicalPort       string
	externalMAC       string
	externalPortRange string
	gatewayPort       string
	allowedExtIPs     string
	exemptedExtIPs    string
	match             string
	priority          *int
	options           map[string]string
	externalIDs       map[string]string
}

// LoadBalancerRef identifies one Load_Balancer row by name.
type LoadBalancerRef struct {
	client *NBClient
	name   string
}

// LoadBalancerBuilder builds Load_Balancer operations.
type LoadBalancerBuilder struct {
	once            useOnce
	client          *NBClient
	name            string
	mode            nbMode
	vips            map[string]string
	protocol        string
	selectionFields []string
	ipPortMappings  map[string]string
	healthChecks    []string
	options         map[string]string
	externalIDs     map[string]string
}

// DHCPOptionsRef identifies one DHCP_Options row by cidr.
type DHCPOptionsRef struct {
	client *NBClient
	cidr   string
}

// DHCPOptionsBuilder builds DHCP_Options operations.
type DHCPOptionsBuilder struct {
	once        useOnce
	client      *NBClient
	cidr        string
	mode        nbMode
	options     map[string]string
	externalIDs map[string]string
}

// DNSRef identifies one DNS row by external_ids name when supplied.
type DNSRef struct {
	client *NBClient
	name   string
}

// DNSBuilder builds DNS operations.
type DNSBuilder struct {
	once        useOnce
	client      *NBClient
	name        string
	mode        nbMode
	records     map[string]string
	options     map[string]string
	externalIDs map[string]string
}

// QoSRef identifies one QoS row by direction, priority, and match.
type QoSRef struct {
	client    *NBClient
	name      string
	direction string
	priority  int
	match     string
}

// QoSBuilder builds QoS operations.
type QoSBuilder struct {
	once        useOnce
	client      *NBClient
	name        string
	direction   string
	priority    int
	match       string
	mode        nbMode
	action      map[string]int
	bandwidth   map[string]int
	externalIDs map[string]string
}

// MeterRef identifies one Meter row by name.
type MeterRef struct {
	client *NBClient
	name   string
}

// MeterBuilder builds Meter operations.
type MeterBuilder struct {
	once        useOnce
	client      *NBClient
	name        string
	mode        nbMode
	unit        string
	bands       []string
	fair        *bool
	externalIDs map[string]string
}

// MeterBandRef identifies Meter_Band rows by an SDK supplied external ID.
type MeterBandRef struct {
	client *NBClient
	name   string
}

// MeterBandBuilder builds Meter_Band operations.
type MeterBandBuilder struct {
	once        useOnce
	client      *NBClient
	name        string
	mode        nbMode
	action      string
	rate        int
	burstSize   *int
	externalIDs map[string]string
}

// PortGroupRef identifies one Port_Group row by name.
type PortGroupRef struct {
	client *NBClient
	name   string
}

// PortGroupBuilder builds Port_Group operations.
type PortGroupBuilder struct {
	once        useOnce
	client      *NBClient
	name        string
	mode        nbMode
	ports       []string
	acls        []string
	externalIDs map[string]string
}

// AddressSetRef identifies one Address_Set row by name.
type AddressSetRef struct {
	client *NBClient
	name   string
}

// AddressSetBuilder builds Address_Set operations.
type AddressSetBuilder struct {
	once        useOnce
	client      *NBClient
	name        string
	mode        nbMode
	addresses   []string
	externalIDs map[string]string
}

// GatewayChassisRef identifies one Gateway_Chassis row by name.
type GatewayChassisRef struct {
	client *NBClient
	name   string
}

// GatewayChassisBuilder builds Gateway_Chassis operations.
type GatewayChassisBuilder struct {
	once        useOnce
	client      *NBClient
	name        string
	mode        nbMode
	chassisName string
	priority    *int
	options     map[string]string
	externalIDs map[string]string
}

// HAChassisRef identifies one HA_Chassis row by chassis_name.
type HAChassisRef struct {
	client      *NBClient
	chassisName string
}

// HAChassisBuilder builds HA_Chassis operations.
type HAChassisBuilder struct {
	once        useOnce
	client      *NBClient
	chassisName string
	mode        nbMode
	priority    *int
	externalIDs map[string]string
}

// HAChassisGroupRef identifies one HA_Chassis_Group row by name.
type HAChassisGroupRef struct {
	client *NBClient
	name   string
}

// HAChassisGroupBuilder builds HA_Chassis_Group operations.
type HAChassisGroupBuilder struct {
	once        useOnce
	client      *NBClient
	name        string
	mode        nbMode
	haChassis   []string
	externalIDs map[string]string
}

// BFDRef identifies one BFD row by logical_port and dst_ip.
type BFDRef struct {
	client      *NBClient
	logicalPort string
	dstIP       string
}

// BFDBuilder builds BFD operations.
type BFDBuilder struct {
	once        useOnce
	client      *NBClient
	logicalPort string
	dstIP       string
	mode        nbMode
	minTx       *int
	minRx       *int
	detectMult  *int
	status      string
	options     map[string]string
	externalIDs map[string]string
}

package ovnflow

import (
	"context"
	"net"

	libovsdb "github.com/ovn-kubernetes/libovsdb/ovsdb"
)

const dnsNameExternalID = "ovnflow.name"

func (n *NBClient) LogicalRouter(name string) *LogicalRouterRef {
	return &LogicalRouterRef{client: n, name: name}
}

func (n *NBClient) GetLogicalRouter(ctx context.Context, name string) (*LogicalRouter, error) {
	rows, err := n.selectRows(ctx, tableLogicalRouter, conditionName(name), nbLogicalRouterColumns(), name)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, wrap(ErrorNotFound, dbOVNNorthbound, tableLogicalRouter, "get", name, "logical router not found", nil)
	}
	return logicalRouterFromRow(rows[0]), nil
}

func (r *LogicalRouterRef) Create() *LogicalRouterBuilder {
	return newLogicalRouterBuilder(r.client, r.name, nbModeCreate)
}

func (r *LogicalRouterRef) Ensure() *LogicalRouterBuilder {
	return newLogicalRouterBuilder(r.client, r.name, nbModeEnsure)
}

func (r *LogicalRouterRef) Delete() *LogicalRouterBuilder {
	return newLogicalRouterBuilder(r.client, r.name, nbModeDelete)
}

func newLogicalRouterBuilder(client *NBClient, name string, mode nbMode) *LogicalRouterBuilder {
	return &LogicalRouterBuilder{
		client:      client,
		name:        name,
		mode:        mode,
		options:     map[string]string{},
		externalIDs: map[string]string{},
	}
}

func (b *LogicalRouterBuilder) WithPortUUID(uuid string) *LogicalRouterBuilder {
	b.ports = append(b.ports, uuid)
	return b
}

func (b *LogicalRouterBuilder) WithNATUUID(uuid string) *LogicalRouterBuilder {
	b.nat = append(b.nat, uuid)
	return b
}

func (b *LogicalRouterBuilder) WithLoadBalancerUUID(uuid string) *LogicalRouterBuilder {
	b.loadBalancers = append(b.loadBalancers, uuid)
	return b
}

func (b *LogicalRouterBuilder) WithOption(key, value string) *LogicalRouterBuilder {
	b.options[key] = value
	return b
}

func (b *LogicalRouterBuilder) WithExternalID(key, value string) *LogicalRouterBuilder {
	b.externalIDs[key] = value
	return b
}

func (b *LogicalRouterBuilder) Execute(ctx context.Context) error {
	if !b.once.mark() {
		return nbBuilderUsed(tableLogicalRouter, string(b.mode), b.name)
	}
	if err := b.validate(); err != nil {
		return err
	}
	row := libovsdb.Row{colName: b.name}
	nbSetUUIDSet(row, colPorts, b.ports)
	nbSetUUIDSet(row, colNAT, b.nat)
	nbSetUUIDSet(row, colLoadBalancer, b.loadBalancers)
	setRowMap(row, colOptions, b.options)
	setRowMap(row, colExternalIDs, b.externalIDs)
	mutations := []libovsdb.Mutation{}
	nbAppendUUIDSetMutation(&mutations, colPorts, b.ports)
	nbAppendUUIDSetMutation(&mutations, colNAT, b.nat)
	nbAppendUUIDSetMutation(&mutations, colLoadBalancer, b.loadBalancers)
	nbAppendMapMutation(&mutations, colOptions, b.options)
	nbAppendMapMutation(&mutations, colExternalIDs, b.externalIDs)
	return b.client.executeNamed(ctx, tableLogicalRouter, b.name, b.mode, row, mutations)
}

func (b *LogicalRouterBuilder) validate() error {
	if err := validateName("logical router", b.name); err != nil {
		return err
	}
	return nbValidateStringMaps(b.options, b.externalIDs)
}

func (n *NBClient) LogicalRouterPort(name string) *LogicalRouterPortRef {
	return &LogicalRouterPortRef{client: n, name: name}
}

func (n *NBClient) GetLogicalRouterPort(ctx context.Context, name string) (*LogicalRouterPort, error) {
	rows, err := n.selectRows(ctx, tableLogicalRouterPort, conditionName(name), nbLogicalRouterPortColumns(), name)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, wrap(ErrorNotFound, dbOVNNorthbound, tableLogicalRouterPort, "get", name, "logical router port not found", nil)
	}
	return logicalRouterPortFromRow(rows[0]), nil
}

func (r *LogicalRouterPortRef) Create() *LogicalRouterPortBuilder {
	return newLogicalRouterPortBuilder(r.client, r.name, nbModeCreate)
}

func (r *LogicalRouterPortRef) Ensure() *LogicalRouterPortBuilder {
	return newLogicalRouterPortBuilder(r.client, r.name, nbModeEnsure)
}

func (r *LogicalRouterPortRef) Delete() *LogicalRouterPortBuilder {
	return newLogicalRouterPortBuilder(r.client, r.name, nbModeDelete)
}

func newLogicalRouterPortBuilder(client *NBClient, name string, mode nbMode) *LogicalRouterPortBuilder {
	return &LogicalRouterPortBuilder{
		client:        client,
		name:          name,
		mode:          mode,
		ipv6RAConfigs: map[string]string{},
		options:       map[string]string{},
		externalIDs:   map[string]string{},
	}
}

func (b *LogicalRouterPortBuilder) WithMAC(mac string) *LogicalRouterPortBuilder {
	b.mac = mac
	return b
}

func (b *LogicalRouterPortBuilder) WithNetwork(network string) *LogicalRouterPortBuilder {
	b.networks = append(b.networks, network)
	return b
}

func (b *LogicalRouterPortBuilder) WithNetworks(networks ...string) *LogicalRouterPortBuilder {
	b.networks = append(b.networks, networks...)
	return b
}

func (b *LogicalRouterPortBuilder) WithGatewayChassisUUID(uuid string) *LogicalRouterPortBuilder {
	b.gatewayChassis = append(b.gatewayChassis, uuid)
	return b
}

func (b *LogicalRouterPortBuilder) WithHAChassisGroupUUID(uuid string) *LogicalRouterPortBuilder {
	b.haChassisGroup = uuid
	return b
}

func (b *LogicalRouterPortBuilder) WithPeer(peer string) *LogicalRouterPortBuilder {
	b.peer = peer
	return b
}

func (b *LogicalRouterPortBuilder) WithEnabled(enabled bool) *LogicalRouterPortBuilder {
	b.enabled = &enabled
	return b
}

func (b *LogicalRouterPortBuilder) WithIPv6Prefix(prefix string) *LogicalRouterPortBuilder {
	b.ipv6Prefixes = append(b.ipv6Prefixes, prefix)
	return b
}

func (b *LogicalRouterPortBuilder) WithIPv6RAConfig(key, value string) *LogicalRouterPortBuilder {
	b.ipv6RAConfigs[key] = value
	return b
}

func (b *LogicalRouterPortBuilder) WithOption(key, value string) *LogicalRouterPortBuilder {
	b.options[key] = value
	return b
}

func (b *LogicalRouterPortBuilder) WithExternalID(key, value string) *LogicalRouterPortBuilder {
	b.externalIDs[key] = value
	return b
}

func (b *LogicalRouterPortBuilder) Execute(ctx context.Context) error {
	if !b.once.mark() {
		return nbBuilderUsed(tableLogicalRouterPort, string(b.mode), b.name)
	}
	if err := b.validate(); err != nil {
		return err
	}
	row := libovsdb.Row{colName: b.name}
	if b.mac != "" {
		row[colMAC] = b.mac
	}
	nbSetStringSet(row, colNetworks, b.networks)
	nbSetUUIDSet(row, colGatewayChassis, b.gatewayChassis)
	nbSetOptionalUUID(row, colHAChassisGroup, b.haChassisGroup)
	nbSetOptionalString(row, colPeer, b.peer)
	if b.enabled != nil {
		row[colEnabled] = ovsSet(*b.enabled)
	}
	nbSetStringSet(row, colIPv6Prefix, b.ipv6Prefixes)
	setRowMap(row, colIPv6RAConfigs, b.ipv6RAConfigs)
	setRowMap(row, colOptions, b.options)
	setRowMap(row, colExternalIDs, b.externalIDs)
	mutations := []libovsdb.Mutation{}
	nbAppendUUIDSetMutation(&mutations, colGatewayChassis, b.gatewayChassis)
	nbAppendStringSetMutation(&mutations, colNetworks, b.networks)
	nbAppendStringSetMutation(&mutations, colIPv6Prefix, b.ipv6Prefixes)
	nbAppendMapMutation(&mutations, colIPv6RAConfigs, b.ipv6RAConfigs)
	nbAppendMapMutation(&mutations, colOptions, b.options)
	nbAppendMapMutation(&mutations, colExternalIDs, b.externalIDs)
	return b.client.executeNamed(ctx, tableLogicalRouterPort, b.name, b.mode, row, mutations)
}

func (b *LogicalRouterPortBuilder) validate() error {
	if err := validateName("logical router port", b.name); err != nil {
		return err
	}
	if b.mac != "" {
		if _, err := net.ParseMAC(b.mac); err != nil {
			return wrap(ErrorValidation, dbOVNNorthbound, tableLogicalRouterPort, string(b.mode), b.name, "invalid mac", err)
		}
	}
	for _, network := range b.networks {
		if _, _, err := net.ParseCIDR(network); err != nil {
			return wrap(ErrorValidation, dbOVNNorthbound, tableLogicalRouterPort, string(b.mode), b.name, "invalid network", err)
		}
	}
	return nbValidateStringMaps(b.ipv6RAConfigs, b.options, b.externalIDs)
}

func (n *NBClient) ACL(name string) *ACLRef {
	return &ACLRef{client: n, name: name}
}

func (n *NBClient) ACLByMatch(direction string, priority int, match string) *ACLRef {
	return &ACLRef{client: n, direction: direction, priority: priority, match: match}
}

func (n *NBClient) GetACL(ctx context.Context, direction string, priority int, match string) (*ACL, error) {
	ref := n.ACLByMatch(direction, priority, match)
	rows, err := n.selectRows(ctx, tableACL, ref.conditions(), nbACLColumns(), match)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, wrap(ErrorNotFound, dbOVNNorthbound, tableACL, "get", match, "acl not found", nil)
	}
	return aclFromRow(rows[0]), nil
}

func (r *ACLRef) Create() *ACLBuilder {
	return newACLBuilder(r, nbModeCreate)
}

func (r *ACLRef) Ensure() *ACLBuilder {
	return newACLBuilder(r, nbModeEnsure)
}

func (r *ACLRef) Delete() *ACLBuilder {
	return newACLBuilder(r, nbModeDelete)
}

func newACLBuilder(ref *ACLRef, mode nbMode) *ACLBuilder {
	return &ACLBuilder{
		client:      ref.client,
		name:        ref.name,
		direction:   ref.direction,
		priority:    ref.priority,
		match:       ref.match,
		mode:        mode,
		options:     map[string]string{},
		externalIDs: map[string]string{},
	}
}

func (b *ACLBuilder) WithDirection(direction string) *ACLBuilder {
	b.direction = direction
	return b
}

func (b *ACLBuilder) WithPriority(priority int) *ACLBuilder {
	b.priority = priority
	return b
}

func (b *ACLBuilder) WithMatch(match string) *ACLBuilder {
	b.match = match
	return b
}

func (b *ACLBuilder) WithAction(action string) *ACLBuilder {
	b.action = action
	return b
}

func (b *ACLBuilder) WithLog(log bool) *ACLBuilder {
	b.log = &log
	return b
}

func (b *ACLBuilder) WithMeter(meter string) *ACLBuilder {
	b.meter = meter
	return b
}

func (b *ACLBuilder) WithSeverity(severity string) *ACLBuilder {
	b.severity = severity
	return b
}

func (b *ACLBuilder) WithLabel(label int) *ACLBuilder {
	b.label = &label
	return b
}

func (b *ACLBuilder) WithTier(tier int) *ACLBuilder {
	b.tier = &tier
	return b
}

func (b *ACLBuilder) WithOption(key, value string) *ACLBuilder {
	b.options[key] = value
	return b
}

func (b *ACLBuilder) WithExternalID(key, value string) *ACLBuilder {
	b.externalIDs[key] = value
	return b
}

func (b *ACLBuilder) Execute(ctx context.Context) error {
	if !b.once.mark() {
		return nbBuilderUsed(tableACL, string(b.mode), b.match)
	}
	if err := b.validate(); err != nil {
		return err
	}
	row := libovsdb.Row{
		colPriority:  b.priority,
		colDirection: b.direction,
		colMatch:     b.match,
	}
	nbSetOptionalString(row, colName, b.name)
	if b.action != "" {
		row[colAction] = b.action
	}
	if b.log != nil {
		row[colLog] = *b.log
	}
	nbSetOptionalString(row, colMeter, b.meter)
	nbSetOptionalString(row, colSeverity, b.severity)
	if b.label != nil {
		row[colLabel] = *b.label
	}
	if b.tier != nil {
		row[colTier] = *b.tier
	}
	setRowMap(row, colOptions, b.options)
	setRowMap(row, colExternalIDs, b.externalIDs)
	mutations := []libovsdb.Mutation{}
	nbAppendMapMutation(&mutations, colOptions, b.options)
	nbAppendMapMutation(&mutations, colExternalIDs, b.externalIDs)
	return b.client.executeByConditions(ctx, tableACL, b.match, b.mode, b.conditions(), row, mutations)
}

func (b *ACLBuilder) validate() error {
	if b.direction == "" || b.match == "" {
		return wrap(ErrorValidation, dbOVNNorthbound, tableACL, string(b.mode), b.match, "direction and match are required", nil)
	}
	if b.priority < 0 || b.priority > 32767 {
		return wrap(ErrorValidation, dbOVNNorthbound, tableACL, string(b.mode), b.match, "priority must be between 0 and 32767", nil)
	}
	return nbValidateStringMaps(b.options, b.externalIDs)
}

func (b *ACLBuilder) conditions() []libovsdb.Condition {
	return []libovsdb.Condition{
		libovsdb.NewCondition(colDirection, libovsdb.ConditionEqual, b.direction),
		libovsdb.NewCondition(colPriority, libovsdb.ConditionEqual, b.priority),
		libovsdb.NewCondition(colMatch, libovsdb.ConditionEqual, b.match),
	}
}

func (r *ACLRef) conditions() []libovsdb.Condition {
	return []libovsdb.Condition{
		libovsdb.NewCondition(colDirection, libovsdb.ConditionEqual, r.direction),
		libovsdb.NewCondition(colPriority, libovsdb.ConditionEqual, r.priority),
		libovsdb.NewCondition(colMatch, libovsdb.ConditionEqual, r.match),
	}
}

func (n *NBClient) NAT(name string) *NATRef {
	return &NATRef{client: n, name: name}
}

func (n *NBClient) NATByLogicalIP(kind, logicalIP string) *NATRef {
	return &NATRef{client: n, kind: kind, logicalIP: logicalIP}
}

func (n *NBClient) GetNAT(ctx context.Context, kind, logicalIP string) (*NAT, error) {
	ref := n.NATByLogicalIP(kind, logicalIP)
	rows, err := n.selectRows(ctx, tableNAT, ref.conditions(), nbNATColumns(), logicalIP)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, wrap(ErrorNotFound, dbOVNNorthbound, tableNAT, "get", logicalIP, "nat not found", nil)
	}
	return natFromRow(rows[0]), nil
}

func (r *NATRef) Create() *NATBuilder {
	return newNATBuilder(r, nbModeCreate)
}

func (r *NATRef) Ensure() *NATBuilder {
	return newNATBuilder(r, nbModeEnsure)
}

func (r *NATRef) Delete() *NATBuilder {
	return newNATBuilder(r, nbModeDelete)
}

func newNATBuilder(ref *NATRef, mode nbMode) *NATBuilder {
	return &NATBuilder{
		client:      ref.client,
		name:        ref.name,
		kind:        ref.kind,
		logicalIP:   ref.logicalIP,
		mode:        mode,
		options:     map[string]string{},
		externalIDs: map[string]string{},
	}
}

func (b *NATBuilder) WithType(kind string) *NATBuilder {
	b.kind = kind
	return b
}

func (b *NATBuilder) WithLogicalIP(ip string) *NATBuilder {
	b.logicalIP = ip
	return b
}

func (b *NATBuilder) WithExternalIP(ip string) *NATBuilder {
	b.externalIP = ip
	return b
}

func (b *NATBuilder) WithLogicalPort(port string) *NATBuilder {
	b.logicalPort = port
	return b
}

func (b *NATBuilder) WithExternalMAC(mac string) *NATBuilder {
	b.externalMAC = mac
	return b
}

func (b *NATBuilder) WithExternalPortRange(portRange string) *NATBuilder {
	b.externalPortRange = portRange
	return b
}

func (b *NATBuilder) WithGatewayPortUUID(uuid string) *NATBuilder {
	b.gatewayPort = uuid
	return b
}

func (b *NATBuilder) WithAllowedExternalIPsUUID(uuid string) *NATBuilder {
	b.allowedExtIPs = uuid
	return b
}

func (b *NATBuilder) WithExemptedExternalIPsUUID(uuid string) *NATBuilder {
	b.exemptedExtIPs = uuid
	return b
}

func (b *NATBuilder) WithMatch(match string) *NATBuilder {
	b.match = match
	return b
}

func (b *NATBuilder) WithPriority(priority int) *NATBuilder {
	b.priority = &priority
	return b
}

func (b *NATBuilder) WithOption(key, value string) *NATBuilder {
	b.options[key] = value
	return b
}

func (b *NATBuilder) WithExternalID(key, value string) *NATBuilder {
	b.externalIDs[key] = value
	return b
}

func (b *NATBuilder) Execute(ctx context.Context) error {
	if !b.once.mark() {
		return nbBuilderUsed(tableNAT, string(b.mode), b.logicalIP)
	}
	if err := b.validate(); err != nil {
		return err
	}
	row := libovsdb.Row{
		colType:              b.kind,
		colLogicalIP:         b.logicalIP,
		colExternalPortRange: b.externalPortRange,
		colMatch:             b.match,
	}
	if b.externalIP != "" {
		row[colExternalIP] = b.externalIP
	}
	nbSetOptionalString(row, colLogicalPort, b.logicalPort)
	nbSetOptionalString(row, colExternalMAC, b.externalMAC)
	nbSetOptionalUUID(row, colGatewayPort, b.gatewayPort)
	nbSetOptionalUUID(row, colAllowedExtIPs, b.allowedExtIPs)
	nbSetOptionalUUID(row, colExemptedExtIPs, b.exemptedExtIPs)
	if b.priority != nil {
		row[colPriority] = *b.priority
	}
	setRowMap(row, colOptions, b.options)
	setRowMap(row, colExternalIDs, b.externalIDs)
	mutations := []libovsdb.Mutation{}
	nbAppendMapMutation(&mutations, colOptions, b.options)
	nbAppendMapMutation(&mutations, colExternalIDs, b.externalIDs)
	return b.client.executeByConditions(ctx, tableNAT, b.logicalIP, b.mode, b.conditions(), row, mutations)
}

func (b *NATBuilder) validate() error {
	if b.kind == "" || b.logicalIP == "" {
		return wrap(ErrorValidation, dbOVNNorthbound, tableNAT, string(b.mode), b.logicalIP, "type and logical_ip are required", nil)
	}
	if b.mode != nbModeDelete && b.externalIP == "" {
		return wrap(ErrorValidation, dbOVNNorthbound, tableNAT, string(b.mode), b.logicalIP, "external_ip is required", nil)
	}
	if b.externalMAC != "" {
		if _, err := net.ParseMAC(b.externalMAC); err != nil {
			return wrap(ErrorValidation, dbOVNNorthbound, tableNAT, string(b.mode), b.logicalIP, "invalid external mac", err)
		}
	}
	if b.priority != nil && (*b.priority < 0 || *b.priority > 32767) {
		return wrap(ErrorValidation, dbOVNNorthbound, tableNAT, string(b.mode), b.logicalIP, "priority must be between 0 and 32767", nil)
	}
	return nbValidateStringMaps(b.options, b.externalIDs)
}

func (b *NATBuilder) conditions() []libovsdb.Condition {
	return []libovsdb.Condition{
		libovsdb.NewCondition(colType, libovsdb.ConditionEqual, b.kind),
		libovsdb.NewCondition(colLogicalIP, libovsdb.ConditionEqual, b.logicalIP),
	}
}

func (r *NATRef) conditions() []libovsdb.Condition {
	return []libovsdb.Condition{
		libovsdb.NewCondition(colType, libovsdb.ConditionEqual, r.kind),
		libovsdb.NewCondition(colLogicalIP, libovsdb.ConditionEqual, r.logicalIP),
	}
}

func (n *NBClient) LoadBalancer(name string) *LoadBalancerRef {
	return &LoadBalancerRef{client: n, name: name}
}

func (n *NBClient) GetLoadBalancer(ctx context.Context, name string) (*LoadBalancer, error) {
	rows, err := n.selectRows(ctx, tableLoadBalancer, conditionName(name), nbLoadBalancerColumns(), name)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, wrap(ErrorNotFound, dbOVNNorthbound, tableLoadBalancer, "get", name, "load balancer not found", nil)
	}
	return loadBalancerFromRow(rows[0]), nil
}

func (r *LoadBalancerRef) Create() *LoadBalancerBuilder {
	return newLoadBalancerBuilder(r.client, r.name, nbModeCreate)
}

func (r *LoadBalancerRef) Ensure() *LoadBalancerBuilder {
	return newLoadBalancerBuilder(r.client, r.name, nbModeEnsure)
}

func (r *LoadBalancerRef) Delete() *LoadBalancerBuilder {
	return newLoadBalancerBuilder(r.client, r.name, nbModeDelete)
}

func newLoadBalancerBuilder(client *NBClient, name string, mode nbMode) *LoadBalancerBuilder {
	return &LoadBalancerBuilder{
		client:         client,
		name:           name,
		mode:           mode,
		vips:           map[string]string{},
		ipPortMappings: map[string]string{},
		options:        map[string]string{},
		externalIDs:    map[string]string{},
	}
}

func (b *LoadBalancerBuilder) WithVIP(vip, backends string) *LoadBalancerBuilder {
	b.vips[vip] = backends
	return b
}

func (b *LoadBalancerBuilder) WithProtocol(protocol string) *LoadBalancerBuilder {
	b.protocol = protocol
	return b
}

func (b *LoadBalancerBuilder) WithSelectionField(field string) *LoadBalancerBuilder {
	b.selectionFields = append(b.selectionFields, field)
	return b
}

func (b *LoadBalancerBuilder) WithIPPortMapping(endpoint, mapping string) *LoadBalancerBuilder {
	b.ipPortMappings[endpoint] = mapping
	return b
}

func (b *LoadBalancerBuilder) WithHealthCheckUUID(uuid string) *LoadBalancerBuilder {
	b.healthChecks = append(b.healthChecks, uuid)
	return b
}

func (b *LoadBalancerBuilder) WithOption(key, value string) *LoadBalancerBuilder {
	b.options[key] = value
	return b
}

func (b *LoadBalancerBuilder) WithExternalID(key, value string) *LoadBalancerBuilder {
	b.externalIDs[key] = value
	return b
}

func (b *LoadBalancerBuilder) Execute(ctx context.Context) error {
	if !b.once.mark() {
		return nbBuilderUsed(tableLoadBalancer, string(b.mode), b.name)
	}
	if err := b.validate(); err != nil {
		return err
	}
	row := libovsdb.Row{colName: b.name}
	setRowMap(row, colVIPs, b.vips)
	nbSetOptionalString(row, colProtocol, b.protocol)
	nbSetStringSet(row, colSelectionFields, b.selectionFields)
	setRowMap(row, colIPPortMappings, b.ipPortMappings)
	nbSetUUIDSet(row, "health_check", b.healthChecks)
	setRowMap(row, colOptions, b.options)
	setRowMap(row, colExternalIDs, b.externalIDs)
	mutations := []libovsdb.Mutation{}
	nbAppendMapMutation(&mutations, colVIPs, b.vips)
	nbAppendStringSetMutation(&mutations, colSelectionFields, b.selectionFields)
	nbAppendMapMutation(&mutations, colIPPortMappings, b.ipPortMappings)
	nbAppendUUIDSetMutation(&mutations, "health_check", b.healthChecks)
	nbAppendMapMutation(&mutations, colOptions, b.options)
	nbAppendMapMutation(&mutations, colExternalIDs, b.externalIDs)
	return b.client.executeNamed(ctx, tableLoadBalancer, b.name, b.mode, row, mutations)
}

func (b *LoadBalancerBuilder) validate() error {
	if err := validateName("load balancer", b.name); err != nil {
		return err
	}
	return nbValidateStringMaps(b.vips, b.ipPortMappings, b.options, b.externalIDs)
}

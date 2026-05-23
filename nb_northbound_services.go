package ovnflow

import (
	"context"
	"net"

	libovsdb "github.com/ovn-kubernetes/libovsdb/ovsdb"
)

func (n *NBClient) DHCPOptions(cidr string) *DHCPOptionsRef {
	return &DHCPOptionsRef{client: n, cidr: cidr}
}

func (n *NBClient) GetDHCPOptions(ctx context.Context, cidr string) (*DHCPOptions, error) {
	rows, err := n.selectRows(ctx, tableDHCPOptions, []libovsdb.Condition{libovsdb.NewCondition(colCIDR, libovsdb.ConditionEqual, cidr)}, nbDHCPOptionsColumns(), cidr)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, wrap(ErrorNotFound, dbOVNNorthbound, tableDHCPOptions, "get", cidr, "dhcp options not found", nil)
	}
	return dhcpOptionsFromRow(rows[0]), nil
}

func (r *DHCPOptionsRef) Create() *DHCPOptionsBuilder {
	return newDHCPOptionsBuilder(r.client, r.cidr, nbModeCreate)
}

func (r *DHCPOptionsRef) Ensure() *DHCPOptionsBuilder {
	return newDHCPOptionsBuilder(r.client, r.cidr, nbModeEnsure)
}

func (r *DHCPOptionsRef) Delete() *DHCPOptionsBuilder {
	return newDHCPOptionsBuilder(r.client, r.cidr, nbModeDelete)
}

func newDHCPOptionsBuilder(client *NBClient, cidr string, mode nbMode) *DHCPOptionsBuilder {
	return &DHCPOptionsBuilder{client: client, cidr: cidr, mode: mode, options: map[string]string{}, externalIDs: map[string]string{}}
}

func (b *DHCPOptionsBuilder) WithOption(key, value string) *DHCPOptionsBuilder {
	b.options[key] = value
	return b
}

func (b *DHCPOptionsBuilder) WithExternalID(key, value string) *DHCPOptionsBuilder {
	b.externalIDs[key] = value
	return b
}

func (b *DHCPOptionsBuilder) Execute(ctx context.Context) error {
	if !b.once.mark() {
		return nbBuilderUsed(tableDHCPOptions, string(b.mode), b.cidr)
	}
	if err := b.validate(); err != nil {
		return err
	}
	row := libovsdb.Row{colCIDR: b.cidr}
	setRowMap(row, colOptions, b.options)
	setRowMap(row, colExternalIDs, b.externalIDs)
	mutations := []libovsdb.Mutation{}
	nbAppendMapMutation(&mutations, colOptions, b.options)
	nbAppendMapMutation(&mutations, colExternalIDs, b.externalIDs)
	conditions := []libovsdb.Condition{libovsdb.NewCondition(colCIDR, libovsdb.ConditionEqual, b.cidr)}
	return b.client.executeByConditions(ctx, tableDHCPOptions, b.cidr, b.mode, conditions, row, mutations)
}

func (b *DHCPOptionsBuilder) validate() error {
	if _, _, err := net.ParseCIDR(b.cidr); err != nil {
		return wrap(ErrorValidation, dbOVNNorthbound, tableDHCPOptions, string(b.mode), b.cidr, "invalid cidr", err)
	}
	return nbValidateStringMaps(b.options, b.externalIDs)
}

func (n *NBClient) DNS(name string) *DNSRef {
	return &DNSRef{client: n, name: name}
}

func (n *NBClient) GetDNS(ctx context.Context, name string) (*DNS, error) {
	rows, err := n.selectRows(ctx, tableDNS, []libovsdb.Condition{libovsdb.NewCondition(colExternalIDs, libovsdb.ConditionIncludes, ovsMap(map[string]string{dnsNameExternalID: name}))}, nbDNSColumns(), name)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, wrap(ErrorNotFound, dbOVNNorthbound, tableDNS, "get", name, "dns row not found", nil)
	}
	return dnsFromRow(rows[0]), nil
}

func (r *DNSRef) Create() *DNSBuilder {
	return newDNSBuilder(r.client, r.name, nbModeCreate)
}

func (r *DNSRef) Ensure() *DNSBuilder {
	return newDNSBuilder(r.client, r.name, nbModeEnsure)
}

func (r *DNSRef) Delete() *DNSBuilder {
	return newDNSBuilder(r.client, r.name, nbModeDelete)
}

func newDNSBuilder(client *NBClient, name string, mode nbMode) *DNSBuilder {
	return &DNSBuilder{client: client, name: name, mode: mode, records: map[string]string{}, options: map[string]string{}, externalIDs: map[string]string{}}
}

func (b *DNSBuilder) WithRecord(name, value string) *DNSBuilder {
	b.records[name] = value
	return b
}

func (b *DNSBuilder) WithOption(key, value string) *DNSBuilder {
	b.options[key] = value
	return b
}

func (b *DNSBuilder) WithExternalID(key, value string) *DNSBuilder {
	b.externalIDs[key] = value
	return b
}

func (b *DNSBuilder) Execute(ctx context.Context) error {
	if !b.once.mark() {
		return nbBuilderUsed(tableDNS, string(b.mode), b.name)
	}
	if err := b.validate(); err != nil {
		return err
	}
	if b.name != "" {
		b.externalIDs[dnsNameExternalID] = b.name
	}
	row := libovsdb.Row{}
	setRowMap(row, colRecords, b.records)
	setRowMap(row, colOptions, b.options)
	setRowMap(row, colExternalIDs, b.externalIDs)
	mutations := []libovsdb.Mutation{}
	nbAppendMapMutation(&mutations, colRecords, b.records)
	nbAppendMapMutation(&mutations, colOptions, b.options)
	nbAppendMapMutation(&mutations, colExternalIDs, b.externalIDs)
	return b.client.executeByConditions(ctx, tableDNS, b.name, b.mode, b.conditions(), row, mutations)
}

func (b *DNSBuilder) validate() error {
	if err := validateName("dns", b.name); err != nil {
		return err
	}
	return nbValidateStringMaps(b.records, b.options, b.externalIDs)
}

func (b *DNSBuilder) conditions() []libovsdb.Condition {
	return []libovsdb.Condition{libovsdb.NewCondition(colExternalIDs, libovsdb.ConditionIncludes, ovsMap(map[string]string{dnsNameExternalID: b.name}))}
}

func (n *NBClient) QoS(name string) *QoSRef {
	return &QoSRef{client: n, name: name}
}

func (n *NBClient) QoSByMatch(direction string, priority int, match string) *QoSRef {
	return &QoSRef{client: n, direction: direction, priority: priority, match: match}
}

func (n *NBClient) GetQoS(ctx context.Context, direction string, priority int, match string) (*QoS, error) {
	ref := n.QoSByMatch(direction, priority, match)
	rows, err := n.selectRows(ctx, tableQoS, ref.conditions(), nbQoSColumns(), match)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, wrap(ErrorNotFound, dbOVNNorthbound, tableQoS, "get", match, "qos row not found", nil)
	}
	return qosFromRow(rows[0]), nil
}

func (r *QoSRef) Create() *QoSBuilder {
	return newQoSBuilder(r, nbModeCreate)
}

func (r *QoSRef) Ensure() *QoSBuilder {
	return newQoSBuilder(r, nbModeEnsure)
}

func (r *QoSRef) Delete() *QoSBuilder {
	return newQoSBuilder(r, nbModeDelete)
}

func newQoSBuilder(ref *QoSRef, mode nbMode) *QoSBuilder {
	return &QoSBuilder{
		client:      ref.client,
		name:        ref.name,
		direction:   ref.direction,
		priority:    ref.priority,
		match:       ref.match,
		mode:        mode,
		action:      map[string]int{},
		bandwidth:   map[string]int{},
		externalIDs: map[string]string{},
	}
}

func (b *QoSBuilder) WithDirection(direction string) *QoSBuilder {
	b.direction = direction
	return b
}

func (b *QoSBuilder) WithPriority(priority int) *QoSBuilder {
	b.priority = priority
	return b
}

func (b *QoSBuilder) WithMatch(match string) *QoSBuilder {
	b.match = match
	return b
}

func (b *QoSBuilder) AttachToSwitch(name string) *QoSBuilder {
	b.switchName = name
	return b
}

func (b *QoSBuilder) WithDSCP(value int) *QoSBuilder {
	b.action["dscp"] = value
	return b
}

func (b *QoSBuilder) WithMark(value int) *QoSBuilder {
	b.action["mark"] = value
	return b
}

func (b *QoSBuilder) WithRate(rate int) *QoSBuilder {
	b.bandwidth["rate"] = rate
	return b
}

func (b *QoSBuilder) WithBurst(burst int) *QoSBuilder {
	b.bandwidth["burst"] = burst
	return b
}

func (b *QoSBuilder) WithExternalID(key, value string) *QoSBuilder {
	b.externalIDs[key] = value
	return b
}

func (b *QoSBuilder) Execute(ctx context.Context) error {
	if !b.once.mark() {
		return nbBuilderUsed(tableQoS, string(b.mode), b.match)
	}
	if err := b.validate(); err != nil {
		return err
	}
	row := libovsdb.Row{
		colPriority:  b.priority,
		colDirection: b.direction,
		colMatch:     b.match,
	}
	if len(b.action) > 0 {
		row[colAction] = nbIntMap(b.action)
	}
	if len(b.bandwidth) > 0 {
		row[colBandwidth] = nbIntMap(b.bandwidth)
	}
	setRowMap(row, colExternalIDs, b.externalIDs)
	mutations := []libovsdb.Mutation{}
	nbAppendMapMutation(&mutations, colExternalIDs, b.externalIDs)
	return b.client.executeByConditionsWithPrePostOps(ctx, tableQoS, b.match, b.mode, b.conditions(), row, mutations, nil, b.attachToSwitchOps)
}

func (b *QoSBuilder) validate() error {
	if b.direction == "" || b.match == "" {
		return wrap(ErrorValidation, dbOVNNorthbound, tableQoS, string(b.mode), b.match, "direction and match are required", nil)
	}
	if b.priority < 0 || b.priority > 32767 {
		return wrap(ErrorValidation, dbOVNNorthbound, tableQoS, string(b.mode), b.match, "priority must be between 0 and 32767", nil)
	}
	if b.switchName != "" {
		if err := validateName("logical switch", b.switchName); err != nil {
			return err
		}
		if err := b.client.db.schema.RequireColumns(tableLogicalSwitch, colQoSRules); err != nil {
			return err
		}
	}
	return nbValidateStringMaps(b.externalIDs)
}

func (b *QoSBuilder) attachToSwitchOps(qosUUID string) []libovsdb.Operation {
	if b.switchName == "" || b.mode == nbModeDelete || qosUUID == "" {
		return nil
	}
	return []libovsdb.Operation{{
		Op:    libovsdb.OperationMutate,
		Table: tableLogicalSwitch,
		Where: conditionName(b.switchName),
		Mutations: []libovsdb.Mutation{
			*libovsdb.NewMutation(colQoSRules, libovsdb.MutateOperationInsert, uuidSet(qosUUID)),
		},
	}}
}

func (b *QoSBuilder) conditions() []libovsdb.Condition {
	return []libovsdb.Condition{
		libovsdb.NewCondition(colDirection, libovsdb.ConditionEqual, b.direction),
		libovsdb.NewCondition(colPriority, libovsdb.ConditionEqual, b.priority),
		libovsdb.NewCondition(colMatch, libovsdb.ConditionEqual, b.match),
	}
}

func (r *QoSRef) conditions() []libovsdb.Condition {
	return []libovsdb.Condition{
		libovsdb.NewCondition(colDirection, libovsdb.ConditionEqual, r.direction),
		libovsdb.NewCondition(colPriority, libovsdb.ConditionEqual, r.priority),
		libovsdb.NewCondition(colMatch, libovsdb.ConditionEqual, r.match),
	}
}

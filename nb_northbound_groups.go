package ovnflow

import (
	"context"

	libovsdb "github.com/ovn-kubernetes/libovsdb/ovsdb"
)

func (n *NBClient) Meter(name string) *MeterRef {
	return &MeterRef{client: n, name: name}
}

func (n *NBClient) GetMeter(ctx context.Context, name string) (*Meter, error) {
	rows, err := n.selectRows(ctx, tableMeter, conditionName(name), nbMeterColumns(), name)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, wrap(ErrorNotFound, dbOVNNorthbound, tableMeter, "get", name, "meter not found", nil)
	}
	return meterFromRow(rows[0]), nil
}

func (r *MeterRef) Create() *MeterBuilder {
	return newMeterBuilder(r.client, r.name, nbModeCreate)
}

func (r *MeterRef) Ensure() *MeterBuilder {
	return newMeterBuilder(r.client, r.name, nbModeEnsure)
}

func (r *MeterRef) Delete() *MeterBuilder {
	return newMeterBuilder(r.client, r.name, nbModeDelete)
}

func newMeterBuilder(client *NBClient, name string, mode nbMode) *MeterBuilder {
	return &MeterBuilder{client: client, name: name, mode: mode, externalIDs: map[string]string{}}
}

func (b *MeterBuilder) WithUnit(unit string) *MeterBuilder {
	b.unit = unit
	return b
}

func (b *MeterBuilder) WithBandUUID(uuid string) *MeterBuilder {
	b.bands = append(b.bands, uuid)
	return b
}

func (b *MeterBuilder) WithBand(action string, rate int) *MeterBuilder {
	return b.WithNamedBand("", action, rate)
}

func (b *MeterBuilder) WithNamedBand(name, action string, rate int) *MeterBuilder {
	b.bandSpecs = append(b.bandSpecs, meterBandSpec{name: name, action: action, rate: rate, externalIDs: map[string]string{}})
	return b
}

func (b *MeterBuilder) WithBandExternalID(key, value string) *MeterBuilder {
	if len(b.bandSpecs) == 0 {
		return b
	}
	last := &b.bandSpecs[len(b.bandSpecs)-1]
	last.externalIDs[key] = value
	return b
}

func (b *MeterBuilder) WithFair(fair bool) *MeterBuilder {
	b.fair = &fair
	return b
}

func (b *MeterBuilder) WithExternalID(key, value string) *MeterBuilder {
	b.externalIDs[key] = value
	return b
}

func (b *MeterBuilder) Execute(ctx context.Context) error {
	if !b.once.mark() {
		return nbBuilderUsed(tableMeter, string(b.mode), b.name)
	}
	if err := b.validate(); err != nil {
		return err
	}
	if len(b.bandSpecs) > 0 {
		return b.executeWithInlineBands(ctx)
	}
	row := libovsdb.Row{colName: b.name}
	if b.unit != "" {
		row[colUnit] = b.unit
	}
	nbSetUUIDSet(row, colBands, b.bands)
	if b.fair != nil {
		row[colFair] = ovsSet(*b.fair)
	}
	setRowMap(row, colExternalIDs, b.externalIDs)
	mutations := []libovsdb.Mutation{}
	nbAppendUUIDSetMutation(&mutations, colBands, b.bands)
	nbAppendMapMutation(&mutations, colExternalIDs, b.externalIDs)
	return b.client.executeNamed(ctx, tableMeter, b.name, b.mode, row, mutations)
}

func (b *MeterBuilder) executeWithInlineBands(ctx context.Context) error {
	row := libovsdb.Row{colName: b.name}
	if b.unit != "" {
		row[colUnit] = b.unit
	}
	if b.fair != nil {
		row[colFair] = ovsSet(*b.fair)
	}
	setRowMap(row, colExternalIDs, b.externalIDs)

	var insertOps []libovsdb.Operation
	bandRefs := append([]string{}, b.bands...)
	for i, band := range b.bandSpecs {
		bandExternalIDs := cloneStringMap(b.externalIDs)
		for key, value := range band.externalIDs {
			if bandExternalIDs == nil {
				bandExternalIDs = map[string]string{}
			}
			bandExternalIDs[key] = value
		}
		bandRow := meterBandRow(band.name, band.action, band.rate, band.burstSize, bandExternalIDs)
		if band.name != "" {
			existingUUID, found, err := b.client.selectFirstUUID(ctx, tableMeterBand, nbExternalIDCondition(dnsNameExternalID, band.name), band.name)
			if err != nil {
				return err
			}
			if found {
				bandRefs = append(bandRefs, existingUUID)
				insertOps = append(insertOps, updateMeterBandOps(existingUUID, bandRow, bandExternalIDs)...)
				continue
			}
		}
		bandUUID := namedUUID("meterband")
		bandRefs = append(bandRefs, bandUUID)
		insertOps = append(insertOps, libovsdb.Operation{
			Op:       libovsdb.OperationInsert,
			Table:    tableMeterBand,
			UUIDName: bandUUID,
			Row:      bandRow,
		})
		if band.name == "" {
			b.bandSpecs[i].name = bandUUID
		}
	}
	nbSetUUIDSet(row, colBands, bandRefs)
	mutations := []libovsdb.Mutation{}
	nbAppendUUIDSetMutation(&mutations, colBands, bandRefs)
	nbAppendMapMutation(&mutations, colExternalIDs, b.externalIDs)
	return b.client.executeNamedWithPreOps(ctx, tableMeter, b.name, b.mode, row, mutations, insertOps)
}

func updateMeterBandOps(uuid string, row libovsdb.Row, externalIDs map[string]string) []libovsdb.Operation {
	updateRow := cloneRow(row)
	delete(updateRow, colExternalIDs)
	ops := []libovsdb.Operation{}
	if len(updateRow) > 0 {
		ops = append(ops, libovsdb.Operation{
			Op:    libovsdb.OperationUpdate,
			Table: tableMeterBand,
			Where: conditionUUID(uuid),
			Row:   updateRow,
		})
	}
	if len(externalIDs) > 0 {
		ops = append(ops, libovsdb.Operation{
			Op:    libovsdb.OperationMutate,
			Table: tableMeterBand,
			Where: conditionUUID(uuid),
			Mutations: []libovsdb.Mutation{
				*libovsdb.NewMutation(colExternalIDs, libovsdb.MutateOperationInsert, ovsMap(externalIDs)),
			},
		})
	}
	return ops
}

func (b *MeterBuilder) validate() error {
	if err := validateName("meter", b.name); err != nil {
		return err
	}
	if b.mode != nbModeDelete && b.unit == "" {
		return wrap(ErrorValidation, dbOVNNorthbound, tableMeter, string(b.mode), b.name, "unit is required", nil)
	}
	for _, band := range b.bandSpecs {
		if band.action == "" {
			return wrap(ErrorValidation, dbOVNNorthbound, tableMeterBand, string(b.mode), b.name, "band action is required", nil)
		}
		if band.rate < 1 {
			return wrap(ErrorValidation, dbOVNNorthbound, tableMeterBand, string(b.mode), b.name, "band rate must be greater than zero", nil)
		}
		if err := nbValidateStringMaps(band.externalIDs); err != nil {
			return err
		}
	}
	return nbValidateStringMaps(b.externalIDs)
}

func (n *NBClient) MeterBand(name string) *MeterBandRef {
	return &MeterBandRef{client: n, name: name}
}

func (n *NBClient) GetMeterBand(ctx context.Context, name string) (*MeterBand, error) {
	rows, err := n.selectRows(ctx, tableMeterBand, nbExternalIDCondition(dnsNameExternalID, name), nbMeterBandColumns(), name)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, wrap(ErrorNotFound, dbOVNNorthbound, tableMeterBand, "get", name, "meter band not found", nil)
	}
	return meterBandFromRow(rows[0]), nil
}

func (r *MeterBandRef) Create() *MeterBandBuilder {
	return newMeterBandBuilder(r.client, r.name, nbModeCreate)
}

func (r *MeterBandRef) Ensure() *MeterBandBuilder {
	return newMeterBandBuilder(r.client, r.name, nbModeEnsure)
}

func (r *MeterBandRef) Delete() *MeterBandBuilder {
	return newMeterBandBuilder(r.client, r.name, nbModeDelete)
}

func newMeterBandBuilder(client *NBClient, name string, mode nbMode) *MeterBandBuilder {
	return &MeterBandBuilder{client: client, name: name, mode: mode, action: "drop", externalIDs: map[string]string{}}
}

func (b *MeterBandBuilder) WithAction(action string) *MeterBandBuilder {
	b.action = action
	return b
}

func (b *MeterBandBuilder) WithRate(rate int) *MeterBandBuilder {
	b.rate = rate
	return b
}

func (b *MeterBandBuilder) WithBurstSize(size int) *MeterBandBuilder {
	b.burstSize = &size
	return b
}

func (b *MeterBandBuilder) WithExternalID(key, value string) *MeterBandBuilder {
	b.externalIDs[key] = value
	return b
}

func (b *MeterBandBuilder) Execute(ctx context.Context) error {
	if !b.once.mark() {
		return nbBuilderUsed(tableMeterBand, string(b.mode), b.name)
	}
	if err := b.validate(); err != nil {
		return err
	}
	row := meterBandRow(b.name, b.action, b.rate, b.burstSize, b.externalIDs)
	mutations := []libovsdb.Mutation{}
	nbAppendMapMutation(&mutations, colExternalIDs, b.externalIDs)
	return b.client.executeByConditions(ctx, tableMeterBand, b.name, b.mode, nbExternalIDCondition(dnsNameExternalID, b.name), row, mutations)
}

func (b *MeterBandBuilder) validate() error {
	if err := validateName("meter band", b.name); err != nil {
		return err
	}
	if b.mode != nbModeDelete && b.rate < 1 {
		return wrap(ErrorValidation, dbOVNNorthbound, tableMeterBand, string(b.mode), b.name, "rate must be greater than zero", nil)
	}
	return nbValidateStringMaps(b.externalIDs)
}

func meterBandRow(name, action string, rate int, burstSize *int, externalIDs map[string]string) libovsdb.Row {
	externalIDs = cloneStringMap(externalIDs)
	if externalIDs == nil {
		externalIDs = map[string]string{}
	}
	if name != "" {
		externalIDs[dnsNameExternalID] = name
	}
	row := libovsdb.Row{
		colAction: action,
		colRate:   rate,
	}
	if burstSize != nil {
		row[colBurstSize] = *burstSize
	}
	setRowMap(row, colExternalIDs, externalIDs)
	return row
}

func (n *NBClient) PortGroup(name string) *PortGroupRef {
	return &PortGroupRef{client: n, name: name}
}

func (n *NBClient) GetPortGroup(ctx context.Context, name string) (*PortGroup, error) {
	rows, err := n.selectRows(ctx, tablePortGroup, conditionName(name), nbPortGroupColumns(), name)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, wrap(ErrorNotFound, dbOVNNorthbound, tablePortGroup, "get", name, "port group not found", nil)
	}
	return portGroupFromRow(rows[0]), nil
}

func (r *PortGroupRef) Create() *PortGroupBuilder {
	return newPortGroupBuilder(r.client, r.name, nbModeCreate)
}

func (r *PortGroupRef) Ensure() *PortGroupBuilder {
	return newPortGroupBuilder(r.client, r.name, nbModeEnsure)
}

func (r *PortGroupRef) Delete() *PortGroupBuilder {
	return newPortGroupBuilder(r.client, r.name, nbModeDelete)
}

func newPortGroupBuilder(client *NBClient, name string, mode nbMode) *PortGroupBuilder {
	return &PortGroupBuilder{client: client, name: name, mode: mode, externalIDs: map[string]string{}}
}

func (b *PortGroupBuilder) WithPortUUID(uuid string) *PortGroupBuilder {
	b.ports = append(b.ports, uuid)
	return b
}

func (b *PortGroupBuilder) WithACLUUID(uuid string) *PortGroupBuilder {
	b.acls = append(b.acls, uuid)
	return b
}

func (b *PortGroupBuilder) WithACL(direction string, priority int, match, action string) *PortGroupBuilder {
	b.aclSpecs = append(b.aclSpecs, inlineACLSpec{
		direction:   direction,
		priority:    priority,
		match:       match,
		action:      action,
		externalIDs: map[string]string{},
	})
	return b
}

func (b *PortGroupBuilder) WithACLExternalID(key, value string) *PortGroupBuilder {
	if len(b.aclSpecs) == 0 {
		return b
	}
	last := &b.aclSpecs[len(b.aclSpecs)-1]
	last.externalIDs[key] = value
	return b
}

func (b *PortGroupBuilder) WithExternalID(key, value string) *PortGroupBuilder {
	b.externalIDs[key] = value
	return b
}

func (b *PortGroupBuilder) Execute(ctx context.Context) error {
	if !b.once.mark() {
		return nbBuilderUsed(tablePortGroup, string(b.mode), b.name)
	}
	if err := b.validate(); err != nil {
		return err
	}
	if len(b.aclSpecs) > 0 {
		return b.executeWithInlineACLs(ctx)
	}
	row := libovsdb.Row{colName: b.name}
	nbSetUUIDSet(row, colPorts, b.ports)
	nbSetUUIDSet(row, colACLs, b.acls)
	setRowMap(row, colExternalIDs, b.externalIDs)
	mutations := []libovsdb.Mutation{}
	nbAppendUUIDSetMutation(&mutations, colPorts, b.ports)
	nbAppendUUIDSetMutation(&mutations, colACLs, b.acls)
	nbAppendMapMutation(&mutations, colExternalIDs, b.externalIDs)
	return b.client.executeNamed(ctx, tablePortGroup, b.name, b.mode, row, mutations)
}

func (b *PortGroupBuilder) executeWithInlineACLs(ctx context.Context) error {
	row := libovsdb.Row{colName: b.name}
	nbSetUUIDSet(row, colPorts, b.ports)
	setRowMap(row, colExternalIDs, b.externalIDs)

	var preOps []libovsdb.Operation
	aclRefs := append([]string{}, b.acls...)
	for _, acl := range b.aclSpecs {
		acl.externalIDs = mergeStringMaps(b.externalIDs, acl.externalIDs)
		conditions := []libovsdb.Condition{
			libovsdb.NewCondition(colDirection, libovsdb.ConditionEqual, acl.direction),
			libovsdb.NewCondition(colPriority, libovsdb.ConditionEqual, acl.priority),
			libovsdb.NewCondition(colMatch, libovsdb.ConditionEqual, acl.match),
		}
		existingUUID, found, err := b.client.selectFirstUUID(ctx, tableACL, conditions, acl.match)
		if err != nil {
			return err
		}
		if found {
			aclRefs = append(aclRefs, existingUUID)
			preOps = append(preOps, updateInlineACLOps(existingUUID, inlineACLRow(acl), acl.externalIDs)...)
			continue
		}
		aclUUID := namedUUID("acl")
		aclRefs = append(aclRefs, aclUUID)
		preOps = append(preOps, libovsdb.Operation{
			Op:       libovsdb.OperationInsert,
			Table:    tableACL,
			UUIDName: aclUUID,
			Row:      inlineACLRow(acl),
		})
	}
	nbSetUUIDSet(row, colACLs, aclRefs)
	mutations := []libovsdb.Mutation{}
	nbAppendUUIDSetMutation(&mutations, colPorts, b.ports)
	nbAppendUUIDSetMutation(&mutations, colACLs, aclRefs)
	nbAppendMapMutation(&mutations, colExternalIDs, b.externalIDs)
	return b.client.executeNamedWithPreOps(ctx, tablePortGroup, b.name, b.mode, row, mutations, preOps)
}

func updateInlineACLOps(uuid string, row libovsdb.Row, externalIDs map[string]string) []libovsdb.Operation {
	updateRow := cloneRow(row)
	delete(updateRow, colExternalIDs)
	ops := []libovsdb.Operation{}
	if len(updateRow) > 0 {
		ops = append(ops, libovsdb.Operation{
			Op:    libovsdb.OperationUpdate,
			Table: tableACL,
			Where: conditionUUID(uuid),
			Row:   updateRow,
		})
	}
	if len(externalIDs) > 0 {
		ops = append(ops, libovsdb.Operation{
			Op:    libovsdb.OperationMutate,
			Table: tableACL,
			Where: conditionUUID(uuid),
			Mutations: []libovsdb.Mutation{
				*libovsdb.NewMutation(colExternalIDs, libovsdb.MutateOperationInsert, ovsMap(externalIDs)),
			},
		})
	}
	return ops
}

func (b *PortGroupBuilder) validate() error {
	if err := validateName("port group", b.name); err != nil {
		return err
	}
	maps := []map[string]string{b.externalIDs}
	for _, acl := range b.aclSpecs {
		if acl.direction == "" || acl.match == "" {
			return wrap(ErrorValidation, dbOVNNorthbound, tableACL, string(b.mode), b.name, "inline ACL direction and match are required", nil)
		}
		if acl.priority < 0 || acl.priority > 32767 {
			return wrap(ErrorValidation, dbOVNNorthbound, tableACL, string(b.mode), b.name, "inline ACL priority must be between 0 and 32767", nil)
		}
		maps = append(maps, acl.externalIDs)
	}
	return nbValidateStringMaps(maps...)
}

func inlineACLRow(acl inlineACLSpec) libovsdb.Row {
	row := libovsdb.Row{
		colPriority:  acl.priority,
		colDirection: acl.direction,
		colMatch:     acl.match,
	}
	if acl.action != "" {
		row[colAction] = acl.action
	}
	setRowMap(row, colExternalIDs, acl.externalIDs)
	return row
}

func (n *NBClient) AddressSet(name string) *AddressSetRef {
	return &AddressSetRef{client: n, name: name}
}

func (n *NBClient) GetAddressSet(ctx context.Context, name string) (*AddressSet, error) {
	rows, err := n.selectRows(ctx, tableAddressSet, conditionName(name), nbAddressSetColumns(), name)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, wrap(ErrorNotFound, dbOVNNorthbound, tableAddressSet, "get", name, "address set not found", nil)
	}
	return addressSetFromRow(rows[0]), nil
}

func (r *AddressSetRef) Create() *AddressSetBuilder {
	return newAddressSetBuilder(r.client, r.name, nbModeCreate)
}

func (r *AddressSetRef) Ensure() *AddressSetBuilder {
	return newAddressSetBuilder(r.client, r.name, nbModeEnsure)
}

func (r *AddressSetRef) Delete() *AddressSetBuilder {
	return newAddressSetBuilder(r.client, r.name, nbModeDelete)
}

func newAddressSetBuilder(client *NBClient, name string, mode nbMode) *AddressSetBuilder {
	return &AddressSetBuilder{client: client, name: name, mode: mode, externalIDs: map[string]string{}}
}

func (b *AddressSetBuilder) WithAddress(address string) *AddressSetBuilder {
	b.addresses = append(b.addresses, address)
	return b
}

func (b *AddressSetBuilder) WithAddresses(addresses ...string) *AddressSetBuilder {
	b.addresses = append(b.addresses, addresses...)
	return b
}

func (b *AddressSetBuilder) WithExternalID(key, value string) *AddressSetBuilder {
	b.externalIDs[key] = value
	return b
}

func (b *AddressSetBuilder) Execute(ctx context.Context) error {
	if !b.once.mark() {
		return nbBuilderUsed(tableAddressSet, string(b.mode), b.name)
	}
	if err := b.validate(); err != nil {
		return err
	}
	row := libovsdb.Row{colName: b.name}
	nbSetStringSet(row, colAddresses, b.addresses)
	setRowMap(row, colExternalIDs, b.externalIDs)
	mutations := []libovsdb.Mutation{}
	nbAppendStringSetMutation(&mutations, colAddresses, b.addresses)
	nbAppendMapMutation(&mutations, colExternalIDs, b.externalIDs)
	return b.client.executeNamed(ctx, tableAddressSet, b.name, b.mode, row, mutations)
}

func (b *AddressSetBuilder) validate() error {
	if err := validateName("address set", b.name); err != nil {
		return err
	}
	return nbValidateStringMaps(b.externalIDs)
}

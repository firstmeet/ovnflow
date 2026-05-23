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

func (b *MeterBuilder) validate() error {
	if err := validateName("meter", b.name); err != nil {
		return err
	}
	if b.mode != nbModeDelete && b.unit == "" {
		return wrap(ErrorValidation, dbOVNNorthbound, tableMeter, string(b.mode), b.name, "unit is required", nil)
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
	if b.name != "" {
		b.externalIDs[dnsNameExternalID] = b.name
	}
	row := libovsdb.Row{
		colAction: b.action,
		colRate:   b.rate,
	}
	if b.burstSize != nil {
		row[colBurstSize] = *b.burstSize
	}
	setRowMap(row, colExternalIDs, b.externalIDs)
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

func (b *PortGroupBuilder) validate() error {
	if err := validateName("port group", b.name); err != nil {
		return err
	}
	return nbValidateStringMaps(b.externalIDs)
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

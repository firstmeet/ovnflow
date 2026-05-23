package ovnflow

import (
	"context"
	"net"

	libovsdb "github.com/ovn-kubernetes/libovsdb/ovsdb"
)

func (n *NBClient) GatewayChassis(name string) *GatewayChassisRef {
	return &GatewayChassisRef{client: n, name: name}
}

func (n *NBClient) GetGatewayChassis(ctx context.Context, name string) (*GatewayChassis, error) {
	rows, err := n.selectRows(ctx, tableGatewayChassis, conditionName(name), nbGatewayChassisColumns(), name)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, wrap(ErrorNotFound, dbOVNNorthbound, tableGatewayChassis, "get", name, "gateway chassis not found", nil)
	}
	return gatewayChassisFromRow(rows[0]), nil
}

func (r *GatewayChassisRef) Create() *GatewayChassisBuilder {
	return newGatewayChassisBuilder(r.client, r.name, nbModeCreate)
}

func (r *GatewayChassisRef) Ensure() *GatewayChassisBuilder {
	return newGatewayChassisBuilder(r.client, r.name, nbModeEnsure)
}

func (r *GatewayChassisRef) Delete() *GatewayChassisBuilder {
	return newGatewayChassisBuilder(r.client, r.name, nbModeDelete)
}

func newGatewayChassisBuilder(client *NBClient, name string, mode nbMode) *GatewayChassisBuilder {
	return &GatewayChassisBuilder{client: client, name: name, mode: mode, options: map[string]string{}, externalIDs: map[string]string{}}
}

func (b *GatewayChassisBuilder) WithChassisName(name string) *GatewayChassisBuilder {
	b.chassisName = name
	return b
}

func (b *GatewayChassisBuilder) WithPriority(priority int) *GatewayChassisBuilder {
	b.priority = &priority
	return b
}

func (b *GatewayChassisBuilder) WithOption(key, value string) *GatewayChassisBuilder {
	b.options[key] = value
	return b
}

func (b *GatewayChassisBuilder) WithExternalID(key, value string) *GatewayChassisBuilder {
	b.externalIDs[key] = value
	return b
}

func (b *GatewayChassisBuilder) Execute(ctx context.Context) error {
	if !b.once.mark() {
		return nbBuilderUsed(tableGatewayChassis, string(b.mode), b.name)
	}
	if err := b.validate(); err != nil {
		return err
	}
	row := gatewayChassisRow(gatewayChassisSpec{
		name:        b.name,
		chassisName: b.chassisName,
		priority:    b.priority,
		options:     b.options,
		externalIDs: b.externalIDs,
	}, nil)
	mutations := []libovsdb.Mutation{}
	nbAppendMapMutation(&mutations, colOptions, b.options)
	nbAppendMapMutation(&mutations, colExternalIDs, b.externalIDs)
	return b.client.executeNamed(ctx, tableGatewayChassis, b.name, b.mode, row, mutations)
}

func (b *GatewayChassisBuilder) validate() error {
	if err := validateName("gateway chassis", b.name); err != nil {
		return err
	}
	if b.mode != nbModeDelete && b.chassisName == "" {
		return wrap(ErrorValidation, dbOVNNorthbound, tableGatewayChassis, string(b.mode), b.name, "chassis_name is required", nil)
	}
	return nbValidateStringMaps(b.options, b.externalIDs)
}

func gatewayChassisRow(spec gatewayChassisSpec, inheritedExternalIDs map[string]string) libovsdb.Row {
	row := libovsdb.Row{colName: spec.name}
	if spec.chassisName != "" {
		row[colChassisName] = spec.chassisName
	}
	if spec.priority != nil {
		row[colPriority] = *spec.priority
	}
	setRowMap(row, colOptions, spec.options)
	setRowMap(row, colExternalIDs, mergeStringMaps(inheritedExternalIDs, spec.externalIDs))
	return row
}

func updateGatewayChassisOps(uuid string, row libovsdb.Row, externalIDs map[string]string) []libovsdb.Operation {
	updateRow := cloneRow(row)
	delete(updateRow, colName)
	delete(updateRow, colExternalIDs)
	ops := []libovsdb.Operation{}
	if len(updateRow) > 0 {
		ops = append(ops, libovsdb.Operation{
			Op:    libovsdb.OperationUpdate,
			Table: tableGatewayChassis,
			Where: conditionUUID(uuid),
			Row:   updateRow,
		})
	}
	if len(externalIDs) > 0 {
		ops = append(ops, libovsdb.Operation{
			Op:    libovsdb.OperationMutate,
			Table: tableGatewayChassis,
			Where: conditionUUID(uuid),
			Mutations: []libovsdb.Mutation{
				*libovsdb.NewMutation(colExternalIDs, libovsdb.MutateOperationInsert, ovsMap(externalIDs)),
			},
		})
	}
	return ops
}

func (n *NBClient) HAChassis(chassisName string) *HAChassisRef {
	return &HAChassisRef{client: n, chassisName: chassisName}
}

func (n *NBClient) GetHAChassis(ctx context.Context, chassisName string) (*HAChassis, error) {
	conditions := []libovsdb.Condition{libovsdb.NewCondition(colChassisName, libovsdb.ConditionEqual, chassisName)}
	rows, err := n.selectRows(ctx, tableHAChassis, conditions, nbHAChassisColumns(), chassisName)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, wrap(ErrorNotFound, dbOVNNorthbound, tableHAChassis, "get", chassisName, "ha chassis not found", nil)
	}
	return haChassisFromRow(rows[0]), nil
}

func (r *HAChassisRef) Create() *HAChassisBuilder {
	return newHAChassisBuilder(r.client, r.chassisName, nbModeCreate)
}

func (r *HAChassisRef) Ensure() *HAChassisBuilder {
	return newHAChassisBuilder(r.client, r.chassisName, nbModeEnsure)
}

func (r *HAChassisRef) Delete() *HAChassisBuilder {
	return newHAChassisBuilder(r.client, r.chassisName, nbModeDelete)
}

func newHAChassisBuilder(client *NBClient, chassisName string, mode nbMode) *HAChassisBuilder {
	return &HAChassisBuilder{client: client, chassisName: chassisName, mode: mode, externalIDs: map[string]string{}}
}

func (b *HAChassisBuilder) WithPriority(priority int) *HAChassisBuilder {
	b.priority = &priority
	return b
}

func (b *HAChassisBuilder) WithExternalID(key, value string) *HAChassisBuilder {
	b.externalIDs[key] = value
	return b
}

func (b *HAChassisBuilder) Execute(ctx context.Context) error {
	if !b.once.mark() {
		return nbBuilderUsed(tableHAChassis, string(b.mode), b.chassisName)
	}
	if err := b.validate(); err != nil {
		return err
	}
	row := haChassisRow(haChassisSpec{
		chassisName: b.chassisName,
		priority:    b.priority,
		externalIDs: b.externalIDs,
	}, nil)
	mutations := []libovsdb.Mutation{}
	nbAppendMapMutation(&mutations, colExternalIDs, b.externalIDs)
	conditions := []libovsdb.Condition{libovsdb.NewCondition(colChassisName, libovsdb.ConditionEqual, b.chassisName)}
	return b.client.executeByConditions(ctx, tableHAChassis, b.chassisName, b.mode, conditions, row, mutations)
}

func (b *HAChassisBuilder) validate() error {
	if err := validateName("ha chassis", b.chassisName); err != nil {
		return err
	}
	return nbValidateStringMaps(b.externalIDs)
}

func haChassisRow(spec haChassisSpec, inheritedExternalIDs map[string]string) libovsdb.Row {
	row := libovsdb.Row{colChassisName: spec.chassisName}
	if spec.priority != nil {
		row[colPriority] = *spec.priority
	}
	setRowMap(row, colExternalIDs, mergeStringMaps(inheritedExternalIDs, spec.externalIDs))
	return row
}

func updateHAChassisOps(uuid string, row libovsdb.Row, externalIDs map[string]string) []libovsdb.Operation {
	updateRow := cloneRow(row)
	delete(updateRow, colChassisName)
	delete(updateRow, colExternalIDs)
	ops := []libovsdb.Operation{}
	if len(updateRow) > 0 {
		ops = append(ops, libovsdb.Operation{
			Op:    libovsdb.OperationUpdate,
			Table: tableHAChassis,
			Where: conditionUUID(uuid),
			Row:   updateRow,
		})
	}
	if len(externalIDs) > 0 {
		ops = append(ops, libovsdb.Operation{
			Op:    libovsdb.OperationMutate,
			Table: tableHAChassis,
			Where: conditionUUID(uuid),
			Mutations: []libovsdb.Mutation{
				*libovsdb.NewMutation(colExternalIDs, libovsdb.MutateOperationInsert, ovsMap(externalIDs)),
			},
		})
	}
	return ops
}

func (n *NBClient) HAChassisGroup(name string) *HAChassisGroupRef {
	return &HAChassisGroupRef{client: n, name: name}
}

func (n *NBClient) GetHAChassisGroup(ctx context.Context, name string) (*HAChassisGroup, error) {
	rows, err := n.selectRows(ctx, tableHAChassisGroup, conditionName(name), nbHAChassisGroupColumns(), name)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, wrap(ErrorNotFound, dbOVNNorthbound, tableHAChassisGroup, "get", name, "ha chassis group not found", nil)
	}
	return haChassisGroupFromRow(rows[0]), nil
}

func (r *HAChassisGroupRef) Create() *HAChassisGroupBuilder {
	return newHAChassisGroupBuilder(r.client, r.name, nbModeCreate)
}

func (r *HAChassisGroupRef) Ensure() *HAChassisGroupBuilder {
	return newHAChassisGroupBuilder(r.client, r.name, nbModeEnsure)
}

func (r *HAChassisGroupRef) Delete() *HAChassisGroupBuilder {
	return newHAChassisGroupBuilder(r.client, r.name, nbModeDelete)
}

func newHAChassisGroupBuilder(client *NBClient, name string, mode nbMode) *HAChassisGroupBuilder {
	return &HAChassisGroupBuilder{client: client, name: name, mode: mode, externalIDs: map[string]string{}}
}

func (b *HAChassisGroupBuilder) WithHAChassisUUID(uuid string) *HAChassisGroupBuilder {
	b.haChassis = append(b.haChassis, uuid)
	return b
}

func (b *HAChassisGroupBuilder) WithExternalID(key, value string) *HAChassisGroupBuilder {
	b.externalIDs[key] = value
	return b
}

func (b *HAChassisGroupBuilder) Execute(ctx context.Context) error {
	if !b.once.mark() {
		return nbBuilderUsed(tableHAChassisGroup, string(b.mode), b.name)
	}
	if err := b.validate(); err != nil {
		return err
	}
	row := haChassisGroupRow(&haChassisGroupSpec{
		name:        b.name,
		haChassis:   nil,
		externalIDs: b.externalIDs,
	}, b.haChassis, nil)
	mutations := []libovsdb.Mutation{}
	nbAppendUUIDSetMutation(&mutations, colHAChassis, b.haChassis)
	nbAppendMapMutation(&mutations, colExternalIDs, b.externalIDs)
	return b.client.executeNamed(ctx, tableHAChassisGroup, b.name, b.mode, row, mutations)
}

func (b *HAChassisGroupBuilder) validate() error {
	if err := validateName("ha chassis group", b.name); err != nil {
		return err
	}
	return nbValidateStringMaps(b.externalIDs)
}

func haChassisGroupRow(spec *haChassisGroupSpec, haChassisRefs []string, inheritedExternalIDs map[string]string) libovsdb.Row {
	row := libovsdb.Row{}
	if spec == nil {
		return row
	}
	row[colName] = spec.name
	nbSetUUIDSet(row, colHAChassis, haChassisRefs)
	setRowMap(row, colExternalIDs, mergeStringMaps(inheritedExternalIDs, spec.externalIDs))
	return row
}

func updateHAChassisGroupOps(uuid string, row libovsdb.Row, haChassisRefs []string, externalIDs map[string]string) []libovsdb.Operation {
	updateRow := cloneRow(row)
	delete(updateRow, colName)
	delete(updateRow, colHAChassis)
	delete(updateRow, colExternalIDs)
	ops := []libovsdb.Operation{}
	if len(updateRow) > 0 {
		ops = append(ops, libovsdb.Operation{
			Op:    libovsdb.OperationUpdate,
			Table: tableHAChassisGroup,
			Where: conditionUUID(uuid),
			Row:   updateRow,
		})
	}
	if len(haChassisRefs) > 0 {
		ops = append(ops, libovsdb.Operation{
			Op:    libovsdb.OperationMutate,
			Table: tableHAChassisGroup,
			Where: conditionUUID(uuid),
			Mutations: []libovsdb.Mutation{
				*libovsdb.NewMutation(colHAChassis, libovsdb.MutateOperationInsert, uuidSet(haChassisRefs...)),
			},
		})
	}
	if len(externalIDs) > 0 {
		ops = append(ops, libovsdb.Operation{
			Op:    libovsdb.OperationMutate,
			Table: tableHAChassisGroup,
			Where: conditionUUID(uuid),
			Mutations: []libovsdb.Mutation{
				*libovsdb.NewMutation(colExternalIDs, libovsdb.MutateOperationInsert, ovsMap(externalIDs)),
			},
		})
	}
	return ops
}

func (n *NBClient) BFD(logicalPort, dstIP string) *BFDRef {
	return &BFDRef{client: n, logicalPort: logicalPort, dstIP: dstIP}
}

func (n *NBClient) GetBFD(ctx context.Context, logicalPort, dstIP string) (*BFD, error) {
	ref := n.BFD(logicalPort, dstIP)
	rows, err := n.selectRows(ctx, tableBFD, ref.conditions(), nbBFDColumns(), logicalPort)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, wrap(ErrorNotFound, dbOVNNorthbound, tableBFD, "get", logicalPort, "bfd row not found", nil)
	}
	return bfdFromRow(rows[0]), nil
}

func (r *BFDRef) Create() *BFDBuilder {
	return newBFDBuilder(r.client, r.logicalPort, r.dstIP, nbModeCreate)
}

func (r *BFDRef) Ensure() *BFDBuilder {
	return newBFDBuilder(r.client, r.logicalPort, r.dstIP, nbModeEnsure)
}

func (r *BFDRef) Delete() *BFDBuilder {
	return newBFDBuilder(r.client, r.logicalPort, r.dstIP, nbModeDelete)
}

func newBFDBuilder(client *NBClient, logicalPort, dstIP string, mode nbMode) *BFDBuilder {
	return &BFDBuilder{client: client, logicalPort: logicalPort, dstIP: dstIP, mode: mode, options: map[string]string{}, externalIDs: map[string]string{}}
}

func (b *BFDBuilder) WithMinTx(value int) *BFDBuilder {
	b.minTx = &value
	return b
}

func (b *BFDBuilder) WithMinRx(value int) *BFDBuilder {
	b.minRx = &value
	return b
}

func (b *BFDBuilder) WithDetectMult(value int) *BFDBuilder {
	b.detectMult = &value
	return b
}

func (b *BFDBuilder) WithStatus(status string) *BFDBuilder {
	b.status = status
	return b
}

func (b *BFDBuilder) WithOption(key, value string) *BFDBuilder {
	b.options[key] = value
	return b
}

func (b *BFDBuilder) WithExternalID(key, value string) *BFDBuilder {
	b.externalIDs[key] = value
	return b
}

func (b *BFDBuilder) Execute(ctx context.Context) error {
	if !b.once.mark() {
		return nbBuilderUsed(tableBFD, string(b.mode), b.logicalPort)
	}
	if err := b.validate(); err != nil {
		return err
	}
	row := libovsdb.Row{
		colLogicalPort: b.logicalPort,
		colDstIP:       b.dstIP,
	}
	if b.minTx != nil {
		row[colMinTx] = ovsSet(*b.minTx)
	}
	if b.minRx != nil {
		row[colMinRx] = ovsSet(*b.minRx)
	}
	if b.detectMult != nil {
		row[colDetectMult] = ovsSet(*b.detectMult)
	}
	nbSetOptionalString(row, colStatus, b.status)
	setRowMap(row, colOptions, b.options)
	setRowMap(row, colExternalIDs, b.externalIDs)
	mutations := []libovsdb.Mutation{}
	nbAppendMapMutation(&mutations, colOptions, b.options)
	nbAppendMapMutation(&mutations, colExternalIDs, b.externalIDs)
	return b.client.executeByConditions(ctx, tableBFD, b.logicalPort, b.mode, b.conditions(), row, mutations)
}

func (b *BFDBuilder) validate() error {
	if err := validateName("bfd logical port", b.logicalPort); err != nil {
		return err
	}
	if net.ParseIP(b.dstIP) == nil {
		return wrap(ErrorValidation, dbOVNNorthbound, tableBFD, string(b.mode), b.logicalPort, "invalid dst_ip", nil)
	}
	return nbValidateStringMaps(b.options, b.externalIDs)
}

func (b *BFDBuilder) conditions() []libovsdb.Condition {
	return []libovsdb.Condition{
		libovsdb.NewCondition(colLogicalPort, libovsdb.ConditionEqual, b.logicalPort),
		libovsdb.NewCondition(colDstIP, libovsdb.ConditionEqual, b.dstIP),
	}
}

func (r *BFDRef) conditions() []libovsdb.Condition {
	return []libovsdb.Condition{
		libovsdb.NewCondition(colLogicalPort, libovsdb.ConditionEqual, r.logicalPort),
		libovsdb.NewCondition(colDstIP, libovsdb.ConditionEqual, r.dstIP),
	}
}

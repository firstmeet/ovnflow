package ovnflow

import (
	"context"

	libovsdb "github.com/ovn-kubernetes/libovsdb/ovsdb"
)

// OVSClient provides local Open_vSwitch fluent APIs.
type OVSClient struct {
	db *dbClient
}

func (o *OVSClient) Bridge(name string) *BridgeRef {
	return &BridgeRef{client: o, name: name}
}

type BridgeRef struct {
	client *OVSClient
	name   string
}

func (r *BridgeRef) Ensure() *BridgeBuilder {
	return newBridgeBuilder(r.client, r.name, ovsModeEnsure)
}

func (r *BridgeRef) Delete() *BridgeBuilder {
	return newBridgeBuilder(r.client, r.name, ovsModeDelete)
}

func (r *BridgeRef) AddPort(name string) *OVSPortBuilder {
	builder := newBridgeBuilder(r.client, r.name, ovsModeEnsure)
	return builder.AddPort(name)
}

func (r *BridgeRef) DeletePort(name string) *BridgeBuilder {
	builder := newBridgeBuilder(r.client, r.name, ovsModeDeletePort)
	builder.deletePort = name
	return builder
}

type ovsMode string

const (
	ovsModeEnsure     ovsMode = "ensure"
	ovsModeDelete     ovsMode = "delete"
	ovsModeDeletePort ovsMode = "delete_port"
)

type BridgeBuilder struct {
	once        useOnce
	client      *OVSClient
	name        string
	mode        ovsMode
	port        *ovsPortSpec
	deletePort  string
	externalIDs map[string]string
}

type ovsPortSpec struct {
	name          string
	interfaceName string
	interfaceType string
	externalIDs   map[string]string
}

func newBridgeBuilder(client *OVSClient, name string, mode ovsMode) *BridgeBuilder {
	return &BridgeBuilder{client: client, name: name, mode: mode, externalIDs: map[string]string{}}
}

func (b *BridgeBuilder) WithExternalID(key, value string) *BridgeBuilder {
	if b.externalIDs == nil {
		b.externalIDs = map[string]string{}
	}
	b.externalIDs[key] = value
	return b
}

func (b *BridgeBuilder) AddPort(name string) *OVSPortBuilder {
	b.port = &ovsPortSpec{name: name, interfaceName: name, externalIDs: map[string]string{}}
	return &OVSPortBuilder{parent: b, spec: b.port}
}

func (b *BridgeBuilder) Execute(ctx context.Context) error {
	if !b.once.mark() {
		return wrap(ErrorValidation, dbOpenVSwitch, tableBridge, string(b.mode), b.name, "builder already executed", nil)
	}
	if err := b.validate(); err != nil {
		return err
	}
	switch b.mode {
	case ovsModeEnsure:
		return b.executeEnsure(ctx)
	case ovsModeDelete:
		return b.executeDelete(ctx)
	case ovsModeDeletePort:
		return b.executeDeletePort(ctx)
	default:
		return wrap(ErrorValidation, dbOpenVSwitch, tableBridge, string(b.mode), b.name, "unsupported operation", nil)
	}
}

func (b *BridgeBuilder) validate() error {
	if err := validateName("bridge", b.name); err != nil {
		return err
	}
	for key := range b.externalIDs {
		if err := validateExternalID(key); err != nil {
			return err
		}
	}
	if b.port != nil {
		if err := validateName("port", b.port.name); err != nil {
			return err
		}
		if err := validateName("interface", b.port.interfaceName); err != nil {
			return err
		}
		for key := range b.port.externalIDs {
			if err := validateExternalID(key); err != nil {
				return err
			}
		}
	}
	if b.deletePort != "" {
		if err := validateName("port", b.deletePort); err != nil {
			return err
		}
	}
	return nil
}

func (b *BridgeBuilder) executeEnsure(ctx context.Context) error {
	attempts := 2
	var err error
	for attempt := 0; attempt < attempts; attempt++ {
		err = b.executeEnsureOnce(ctx)
		if err == nil {
			return nil
		}
		if !IsKind(err, ErrorAlreadyExists) {
			return err
		}
	}
	return err
}

func (b *BridgeBuilder) executeEnsureOnce(ctx context.Context) error {
	bridges, err := b.client.selectBridges(ctx, b.name)
	if err != nil {
		return err
	}

	var ops []libovsdb.Operation
	var bridgeNeedsCreate bool
	if len(bridges) == 0 {
		bridgeNeedsCreate = true
	}

	if b.port != nil {
		existingPorts, err := b.client.selectPorts(ctx, b.port.name)
		if err != nil {
			return err
		}
		if len(existingPorts) > 0 {
			if bridgeNeedsCreate {
				ops = append(ops, b.insertBridgeOp(existingPorts[0].UUID))
			} else if !containsString(bridges[0].Ports, existingPorts[0].UUID) {
				ops = append(ops, libovsdb.Operation{
					Op:    libovsdb.OperationMutate,
					Table: tableBridge,
					Where: conditionUUID(bridges[0].UUID),
					Mutations: []libovsdb.Mutation{
						*libovsdb.NewMutation(colPorts, libovsdb.MutateOperationInsert, uuidSet(existingPorts[0].UUID)),
					},
				})
			}
			var portMutations []libovsdb.Mutation
			if len(b.port.externalIDs) > 0 {
				portMutations = append(portMutations, *libovsdb.NewMutation(colExternalIDs, libovsdb.MutateOperationInsert, ovsMap(b.port.externalIDs)))
			}
			if len(portMutations) > 0 {
				ops = append(ops, libovsdb.Operation{
					Op:        libovsdb.OperationMutate,
					Table:     tablePort,
					Where:     conditionUUID(existingPorts[0].UUID),
					Mutations: portMutations,
				})
			}
			if b.port.interfaceType != "" && len(existingPorts[0].Interfaces) > 0 {
				for _, ifaceUUID := range existingPorts[0].Interfaces {
					row := libovsdb.Row{colType: b.port.interfaceType}
					if len(b.port.externalIDs) > 0 {
						row[colExternalIDs] = ovsMap(b.port.externalIDs)
					}
					ops = append(ops, libovsdb.Operation{
						Op:    libovsdb.OperationUpdate,
						Table: tableInterface,
						Where: conditionUUID(ifaceUUID),
						Row:   row,
					})
				}
			}
		} else {
			ifaceUUID := namedUUID("iface")
			portUUID := namedUUID("port")
			ifaceRow := libovsdb.Row{
				colName: b.port.interfaceName,
			}
			setRowMap(ifaceRow, colExternalIDs, b.port.externalIDs)
			if b.port.interfaceType != "" {
				ifaceRow[colType] = b.port.interfaceType
			}
			ops = append(ops, libovsdb.Operation{
				Op:       libovsdb.OperationInsert,
				Table:    tableInterface,
				UUIDName: ifaceUUID,
				Row:      ifaceRow,
			})
			portRow := libovsdb.Row{
				colName:       b.port.name,
				colInterfaces: uuidSet(ifaceUUID),
			}
			setRowMap(portRow, colExternalIDs, b.port.externalIDs)
			ops = append(ops, libovsdb.Operation{
				Op:       libovsdb.OperationInsert,
				Table:    tablePort,
				UUIDName: portUUID,
				Row:      portRow,
			})
			if bridgeNeedsCreate {
				ops = append(ops, b.insertBridgeOp(portUUID))
			} else {
				ops = append(ops, libovsdb.Operation{
					Op:    libovsdb.OperationMutate,
					Table: tableBridge,
					Where: conditionUUID(bridges[0].UUID),
					Mutations: []libovsdb.Mutation{
						*libovsdb.NewMutation(colPorts, libovsdb.MutateOperationInsert, uuidSet(portUUID)),
					},
				})
			}
		}
	} else if bridgeNeedsCreate {
		ops = append(ops, b.insertBridgeOp())
	} else if len(b.externalIDs) > 0 {
		ops = append(ops, libovsdb.Operation{
			Op:    libovsdb.OperationMutate,
			Table: tableBridge,
			Where: conditionUUID(bridges[0].UUID),
			Mutations: []libovsdb.Mutation{
				*libovsdb.NewMutation(colExternalIDs, libovsdb.MutateOperationInsert, ovsMap(b.externalIDs)),
			},
		})
	}

	if bridgeNeedsCreate {
		bridgeUUID := opsBridgeUUID(ops)
		if bridgeUUID == "" {
			return wrap(ErrorConflict, dbOpenVSwitch, tableBridge, string(b.mode), b.name, "bridge insert operation missing UUID", nil)
		}
		rootUUID, err := b.client.openVSwitchUUID(ctx)
		if err != nil {
			return err
		}
		ops = append(ops, libovsdb.Operation{
			Op:    libovsdb.OperationMutate,
			Table: tableOpenVSwitch,
			Where: conditionUUID(rootUUID),
			Mutations: []libovsdb.Mutation{
				*libovsdb.NewMutation(colBridges, libovsdb.MutateOperationInsert, uuidSet(bridgeUUID)),
			},
		})
	}
	if len(ops) == 0 {
		return nil
	}
	results, err := b.client.db.raw.Transact(ctx, ops...)
	if err != nil {
		return classifyTransactError(err, dbOpenVSwitch, tableBridge, string(b.mode), b.name)
	}
	return checkOperationResults(results, dbOpenVSwitch, tableBridge, string(b.mode), b.name)
}

func (b *BridgeBuilder) insertBridgeOp(portNamedUUIDs ...string) libovsdb.Operation {
	bridgeUUID := namedUUID("bridge")
	row := libovsdb.Row{
		colName: b.name,
	}
	setRowMap(row, colExternalIDs, b.externalIDs)
	if len(portNamedUUIDs) > 0 {
		row[colPorts] = uuidSet(portNamedUUIDs...)
	}
	return libovsdb.Operation{
		Op:       libovsdb.OperationInsert,
		Table:    tableBridge,
		UUIDName: bridgeUUID,
		Row:      row,
	}
}

func opsBridgeUUID(ops []libovsdb.Operation) string {
	for i := range ops {
		if ops[i].Table == tableBridge && ops[i].Op == libovsdb.OperationInsert {
			if ops[i].UUIDName != "" {
				return ops[i].UUIDName
			}
			return ops[i].UUID
		}
	}
	return ""
}

func (b *BridgeBuilder) executeDelete(ctx context.Context) error {
	bridges, err := b.client.selectBridges(ctx, b.name)
	if err != nil {
		return err
	}
	if len(bridges) == 0 {
		return wrap(ErrorNotFound, dbOpenVSwitch, tableBridge, "delete", b.name, "bridge not found", nil)
	}
	rootUUID, err := b.client.openVSwitchUUID(ctx)
	if err != nil {
		return err
	}
	ports, err := b.client.selectPortsByUUID(ctx, bridges[0].Ports)
	if err != nil {
		return err
	}

	var mustAffect []int
	ops := []libovsdb.Operation{
		{
			Op:    libovsdb.OperationMutate,
			Table: tableOpenVSwitch,
			Where: conditionUUID(rootUUID),
			Mutations: []libovsdb.Mutation{
				*libovsdb.NewMutation(colBridges, libovsdb.MutateOperationDelete, uuidSet(bridges[0].UUID)),
			},
		},
		{
			Op:    libovsdb.OperationDelete,
			Table: tableBridge,
			Where: conditionUUID(bridges[0].UUID),
		},
	}
	mustAffect = append(mustAffect, 0, 1)
	for _, port := range ports {
		ops = append(ops, libovsdb.Operation{
			Op:    libovsdb.OperationDelete,
			Table: tablePort,
			Where: conditionUUID(port.UUID),
		})
		mustAffect = append(mustAffect, len(ops)-1)
		for _, ifaceUUID := range port.Interfaces {
			ops = append(ops, libovsdb.Operation{
				Op:    libovsdb.OperationDelete,
				Table: tableInterface,
				Where: conditionUUID(ifaceUUID),
			})
			mustAffect = append(mustAffect, len(ops)-1)
		}
	}
	results, err := b.client.db.raw.Transact(ctx, ops...)
	if err != nil {
		return classifyTransactError(err, dbOpenVSwitch, tableBridge, "delete", b.name)
	}
	return ensureAffected(results, mustAffect, dbOpenVSwitch, tableBridge, "delete", b.name)
}

func (b *BridgeBuilder) executeDeletePort(ctx context.Context) error {
	bridges, err := b.client.selectBridges(ctx, b.name)
	if err != nil {
		return err
	}
	if len(bridges) == 0 {
		return wrap(ErrorNotFound, dbOpenVSwitch, tableBridge, "delete_port", b.name, "bridge not found", nil)
	}
	ports, err := b.client.selectPorts(ctx, b.deletePort)
	if err != nil {
		return err
	}
	if len(ports) == 0 {
		return wrap(ErrorNotFound, dbOpenVSwitch, tablePort, "delete", b.deletePort, "port not found", nil)
	}
	portUUID := ports[0].UUID
	if !containsString(bridges[0].Ports, portUUID) {
		return wrap(ErrorNotFound, dbOpenVSwitch, tablePort, "delete", b.deletePort, "port is not attached to bridge", nil)
	}
	var mustAffect []int
	ops := []libovsdb.Operation{
		{
			Op:    libovsdb.OperationMutate,
			Table: tableBridge,
			Where: conditionUUID(bridges[0].UUID),
			Mutations: []libovsdb.Mutation{
				*libovsdb.NewMutation(colPorts, libovsdb.MutateOperationDelete, uuidSet(portUUID)),
			},
		},
		{
			Op:    libovsdb.OperationDelete,
			Table: tablePort,
			Where: conditionUUID(portUUID),
		},
	}
	mustAffect = append(mustAffect, 0, 1)
	for _, ifaceUUID := range ports[0].Interfaces {
		ops = append(ops, libovsdb.Operation{
			Op:    libovsdb.OperationDelete,
			Table: tableInterface,
			Where: conditionUUID(ifaceUUID),
		})
		mustAffect = append(mustAffect, len(ops)-1)
	}
	results, err := b.client.db.raw.Transact(ctx, ops...)
	if err != nil {
		return classifyTransactError(err, dbOpenVSwitch, tablePort, "delete", b.deletePort)
	}
	return ensureAffected(results, mustAffect, dbOpenVSwitch, tablePort, "delete", b.deletePort)
}

func (o *OVSClient) selectBridges(ctx context.Context, name string) ([]OVSBridge, error) {
	results, err := o.db.raw.Transact(ctx, libovsdb.Operation{
		Op:      libovsdb.OperationSelect,
		Table:   tableBridge,
		Where:   conditionName(name),
		Columns: []string{colUUID, colName, colPorts, colExternalIDs},
	})
	if err != nil {
		return nil, classifyTransactError(err, dbOpenVSwitch, tableBridge, "select", name)
	}
	if err := checkOperationResults(results, dbOpenVSwitch, tableBridge, "select", name); err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, nil
	}
	rows := make([]OVSBridge, 0, len(results[0].Rows))
	for _, row := range results[0].Rows {
		rows = append(rows, OVSBridge{
			UUID:        rowUUIDValue(row),
			Name:        rowStringValue(row, colName),
			Ports:       rowUUIDSliceValue(row, colPorts),
			ExternalIDs: rowStringMapValue(row, colExternalIDs),
		})
	}
	return rows, nil
}

func (o *OVSClient) selectPorts(ctx context.Context, name string) ([]OVSPort, error) {
	results, err := o.db.raw.Transact(ctx, libovsdb.Operation{
		Op:      libovsdb.OperationSelect,
		Table:   tablePort,
		Where:   conditionName(name),
		Columns: []string{colUUID, colName, colInterfaces, colExternalIDs},
	})
	if err != nil {
		return nil, classifyTransactError(err, dbOpenVSwitch, tablePort, "select", name)
	}
	if err := checkOperationResults(results, dbOpenVSwitch, tablePort, "select", name); err != nil {
		return nil, err
	}
	return decodeOVSPorts(results)
}

func (o *OVSClient) selectPortsByUUID(ctx context.Context, ids []string) ([]OVSPort, error) {
	var rows []OVSPort
	for _, id := range uniqueStrings(ids) {
		results, err := o.db.raw.Transact(ctx, libovsdb.Operation{
			Op:      libovsdb.OperationSelect,
			Table:   tablePort,
			Where:   conditionUUID(id),
			Columns: []string{colUUID, colName, colInterfaces, colExternalIDs},
		})
		if err != nil {
			return nil, classifyTransactError(err, dbOpenVSwitch, tablePort, "select", id)
		}
		if err := checkOperationResults(results, dbOpenVSwitch, tablePort, "select", id); err != nil {
			return nil, err
		}
		ports, err := decodeOVSPorts(results)
		if err != nil {
			return nil, err
		}
		rows = append(rows, ports...)
	}
	return rows, nil
}

func decodeOVSPorts(results []libovsdb.OperationResult) ([]OVSPort, error) {
	if len(results) == 0 {
		return nil, nil
	}
	rows := make([]OVSPort, 0, len(results[0].Rows))
	for _, row := range results[0].Rows {
		rows = append(rows, OVSPort{
			UUID:        rowUUIDValue(row),
			Name:        rowStringValue(row, colName),
			Interfaces:  rowUUIDSliceValue(row, colInterfaces),
			ExternalIDs: rowStringMapValue(row, colExternalIDs),
		})
	}
	return rows, nil
}

func (o *OVSClient) openVSwitchUUID(ctx context.Context) (string, error) {
	results, err := o.db.raw.Transact(ctx, libovsdb.Operation{
		Op:      libovsdb.OperationSelect,
		Table:   tableOpenVSwitch,
		Where:   []libovsdb.Condition{},
		Columns: []string{colUUID},
	})
	if err != nil {
		return "", classifyTransactError(err, dbOpenVSwitch, tableOpenVSwitch, "select", "")
	}
	if err := checkOperationResults(results, dbOpenVSwitch, tableOpenVSwitch, "select", ""); err != nil {
		return "", err
	}
	if len(results) == 0 || len(results[0].Rows) == 0 {
		return "", wrap(ErrorNotFound, dbOpenVSwitch, tableOpenVSwitch, "select", "", "Open_vSwitch root row not found", nil)
	}
	uuid := rowUUIDValue(results[0].Rows[0])
	if uuid == "" {
		return "", wrap(ErrorConflict, dbOpenVSwitch, tableOpenVSwitch, "select", "", "Open_vSwitch root UUID missing", nil)
	}
	return uuid, nil
}

type OVSPortBuilder struct {
	parent *BridgeBuilder
	spec   *ovsPortSpec
}

func (p *OVSPortBuilder) WithInterfaceName(name string) *OVSPortBuilder {
	p.spec.interfaceName = name
	return p
}

func (p *OVSPortBuilder) WithInterfaceType(kind string) *OVSPortBuilder {
	p.spec.interfaceType = kind
	return p
}

func (p *OVSPortBuilder) WithExternalID(key, value string) *OVSPortBuilder {
	if p.spec.externalIDs == nil {
		p.spec.externalIDs = map[string]string{}
	}
	p.spec.externalIDs[key] = value
	return p
}

func (p *OVSPortBuilder) Execute(ctx context.Context) error {
	return p.parent.Execute(ctx)
}

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
	once              useOnce
	client            *OVSClient
	name              string
	mode              ovsMode
	port              *ovsPortSpec
	deletePort        string
	externalIDs       map[string]string
	controllerTargets []string
	failMode          string
	datapathType      string
}

type ovsPortSpec struct {
	name                 string
	interfaceName        string
	interfaceType        string
	externalIDs          map[string]string
	options              map[string]string
	interfaceOptions     map[string]string
	interfaceExternalIDs map[string]string
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
		for key := range b.port.options {
			if err := validateExternalID(key); err != nil {
				return err
			}
		}
		for key := range b.port.interfaceExternalIDs {
			if err := validateExternalID(key); err != nil {
				return err
			}
		}
		for key := range b.port.interfaceOptions {
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
	for _, target := range b.controllerTargets {
		if err := validateName("controller target", target); err != nil {
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
	var controllerUUIDs []string
	var controllerOps []libovsdb.Operation
	if b.client.db.schema.HasTable(tableController) && b.client.db.schema.HasColumn(tableBridge, colController) {
		controllerUUIDs, controllerOps, err = b.controllerOps(ctx)
		if err != nil {
			return err
		}
	}

	if b.port != nil {
		existingPorts, err := b.client.selectPorts(ctx, b.port.name)
		if err != nil {
			return err
		}
		if len(existingPorts) > 0 {
			if bridgeNeedsCreate {
				ops = append(ops, b.insertBridgeOp(controllerUUIDs, existingPorts[0].UUID))
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
			if len(b.port.options) > 0 && b.client.db.schema.HasColumn(tablePort, colOptions) {
				portMutations = append(portMutations, *libovsdb.NewMutation(colOptions, libovsdb.MutateOperationInsert, ovsMap(b.port.options)))
			}
			if len(portMutations) > 0 {
				ops = append(ops, libovsdb.Operation{
					Op:        libovsdb.OperationMutate,
					Table:     tablePort,
					Where:     conditionUUID(existingPorts[0].UUID),
					Mutations: portMutations,
				})
			}
			if (b.port.interfaceType != "" || len(b.port.interfaceOptions) > 0 || len(b.port.interfaceExternalIDs) > 0) && len(existingPorts[0].Interfaces) > 0 {
				for _, ifaceUUID := range existingPorts[0].Interfaces {
					row := libovsdb.Row{}
					if b.port.interfaceType != "" {
						row[colType] = b.port.interfaceType
					}
					if len(row) > 0 {
						ops = append(ops, libovsdb.Operation{
							Op:    libovsdb.OperationUpdate,
							Table: tableInterface,
							Where: conditionUUID(ifaceUUID),
							Row:   row,
						})
					}
					var ifaceMutations []libovsdb.Mutation
					if len(b.port.interfaceOptions) > 0 && b.client.db.schema.HasColumn(tableInterface, colOptions) {
						ifaceMutations = append(ifaceMutations, *libovsdb.NewMutation(colOptions, libovsdb.MutateOperationInsert, ovsMap(b.port.interfaceOptions)))
					}
					interfaceExternalIDs := b.port.interfaceExternalIDs
					if len(interfaceExternalIDs) == 0 {
						interfaceExternalIDs = b.port.externalIDs
					}
					if len(interfaceExternalIDs) > 0 {
						ifaceMutations = append(ifaceMutations, *libovsdb.NewMutation(colExternalIDs, libovsdb.MutateOperationInsert, ovsMap(interfaceExternalIDs)))
					}
					if len(ifaceMutations) > 0 {
						ops = append(ops, libovsdb.Operation{
							Op:        libovsdb.OperationMutate,
							Table:     tableInterface,
							Where:     conditionUUID(ifaceUUID),
							Mutations: ifaceMutations,
						})
					}
				}
			}
		} else {
			ifaceUUID := namedUUID("iface")
			portUUID := namedUUID("port")
			ifaceRow := libovsdb.Row{
				colName: b.port.interfaceName,
			}
			interfaceExternalIDs := b.port.interfaceExternalIDs
			if len(interfaceExternalIDs) == 0 {
				interfaceExternalIDs = b.port.externalIDs
			}
			setRowMap(ifaceRow, colExternalIDs, interfaceExternalIDs)
			if b.client.db.schema.HasColumn(tableInterface, colOptions) {
				setRowMap(ifaceRow, colOptions, b.port.interfaceOptions)
			}
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
			if b.client.db.schema.HasColumn(tablePort, colOptions) {
				setRowMap(portRow, colOptions, b.port.options)
			}
			ops = append(ops, libovsdb.Operation{
				Op:       libovsdb.OperationInsert,
				Table:    tablePort,
				UUIDName: portUUID,
				Row:      portRow,
			})
			if bridgeNeedsCreate {
				ops = append(ops, b.insertBridgeOp(controllerUUIDs, portUUID))
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
	}

	ops = append(ops, controllerOps...)
	if len(controllerUUIDs) > 0 && !bridgeNeedsCreate && len(bridges) > 0 {
		ops = append(ops, libovsdb.Operation{
			Op:    libovsdb.OperationMutate,
			Table: tableBridge,
			Where: conditionUUID(bridges[0].UUID),
			Mutations: []libovsdb.Mutation{
				*libovsdb.NewMutation(colController, libovsdb.MutateOperationInsert, uuidSet(controllerUUIDs...)),
			},
		})
	}

	if bridgeNeedsCreate && b.port == nil {
		ops = append(ops, b.insertBridgeOp(controllerUUIDs))
	} else if !bridgeNeedsCreate && len(bridges) > 0 && len(b.externalIDs) > 0 {
		ops = append(ops, libovsdb.Operation{
			Op:    libovsdb.OperationMutate,
			Table: tableBridge,
			Where: conditionUUID(bridges[0].UUID),
			Mutations: []libovsdb.Mutation{
				*libovsdb.NewMutation(colExternalIDs, libovsdb.MutateOperationInsert, ovsMap(b.externalIDs)),
			},
		})
	}

	if !bridgeNeedsCreate && len(bridges) > 0 {
		updateRow := libovsdb.Row{}
		if b.failMode != "" && b.client.db.schema.HasColumn(tableBridge, colFailMode) {
			updateRow[colFailMode] = b.failMode
		}
		if b.datapathType != "" && b.client.db.schema.HasColumn(tableBridge, colDatapathType) {
			updateRow[colDatapathType] = b.datapathType
		}
		if len(updateRow) > 0 {
			ops = append(ops, libovsdb.Operation{
				Op:    libovsdb.OperationUpdate,
				Table: tableBridge,
				Where: conditionUUID(bridges[0].UUID),
				Row:   updateRow,
			})
		}
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
	results, err := b.client.db.executor.Transact(ctx, ops...)
	if err != nil {
		return classifyTransactError(err, dbOpenVSwitch, tableBridge, string(b.mode), b.name)
	}
	return ensureAffected(results, mustAffectNonInsertOps(ops), dbOpenVSwitch, tableBridge, string(b.mode), b.name)
}

func (b *BridgeBuilder) insertBridgeOp(controllerNamedUUIDs []string, portNamedUUIDs ...string) libovsdb.Operation {
	bridgeUUID := namedUUID("bridge")
	row := libovsdb.Row{
		colName: b.name,
	}
	setRowMap(row, colExternalIDs, b.externalIDs)
	if b.failMode != "" && b.client.db.schema.HasColumn(tableBridge, colFailMode) {
		row[colFailMode] = b.failMode
	}
	if b.datapathType != "" && b.client.db.schema.HasColumn(tableBridge, colDatapathType) {
		row[colDatapathType] = b.datapathType
	}
	if len(portNamedUUIDs) > 0 {
		row[colPorts] = uuidSet(portNamedUUIDs...)
	}
	if len(controllerNamedUUIDs) > 0 {
		row[colController] = uuidSet(controllerNamedUUIDs...)
	}
	return libovsdb.Operation{
		Op:       libovsdb.OperationInsert,
		Table:    tableBridge,
		UUIDName: bridgeUUID,
		Row:      row,
	}
}

func (b *BridgeBuilder) controllerOps(ctx context.Context) ([]string, []libovsdb.Operation, error) {
	targets := uniqueStrings(b.controllerTargets)
	controllerUUIDs := make([]string, 0, len(targets))
	ops := make([]libovsdb.Operation, 0, len(targets))
	for _, target := range targets {
		if target == "" {
			continue
		}
		existing, err := b.client.selectControllers(ctx, target)
		if err != nil {
			return nil, nil, err
		}
		if len(existing) > 0 {
			controllerUUIDs = append(controllerUUIDs, existing[0].UUID)
			continue
		}
		controllerUUID := namedUUID("controller")
		controllerUUIDs = append(controllerUUIDs, controllerUUID)
		ops = append(ops, libovsdb.Operation{
			Op:       libovsdb.OperationInsert,
			Table:    tableController,
			UUIDName: controllerUUID,
			Row: libovsdb.Row{
				colTarget: target,
			},
		})
	}
	return controllerUUIDs, ops, nil
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
	allBridges, err := b.client.selectAllBridges(ctx)
	if err != nil {
		return err
	}
	allPorts, err := b.client.selectAllPorts(ctx)
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
		if portUsedByOtherBridges(allBridges, bridges[0].UUID, port.UUID) {
			continue
		}
		ops = append(ops, libovsdb.Operation{
			Op:    libovsdb.OperationDelete,
			Table: tablePort,
			Where: conditionUUID(port.UUID),
		})
		for _, ifaceUUID := range port.Interfaces {
			if interfaceUsedByOtherPorts(allPorts, port.UUID, ifaceUUID) {
				continue
			}
			ops = append(ops, libovsdb.Operation{
				Op:    libovsdb.OperationDelete,
				Table: tableInterface,
				Where: conditionUUID(ifaceUUID),
			})
		}
	}
	results, err := b.client.db.executor.Transact(ctx, ops...)
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
	allPorts, err := b.client.selectAllPorts(ctx)
	if err != nil {
		return err
	}
	mirrorUUIDs, err := b.client.selectBridgeMirrorsForPort(ctx, bridges[0], portUUID)
	if err != nil {
		return err
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
	for _, mirrorUUID := range mirrorUUIDs {
		ops = append(ops, libovsdb.Operation{
			Op:    libovsdb.OperationMutate,
			Table: tableBridge,
			Where: conditionUUID(bridges[0].UUID),
			Mutations: []libovsdb.Mutation{
				*libovsdb.NewMutation(colMirrors, libovsdb.MutateOperationDelete, uuidSet(mirrorUUID)),
			},
		})
	}
	for _, ifaceUUID := range ports[0].Interfaces {
		if interfaceUsedByOtherPorts(allPorts, portUUID, ifaceUUID) {
			continue
		}
		ops = append(ops, libovsdb.Operation{
			Op:    libovsdb.OperationDelete,
			Table: tableInterface,
			Where: conditionUUID(ifaceUUID),
		})
	}
	results, err := b.client.db.executor.Transact(ctx, ops...)
	if err != nil {
		return classifyTransactError(err, dbOpenVSwitch, tablePort, "delete", b.deletePort)
	}
	return ensureAffected(results, mustAffect, dbOpenVSwitch, tablePort, "delete", b.deletePort)
}

func (o *OVSClient) selectBridges(ctx context.Context, name string) ([]OVSBridge, error) {
	results, err := o.db.executor.Transact(ctx, libovsdb.Operation{
		Op:    libovsdb.OperationSelect,
		Table: tableBridge,
		Where: conditionName(name),
		Columns: o.db.schema.existingColumns(tableBridge,
			colUUID, colName, colPorts, colController, colMirrors, colNetFlow, colSFlow, colIPFIX, colFlowTables, colAutoAttach, colExternalIDs, colOtherConfig,
		),
	})
	if err != nil {
		return nil, classifyTransactError(err, dbOpenVSwitch, tableBridge, "select", name)
	}
	if err := checkOperationResults(results, dbOpenVSwitch, tableBridge, "select", name); err != nil {
		return nil, err
	}
	return decodeOVSBridges(results), nil
}

func (o *OVSClient) selectAllBridges(ctx context.Context) ([]OVSBridge, error) {
	results, err := o.db.executor.Transact(ctx, libovsdb.Operation{
		Op:    libovsdb.OperationSelect,
		Table: tableBridge,
		Where: []libovsdb.Condition{},
		Columns: o.db.schema.existingColumns(tableBridge,
			colUUID, colName, colPorts, colController, colMirrors, colNetFlow, colSFlow, colIPFIX, colFlowTables, colAutoAttach, colExternalIDs, colOtherConfig,
		),
	})
	if err != nil {
		return nil, classifyTransactError(err, dbOpenVSwitch, tableBridge, "select", "")
	}
	if err := checkOperationResults(results, dbOpenVSwitch, tableBridge, "select", ""); err != nil {
		return nil, err
	}
	return decodeOVSBridges(results), nil
}

func decodeOVSBridges(results []libovsdb.OperationResult) []OVSBridge {
	if len(results) == 0 {
		return nil
	}
	rows := make([]OVSBridge, 0, len(results[0].Rows))
	for _, row := range results[0].Rows {
		rows = append(rows, OVSBridge{
			UUID:        rowUUIDValue(row),
			Name:        rowStringValue(row, colName),
			Ports:       rowUUIDSliceValue(row, colPorts),
			Controllers: rowUUIDSliceValue(row, colController),
			Mirrors:     rowUUIDSliceValue(row, colMirrors),
			NetFlow:     rowOptionalUUIDValue(row, colNetFlow),
			SFlow:       rowOptionalUUIDValue(row, colSFlow),
			IPFIX:       rowOptionalUUIDValue(row, colIPFIX),
			FlowTables:  rowIntUUIDMapValue(row, colFlowTables),
			AutoAttach:  rowOptionalUUIDValue(row, colAutoAttach),
			ExternalIDs: rowStringMapValue(row, colExternalIDs),
			OtherConfig: rowStringMapValue(row, colOtherConfig),
		})
	}
	return rows
}

func (o *OVSClient) selectBridgeMirrorsForPort(ctx context.Context, bridge OVSBridge, portUUID string) ([]string, error) {
	if len(bridge.Mirrors) == 0 || !o.db.schema.HasTable(tableMirror) {
		return nil, nil
	}
	var out []string
	for _, mirrorUUID := range uniqueStrings(bridge.Mirrors) {
		results, err := o.db.executor.Transact(ctx, libovsdb.Operation{
			Op:      libovsdb.OperationSelect,
			Table:   tableMirror,
			Where:   conditionUUID(mirrorUUID),
			Columns: o.db.schema.existingColumns(tableMirror, colUUID, colSelectSrcPort, colSelectDstPort, colOutputPort),
		})
		if err != nil {
			return nil, classifyTransactError(err, dbOpenVSwitch, tableMirror, "select", mirrorUUID)
		}
		if err := checkOperationResults(results, dbOpenVSwitch, tableMirror, "select", mirrorUUID); err != nil {
			return nil, err
		}
		if len(results) == 0 || len(results[0].Rows) == 0 {
			continue
		}
		row := results[0].Rows[0]
		if containsString(rowUUIDSliceValue(row, colSelectSrcPort), portUUID) ||
			containsString(rowUUIDSliceValue(row, colSelectDstPort), portUUID) ||
			containsString(rowUUIDSliceValue(row, colOutputPort), portUUID) {
			out = append(out, mirrorUUID)
		}
	}
	return uniqueStrings(out), nil
}

func qosUsedByOtherPorts(ports []OVSPort, deletedPortUUID, qosUUID string) bool {
	for _, port := range ports {
		if port.UUID == deletedPortUUID || port.QoS == nil {
			continue
		}
		if *port.QoS == qosUUID {
			return true
		}
	}
	return false
}

func portUsedByOtherBridges(bridges []OVSBridge, deletedBridgeUUID, portUUID string) bool {
	for _, bridge := range bridges {
		if bridge.UUID == deletedBridgeUUID {
			continue
		}
		if containsString(bridge.Ports, portUUID) {
			return true
		}
	}
	return false
}

func interfaceUsedByOtherPorts(ports []OVSPort, deletedPortUUID, ifaceUUID string) bool {
	for _, port := range ports {
		if port.UUID == deletedPortUUID {
			continue
		}
		if containsString(port.Interfaces, ifaceUUID) {
			return true
		}
	}
	return false
}

func (o *OVSClient) selectPorts(ctx context.Context, name string) ([]OVSPort, error) {
	results, err := o.db.executor.Transact(ctx, libovsdb.Operation{
		Op:      libovsdb.OperationSelect,
		Table:   tablePort,
		Where:   conditionName(name),
		Columns: o.db.schema.existingColumns(tablePort, colUUID, colName, colInterfaces, colQoS, colExternalIDs, colOtherConfig),
	})
	if err != nil {
		return nil, classifyTransactError(err, dbOpenVSwitch, tablePort, "select", name)
	}
	if err := checkOperationResults(results, dbOpenVSwitch, tablePort, "select", name); err != nil {
		return nil, err
	}
	return decodeOVSPorts(results)
}

func (o *OVSClient) selectAllPorts(ctx context.Context) ([]OVSPort, error) {
	results, err := o.db.executor.Transact(ctx, libovsdb.Operation{
		Op:      libovsdb.OperationSelect,
		Table:   tablePort,
		Where:   []libovsdb.Condition{},
		Columns: o.db.schema.existingColumns(tablePort, colUUID, colName, colInterfaces, colQoS, colExternalIDs, colOtherConfig),
	})
	if err != nil {
		return nil, classifyTransactError(err, dbOpenVSwitch, tablePort, "select", "")
	}
	if err := checkOperationResults(results, dbOpenVSwitch, tablePort, "select", ""); err != nil {
		return nil, err
	}
	return decodeOVSPorts(results)
}

func (o *OVSClient) selectControllers(ctx context.Context, target string) ([]OVSController, error) {
	results, err := o.db.executor.Transact(ctx, libovsdb.Operation{
		Op:      libovsdb.OperationSelect,
		Table:   tableController,
		Where:   []libovsdb.Condition{libovsdb.NewCondition(colTarget, libovsdb.ConditionEqual, target)},
		Columns: o.db.schema.existingColumns(tableController, colUUID, colTarget, colExternalIDs, colOtherConfig),
	})
	if err != nil {
		return nil, classifyTransactError(err, dbOpenVSwitch, tableController, "select", target)
	}
	if err := checkOperationResults(results, dbOpenVSwitch, tableController, "select", target); err != nil {
		return nil, err
	}
	return decodeOVSControllers(results), nil
}

func decodeOVSControllers(results []libovsdb.OperationResult) []OVSController {
	if len(results) == 0 {
		return nil
	}
	rows := make([]OVSController, 0, len(results[0].Rows))
	for _, row := range results[0].Rows {
		rows = append(rows, OVSController{
			UUID:        rowUUIDValue(row),
			Target:      rowStringValue(row, colTarget),
			ExternalIDs: rowStringMapValue(row, colExternalIDs),
			OtherConfig: rowStringMapValue(row, colOtherConfig),
		})
	}
	return rows
}

func (o *OVSClient) selectPortsByUUID(ctx context.Context, ids []string) ([]OVSPort, error) {
	var rows []OVSPort
	for _, id := range uniqueStrings(ids) {
		results, err := o.db.executor.Transact(ctx, libovsdb.Operation{
			Op:      libovsdb.OperationSelect,
			Table:   tablePort,
			Where:   conditionUUID(id),
			Columns: o.db.schema.existingColumns(tablePort, colUUID, colName, colInterfaces, colQoS, colExternalIDs, colOtherConfig),
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
			QoS:         rowOptionalUUIDValue(row, colQoS),
			ExternalIDs: rowStringMapValue(row, colExternalIDs),
			OtherConfig: rowStringMapValue(row, colOtherConfig),
		})
	}
	return rows, nil
}

func (o *OVSClient) openVSwitchUUID(ctx context.Context) (string, error) {
	results, err := o.db.executor.Transact(ctx, libovsdb.Operation{
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

package ovnflow

import (
	"context"

	libovsdb "github.com/ovn-kubernetes/libovsdb/ovsdb"
)

// Table exposes the full runtime Open_vSwitch schema through the fluent API.
func (o *OVSClient) Table(table string) *TableRef {
	return o.db.Table(table)
}

// TableBy exposes a runtime Open_vSwitch table row selected by column=value.
func (o *OVSClient) TableBy(table, column, value string) *TableRef {
	return o.db.TableBy(table, column, value)
}

func (o *OVSClient) OpenVSwitch() *TableRef {
	return o.Table(tableOpenVSwitch)
}

func (o *OVSClient) Port(name string) *TableRef {
	return o.TableBy(tablePort, colName, name)
}

func (o *OVSClient) Interface(name string) *TableRef {
	return o.TableBy(tableInterface, colName, name)
}

func (o *OVSClient) Controller(target string) *TableRef {
	return o.TableBy(tableController, colTarget, target)
}

func (o *OVSClient) Manager(target string) *TableRef {
	return o.TableBy(tableManager, colTarget, target)
}

func (o *OVSClient) Mirror(name string) *TableRef {
	return o.TableBy(tableMirror, colName, name)
}

func (o *OVSClient) QoS(name string) *TableRef {
	return o.namedByExternalID(tableQoS, name)
}

func (o *OVSClient) Queue(name string) *TableRef {
	return o.namedByExternalID(tableQueue, name)
}

func (o *OVSClient) FlowTable(name string) *TableRef {
	return o.TableBy(tableFlowTable, colName, name)
}

func (o *OVSClient) NetFlow(name string) *TableRef {
	return o.namedByExternalID(tableNetFlow, name)
}

func (o *OVSClient) SFlow(name string) *TableRef {
	return o.namedByExternalID(tableSFlow, name)
}

func (o *OVSClient) IPFIX(name string) *TableRef {
	return o.namedByExternalID(tableIPFIX, name)
}

func (o *OVSClient) SSL() *TableRef {
	return o.Table(tableSSL)
}

func (o *OVSClient) AutoAttach(systemName string) *TableRef {
	return o.TableBy(tableAutoAttach, "system_name", systemName)
}

func (o *OVSClient) namedByExternalID(table, name string) *TableRef {
	return o.Table(table).
		WhereCondition(colExternalIDs, libovsdb.ConditionIncludes, ovsMap(map[string]string{"name": name})).
		withDefaultMap(colExternalIDs, map[string]string{"name": name})
}

func (o *OVSClient) GetBridge(ctx context.Context, name string) (*OVSBridge, error) {
	rows, err := o.selectBridges(ctx, name)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, wrap(ErrorNotFound, dbOpenVSwitch, tableBridge, "get", name, "bridge not found", nil)
	}
	return &rows[0], nil
}

func (o *OVSClient) ListBridges(ctx context.Context) ([]OVSBridge, error) {
	rows, err := o.Table(tableBridge).List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]OVSBridge, 0, len(rows))
	for _, row := range rows {
		out = append(out, OVSBridge{
			UUID:        anyString(row[colUUID]),
			Name:        anyString(row[colName]),
			Ports:       anyStringSlice(row[colPorts]),
			Controllers: anyStringSlice(row[colController]),
			Mirrors:     anyStringSlice(row[colMirrors]),
			ExternalIDs: anyStringMap(row[colExternalIDs]),
			OtherConfig: anyStringMap(row[colOtherConfig]),
		})
	}
	return out, nil
}

func (o *OVSClient) GetPort(ctx context.Context, name string) (*OVSPort, error) {
	rows, err := o.selectPorts(ctx, name)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, wrap(ErrorNotFound, dbOpenVSwitch, tablePort, "get", name, "port not found", nil)
	}
	return &rows[0], nil
}

func (o *OVSClient) ListPorts(ctx context.Context) ([]OVSPort, error) {
	rows, err := o.Table(tablePort).List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]OVSPort, 0, len(rows))
	for _, row := range rows {
		out = append(out, OVSPort{
			UUID:        anyString(row[colUUID]),
			Name:        anyString(row[colName]),
			Interfaces:  anyStringSlice(row[colInterfaces]),
			QoS:         anyOptionalString(row[colQoS]),
			ExternalIDs: anyStringMap(row[colExternalIDs]),
			OtherConfig: anyStringMap(row[colOtherConfig]),
		})
	}
	return out, nil
}

func (o *OVSClient) GetInterface(ctx context.Context, name string) (*OVSInterface, error) {
	row, err := o.Interface(name).Get(ctx)
	if err != nil {
		return nil, err
	}
	return &OVSInterface{
		UUID:        anyString(row[colUUID]),
		Name:        anyString(row[colName]),
		Type:        anyString(row[colType]),
		Options:     anyStringMap(row[colOptions]),
		ExternalIDs: anyStringMap(row[colExternalIDs]),
		OtherConfig: anyStringMap(row[colOtherConfig]),
	}, nil
}

func (o *OVSClient) ListInterfaces(ctx context.Context) ([]OVSInterface, error) {
	rows, err := o.Table(tableInterface).List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]OVSInterface, 0, len(rows))
	for _, row := range rows {
		out = append(out, OVSInterface{
			UUID:        anyString(row[colUUID]),
			Name:        anyString(row[colName]),
			Type:        anyString(row[colType]),
			Options:     anyStringMap(row[colOptions]),
			ExternalIDs: anyStringMap(row[colExternalIDs]),
			OtherConfig: anyStringMap(row[colOtherConfig]),
		})
	}
	return out, nil
}

func (o *OVSClient) WatchTable(ctx context.Context, table string) (<-chan RowEvent, <-chan error) {
	return o.Table(table).Watch(ctx)
}

func (o *OVSClient) WatchBridges(ctx context.Context) (<-chan RowEvent, <-chan error) {
	return o.WatchTable(ctx, tableBridge)
}

func (o *OVSClient) WatchPorts(ctx context.Context) (<-chan RowEvent, <-chan error) {
	return o.WatchTable(ctx, tablePort)
}

func (o *OVSClient) WatchInterfaces(ctx context.Context) (<-chan RowEvent, <-chan error) {
	return o.WatchTable(ctx, tableInterface)
}

func (b *BridgeBuilder) WithControllerTarget(target string) *BridgeBuilder {
	b.controllerTargets = append(b.controllerTargets, target)
	return b
}

func (b *BridgeBuilder) WithFailMode(mode string) *BridgeBuilder {
	b.failMode = mode
	return b
}

func (b *BridgeBuilder) WithDatapathType(kind string) *BridgeBuilder {
	b.datapathType = kind
	return b
}

func (p *OVSPortBuilder) WithOption(key, value string) *OVSPortBuilder {
	if p.spec.options == nil {
		p.spec.options = map[string]string{}
	}
	p.spec.options[key] = value
	return p
}

func (p *OVSPortBuilder) WithInterfaceOption(key, value string) *OVSPortBuilder {
	if p.spec.interfaceOptions == nil {
		p.spec.interfaceOptions = map[string]string{}
	}
	p.spec.interfaceOptions[key] = value
	return p
}

func (p *OVSPortBuilder) WithInterfaceExternalID(key, value string) *OVSPortBuilder {
	if p.spec.interfaceExternalIDs == nil {
		p.spec.interfaceExternalIDs = map[string]string{}
	}
	p.spec.interfaceExternalIDs[key] = value
	return p
}

func (b *TableBuilder) WithController(target string) *TableBuilder {
	return b.WithTarget(target)
}

func (b *TableBuilder) WithManager(target string) *TableBuilder {
	return b.WithTarget(target)
}

func (b *TableBuilder) WithQueueDSCP(dscp int) *TableBuilder {
	return b.WithOptionalColumn("dscp", dscp)
}

func (b *TableBuilder) WithQueueOtherConfig(key, value string) *TableBuilder {
	return b.MutateMap(colOtherConfig, map[string]string{key: value})
}

func (b *TableBuilder) WithQoSType(kind string) *TableBuilder {
	return b.WithType(kind)
}

func (b *TableBuilder) WithMirrorSelectAll() *TableBuilder {
	return b.WithColumn("select_all", true)
}

func (b *TableBuilder) WithSamplingTarget(target string) *TableBuilder {
	return b.WithColumn("targets", ovsSet(target))
}

func anyOptionalString(value any) *string {
	s := anyString(value)
	if s == "" {
		values := anyStringSlice(value)
		if len(values) == 0 {
			return nil
		}
		s = values[0]
	}
	return &s
}

func (b *TableBuilder) executeOVSDelete(ctx context.Context) error {
	if err := b.ref.validateIdentity(string(b.mode)); err != nil {
		return err
	}
	rows, err := b.ref.selectRows(ctx, b.ref.identityConditions(), []string{colUUID})
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return wrap(ErrorNotFound, dbOpenVSwitch, b.ref.table, string(b.mode), b.ref.identityValue, "row not found", nil)
	}

	var ops []libovsdb.Operation
	var mustAffect []int
	for _, row := range rows {
		id := anyString(row[colUUID])
		if id == "" {
			return wrap(ErrorConflict, dbOpenVSwitch, b.ref.table, string(b.mode), b.ref.identityValue, "row UUID missing", nil)
		}
		refOps, err := b.ref.db.ovsUnreferenceOps(ctx, b.ref.table, id)
		if err != nil {
			return err
		}
		ops = append(ops, refOps...)
		ops = append(ops, libovsdb.Operation{
			Op:    libovsdb.OperationDelete,
			Table: b.ref.table,
			Where: conditionUUID(id),
		})
		mustAffect = append(mustAffect, len(ops)-1)
	}
	results, err := b.ref.db.transact(ctx, b.ref.table, string(b.mode), b.ref.identityValue, ops...)
	if err != nil {
		return err
	}
	return ensureAffected(results, mustAffect, dbOpenVSwitch, b.ref.table, string(b.mode), b.ref.identityValue)
}

func (d *dbClient) ovsUnreferenceOps(ctx context.Context, targetTable, targetUUID string) ([]libovsdb.Operation, error) {
	var ops []libovsdb.Operation
	for tableName := range d.schema.schema.Tables {
		if tableName == targetTable {
			continue
		}
		for _, column := range d.schema.ReferenceColumns(tableName, targetTable) {
			columnSchema := d.schema.column(tableName, column)
			if columnSchema != nil && columnSchema.Type == libovsdb.TypeMap {
				rows, err := newTableRef(d, tableName, "", "").selectRows(ctx, nil, []string{colUUID, column})
				if err != nil {
					return nil, err
				}
				for _, row := range rows {
					referrerUUID := anyString(row[colUUID])
					deleteKeys := ovsMapDeleteKeysForUUID(row[column], targetUUID)
					if referrerUUID == "" || len(deleteKeys) == 0 {
						continue
					}
					ops = append(ops, ovsUnreferenceMapOp(tableName, column, referrerUUID, deleteKeys...))
				}
				continue
			}
			rows, err := newTableRef(d, tableName, "", "").selectRows(ctx,
				[]libovsdb.Condition{libovsdb.NewCondition(column, libovsdb.ConditionIncludes, uuidValue(targetUUID))},
				[]string{colUUID},
			)
			if err != nil {
				return nil, err
			}
			for _, row := range rows {
				referrerUUID := anyString(row[colUUID])
				if referrerUUID == "" {
					continue
				}
				ops = append(ops, ovsUnreferenceUUIDOp(tableName, column, referrerUUID, targetUUID))
			}
		}
	}
	return ops, nil
}

func ovsUnreferenceUUIDOp(tableName, column, referrerUUID, targetUUID string) libovsdb.Operation {
	return libovsdb.Operation{
		Op:    libovsdb.OperationMutate,
		Table: tableName,
		Where: conditionUUID(referrerUUID),
		Mutations: []libovsdb.Mutation{
			*libovsdb.NewMutation(column, libovsdb.MutateOperationDelete, uuidSet(targetUUID)),
		},
	}
}

func ovsUnreferenceMapOp(tableName, column, referrerUUID string, keys ...any) libovsdb.Operation {
	return libovsdb.Operation{
		Op:    libovsdb.OperationMutate,
		Table: tableName,
		Where: conditionUUID(referrerUUID),
		Mutations: []libovsdb.Mutation{
			*libovsdb.NewMutation(column, libovsdb.MutateOperationDelete, ovsSet(keys...)),
		},
	}
}

func ovsMapDeleteKeysForUUID(value any, targetUUID string) []any {
	var keys []any
	switch typed := value.(type) {
	case libovsdb.OvsMap:
		for key, value := range typed.GoMap {
			if s, ok := anyUUIDString(value); ok && s == targetUUID {
				keys = append(keys, key)
			}
		}
	case []any:
		if len(typed) != 2 || typed[0] != "map" {
			return nil
		}
		pairs, ok := typed[1].([]any)
		if !ok {
			return nil
		}
		for _, item := range pairs {
			pair, ok := item.([]any)
			if !ok || len(pair) != 2 {
				continue
			}
			if s, ok := anyUUIDString(pair[1]); ok && s == targetUUID {
				keys = append(keys, ovsMapMutationKey(pair[0]))
			}
		}
	}
	return keys
}

func ovsMapMutationKey(value any) any {
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case []any:
		if s, ok := anyUUIDString(typed); ok {
			return uuidValue(s)
		}
	}
	return value
}

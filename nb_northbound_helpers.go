package ovnflow

import (
	"context"
	"strconv"

	libovsdb "github.com/ovn-kubernetes/libovsdb/ovsdb"
)

func nbBuilderUsed(table, op, object string) error {
	return wrap(ErrorValidation, dbOVNNorthbound, table, op, object, "builder already executed", nil)
}

func (n *NBClient) selectRows(ctx context.Context, table string, where []libovsdb.Condition, columns []string, object string) ([]libovsdb.Row, error) {
	if err := n.db.schema.RequireTable(table); err != nil {
		return nil, err
	}
	if err := n.db.schema.RequireConditionColumns(table, where...); err != nil {
		return nil, err
	}
	columns = n.supportedColumns(table, columns)
	results, err := n.db.transact(ctx, table, "select", object, libovsdb.Operation{
		Op:      libovsdb.OperationSelect,
		Table:   table,
		Where:   where,
		Columns: columns,
	})
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, nil
	}
	return results[0].Rows, nil
}

func (n *NBClient) selectFirstUUID(ctx context.Context, table string, where []libovsdb.Condition, object string) (string, bool, error) {
	rows, err := n.selectRows(ctx, table, where, []string{colUUID}, object)
	if err != nil {
		return "", false, err
	}
	if len(rows) == 0 {
		return "", false, nil
	}
	uuid := rowUUIDValue(rows[0])
	if uuid == "" {
		return "", false, wrap(ErrorConflict, dbOVNNorthbound, table, "select", object, "row UUID missing", nil)
	}
	return uuid, true, nil
}

func (n *NBClient) executeNamed(ctx context.Context, table, name string, mode nbMode, row libovsdb.Row, mutations []libovsdb.Mutation) error {
	return n.executeByConditions(ctx, table, name, mode, conditionName(name), row, mutations)
}

func (n *NBClient) executeNamedWithPreOps(ctx context.Context, table, name string, mode nbMode, row libovsdb.Row, mutations []libovsdb.Mutation, preOps []libovsdb.Operation) error {
	return n.executeByConditionsWithPreOps(ctx, table, name, mode, conditionName(name), row, mutations, preOps)
}

func (n *NBClient) executeNamedWithPrePostOps(ctx context.Context, table, name string, mode nbMode, row libovsdb.Row, mutations []libovsdb.Mutation, preOps []libovsdb.Operation, postOps func(string) []libovsdb.Operation) error {
	return n.executeByConditionsWithPrePostOps(ctx, table, name, mode, conditionName(name), row, mutations, preOps, postOps)
}

func (n *NBClient) executeByConditions(ctx context.Context, table, object string, mode nbMode, conditions []libovsdb.Condition, row libovsdb.Row, mutations []libovsdb.Mutation) error {
	return n.executeByConditionsWithPreOps(ctx, table, object, mode, conditions, row, mutations, nil)
}

func (n *NBClient) executeByConditionsWithPreOps(ctx context.Context, table, object string, mode nbMode, conditions []libovsdb.Condition, row libovsdb.Row, mutations []libovsdb.Mutation, preOps []libovsdb.Operation) error {
	return n.executeByConditionsWithPrePostOps(ctx, table, object, mode, conditions, row, mutations, preOps, nil)
}

func (n *NBClient) executeByConditionsWithPrePostOps(ctx context.Context, table, object string, mode nbMode, conditions []libovsdb.Condition, row libovsdb.Row, mutations []libovsdb.Mutation, preOps []libovsdb.Operation, postOps func(string) []libovsdb.Operation) error {
	if err := n.db.schema.RequireTable(table); err != nil {
		return err
	}
	if err := n.db.schema.RequireConditionColumns(table, conditions...); err != nil {
		return err
	}
	row = n.supportedRow(table, row)
	mutations = n.supportedMutations(table, mutations)
	preOps = n.supportedPreOps(preOps)
	switch mode {
	case nbModeCreate:
		return n.executeInsertWithPrePostOps(ctx, table, object, string(mode), row, preOps, postOps)
	case nbModeEnsure:
		rows, err := n.selectRows(ctx, table, conditions, []string{colUUID}, object)
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			err := n.executeInsertWithPrePostOps(ctx, table, object, string(mode), row, preOps, postOps)
			if err == nil {
				return nil
			}
			if IsKind(err, ErrorAlreadyExists) {
				rows, selectErr := n.selectRows(ctx, table, conditions, []string{colUUID}, object)
				if selectErr != nil {
					return selectErr
				}
				existingUUID := ""
				if len(rows) > 0 {
					existingUUID = rowUUIDValue(rows[0])
				}
				return n.executeUpdateWithPrePostOps(ctx, table, object, string(mode), conditions, row, mutations, false, preOps, postOpsForUUID(postOps, existingUUID))
			}
			return err
		}
		existingUUID := rowUUIDValue(rows[0])
		return n.executeUpdateWithPrePostOps(ctx, table, object, string(mode), conditions, row, mutations, false, preOps, postOpsForUUID(postOps, existingUUID))
	case nbModeDelete:
		rows, err := n.selectRows(ctx, table, conditions, []string{colUUID}, object)
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			return wrap(ErrorNotFound, dbOVNNorthbound, table, string(mode), object, "row not found", nil)
		}
		var ops []libovsdb.Operation
		var mustAffect []int
		for _, row := range rows {
			uuid := rowUUIDValue(row)
			if uuid == "" {
				return wrap(ErrorConflict, dbOVNNorthbound, table, string(mode), object, "row UUID missing", nil)
			}
			refOps, err := n.unreferenceOps(ctx, table, uuid)
			if err != nil {
				return err
			}
			ops = append(ops, refOps...)
			ops = append(ops, libovsdb.Operation{
				Op:    libovsdb.OperationDelete,
				Table: table,
				Where: conditionUUID(uuid),
			})
			mustAffect = append(mustAffect, len(ops)-1)
		}
		results, err := n.db.transact(ctx, table, string(mode), object, ops...)
		if err != nil {
			return err
		}
		return ensureAffected(results, mustAffect, dbOVNNorthbound, table, string(mode), object)
	default:
		return wrap(ErrorValidation, dbOVNNorthbound, table, string(mode), object, "unsupported operation", nil)
	}
}

func (n *NBClient) unreferenceOps(ctx context.Context, targetTable, targetUUID string) ([]libovsdb.Operation, error) {
	if targetUUID == "" || n == nil || n.db == nil || n.db.schema == nil {
		return nil, nil
	}
	var ops []libovsdb.Operation
	for table := range n.db.schema.schema.Tables {
		if table == targetTable {
			continue
		}
		for _, ref := range n.db.schema.ReferenceColumnInfos(table, targetTable) {
			switch ref.Kind {
			case referenceColumnMapUUID:
				rows, err := newTableRef(n.db, table, "", "").selectRows(ctx, nil, []string{colUUID, ref.Name})
				if err != nil {
					return nil, err
				}
				for _, row := range rows {
					referrerUUID := anyString(row[colUUID])
					deleteKeys := ovsMapDeleteKeysForUUID(row[ref.Name], targetUUID, ref.KeyRef, ref.ValueRef)
					if referrerUUID == "" || len(deleteKeys) == 0 {
						continue
					}
					ops = append(ops, ovsUnreferenceMapOp(table, ref.Name, referrerUUID, deleteKeys...))
				}
			case referenceColumnSetUUID:
				rows, err := newTableRef(n.db, table, "", "").selectRows(ctx,
					[]libovsdb.Condition{libovsdb.NewCondition(ref.Name, libovsdb.ConditionIncludes, uuidValue(targetUUID))},
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
					ops = append(ops, ovsUnreferenceUUIDSetOp(table, ref.Name, referrerUUID, targetUUID))
				}
			case referenceColumnScalarUUID:
				if ref.Reference == libovsdb.Weak {
					continue
				}
				rows, err := newTableRef(n.db, table, "", "").selectRows(ctx,
					[]libovsdb.Condition{libovsdb.NewCondition(ref.Name, libovsdb.ConditionEqual, uuidValue(targetUUID))},
					[]string{colUUID},
				)
				if err != nil {
					return nil, err
				}
				if len(rows) > 0 {
					return nil, wrap(ErrorConflict, dbOVNNorthbound, targetTable, "delete", targetUUID, "row is still referenced by "+table+"."+ref.Name, nil)
				}
			}
		}
	}
	return ops, nil
}

func (n *NBClient) executeInsert(ctx context.Context, table, object, op string, row libovsdb.Row) error {
	return n.executeInsertWithPreOps(ctx, table, object, op, row, nil)
}

func (n *NBClient) executeInsertWithPreOps(ctx context.Context, table, object, op string, row libovsdb.Row, preOps []libovsdb.Operation) error {
	return n.executeInsertWithPrePostOps(ctx, table, object, op, row, preOps, nil)
}

func (n *NBClient) executeInsertWithPrePostOps(ctx context.Context, table, object, op string, row libovsdb.Row, preOps []libovsdb.Operation, postOps func(string) []libovsdb.Operation) error {
	if len(row) == 0 {
		return wrap(ErrorValidation, dbOVNNorthbound, table, op, object, "insert row is empty", nil)
	}
	targetUUID := namedUUID(tableUUIDPrefix(table))
	ops := append([]libovsdb.Operation{}, preOps...)
	ops = append(ops, libovsdb.Operation{
		Op:       libovsdb.OperationInsert,
		Table:    table,
		UUIDName: targetUUID,
		Row:      row,
	})
	if postOps != nil {
		ops = append(ops, n.supportedPreOps(postOps(targetUUID))...)
	}
	results, err := n.db.transact(ctx, table, op, object, ops...)
	if err != nil {
		return err
	}
	return ensureAffected(results, nbPostMustAffectIndexes(len(preOps)+1, ops), dbOVNNorthbound, table, op, object)
}

func (n *NBClient) executeUpdate(ctx context.Context, table, object, op string, conditions []libovsdb.Condition, row libovsdb.Row, mutations []libovsdb.Mutation, allowNoop bool) error {
	return n.executeUpdateWithPreOps(ctx, table, object, op, conditions, row, mutations, allowNoop, nil)
}

func (n *NBClient) executeUpdateWithPreOps(ctx context.Context, table, object, op string, conditions []libovsdb.Condition, row libovsdb.Row, mutations []libovsdb.Mutation, allowNoop bool, preOps []libovsdb.Operation) error {
	return n.executeUpdateWithPrePostOps(ctx, table, object, op, conditions, row, mutations, allowNoop, preOps, nil)
}

func (n *NBClient) executeUpdateWithPrePostOps(ctx context.Context, table, object, op string, conditions []libovsdb.Condition, row libovsdb.Row, mutations []libovsdb.Mutation, allowNoop bool, preOps []libovsdb.Operation, postOps func(string) []libovsdb.Operation) error {
	var ops []libovsdb.Operation
	ops = append(ops, preOps...)
	mainStart := len(ops)
	updateRow := cloneRow(row)
	delete(updateRow, colUUID)
	delete(updateRow, colName)
	for _, mutation := range mutations {
		delete(updateRow, mutation.Column)
	}
	if len(updateRow) > 0 {
		ops = append(ops, libovsdb.Operation{Op: libovsdb.OperationUpdate, Table: table, Where: conditions, Row: updateRow})
	}
	if len(mutations) > 0 {
		ops = append(ops, libovsdb.Operation{Op: libovsdb.OperationMutate, Table: table, Where: conditions, Mutations: mutations})
	}
	if postOps != nil {
		ops = append(ops, n.supportedPreOps(postOps(""))...)
	}
	if len(ops) == 0 {
		if allowNoop {
			return nil
		}
		return nil
	}
	results, err := n.db.transact(ctx, table, op, object, ops...)
	if err != nil {
		return err
	}
	mustAffect := make([]int, 0, len(ops))
	for i := range ops {
		if i >= mainStart {
			mustAffect = append(mustAffect, i)
		}
	}
	return ensureAffected(results, mustAffect, dbOVNNorthbound, table, op, object)
}

func postOpsForUUID(postOps func(string) []libovsdb.Operation, uuid string) func(string) []libovsdb.Operation {
	if postOps == nil {
		return nil
	}
	return func(string) []libovsdb.Operation {
		return postOps(uuid)
	}
}

func nbPostMustAffectIndexes(start int, ops []libovsdb.Operation) []int {
	indexes := make([]int, 0, len(ops))
	for i := start; i < len(ops); i++ {
		switch ops[i].Op {
		case libovsdb.OperationUpdate, libovsdb.OperationMutate, libovsdb.OperationDelete:
			indexes = append(indexes, i)
		}
	}
	return indexes
}

func (n *NBClient) supportedColumns(table string, columns []string) []string {
	if len(columns) == 0 {
		return n.db.schema.Columns(table)
	}
	out := make([]string, 0, len(columns))
	for _, column := range columns {
		if n.db.schema.HasColumn(table, column) {
			out = append(out, column)
		}
	}
	return out
}

func (n *NBClient) supportedRow(table string, row libovsdb.Row) libovsdb.Row {
	out := libovsdb.Row{}
	for column, value := range row {
		if n.db.schema.HasColumn(table, column) {
			out[column] = value
		}
	}
	return out
}

func (n *NBClient) supportedMutations(table string, mutations []libovsdb.Mutation) []libovsdb.Mutation {
	out := make([]libovsdb.Mutation, 0, len(mutations))
	for _, mutation := range mutations {
		if n.db.schema.HasColumn(table, mutation.Column) {
			out = append(out, mutation)
		}
	}
	return out
}

func (n *NBClient) supportedPreOps(ops []libovsdb.Operation) []libovsdb.Operation {
	out := make([]libovsdb.Operation, 0, len(ops))
	for _, op := range ops {
		if op.Table != "" && !n.db.schema.HasTable(op.Table) {
			continue
		}
		op.Row = n.supportedRow(op.Table, op.Row)
		op.Mutations = n.supportedMutations(op.Table, op.Mutations)
		out = append(out, op)
	}
	return out
}

func nbSetUUIDSet(row libovsdb.Row, column string, values []string) {
	if len(values) > 0 {
		row[column] = uuidSet(values...)
	}
}

func nbSetStringSet(row libovsdb.Row, column string, values []string) {
	if len(values) > 0 {
		row[column] = stringSet(values)
	}
}

func nbSetOptionalUUID(row libovsdb.Row, column, value string) {
	if value != "" {
		row[column] = uuidValue(value)
	}
}

func nbSetOptionalString(row libovsdb.Row, column, value string) {
	if value != "" {
		row[column] = value
	}
}

func nbAppendUUIDSetMutation(mutations *[]libovsdb.Mutation, column string, values []string) {
	if len(values) > 0 {
		*mutations = append(*mutations, *libovsdb.NewMutation(column, libovsdb.MutateOperationInsert, uuidSet(values...)))
	}
}

func nbAppendStringSetMutation(mutations *[]libovsdb.Mutation, column string, values []string) {
	if len(values) > 0 {
		*mutations = append(*mutations, *libovsdb.NewMutation(column, libovsdb.MutateOperationInsert, stringSet(values)))
	}
}

func nbAppendMapMutation(mutations *[]libovsdb.Mutation, column string, values map[string]string) {
	if len(values) > 0 {
		*mutations = append(*mutations, *libovsdb.NewMutation(column, libovsdb.MutateOperationInsert, ovsMap(values)))
	}
}

func nbExternalIDCondition(key, value string) []libovsdb.Condition {
	return []libovsdb.Condition{libovsdb.NewCondition(colExternalIDs, libovsdb.ConditionIncludes, ovsMap(map[string]string{key: value}))}
}

func nbValidateStringMaps(values ...map[string]string) error {
	for _, item := range values {
		for key := range item {
			if err := validateExternalID(key); err != nil {
				return err
			}
		}
	}
	return nil
}

func nbIntMap(values map[string]int) libovsdb.OvsMap {
	raw := map[string]int{}
	for k, v := range values {
		raw[k] = v
	}
	m, _ := libovsdb.NewOvsMap(raw)
	return m
}

func nbLogicalRouterColumns() []string {
	return []string{colUUID, colName, colPorts, colStaticRoutes, colNAT, colLoadBalancer, colOptions, colExternalIDs}
}

func nbLogicalRouterPortColumns() []string {
	return []string{colUUID, colName, colMAC, colNetworks, colGatewayChassis, colHAChassisGroup, colPeer, colEnabled, colIPv6Prefix, colIPv6RAConfigs, colOptions, colExternalIDs}
}

func nbACLColumns() []string {
	return []string{colUUID, colName, colPriority, colDirection, colMatch, colAction, colLog, colMeter, colSeverity, colLabel, colTier, colOptions, colExternalIDs}
}

func nbNATColumns() []string {
	return []string{colUUID, colType, colLogicalIP, colExternalIP, colLogicalPort, colExternalMAC, colExternalPortRange, colGatewayPort, colAllowedExtIPs, colExemptedExtIPs, colMatch, colPriority, colOptions, colExternalIDs}
}

func nbLoadBalancerColumns() []string {
	return []string{colUUID, colName, colVIPs, colProtocol, colSelectionFields, colIPPortMappings, "health_check", colOptions, colExternalIDs}
}

func nbDHCPOptionsColumns() []string {
	return []string{colUUID, colCIDR, colOptions, colExternalIDs}
}

func nbDNSColumns() []string {
	return []string{colUUID, colRecords, colOptions, colExternalIDs}
}

func nbQoSColumns() []string {
	return []string{colUUID, colPriority, colDirection, colMatch, colAction, colBandwidth, colExternalIDs}
}

func nbMeterColumns() []string {
	return []string{colUUID, colName, colUnit, colBands, colFair, colExternalIDs}
}

func nbMeterBandColumns() []string {
	return []string{colUUID, colAction, colRate, colBurstSize, colExternalIDs}
}

func nbPortGroupColumns() []string {
	return []string{colUUID, colName, colPorts, colACLs, colExternalIDs}
}

func nbAddressSetColumns() []string {
	return []string{colUUID, colName, colAddresses, colExternalIDs}
}

func nbGatewayChassisColumns() []string {
	return []string{colUUID, colName, colChassisName, colPriority, colOptions, colExternalIDs}
}

func nbHAChassisColumns() []string {
	return []string{colUUID, colChassisName, colPriority, colExternalIDs}
}

func nbHAChassisGroupColumns() []string {
	return []string{colUUID, colName, colHAChassis, colExternalIDs}
}

func nbBFDColumns() []string {
	return []string{colUUID, colLogicalPort, colDstIP, colMinTx, colMinRx, colDetectMult, colStatus, colOptions, colExternalIDs}
}

func nbStringPtr(row libovsdb.Row, column string) *string {
	value := rowStringValue(row, column)
	if value == "" {
		values := rowStringSliceValue(row, column)
		if len(values) > 0 {
			value = values[0]
		}
	}
	if value == "" {
		return nil
	}
	return &value
}

func nbBoolPtr(row libovsdb.Row, column string) *bool {
	value, ok := row[column]
	if !ok {
		return nil
	}
	switch typed := value.(type) {
	case bool:
		return &typed
	case libovsdb.OvsSet:
		if len(typed.GoSet) == 0 {
			return nil
		}
		if b, ok := typed.GoSet[0].(bool); ok {
			return &b
		}
	}
	return nil
}

func nbIntPtr(row libovsdb.Row, column string) *int {
	value, ok := nbIntValue(row[column])
	if !ok {
		return nil
	}
	return &value
}

func nbInt(row libovsdb.Row, column string) int {
	value, _ := nbIntValue(row[column])
	return value
}

func nbIntValue(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, true
	case int64:
		return int(typed), true
	case float64:
		return int(typed), true
	case jsonNumber:
		i, err := typed.Int64()
		return int(i), err == nil
	case string:
		i, err := strconv.Atoi(typed)
		return i, err == nil
	case libovsdb.OvsSet:
		if len(typed.GoSet) == 0 {
			return 0, false
		}
		return nbIntValue(typed.GoSet[0])
	default:
		return 0, false
	}
}

func nbIntMapValue(row libovsdb.Row, column string) map[string]int {
	value, ok := row[column]
	if !ok {
		return nil
	}
	out := map[string]int{}
	switch typed := value.(type) {
	case map[string]int:
		return typed
	case libovsdb.OvsMap:
		for k, v := range typed.GoMap {
			key, keyOK := k.(string)
			val, valOK := nbIntValue(v)
			if keyOK && valOK {
				out[key] = val
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func rowBoolValue(row libovsdb.Row, column string) bool {
	value, ok := row[column]
	if !ok {
		return false
	}
	if b, ok := value.(bool); ok {
		return b
	}
	return false
}

func logicalRouterFromRow(row libovsdb.Row) *LogicalRouter {
	return &LogicalRouter{
		UUID:          rowUUIDValue(row),
		Name:          rowStringValue(row, colName),
		Ports:         rowUUIDSliceValue(row, colPorts),
		StaticRoutes:  rowUUIDSliceValue(row, colStaticRoutes),
		NAT:           rowUUIDSliceValue(row, colNAT),
		LoadBalancers: rowUUIDSliceValue(row, colLoadBalancer),
		Options:       rowStringMapValue(row, colOptions),
		ExternalIDs:   rowStringMapValue(row, colExternalIDs),
	}
}

func logicalRouterPortFromRow(row libovsdb.Row) *LogicalRouterPort {
	return &LogicalRouterPort{
		UUID:           rowUUIDValue(row),
		Name:           rowStringValue(row, colName),
		MAC:            rowStringValue(row, colMAC),
		Networks:       rowStringSliceValue(row, colNetworks),
		GatewayChassis: rowUUIDSliceValue(row, colGatewayChassis),
		HAChassisGroup: nbStringPtr(row, colHAChassisGroup),
		Peer:           nbStringPtr(row, colPeer),
		Enabled:        nbBoolPtr(row, colEnabled),
		IPv6Prefix:     rowStringSliceValue(row, colIPv6Prefix),
		IPv6RAConfigs:  rowStringMapValue(row, colIPv6RAConfigs),
		Options:        rowStringMapValue(row, colOptions),
		ExternalIDs:    rowStringMapValue(row, colExternalIDs),
	}
}

func aclFromRow(row libovsdb.Row) *ACL {
	return &ACL{
		UUID:        rowUUIDValue(row),
		Name:        nbStringPtr(row, colName),
		Priority:    nbInt(row, colPriority),
		Direction:   rowStringValue(row, colDirection),
		Match:       rowStringValue(row, colMatch),
		Action:      rowStringValue(row, colAction),
		Log:         rowBoolValue(row, colLog),
		Meter:       nbStringPtr(row, colMeter),
		Severity:    nbStringPtr(row, colSeverity),
		Label:       nbInt(row, colLabel),
		Tier:        nbInt(row, colTier),
		Options:     rowStringMapValue(row, colOptions),
		ExternalIDs: rowStringMapValue(row, colExternalIDs),
	}
}

func natFromRow(row libovsdb.Row) *NAT {
	return &NAT{
		UUID:              rowUUIDValue(row),
		Type:              rowStringValue(row, colType),
		LogicalIP:         rowStringValue(row, colLogicalIP),
		ExternalIP:        rowStringValue(row, colExternalIP),
		LogicalPort:       nbStringPtr(row, colLogicalPort),
		ExternalMAC:       nbStringPtr(row, colExternalMAC),
		ExternalPortRange: rowStringValue(row, colExternalPortRange),
		GatewayPort:       nbStringPtr(row, colGatewayPort),
		AllowedExtIPs:     nbStringPtr(row, colAllowedExtIPs),
		ExemptedExtIPs:    nbStringPtr(row, colExemptedExtIPs),
		Match:             rowStringValue(row, colMatch),
		Priority:          nbInt(row, colPriority),
		Options:           rowStringMapValue(row, colOptions),
		ExternalIDs:       rowStringMapValue(row, colExternalIDs),
	}
}

func loadBalancerFromRow(row libovsdb.Row) *LoadBalancer {
	return &LoadBalancer{
		UUID:            rowUUIDValue(row),
		Name:            rowStringValue(row, colName),
		VIPs:            rowStringMapValue(row, colVIPs),
		Protocol:        nbStringPtr(row, colProtocol),
		SelectionFields: rowStringSliceValue(row, colSelectionFields),
		IPPortMappings:  rowStringMapValue(row, colIPPortMappings),
		HealthCheck:     rowUUIDSliceValue(row, "health_check"),
		Options:         rowStringMapValue(row, colOptions),
		ExternalIDs:     rowStringMapValue(row, colExternalIDs),
	}
}

func dhcpOptionsFromRow(row libovsdb.Row) *DHCPOptions {
	return &DHCPOptions{
		UUID:        rowUUIDValue(row),
		CIDR:        rowStringValue(row, colCIDR),
		Options:     rowStringMapValue(row, colOptions),
		ExternalIDs: rowStringMapValue(row, colExternalIDs),
	}
}

func dnsFromRow(row libovsdb.Row) *DNS {
	return &DNS{
		UUID:        rowUUIDValue(row),
		Records:     rowStringMapValue(row, colRecords),
		Options:     rowStringMapValue(row, colOptions),
		ExternalIDs: rowStringMapValue(row, colExternalIDs),
	}
}

func qosFromRow(row libovsdb.Row) *QoS {
	return &QoS{
		UUID:        rowUUIDValue(row),
		Priority:    nbInt(row, colPriority),
		Direction:   rowStringValue(row, colDirection),
		Match:       rowStringValue(row, colMatch),
		Action:      nbIntMapValue(row, colAction),
		Bandwidth:   nbIntMapValue(row, colBandwidth),
		ExternalIDs: rowStringMapValue(row, colExternalIDs),
	}
}

func meterFromRow(row libovsdb.Row) *Meter {
	return &Meter{
		UUID:        rowUUIDValue(row),
		Name:        rowStringValue(row, colName),
		Unit:        rowStringValue(row, colUnit),
		Bands:       rowUUIDSliceValue(row, colBands),
		Fair:        nbBoolPtr(row, colFair),
		ExternalIDs: rowStringMapValue(row, colExternalIDs),
	}
}

func meterBandFromRow(row libovsdb.Row) *MeterBand {
	return &MeterBand{
		UUID:        rowUUIDValue(row),
		Action:      rowStringValue(row, colAction),
		Rate:        nbInt(row, colRate),
		BurstSize:   nbInt(row, colBurstSize),
		ExternalIDs: rowStringMapValue(row, colExternalIDs),
	}
}

func portGroupFromRow(row libovsdb.Row) *PortGroup {
	return &PortGroup{
		UUID:        rowUUIDValue(row),
		Name:        rowStringValue(row, colName),
		Ports:       rowUUIDSliceValue(row, colPorts),
		ACLs:        rowUUIDSliceValue(row, colACLs),
		ExternalIDs: rowStringMapValue(row, colExternalIDs),
	}
}

func addressSetFromRow(row libovsdb.Row) *AddressSet {
	return &AddressSet{
		UUID:        rowUUIDValue(row),
		Name:        rowStringValue(row, colName),
		Addresses:   rowStringSliceValue(row, colAddresses),
		ExternalIDs: rowStringMapValue(row, colExternalIDs),
	}
}

func gatewayChassisFromRow(row libovsdb.Row) *GatewayChassis {
	return &GatewayChassis{
		UUID:        rowUUIDValue(row),
		Name:        rowStringValue(row, colName),
		ChassisName: rowStringValue(row, colChassisName),
		Priority:    nbInt(row, colPriority),
		Options:     rowStringMapValue(row, colOptions),
		ExternalIDs: rowStringMapValue(row, colExternalIDs),
	}
}

func haChassisFromRow(row libovsdb.Row) *HAChassis {
	return &HAChassis{
		UUID:        rowUUIDValue(row),
		ChassisName: rowStringValue(row, colChassisName),
		Priority:    nbInt(row, colPriority),
		ExternalIDs: rowStringMapValue(row, colExternalIDs),
	}
}

func haChassisGroupFromRow(row libovsdb.Row) *HAChassisGroup {
	return &HAChassisGroup{
		UUID:        rowUUIDValue(row),
		Name:        rowStringValue(row, colName),
		HAChassis:   rowUUIDSliceValue(row, colHAChassis),
		ExternalIDs: rowStringMapValue(row, colExternalIDs),
	}
}

func bfdFromRow(row libovsdb.Row) *BFD {
	return &BFD{
		UUID:        rowUUIDValue(row),
		LogicalPort: rowStringValue(row, colLogicalPort),
		DstIP:       rowStringValue(row, colDstIP),
		MinTx:       nbIntPtr(row, colMinTx),
		MinRx:       nbIntPtr(row, colMinRx),
		DetectMult:  nbIntPtr(row, colDetectMult),
		Status:      nbStringPtr(row, colStatus),
		Options:     rowStringMapValue(row, colOptions),
		ExternalIDs: rowStringMapValue(row, colExternalIDs),
	}
}

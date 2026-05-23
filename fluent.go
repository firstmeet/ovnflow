package ovnflow

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	libovsdb "github.com/ovn-kubernetes/libovsdb/ovsdb"
)

// Row is a dynamic OVSDB row returned by the table-level fluent API.
type Row map[string]any

// RowEvent is emitted by table-level watches.
type RowEvent struct {
	Type EventType
	Old  Row
	New  Row
}

// TableRef is a runtime-schema-aware fluent handle for any OVSDB table.
// Typed NB/SB/OVS APIs are thin wrappers over this for less common tables.
type TableRef struct {
	db               *dbClient
	table            string
	identityColumn   string
	identityValue    string
	conditions       []libovsdb.Condition
	defaultMutations []libovsdb.Mutation
}

func newTableRef(db *dbClient, table, identityColumn, identityValue string) *TableRef {
	if identityColumn == "" {
		identityColumn = colName
	}
	return &TableRef{db: db, table: table, identityColumn: identityColumn, identityValue: identityValue}
}

// Create starts an insert operation.
func (r *TableRef) Create() *TableBuilder {
	return newTableBuilder(r, tableModeCreate)
}

// Ensure starts an idempotent create-or-mutate operation.
func (r *TableRef) Ensure() *TableBuilder {
	return newTableBuilder(r, tableModeEnsure)
}

// Update starts a mutate/update operation for an existing row.
func (r *TableRef) Update() *TableBuilder {
	return newTableBuilder(r, tableModeUpdate)
}

// Delete starts a delete operation.
func (r *TableRef) Delete() *TableBuilder {
	return newTableBuilder(r, tableModeDelete)
}

// Get selects one row by this reference identity.
func (r *TableRef) Get(ctx context.Context) (Row, error) {
	if err := r.validateIdentity("get"); err != nil {
		return nil, err
	}
	rows, err := r.selectRows(ctx, r.identityConditions(), nil)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, wrap(ErrorNotFound, r.db.database, r.table, "get", r.identityValue, "row not found", nil)
	}
	return rows[0], nil
}

// List selects rows from this table. Identity and explicit Where conditions are
// both honored; an empty reference returns the full table.
func (r *TableRef) List(ctx context.Context) ([]Row, error) {
	if err := r.validateTable("list"); err != nil {
		return nil, err
	}
	where := r.identityConditions()
	if err := r.db.schema.RequireConditionColumns(r.table, where...); err != nil {
		return nil, err
	}
	return r.selectRows(ctx, where, nil)
}

// Watch subscribes to table changes through libovsdb monitor/cache events.
func (r *TableRef) Watch(ctx context.Context) (<-chan RowEvent, <-chan error) {
	return r.db.watchRows(ctx, r.table, r.identityConditions(), 64, 256)
}

// Where adds an equality condition to this table reference.
func (r *TableRef) Where(column string, value any) *TableRef {
	return r.WhereCondition(column, libovsdb.ConditionEqual, value)
}

// WhereCondition adds a condition to this table reference.
func (r *TableRef) WhereCondition(column string, fn libovsdb.ConditionFunction, value any) *TableRef {
	next := *r
	next.conditions = append(append([]libovsdb.Condition{}, r.conditions...), libovsdb.NewCondition(column, fn, value))
	return &next
}

// WhereConditions adds prebuilt OVSDB conditions to this table reference.
func (r *TableRef) WhereConditions(conditions ...libovsdb.Condition) *TableRef {
	next := *r
	next.conditions = append(append([]libovsdb.Condition{}, r.conditions...), conditions...)
	return &next
}

func (r *TableRef) withDefaultMap(column string, values map[string]string) *TableRef {
	next := *r
	next.defaultMutations = append(append([]libovsdb.Mutation{}, r.defaultMutations...), *libovsdb.NewMutation(column, libovsdb.MutateOperationInsert, ovsMap(values)))
	return &next
}

func (r *TableRef) validateTable(op string) error {
	if r == nil || r.db == nil {
		return wrap(ErrorValidation, "", "", op, "", "nil table reference", nil)
	}
	return r.db.schema.RequireTable(r.table)
}

func (r *TableRef) validateIdentity(op string) error {
	if err := r.validateTable(op); err != nil {
		return err
	}
	if len(r.conditions) > 0 {
		for _, condition := range r.conditions {
			if err := r.db.schema.RequireColumns(r.table, condition.Column); err != nil {
				return err
			}
		}
		return nil
	}
	if strings.TrimSpace(r.identityColumn) == "" || strings.TrimSpace(r.identityValue) == "" {
		return wrap(ErrorValidation, r.db.database, r.table, op, r.identityValue, "identity column and value are required", nil)
	}
	return r.db.schema.RequireColumns(r.table, r.identityColumn)
}

func (r *TableRef) identityConditions() []libovsdb.Condition {
	conditions := append([]libovsdb.Condition{}, r.conditions...)
	if r.identityColumn == "" || r.identityValue == "" {
		return conditions
	}
	value := any(r.identityValue)
	if r.identityColumn == colUUID {
		value = uuidValue(r.identityValue)
	}
	return append(conditions, libovsdb.NewCondition(r.identityColumn, libovsdb.ConditionEqual, value))
}

func (r *TableRef) selectRows(ctx context.Context, where []libovsdb.Condition, columns []string) ([]Row, error) {
	if err := r.validateTable("select"); err != nil {
		return nil, err
	}
	if len(columns) == 0 {
		columns = r.db.schema.Columns(r.table)
	}
	if len(columns) > 0 {
		if err := r.db.schema.RequireColumns(r.table, columns...); err != nil {
			return nil, err
		}
	}
	results, err := r.db.transact(ctx, r.table, "select", r.identityValue, libovsdb.Operation{
		Op:      libovsdb.OperationSelect,
		Table:   r.table,
		Where:   where,
		Columns: columns,
	})
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, nil
	}
	out := make([]Row, 0, len(results[0].Rows))
	for _, row := range results[0].Rows {
		out = append(out, Row(rawRow(row)))
	}
	return out, nil
}

type tableMode string

const (
	tableModeCreate tableMode = "create"
	tableModeEnsure tableMode = "ensure"
	tableModeUpdate tableMode = "update"
	tableModeDelete tableMode = "delete"
)

// TableBuilder builds a single-row table operation. Map and set mutations use
// OVSDB mutate by default so external controller-owned keys are preserved.
type TableBuilder struct {
	once      useOnce
	ref       *TableRef
	mode      tableMode
	row       libovsdb.Row
	columns   []string
	mutations []libovsdb.Mutation
}

func newTableBuilder(ref *TableRef, mode tableMode) *TableBuilder {
	builder := &TableBuilder{ref: ref, mode: mode, row: libovsdb.Row{}}
	if ref != nil && len(ref.defaultMutations) > 0 {
		builder.mutations = append(builder.mutations, ref.defaultMutations...)
	}
	return builder
}

// WithColumn sets a column in the insert/update row.
func (b *TableBuilder) WithColumn(column string, value any) *TableBuilder {
	b.row[column] = value
	return b
}

// WithName sets the conventional name column.
func (b *TableBuilder) WithName(name string) *TableBuilder {
	return b.WithColumn(colName, name)
}

// WithType sets the conventional type column.
func (b *TableBuilder) WithType(kind string) *TableBuilder {
	return b.WithColumn(colType, kind)
}

// WithTarget sets target, used by OVSDB Connection and Manager-like tables.
func (b *TableBuilder) WithTarget(target string) *TableBuilder {
	return b.WithColumn(colTarget, target)
}

// WithPriority sets a priority column.
func (b *TableBuilder) WithPriority(priority int) *TableBuilder {
	return b.WithColumn(colPriority, priority)
}

// WithDirection sets a direction column, commonly from-lport or to-lport.
func (b *TableBuilder) WithDirection(direction string) *TableBuilder {
	return b.WithColumn(colDirection, direction)
}

// WithMatch sets a match expression column.
func (b *TableBuilder) WithMatch(match string) *TableBuilder {
	return b.WithColumn(colMatch, match)
}

// WithAction sets an action column.
func (b *TableBuilder) WithAction(action string) *TableBuilder {
	return b.WithColumn(colAction, action)
}

// WithTier sets a tier column when supported by newer schemas.
func (b *TableBuilder) WithTier(tier int) *TableBuilder {
	return b.WithOptionalColumn(colTier, tier)
}

// WithAddresses sets an OVSDB set of string addresses.
func (b *TableBuilder) WithAddresses(addresses ...string) *TableBuilder {
	values := make([]any, 0, len(addresses))
	for _, address := range addresses {
		values = append(values, address)
	}
	return b.WithColumn(colAddresses, ovsSet(values...))
}

// WithNetworks sets an OVSDB set of network CIDRs.
func (b *TableBuilder) WithNetworks(networks ...string) *TableBuilder {
	values := make([]any, 0, len(networks))
	for _, network := range networks {
		values = append(values, network)
	}
	return b.WithColumn(colNetworks, ovsSet(values...))
}

// WithLogicalPort sets logical_port.
func (b *TableBuilder) WithLogicalPort(port string) *TableBuilder {
	return b.WithColumn(colLogicalPort, port)
}

// WithUUIDRef sets a UUID reference column.
func (b *TableBuilder) WithUUIDRef(column, uuid string) *TableBuilder {
	return b.WithColumn(column, uuidValue(uuid))
}

// WithUUIDSet sets a UUID set column.
func (b *TableBuilder) WithUUIDSet(column string, uuids ...string) *TableBuilder {
	return b.WithColumn(column, uuidSet(uuids...))
}

// MutateUUIDSet inserts UUID references into a set column.
func (b *TableBuilder) MutateUUIDSet(column string, uuids ...string) *TableBuilder {
	values := make([]any, 0, len(uuids))
	for _, id := range uuids {
		values = append(values, uuidValue(id))
	}
	return b.MutateSet(column, values...)
}

// DeleteUUIDSet removes UUID references from a set column.
func (b *TableBuilder) DeleteUUIDSet(column string, uuids ...string) *TableBuilder {
	values := make([]any, 0, len(uuids))
	for _, id := range uuids {
		values = append(values, uuidValue(id))
	}
	return b.DeleteSet(column, values...)
}

// WithOptionalColumn sets a column only if the runtime schema supports it.
func (b *TableBuilder) WithOptionalColumn(column string, value any) *TableBuilder {
	if b.ref != nil && b.ref.db != nil && b.ref.db.schema.HasColumn(b.ref.table, column) {
		b.row[column] = value
	}
	return b
}

// WithExternalID mutates external_ids without replacing existing keys.
func (b *TableBuilder) WithExternalID(key, value string) *TableBuilder {
	return b.MutateMap(colExternalIDs, map[string]string{key: value})
}

// WithOption mutates options without replacing existing keys.
func (b *TableBuilder) WithOption(key, value string) *TableBuilder {
	return b.MutateMap(colOptions, map[string]string{key: value})
}

// MutateMap inserts map entries into column.
func (b *TableBuilder) MutateMap(column string, values map[string]string) *TableBuilder {
	if len(values) == 0 {
		return b
	}
	b.mutations = append(b.mutations, *libovsdb.NewMutation(column, libovsdb.MutateOperationInsert, ovsMap(values)))
	return b
}

// DeleteMap removes map entries from column.
func (b *TableBuilder) DeleteMap(column string, values map[string]string) *TableBuilder {
	if len(values) == 0 {
		return b
	}
	b.mutations = append(b.mutations, *libovsdb.NewMutation(column, libovsdb.MutateOperationDelete, ovsMap(values)))
	return b
}

// MutateSet inserts one or more set values into column.
func (b *TableBuilder) MutateSet(column string, values ...any) *TableBuilder {
	if len(values) == 0 {
		return b
	}
	b.mutations = append(b.mutations, *libovsdb.NewMutation(column, libovsdb.MutateOperationInsert, ovsSet(values...)))
	return b
}

// DeleteSet removes one or more set values from column.
func (b *TableBuilder) DeleteSet(column string, values ...any) *TableBuilder {
	if len(values) == 0 {
		return b
	}
	b.mutations = append(b.mutations, *libovsdb.NewMutation(column, libovsdb.MutateOperationDelete, ovsSet(values...)))
	return b
}

// SelectColumns constrains columns used by Get/List helpers chained from tests
// and future extensions.
func (b *TableBuilder) SelectColumns(columns ...string) *TableBuilder {
	b.columns = append([]string{}, columns...)
	return b
}

// Execute commits the configured operation.
func (b *TableBuilder) Execute(ctx context.Context) error {
	if !b.once.mark() {
		return wrap(ErrorValidation, b.database(), b.table(), string(b.mode), b.object(), "builder already executed", nil)
	}
	if err := b.validate(); err != nil {
		return err
	}
	switch b.mode {
	case tableModeCreate:
		return b.executeCreate(ctx, false)
	case tableModeEnsure:
		return b.executeCreate(ctx, true)
	case tableModeUpdate:
		return b.executeUpdate(ctx)
	case tableModeDelete:
		return b.executeDelete(ctx)
	default:
		return wrap(ErrorValidation, b.database(), b.table(), string(b.mode), b.object(), "unsupported operation", nil)
	}
}

func (b *TableBuilder) validate() error {
	if b == nil || b.ref == nil {
		return wrap(ErrorValidation, "", "", "", "", "nil table builder", nil)
	}
	if err := b.ref.validateTable(string(b.mode)); err != nil {
		return err
	}
	if b.mode != tableModeCreate {
		if err := b.ref.validateIdentity(string(b.mode)); err != nil {
			return err
		}
	}
	if b.ref.identityValue != "" {
		if err := b.ref.validateIdentity(string(b.mode)); err != nil {
			return err
		}
		if b.ref.identityColumn != colUUID {
			b.row[b.ref.identityColumn] = b.ref.identityValue
		}
	}
	for column := range b.row {
		if err := b.ref.db.schema.RequireColumns(b.ref.table, column); err != nil {
			return err
		}
	}
	for _, mutation := range b.mutations {
		if err := b.ref.db.schema.RequireColumns(b.ref.table, mutation.Column); err != nil {
			return err
		}
	}
	for _, column := range b.columns {
		if err := b.ref.db.schema.RequireColumns(b.ref.table, column); err != nil {
			return err
		}
	}
	return nil
}

func (b *TableBuilder) executeCreate(ctx context.Context, ensure bool) error {
	if b.ref != nil && b.ref.db != nil && b.ref.db.database == dbOpenVSwitch && b.ref.table == tableManager {
		return b.executeOVSManagerCreate(ctx, ensure)
	}
	if ensure && len(b.ref.identityConditions()) > 0 {
		rows, err := b.ref.selectRows(ctx, b.ref.identityConditions(), []string{colUUID})
		if err != nil {
			return err
		}
		if len(rows) > 0 {
			return b.executeUpdate(ctx)
		}
	}

	row := cloneRow(b.row)
	for _, mutation := range b.mutations {
		mergeMutationIntoInsertRow(row, mutation)
	}
	op := libovsdb.Operation{
		Op:       libovsdb.OperationInsert,
		Table:    b.ref.table,
		UUIDName: namedUUID(tableUUIDPrefix(b.ref.table)),
		Row:      row,
	}
	_, err := b.ref.db.transact(ctx, b.ref.table, string(b.mode), b.ref.identityValue, op)
	if ensure && IsKind(err, ErrorAlreadyExists) {
		return b.executeUpdate(ctx)
	}
	return err
}

func (b *TableBuilder) executeUpdate(ctx context.Context) error {
	var ops []libovsdb.Operation
	if len(b.row) > 0 {
		row := cloneRow(b.row)
		delete(row, colUUID)
		delete(row, b.ref.identityColumn)
		if len(row) > 0 {
			ops = append(ops, libovsdb.Operation{
				Op:    libovsdb.OperationUpdate,
				Table: b.ref.table,
				Where: b.ref.identityConditions(),
				Row:   row,
			})
		}
	}
	if len(b.mutations) > 0 {
		ops = append(ops, libovsdb.Operation{
			Op:        libovsdb.OperationMutate,
			Table:     b.ref.table,
			Where:     b.ref.identityConditions(),
			Mutations: b.mutations,
		})
	}
	if len(ops) == 0 {
		return nil
	}
	results, err := b.ref.db.transact(ctx, b.ref.table, string(b.mode), b.ref.identityValue, ops...)
	if err != nil {
		return err
	}
	mustAffect := make([]int, len(ops))
	for i := range ops {
		mustAffect[i] = i
	}
	return ensureAffected(results, mustAffect, b.ref.db.database, b.ref.table, string(b.mode), b.ref.identityValue)
}

func (b *TableBuilder) executeDelete(ctx context.Context) error {
	if b.ref != nil && b.ref.db != nil && b.ref.db.database == dbOpenVSwitch {
		return b.executeOVSDelete(ctx)
	}
	rows, err := b.ref.selectRows(ctx, b.ref.identityConditions(), []string{colUUID})
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return wrap(ErrorNotFound, b.ref.db.database, b.ref.table, string(b.mode), b.ref.identityValue, "row not found", nil)
	}
	var ops []libovsdb.Operation
	var mustAffect []int
	for _, row := range rows {
		id := anyString(row[colUUID])
		if id == "" {
			return wrap(ErrorConflict, b.ref.db.database, b.ref.table, string(b.mode), b.ref.identityValue, "row UUID missing", nil)
		}
		refOps, err := b.ref.db.unreferenceOps(ctx, b.ref.table, id)
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
	return ensureAffected(results, mustAffect, b.ref.db.database, b.ref.table, string(b.mode), b.ref.identityValue)
}

func (b *TableBuilder) database() string {
	if b == nil || b.ref == nil || b.ref.db == nil {
		return ""
	}
	return b.ref.db.database
}

func (b *TableBuilder) table() string {
	if b == nil || b.ref == nil {
		return ""
	}
	return b.ref.table
}

func (b *TableBuilder) object() string {
	if b == nil || b.ref == nil {
		return ""
	}
	return b.ref.identityValue
}

func (d *dbClient) Table(table string) *TableRef {
	return newTableRef(d, table, "", "")
}

func (d *dbClient) TableBy(table, column, value string) *TableRef {
	return newTableRef(d, table, column, value)
}

func (d *dbClient) transact(ctx context.Context, table, op, object string, ops ...libovsdb.Operation) ([]libovsdb.OperationResult, error) {
	if d == nil {
		return nil, wrap(ErrorUnavailable, "", table, op, object, "database client is nil", nil)
	}
	exec := d.executor
	if exec == nil {
		exec = d.raw
	}
	if exec == nil {
		return nil, wrap(ErrorUnavailable, "", table, op, object, "database executor is nil", nil)
	}
	for _, operation := range ops {
		if operation.Table != "" {
			if err := d.schema.RequireTable(operation.Table); err != nil {
				return nil, err
			}
		}
		if err := d.validateOperationColumns(operation); err != nil {
			return nil, err
		}
	}
	results, err := exec.Transact(ctx, ops...)
	if err != nil {
		return nil, classifyTransactError(err, d.database, table, op, object)
	}
	if err := checkOperationResults(results, d.database, table, op, object); err != nil {
		return nil, err
	}
	return results, nil
}

func (d *dbClient) validateOperationColumns(op libovsdb.Operation) error {
	if op.Table == "" {
		return nil
	}
	for column := range op.Row {
		if err := d.schema.RequireColumns(op.Table, column); err != nil {
			return err
		}
	}
	for _, column := range op.Columns {
		if err := d.schema.RequireColumns(op.Table, column); err != nil {
			return err
		}
	}
	for _, mutation := range op.Mutations {
		if err := d.schema.RequireColumns(op.Table, mutation.Column); err != nil {
			return err
		}
	}
	return d.schema.RequireConditionColumns(op.Table, op.Where...)
}

func (d *dbClient) unreferenceOps(ctx context.Context, targetTable, targetUUID string) ([]libovsdb.Operation, error) {
	if targetTable == "" || targetUUID == "" || d == nil || d.schema == nil {
		return nil, nil
	}
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

func cloneRow(in libovsdb.Row) libovsdb.Row {
	out := make(libovsdb.Row, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func insertMutationValue(mutation libovsdb.Mutation) any {
	switch value := mutation.Value.(type) {
	case libovsdb.OvsMap, libovsdb.OvsSet:
		return value
	default:
		return value
	}
}

func mergeMutationIntoInsertRow(row libovsdb.Row, mutation libovsdb.Mutation) {
	if mutation.Mutator != libovsdb.MutateOperationInsert {
		return
	}
	current, exists := row[mutation.Column]
	if !exists {
		row[mutation.Column] = insertMutationValue(mutation)
		return
	}
	switch currentValue := current.(type) {
	case libovsdb.OvsMap:
		nextValue, ok := mutation.Value.(libovsdb.OvsMap)
		if !ok {
			return
		}
		if currentValue.GoMap == nil {
			currentValue.GoMap = map[any]any{}
		}
		for k, v := range nextValue.GoMap {
			currentValue.GoMap[k] = v
		}
		row[mutation.Column] = currentValue
	case libovsdb.OvsSet:
		nextValue, ok := mutation.Value.(libovsdb.OvsSet)
		if !ok {
			return
		}
		currentValue.GoSet = append(currentValue.GoSet, nextValue.GoSet...)
		row[mutation.Column] = currentValue
	}
}

func tableUUIDPrefix(table string) string {
	table = strings.ToLower(table)
	var b strings.Builder
	for _, r := range table {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "row"
	}
	return b.String()
}

func decodeModelRow(model any) Row {
	if model == nil {
		return nil
	}
	value := reflect.ValueOf(model)
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return nil
		}
		value = value.Elem()
	}
	if value.Kind() == reflect.Struct {
		row := Row{}
		typ := value.Type()
		for i := 0; i < value.NumField(); i++ {
			field := typ.Field(i)
			column := strings.Split(field.Tag.Get("ovsdb"), ",")[0]
			if column == "" || column == "-" {
				continue
			}
			if decoded, ok := decodeModelField(value.Field(i)); ok {
				row[column] = decoded
			}
		}
		return row
	}
	data, err := json.Marshal(model)
	if err != nil {
		return Row{"value": fmt.Sprintf("%v", model)}
	}
	var out Row
	if err := json.Unmarshal(data, &out); err != nil {
		return Row{"value": fmt.Sprintf("%v", model)}
	}
	return out
}

func decodeModelField(value reflect.Value) (any, bool) {
	if !value.IsValid() || !value.CanInterface() {
		return nil, false
	}
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return nil, false
		}
		value = value.Elem()
	}
	return value.Interface(), true
}

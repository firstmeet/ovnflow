package ovnflow

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"reflect"
	"strings"
	"sync"

	"github.com/google/uuid"
	libovsdb "github.com/ovn-kubernetes/libovsdb/ovsdb"
)

type executor interface {
	Transact(context.Context, ...libovsdb.Operation) ([]libovsdb.OperationResult, error)
	List(context.Context, any) error
}

type useOnce struct {
	mu   sync.Mutex
	used bool
}

func (u *useOnce) mark() bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.used {
		return false
	}
	u.used = true
	return true
}

func validateName(kind, name string) error {
	if strings.TrimSpace(name) == "" {
		return wrap(ErrorValidation, "", "", "validate", kind, "name must not be empty", nil)
	}
	return nil
}

func validateExternalID(key string) error {
	if strings.TrimSpace(key) == "" {
		return wrap(ErrorValidation, "", "", "validate", "", "external id key must not be empty", nil)
	}
	return nil
}

func ovsSet(values ...any) libovsdb.OvsSet {
	set, _ := libovsdb.NewOvsSet(values)
	return set
}

func ovsSetFromSlice(values any) libovsdb.OvsSet {
	if values == nil {
		return libovsdb.OvsSet{}
	}
	value := reflect.ValueOf(values)
	if value.Kind() != reflect.Slice {
		set, _ := libovsdb.NewOvsSet(values)
		return set
	}
	items := make([]any, 0, value.Len())
	for i := 0; i < value.Len(); i++ {
		items = append(items, value.Index(i).Interface())
	}
	return ovsSet(items...)
}

func stringSet(values []string) libovsdb.OvsSet {
	set, _ := libovsdb.NewOvsSet(values)
	return set
}

func uuidSet(values ...string) any {
	items := make([]libovsdb.UUID, 0, len(values))
	for _, value := range values {
		items = append(items, libovsdb.UUID{GoUUID: value})
	}
	out := make([]any, 0, len(items))
	for _, item := range items {
		out = append(out, item)
	}
	return libovsdb.OvsSet{GoSet: out}
}

func ovsMap(values map[string]string) libovsdb.OvsMap {
	if values == nil {
		values = map[string]string{}
	}
	m, _ := libovsdb.NewOvsMap(values)
	return m
}

func ovsIntMap(values map[int]int) libovsdb.OvsMap {
	goMap := make(map[any]any, len(values))
	for key, value := range values {
		goMap[key] = value
	}
	return libovsdb.OvsMap{GoMap: goMap}
}

func ovsIntUUIDMap(values map[int]string) libovsdb.OvsMap {
	goMap := make(map[any]any, len(values))
	for key, value := range values {
		goMap[key] = uuidValue(value)
	}
	return libovsdb.OvsMap{GoMap: goMap}
}

func setRowMap(row libovsdb.Row, column string, values map[string]string) {
	if len(values) == 0 {
		return
	}
	row[column] = ovsMap(values)
}

func uuidValue(id string) libovsdb.UUID {
	return libovsdb.UUID{GoUUID: id}
}

func namedUUID(prefix string) string {
	return prefix + "_" + strings.ReplaceAll(uuid.NewString(), "-", "")
}

func conditionName(name string) []libovsdb.Condition {
	return []libovsdb.Condition{libovsdb.NewCondition(colName, libovsdb.ConditionEqual, name)}
}

func conditionUUID(uuid string) []libovsdb.Condition {
	return []libovsdb.Condition{libovsdb.NewCondition(colUUID, libovsdb.ConditionEqual, uuidValue(uuid))}
}

func rawRow(row libovsdb.Row) map[string]any {
	data, _ := json.Marshal(row)
	var out map[string]any
	_ = json.Unmarshal(data, &out)
	return out
}

func rawConditions(conditions []libovsdb.Condition) []any {
	out := make([]any, 0, len(conditions))
	for _, condition := range conditions {
		data, _ := json.Marshal(condition)
		var raw any
		_ = json.Unmarshal(data, &raw)
		out = append(out, raw)
	}
	return out
}

func classifyTransactError(err error, database, table, op, object string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return classifyContext(err, database, table, op, object)
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "constraint violation"), strings.Contains(msg, "duplicate"):
		return wrap(ErrorAlreadyExists, database, table, op, object, "", err)
	case strings.Contains(msg, "not found"), strings.Contains(msg, "not exist"):
		return wrap(ErrorNotFound, database, table, op, object, "", err)
	case strings.Contains(msg, "referential integrity"):
		return wrap(ErrorConflict, database, table, op, object, "", err)
	default:
		var netErr net.Error
		if errors.As(err, &netErr) {
			return wrap(ErrorUnavailable, database, table, op, object, "", err)
		}
		return wrap(ErrorConflict, database, table, op, object, "", err)
	}
}

func checkOperationResults(results []libovsdb.OperationResult, database, table, op, object string) error {
	for _, result := range results {
		if result.Error == "" {
			continue
		}
		msg := result.Error
		if result.Details != "" {
			msg += ": " + result.Details
		}
		kind := ErrorConflict
		if strings.Contains(msg, "not found") || strings.Contains(msg, "not exist") {
			kind = ErrorNotFound
		} else if strings.Contains(msg, "constraint") || strings.Contains(msg, "duplicate") {
			kind = ErrorAlreadyExists
		}
		return wrap(kind, database, table, op, object, msg, nil)
	}
	return nil
}

func ensureAffected(results []libovsdb.OperationResult, opIndexes []int, database, table, op, object string) error {
	if err := checkOperationResults(results, database, table, op, object); err != nil {
		return err
	}
	for _, index := range opIndexes {
		if index < 0 || index >= len(results) {
			return wrap(ErrorConflict, database, table, op, object, "operation result missing", nil)
		}
		if results[index].Count == 0 {
			return wrap(ErrorNotFound, database, table, op, object, "operation did not affect any rows", nil)
		}
	}
	return nil
}

func mustAffectNonInsertOps(ops []libovsdb.Operation) []int {
	indexes := make([]int, 0, len(ops))
	for i, op := range ops {
		switch op.Op {
		case libovsdb.OperationDelete, libovsdb.OperationMutate, libovsdb.OperationUpdate:
			indexes = append(indexes, i)
		}
	}
	return indexes
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func mergeStringMaps(base, overlay map[string]string) map[string]string {
	out := cloneStringMap(base)
	for key, value := range overlay {
		if out == nil {
			out = map[string]string{}
		}
		out[key] = value
	}
	return out
}

func decodeRows[T any](results []libovsdb.OperationResult) ([]T, error) {
	if len(results) == 0 {
		return nil, nil
	}
	out := make([]T, 0, len(results[0].Rows))
	for _, row := range results[0].Rows {
		data, err := json.Marshal(row)
		if err != nil {
			return nil, err
		}
		var item T
		if err := json.Unmarshal(data, &item); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, nil
}

func rowUUIDValue(row libovsdb.Row) string {
	switch value := row[colUUID].(type) {
	case libovsdb.UUID:
		return value.GoUUID
	case string:
		return value
	default:
		return ""
	}
}

func rowStringValue(row libovsdb.Row, column string) string {
	if value, ok := row[column].(string); ok {
		return value
	}
	return ""
}

func rowStringSliceValue(row libovsdb.Row, column string) []string {
	value, ok := row[column]
	if !ok {
		return nil
	}
	switch typed := value.(type) {
	case string:
		return []string{typed}
	case libovsdb.OvsSet:
		out := make([]string, 0, len(typed.GoSet))
		for _, item := range typed.GoSet {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return typed
	default:
		return nil
	}
}

func rowUUIDSliceValue(row libovsdb.Row, column string) []string {
	value, ok := row[column]
	if !ok {
		return nil
	}
	switch typed := value.(type) {
	case libovsdb.UUID:
		if typed.GoUUID == "" {
			return nil
		}
		return []string{typed.GoUUID}
	case string:
		if typed == "" {
			return nil
		}
		return []string{typed}
	case libovsdb.OvsSet:
		out := make([]string, 0, len(typed.GoSet))
		for _, item := range typed.GoSet {
			switch v := item.(type) {
			case libovsdb.UUID:
				if v.GoUUID != "" {
					out = append(out, v.GoUUID)
				}
			case string:
				if v != "" {
					out = append(out, v)
				}
			}
		}
		return out
	case []string:
		return typed
	case []libovsdb.UUID:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if item.GoUUID != "" {
				out = append(out, item.GoUUID)
			}
		}
		return out
	default:
		return nil
	}
}

func rowOptionalUUIDValue(row libovsdb.Row, column string) *string {
	values := rowUUIDSliceValue(row, column)
	if len(values) == 0 {
		return nil
	}
	value := values[0]
	return &value
}

func rowIntUUIDMapValue(row libovsdb.Row, column string) map[int]string {
	value, ok := row[column]
	if !ok {
		return nil
	}
	out := map[int]string{}
	switch typed := value.(type) {
	case map[int]string:
		return typed
	case libovsdb.OvsMap:
		for k, v := range typed.GoMap {
			key, keyOK := anyIntValue(k)
			val, valOK := anyUUIDString(v)
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

func rowStringMapValue(row libovsdb.Row, column string) map[string]string {
	value, ok := row[column]
	if !ok {
		return nil
	}
	out := map[string]string{}
	switch typed := value.(type) {
	case map[string]string:
		return typed
	case libovsdb.OvsMap:
		for k, v := range typed.GoMap {
			key, keyOK := k.(string)
			val, valOK := v.(string)
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

func anyIntValue(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, true
	case int64:
		return int(typed), true
	case float64:
		return int(typed), true
	default:
		return 0, false
	}
}

func anyUUIDString(value any) (string, bool) {
	switch typed := value.(type) {
	case libovsdb.UUID:
		return typed.GoUUID, typed.GoUUID != ""
	case string:
		return typed, typed != ""
	case []any:
		if len(typed) == 2 {
			if marker, ok := typed[0].(string); ok && (marker == "uuid" || marker == "named-uuid") {
				id, ok := typed[1].(string)
				return id, ok && id != ""
			}
		}
	case map[string]any:
		id, ok := typed["GoUUID"].(string)
		return id, ok && id != ""
	default:
		return "", false
	}
	return "", false
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func uniqueStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

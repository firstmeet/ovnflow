package ovsdbjson

import "sort"

// UUID returns an OVSDB UUID atom.
func UUID(id string) []any {
	return []any{"uuid", id}
}

// NamedUUID returns an OVSDB named-uuid atom for references inside one
// transaction.
func NamedUUID(name string) []any {
	return []any{"named-uuid", name}
}

// Set returns an OVSDB set value.
func Set(values ...any) []any {
	return []any{"set", values}
}

// Map returns an OVSDB map value with deterministic key ordering.
func Map(values map[string]string) []any {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	pairs := make([]any, 0, len(keys))
	for _, key := range keys {
		pairs = append(pairs, []any{key, values[key]})
	}
	return []any{"map", pairs}
}

// Condition returns one OVSDB condition tuple.
func Condition(column, function string, value any) []any {
	return []any{column, function, value}
}

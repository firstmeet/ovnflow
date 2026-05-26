package ovnflow

import (
	"errors"
	"reflect"
	"testing"

	libovsdb "github.com/ovn-kubernetes/libovsdb/ovsdb"
)

func TestCheckOperationResultsClassifiesOVSDBErrors(t *testing.T) {
	tests := []struct {
		name   string
		result libovsdb.OperationResult
		want   ErrorKind
	}{
		{
			name:   "duplicate constraint",
			result: libovsdb.OperationResult{Error: "constraint violation", Details: "duplicate name"},
			want:   ErrorAlreadyExists,
		},
		{
			name:   "referential integrity",
			result: libovsdb.OperationResult{Error: "referential integrity violation"},
			want:   ErrorConflict,
		},
		{
			name:   "generic conflict",
			result: libovsdb.OperationResult{Error: "resources changed concurrently"},
			want:   ErrorConflict,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkOperationResults([]libovsdb.OperationResult{tt.result}, dbOVNNorthbound, tableLogicalSwitch, "create", "ls")
			if !IsKind(err, tt.want) {
				t.Fatalf("error kind = %q for %v, want %q", KindOf(err), err, tt.want)
			}
		})
	}
}

func TestEnsureAffectedRequiresResultAndRowCount(t *testing.T) {
	if err := ensureAffected([]libovsdb.OperationResult{{Count: 1}}, []int{0}, dbOpenVSwitch, tableBridge, "delete", "br"); err != nil {
		t.Fatalf("ensureAffected() = %v, want nil", err)
	}

	err := ensureAffected(nil, []int{0}, dbOpenVSwitch, tableBridge, "delete", "br")
	if !IsKind(err, ErrorConflict) {
		t.Fatalf("missing result kind = %q for %v, want %q", KindOf(err), err, ErrorConflict)
	}

	err = ensureAffected([]libovsdb.OperationResult{{Count: 0}}, []int{0}, dbOpenVSwitch, tableBridge, "delete", "br")
	if !IsKind(err, ErrorNotFound) {
		t.Fatalf("zero count kind = %q for %v, want %q", KindOf(err), err, ErrorNotFound)
	}
}

func TestClassifyTransactErrorMapsKnownFailures(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want ErrorKind
	}{
		{name: "duplicate", err: errors.New("constraint violation: duplicate key"), want: ErrorAlreadyExists},
		{name: "missing", err: errors.New("row not found"), want: ErrorNotFound},
		{name: "conflict", err: errors.New("transaction aborted"), want: ErrorConflict},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := classifyTransactError(tt.err, dbOVNNorthbound, tableLogicalSwitch, "create", "ls")
			if !IsKind(err, tt.want) {
				t.Fatalf("error kind = %q for %v, want %q", KindOf(err), err, tt.want)
			}
		})
	}
}

func TestRowDecodersSupportOVSDBShapes(t *testing.T) {
	row := libovsdb.Row{
		colUUID:        uuidValue("row-uuid"),
		colName:        "ls",
		colPorts:       uuidSet("port-1", "port-2"),
		colAddresses:   stringSet([]string{"00:11:22:33:44:55 192.168.1.10"}),
		colExternalIDs: ovsMap(map[string]string{"owner": "test"}),
	}

	if got := rowUUIDValue(row); got != "row-uuid" {
		t.Fatalf("rowUUIDValue() = %q, want row-uuid", got)
	}
	if got := rowStringValue(row, colName); got != "ls" {
		t.Fatalf("rowStringValue() = %q, want ls", got)
	}
	if got, want := rowUUIDSliceValue(row, colPorts), []string{"port-1", "port-2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("rowUUIDSliceValue() = %#v, want %#v", got, want)
	}
	if got, want := rowStringSliceValue(row, colAddresses), []string{"00:11:22:33:44:55 192.168.1.10"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("rowStringSliceValue() = %#v, want %#v", got, want)
	}
	if got, want := rowStringMapValue(row, colExternalIDs), map[string]string{"owner": "test"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("rowStringMapValue() = %#v, want %#v", got, want)
	}
}

func TestAnyStringSliceSupportsSingleUUIDJSONShape(t *testing.T) {
	got := anyStringSlice([]any{"uuid", "iface-uuid"})
	want := []string{"iface-uuid"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("anyStringSlice() = %#v, want %#v", got, want)
	}
}

func TestUniqueStringsDropsEmptyValuesAndPreservesOrder(t *testing.T) {
	got := uniqueStrings([]string{"", "a", "b", "a", "", "c", "b"})
	want := []string{"a", "b", "c"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("uniqueStrings() = %#v, want %#v", got, want)
	}
}

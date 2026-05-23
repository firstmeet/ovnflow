package ovsdbjson

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestParseEndpointTCP(t *testing.T) {
	endpoint, err := ParseEndpoint("tcp:172.27.192.120:6641")
	if err != nil {
		t.Fatalf("ParseEndpoint() = %v", err)
	}
	if endpoint.Network != "tcp" || endpoint.Address != "172.27.192.120:6641" {
		t.Fatalf("endpoint = %#v", endpoint)
	}
}

func TestParseEndpointUnix(t *testing.T) {
	endpoint, err := ParseEndpoint("unix:/var/run/openvswitch/db.sock")
	if err != nil {
		t.Fatalf("ParseEndpoint() = %v", err)
	}
	if endpoint.Network != "unix" || endpoint.Address != "/var/run/openvswitch/db.sock" {
		t.Fatalf("endpoint = %#v", endpoint)
	}
}

func TestParseEndpointRejectsMalformedTCP(t *testing.T) {
	if _, err := ParseEndpoint("tcp:127.0.0.1"); err == nil {
		t.Fatal("ParseEndpoint() succeeded, want malformed tcp error")
	}
}

func TestParseEndpointRejectsUnsupportedScheme(t *testing.T) {
	if _, err := ParseEndpoint("ssl:127.0.0.1:6641"); err == nil {
		t.Fatal("ParseEndpoint() succeeded, want unsupported ssl error")
	}
}

func TestOperationErrorFormattingIncludesDetails(t *testing.T) {
	err := OperationError{Index: 2, Reason: "constraint violation", Details: "duplicate name"}
	want := "ovsdb operation 2 failed: constraint violation: duplicate name"
	if err.Error() != want {
		t.Fatalf("OperationError.Error() = %q, want %q", err.Error(), want)
	}
}

func TestMapUsesDeterministicOrder(t *testing.T) {
	got := Map(map[string]string{
		"z": "last",
		"a": "first",
	})
	want := []any{
		"map",
		[]any{
			[]any{"a", "first"},
			[]any{"z", "last"},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Map() = %#v, want %#v", got, want)
	}
}

func TestOVSDBValueJSONShapes(t *testing.T) {
	value := []any{
		UUID("uuid-1"),
		NamedUUID("new_port"),
		Set("a", "b"),
		Condition("name", "==", "ls-web"),
	}
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("Marshal() = %v", err)
	}
	const want = `[["uuid","uuid-1"],["named-uuid","new_port"],["set",["a","b"]],["name","==","ls-web"]]`
	if string(data) != want {
		t.Fatalf("JSON = %s, want %s", data, want)
	}
}

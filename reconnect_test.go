package ovnflow

import (
	"context"
	"errors"
	"sync"
	"testing"

	libovsdb "github.com/ovn-kubernetes/libovsdb/ovsdb"
)

func TestDBTransactReconnectsAndRetriesIdempotentOperations(t *testing.T) {
	first := &sequenceExecutor{errs: []error{errors.New("not connected")}}
	second := &sequenceExecutor{results: [][]libovsdb.OperationResult{{
		{Rows: []libovsdb.Row{{colUUID: uuidValue("ls-uuid"), colName: "ls-default-net"}}},
	}}}
	db := testReconnectDBClient(first)
	reconnects := 0
	db.reconnect = func(context.Context) error {
		reconnects++
		db.executor = second
		return nil
	}

	rows, err := (&NBClient{db: db}).selectRows(context.Background(), tableLogicalSwitch, conditionName("ls-default-net"), []string{colUUID, colName}, "ls-default-net")
	if err != nil {
		t.Fatalf("selectRows() = %v, want nil", err)
	}
	if reconnects != 1 {
		t.Fatalf("reconnects = %d, want 1", reconnects)
	}
	if first.calls != 1 || second.calls != 1 {
		t.Fatalf("executor calls first=%d second=%d, want 1 each", first.calls, second.calls)
	}
	if len(rows) != 1 || rowUUIDValue(rows[0]) != "ls-uuid" {
		t.Fatalf("rows = %#v, want ls-uuid", rows)
	}
}

func TestTableEnsureRetriesAfterDisconnect(t *testing.T) {
	first := &sequenceExecutor{errs: []error{errors.New("transport is closing")}}
	second := &sequenceExecutor{results: [][]libovsdb.OperationResult{
		{{Rows: nil}},
		{{Count: 1}},
	}}
	db := testReconnectDBClient(first)
	db.retryBackoff = 0
	db.reconnect = func(context.Context) error {
		db.executor = second
		return nil
	}

	err := db.TableBy(tableLogicalSwitch, colName, "ls-default-net").
		Ensure().
		WithExternalID("owner", "test").
		Execute(context.Background())
	if err != nil {
		t.Fatalf("Ensure().Execute() = %v, want nil", err)
	}
	if first.calls != 1 || second.calls != 2 {
		t.Fatalf("executor calls first=%d second=%d, want first=1 second=2", first.calls, second.calls)
	}
}

func TestTableEnsureDoesNotReplayInsertAfterDisconnect(t *testing.T) {
	first := &sequenceExecutor{
		results: [][]libovsdb.OperationResult{{{Rows: nil}}},
		errs:    []error{nil, errors.New("transport is closing")},
	}
	db := testReconnectDBClient(first)
	reconnects := 0
	db.retryBackoff = 0
	db.reconnect = func(context.Context) error {
		reconnects++
		return nil
	}

	err := db.TableBy(tableLogicalSwitch, colName, "ls-default-net").
		Ensure().
		WithExternalID("owner", "test").
		Execute(context.Background())
	if !IsKind(err, ErrorUnavailable) {
		t.Fatalf("Ensure().Execute() kind = %q for %v, want %q", KindOf(err), err, ErrorUnavailable)
	}
	if first.calls != 2 || reconnects != 0 {
		t.Fatalf("executor calls=%d reconnects=%d, want calls=2 reconnects=0", first.calls, reconnects)
	}
}

func TestDBTransactDoesNotRetryCreateAfterDisconnect(t *testing.T) {
	first := &sequenceExecutor{errs: []error{errors.New("transport is closing")}}
	second := &sequenceExecutor{}
	db := testReconnectDBClient(first)
	db.reconnect = func(context.Context) error {
		db.executor = second
		return nil
	}

	_, err := db.transact(context.Background(), tableLogicalSwitch, "create", "ls-default-net", libovsdb.Operation{
		Op:    libovsdb.OperationInsert,
		Table: tableLogicalSwitch,
		Row:   libovsdb.Row{colName: "ls-default-net"},
	})
	if !IsKind(err, ErrorUnavailable) {
		t.Fatalf("transact() kind = %q for %v, want %q", KindOf(err), err, ErrorUnavailable)
	}
	if first.calls != 1 || second.calls != 0 {
		t.Fatalf("executor calls first=%d second=%d, want first=1 second=0", first.calls, second.calls)
	}
}

func TestDBTransactReturnsUnavailableWhenReconnectFails(t *testing.T) {
	first := &sequenceExecutor{errs: []error{errors.New("not connected")}}
	db := testReconnectDBClient(first)
	db.retryBackoff = 0
	db.reconnect = func(context.Context) error {
		return errors.New("dial tcp 127.0.0.1:6641: connect: connection refused")
	}

	_, err := (&NBClient{db: db}).selectRows(context.Background(), tableLogicalSwitch, conditionName("ls-default-net"), []string{colUUID}, "ls-default-net")
	if !IsKind(err, ErrorUnavailable) {
		t.Fatalf("selectRows() kind = %q for %v, want %q", KindOf(err), err, ErrorUnavailable)
	}
}

func testReconnectDBClient(exec executor) *dbClient {
	return &dbClient{
		database: dbOVNNorthbound,
		address:  "tcp:127.0.0.1:6641",
		executor: exec,
		schema:   newSchemaRegistry(dbOVNNorthbound, databaseSchemaWithColumns(dbOVNNorthbound, requiredSchema(dbOVNNorthbound))),
	}
}

type sequenceExecutor struct {
	mu      sync.Mutex
	calls   int
	ops     [][]libovsdb.Operation
	results [][]libovsdb.OperationResult
	errs    []error
}

func (s *sequenceExecutor) Transact(_ context.Context, ops ...libovsdb.Operation) ([]libovsdb.OperationResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	s.ops = append(s.ops, append([]libovsdb.Operation{}, ops...))

	var err error
	if len(s.errs) > 0 {
		err = s.errs[0]
		s.errs = s.errs[1:]
	}
	if len(s.results) > 0 {
		result := append([]libovsdb.OperationResult{}, s.results[0]...)
		s.results = s.results[1:]
		return result, err
	}
	out := make([]libovsdb.OperationResult, len(ops))
	for i := range out {
		out[i] = libovsdb.OperationResult{Count: 1}
	}
	return out, err
}

func (s *sequenceExecutor) List(context.Context, any) error {
	return nil
}

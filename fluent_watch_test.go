package ovnflow

import (
	"context"
	"sync"
	"testing"
	"time"

	libovsdb "github.com/ovn-kubernetes/libovsdb/ovsdb"
)

func TestRowMatchesConditionsSupportsNameAndUUIDIdentity(t *testing.T) {
	row := Row{
		colUUID: "row-uuid",
		colName: "ls-web",
	}

	conditions := []libovsdb.Condition{
		libovsdb.NewCondition(colName, libovsdb.ConditionEqual, "ls-web"),
		libovsdb.NewCondition(colUUID, libovsdb.ConditionEqual, uuidValue("row-uuid")),
	}
	if !rowMatches(conditions, row) {
		t.Fatalf("rowMatches() = false, want true")
	}

	conditions[0] = libovsdb.NewCondition(colName, libovsdb.ConditionEqual, "other")
	if rowMatches(conditions, row) {
		t.Fatalf("rowMatches() = true, want false")
	}
}

func TestRowEventMatchesUpdateWhenOldOrNewMatches(t *testing.T) {
	conditions := []libovsdb.Condition{
		libovsdb.NewCondition(colName, libovsdb.ConditionEqual, "ls-web"),
	}
	event := RowEvent{
		Type: EventUpdate,
		Old:  Row{colName: "ls-web"},
		New:  Row{colName: "ls-renamed"},
	}
	if !eventMatches(conditions, event) {
		t.Fatal("eventMatches() = false, want true for old matching row")
	}
}

func TestRowMatchesConditionFunctions(t *testing.T) {
	row := Row{
		colName:        "lp0",
		colExternalIDs: map[string]string{"owner": "ovnflow", "env": "test"},
		colPorts:       []string{"p1", "p2"},
	}
	tests := []struct {
		name      string
		condition libovsdb.Condition
		want      bool
	}{
		{name: "equal", condition: libovsdb.NewCondition(colName, libovsdb.ConditionEqual, "lp0"), want: true},
		{name: "not equal", condition: libovsdb.NewCondition(colName, libovsdb.ConditionNotEqual, "other"), want: true},
		{name: "map includes", condition: libovsdb.NewCondition(colExternalIDs, libovsdb.ConditionIncludes, ovsMap(map[string]string{"owner": "ovnflow"})), want: true},
		{name: "map excludes", condition: libovsdb.NewCondition(colExternalIDs, libovsdb.ConditionExcludes, ovsMap(map[string]string{"owner": "other"})), want: true},
		{name: "set includes", condition: libovsdb.NewCondition(colPorts, libovsdb.ConditionIncludes, "p2"), want: true},
		{name: "set excludes", condition: libovsdb.NewCondition(colPorts, libovsdb.ConditionExcludes, "p3"), want: true},
		{name: "includes missing", condition: libovsdb.NewCondition(colPorts, libovsdb.ConditionIncludes, "p3"), want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := rowMatches([]libovsdb.Condition{tt.condition}, row); got != tt.want {
				t.Fatalf("rowMatches() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDecodeModelRowUsesOVSDBTags(t *testing.T) {
	row := decodeModelRow(&SBPortBinding{
		UUID:        "pb-uuid",
		LogicalPort: "lp0",
		ExternalIDs: map[string]string{"owner": "test"},
	})
	if got := anyString(row[colUUID]); got != "pb-uuid" {
		t.Fatalf("decoded _uuid = %q, want pb-uuid: %#v", got, row)
	}
	if got := anyString(row[colLogicalPort]); got != "lp0" {
		t.Fatalf("decoded logical_port = %q, want lp0: %#v", got, row)
	}
	if got := anyStringMap(row[colExternalIDs])["owner"]; got != "test" {
		t.Fatalf("decoded external_ids.owner = %q, want test: %#v", got, row)
	}
}

func TestWatchSubscriptionFanoutCancelAndOverflow(t *testing.T) {
	manager := newWatchManager(&dbClient{database: dbOVNSouthbound})

	errsA := make(chan error, 1)
	errsB := make(chan error, 1)
	eventsA := make(chan RowEvent, 1)
	eventsB := make(chan RowEvent, 1)
	subA := &rowWatchSubscription{id: 1, m: manager, table: tablePortBinding, events: eventsA, errs: errsA, done: make(chan struct{}), initialDone: true}
	subB := &rowWatchSubscription{
		id:          2,
		m:           manager,
		table:       tablePortBinding,
		where:       []libovsdb.Condition{libovsdb.NewCondition(colLogicalPort, libovsdb.ConditionEqual, "lp0")},
		events:      eventsB,
		errs:        errsB,
		done:        make(chan struct{}),
		initialDone: true,
	}
	manager.byTable[tablePortBinding] = map[uint64]*rowWatchSubscription{subA.id: subA, subB.id: subB}

	event := RowEvent{Type: EventAdd, New: Row{colLogicalPort: "lp0"}}
	manager.publish(tablePortBinding, event)
	if got := <-eventsA; got.Type != EventAdd {
		t.Fatalf("subscriber A got %s, want add", got.Type)
	}
	if got := <-eventsB; got.Type != EventAdd {
		t.Fatalf("subscriber B got %s, want add", got.Type)
	}

	eventsA <- event
	if subA.offer(event) {
		t.Fatal("offer succeeded with a full subscriber queue, want overflow")
	}
	if !IsKind(<-errsA, ErrorPartial) {
		t.Fatal("subscriber A overflow did not report partial error")
	}
	if !subA.closed.Load() {
		t.Fatal("subscriber A was not canceled after overflow")
	}

	subB.cancel()
	manager.publish(tablePortBinding, RowEvent{Type: EventDelete, Old: Row{colLogicalPort: "lp0"}})
	select {
	case got := <-eventsB:
		t.Fatalf("canceled subscriber received event %#v", got)
	default:
	}
}

func TestWatchSubscriptionOverflowThenPublishDoesNotPanic(t *testing.T) {
	manager := newWatchManager(&dbClient{database: dbOVNSouthbound})
	errs := make(chan error, 1)
	events := make(chan RowEvent, 1)
	sub := &rowWatchSubscription{id: 1, m: manager, table: tablePortBinding, events: events, errs: errs, done: make(chan struct{}), initialDone: true}
	manager.byTable[tablePortBinding] = map[uint64]*rowWatchSubscription{sub.id: sub}

	event := RowEvent{Type: EventAdd, New: Row{colLogicalPort: "lp0"}}
	events <- event
	if sub.offer(event) {
		t.Fatal("offer succeeded with a full subscriber queue, want overflow")
	}
	if !IsKind(<-errs, ErrorPartial) {
		t.Fatal("subscriber overflow did not report partial error")
	}

	manager.publish(tablePortBinding, event)
	select {
	case got := <-events:
		if got.Type != EventAdd {
			t.Fatalf("queued event type = %s, want add", got.Type)
		}
	default:
		t.Fatal("original queued event was unexpectedly removed")
	}
}

func TestWatchSubscriptionQueuesLiveEventsUntilInitialDelivered(t *testing.T) {
	manager := newWatchManager(&dbClient{database: dbOVNSouthbound})
	errs := make(chan error, 1)
	events := make(chan RowEvent, 4)
	sub := &rowWatchSubscription{id: 1, m: manager, table: tablePortBinding, events: events, errs: errs, done: make(chan struct{})}
	manager.byTable[tablePortBinding] = map[uint64]*rowWatchSubscription{sub.id: sub}

	live := RowEvent{Type: EventUpdate, New: Row{colLogicalPort: "lp0", colExternalIDs: map[string]string{"phase": "live"}}}
	if !sub.offer(live) {
		t.Fatal("live offer before initial failed")
	}
	select {
	case got := <-events:
		t.Fatalf("live event was delivered before initial barrier completed: %#v", got)
	default:
	}

	initial := RowEvent{Type: EventInitial, New: Row{colLogicalPort: "lp0", colExternalIDs: map[string]string{"phase": "initial"}}}
	sub.finishInitial([]RowEvent{initial})

	first := <-events
	second := <-events
	if first.Type != EventInitial {
		t.Fatalf("first event = %s, want initial", first.Type)
	}
	if second.Type != EventUpdate {
		t.Fatalf("second event = %s, want update", second.Type)
	}
}

func TestWatchManagerInitializesOnceUnderConcurrentWatch(t *testing.T) {
	db := &dbClient{
		database: dbOVNNorthbound,
		schema: newSchemaRegistry(dbOVNNorthbound, databaseSchemaWithColumns(dbOVNNorthbound, map[string][]string{
			tableLogicalSwitch: {colName},
		})),
		executor: &nbRecordingExecutor{},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			events, _ := db.Table(tableLogicalSwitch).Watch(ctx)
			select {
			case <-events:
			case <-time.After(10 * time.Millisecond):
			}
		}()
	}
	wg.Wait()

	first := db.watchManager()
	if first == nil {
		t.Fatal("watch manager is nil")
	}
	if got := db.watchManager(); got != first {
		t.Fatal("watch manager was reinitialized")
	}
}

func TestWatchSubscriptionDoesNotReportLateErrorAfterCancel(t *testing.T) {
	manager := newWatchManager(&dbClient{database: dbOVNSouthbound})
	errs := make(chan error, 1)
	events := make(chan RowEvent, 1)
	sub := &rowWatchSubscription{id: 1, m: manager, table: tablePortBinding, events: events, errs: errs, done: make(chan struct{}), initialDone: true}
	manager.byTable[tablePortBinding] = map[uint64]*rowWatchSubscription{sub.id: sub}

	sub.cancel()
	manager.publishError(tablePortBinding, wrap(ErrorPartial, dbOVNSouthbound, tablePortBinding, "watch", "", "late", nil))

	select {
	case err := <-errs:
		t.Fatalf("received late error after cancel: %v", err)
	default:
	}
}

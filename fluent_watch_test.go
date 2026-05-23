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
		{name: "map excludes rejects one matching pair", condition: libovsdb.NewCondition(colExternalIDs, libovsdb.ConditionExcludes, ovsMap(map[string]string{"owner": "ovnflow", "missing": "value"})), want: false},
		{name: "map equal rejects subset", condition: libovsdb.NewCondition(colExternalIDs, libovsdb.ConditionEqual, ovsMap(map[string]string{"owner": "ovnflow"})), want: false},
		{name: "map equal exact", condition: libovsdb.NewCondition(colExternalIDs, libovsdb.ConditionEqual, ovsMap(map[string]string{"owner": "ovnflow", "env": "test"})), want: true},
		{name: "set includes", condition: libovsdb.NewCondition(colPorts, libovsdb.ConditionIncludes, "p2"), want: true},
		{name: "set excludes", condition: libovsdb.NewCondition(colPorts, libovsdb.ConditionExcludes, "p3"), want: true},
		{name: "set excludes rejects partial overlap", condition: libovsdb.NewCondition(colPorts, libovsdb.ConditionExcludes, ovsSet("p2", "p3")), want: false},
		{name: "set equal rejects subset", condition: libovsdb.NewCondition(colPorts, libovsdb.ConditionEqual, ovsSet("p1")), want: false},
		{name: "set equal exact", condition: libovsdb.NewCondition(colPorts, libovsdb.ConditionEqual, ovsSet("p1", "p2")), want: true},
		{name: "non string set equal does not match unrelated values", condition: libovsdb.NewCondition(colPorts, libovsdb.ConditionEqual, ovsSet(1, 2)), want: false},
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

func TestRowMatchesOVSDBJSONMapShape(t *testing.T) {
	row := Row(rawRow(libovsdb.Row{
		colExternalIDs: ovsMap(map[string]string{"owner": "ovnflow"}),
	}))
	condition := libovsdb.NewCondition(colExternalIDs, libovsdb.ConditionIncludes, ovsMap(map[string]string{"owner": "ovnflow"}))
	if !rowMatches([]libovsdb.Condition{condition}, row) {
		t.Fatalf("rowMatches() = false for OVSDB JSON map row: %#v", row)
	}
	if got := anyStringMap(row[colExternalIDs])["owner"]; got != "ovnflow" {
		t.Fatalf("anyStringMap() owner = %q, want ovnflow; row=%#v", got, row)
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

func TestWatchRowsForwarderStopsAfterSubscriptionDoneWithFullOutput(t *testing.T) {
	db := &dbClient{
		database: dbOVNNorthbound,
		schema: newSchemaRegistry(dbOVNNorthbound, databaseSchemaWithColumns(dbOVNNorthbound, map[string][]string{
			tableLogicalSwitch: {colName},
		})),
		executor: &nbRecordingExecutor{},
	}
	ctx, cancel := context.WithCancel(context.Background())
	events, _ := db.watchRows(ctx, tableLogicalSwitch, nil, 1, 1)
	manager := db.watchManager()
	waitForWatchSubscriptions(t, manager, tableLogicalSwitch, 1)

	manager.publish(tableLogicalSwitch, RowEvent{Type: EventAdd, New: Row{colName: "ls0"}})
	manager.publish(tableLogicalSwitch, RowEvent{Type: EventAdd, New: Row{colName: "ls1"}})
	cancel()
	select {
	case <-events:
	case <-time.After(time.Second):
		t.Fatal("watch forwarder did not unblock after subscription cancellation")
	}
	waitForWatchSubscriptions(t, manager, tableLogicalSwitch, 0)
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

func TestWatchSubscriptionBaselineSuppressesDuplicateInitialRows(t *testing.T) {
	manager := newWatchManager(&dbClient{database: dbOVNSouthbound})
	errs := make(chan error, 1)
	events := make(chan RowEvent, 4)
	sub := &rowWatchSubscription{id: 1, m: manager, table: tablePortBinding, events: events, errs: errs, done: make(chan struct{})}

	initial := RowEvent{Type: EventInitial, New: Row{colUUID: "pb-uuid", colLogicalPort: "lp0"}}
	baseline := RowEvent{Type: EventAdd, New: Row{colUUID: "pb-uuid", colLogicalPort: "lp0"}, baseline: true}
	if !sub.offer(baseline) {
		t.Fatal("baseline offer before initial failed")
	}
	sub.finishInitial([]RowEvent{initial})

	if got := <-events; got.Type != EventInitial {
		t.Fatalf("first event = %s, want initial", got.Type)
	}
	select {
	case got := <-events:
		t.Fatalf("duplicate baseline event was delivered: %#v", got)
	default:
	}
}

func TestWatchSubscriptionBaselineDeliversRowsMissingFromInitial(t *testing.T) {
	manager := newWatchManager(&dbClient{database: dbOVNSouthbound})
	errs := make(chan error, 1)
	events := make(chan RowEvent, 4)
	sub := &rowWatchSubscription{id: 1, m: manager, table: tablePortBinding, events: events, errs: errs, done: make(chan struct{})}

	baseline := RowEvent{Type: EventAdd, New: Row{colUUID: "pb-uuid", colLogicalPort: "lp0"}, baseline: true}
	if !sub.offer(baseline) {
		t.Fatal("baseline offer before initial failed")
	}
	sub.finishInitial(nil)

	got := <-events
	if got.Type != EventAdd || anyString(got.New[colUUID]) != "pb-uuid" {
		t.Fatalf("baseline event = %#v, want add for pb-uuid", got)
	}
}

func TestWatchSubscriptionBaselineBecomesUpdateWhenInitialDiffers(t *testing.T) {
	manager := newWatchManager(&dbClient{database: dbOVNSouthbound})
	errs := make(chan error, 1)
	events := make(chan RowEvent, 4)
	sub := &rowWatchSubscription{id: 1, m: manager, table: tablePortBinding, events: events, errs: errs, done: make(chan struct{})}

	initial := RowEvent{Type: EventInitial, New: Row{colUUID: "pb-uuid", colLogicalPort: "lp0", colExternalIDs: map[string]string{"phase": "initial"}}}
	baseline := RowEvent{Type: EventAdd, New: Row{colUUID: "pb-uuid", colLogicalPort: "lp0", colExternalIDs: map[string]string{"phase": "baseline"}}, baseline: true}
	if !sub.offer(baseline) {
		t.Fatal("baseline offer before initial failed")
	}
	sub.finishInitial([]RowEvent{initial})

	if got := <-events; got.Type != EventInitial {
		t.Fatalf("first event = %s, want initial", got.Type)
	}
	got := <-events
	if got.Type != EventUpdate {
		t.Fatalf("baseline event = %s, want update", got.Type)
	}
	if anyStringMap(got.Old[colExternalIDs])["phase"] != "initial" || anyStringMap(got.New[colExternalIDs])["phase"] != "baseline" {
		t.Fatalf("update old/new = %#v/%#v", got.Old, got.New)
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

func TestTableWatchCancelRemovesSubscriptionAndStopsPoller(t *testing.T) {
	db := &dbClient{
		database: dbOVNNorthbound,
		schema: newSchemaRegistry(dbOVNNorthbound, databaseSchemaWithColumns(dbOVNNorthbound, map[string][]string{
			tableLogicalSwitch: {colName},
		})),
		executor: &nbRecordingExecutor{},
	}
	ctx, cancel := context.WithCancel(context.Background())
	events, _ := db.Table(tableLogicalSwitch).Watch(ctx)

	waitForWatchSubscriptions(t, db.watchManager(), tableLogicalSwitch, 1)
	cancel()
	waitForWatchSubscriptions(t, db.watchManager(), tableLogicalSwitch, 0)

	select {
	case _, ok := <-events:
		if ok {
			t.Fatal("watch event channel delivered an event after cancellation")
		}
	case <-time.After(time.Second):
		t.Fatal("watch event channel did not close after cancellation")
	}
}

func TestTableWatchNilReferenceReturnsValidationError(t *testing.T) {
	tests := []struct {
		name string
		ref  *TableRef
	}{
		{name: "nil ref", ref: nil},
		{name: "nil db", ref: &TableRef{table: tableLogicalSwitch}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			events, errs := tt.ref.Watch(context.Background())
			if err := <-errs; !IsKind(err, ErrorValidation) {
				t.Fatalf("watch error = %v, want ErrorValidation", err)
			}
			if _, ok := <-events; ok {
				t.Fatal("watch event channel is open after validation failure")
			}
		})
	}
}

func TestTableWatchRejectsMissingConditionColumnBeforeSubscribing(t *testing.T) {
	db := &dbClient{
		database: dbOVNNorthbound,
		schema: newSchemaRegistry(dbOVNNorthbound, databaseSchemaWithColumns(dbOVNNorthbound, map[string][]string{
			tableLogicalSwitch: {colName},
		})),
		executor: &nbRecordingExecutor{},
	}
	events, errs := db.Table(tableLogicalSwitch).
		Where("missing_column", "value").
		Watch(context.Background())

	if err := <-errs; !IsKind(err, ErrorInvalidSchema) {
		t.Fatalf("watch error = %v, want ErrorInvalidSchema", err)
	}
	if _, ok := <-events; ok {
		t.Fatal("watch event channel is open after schema validation failure")
	}
	if db.watches != nil {
		t.Fatal("watch manager was initialized despite schema validation failure")
	}
}

func TestTableWatchConcurrentSubscribeCancelLeavesNoSubscriptions(t *testing.T) {
	db := &dbClient{
		database: dbOVNNorthbound,
		schema: newSchemaRegistry(dbOVNNorthbound, databaseSchemaWithColumns(dbOVNNorthbound, map[string][]string{
			tableLogicalSwitch: {colName},
		})),
		executor: &nbRecordingExecutor{},
	}

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithCancel(context.Background())
			events, _ := db.Table(tableLogicalSwitch).Watch(ctx)
			cancel()
			select {
			case <-events:
			case <-time.After(time.Second):
				t.Error("watch event channel did not close after cancellation")
			}
		}()
	}
	wg.Wait()
	waitForWatchSubscriptions(t, db.watchManager(), tableLogicalSwitch, 0)
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

func TestDBClientCloseStopsWatchManagerAndPollers(t *testing.T) {
	db := &dbClient{
		database: dbOVNNorthbound,
		schema: newSchemaRegistry(dbOVNNorthbound, databaseSchemaWithColumns(dbOVNNorthbound, map[string][]string{
			tableLogicalSwitch: {colName},
		})),
		executor: &nbRecordingExecutor{},
	}
	ctx := context.Background()
	events, _ := db.Table(tableLogicalSwitch).Watch(ctx)
	manager := db.watchManager()
	waitForWatchSubscriptions(t, manager, tableLogicalSwitch, 1)

	db.close()
	select {
	case _, ok := <-events:
		if ok {
			t.Fatal("watch event channel delivered an event after db close")
		}
	case <-time.After(time.Second):
		t.Fatal("watch event channel did not close after db close")
	}
	waitForWatchSubscriptions(t, manager, tableLogicalSwitch, 0)
	select {
	case <-manager.done:
	default:
		t.Fatal("watch manager done channel is not closed after db close")
	}
	db.close()
}

func waitForWatchSubscriptions(t *testing.T, manager *watchManager, table string, want int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		manager.mu.RLock()
		got := len(manager.byTable[table])
		_, pollerRunning := manager.pollOnce[table]
		manager.mu.RUnlock()
		if got == want && (want > 0 || !pollerRunning) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("watch subscriptions for %s = %d, poller running = %v, want %d", table, got, pollerRunning, want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

package ovnflow

import (
	"context"
	"encoding/json"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ovn-kubernetes/libovsdb/cache"
	libmodel "github.com/ovn-kubernetes/libovsdb/model"
	libovsdb "github.com/ovn-kubernetes/libovsdb/ovsdb"
)

const (
	defaultWatchEventBuffer = 64
	defaultWatchQueueBuffer = 256
	defaultWatchRawBuffer   = 4096
)

func (d *dbClient) watchRows(ctx context.Context, table string, where []libovsdb.Condition, eventBuffer, queueBuffer int) (<-chan RowEvent, <-chan error) {
	if eventBuffer <= 0 {
		eventBuffer = defaultWatchEventBuffer
	}
	if queueBuffer <= 0 {
		queueBuffer = defaultWatchQueueBuffer
	}

	events := make(chan RowEvent, eventBuffer)
	errs := make(chan error, 1)
	queue := make(chan RowEvent, queueBuffer)
	if err := d.schema.RequireTable(table); err != nil {
		errs <- err
		close(events)
		return events, errs
	}
	watches := d.watchManager()

	sub := watches.subscribe(ctx, table, where, queue, errs)
	go func() {
		defer close(events)
		defer sub.cancel()
		for {
			select {
			case event, ok := <-queue:
				if !ok {
					return
				}
				select {
				case events <- event:
				case <-ctx.Done():
					return
				}
			case <-sub.done:
				return
			case <-ctx.Done():
				return
			}
		}
	}()
	go sub.sendInitial(ctx)
	return events, errs
}

func (d *dbClient) watchManager() *watchManager {
	d.watchesMu.Lock()
	defer d.watchesMu.Unlock()
	if d.watches == nil {
		d.watches = newWatchManager(d)
	}
	return d.watches
}

type watchManager struct {
	db *dbClient

	once sync.Once
	in   chan rowWatchDispatch

	mu       sync.RWMutex
	nextID   uint64
	byTable  map[string]map[uint64]*rowWatchSubscription
	pollOnce map[string]chan struct{}
}

type rowWatchDispatch struct {
	table string
	event RowEvent
}

type rowWatchSubscription struct {
	mu          sync.Mutex
	id          uint64
	m           *watchManager
	table       string
	where       []libovsdb.Condition
	events      chan RowEvent
	errs        chan<- error
	done        chan struct{}
	closed      atomic.Bool
	initialDone bool
	pending     []RowEvent
}

func newWatchManager(db *dbClient) *watchManager {
	return &watchManager{
		db:       db,
		in:       make(chan rowWatchDispatch, defaultWatchRawBuffer),
		byTable:  map[string]map[uint64]*rowWatchSubscription{},
		pollOnce: map[string]chan struct{}{},
	}
}

func (m *watchManager) subscribe(ctx context.Context, table string, where []libovsdb.Condition, events chan RowEvent, errs chan<- error) *rowWatchSubscription {
	m.start()

	m.mu.Lock()
	m.nextID++
	sub := &rowWatchSubscription{
		id:     m.nextID,
		m:      m,
		table:  table,
		where:  append([]libovsdb.Condition{}, where...),
		events: events,
		errs:   errs,
		done:   make(chan struct{}),
	}
	if m.byTable[table] == nil {
		m.byTable[table] = map[uint64]*rowWatchSubscription{}
	}
	m.byTable[table][sub.id] = sub
	m.mu.Unlock()

	if m.db.monitorModel(table) == nil {
		m.startPoller(table)
	}
	go func() {
		select {
		case <-ctx.Done():
			sub.cancel()
		case <-sub.done:
		}
	}()
	return sub
}

func (m *watchManager) start() {
	m.once.Do(func() {
		if m.db != nil && m.db.raw != nil && m.db.raw.Cache() != nil {
			m.db.raw.Cache().AddEventHandler(&cache.EventHandlerFuncs{
				AddFunc: func(table string, row libmodel.Model) {
					m.enqueue(table, RowEvent{Type: EventAdd, New: decodeModelRow(row)})
				},
				UpdateFunc: func(table string, old libmodel.Model, newModel libmodel.Model) {
					m.enqueue(table, RowEvent{Type: EventUpdate, Old: decodeModelRow(old), New: decodeModelRow(newModel)})
				},
				DeleteFunc: func(table string, row libmodel.Model) {
					m.enqueue(table, RowEvent{Type: EventDelete, Old: decodeModelRow(row)})
				},
			})
		}
		go m.run()
	})
}

func (m *watchManager) enqueue(table string, event RowEvent) {
	select {
	case m.in <- rowWatchDispatch{table: table, event: event}:
	default:
		m.publishError(table, wrap(ErrorPartial, m.db.database, table, "watch", "", "watch dispatch queue overflow", nil))
	}
}

func (m *watchManager) run() {
	for dispatch := range m.in {
		m.publish(dispatch.table, dispatch.event)
	}
}

func (m *watchManager) publish(table string, event RowEvent) {
	m.mu.RLock()
	subs := make([]*rowWatchSubscription, 0, len(m.byTable[table]))
	for _, sub := range m.byTable[table] {
		subs = append(subs, sub)
	}
	m.mu.RUnlock()
	for _, sub := range subs {
		sub.offer(event)
	}
}

func (m *watchManager) publishError(table string, err error) {
	m.mu.RLock()
	subs := make([]*rowWatchSubscription, 0, len(m.byTable[table]))
	for _, sub := range m.byTable[table] {
		subs = append(subs, sub)
	}
	m.mu.RUnlock()
	for _, sub := range subs {
		sub.offerError(err)
	}
}

func (m *watchManager) remove(sub *rowWatchSubscription) {
	m.mu.Lock()
	defer m.mu.Unlock()
	subs := m.byTable[sub.table]
	if subs == nil {
		return
	}
	delete(subs, sub.id)
	if len(subs) == 0 {
		delete(m.byTable, sub.table)
		if stop, ok := m.pollOnce[sub.table]; ok {
			close(stop)
			delete(m.pollOnce, sub.table)
		}
	}
}

func (m *watchManager) startPoller(table string) {
	m.mu.Lock()
	if _, ok := m.pollOnce[table]; ok {
		m.mu.Unlock()
		return
	}
	stop := make(chan struct{})
	m.pollOnce[table] = stop
	m.mu.Unlock()

	go m.pollRows(table, stop)
}

func (m *watchManager) pollRows(table string, stop <-chan struct{}) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	seen := map[string]Row{}
	for {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		rows, err := newTableRef(m.db, table, "", "").selectRows(ctx, nil, nil)
		cancel()
		if err == nil {
			next := map[string]Row{}
			for _, row := range rows {
				id := rowIdentity(row)
				if id == "" {
					continue
				}
				next[id] = row
				old, hadOld := seen[id]
				switch {
				case !hadOld:
					m.enqueue(table, RowEvent{Type: EventAdd, New: row})
				case rowFingerprint(old) != rowFingerprint(row):
					m.enqueue(table, RowEvent{Type: EventUpdate, Old: old, New: row})
				}
			}
			for id, old := range seen {
				if _, ok := next[id]; !ok {
					m.enqueue(table, RowEvent{Type: EventDelete, Old: old})
				}
			}
			seen = next
		} else {
			m.publishError(table, err)
		}

		select {
		case <-ticker.C:
		case <-stop:
			return
		}
	}
}

func (s *rowWatchSubscription) sendInitial(ctx context.Context) {
	rows, err := newTableRef(s.m.db, s.table, "", "").selectRows(ctx, s.where, nil)
	if err != nil {
		s.finishInitial(nil)
		s.offerError(err)
		return
	}
	initial := make([]RowEvent, 0, len(rows))
	for _, row := range rows {
		initial = append(initial, RowEvent{Type: EventInitial, New: row})
	}
	s.finishInitial(initial)
}

func (s *rowWatchSubscription) offer(event RowEvent) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.offerLocked(event)
}

func (s *rowWatchSubscription) offerLocked(event RowEvent) bool {
	if s.closed.Load() || !eventMatches(s.where, event) {
		return false
	}
	if !s.initialDone {
		limit := cap(s.events)
		if limit <= 0 {
			limit = defaultWatchQueueBuffer
		}
		if len(s.pending) >= limit {
			s.reportOverflowLocked()
			return false
		}
		s.pending = append(s.pending, event)
		return true
	}
	return s.sendLocked(event)
}

func (s *rowWatchSubscription) sendLocked(event RowEvent) bool {
	select {
	case s.events <- event:
		return true
	default:
		s.reportOverflowLocked()
		return false
	}
}

func (s *rowWatchSubscription) finishInitial(initial []RowEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed.Load() {
		return
	}
	for _, event := range initial {
		if !s.sendLocked(event) {
			return
		}
	}
	s.initialDone = true
	pending := s.pending
	s.pending = nil
	for _, event := range pending {
		if !s.sendLocked(event) {
			return
		}
	}
}

func (s *rowWatchSubscription) reportOverflowLocked() {
	select {
	case s.errs <- wrap(ErrorPartial, s.m.db.database, s.table, "watch", "", "watch event queue overflow", nil):
	default:
	}
	s.closeLocked()
}

func (s *rowWatchSubscription) offerError(err error) {
	if err == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed.Load() {
		return
	}
	select {
	case s.errs <- err:
	default:
	}
}

func (s *rowWatchSubscription) cancel() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed.Load() {
		return
	}
	s.closeLocked()
}

func (s *rowWatchSubscription) closeLocked() {
	s.closed.Store(true)
	s.m.remove(s)
	close(s.done)
}

func (d *dbClient) monitorModel(table string) libmodel.Model {
	switch d.database {
	case dbOVNNorthbound:
		switch table {
		case tableLogicalSwitch:
			return &LogicalSwitch{}
		case tableLogicalSwitchPort:
			return &LogicalSwitchPort{}
		}
	case dbOVNSouthbound:
		switch table {
		case tableChassis:
			return &SBChassis{}
		case tablePortBinding:
			return &SBPortBinding{}
		case tableDatapathBinding:
			return &SBDatapathBinding{}
		case tableLogicalFlow:
			return &SBLogicalFlow{}
		case tableMACBinding:
			return &SBMACBinding{}
		case tableFDB:
			return &SBFDB{}
		case tableMulticastGroup:
			return &SBMulticastGroup{}
		case tableServiceMonitor:
			return &SBServiceMonitor{}
		case tableRBACRole:
			return &SBRBACRole{}
		case tableRBACPermission:
			return &SBRBACPermission{}
		case tableMeter:
			return &SBMeter{}
		case tableMeterBand:
			return &SBMeterBand{}
		case tableDNS:
			return &SBDNS{}
		case tableBFD:
			return &SBBFD{}
		}
	case dbOpenVSwitch:
		switch table {
		case tableOpenVSwitch:
			return &OpenVSwitch{}
		case tableBridge:
			return &OVSBridge{}
		case tablePort:
			return &OVSPort{}
		case tableInterface:
			return &OVSInterface{}
		}
	}
	return nil
}

func eventMatches(where []libovsdb.Condition, event RowEvent) bool {
	if len(where) == 0 {
		return true
	}
	return rowMatches(where, event.New) || rowMatches(where, event.Old)
}

func rowMatches(where []libovsdb.Condition, row Row) bool {
	if len(where) == 0 {
		return true
	}
	if row == nil {
		return false
	}
	for _, condition := range where {
		got, ok := row[condition.Column]
		if !ok {
			return false
		}
		if !conditionValueEqual(got, condition.Value) {
			return false
		}
	}
	return true
}

func conditionValueEqual(got, want any) bool {
	switch typed := want.(type) {
	case libovsdb.UUID:
		return got == typed.GoUUID || anyString(got) == typed.GoUUID
	case string:
		return got == typed || anyString(got) == typed
	case libovsdb.OvsMap:
		gotMap := anyStringMap(got)
		wantMap := anyStringMap(typed)
		if len(wantMap) == 0 {
			return len(gotMap) == 0
		}
		for k, v := range wantMap {
			if gotMap[k] != v {
				return false
			}
		}
		return true
	default:
		return reflect.DeepEqual(got, want)
	}
}

func rowIdentity(row Row) string {
	if row == nil {
		return ""
	}
	value, ok := row[colUUID]
	if !ok {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return typed
	case []any:
		if len(typed) == 2 {
			if marker, ok := typed[0].(string); ok && marker == "uuid" {
				if id, ok := typed[1].(string); ok {
					return id
				}
			}
		}
	case map[string]any:
		if id, ok := typed["GoUUID"].(string); ok {
			return id
		}
	}
	return ""
}

func rowFingerprint(row Row) string {
	data, _ := json.Marshal(row)
	return string(data)
}

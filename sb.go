package ovnflow

import (
	"context"
	"sync/atomic"

	"github.com/ovn-kubernetes/libovsdb/cache"
	ovsclient "github.com/ovn-kubernetes/libovsdb/client"
	libmodel "github.com/ovn-kubernetes/libovsdb/model"
)

// SBClient exposes read-only OVN Southbound APIs.
type SBClient struct {
	db *dbClient
}

type EventType string

const (
	EventInitial EventType = "initial"
	EventAdd     EventType = "add"
	EventUpdate  EventType = "update"
	EventDelete  EventType = "delete"
)

type PortBindingEvent struct {
	Type EventType
	Old  *SBPortBinding
	New  *SBPortBinding
}

type ChassisEvent struct {
	Type EventType
	Old  *SBChassis
	New  *SBChassis
}

func (e PortBindingEvent) isZero() bool {
	return e.Type == "" && e.Old == nil && e.New == nil
}

func (e ChassisEvent) isZero() bool {
	return e.Type == "" && e.Old == nil && e.New == nil
}

func (s *SBClient) ListChassis(ctx context.Context) ([]SBChassis, error) {
	var out []SBChassis
	if err := s.db.raw.List(ctx, &out); err != nil {
		return nil, classifyTransactError(err, dbOVNSouthbound, tableChassis, "list", "")
	}
	return out, nil
}

func (s *SBClient) ListPortBindings(ctx context.Context) ([]SBPortBinding, error) {
	var out []SBPortBinding
	if err := s.db.raw.List(ctx, &out); err != nil {
		return nil, classifyTransactError(err, dbOVNSouthbound, tablePortBinding, "list", "")
	}
	return out, nil
}

func (s *SBClient) ListDatapaths(ctx context.Context) ([]SBDatapathBinding, error) {
	var out []SBDatapathBinding
	if err := s.db.raw.List(ctx, &out); err != nil {
		return nil, classifyTransactError(err, dbOVNSouthbound, tableDatapathBinding, "list", "")
	}
	return out, nil
}

func (s *SBClient) WatchPortBindings(ctx context.Context) (<-chan PortBindingEvent, <-chan error) {
	events := make(chan PortBindingEvent, 64)
	errs := make(chan error, 1)
	queue := make(chan PortBindingEvent, 256)
	var active atomic.Bool
	active.Store(true)
	push := func(event PortBindingEvent) {
		if !active.Load() || ctx.Err() != nil || event.isZero() {
			return
		}
		select {
		case queue <- event:
		default:
			select {
			case errs <- wrap(ErrorPartial, dbOVNSouthbound, tablePortBinding, "watch", "", "port binding watch event queue overflow", nil):
			default:
			}
		}
	}
	handler := &cache.EventHandlerFuncs{
		AddFunc: func(table string, m libmodel.Model) {
			if !active.Load() {
				return
			}
			if table != tablePortBinding {
				return
			}
			if pb, ok := m.(*SBPortBinding); ok {
				push(PortBindingEvent{Type: EventAdd, New: pb})
			}
		},
		UpdateFunc: func(table string, old libmodel.Model, newModel libmodel.Model) {
			if !active.Load() {
				return
			}
			if table != tablePortBinding {
				return
			}
			oldPB, _ := old.(*SBPortBinding)
			newPB, _ := newModel.(*SBPortBinding)
			push(PortBindingEvent{Type: EventUpdate, Old: oldPB, New: newPB})
		},
		DeleteFunc: func(table string, m libmodel.Model) {
			if !active.Load() {
				return
			}
			if table != tablePortBinding {
				return
			}
			if pb, ok := m.(*SBPortBinding); ok {
				push(PortBindingEvent{Type: EventDelete, Old: pb})
			}
		},
	}
	s.db.raw.Cache().AddEventHandler(handler)
	startDedicatedMonitor(ctx, s.db.raw, &SBPortBinding{})
	go func() {
		defer close(events)
		for {
			select {
			case event := <-queue:
				select {
				case events <- event:
				case <-ctx.Done():
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() {
		defer active.Store(false)
		rows, err := s.ListPortBindings(ctx)
		if err != nil {
			errs <- err
			return
		}
		for i := range rows {
			row := rows[i]
			select {
			case queue <- PortBindingEvent{Type: EventInitial, New: &row}:
			case <-ctx.Done():
				errs <- classifyContext(ctx.Err(), dbOVNSouthbound, tablePortBinding, "watch", "")
				return
			}
		}
		<-ctx.Done()
	}()
	return events, errs
}

func (s *SBClient) WatchChassis(ctx context.Context) (<-chan ChassisEvent, <-chan error) {
	events := make(chan ChassisEvent, 64)
	errs := make(chan error, 1)
	queue := make(chan ChassisEvent, 256)
	var active atomic.Bool
	active.Store(true)
	push := func(event ChassisEvent) {
		if !active.Load() || ctx.Err() != nil || event.isZero() {
			return
		}
		select {
		case queue <- event:
		default:
			select {
			case errs <- wrap(ErrorPartial, dbOVNSouthbound, tableChassis, "watch", "", "chassis watch event queue overflow", nil):
			default:
			}
		}
	}
	handler := &cache.EventHandlerFuncs{
		AddFunc: func(table string, m libmodel.Model) {
			if !active.Load() {
				return
			}
			if table != tableChassis {
				return
			}
			if ch, ok := m.(*SBChassis); ok {
				push(ChassisEvent{Type: EventAdd, New: ch})
			}
		},
		UpdateFunc: func(table string, old libmodel.Model, newModel libmodel.Model) {
			if !active.Load() {
				return
			}
			if table != tableChassis {
				return
			}
			oldCh, _ := old.(*SBChassis)
			newCh, _ := newModel.(*SBChassis)
			push(ChassisEvent{Type: EventUpdate, Old: oldCh, New: newCh})
		},
		DeleteFunc: func(table string, m libmodel.Model) {
			if !active.Load() {
				return
			}
			if table != tableChassis {
				return
			}
			if ch, ok := m.(*SBChassis); ok {
				push(ChassisEvent{Type: EventDelete, Old: ch})
			}
		},
	}
	s.db.raw.Cache().AddEventHandler(handler)
	startDedicatedMonitor(ctx, s.db.raw, &SBChassis{})
	go func() {
		defer close(events)
		for {
			select {
			case event := <-queue:
				select {
				case events <- event:
				case <-ctx.Done():
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() {
		defer active.Store(false)
		rows, err := s.ListChassis(ctx)
		if err != nil {
			errs <- err
			return
		}
		for i := range rows {
			row := rows[i]
			select {
			case queue <- ChassisEvent{Type: EventInitial, New: &row}:
			case <-ctx.Done():
				errs <- classifyContext(ctx.Err(), dbOVNSouthbound, tableChassis, "watch", "")
				return
			}
		}
		<-ctx.Done()
	}()
	return events, errs
}

func startDedicatedMonitor(ctx context.Context, raw ovsclient.Client, model libmodel.Model) {
	monitor := raw.NewMonitor(ovsclient.WithTable(model))
	if len(monitor.Errors) > 0 {
		return
	}
	cookie, err := raw.Monitor(ctx, monitor)
	if err != nil {
		return
	}
	go func() {
		<-ctx.Done()
		cancelCtx := context.Background()
		_ = raw.MonitorCancel(cancelCtx, cookie)
	}()
}

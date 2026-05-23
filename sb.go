package ovnflow

import (
	"context"

	libovsdb "github.com/ovn-kubernetes/libovsdb/ovsdb"
)

// SBClient exposes OVN Southbound APIs.
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

type DatapathEvent struct {
	Type EventType
	Old  *SBDatapathBinding
	New  *SBDatapathBinding
}

type LogicalFlowEvent struct {
	Type EventType
	Old  *SBLogicalFlow
	New  *SBLogicalFlow
}

type MACBindingEvent struct {
	Type EventType
	Old  *SBMACBinding
	New  *SBMACBinding
}

type FDBEvent struct {
	Type EventType
	Old  *SBFDB
	New  *SBFDB
}

type MulticastGroupEvent struct {
	Type EventType
	Old  *SBMulticastGroup
	New  *SBMulticastGroup
}

type ServiceMonitorEvent struct {
	Type EventType
	Old  *SBServiceMonitor
	New  *SBServiceMonitor
}

type RBACRoleEvent struct {
	Type EventType
	Old  *SBRBACRole
	New  *SBRBACRole
}

type RBACPermissionEvent struct {
	Type EventType
	Old  *SBRBACPermission
	New  *SBRBACPermission
}

type MeterEvent struct {
	Type EventType
	Old  *SBMeter
	New  *SBMeter
}

type MeterBandEvent struct {
	Type EventType
	Old  *SBMeterBand
	New  *SBMeterBand
}

type DNSEvent struct {
	Type EventType
	Old  *SBDNS
	New  *SBDNS
}

type BFDEvent struct {
	Type EventType
	Old  *SBBFD
	New  *SBBFD
}

func (s *SBClient) ListChassis(ctx context.Context) ([]SBChassis, error) {
	return listSB[SBChassis](ctx, s, tableChassis)
}

func (s *SBClient) ListPortBindings(ctx context.Context) ([]SBPortBinding, error) {
	return listSB[SBPortBinding](ctx, s, tablePortBinding)
}

func (s *SBClient) ListDatapaths(ctx context.Context) ([]SBDatapathBinding, error) {
	return listSB[SBDatapathBinding](ctx, s, tableDatapathBinding)
}

func (s *SBClient) ListLogicalFlows(ctx context.Context) ([]SBLogicalFlow, error) {
	return listSB[SBLogicalFlow](ctx, s, tableLogicalFlow)
}

func (s *SBClient) ListMACBindings(ctx context.Context) ([]SBMACBinding, error) {
	return listSB[SBMACBinding](ctx, s, tableMACBinding)
}

func (s *SBClient) ListFDB(ctx context.Context) ([]SBFDB, error) {
	return listSB[SBFDB](ctx, s, tableFDB)
}

func (s *SBClient) ListMulticastGroups(ctx context.Context) ([]SBMulticastGroup, error) {
	return listSB[SBMulticastGroup](ctx, s, tableMulticastGroup)
}

func (s *SBClient) ListServiceMonitors(ctx context.Context) ([]SBServiceMonitor, error) {
	return listSB[SBServiceMonitor](ctx, s, tableServiceMonitor)
}

func (s *SBClient) ListRBACRoles(ctx context.Context) ([]SBRBACRole, error) {
	return listSB[SBRBACRole](ctx, s, tableRBACRole)
}

func (s *SBClient) ListRBACPermissions(ctx context.Context) ([]SBRBACPermission, error) {
	return listSB[SBRBACPermission](ctx, s, tableRBACPermission)
}

func (s *SBClient) ListMeters(ctx context.Context) ([]SBMeter, error) {
	return listSB[SBMeter](ctx, s, tableMeter)
}

func (s *SBClient) ListMeterBands(ctx context.Context) ([]SBMeterBand, error) {
	return listSB[SBMeterBand](ctx, s, tableMeterBand)
}

func (s *SBClient) ListDNS(ctx context.Context) ([]SBDNS, error) {
	return listSB[SBDNS](ctx, s, tableDNS)
}

func (s *SBClient) ListBFD(ctx context.Context) ([]SBBFD, error) {
	return listSB[SBBFD](ctx, s, tableBFD)
}

func (s *SBClient) GetChassis(ctx context.Context, name string) (*SBChassis, error) {
	return getSB[*SBChassis](ctx, s, tableChassis, &SBChassis{Name: name}, name)
}

func (s *SBClient) GetPortBinding(ctx context.Context, logicalPort string) (*SBPortBinding, error) {
	return getSB[*SBPortBinding](ctx, s, tablePortBinding, &SBPortBinding{LogicalPort: logicalPort}, logicalPort)
}

func (s *SBClient) GetDatapath(ctx context.Context, tunnelKey int) (*SBDatapathBinding, error) {
	return getSB[*SBDatapathBinding](ctx, s, tableDatapathBinding, &SBDatapathBinding{TunnelKey: tunnelKey}, "")
}

func (s *SBClient) GetDatapathByUUID(ctx context.Context, uuid string) (*SBDatapathBinding, error) {
	return getSB[*SBDatapathBinding](ctx, s, tableDatapathBinding, &SBDatapathBinding{UUID: uuid}, uuid)
}

func (s *SBClient) GetLogicalFlow(ctx context.Context, uuid string) (*SBLogicalFlow, error) {
	return getSB[*SBLogicalFlow](ctx, s, tableLogicalFlow, &SBLogicalFlow{UUID: uuid}, uuid)
}

func (s *SBClient) GetMACBinding(ctx context.Context, logicalPort, ip string) (*SBMACBinding, error) {
	return getSB[*SBMACBinding](ctx, s, tableMACBinding, &SBMACBinding{LogicalPort: logicalPort, IP: ip}, logicalPort)
}

func (s *SBClient) GetFDB(ctx context.Context, mac string, dpKey int) (*SBFDB, error) {
	return getSB[*SBFDB](ctx, s, tableFDB, &SBFDB{MAC: mac, DPKey: dpKey}, mac)
}

func (s *SBClient) GetMulticastGroup(ctx context.Context, datapath string, tunnelKey int) (*SBMulticastGroup, error) {
	return getSB[*SBMulticastGroup](ctx, s, tableMulticastGroup, &SBMulticastGroup{Datapath: datapath, TunnelKey: tunnelKey}, datapath)
}

func (s *SBClient) GetServiceMonitor(ctx context.Context, logicalPort, ip, protocol string, port int) (*SBServiceMonitor, error) {
	return getSB[*SBServiceMonitor](ctx, s, tableServiceMonitor, &SBServiceMonitor{LogicalPort: logicalPort, IP: ip, Protocol: stringPtr(protocol), Port: port}, logicalPort)
}

func (s *SBClient) GetRBACRole(ctx context.Context, name string) (*SBRBACRole, error) {
	return getSB[*SBRBACRole](ctx, s, tableRBACRole, &SBRBACRole{Name: name}, name)
}

func (s *SBClient) GetRBACPermission(ctx context.Context, uuid string) (*SBRBACPermission, error) {
	return getSB[*SBRBACPermission](ctx, s, tableRBACPermission, &SBRBACPermission{UUID: uuid}, uuid)
}

func (s *SBClient) GetMeter(ctx context.Context, name string) (*SBMeter, error) {
	return getSB[*SBMeter](ctx, s, tableMeter, &SBMeter{Name: name}, name)
}

func (s *SBClient) GetMeterBand(ctx context.Context, uuid string) (*SBMeterBand, error) {
	return getSB[*SBMeterBand](ctx, s, tableMeterBand, &SBMeterBand{UUID: uuid}, uuid)
}

func (s *SBClient) GetDNS(ctx context.Context, uuid string) (*SBDNS, error) {
	return getSB[*SBDNS](ctx, s, tableDNS, &SBDNS{UUID: uuid}, uuid)
}

func (s *SBClient) GetBFD(ctx context.Context, logicalPort, dstIP string, srcPort, disc int) (*SBBFD, error) {
	return getSB[*SBBFD](ctx, s, tableBFD, &SBBFD{LogicalPort: logicalPort, DstIP: dstIP, SrcPort: srcPort, Disc: disc}, logicalPort)
}

func (s *SBClient) WatchPortBindings(ctx context.Context) (<-chan PortBindingEvent, <-chan error) {
	return watchSB(ctx, s, tablePortBinding, portBindingEventFromRowEvent)
}

func (s *SBClient) WatchTable(ctx context.Context, table string) (<-chan RowEvent, <-chan error) {
	if s == nil || s.db == nil {
		events := make(chan RowEvent)
		errs := make(chan error, 1)
		errs <- wrap(ErrorUnavailable, dbOVNSouthbound, table, "watch", "", "southbound client is nil", nil)
		close(events)
		return events, errs
	}
	return s.Table(table).Watch(ctx)
}

func (s *SBClient) WatchChassis(ctx context.Context) (<-chan ChassisEvent, <-chan error) {
	return watchSB(ctx, s, tableChassis, chassisEventFromRowEvent)
}

func (s *SBClient) WatchDatapaths(ctx context.Context) (<-chan DatapathEvent, <-chan error) {
	return watchSB(ctx, s, tableDatapathBinding, datapathEventFromRowEvent)
}

func (s *SBClient) WatchLogicalFlows(ctx context.Context) (<-chan LogicalFlowEvent, <-chan error) {
	return watchSB(ctx, s, tableLogicalFlow, logicalFlowEventFromRowEvent)
}

func (s *SBClient) WatchMACBindings(ctx context.Context) (<-chan MACBindingEvent, <-chan error) {
	return watchSB(ctx, s, tableMACBinding, macBindingEventFromRowEvent)
}

func (s *SBClient) WatchFDB(ctx context.Context) (<-chan FDBEvent, <-chan error) {
	return watchSB(ctx, s, tableFDB, fdbEventFromRowEvent)
}

func (s *SBClient) WatchMulticastGroups(ctx context.Context) (<-chan MulticastGroupEvent, <-chan error) {
	return watchSB(ctx, s, tableMulticastGroup, multicastGroupEventFromRowEvent)
}

func (s *SBClient) WatchServiceMonitors(ctx context.Context) (<-chan ServiceMonitorEvent, <-chan error) {
	return watchSB(ctx, s, tableServiceMonitor, serviceMonitorEventFromRowEvent)
}

func (s *SBClient) WatchRBACRoles(ctx context.Context) (<-chan RBACRoleEvent, <-chan error) {
	return watchSB(ctx, s, tableRBACRole, rbacRoleEventFromRowEvent)
}

func (s *SBClient) WatchRBACPermissions(ctx context.Context) (<-chan RBACPermissionEvent, <-chan error) {
	return watchSB(ctx, s, tableRBACPermission, rbacPermissionEventFromRowEvent)
}

func (s *SBClient) WatchMeters(ctx context.Context) (<-chan MeterEvent, <-chan error) {
	return watchSB(ctx, s, tableMeter, meterEventFromRowEvent)
}

func (s *SBClient) WatchMeterBands(ctx context.Context) (<-chan MeterBandEvent, <-chan error) {
	return watchSB(ctx, s, tableMeterBand, meterBandEventFromRowEvent)
}

func (s *SBClient) WatchDNS(ctx context.Context) (<-chan DNSEvent, <-chan error) {
	return watchSB(ctx, s, tableDNS, dnsEventFromRowEvent)
}

func (s *SBClient) WatchBFD(ctx context.Context) (<-chan BFDEvent, <-chan error) {
	return watchSB(ctx, s, tableBFD, bfdEventFromRowEvent)
}

func listSB[T any](ctx context.Context, s *SBClient, table string) ([]T, error) {
	var out []T
	if s == nil || s.db == nil {
		return nil, wrap(ErrorUnavailable, dbOVNSouthbound, table, "list", "", "southbound client is nil", nil)
	}
	rows, err := s.Table(table).List(ctx)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		item, err := decodeSBTypedRow[T](table, row)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, nil
}

func getSB[T any](ctx context.Context, s *SBClient, table string, identity any, object string) (T, error) {
	var zero T
	if s == nil || s.db == nil {
		return zero, wrap(ErrorUnavailable, dbOVNSouthbound, table, "get", object, "southbound client is nil", nil)
	}
	where, err := sbIdentityConditions(table, identity)
	if err != nil {
		return zero, err
	}
	rows, err := s.Table(table).WhereConditions(where...).List(ctx)
	if err != nil {
		return zero, err
	}
	if len(rows) == 0 {
		return zero, wrap(ErrorNotFound, dbOVNSouthbound, table, "get", object, "row not found", nil)
	}
	item, err := decodeSBTypedRow[T](table, rows[0])
	if err != nil {
		return zero, err
	}
	return item, nil
}

func watchSB[E any](ctx context.Context, s *SBClient, table string, convert func(RowEvent) E) (<-chan E, <-chan error) {
	events := make(chan E, defaultWatchEventBuffer)
	errs := make(chan error, 1)
	rowEvents, rowErrs := watchSBTable(ctx, s, table)
	go func() {
		defer close(events)
		for {
			select {
			case event, ok := <-rowEvents:
				if !ok {
					return
				}
				select {
				case events <- convert(event):
				case <-ctx.Done():
					return
				}
			case err := <-rowErrs:
				if err != nil {
					select {
					case errs <- err:
					default:
					}
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return events, errs
}

func watchSBTable(ctx context.Context, s *SBClient, table string) (<-chan RowEvent, <-chan error) {
	if s == nil {
		events := make(chan RowEvent)
		errs := make(chan error, 1)
		errs <- wrap(ErrorUnavailable, dbOVNSouthbound, table, "watch", "", "southbound client is nil", nil)
		close(events)
		return events, errs
	}
	return s.WatchTable(ctx, table)
}

func sbIdentityConditions(table string, identity any) ([]libovsdb.Condition, error) {
	switch table {
	case tableChassis:
		row := identity.(*SBChassis)
		return []libovsdb.Condition{libovsdb.NewCondition(colName, libovsdb.ConditionEqual, row.Name)}, nil
	case tablePortBinding:
		row := identity.(*SBPortBinding)
		return []libovsdb.Condition{libovsdb.NewCondition(colLogicalPort, libovsdb.ConditionEqual, row.LogicalPort)}, nil
	case tableDatapathBinding:
		row := identity.(*SBDatapathBinding)
		if row.UUID != "" {
			return conditionUUID(row.UUID), nil
		}
		return []libovsdb.Condition{libovsdb.NewCondition(colTunnelKey, libovsdb.ConditionEqual, row.TunnelKey)}, nil
	case tableLogicalFlow:
		row := identity.(*SBLogicalFlow)
		return conditionUUID(row.UUID), nil
	case tableMACBinding:
		row := identity.(*SBMACBinding)
		return []libovsdb.Condition{
			libovsdb.NewCondition(colLogicalPort, libovsdb.ConditionEqual, row.LogicalPort),
			libovsdb.NewCondition(colIP, libovsdb.ConditionEqual, row.IP),
		}, nil
	case tableFDB:
		row := identity.(*SBFDB)
		return []libovsdb.Condition{
			libovsdb.NewCondition(colMAC, libovsdb.ConditionEqual, row.MAC),
			libovsdb.NewCondition(colDPKey, libovsdb.ConditionEqual, row.DPKey),
		}, nil
	case tableMulticastGroup:
		row := identity.(*SBMulticastGroup)
		return []libovsdb.Condition{
			libovsdb.NewCondition(colDatapath, libovsdb.ConditionEqual, uuidValue(row.Datapath)),
			libovsdb.NewCondition(colTunnelKey, libovsdb.ConditionEqual, row.TunnelKey),
		}, nil
	case tableServiceMonitor:
		row := identity.(*SBServiceMonitor)
		return []libovsdb.Condition{
			libovsdb.NewCondition(colLogicalPort, libovsdb.ConditionEqual, row.LogicalPort),
			libovsdb.NewCondition(colIP, libovsdb.ConditionEqual, row.IP),
			libovsdb.NewCondition(colProtocol, libovsdb.ConditionEqual, optionalStringValue(row.Protocol)),
			libovsdb.NewCondition(colPort, libovsdb.ConditionEqual, row.Port),
		}, nil
	case tableRBACRole:
		row := identity.(*SBRBACRole)
		return []libovsdb.Condition{libovsdb.NewCondition(colName, libovsdb.ConditionEqual, row.Name)}, nil
	case tableRBACPermission:
		row := identity.(*SBRBACPermission)
		return conditionUUID(row.UUID), nil
	case tableMeter:
		row := identity.(*SBMeter)
		return []libovsdb.Condition{libovsdb.NewCondition(colName, libovsdb.ConditionEqual, row.Name)}, nil
	case tableMeterBand:
		row := identity.(*SBMeterBand)
		return conditionUUID(row.UUID), nil
	case tableDNS:
		row := identity.(*SBDNS)
		return conditionUUID(row.UUID), nil
	case tableBFD:
		row := identity.(*SBBFD)
		return []libovsdb.Condition{
			libovsdb.NewCondition(colLogicalPort, libovsdb.ConditionEqual, row.LogicalPort),
			libovsdb.NewCondition(colDstIP, libovsdb.ConditionEqual, row.DstIP),
			libovsdb.NewCondition(colSrcPort, libovsdb.ConditionEqual, row.SrcPort),
			libovsdb.NewCondition(colDisc, libovsdb.ConditionEqual, row.Disc),
		}, nil
	default:
		return nil, wrap(ErrorInvalidSchema, dbOVNSouthbound, table, "get", "", "unsupported southbound table", nil)
	}
}

func optionalStringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func decodeSBTypedRow[T any](table string, row Row) (T, error) {
	var zero T
	var value any
	switch table {
	case tableChassis:
		value = sbChassisFromRow(row)
	case tablePortBinding:
		value = sbPortBindingFromRow(row)
	case tableDatapathBinding:
		value = sbDatapathFromRow(row)
	case tableLogicalFlow:
		value = sbLogicalFlowFromRow(row)
	case tableMACBinding:
		value = sbMACBindingFromRow(row)
	case tableFDB:
		value = sbFDBFromRow(row)
	case tableMulticastGroup:
		value = sbMulticastGroupFromRow(row)
	case tableServiceMonitor:
		value = sbServiceMonitorFromRow(row)
	case tableRBACRole:
		value = sbRBACRoleFromRow(row)
	case tableRBACPermission:
		value = sbRBACPermissionFromRow(row)
	case tableMeter:
		value = sbMeterFromRow(row)
	case tableMeterBand:
		value = sbMeterBandFromRow(row)
	case tableDNS:
		value = sbDNSFromRow(row)
	case tableBFD:
		value = sbBFDFromRow(row)
	default:
		return zero, wrap(ErrorInvalidSchema, dbOVNSouthbound, table, "decode", "", "unsupported southbound table", nil)
	}
	if ptr, ok := value.(*T); ok {
		return *ptr, nil
	}
	if typed, ok := value.(T); ok {
		return typed, nil
	}
	return zero, wrap(ErrorConflict, dbOVNSouthbound, table, "decode", "", "southbound row type mismatch", nil)
}

func portBindingEventFromRowEvent(event RowEvent) PortBindingEvent {
	return PortBindingEvent{Type: event.Type, Old: sbPortBindingFromRow(event.Old), New: sbPortBindingFromRow(event.New)}
}

func chassisEventFromRowEvent(event RowEvent) ChassisEvent {
	return ChassisEvent{Type: event.Type, Old: sbChassisFromRow(event.Old), New: sbChassisFromRow(event.New)}
}

func datapathEventFromRowEvent(event RowEvent) DatapathEvent {
	return DatapathEvent{Type: event.Type, Old: sbDatapathFromRow(event.Old), New: sbDatapathFromRow(event.New)}
}

func logicalFlowEventFromRowEvent(event RowEvent) LogicalFlowEvent {
	return LogicalFlowEvent{Type: event.Type, Old: sbLogicalFlowFromRow(event.Old), New: sbLogicalFlowFromRow(event.New)}
}

func macBindingEventFromRowEvent(event RowEvent) MACBindingEvent {
	return MACBindingEvent{Type: event.Type, Old: sbMACBindingFromRow(event.Old), New: sbMACBindingFromRow(event.New)}
}

func fdbEventFromRowEvent(event RowEvent) FDBEvent {
	return FDBEvent{Type: event.Type, Old: sbFDBFromRow(event.Old), New: sbFDBFromRow(event.New)}
}

func multicastGroupEventFromRowEvent(event RowEvent) MulticastGroupEvent {
	return MulticastGroupEvent{Type: event.Type, Old: sbMulticastGroupFromRow(event.Old), New: sbMulticastGroupFromRow(event.New)}
}

func serviceMonitorEventFromRowEvent(event RowEvent) ServiceMonitorEvent {
	return ServiceMonitorEvent{Type: event.Type, Old: sbServiceMonitorFromRow(event.Old), New: sbServiceMonitorFromRow(event.New)}
}

func rbacRoleEventFromRowEvent(event RowEvent) RBACRoleEvent {
	return RBACRoleEvent{Type: event.Type, Old: sbRBACRoleFromRow(event.Old), New: sbRBACRoleFromRow(event.New)}
}

func rbacPermissionEventFromRowEvent(event RowEvent) RBACPermissionEvent {
	return RBACPermissionEvent{Type: event.Type, Old: sbRBACPermissionFromRow(event.Old), New: sbRBACPermissionFromRow(event.New)}
}

func meterEventFromRowEvent(event RowEvent) MeterEvent {
	return MeterEvent{Type: event.Type, Old: sbMeterFromRow(event.Old), New: sbMeterFromRow(event.New)}
}

func meterBandEventFromRowEvent(event RowEvent) MeterBandEvent {
	return MeterBandEvent{Type: event.Type, Old: sbMeterBandFromRow(event.Old), New: sbMeterBandFromRow(event.New)}
}

func dnsEventFromRowEvent(event RowEvent) DNSEvent {
	return DNSEvent{Type: event.Type, Old: sbDNSFromRow(event.Old), New: sbDNSFromRow(event.New)}
}

func bfdEventFromRowEvent(event RowEvent) BFDEvent {
	return BFDEvent{Type: event.Type, Old: sbBFDFromRow(event.Old), New: sbBFDFromRow(event.New)}
}

func sbChassisFromRow(row Row) *SBChassis {
	if row == nil {
		return nil
	}
	return &SBChassis{
		UUID:        anyString(row[colUUID]),
		Name:        anyString(row[colName]),
		Hostname:    anyString(row["hostname"]),
		ExternalIDs: anyStringMap(row[colExternalIDs]),
		Encaps:      anyStringSlice(row["encaps"]),
		NbCfg:       anyInt(row["nb_cfg"]),
		OtherConfig: anyStringMap(row[colOtherConfig]),
	}
}

func sbPortBindingFromRow(row Row) *SBPortBinding {
	if row == nil {
		return nil
	}
	return &SBPortBinding{
		UUID:           anyString(row[colUUID]),
		LogicalPort:    anyString(row[colLogicalPort]),
		Type:           anyString(row[colType]),
		Chassis:        optionalString(row[colChassis]),
		Datapath:       anyString(row[colDatapath]),
		TunnelKey:      anyInt(row[colTunnelKey]),
		ParentPort:     optionalString(row["parent_port"]),
		Tag:            optionalInt(row["tag"]),
		VirtualParent:  optionalString(row["virtual_parent"]),
		Encap:          optionalString(row["encap"]),
		GatewayChassis: anyStringSlice(row["gateway_chassis"]),
		HAChassisGroup: optionalString(row["ha_chassis_group"]),
		MAC:            anyStringSlice(row[colMAC]),
		NatAddresses:   anyStringSlice(row["nat_addresses"]),
		Up:             optionalBool(row["up"]),
		Options:        anyStringMap(row[colOptions]),
		ExternalIDs:    anyStringMap(row[colExternalIDs]),
	}
}

func sbDatapathFromRow(row Row) *SBDatapathBinding {
	if row == nil {
		return nil
	}
	return &SBDatapathBinding{
		UUID:          anyString(row[colUUID]),
		TunnelKey:     anyInt(row[colTunnelKey]),
		LoadBalancers: anyStringSlice(row["load_balancers"]),
		ExternalIDs:   anyStringMap(row[colExternalIDs]),
	}
}

func sbLogicalFlowFromRow(row Row) *SBLogicalFlow {
	if row == nil {
		return nil
	}
	return &SBLogicalFlow{
		UUID:            anyString(row[colUUID]),
		LogicalDatapath: optionalString(row["logical_datapath"]),
		LogicalDPGroup:  optionalString(row["logical_dp_group"]),
		Pipeline:        anyString(row["pipeline"]),
		TableID:         anyInt(row["table_id"]),
		Priority:        anyInt(row[colPriority]),
		Match:           anyString(row[colMatch]),
		Actions:         anyString(row["actions"]),
		ExternalIDs:     anyStringMap(row[colExternalIDs]),
	}
}

func sbMACBindingFromRow(row Row) *SBMACBinding {
	if row == nil {
		return nil
	}
	return &SBMACBinding{
		UUID:        anyString(row[colUUID]),
		LogicalPort: anyString(row[colLogicalPort]),
		IP:          anyString(row[colIP]),
		MAC:         anyString(row[colMAC]),
		Datapath:    anyString(row[colDatapath]),
	}
}

func sbFDBFromRow(row Row) *SBFDB {
	if row == nil {
		return nil
	}
	return &SBFDB{
		UUID:    anyString(row[colUUID]),
		MAC:     anyString(row[colMAC]),
		DPKey:   anyInt(row[colDPKey]),
		PortKey: anyInt(row[colPortKey]),
	}
}

func sbMulticastGroupFromRow(row Row) *SBMulticastGroup {
	if row == nil {
		return nil
	}
	return &SBMulticastGroup{
		UUID:      anyString(row[colUUID]),
		Datapath:  anyString(row[colDatapath]),
		Name:      anyString(row[colName]),
		TunnelKey: anyInt(row[colTunnelKey]),
		Ports:     anyStringSlice(row[colPorts]),
	}
}

func sbServiceMonitorFromRow(row Row) *SBServiceMonitor {
	if row == nil {
		return nil
	}
	return &SBServiceMonitor{
		UUID:        anyString(row[colUUID]),
		IP:          anyString(row[colIP]),
		Protocol:    optionalString(row[colProtocol]),
		Port:        anyInt(row[colPort]),
		LogicalPort: anyString(row[colLogicalPort]),
		SrcMAC:      anyString(row["src_mac"]),
		SrcIP:       anyString(row["src_ip"]),
		Status:      optionalString(row[colStatus]),
		Options:     anyStringMap(row[colOptions]),
		ExternalIDs: anyStringMap(row[colExternalIDs]),
	}
}

func sbRBACRoleFromRow(row Row) *SBRBACRole {
	if row == nil {
		return nil
	}
	return &SBRBACRole{
		UUID:        anyString(row[colUUID]),
		Name:        anyString(row[colName]),
		Permissions: anyStringMap(row["permissions"]),
	}
}

func sbRBACPermissionFromRow(row Row) *SBRBACPermission {
	if row == nil {
		return nil
	}
	return &SBRBACPermission{
		UUID:          anyString(row[colUUID]),
		Table:         anyString(row["table"]),
		Authorization: anyStringSlice(row["authorization"]),
		InsertDelete:  anyBool(row["insert_delete"]),
		Update:        anyStringSlice(row["update"]),
	}
}

func sbMeterFromRow(row Row) *SBMeter {
	if row == nil {
		return nil
	}
	return &SBMeter{
		UUID:  anyString(row[colUUID]),
		Name:  anyString(row[colName]),
		Unit:  anyString(row["unit"]),
		Bands: anyStringSlice(row["bands"]),
	}
}

func sbMeterBandFromRow(row Row) *SBMeterBand {
	if row == nil {
		return nil
	}
	return &SBMeterBand{
		UUID:      anyString(row[colUUID]),
		Action:    anyString(row[colAction]),
		Rate:      anyInt(row["rate"]),
		BurstSize: anyInt(row["burst_size"]),
	}
}

func sbDNSFromRow(row Row) *SBDNS {
	if row == nil {
		return nil
	}
	return &SBDNS{
		UUID:        anyString(row[colUUID]),
		Records:     anyStringMap(row["records"]),
		Datapaths:   anyStringSlice(row["datapaths"]),
		ExternalIDs: anyStringMap(row[colExternalIDs]),
	}
}

func sbBFDFromRow(row Row) *SBBFD {
	if row == nil {
		return nil
	}
	return &SBBFD{
		UUID:        anyString(row[colUUID]),
		SrcPort:     anyInt(row[colSrcPort]),
		Disc:        anyInt(row[colDisc]),
		LogicalPort: anyString(row[colLogicalPort]),
		DstIP:       anyString(row[colDstIP]),
		MinTx:       anyInt(row["min_tx"]),
		MinRx:       anyInt(row["min_rx"]),
		DetectMult:  anyInt(row["detect_mult"]),
		Status:      anyString(row[colStatus]),
		ExternalIDs: anyStringMap(row[colExternalIDs]),
		Options:     anyStringMap(row[colOptions]),
	}
}

func optionalString(value any) *string {
	if s := anyString(value); s != "" {
		return &s
	}
	values := anyStringSlice(value)
	if len(values) > 0 {
		return &values[0]
	}
	return nil
}

func optionalInt(value any) *int {
	if value == nil {
		return nil
	}
	i := anyInt(value)
	return &i
}

func anyBool(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	default:
		return false
	}
}

func optionalBool(value any) *bool {
	if value == nil {
		return nil
	}
	v := anyBool(value)
	return &v
}

func stringPtr(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

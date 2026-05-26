package ovnflow

import (
	"context"

	libovsdb "github.com/ovn-kubernetes/libovsdb/ovsdb"
)

// Table exposes the full runtime OVN Northbound schema through the fluent API.
func (n *NBClient) Table(table string) *TableRef {
	return n.db.Table(table)
}

// TableBy exposes a runtime OVN Northbound table row selected by column=value.
func (n *NBClient) TableBy(table, column, value string) *TableRef {
	return n.db.TableBy(table, column, value)
}

func (n *NBClient) TableLogicalSwitchPort(name string) *TableRef {
	return n.TableBy(tableLogicalSwitchPort, colName, name)
}

func (n *NBClient) NBGlobal() *TableRef {
	return n.Table(tableNBGlobal)
}

func (n *NBClient) Connection(target string) *TableRef {
	return n.TableBy(tableConnection, colTarget, target)
}

func (n *NBClient) SSL() *TableRef {
	return n.Table(tableSSL)
}

func (n *NBClient) ForwardingGroup(name string) *TableRef {
	return n.TableBy(tableForwardingGroup, colName, name)
}

func (n *NBClient) ListLogicalSwitches(ctx context.Context) ([]LogicalSwitch, error) {
	rows, err := n.Table(tableLogicalSwitch).List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]LogicalSwitch, 0, len(rows))
	for _, row := range rows {
		out = append(out, logicalSwitchFromRow(row))
	}
	return out, nil
}

func (n *NBClient) GetLogicalSwitchPort(ctx context.Context, name string) (*LogicalSwitchPort, error) {
	row, err := n.TableLogicalSwitchPort(name).Get(ctx)
	if err != nil {
		return nil, err
	}
	port := logicalSwitchPortFromRow(row)
	return &port, nil
}

func (n *NBClient) ListLogicalSwitchPorts(ctx context.Context) ([]LogicalSwitchPort, error) {
	rows, err := n.Table(tableLogicalSwitchPort).List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]LogicalSwitchPort, 0, len(rows))
	for _, row := range rows {
		out = append(out, logicalSwitchPortFromRow(row))
	}
	return out, nil
}

func (r *TableRef) AddUUID(column, uuid string) *TableBuilder {
	return r.Update().MutateUUIDSet(column, uuid)
}

func (r *TableRef) DeleteUUID(column, uuid string) *TableBuilder {
	return r.Update().DeleteUUIDSet(column, uuid)
}

func (b *TableBuilder) WithACL(direction string, priority int, match, action string) *TableBuilder {
	return b.WithDirection(direction).WithPriority(priority).WithMatch(match).WithAction(action)
}

func (b *TableBuilder) WithNAT(kind, logicalIP, externalIP string) *TableBuilder {
	return b.WithType(kind).WithColumn("logical_ip", logicalIP).WithColumn("external_ip", externalIP)
}

func (b *TableBuilder) WithVIP(vip, backends string) *TableBuilder {
	return b.MutateMap("vips", map[string]string{vip: backends})
}

func (b *TableBuilder) WithDHCPOption(key, value string) *TableBuilder {
	return b.MutateMap(colOptions, map[string]string{key: value})
}

func (b *TableBuilder) WithDNSRecord(name, value string) *TableBuilder {
	return b.MutateMap(colDNSRecords, map[string]string{name: value})
}

func (b *TableBuilder) WithBFDLogicalPort(port string) *TableBuilder {
	return b.WithLogicalPort(port)
}

func (b *TableBuilder) WithBFDStatus(status string) *TableBuilder {
	return b.WithOptionalColumn("status", status)
}

func (b *TableBuilder) WithRouterPort(mac string, networks ...string) *TableBuilder {
	return b.WithColumn("mac", mac).WithNetworks(networks...)
}

func (b *TableBuilder) WithGatewayPriority(priority int) *TableBuilder {
	return b.WithColumn(colPriority, priority)
}

func (b *TableBuilder) WithAddressSetAddresses(addresses ...string) *TableBuilder {
	return b.WithAddresses(addresses...)
}

func (b *TableBuilder) WithPortGroupPorts(portUUIDs ...string) *TableBuilder {
	return b.WithUUIDSet(colPorts, portUUIDs...)
}

func logicalSwitchFromRow(row Row) LogicalSwitch {
	return LogicalSwitch{
		UUID:        anyString(row[colUUID]),
		Name:        anyString(row[colName]),
		Ports:       anyStringSlice(row[colPorts]),
		ExternalIDs: anyStringMap(row[colExternalIDs]),
		OtherConfig: anyStringMap(row[colOtherConfig]),
	}
}

func logicalSwitchPortFromRow(row Row) LogicalSwitchPort {
	return LogicalSwitchPort{
		UUID:        anyString(row[colUUID]),
		Name:        anyString(row[colName]),
		Addresses:   anyStringSlice(row[colAddresses]),
		ExternalIDs: anyStringMap(row[colExternalIDs]),
		Options:     anyStringMap(row[colOptions]),
		Type:        anyString(row[colType]),
	}
}

func anyString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case libovsdb.UUID:
		return typed.GoUUID
	case map[string]any:
		if id, ok := typed["GoUUID"].(string); ok {
			return id
		}
	case []any:
		if len(typed) == 2 {
			if marker, ok := typed[0].(string); ok && (marker == "uuid" || marker == "named-uuid") {
				if id, ok := typed[1].(string); ok {
					return id
				}
			}
		}
	}
	return ""
}

func anyStringSlice(value any) []string {
	switch typed := value.(type) {
	case nil:
		return nil
	case string:
		return []string{typed}
	case []string:
		return typed
	case []any:
		if len(typed) == 2 {
			if marker, ok := typed[0].(string); ok && (marker == "uuid" || marker == "named-uuid") {
				if id, ok := typed[1].(string); ok && id != "" {
					return []string{id}
				}
			}
		}
		if len(typed) == 2 {
			if marker, ok := typed[0].(string); ok && marker == "set" {
				if values, ok := typed[1].([]any); ok {
					return anySliceStrings(values)
				}
			}
		}
		return anySliceStrings(typed)
	case libovsdb.OvsSet:
		return anySliceStrings(typed.GoSet)
	default:
		return nil
	}
}

func anySliceStrings(values []any) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		switch typed := value.(type) {
		case string:
			out = append(out, typed)
		case libovsdb.UUID:
			out = append(out, typed.GoUUID)
		case []any:
			if s := anyString(typed); s != "" {
				out = append(out, s)
			}
		case map[string]any:
			if s := anyString(typed); s != "" {
				out = append(out, s)
			}
		}
	}
	return uniqueStrings(out)
}

func anyStringMap(value any) map[string]string {
	switch typed := value.(type) {
	case nil:
		return nil
	case map[string]string:
		return typed
	case map[string]any:
		out := map[string]string{}
		for k, v := range typed {
			if s, ok := anyMapStringValue(v); ok {
				out[k] = s
			}
		}
		return out
	case []any:
		if len(typed) != 2 {
			return nil
		}
		marker, ok := typed[0].(string)
		if !ok || marker != "map" {
			return nil
		}
		pairs, ok := typed[1].([]any)
		if !ok {
			return nil
		}
		out := map[string]string{}
		for _, item := range pairs {
			pair, ok := item.([]any)
			if !ok || len(pair) != 2 {
				continue
			}
			key, ok := pair[0].(string)
			if !ok {
				continue
			}
			if value, ok := anyMapStringValue(pair[1]); ok {
				out[key] = value
			}
		}
		return out
	case libovsdb.OvsMap:
		out := map[string]string{}
		for k, v := range typed.GoMap {
			key, keyOK := k.(string)
			val, valOK := anyMapStringValue(v)
			if keyOK && valOK {
				out[key] = val
			}
		}
		return out
	default:
		return nil
	}
}

func anyMapStringValue(value any) (string, bool) {
	if s, ok := value.(string); ok {
		return s, true
	}
	return anyUUIDString(value)
}

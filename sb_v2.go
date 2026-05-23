package ovnflow

// Table exposes the full runtime OVN Southbound schema through the fluent API.
func (s *SBClient) Table(table string) *TableRef {
	return s.db.Table(table)
}

// TableBy exposes a runtime OVN Southbound table row selected by column=value.
func (s *SBClient) TableBy(table, column, value string) *TableRef {
	return s.db.TableBy(table, column, value)
}

func (s *SBClient) Chassis(name string) *TableRef {
	return s.TableBy(tableChassis, colName, name)
}

func (s *SBClient) Encap(ip string) *TableRef {
	return s.TableBy(tableEncap, "ip", ip)
}

func (s *SBClient) PortBinding(logicalPort string) *TableRef {
	return s.TableBy(tablePortBinding, colLogicalPort, logicalPort)
}

func (s *SBClient) Datapath(uuid string) *TableRef {
	return s.TableBy(tableDatapathBinding, colUUID, uuid)
}

func (s *SBClient) LogicalFlow(uuid string) *TableRef {
	return s.TableBy(tableLogicalFlow, colUUID, uuid)
}

func (s *SBClient) MACBinding(logicalPort string) *TableRef {
	return s.TableBy(tableMACBinding, colLogicalPort, logicalPort)
}

func (s *SBClient) FDB(mac string) *TableRef {
	return s.TableBy(tableFDB, "mac", mac)
}

func (s *SBClient) MulticastGroup(name string) *TableRef {
	return s.TableBy(tableMulticastGroup, colName, name)
}

func (s *SBClient) ServiceMonitor(logicalPort string) *TableRef {
	return s.TableBy(tableServiceMonitor, colLogicalPort, logicalPort)
}

func (s *SBClient) RBACRole(name string) *TableRef {
	return s.TableBy(tableRBACRole, colName, name)
}

func (s *SBClient) RBACPermission(name string) *TableRef {
	return s.TableBy(tableRBACPermission, colName, name)
}

func (s *SBClient) Meter(name string) *TableRef {
	return s.TableBy(tableMeter, colName, name)
}

func (s *SBClient) DNS(name string) *TableRef {
	return s.TableBy(tableDNS, colUUID, name)
}

func (s *SBClient) BFD(logicalPort string) *TableRef {
	return s.TableBy(tableBFD, colLogicalPort, logicalPort)
}

func (s *SBClient) SBGlobal() *TableRef {
	return s.Table(tableSBGlobal)
}

func (s *SBClient) Connection(target string) *TableRef {
	return s.TableBy(tableConnection, colTarget, target)
}

func (s *SBClient) SSL() *TableRef {
	return s.Table(tableSSL)
}

func (b *TableBuilder) WithDatapath(uuid string) *TableBuilder {
	return b.WithUUIDRef(colDatapath, uuid)
}

func (b *TableBuilder) WithChassis(uuid string) *TableBuilder {
	return b.WithUUIDRef(colChassis, uuid)
}

func (b *TableBuilder) WithMAC(mac string) *TableBuilder {
	return b.WithColumn("mac", mac)
}

func (b *TableBuilder) WithEncap(kind, ip string) *TableBuilder {
	return b.WithType(kind).WithColumn("ip", ip)
}

func anyInt(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case jsonNumber:
		i, _ := typed.Int64()
		return int(i)
	default:
		return 0
	}
}

type jsonNumber interface {
	Int64() (int64, error)
	String() string
}

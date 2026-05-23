package ovnflow

import (
	"context"
	"net"

	libovsdb "github.com/ovn-kubernetes/libovsdb/ovsdb"
)

// NBClient provides OVN Northbound fluent APIs.
type NBClient struct {
	db *dbClient
}

func (n *NBClient) LogicalSwitch(name string) *LogicalSwitchRef {
	return &LogicalSwitchRef{client: n, name: name}
}

// GetLogicalSwitch returns a logical switch by name.
func (n *NBClient) GetLogicalSwitch(ctx context.Context, name string) (*LogicalSwitch, error) {
	if err := validateName("logical switch", name); err != nil {
		return nil, err
	}
	rows, err := n.selectLogicalSwitches(ctx, name)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, wrap(ErrorNotFound, dbOVNNorthbound, tableLogicalSwitch, "get", name, "", nil)
	}
	return &rows[0], nil
}

type LogicalSwitchRef struct {
	client *NBClient
	name   string
}

func (r *LogicalSwitchRef) Create() *LogicalSwitchBuilder {
	return newLogicalSwitchBuilder(r.client, r.name, nbModeCreate)
}

func (r *LogicalSwitchRef) Ensure() *LogicalSwitchBuilder {
	return newLogicalSwitchBuilder(r.client, r.name, nbModeEnsure)
}

func (r *LogicalSwitchRef) Delete() *LogicalSwitchBuilder {
	return newLogicalSwitchBuilder(r.client, r.name, nbModeDelete)
}

type nbMode string

const (
	nbModeCreate nbMode = "create"
	nbModeEnsure nbMode = "ensure"
	nbModeDelete nbMode = "delete"
)

type LogicalSwitchBuilder struct {
	once        useOnce
	client      *NBClient
	name        string
	mode        nbMode
	subnet      string
	ports       []*logicalPortSpec
	externalIDs map[string]string
}

type logicalPortSpec struct {
	name        string
	mac         string
	ip          string
	addresses   []string
	externalIDs map[string]string
}

func newLogicalSwitchBuilder(client *NBClient, name string, mode nbMode) *LogicalSwitchBuilder {
	return &LogicalSwitchBuilder{
		client:      client,
		name:        name,
		mode:        mode,
		externalIDs: map[string]string{},
	}
}

func (b *LogicalSwitchBuilder) WithSubnet(cidr string) *LogicalSwitchBuilder {
	b.subnet = cidr
	return b
}

func (b *LogicalSwitchBuilder) WithExternalID(key, value string) *LogicalSwitchBuilder {
	if b.externalIDs == nil {
		b.externalIDs = map[string]string{}
	}
	b.externalIDs[key] = value
	return b
}

func (b *LogicalSwitchBuilder) AddPort(name string) *LogicalSwitchPortBuilder {
	spec := &logicalPortSpec{name: name, externalIDs: map[string]string{}}
	b.ports = append(b.ports, spec)
	return &LogicalSwitchPortBuilder{parent: b, spec: spec}
}

// Execute commits the logical switch operation.
func (b *LogicalSwitchBuilder) Execute(ctx context.Context) error {
	if !b.once.mark() {
		return wrap(ErrorValidation, dbOVNNorthbound, tableLogicalSwitch, string(b.mode), b.name, "builder already executed", nil)
	}
	if err := b.validate(); err != nil {
		return err
	}
	switch b.mode {
	case nbModeCreate:
		return b.executeCreate(ctx, false)
	case nbModeEnsure:
		return b.executeCreate(ctx, true)
	case nbModeDelete:
		return b.executeDelete(ctx)
	default:
		return wrap(ErrorValidation, dbOVNNorthbound, tableLogicalSwitch, string(b.mode), b.name, "unsupported operation", nil)
	}
}

func (b *LogicalSwitchBuilder) validate() error {
	if err := validateName("logical switch", b.name); err != nil {
		return err
	}
	if b.subnet != "" {
		if _, _, err := net.ParseCIDR(b.subnet); err != nil {
			return wrap(ErrorValidation, dbOVNNorthbound, tableLogicalSwitch, string(b.mode), b.name, "invalid subnet", err)
		}
	}
	for key := range b.externalIDs {
		if err := validateExternalID(key); err != nil {
			return err
		}
	}
	for _, port := range b.ports {
		if err := validateName("logical switch port", port.name); err != nil {
			return err
		}
		if port.mac != "" {
			if _, err := net.ParseMAC(port.mac); err != nil {
				return wrap(ErrorValidation, dbOVNNorthbound, tableLogicalSwitchPort, string(b.mode), port.name, "invalid mac", err)
			}
		}
		if port.ip != "" && net.ParseIP(port.ip) == nil {
			return wrap(ErrorValidation, dbOVNNorthbound, tableLogicalSwitchPort, string(b.mode), port.name, "invalid ip", nil)
		}
		for key := range port.externalIDs {
			if err := validateExternalID(key); err != nil {
				return err
			}
		}
	}
	return nil
}

func (b *LogicalSwitchBuilder) executeCreate(ctx context.Context, ensure bool) error {
	attempts := 1
	if ensure {
		attempts = 2
	}
	var err error
	for attempt := 0; attempt < attempts; attempt++ {
		err = b.executeCreateOnce(ctx, ensure)
		if err == nil {
			return nil
		}
		if !ensure || !IsKind(err, ErrorAlreadyExists) {
			return err
		}
	}
	return err
}

func (b *LogicalSwitchBuilder) executeCreateOnce(ctx context.Context, ensure bool) error {
	existing, err := b.client.selectLogicalSwitches(ctx, b.name)
	if err != nil {
		return err
	}
	if len(existing) > 0 && !ensure {
		return wrap(ErrorAlreadyExists, dbOVNNorthbound, tableLogicalSwitch, "create", b.name, "logical switch already exists", nil)
	}

	externalIDs := cloneStringMap(b.externalIDs)
	if externalIDs == nil {
		externalIDs = map[string]string{}
	}
	options := map[string]string{}
	if b.subnet != "" {
		options["subnet"] = b.subnet
	}

	var ops []libovsdb.Operation
	portUUIDs := make([]string, 0, len(b.ports))
	for _, port := range b.ports {
		if ensure {
			existingPorts, err := b.client.selectLogicalSwitchPorts(ctx, port.name)
			if err != nil {
				return err
			}
			if len(existingPorts) > 0 {
				portUUIDs = append(portUUIDs, existingPorts[0].UUID)
				updateRow := libovsdb.Row{}
				addresses := port.addresses
				if len(addresses) == 0 && port.mac != "" {
					addr := port.mac
					if port.ip != "" {
						addr += " " + port.ip
					}
					addresses = []string{addr}
				}
				if len(addresses) > 0 {
					updateRow[colAddresses] = stringSet(addresses)
				}
				if len(updateRow) > 0 {
					ops = append(ops, libovsdb.Operation{
						Op:    libovsdb.OperationUpdate,
						Table: tableLogicalSwitchPort,
						Where: conditionUUID(existingPorts[0].UUID),
						Row:   updateRow,
					})
				}
				if len(port.externalIDs) > 0 {
					ops = append(ops, libovsdb.Operation{
						Op:    libovsdb.OperationMutate,
						Table: tableLogicalSwitchPort,
						Where: conditionUUID(existingPorts[0].UUID),
						Mutations: []libovsdb.Mutation{
							*libovsdb.NewMutation(colExternalIDs, libovsdb.MutateOperationInsert, ovsMap(port.externalIDs)),
						},
					})
				}
				continue
			}
		}
		portUUID := namedUUID("lsp")
		portUUIDs = append(portUUIDs, portUUID)
		addresses := port.addresses
		if len(addresses) == 0 && port.mac != "" {
			addr := port.mac
			if port.ip != "" {
				addr += " " + port.ip
			}
			addresses = []string{addr}
		}
		row := libovsdb.Row{
			colName: port.name,
		}
		setRowMap(row, colExternalIDs, port.externalIDs)
		if len(addresses) > 0 {
			row[colAddresses] = stringSet(addresses)
		}
		ops = append(ops, libovsdb.Operation{
			Op:       libovsdb.OperationInsert,
			Table:    tableLogicalSwitchPort,
			UUIDName: portUUID,
			Row:      row,
		})
	}

	if len(existing) == 0 {
		portRefs := make([]any, 0, len(portUUIDs))
		for _, id := range portUUIDs {
			portRefs = append(portRefs, uuidValue(id))
		}
		row := libovsdb.Row{
			colName: b.name,
		}
		setRowMap(row, colExternalIDs, externalIDs)
		if len(options) > 0 {
			row[colOtherConfig] = ovsMap(options)
		}
		if len(portRefs) > 0 {
			row[colPorts] = ovsSet(portRefs...)
		}
		ops = append(ops, libovsdb.Operation{
			Op:    libovsdb.OperationInsert,
			Table: tableLogicalSwitch,
			Row:   row,
		})
	} else {
		var mutations []libovsdb.Mutation
		if len(externalIDs) > 0 {
			mutations = append(mutations, *libovsdb.NewMutation(colExternalIDs, libovsdb.MutateOperationInsert, ovsMap(externalIDs)))
		}
		if len(options) > 0 {
			mutations = append(mutations, *libovsdb.NewMutation(colOtherConfig, libovsdb.MutateOperationInsert, ovsMap(options)))
		}
		if len(mutations) > 0 {
			ops = append(ops, libovsdb.Operation{
				Op:        libovsdb.OperationMutate,
				Table:     tableLogicalSwitch,
				Where:     conditionUUID(existing[0].UUID),
				Mutations: mutations,
			})
		}
		if len(portUUIDs) > 0 {
			values := make([]any, 0, len(portUUIDs))
			for _, id := range portUUIDs {
				values = append(values, uuidValue(id))
			}
			ops = append(ops, libovsdb.Operation{
				Op:    libovsdb.OperationMutate,
				Table: tableLogicalSwitch,
				Where: conditionUUID(existing[0].UUID),
				Mutations: []libovsdb.Mutation{
					*libovsdb.NewMutation(colPorts, libovsdb.MutateOperationInsert, ovsSet(values...)),
				},
			})
		}
	}

	if len(ops) == 0 {
		return nil
	}
	results, err := b.client.db.raw.Transact(ctx, ops...)
	if err != nil {
		return classifyTransactError(err, dbOVNNorthbound, tableLogicalSwitch, string(b.mode), b.name)
	}
	return checkOperationResults(results, dbOVNNorthbound, tableLogicalSwitch, string(b.mode), b.name)
}

func (b *LogicalSwitchBuilder) executeDelete(ctx context.Context) error {
	lsRows, err := b.client.selectLogicalSwitches(ctx, b.name)
	if err != nil {
		return err
	}
	if len(lsRows) == 0 {
		return wrap(ErrorNotFound, dbOVNNorthbound, tableLogicalSwitch, "delete", b.name, "logical switch not found", nil)
	}
	portUUIDs := append([]string{}, lsRows[0].Ports...)
	for _, port := range b.ports {
		rows, err := b.client.selectLogicalSwitchPorts(ctx, port.name)
		if err != nil {
			return err
		}
		if len(rows) > 0 {
			portUUIDs = append(portUUIDs, rows[0].UUID)
		}
	}
	portUUIDs = uniqueStrings(portUUIDs)

	var ops []libovsdb.Operation
	var mustAffect []int
	ops = append(ops, libovsdb.Operation{
		Op:    libovsdb.OperationDelete,
		Table: tableLogicalSwitch,
		Where: conditionUUID(lsRows[0].UUID),
	})
	mustAffect = append(mustAffect, len(ops)-1)
	for _, portUUID := range portUUIDs {
		ops = append(ops, libovsdb.Operation{
			Op:    libovsdb.OperationDelete,
			Table: tableLogicalSwitchPort,
			Where: conditionUUID(portUUID),
		})
		mustAffect = append(mustAffect, len(ops)-1)
	}
	results, err := b.client.db.raw.Transact(ctx, ops...)
	if err != nil {
		return classifyTransactError(err, dbOVNNorthbound, tableLogicalSwitch, "delete", b.name)
	}
	return ensureAffected(results, mustAffect, dbOVNNorthbound, tableLogicalSwitch, "delete", b.name)
}

func (n *NBClient) selectLogicalSwitches(ctx context.Context, name string) ([]LogicalSwitch, error) {
	results, err := n.db.raw.Transact(ctx, libovsdb.Operation{
		Op:      libovsdb.OperationSelect,
		Table:   tableLogicalSwitch,
		Where:   conditionName(name),
		Columns: []string{colUUID, colName, colPorts, colExternalIDs, colOtherConfig},
	})
	if err != nil {
		return nil, classifyTransactError(err, dbOVNNorthbound, tableLogicalSwitch, "select", name)
	}
	if err := checkOperationResults(results, dbOVNNorthbound, tableLogicalSwitch, "select", name); err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, nil
	}
	rows := make([]LogicalSwitch, 0, len(results[0].Rows))
	for _, row := range results[0].Rows {
		rows = append(rows, LogicalSwitch{
			UUID:        rowUUIDValue(row),
			Name:        rowStringValue(row, colName),
			Ports:       rowUUIDSliceValue(row, colPorts),
			ExternalIDs: rowStringMapValue(row, colExternalIDs),
			OtherConfig: rowStringMapValue(row, colOtherConfig),
		})
	}
	return rows, nil
}

func (n *NBClient) selectLogicalSwitchPorts(ctx context.Context, name string) ([]LogicalSwitchPort, error) {
	results, err := n.db.raw.Transact(ctx, libovsdb.Operation{
		Op:      libovsdb.OperationSelect,
		Table:   tableLogicalSwitchPort,
		Where:   conditionName(name),
		Columns: []string{colUUID, colName, colAddresses, colExternalIDs, colOptions, colType},
	})
	if err != nil {
		return nil, classifyTransactError(err, dbOVNNorthbound, tableLogicalSwitchPort, "select", name)
	}
	if err := checkOperationResults(results, dbOVNNorthbound, tableLogicalSwitchPort, "select", name); err != nil {
		return nil, err
	}
	rows := make([]LogicalSwitchPort, 0, len(results[0].Rows))
	for _, row := range results[0].Rows {
		item := LogicalSwitchPort{
			UUID:        rowUUIDValue(row),
			Name:        rowStringValue(row, colName),
			Addresses:   rowStringSliceValue(row, colAddresses),
			ExternalIDs: rowStringMapValue(row, colExternalIDs),
			Options:     rowStringMapValue(row, colOptions),
			Type:        rowStringValue(row, colType),
		}
		rows = append(rows, item)
	}
	for i := range rows {
		if i < len(results[0].Rows) {
			rows[i].UUID = rowUUIDValue(results[0].Rows[i])
		}
	}
	return rows, nil
}

type LogicalSwitchPortBuilder struct {
	parent *LogicalSwitchBuilder
	spec   *logicalPortSpec
}

func (p *LogicalSwitchPortBuilder) WithMac(mac string) *LogicalSwitchPortBuilder {
	p.spec.mac = mac
	return p
}

func (p *LogicalSwitchPortBuilder) WithIP(ip string) *LogicalSwitchPortBuilder {
	p.spec.ip = ip
	return p
}

func (p *LogicalSwitchPortBuilder) WithAddress(mac, ip string) *LogicalSwitchPortBuilder {
	addr := mac
	if ip != "" {
		addr += " " + ip
	}
	p.spec.addresses = append(p.spec.addresses, addr)
	return p
}

func (p *LogicalSwitchPortBuilder) WithExternalID(key, value string) *LogicalSwitchPortBuilder {
	if p.spec.externalIDs == nil {
		p.spec.externalIDs = map[string]string{}
	}
	p.spec.externalIDs[key] = value
	return p
}

func (p *LogicalSwitchPortBuilder) Execute(ctx context.Context) error {
	return p.parent.Execute(ctx)
}

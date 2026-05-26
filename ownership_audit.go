package ovnflow

import (
	"context"
	"sort"
	"strings"
)

type OwnershipAuditOptions struct {
	Owner OwnerRef
	Kinds []string
	Names []string
}

type OwnershipAuditReport struct {
	Summary   OwnershipAuditSummary    `json:"summary"`
	Resources []OwnershipAuditResource `json:"resources"`
	Findings  []OwnershipAuditFinding  `json:"findings"`
}

type OwnershipAuditSummary struct {
	OwnedResources int `json:"owned_resources"`
	Findings       int `json:"findings"`
	Errors         int `json:"errors"`
	Warnings       int `json:"warnings"`
}

type OwnershipAuditResource struct {
	Database    string            `json:"database"`
	Table       string            `json:"table"`
	UUID        string            `json:"uuid,omitempty"`
	Name        string            `json:"name,omitempty"`
	Kind        string            `json:"kind,omitempty"`
	Owner       OwnerRef          `json:"owner"`
	Labels      Labels            `json:"labels,omitempty"`
	ExternalIDs map[string]string `json:"external_ids,omitempty"`
}

type OwnershipAuditFinding struct {
	Severity DoctorSeverity    `json:"severity"`
	Code     string            `json:"code"`
	Database string            `json:"database,omitempty"`
	Table    string            `json:"table,omitempty"`
	Object   string            `json:"object,omitempty"`
	Message  string            `json:"message"`
	Details  map[string]string `json:"details,omitempty"`
}

type auditState struct {
	nbLogicalSwitches map[string]LogicalSwitch
	nbPorts           map[string]LogicalSwitchPort
	nbPortsByName     map[string]LogicalSwitchPort
	nbDNS             map[string]DNS
	nbPortGroups      map[string]PortGroup
	nbACLs            map[string]ACL
	ovsRoots          []OpenVSwitch
	ovsBridges        map[string]OVSBridge
	ovsBridgesByName  map[string]OVSBridge
	ovsPorts          map[string]OVSPort
	ovsPortsByName    map[string]OVSPort
	ovsInterfaces     map[string]OVSInterface
	ovsIfacesByName   map[string]OVSInterface
}

func (d *Diagnostics) AuditOwnership(ctx context.Context, opts OwnershipAuditOptions) (*OwnershipAuditReport, error) {
	report := &OwnershipAuditReport{}
	if ownerFilterSet(opts.Owner) {
		if err := opts.Owner.Validate(); err != nil {
			return nil, err
		}
	}
	if d == nil || d.client == nil {
		report.addFinding(DoctorError, "client_unavailable", "", "", "", "ovnflow client is nil", nil)
		return report.finalize(), nil
	}
	state := newAuditState()
	if err := report.collectNorthbound(ctx, d.client.nb, opts, state); err != nil {
		return nil, err
	}
	if err := report.collectOVS(ctx, d.client.ovs, opts, state); err != nil {
		return nil, err
	}
	report.checkNorthboundReferences(state, opts)
	report.checkProviderNetworks(state, opts)
	report.checkWorkloadAttachments(state, opts)
	report.checkSecurityPolicies(state, opts)
	report.checkOVSReferences(state, opts)
	return report.finalize(), nil
}

func newAuditState() *auditState {
	return &auditState{
		nbLogicalSwitches: map[string]LogicalSwitch{},
		nbPorts:           map[string]LogicalSwitchPort{},
		nbPortsByName:     map[string]LogicalSwitchPort{},
		nbDNS:             map[string]DNS{},
		nbPortGroups:      map[string]PortGroup{},
		nbACLs:            map[string]ACL{},
		ovsBridges:        map[string]OVSBridge{},
		ovsBridgesByName:  map[string]OVSBridge{},
		ovsPorts:          map[string]OVSPort{},
		ovsPortsByName:    map[string]OVSPort{},
		ovsInterfaces:     map[string]OVSInterface{},
		ovsIfacesByName:   map[string]OVSInterface{},
	}
}

func (r *OwnershipAuditReport) collectNorthbound(ctx context.Context, db *dbClient, opts OwnershipAuditOptions, state *auditState) error {
	if db == nil {
		r.addFinding(DoctorWarning, "database_unavailable", dbOVNNorthbound, "", "", "OVN Northbound client is nil", nil)
		return nil
	}
	switchRows, err := auditListRows(ctx, db, tableLogicalSwitch)
	if err != nil {
		return err
	}
	for _, row := range switchRows {
		ls := logicalSwitchFromRow(row)
		state.nbLogicalSwitches[ls.UUID] = ls
		r.addOwnedResource(dbOVNNorthbound, tableLogicalSwitch, ls.UUID, ls.Name, ls.ExternalIDs, opts)
	}
	portRows, err := auditListRows(ctx, db, tableLogicalSwitchPort)
	if err != nil {
		return err
	}
	for _, row := range portRows {
		lsp := logicalSwitchPortFromRow(row)
		state.nbPorts[lsp.UUID] = lsp
		state.nbPortsByName[lsp.Name] = lsp
		r.addOwnedResource(dbOVNNorthbound, tableLogicalSwitchPort, lsp.UUID, lsp.Name, lsp.ExternalIDs, opts)
	}
	dnsRows, err := auditListRows(ctx, db, tableDNS)
	if err != nil {
		return err
	}
	for _, row := range dnsRows {
		dns := dnsFromAuditRow(row)
		state.nbDNS[dns.UUID] = dns
		r.addOwnedResource(dbOVNNorthbound, tableDNS, dns.UUID, dns.ExternalIDs[dnsNameExternalID], dns.ExternalIDs, opts)
	}
	portGroupRows, err := auditListRows(ctx, db, tablePortGroup)
	if err != nil {
		return err
	}
	for _, row := range portGroupRows {
		pg := portGroupFromAuditRow(row)
		state.nbPortGroups[pg.UUID] = pg
		r.addOwnedResource(dbOVNNorthbound, tablePortGroup, pg.UUID, pg.Name, pg.ExternalIDs, opts)
	}
	aclRows, err := auditListRows(ctx, db, tableACL)
	if err != nil {
		return err
	}
	for _, row := range aclRows {
		acl := aclFromAuditRow(row)
		state.nbACLs[acl.UUID] = acl
		r.addOwnedResource(dbOVNNorthbound, tableACL, acl.UUID, auditObjectName(acl.ExternalIDs), acl.ExternalIDs, opts)
	}
	return nil
}

func (r *OwnershipAuditReport) collectOVS(ctx context.Context, db *dbClient, opts OwnershipAuditOptions, state *auditState) error {
	if db == nil {
		r.addFinding(DoctorWarning, "database_unavailable", dbOpenVSwitch, "", "", "Open_vSwitch client is nil", nil)
		return nil
	}
	rootRows, err := auditListRows(ctx, db, tableOpenVSwitch)
	if err != nil {
		return err
	}
	for _, row := range rootRows {
		root := openVSwitchFromAuditRow(row)
		state.ovsRoots = append(state.ovsRoots, root)
		r.addOwnedResource(dbOpenVSwitch, tableOpenVSwitch, root.UUID, "", root.ExternalIDs, opts)
	}
	bridgeRows, err := auditListRows(ctx, db, tableBridge)
	if err != nil {
		return err
	}
	for _, row := range bridgeRows {
		bridge := ovsBridgeFromAuditRow(row)
		state.ovsBridges[bridge.UUID] = bridge
		state.ovsBridgesByName[bridge.Name] = bridge
		r.addOwnedResource(dbOpenVSwitch, tableBridge, bridge.UUID, bridge.Name, bridge.ExternalIDs, opts)
	}
	portRows, err := auditListRows(ctx, db, tablePort)
	if err != nil {
		return err
	}
	for _, row := range portRows {
		port := ovsPortFromAuditRow(row)
		state.ovsPorts[port.UUID] = port
		state.ovsPortsByName[port.Name] = port
		r.addOwnedResource(dbOpenVSwitch, tablePort, port.UUID, port.Name, port.ExternalIDs, opts)
	}
	interfaceRows, err := auditListRows(ctx, db, tableInterface)
	if err != nil {
		return err
	}
	for _, row := range interfaceRows {
		iface := ovsInterfaceFromAuditRow(row)
		state.ovsInterfaces[iface.UUID] = iface
		state.ovsIfacesByName[iface.Name] = iface
		r.addOwnedResource(dbOpenVSwitch, tableInterface, iface.UUID, iface.Name, iface.ExternalIDs, opts)
	}
	return nil
}

func auditListRows(ctx context.Context, db *dbClient, table string) ([]Row, error) {
	if db == nil || db.schema == nil || !db.schema.HasTable(table) {
		return nil, nil
	}
	return newTableRef(db, table, "", "").List(ctx)
}

func (r *OwnershipAuditReport) checkNorthboundReferences(state *auditState, opts OwnershipAuditOptions) {
	for _, ls := range state.nbLogicalSwitches {
		if !auditExternalIDsInScope(ls.ExternalIDs, ls.Name, opts) {
			continue
		}
		for _, portUUID := range ls.Ports {
			if _, ok := state.nbPorts[portUUID]; !ok {
				r.addFinding(DoctorError, "missing_logical_switch_port", dbOVNNorthbound, tableLogicalSwitch, ls.Name, "logical switch references a missing logical switch port", map[string]string{"port_uuid": portUUID})
			}
		}
	}
}

func (r *OwnershipAuditReport) checkProviderNetworks(state *auditState, opts OwnershipAuditOptions) {
	mappings := map[string]string{}
	markers := map[string]string{}
	for _, root := range state.ovsRoots {
		parsed, err := ParseBridgeMappings(root.ExternalIDs[ovsBridgeMappingsKey])
		if err != nil {
			r.addFinding(DoctorWarning, "invalid_bridge_mappings", dbOpenVSwitch, tableOpenVSwitch, root.UUID, "could not parse OVN bridge mappings", map[string]string{"error": err.Error()})
			continue
		}
		for key, value := range parsed {
			mappings[key] = value
		}
		for key, value := range root.ExternalIDs {
			if strings.HasPrefix(key, ExternalIDPrefix+"provider-network-mapping/") {
				markers[key] = value
			}
		}
	}
	localnetsByProvider := map[string]LogicalSwitchPort{}
	for _, port := range state.nbPorts {
		if port.Type != "localnet" || !providerNetworkLocalnetOwnedBy(port.ExternalIDs, port.ExternalIDs[ExternalIDNameKey]) {
			continue
		}
		if !auditExternalIDsInScope(port.ExternalIDs, port.Name, opts) {
			continue
		}
		name := port.ExternalIDs[ExternalIDNameKey]
		localnetsByProvider[name] = port
		physicalNetwork := port.Options["network_name"]
		if physicalNetwork == "" {
			physicalNetwork = port.ExternalIDs[ExternalIDPrefix+"physical-network"]
		}
		if physicalNetwork == "" {
			r.addFinding(DoctorError, "provider_network_missing_physical_network", dbOVNNorthbound, tableLogicalSwitchPort, port.Name, "provider localnet port is missing physical network metadata", nil)
			continue
		}
		mappedBridge := mappings[physicalNetwork]
		if mappedBridge == "" {
			r.addFinding(DoctorError, "provider_network_mapping_missing", dbOpenVSwitch, tableOpenVSwitch, physicalNetwork, "provider network has a localnet port but no OVS bridge mapping", map[string]string{"provider": name})
		} else if expectedBridge := port.ExternalIDs[ExternalIDPrefix+"bridge"]; expectedBridge != "" && mappedBridge != expectedBridge {
			r.addFinding(DoctorError, "provider_network_mapping_mismatch", dbOpenVSwitch, tableOpenVSwitch, physicalNetwork, "provider network bridge mapping points at a different bridge", map[string]string{"provider": name, "expected_bridge": expectedBridge, "actual_bridge": mappedBridge})
		}
		if mappedBridge != "" {
			if _, ok := state.ovsBridgesByName[mappedBridge]; !ok {
				r.addFinding(DoctorError, "provider_network_bridge_missing", dbOpenVSwitch, tableBridge, mappedBridge, "provider network bridge mapping points at a missing OVS bridge", map[string]string{"provider": name, "physical_network": physicalNetwork})
			}
		}
		markerKey := providerNetworkMappingOwnerKey(physicalNetwork)
		if markers[markerKey] == "" {
			r.addFinding(DoctorWarning, "provider_network_mapping_marker_missing", dbOpenVSwitch, tableOpenVSwitch, physicalNetwork, "provider bridge mapping has no ovnflow owner marker", map[string]string{"provider": name})
		} else if markers[markerKey] != name {
			r.addFinding(DoctorError, "provider_network_mapping_marker_mismatch", dbOpenVSwitch, tableOpenVSwitch, physicalNetwork, "provider bridge mapping marker belongs to another provider network", map[string]string{"provider": name, "marker_owner": markers[markerKey]})
		}
	}
	for key, provider := range markers {
		if provider == "" {
			continue
		}
		if !auditProviderMarkerInScope(provider, opts) {
			continue
		}
		if _, ok := localnetsByProvider[provider]; !ok {
			r.addFinding(DoctorWarning, "provider_network_marker_without_localnet", dbOpenVSwitch, tableOpenVSwitch, key, "provider bridge mapping marker has no matching owned localnet port", map[string]string{"provider": provider})
		}
	}
}

func (r *OwnershipAuditReport) checkWorkloadAttachments(state *auditState, opts OwnershipAuditOptions) {
	ownedNB := map[string]LogicalSwitchPort{}
	for _, port := range state.nbPorts {
		if ovsResourceOwnedBy(port.ExternalIDs, "WorkloadAttachment", port.ExternalIDs[ExternalIDNameKey]) {
			if !auditExternalIDsInScope(port.ExternalIDs, port.Name, opts) {
				continue
			}
			ownedNB[port.ExternalIDs[ExternalIDNameKey]] = port
		}
	}
	for name, lsp := range ownedNB {
		hasLocalPort := false
		for _, port := range state.ovsPorts {
			if !ovsResourceOwnedBy(port.ExternalIDs, "WorkloadAttachment", name) {
				continue
			}
			hasLocalPort = true
			for _, ifaceUUID := range port.Interfaces {
				iface, ok := state.ovsInterfaces[ifaceUUID]
				if !ok {
					r.addFinding(DoctorError, "workload_interface_missing", dbOpenVSwitch, tablePort, port.Name, "workload OVS port references a missing interface", map[string]string{"attachment": name, "interface_uuid": ifaceUUID})
					continue
				}
				if !ovsResourceOwnedBy(iface.ExternalIDs, "WorkloadAttachment", name) {
					r.addFinding(DoctorWarning, "workload_interface_unowned", dbOpenVSwitch, tableInterface, iface.Name, "workload OVS port references an interface without matching ovnflow ownership", map[string]string{"attachment": name})
				}
				if iface.ExternalIDs["iface-id"] != "" && iface.ExternalIDs["iface-id"] != name {
					r.addFinding(DoctorError, "workload_iface_id_mismatch", dbOpenVSwitch, tableInterface, iface.Name, "workload OVS interface iface-id does not match logical switch port", map[string]string{"attachment": name, "iface_id": iface.ExternalIDs["iface-id"]})
				}
			}
		}
		if lsp.ExternalIDs[ExternalIDPrefix+"network"] != "" {
			foundNetwork := false
			for _, ls := range state.nbLogicalSwitches {
				if ls.Name == lsp.ExternalIDs[ExternalIDPrefix+"network"] {
					foundNetwork = true
					break
				}
			}
			if !foundNetwork {
				r.addFinding(DoctorError, "workload_network_missing", dbOVNNorthbound, tableLogicalSwitchPort, lsp.Name, "workload attachment references a missing virtual network", map[string]string{"network": lsp.ExternalIDs[ExternalIDPrefix+"network"]})
			}
		}
		if len(state.ovsPorts) > 0 && !hasLocalPort {
			r.addFinding(DoctorWarning, "workload_local_ovs_missing", dbOpenVSwitch, tablePort, name, "workload attachment has no matching local OVS port", nil)
		}
	}
	for _, port := range state.ovsPorts {
		if !ovsResourceOwnedBy(port.ExternalIDs, "WorkloadAttachment", port.ExternalIDs[ExternalIDNameKey]) {
			continue
		}
		if !auditExternalIDsInScope(port.ExternalIDs, port.Name, opts) {
			continue
		}
		name := port.ExternalIDs[ExternalIDNameKey]
		if _, ok := ownedNB[name]; !ok {
			r.addFinding(DoctorWarning, "workload_local_port_without_lsp", dbOpenVSwitch, tablePort, port.Name, "owned local OVS port has no matching logical switch port", map[string]string{"attachment": name})
		}
	}
	for _, iface := range state.ovsInterfaces {
		if !ovsResourceOwnedBy(iface.ExternalIDs, "WorkloadAttachment", iface.ExternalIDs[ExternalIDNameKey]) {
			continue
		}
		if !auditExternalIDsInScope(iface.ExternalIDs, iface.Name, opts) {
			continue
		}
		name := iface.ExternalIDs[ExternalIDNameKey]
		foundPort := false
		for _, port := range state.ovsPorts {
			if ovsResourceOwnedBy(port.ExternalIDs, "WorkloadAttachment", name) && containsString(port.Interfaces, iface.UUID) {
				foundPort = true
				break
			}
		}
		if !foundPort {
			r.addFinding(DoctorWarning, "workload_interface_without_port", dbOpenVSwitch, tableInterface, iface.Name, "owned local OVS interface is not referenced by a matching owned port", map[string]string{"attachment": name})
		}
	}
}

func (r *OwnershipAuditReport) checkSecurityPolicies(state *auditState, opts OwnershipAuditOptions) {
	ownedACLRefs := map[string]bool{}
	for _, pg := range state.nbPortGroups {
		if !ovsResourceOwnedBy(pg.ExternalIDs, "SecurityPolicy", pg.Name) {
			continue
		}
		if !auditExternalIDsInScope(pg.ExternalIDs, pg.Name, opts) {
			continue
		}
		for _, aclUUID := range pg.ACLs {
			acl, ok := state.nbACLs[aclUUID]
			if !ok {
				r.addFinding(DoctorError, "security_policy_acl_missing", dbOVNNorthbound, tablePortGroup, pg.Name, "security policy references a missing ACL", map[string]string{"acl_uuid": aclUUID})
				continue
			}
			if ovsResourceOwnedBy(acl.ExternalIDs, "SecurityPolicy", pg.Name) {
				ownedACLRefs[aclUUID] = true
				continue
			}
			r.addFinding(DoctorWarning, "security_policy_acl_unowned", dbOVNNorthbound, tableACL, aclUUID, "security policy references an ACL without matching ovnflow ownership", map[string]string{"policy": pg.Name})
		}
	}
	for uuid, acl := range state.nbACLs {
		if !ovsResourceOwnedBy(acl.ExternalIDs, "SecurityPolicy", acl.ExternalIDs[ExternalIDNameKey]) {
			continue
		}
		if !auditExternalIDsInScope(acl.ExternalIDs, auditObjectName(acl.ExternalIDs), opts) {
			continue
		}
		if !ownedACLRefs[uuid] {
			r.addFinding(DoctorWarning, "security_policy_acl_without_port_group", dbOVNNorthbound, tableACL, uuid, "owned security policy ACL is not referenced by its Port_Group", map[string]string{"policy": acl.ExternalIDs[ExternalIDNameKey]})
		}
	}
}

func (r *OwnershipAuditReport) checkOVSReferences(state *auditState, opts OwnershipAuditOptions) {
	for _, root := range state.ovsRoots {
		if !auditExternalIDsInScope(root.ExternalIDs, "", opts) {
			continue
		}
		for _, bridgeUUID := range root.Bridges {
			if _, ok := state.ovsBridges[bridgeUUID]; !ok {
				r.addFinding(DoctorError, "ovs_bridge_missing", dbOpenVSwitch, tableOpenVSwitch, root.UUID, "Open_vSwitch root references a missing bridge", map[string]string{"bridge_uuid": bridgeUUID})
			}
		}
	}
	for _, bridge := range state.ovsBridges {
		if !auditExternalIDsInScope(bridge.ExternalIDs, bridge.Name, opts) {
			continue
		}
		for _, portUUID := range bridge.Ports {
			if _, ok := state.ovsPorts[portUUID]; !ok {
				r.addFinding(DoctorError, "ovs_port_missing", dbOpenVSwitch, tableBridge, bridge.Name, "OVS bridge references a missing port", map[string]string{"port_uuid": portUUID})
			}
		}
	}
	for _, port := range state.ovsPorts {
		if !auditExternalIDsInScope(port.ExternalIDs, port.Name, opts) {
			continue
		}
		for _, ifaceUUID := range port.Interfaces {
			if _, ok := state.ovsInterfaces[ifaceUUID]; !ok {
				r.addFinding(DoctorError, "ovs_interface_missing", dbOpenVSwitch, tablePort, port.Name, "OVS port references a missing interface", map[string]string{"interface_uuid": ifaceUUID})
			}
		}
	}
}

func (r *OwnershipAuditReport) addOwnedResource(database, table, uuid, name string, externalIDs map[string]string, opts OwnershipAuditOptions) {
	if !auditIsManaged(externalIDs) {
		return
	}
	kind := externalIDs[ExternalIDKindKey]
	if name == "" {
		name = auditObjectName(externalIDs)
	}
	owner, labels := ownerAndLabelsFromExternalIDs(externalIDs)
	resource := OwnershipAuditResource{
		Database:    database,
		Table:       table,
		UUID:        uuid,
		Name:        name,
		Kind:        kind,
		Owner:       owner,
		Labels:      labels,
		ExternalIDs: cloneStringMap(externalIDs),
	}
	if !auditResourceMatches(resource, opts) {
		return
	}
	r.Resources = append(r.Resources, resource)
	r.checkOwnershipMarker(resource)
}

func (r *OwnershipAuditReport) checkOwnershipMarker(resource OwnershipAuditResource) {
	if resource.ExternalIDs[ExternalIDAPIVersionKey] != "v2" {
		r.addFinding(DoctorWarning, "owned_resource_missing_v2_marker", resource.Database, resource.Table, resource.Name, "owned resource is missing ovnflow v2 api-version marker", map[string]string{"uuid": resource.UUID})
	}
	if resource.Kind == "" {
		r.addFinding(DoctorWarning, "owned_resource_missing_kind", resource.Database, resource.Table, resource.Name, "owned resource is missing ovnflow kind marker", map[string]string{"uuid": resource.UUID})
	}
	if resource.ExternalIDs[ExternalIDNameKey] == "" {
		r.addFinding(DoctorWarning, "owned_resource_missing_name", resource.Database, resource.Table, resource.Name, "owned resource is missing ovnflow name marker", map[string]string{"uuid": resource.UUID})
	}
	if resource.Owner.Kind == "" || (resource.Owner.Name == "" && resource.Owner.ID == "") {
		r.addFinding(DoctorWarning, "owned_resource_missing_owner", resource.Database, resource.Table, resource.Name, "owned resource is missing a complete owner marker", map[string]string{"uuid": resource.UUID})
	}
	for key := range resource.ExternalIDs {
		if strings.HasPrefix(key, ExternalIDLabelPrefix) {
			if _, ok := DecodeExternalIDLabelKey(key); !ok {
				r.addFinding(DoctorWarning, "owned_resource_invalid_label_key", resource.Database, resource.Table, resource.Name, "owned resource has an invalid encoded label key", map[string]string{"key": key})
			}
		}
	}
}

func auditIsManaged(externalIDs map[string]string) bool {
	return externalIDs[ExternalIDManagedByKey] == "ovnflow"
}

func auditObjectName(externalIDs map[string]string) string {
	if externalIDs[ExternalIDNameKey] != "" {
		return externalIDs[ExternalIDNameKey]
	}
	if externalIDs[dnsNameExternalID] != "" {
		return externalIDs[dnsNameExternalID]
	}
	return ""
}

func auditResourceMatches(resource OwnershipAuditResource, opts OwnershipAuditOptions) bool {
	if ownerFilterSet(opts.Owner) && !OwnershipMatches(resource.ExternalIDs, opts.Owner) {
		return false
	}
	if len(opts.Kinds) > 0 && !stringInSet(resource.Kind, opts.Kinds) {
		return false
	}
	if len(opts.Names) > 0 && !stringInSet(resource.Name, opts.Names) && !stringInSet(resource.ExternalIDs[ExternalIDNameKey], opts.Names) {
		return false
	}
	return true
}

func auditExternalIDsInScope(externalIDs map[string]string, name string, opts OwnershipAuditOptions) bool {
	if !auditIsManaged(externalIDs) {
		return false
	}
	resource := OwnershipAuditResource{
		Name:        name,
		Kind:        externalIDs[ExternalIDKindKey],
		ExternalIDs: externalIDs,
	}
	if resource.Name == "" {
		resource.Name = auditObjectName(externalIDs)
	}
	return auditResourceMatches(resource, opts)
}

func auditProviderMarkerInScope(provider string, opts OwnershipAuditOptions) bool {
	if ownerFilterSet(opts.Owner) {
		return false
	}
	if len(opts.Kinds) > 0 && !stringInSet("ProviderNetwork", opts.Kinds) {
		return false
	}
	if len(opts.Names) > 0 && !stringInSet(provider, opts.Names) {
		return false
	}
	return true
}

func ownerFilterSet(owner OwnerRef) bool {
	return owner.Kind != "" || owner.Name != "" || owner.ID != ""
}

func stringInSet(value string, values []string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}

func (r *OwnershipAuditReport) addFinding(severity DoctorSeverity, code, database, table, object, message string, details map[string]string) {
	r.Findings = append(r.Findings, OwnershipAuditFinding{
		Severity: severity,
		Code:     code,
		Database: database,
		Table:    table,
		Object:   object,
		Message:  message,
		Details:  cloneStringMap(details),
	})
}

func (r *OwnershipAuditReport) finalize() *OwnershipAuditReport {
	sort.SliceStable(r.Resources, func(i, j int) bool {
		if r.Resources[i].Database != r.Resources[j].Database {
			return r.Resources[i].Database < r.Resources[j].Database
		}
		if r.Resources[i].Table != r.Resources[j].Table {
			return r.Resources[i].Table < r.Resources[j].Table
		}
		if r.Resources[i].Kind != r.Resources[j].Kind {
			return r.Resources[i].Kind < r.Resources[j].Kind
		}
		return r.Resources[i].Name < r.Resources[j].Name
	})
	sort.SliceStable(r.Findings, func(i, j int) bool {
		if r.Findings[i].Severity != r.Findings[j].Severity {
			return severityRank(r.Findings[i].Severity) > severityRank(r.Findings[j].Severity)
		}
		if r.Findings[i].Code != r.Findings[j].Code {
			return r.Findings[i].Code < r.Findings[j].Code
		}
		if r.Findings[i].Database != r.Findings[j].Database {
			return r.Findings[i].Database < r.Findings[j].Database
		}
		if r.Findings[i].Table != r.Findings[j].Table {
			return r.Findings[i].Table < r.Findings[j].Table
		}
		return r.Findings[i].Object < r.Findings[j].Object
	})
	r.Summary.OwnedResources = len(r.Resources)
	r.Summary.Findings = len(r.Findings)
	r.Summary.Errors = 0
	r.Summary.Warnings = 0
	for _, finding := range r.Findings {
		switch finding.Severity {
		case DoctorError:
			r.Summary.Errors++
		case DoctorWarning:
			r.Summary.Warnings++
		}
	}
	return r
}

func dnsFromAuditRow(row Row) DNS {
	return DNS{
		UUID:        anyString(row[colUUID]),
		Records:     anyStringMap(row[colRecords]),
		Options:     anyStringMap(row[colOptions]),
		ExternalIDs: anyStringMap(row[colExternalIDs]),
	}
}

func portGroupFromAuditRow(row Row) PortGroup {
	return PortGroup{
		UUID:        anyString(row[colUUID]),
		Name:        anyString(row[colName]),
		Ports:       anyStringSlice(row[colPorts]),
		ACLs:        anyStringSlice(row[colACLs]),
		ExternalIDs: anyStringMap(row[colExternalIDs]),
	}
}

func aclFromAuditRow(row Row) ACL {
	return ACL{
		UUID:        anyString(row[colUUID]),
		Priority:    auditAnyInt(row[colPriority]),
		Direction:   anyString(row[colDirection]),
		Match:       anyString(row[colMatch]),
		Action:      anyString(row[colAction]),
		ExternalIDs: anyStringMap(row[colExternalIDs]),
	}
}

func openVSwitchFromAuditRow(row Row) OpenVSwitch {
	return OpenVSwitch{
		UUID:        anyString(row[colUUID]),
		Bridges:     anyStringSlice(row[colBridges]),
		Managers:    anyStringSlice(row[colManagerOptions]),
		SSL:         anyOptionalString(row[colSSL]),
		ExternalIDs: anyStringMap(row[colExternalIDs]),
		OtherConfig: anyStringMap(row[colOtherConfig]),
	}
}

func ovsBridgeFromAuditRow(row Row) OVSBridge {
	return OVSBridge{
		UUID:        anyString(row[colUUID]),
		Name:        anyString(row[colName]),
		Ports:       anyStringSlice(row[colPorts]),
		Controllers: anyStringSlice(row[colController]),
		Mirrors:     anyStringSlice(row[colMirrors]),
		ExternalIDs: anyStringMap(row[colExternalIDs]),
		OtherConfig: anyStringMap(row[colOtherConfig]),
	}
}

func ovsPortFromAuditRow(row Row) OVSPort {
	return OVSPort{
		UUID:        anyString(row[colUUID]),
		Name:        anyString(row[colName]),
		Interfaces:  anyStringSlice(row[colInterfaces]),
		QoS:         anyOptionalString(row[colQoS]),
		ExternalIDs: anyStringMap(row[colExternalIDs]),
		OtherConfig: anyStringMap(row[colOtherConfig]),
	}
}

func ovsInterfaceFromAuditRow(row Row) OVSInterface {
	return OVSInterface{
		UUID:        anyString(row[colUUID]),
		Name:        anyString(row[colName]),
		Type:        anyString(row[colType]),
		Options:     anyStringMap(row[colOptions]),
		ExternalIDs: anyStringMap(row[colExternalIDs]),
		OtherConfig: anyStringMap(row[colOtherConfig]),
	}
}

func auditAnyInt(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	default:
		return 0
	}
}

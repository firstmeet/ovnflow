package ovnflow

import (
	"context"
	"encoding/base64"
	"net"
	"reflect"
	"sort"
	"strconv"
	"strings"

	libovsdb "github.com/ovn-kubernetes/libovsdb/ovsdb"
)

type VirtualNetwork struct {
	Name    string
	CIDRs   []string
	Gateway string
	DNS     LogicalSwitchDNS
	Owner   OwnerRef
	Labels  Labels
}

type LogicalSwitchDNS struct {
	Name    string
	Records []DNSRecord
	Owner   OwnerRef
	Labels  Labels
}

type DNSRecord struct {
	Domain string
	IPs    []string
}

type WorkloadAttachment struct {
	Name          string
	Network       string
	Workload      string
	InterfaceName string
	MAC           string
	IPs           []string
	LocalOVS      WorkloadLocalOVS
	Owner         OwnerRef
	Labels        Labels
}

type WorkloadLocalOVS struct {
	Bridge        string
	PortName      string
	InterfaceName string
	InterfaceType string
	Options       map[string]string
}

type ProviderNetwork struct {
	Name            string
	PhysicalNetwork string
	LogicalSwitch   string
	LocalnetPort    string
	Bridge          string
	Owner           OwnerRef
	Labels          Labels
}

type SecurityPolicy struct {
	Name    string
	Subject string
	Rules   []SecurityRule
	Owner   OwnerRef
	Labels  Labels
}

type SecurityRule struct {
	Name        string
	Action      string
	Direction   string
	Protocol    string
	CIDRs       []string
	Ports       []int
	Established bool
}

type VirtualNetworkPatch struct {
	AddCIDRs     []string
	RemoveCIDRs  []string
	Gateway      *string
	DNS          *LogicalSwitchDNS
	Owner        *OwnerRef
	Labels       Labels
	RemoveLabels []string
}

type LogicalSwitchDNSPatch struct {
	ReplaceRecords bool
	Records        []DNSRecord
	AddRecords     []DNSRecord
	RemoveDomains  []string
	Owner          *OwnerRef
	Labels         Labels
	RemoveLabels   []string
}

type WorkloadAttachmentPatch struct {
	Network       *string
	Workload      *string
	InterfaceName *string
	MAC           *string
	AddIPs        []string
	RemoveIPs     []string
	Owner         *OwnerRef
	Labels        Labels
	RemoveLabels  []string
}

type ProviderNetworkPatch struct {
	PhysicalNetwork *string
	LogicalSwitch   *string
	LocalnetPort    *string
	Bridge          *string
	Owner           *OwnerRef
	Labels          Labels
	RemoveLabels    []string
}

type SecurityPolicyPatch struct {
	Subject      *string
	ReplaceRules bool
	Rules        []SecurityRule
	AddRules     []SecurityRule
	RemoveRules  []string
	Owner        *OwnerRef
	Labels       Labels
	RemoveLabels []string
}

func (n *NBClient) VirtualNetwork(name string) *VirtualNetworkRef {
	return &VirtualNetworkRef{client: n, name: name}
}

func (n *NBClient) LogicalSwitchDNS(name string) *LogicalSwitchDNSRef {
	return &LogicalSwitchDNSRef{client: n, name: name}
}

func (n *NBClient) WorkloadAttachment(name string) *WorkloadAttachmentRef {
	return &WorkloadAttachmentRef{client: n, name: name}
}

func (n *NBClient) SecurityPolicy(name string) *SecurityPolicyRef {
	return &SecurityPolicyRef{client: n, name: name}
}

func (c *Client) WorkloadAttachment(name string) *WorkloadAttachmentRef {
	if c == nil {
		return &WorkloadAttachmentRef{name: name}
	}
	return &WorkloadAttachmentRef{
		client: &NBClient{db: c.nb},
		ovs:    &OVSClient{db: c.ovs},
		name:   name,
	}
}

func (c *Client) ProviderNetwork(name string) *ProviderNetworkRef {
	if c == nil {
		return &ProviderNetworkRef{name: name}
	}
	return &ProviderNetworkRef{
		client: &NBClient{db: c.nb},
		ovs:    &OVSClient{db: c.ovs},
		name:   name,
	}
}

type VirtualNetworkRef struct {
	client *NBClient
	name   string
}

func (r *VirtualNetworkRef) Ensure() *VirtualNetworkBuilder {
	return &VirtualNetworkBuilder{ref: r, spec: VirtualNetwork{Name: r.name, Labels: Labels{}}}
}

func (r *VirtualNetworkRef) Get(ctx context.Context) (*VirtualNetwork, error) {
	if err := validateName("virtual network", r.name); err != nil {
		return nil, err
	}
	if r.client == nil || r.client.db == nil {
		return nil, ErrBackendUnavailable
	}
	ls, err := r.client.GetLogicalSwitch(ctx, r.name)
	if err != nil {
		return nil, err
	}
	return virtualNetworkFromLogicalSwitch(ls), nil
}

func (r *VirtualNetworkRef) Inspect(ctx context.Context) (InspectResult, error) {
	if err := validateName("virtual network", r.name); err != nil {
		return InspectResult{}, err
	}
	if r.client != nil && r.client.db != nil {
		vn, err := r.Get(ctx)
		if err != nil {
			return InspectResult{}, err
		}
		return InspectResult{Resource: "VirtualNetwork", Name: r.name, Status: map[string]any{
			"state":   "present",
			"cidrs":   vn.CIDRs,
			"gateway": vn.Gateway,
			"owner":   vn.Owner,
			"labels":  vn.Labels,
		}}, nil
	}
	return InspectResult{Resource: "VirtualNetwork", Name: r.name, Status: "stub"}, nil
}

func (r *VirtualNetworkRef) Delete(ctx context.Context) error {
	if err := validateName("virtual network", r.name); err != nil {
		return err
	}
	if r.client == nil || r.client.db == nil {
		return ErrBackendUnavailable
	}
	return r.client.LogicalSwitch(r.name).Delete().Execute(ctx)
}

func (r *VirtualNetworkRef) Apply(ctx context.Context, network VirtualNetwork) error {
	if r.client == nil || r.client.db == nil {
		return ErrBackendUnavailable
	}
	if network.Name == "" {
		network.Name = r.name
	}
	if network.Name != r.name {
		return wrap(ErrorConflict, dbOVNNorthbound, tableLogicalSwitch, "apply", network.Name, "virtual network name does not match reference", nil)
	}
	builder := &VirtualNetworkBuilder{ref: r, spec: network}
	_, err := builder.Reconcile(ctx)
	return err
}

func (r *VirtualNetworkRef) Patch(ctx context.Context, patch VirtualNetworkPatch) (*VirtualNetwork, error) {
	current, err := r.Get(ctx)
	if err != nil {
		return nil, err
	}
	next := cloneVirtualNetwork(current)
	next.CIDRs = uniqueStrings(append(next.CIDRs, patch.AddCIDRs...))
	next.CIDRs = removeStrings(next.CIDRs, patch.RemoveCIDRs)
	if patch.Gateway != nil {
		next.Gateway = *patch.Gateway
	}
	if patch.DNS != nil {
		next.DNS = cloneLogicalSwitchDNS(patch.DNS)
	}
	if patch.Owner != nil {
		next.Owner = *patch.Owner
	}
	next.Labels = patchLabels(next.Labels, patch.Labels, patch.RemoveLabels)
	if err := r.Apply(ctx, next); err != nil {
		return nil, err
	}
	var removeOtherConfig []string
	if len(next.CIDRs) == 0 && len(current.CIDRs) > 0 {
		removeOtherConfig = append(removeOtherConfig, "subnet")
	}
	if patch.Gateway != nil && next.Gateway == "" && current.Gateway != "" {
		removeOtherConfig = append(removeOtherConfig, "gateway")
	}
	if err := r.client.deleteMapKeys(ctx, tableLogicalSwitch, r.name, colOtherConfig, conditionName(r.name), removeOtherConfig); err != nil {
		return nil, err
	}
	if err := r.client.deleteExternalIDKeys(ctx, tableLogicalSwitch, r.name, conditionName(r.name), labelDeleteKeys(patch.RemoveLabels, patch.Labels)); err != nil {
		return nil, err
	}
	return &next, nil
}

type VirtualNetworkBuilder struct {
	ref  *VirtualNetworkRef
	spec VirtualNetwork
}

func (b *VirtualNetworkBuilder) WithCIDR(cidr string) *VirtualNetworkBuilder {
	b.spec.CIDRs = append(b.spec.CIDRs, cidr)
	return b
}

func (b *VirtualNetworkBuilder) WithGateway(ip string) *VirtualNetworkBuilder {
	b.spec.Gateway = ip
	return b
}

func (b *VirtualNetworkBuilder) WithOwner(kind, name string) *VirtualNetworkBuilder {
	b.spec.Owner = OwnerRef{Kind: kind, Name: name}
	return b
}

func (b *VirtualNetworkBuilder) WithLabel(key, value string) *VirtualNetworkBuilder {
	if b.spec.Labels == nil {
		b.spec.Labels = Labels{}
	}
	b.spec.Labels[key] = value
	return b
}

func (b *VirtualNetworkBuilder) WithDNS(name string, configure func(*LogicalSwitchDNSBuilder)) *VirtualNetworkBuilder {
	dns := &LogicalSwitchDNSBuilder{spec: LogicalSwitchDNS{Name: name}}
	if configure != nil {
		configure(dns)
	}
	b.spec.DNS = dns.spec
	return b
}

func (b *VirtualNetworkBuilder) Validate() error {
	return b.spec.Validate()
}

func (b *VirtualNetworkBuilder) Plan(ctx context.Context) (Plan, error) {
	if err := b.Validate(); err != nil {
		return Plan{}, err
	}
	return Plan{Operations: []PlannedOperation{{
		Action:      IntentActionEnsure,
		Resource:    "VirtualNetwork",
		Name:        b.spec.Name,
		Description: "validate and plan logical switch intent",
	}}}, nil
}

func (b *VirtualNetworkBuilder) DryRun(ctx context.Context) (DryRunResult, error) {
	plan, err := b.Plan(ctx)
	if err != nil {
		return DryRunResult{}, err
	}
	diff, err := b.diff(ctx)
	if err != nil {
		return DryRunResult{}, err
	}
	return DryRunResult{Plan: plan, Diff: diff}, nil
}

func (b *VirtualNetworkBuilder) Reconcile(ctx context.Context) (ReconcileResult, error) {
	plan, err := b.Plan(ctx)
	if err != nil {
		return ReconcileResult{}, err
	}
	diff, err := b.diff(ctx)
	if err != nil {
		return ReconcileResult{}, err
	}
	if b.ref == nil || b.ref.client == nil || b.ref.client.db == nil {
		return ReconcileResult{Plan: plan, Diff: diff, Applied: false}, nil
	}
	if diff.Empty() {
		return ReconcileResult{Plan: plan, Diff: diff, Applied: false}, nil
	}
	if err := b.reconcileOVSDB(ctx); err != nil {
		return ReconcileResult{}, err
	}
	return ReconcileResult{Plan: plan, Diff: diff, Applied: !diff.Empty()}, nil
}

func (b *VirtualNetworkBuilder) Execute(ctx context.Context) error {
	_, err := b.Reconcile(ctx)
	return err
}

type LogicalSwitchDNSBuilder struct {
	ref  *LogicalSwitchDNSRef
	spec LogicalSwitchDNS
}

type LogicalSwitchDNSRef struct {
	client *NBClient
	name   string
}

func (r *LogicalSwitchDNSRef) Ensure() *LogicalSwitchDNSBuilder {
	return &LogicalSwitchDNSBuilder{ref: r, spec: LogicalSwitchDNS{Name: r.name, Labels: Labels{}}}
}

func (r *LogicalSwitchDNSRef) Get(ctx context.Context) (*LogicalSwitchDNS, error) {
	if err := validateName("logical switch dns", r.name); err != nil {
		return nil, err
	}
	if r.client == nil || r.client.db == nil {
		return nil, ErrBackendUnavailable
	}
	dns, err := r.client.GetDNS(ctx, r.name)
	if err != nil {
		return nil, err
	}
	return logicalSwitchDNSFromDNS(r.name, dns), nil
}

func (r *LogicalSwitchDNSRef) Inspect(ctx context.Context) (InspectResult, error) {
	dns, err := r.Get(ctx)
	if err != nil {
		return InspectResult{}, err
	}
	return InspectResult{Resource: "LogicalSwitchDNS", Name: r.name, Status: dns}, nil
}

func (r *LogicalSwitchDNSRef) Delete(ctx context.Context) error {
	if err := validateName("logical switch dns", r.name); err != nil {
		return err
	}
	if r.client == nil || r.client.db == nil {
		return ErrBackendUnavailable
	}
	return r.client.DNS(r.name).Delete().Execute(ctx)
}

func (r *LogicalSwitchDNSRef) Apply(ctx context.Context, dns LogicalSwitchDNS) error {
	if r.client == nil || r.client.db == nil {
		return ErrBackendUnavailable
	}
	if dns.Name == "" {
		dns.Name = r.name
	}
	if dns.Name != r.name {
		return wrap(ErrorConflict, dbOVNNorthbound, tableDNS, "apply", dns.Name, "logical switch dns name does not match reference", nil)
	}
	builder := &LogicalSwitchDNSBuilder{ref: r, spec: dns}
	_, err := builder.Reconcile(ctx)
	return err
}

func (r *LogicalSwitchDNSRef) Patch(ctx context.Context, patch LogicalSwitchDNSPatch) (*LogicalSwitchDNS, error) {
	current, err := r.Get(ctx)
	if err != nil {
		return nil, err
	}
	next := cloneLogicalSwitchDNS(current)
	if patch.ReplaceRecords {
		next.Records = cloneDNSRecords(patch.Records)
	}
	next.Records = mergeDNSRecords(next.Records, patch.AddRecords)
	next.Records = removeDNSRecordDomains(next.Records, patch.RemoveDomains)
	if patch.Owner != nil {
		next.Owner = *patch.Owner
	}
	next.Labels = patchLabels(next.Labels, patch.Labels, patch.RemoveLabels)
	if err := r.Apply(ctx, next); err != nil {
		return nil, err
	}
	if err := r.deleteRecords(ctx, dnsDomainsAbsent(current.Records, next.Records)); err != nil {
		return nil, err
	}
	if err := r.client.deleteExternalIDKeys(ctx, tableDNS, r.name, nbExternalIDCondition(dnsNameExternalID, r.name), labelDeleteKeys(patch.RemoveLabels, patch.Labels)); err != nil {
		return nil, err
	}
	return &next, nil
}

func (r *LogicalSwitchDNSRef) deleteRecords(ctx context.Context, domains []string) error {
	domains = uniqueStrings(domains)
	if len(domains) == 0 {
		return nil
	}
	rows, err := r.client.selectRows(ctx, tableDNS, nbExternalIDCondition(dnsNameExternalID, r.name), []string{colUUID}, r.name)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	results, err := r.client.db.transact(ctx, tableDNS, "patch", r.name, libovsdb.Operation{
		Op:    libovsdb.OperationMutate,
		Table: tableDNS,
		Where: conditionUUID(rowUUIDValue(rows[0])),
		Mutations: []libovsdb.Mutation{
			*libovsdb.NewMutation(colRecords, libovsdb.MutateOperationDelete, ovsMapKeys(domains)),
		},
	})
	if err != nil {
		return err
	}
	return ensureAffected(results, []int{0}, dbOVNNorthbound, tableDNS, "patch", r.name)
}

func (b *LogicalSwitchDNSBuilder) AddRecord(domain string, ips ...string) *LogicalSwitchDNSBuilder {
	b.spec.Records = append(b.spec.Records, DNSRecord{Domain: domain, IPs: append([]string{}, ips...)})
	return b
}

func (b *LogicalSwitchDNSBuilder) WithOwner(kind, name string) *LogicalSwitchDNSBuilder {
	b.spec.Owner = OwnerRef{Kind: kind, Name: name}
	return b
}

func (b *LogicalSwitchDNSBuilder) WithLabel(key, value string) *LogicalSwitchDNSBuilder {
	if b.spec.Labels == nil {
		b.spec.Labels = Labels{}
	}
	b.spec.Labels[key] = value
	return b
}

func (b *LogicalSwitchDNSBuilder) Validate() error {
	return b.spec.Validate()
}

func (b *LogicalSwitchDNSBuilder) Plan(ctx context.Context) (Plan, error) {
	if err := b.Validate(); err != nil {
		return Plan{}, err
	}
	return Plan{Operations: []PlannedOperation{{
		Action:      IntentActionEnsure,
		Resource:    "LogicalSwitchDNS",
		Name:        b.spec.Name,
		Description: "validate and plan logical switch DNS records",
	}}}, nil
}

func (b *LogicalSwitchDNSBuilder) DryRun(ctx context.Context) (DryRunResult, error) {
	plan, err := b.Plan(ctx)
	if err != nil {
		return DryRunResult{}, err
	}
	diff, err := b.diff(ctx)
	if err != nil {
		return DryRunResult{}, err
	}
	return DryRunResult{Plan: plan, Diff: diff}, nil
}

func (b *LogicalSwitchDNSBuilder) Reconcile(ctx context.Context) (ReconcileResult, error) {
	plan, err := b.Plan(ctx)
	if err != nil {
		return ReconcileResult{}, err
	}
	diff, err := b.diff(ctx)
	if err != nil {
		return ReconcileResult{}, err
	}
	if b.ref == nil || b.ref.client == nil || b.ref.client.db == nil {
		return ReconcileResult{Plan: plan, Diff: diff, Applied: false}, nil
	}
	if diff.Empty() {
		return ReconcileResult{Plan: plan, Diff: diff, Applied: false}, nil
	}
	if err := b.reconcileOVSDB(ctx); err != nil {
		return ReconcileResult{}, err
	}
	return ReconcileResult{Plan: plan, Diff: diff, Applied: !diff.Empty()}, nil
}

func (d LogicalSwitchDNS) RecordMap() map[string][]string {
	out := map[string][]string{}
	for _, record := range d.Records {
		out[record.Domain] = append(out[record.Domain], record.IPs...)
	}
	for domain := range out {
		sort.Strings(out[domain])
	}
	return out
}

type WorkloadAttachmentRef struct {
	client *NBClient
	ovs    *OVSClient
	name   string
}

func (r *WorkloadAttachmentRef) Ensure() *WorkloadAttachmentBuilder {
	return &WorkloadAttachmentBuilder{ref: r, spec: WorkloadAttachment{Name: r.name, Labels: Labels{}}}
}

func (r *WorkloadAttachmentRef) Get(ctx context.Context) (*WorkloadAttachment, error) {
	if err := validateName("workload attachment", r.name); err != nil {
		return nil, err
	}
	if r.client == nil || r.client.db == nil {
		return nil, ErrBackendUnavailable
	}
	lsp, err := r.client.GetLogicalSwitchPort(ctx, r.name)
	if err != nil {
		return nil, err
	}
	attachment := workloadAttachmentFromLogicalSwitchPort(lsp)
	if r.ovs != nil && r.ovs.db != nil {
		if local, err := r.getLocalOVS(ctx); err == nil {
			attachment.LocalOVS = local
		} else if !IsKind(err, ErrorNotFound) {
			return nil, err
		}
	}
	return attachment, nil
}

func (r *WorkloadAttachmentRef) Inspect(ctx context.Context) (InspectResult, error) {
	attachment, err := r.Get(ctx)
	if err != nil {
		return InspectResult{}, err
	}
	return InspectResult{Resource: "WorkloadAttachment", Name: r.name, Status: attachment}, nil
}

func (r *WorkloadAttachmentRef) Delete(ctx context.Context) error {
	if err := validateName("workload attachment", r.name); err != nil {
		return err
	}
	if r.client == nil || r.client.db == nil {
		return ErrBackendUnavailable
	}
	return r.client.TableLogicalSwitchPort(r.name).Delete().Execute(ctx)
}

func (r *WorkloadAttachmentRef) Apply(ctx context.Context, attachment WorkloadAttachment) error {
	if r.client == nil || r.client.db == nil {
		return ErrBackendUnavailable
	}
	if attachment.Name == "" {
		attachment.Name = r.name
	}
	if attachment.Name != r.name {
		return wrap(ErrorConflict, dbOVNNorthbound, tableLogicalSwitchPort, "apply", attachment.Name, "workload attachment name does not match reference", nil)
	}
	builder := &WorkloadAttachmentBuilder{ref: r, spec: attachment}
	_, err := builder.Reconcile(ctx)
	return err
}

func (r *WorkloadAttachmentRef) Patch(ctx context.Context, patch WorkloadAttachmentPatch) (*WorkloadAttachment, error) {
	current, err := r.Get(ctx)
	if err != nil {
		return nil, err
	}
	next := cloneWorkloadAttachment(current)
	if patch.Network != nil {
		next.Network = *patch.Network
	}
	if patch.Workload != nil {
		next.Workload = *patch.Workload
	}
	if patch.InterfaceName != nil {
		next.InterfaceName = *patch.InterfaceName
	}
	if patch.MAC != nil {
		next.MAC = *patch.MAC
	}
	next.IPs = uniqueStrings(append(next.IPs, patch.AddIPs...))
	next.IPs = removeStrings(next.IPs, patch.RemoveIPs)
	if patch.Owner != nil {
		next.Owner = *patch.Owner
	}
	next.Labels = patchLabels(next.Labels, patch.Labels, patch.RemoveLabels)
	if err := r.Apply(ctx, next); err != nil {
		return nil, err
	}
	removeKeys := labelDeleteKeys(patch.RemoveLabels, patch.Labels)
	if patch.Workload != nil && *patch.Workload == "" {
		removeKeys = append(removeKeys, ExternalIDPrefix+"workload")
	}
	if patch.InterfaceName != nil && *patch.InterfaceName == "" {
		removeKeys = append(removeKeys, ExternalIDPrefix+"interface")
	}
	if err := r.client.deleteExternalIDKeys(ctx, tableLogicalSwitchPort, r.name, conditionName(r.name), removeKeys); err != nil {
		return nil, err
	}
	return &next, nil
}

func (r *WorkloadAttachmentRef) DetachLocalOVS(ctx context.Context) error {
	if err := validateName("workload attachment", r.name); err != nil {
		return err
	}
	if r.ovs == nil || r.ovs.db == nil {
		return ErrBackendUnavailable
	}
	ports, err := r.ovs.ListPorts(ctx)
	if err != nil {
		return err
	}
	var ownedPort *OVSPort
	for i := range ports {
		if ovsResourceOwnedBy(ports[i].ExternalIDs, "WorkloadAttachment", r.name) {
			ownedPort = &ports[i]
			break
		}
	}
	if ownedPort == nil {
		return wrap(ErrorNotFound, dbOpenVSwitch, tablePort, "detach", r.name, "local OVS workload port not found", nil)
	}
	bridges, err := r.ovs.ListBridges(ctx)
	if err != nil {
		return err
	}
	for _, bridge := range bridges {
		if containsString(bridge.Ports, ownedPort.UUID) {
			return r.ovs.Bridge(bridge.Name).DeletePort(ownedPort.Name).Execute(ctx)
		}
	}
	return wrap(ErrorNotFound, dbOpenVSwitch, tableBridge, "detach", ownedPort.Name, "owning bridge not found", nil)
}

func (r *WorkloadAttachmentRef) getLocalOVS(ctx context.Context) (WorkloadLocalOVS, error) {
	ports, err := r.ovs.ListPorts(ctx)
	if err != nil {
		return WorkloadLocalOVS{}, err
	}
	for _, port := range ports {
		if !ovsResourceOwnedBy(port.ExternalIDs, "WorkloadAttachment", r.name) {
			continue
		}
		local := WorkloadLocalOVS{PortName: port.Name}
		if len(port.Interfaces) > 0 {
			iface, err := r.ovs.getInterfaceByUUID(ctx, port.Interfaces[0])
			if err != nil {
				return WorkloadLocalOVS{}, err
			}
			local.InterfaceName = iface.Name
			local.InterfaceType = iface.Type
			local.Options = cloneStringMap(iface.Options)
		}
		bridges, err := r.ovs.ListBridges(ctx)
		if err != nil {
			return WorkloadLocalOVS{}, err
		}
		for _, bridge := range bridges {
			if containsString(bridge.Ports, port.UUID) {
				local.Bridge = bridge.Name
				break
			}
		}
		return local, nil
	}
	return WorkloadLocalOVS{}, wrap(ErrorNotFound, dbOpenVSwitch, tablePort, "get", r.name, "local OVS workload port not found", nil)
}

type WorkloadAttachmentBuilder struct {
	ref  *WorkloadAttachmentRef
	spec WorkloadAttachment
}

func (b *WorkloadAttachmentBuilder) OnNetwork(name string) *WorkloadAttachmentBuilder {
	b.spec.Network = name
	return b
}

func (b *WorkloadAttachmentBuilder) WithWorkload(name string) *WorkloadAttachmentBuilder {
	b.spec.Workload = name
	return b
}

func (b *WorkloadAttachmentBuilder) WithInterface(name string) *WorkloadAttachmentBuilder {
	b.spec.InterfaceName = name
	return b
}

func (b *WorkloadAttachmentBuilder) WithMAC(mac string) *WorkloadAttachmentBuilder {
	b.spec.MAC = mac
	return b
}

func (b *WorkloadAttachmentBuilder) WithIP(ip string) *WorkloadAttachmentBuilder {
	b.spec.IPs = append(b.spec.IPs, ip)
	return b
}

func (b *WorkloadAttachmentBuilder) SyncLocalOVS(bridge string) *WorkloadAttachmentBuilder {
	b.spec.LocalOVS.Bridge = bridge
	return b
}

func (b *WorkloadAttachmentBuilder) WithOVSPort(name string) *WorkloadAttachmentBuilder {
	b.spec.LocalOVS.PortName = name
	return b
}

func (b *WorkloadAttachmentBuilder) WithOVSInterface(name string) *WorkloadAttachmentBuilder {
	b.spec.LocalOVS.InterfaceName = name
	return b
}

func (b *WorkloadAttachmentBuilder) WithOVSInterfaceType(kind string) *WorkloadAttachmentBuilder {
	b.spec.LocalOVS.InterfaceType = kind
	return b
}

func (b *WorkloadAttachmentBuilder) WithOVSOption(key, value string) *WorkloadAttachmentBuilder {
	if b.spec.LocalOVS.Options == nil {
		b.spec.LocalOVS.Options = map[string]string{}
	}
	b.spec.LocalOVS.Options[key] = value
	return b
}

func (b *WorkloadAttachmentBuilder) WithOwner(kind, name string) *WorkloadAttachmentBuilder {
	b.spec.Owner = OwnerRef{Kind: kind, Name: name}
	return b
}

func (b *WorkloadAttachmentBuilder) WithLabel(key, value string) *WorkloadAttachmentBuilder {
	if b.spec.Labels == nil {
		b.spec.Labels = Labels{}
	}
	b.spec.Labels[key] = value
	return b
}

func (b *WorkloadAttachmentBuilder) Validate() error {
	return b.spec.Validate()
}

func (b *WorkloadAttachmentBuilder) Plan(ctx context.Context) (Plan, error) {
	if err := b.Validate(); err != nil {
		return Plan{}, err
	}
	return Plan{Operations: []PlannedOperation{{
		Action:      IntentActionEnsure,
		Resource:    "WorkloadAttachment",
		Name:        b.spec.Name,
		Description: "validate and plan logical switch port attachment intent",
	}}}, nil
}

func (b *WorkloadAttachmentBuilder) DryRun(ctx context.Context) (DryRunResult, error) {
	plan, err := b.Plan(ctx)
	if err != nil {
		return DryRunResult{}, err
	}
	diff, err := b.diff(ctx)
	if err != nil {
		return DryRunResult{}, err
	}
	return DryRunResult{Plan: plan, Diff: diff}, nil
}

func (b *WorkloadAttachmentBuilder) Reconcile(ctx context.Context) (ReconcileResult, error) {
	plan, err := b.Plan(ctx)
	if err != nil {
		return ReconcileResult{}, err
	}
	if err := b.validateLocalOVSTarget(ctx); err != nil {
		return ReconcileResult{}, err
	}
	diff, err := b.diff(ctx)
	if err != nil {
		return ReconcileResult{}, err
	}
	if b.ref == nil || b.ref.client == nil || b.ref.client.db == nil {
		return ReconcileResult{Plan: plan, Diff: diff, Applied: false}, nil
	}
	if diff.Empty() {
		return ReconcileResult{Plan: plan, Diff: diff, Applied: false}, nil
	}
	if err := b.reconcileOVSDB(ctx); err != nil {
		return ReconcileResult{}, err
	}
	if err := b.reconcileLocalOVS(ctx); err != nil {
		return ReconcileResult{}, err
	}
	return ReconcileResult{Plan: plan, Diff: diff, Applied: !diff.Empty()}, nil
}

func (b *WorkloadAttachmentBuilder) Execute(ctx context.Context) error {
	_, err := b.Reconcile(ctx)
	return err
}

type ProviderNetworkRef struct {
	client *NBClient
	ovs    *OVSClient
	name   string
}

func (r *ProviderNetworkRef) Ensure() *ProviderNetworkBuilder {
	return &ProviderNetworkBuilder{ref: r, spec: ProviderNetwork{Name: r.name, Labels: Labels{}}}
}

func (r *ProviderNetworkRef) Get(ctx context.Context) (*ProviderNetwork, error) {
	if err := validateName("provider network", r.name); err != nil {
		return nil, err
	}
	if r.client == nil || r.client.db == nil {
		return nil, ErrBackendUnavailable
	}
	ports, err := r.client.ListLogicalSwitchPorts(ctx)
	if err != nil {
		return nil, err
	}
	var localnet *LogicalSwitchPort
	for i := range ports {
		if ports[i].Type == "localnet" && ports[i].ExternalIDs[ExternalIDKindKey] == "ProviderNetwork" && ports[i].ExternalIDs[ExternalIDNameKey] == r.name {
			localnet = &ports[i]
			break
		}
	}
	if localnet == nil {
		return nil, wrap(ErrorNotFound, dbOVNNorthbound, tableLogicalSwitchPort, "get", r.name, "provider localnet port not found", nil)
	}
	network := providerNetworkFromLocalnetPort(localnet)
	if network.Name == "" {
		network.Name = r.name
	}
	if r.ovs != nil && r.ovs.db != nil {
		mappings, err := r.ovs.GetBridgeMappings(ctx)
		if err != nil {
			return nil, err
		}
		network.Bridge = mappings[network.PhysicalNetwork]
	}
	return network, nil
}

func (r *ProviderNetworkRef) Inspect(ctx context.Context) (InspectResult, error) {
	network, err := r.Get(ctx)
	if err != nil {
		return InspectResult{}, err
	}
	return InspectResult{Resource: "ProviderNetwork", Name: r.name, Status: network}, nil
}

func (r *ProviderNetworkRef) Apply(ctx context.Context, network ProviderNetwork) error {
	if r.client == nil || r.client.db == nil || r.ovs == nil || r.ovs.db == nil {
		return ErrBackendUnavailable
	}
	if network.Name == "" {
		network.Name = r.name
	}
	if network.Name != r.name {
		return wrap(ErrorConflict, dbOVNNorthbound, tableLogicalSwitchPort, "apply", network.Name, "provider network name does not match reference", nil)
	}
	builder := &ProviderNetworkBuilder{ref: r, spec: network}
	_, err := builder.Reconcile(ctx)
	return err
}

func (r *ProviderNetworkRef) Patch(ctx context.Context, patch ProviderNetworkPatch) (*ProviderNetwork, error) {
	current, err := r.Get(ctx)
	if err != nil {
		return nil, err
	}
	next := cloneProviderNetwork(current)
	if patch.PhysicalNetwork != nil {
		next.PhysicalNetwork = *patch.PhysicalNetwork
	}
	if patch.LogicalSwitch != nil {
		next.LogicalSwitch = *patch.LogicalSwitch
	}
	if patch.LocalnetPort != nil {
		next.LocalnetPort = *patch.LocalnetPort
	}
	if patch.Bridge != nil {
		next.Bridge = *patch.Bridge
	}
	if patch.Owner != nil {
		next.Owner = *patch.Owner
	}
	next.Labels = patchLabels(next.Labels, patch.Labels, patch.RemoveLabels)
	if err := r.Apply(ctx, next); err != nil {
		return nil, err
	}
	if current.PhysicalNetwork != "" && current.PhysicalNetwork != next.PhysicalNetwork {
		if err := r.detachBridgeMapping(ctx, *current); err != nil && !IsKind(err, ErrorNotFound) {
			return nil, err
		}
	}
	if current.LocalnetPort != "" && current.LocalnetPort != next.LocalnetPort {
		if err := r.client.TableLogicalSwitchPort(current.LocalnetPort).Delete().Execute(ctx); err != nil && !IsKind(err, ErrorNotFound) {
			return nil, err
		}
	}
	removeKeys := labelDeleteKeys(patch.RemoveLabels, patch.Labels)
	if err := r.client.deleteExternalIDKeys(ctx, tableLogicalSwitchPort, next.LocalnetPort, conditionName(next.LocalnetPort), removeKeys); err != nil {
		return nil, err
	}
	return &next, nil
}

func (r *ProviderNetworkRef) Delete(ctx context.Context) error {
	if err := validateName("provider network", r.name); err != nil {
		return err
	}
	if r.client == nil || r.client.db == nil || r.ovs == nil || r.ovs.db == nil {
		return ErrBackendUnavailable
	}
	network, err := r.Get(ctx)
	if err != nil {
		return err
	}
	if err := r.detachBridgeMapping(ctx, *network); err != nil && !IsKind(err, ErrorNotFound) {
		return err
	}
	return r.client.TableLogicalSwitchPort(network.LocalnetPort).Delete().Execute(ctx)
}

func (r *ProviderNetworkRef) detachBridgeMapping(ctx context.Context, network ProviderNetwork) error {
	if network.PhysicalNetwork == "" {
		return wrap(ErrorValidation, dbOpenVSwitch, tableOpenVSwitch, "delete", r.name, "physical network is required", nil)
	}
	markers, err := r.providerMappingMarkers(ctx)
	if err != nil {
		return err
	}
	key := providerNetworkMappingOwnerKey(network.PhysicalNetwork)
	owner := markers[key]
	if owner != "" && owner != r.name {
		return wrap(ErrorOwnershipViolation, dbOpenVSwitch, tableOpenVSwitch, "delete", network.PhysicalNetwork, "bridge mapping is managed by another provider network", nil)
	}
	if owner == "" {
		return wrap(ErrorOwnershipViolation, dbOpenVSwitch, tableOpenVSwitch, "delete", network.PhysicalNetwork, "bridge mapping is not managed by ovnflow", nil)
	}
	if err := r.ovs.DeleteBridgeMapping(ctx, network.PhysicalNetwork, network.Bridge); err != nil && !IsKind(err, ErrorNotFound) {
		return err
	}
	return r.deleteProviderMappingMarker(ctx, network.PhysicalNetwork)
}

func (r *ProviderNetworkRef) providerMappingMarkers(ctx context.Context) (map[string]string, error) {
	root, err := r.ovs.GetOpenVSwitch(ctx)
	if err != nil {
		return nil, err
	}
	return root.ExternalIDs, nil
}

func (r *ProviderNetworkRef) deleteProviderMappingMarker(ctx context.Context, physicalNetwork string) error {
	root, err := r.ovs.GetOpenVSwitch(ctx)
	if err != nil {
		return err
	}
	results, err := r.ovs.db.executor.Transact(ctx, libovsdb.Operation{
		Op:    libovsdb.OperationMutate,
		Table: tableOpenVSwitch,
		Where: conditionUUID(root.UUID),
		Mutations: []libovsdb.Mutation{
			*libovsdb.NewMutation(colExternalIDs, libovsdb.MutateOperationDelete, ovsMapKeys([]string{providerNetworkMappingOwnerKey(physicalNetwork)})),
		},
	})
	if err != nil {
		return classifyTransactError(err, dbOpenVSwitch, tableOpenVSwitch, "delete", physicalNetwork)
	}
	return ensureAffected(results, []int{0}, dbOpenVSwitch, tableOpenVSwitch, "delete", physicalNetwork)
}

type ProviderNetworkBuilder struct {
	ref  *ProviderNetworkRef
	spec ProviderNetwork
}

func (b *ProviderNetworkBuilder) WithPhysicalNetwork(name string) *ProviderNetworkBuilder {
	b.spec.PhysicalNetwork = name
	return b
}

func (b *ProviderNetworkBuilder) OnLogicalSwitch(name string) *ProviderNetworkBuilder {
	b.spec.LogicalSwitch = name
	return b
}

func (b *ProviderNetworkBuilder) WithLogicalSwitch(name string) *ProviderNetworkBuilder {
	return b.OnLogicalSwitch(name)
}

func (b *ProviderNetworkBuilder) WithLocalnetPort(name string) *ProviderNetworkBuilder {
	b.spec.LocalnetPort = name
	return b
}

func (b *ProviderNetworkBuilder) UseBridge(name string) *ProviderNetworkBuilder {
	b.spec.Bridge = name
	return b
}

func (b *ProviderNetworkBuilder) WithBridge(name string) *ProviderNetworkBuilder {
	return b.UseBridge(name)
}

func (b *ProviderNetworkBuilder) WithOwner(kind, name string) *ProviderNetworkBuilder {
	b.spec.Owner = OwnerRef{Kind: kind, Name: name}
	return b
}

func (b *ProviderNetworkBuilder) WithLabel(key, value string) *ProviderNetworkBuilder {
	if b.spec.Labels == nil {
		b.spec.Labels = Labels{}
	}
	b.spec.Labels[key] = value
	return b
}

func (b *ProviderNetworkBuilder) Validate() error {
	return normalizeProviderNetwork(b.spec).Validate()
}

func (b *ProviderNetworkBuilder) Plan(ctx context.Context) (Plan, error) {
	if err := b.Validate(); err != nil {
		return Plan{}, err
	}
	desired := normalizeProviderNetwork(b.spec)
	return Plan{Operations: []PlannedOperation{{
		Action:      IntentActionEnsure,
		Resource:    "ProviderNetwork",
		Name:        desired.Name,
		Description: "validate and plan localnet port and OVS bridge mapping intent",
	}}}, nil
}

func (b *ProviderNetworkBuilder) DryRun(ctx context.Context) (DryRunResult, error) {
	plan, err := b.Plan(ctx)
	if err != nil {
		return DryRunResult{}, err
	}
	diff, err := b.diff(ctx)
	if err != nil {
		return DryRunResult{}, err
	}
	return DryRunResult{Plan: plan, Diff: diff}, nil
}

func (b *ProviderNetworkBuilder) Reconcile(ctx context.Context) (ReconcileResult, error) {
	plan, err := b.Plan(ctx)
	if err != nil {
		return ReconcileResult{}, err
	}
	if err := b.validateTargets(ctx); err != nil {
		return ReconcileResult{}, err
	}
	diff, err := b.diff(ctx)
	if err != nil {
		return ReconcileResult{}, err
	}
	if b.ref == nil || b.ref.client == nil || b.ref.client.db == nil || b.ref.ovs == nil || b.ref.ovs.db == nil {
		return ReconcileResult{Plan: plan, Diff: diff, Applied: false}, nil
	}
	if diff.Empty() {
		return ReconcileResult{Plan: plan, Diff: diff, Applied: false}, nil
	}
	if err := b.reconcileOVSDB(ctx); err != nil {
		return ReconcileResult{}, err
	}
	return ReconcileResult{Plan: plan, Diff: diff, Applied: true}, nil
}

func (b *ProviderNetworkBuilder) Execute(ctx context.Context) error {
	_, err := b.Reconcile(ctx)
	return err
}

type SecurityPolicyRef struct {
	client *NBClient
	name   string
}

func (r *SecurityPolicyRef) Ensure() *SecurityPolicyBuilder {
	return &SecurityPolicyBuilder{ref: r, spec: SecurityPolicy{Name: r.name, Labels: Labels{}}}
}

func (r *SecurityPolicyRef) Get(ctx context.Context) (*SecurityPolicy, error) {
	if err := validateName("security policy", r.name); err != nil {
		return nil, err
	}
	if r.client == nil || r.client.db == nil {
		return nil, ErrBackendUnavailable
	}
	pg, err := r.client.GetPortGroup(ctx, r.name)
	if err != nil {
		return nil, err
	}
	policy := securityPolicyFromPortGroup(pg)
	acls, err := r.client.selectACLsByUUID(ctx, pg.ACLs, r.name)
	if err != nil {
		return nil, err
	}
	for _, acl := range acls {
		if acl.ExternalIDs[ExternalIDKindKey] != "SecurityPolicy" || acl.ExternalIDs[ExternalIDNameKey] != r.name {
			continue
		}
		policy.Rules = append(policy.Rules, securityRuleFromACL(acl))
	}
	*policy = normalizeSecurityPolicy(*policy)
	return policy, nil
}

func (r *SecurityPolicyRef) Inspect(ctx context.Context) (InspectResult, error) {
	policy, err := r.Get(ctx)
	if err != nil {
		return InspectResult{}, err
	}
	return InspectResult{Resource: "SecurityPolicy", Name: r.name, Status: policy}, nil
}

func (r *SecurityPolicyRef) Delete(ctx context.Context) error {
	if err := validateName("security policy", r.name); err != nil {
		return err
	}
	if r.client == nil || r.client.db == nil {
		return ErrBackendUnavailable
	}
	return r.client.PortGroup(r.name).Delete().Execute(ctx)
}

func (r *SecurityPolicyRef) Apply(ctx context.Context, policy SecurityPolicy) error {
	if r.client == nil || r.client.db == nil {
		return ErrBackendUnavailable
	}
	if policy.Name == "" {
		policy.Name = r.name
	}
	if policy.Name != r.name {
		return wrap(ErrorConflict, dbOVNNorthbound, tablePortGroup, "apply", policy.Name, "security policy name does not match reference", nil)
	}
	builder := &SecurityPolicyBuilder{ref: r, spec: policy}
	_, err := builder.Reconcile(ctx)
	return err
}

func (r *SecurityPolicyRef) Patch(ctx context.Context, patch SecurityPolicyPatch) (*SecurityPolicy, error) {
	current, err := r.Get(ctx)
	if err != nil {
		return nil, err
	}
	next := cloneSecurityPolicy(current)
	if patch.Subject != nil {
		next.Subject = *patch.Subject
	}
	if patch.ReplaceRules {
		next.Rules = cloneSecurityRules(patch.Rules)
	}
	next.Rules = append(next.Rules, cloneSecurityRules(patch.AddRules)...)
	next.Rules = removeSecurityRules(next.Rules, patch.RemoveRules)
	if patch.Owner != nil {
		next.Owner = *patch.Owner
	}
	next.Labels = patchLabels(next.Labels, patch.Labels, patch.RemoveLabels)
	if err := r.Apply(ctx, next); err != nil {
		return nil, err
	}
	removeKeys := labelDeleteKeys(patch.RemoveLabels, patch.Labels)
	if patch.Subject != nil && *patch.Subject == "" {
		removeKeys = append(removeKeys, ExternalIDPrefix+"subject")
	}
	if err := r.client.deleteExternalIDKeys(ctx, tablePortGroup, r.name, conditionName(r.name), removeKeys); err != nil {
		return nil, err
	}
	return &next, nil
}

type SecurityPolicyBuilder struct {
	ref  *SecurityPolicyRef
	spec SecurityPolicy
}

func (b *SecurityPolicyBuilder) ForSubject(subject string) *SecurityPolicyBuilder {
	b.spec.Subject = subject
	return b
}

func (b *SecurityPolicyBuilder) AddRule(rule SecurityRule) *SecurityPolicyBuilder {
	b.spec.Rules = append(b.spec.Rules, rule)
	return b
}

func (b *SecurityPolicyBuilder) WithOwner(kind, name string) *SecurityPolicyBuilder {
	b.spec.Owner = OwnerRef{Kind: kind, Name: name}
	return b
}

func (b *SecurityPolicyBuilder) WithLabel(key, value string) *SecurityPolicyBuilder {
	if b.spec.Labels == nil {
		b.spec.Labels = Labels{}
	}
	b.spec.Labels[key] = value
	return b
}

func (b *SecurityPolicyBuilder) Validate() error {
	return b.spec.Validate()
}

func (b *SecurityPolicyBuilder) Plan(ctx context.Context) (Plan, error) {
	if err := b.Validate(); err != nil {
		return Plan{}, err
	}
	return Plan{Operations: []PlannedOperation{{
		Action:      IntentActionEnsure,
		Resource:    "SecurityPolicy",
		Name:        b.spec.Name,
		Description: "validate and plan ACL or group policy intent",
	}}}, nil
}

func (b *SecurityPolicyBuilder) DryRun(ctx context.Context) (DryRunResult, error) {
	plan, err := b.Plan(ctx)
	if err != nil {
		return DryRunResult{}, err
	}
	diff, err := b.diff(ctx)
	if err != nil {
		return DryRunResult{}, err
	}
	return DryRunResult{Plan: plan, Diff: diff}, nil
}

func (b *SecurityPolicyBuilder) Reconcile(ctx context.Context) (ReconcileResult, error) {
	plan, err := b.Plan(ctx)
	if err != nil {
		return ReconcileResult{}, err
	}
	diff, err := b.diff(ctx)
	if err != nil {
		return ReconcileResult{}, err
	}
	if b.ref == nil || b.ref.client == nil || b.ref.client.db == nil {
		return ReconcileResult{Plan: plan, Diff: diff, Applied: false}, nil
	}
	if diff.Empty() {
		return ReconcileResult{Plan: plan, Diff: diff, Applied: false}, nil
	}
	if err := b.reconcileOVSDB(ctx); err != nil {
		return ReconcileResult{}, err
	}
	return ReconcileResult{Plan: plan, Diff: diff, Applied: !diff.Empty()}, nil
}

func (b *SecurityPolicyBuilder) Execute(ctx context.Context) error {
	_, err := b.Reconcile(ctx)
	return err
}

func (v VirtualNetwork) Validate() error {
	if err := validateName("virtual network", v.Name); err != nil {
		return err
	}
	for _, cidr := range v.CIDRs {
		if _, _, err := net.ParseCIDR(cidr); err != nil {
			return wrap(ErrorValidation, dbOVNNorthbound, tableLogicalSwitch, "validate", v.Name, "invalid cidr", err)
		}
	}
	if v.Gateway != "" && net.ParseIP(v.Gateway) == nil {
		return wrap(ErrorValidation, dbOVNNorthbound, tableLogicalSwitch, "validate", v.Name, "invalid gateway", nil)
	}
	if v.Owner.Kind != "" || v.Owner.Name != "" || v.Owner.ID != "" {
		if err := v.Owner.Validate(); err != nil {
			return err
		}
	}
	if err := v.Labels.Validate(); err != nil {
		return err
	}
	return v.DNS.Validate()
}

func (d LogicalSwitchDNS) Validate() error {
	for _, record := range d.Records {
		if strings.TrimSpace(record.Domain) == "" {
			return wrap(ErrorValidation, dbOVNNorthbound, tableDNS, "validate", d.Name, "dns domain must not be empty", nil)
		}
		for _, ip := range record.IPs {
			if net.ParseIP(ip) == nil {
				return wrap(ErrorValidation, dbOVNNorthbound, tableDNS, "validate", record.Domain, "invalid dns record ip", nil)
			}
		}
	}
	if d.Owner.Kind != "" || d.Owner.Name != "" || d.Owner.ID != "" {
		if err := d.Owner.Validate(); err != nil {
			return err
		}
	}
	return d.Labels.Validate()
}

func (w WorkloadAttachment) Validate() error {
	if err := validateName("workload attachment", w.Name); err != nil {
		return err
	}
	if err := validateName("virtual network", w.Network); err != nil {
		return err
	}
	if w.MAC != "" {
		if _, err := net.ParseMAC(w.MAC); err != nil {
			return wrap(ErrorValidation, dbOVNNorthbound, tableLogicalSwitchPort, "validate", w.Name, "invalid mac", err)
		}
	}
	for _, ip := range w.IPs {
		if net.ParseIP(ip) == nil {
			return wrap(ErrorValidation, dbOVNNorthbound, tableLogicalSwitchPort, "validate", w.Name, "invalid ip", nil)
		}
	}
	if w.Owner.Kind != "" || w.Owner.Name != "" || w.Owner.ID != "" {
		if err := w.Owner.Validate(); err != nil {
			return err
		}
	}
	if err := w.LocalOVS.Validate(); err != nil {
		return err
	}
	return w.Labels.Validate()
}

func (o WorkloadLocalOVS) Validate() error {
	if o.Empty() {
		return nil
	}
	if err := validateName("bridge", o.Bridge); err != nil {
		return err
	}
	if o.PortName != "" {
		if err := validateName("port", o.PortName); err != nil {
			return err
		}
	}
	if o.InterfaceName != "" {
		if err := validateName("interface", o.InterfaceName); err != nil {
			return err
		}
	}
	for key := range o.Options {
		if err := validateExternalID(key); err != nil {
			return err
		}
	}
	return nil
}

func (o WorkloadLocalOVS) Empty() bool {
	return o.Bridge == "" && o.PortName == "" && o.InterfaceName == "" && o.InterfaceType == "" && len(o.Options) == 0
}

func (p ProviderNetwork) Validate() error {
	if err := validateName("provider network", p.Name); err != nil {
		return err
	}
	if err := validateName("physical network", p.PhysicalNetwork); err != nil {
		return err
	}
	if err := validateName("logical switch", p.LogicalSwitch); err != nil {
		return err
	}
	if err := validateName("localnet port", p.LocalnetPort); err != nil {
		return err
	}
	if err := validateName("bridge", p.Bridge); err != nil {
		return err
	}
	if strings.ContainsAny(p.PhysicalNetwork, ",:") || strings.ContainsAny(p.Bridge, ",:") {
		return wrap(ErrorValidation, dbOpenVSwitch, tableOpenVSwitch, "validate", p.Name, "bridge mapping values must not contain comma or colon", nil)
	}
	if p.Owner.Kind != "" || p.Owner.Name != "" || p.Owner.ID != "" {
		if err := p.Owner.Validate(); err != nil {
			return err
		}
	}
	return p.Labels.Validate()
}

func (p SecurityPolicy) Validate() error {
	if err := validateName("security policy", p.Name); err != nil {
		return err
	}
	for _, rule := range p.Rules {
		if strings.TrimSpace(rule.Action) == "" {
			return wrap(ErrorValidation, dbOVNNorthbound, tableACL, "validate", p.Name, "security rule action is required", nil)
		}
		for _, cidr := range rule.CIDRs {
			if _, _, err := net.ParseCIDR(cidr); err != nil {
				return wrap(ErrorValidation, dbOVNNorthbound, tableACL, "validate", p.Name, "invalid security rule cidr", err)
			}
		}
	}
	if p.Owner.Kind != "" || p.Owner.Name != "" || p.Owner.ID != "" {
		if err := p.Owner.Validate(); err != nil {
			return err
		}
	}
	return p.Labels.Validate()
}

func (b *VirtualNetworkBuilder) reconcileOVSDB(ctx context.Context) error {
	externalIDs, err := intentExternalIDs("VirtualNetwork", b.spec.Name, b.spec.Owner, b.spec.Labels)
	if err != nil {
		return err
	}
	builder := b.ref.client.LogicalSwitch(b.spec.Name).Ensure()
	for _, cidr := range b.spec.CIDRs {
		builder.WithSubnet(cidr)
		break
	}
	for key, value := range externalIDs {
		builder.WithExternalID(key, value)
	}
	if err := builder.Execute(ctx); err != nil {
		return err
	}
	if len(b.spec.DNS.Records) > 0 {
		dns := &LogicalSwitchDNSBuilder{
			ref:  &LogicalSwitchDNSRef{client: b.ref.client, name: b.spec.DNS.Name},
			spec: b.spec.DNS,
		}
		if dns.spec.Name == "" {
			dns.spec.Name = b.spec.Name
		}
		if err := dns.reconcileOVSDBWithOwner(ctx, b.spec.Owner, b.spec.Labels); err != nil {
			return err
		}
	}
	ops, err := b.logicalSwitchPatchOps(ctx, b.spec.Gateway, nil, nil)
	if err != nil {
		return err
	}
	if len(ops) > 0 {
		results, err := b.ref.client.db.transact(ctx, tableLogicalSwitch, "patch", b.spec.Name, ops...)
		if err != nil {
			return err
		}
		if err := ensureAffected(results, mustAffectNonInsertOps(ops), dbOVNNorthbound, tableLogicalSwitch, "patch", b.spec.Name); err != nil {
			return err
		}
	}
	return nil
}

func (b *VirtualNetworkBuilder) logicalSwitchPatchOps(ctx context.Context, gateway string, removeOtherConfig []string, removeExternalIDs []string) ([]libovsdb.Operation, error) {
	rows, err := b.ref.client.selectLogicalSwitches(ctx, b.spec.Name)
	if err != nil {
		if IsKind(err, ErrorNotFound) {
			return nil, nil
		}
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	var mutations []libovsdb.Mutation
	if gateway != "" {
		mutations = append(mutations, *libovsdb.NewMutation(colOtherConfig, libovsdb.MutateOperationInsert, ovsMap(map[string]string{"gateway": gateway})))
	}
	if len(removeOtherConfig) > 0 {
		mutations = append(mutations, *libovsdb.NewMutation(colOtherConfig, libovsdb.MutateOperationDelete, ovsMapKeys(removeOtherConfig)))
	}
	if len(removeExternalIDs) > 0 {
		mutations = append(mutations, *libovsdb.NewMutation(colExternalIDs, libovsdb.MutateOperationDelete, ovsMapKeys(removeExternalIDs)))
	}
	if len(mutations) == 0 {
		return nil, nil
	}
	return []libovsdb.Operation{{
		Op:        libovsdb.OperationMutate,
		Table:     tableLogicalSwitch,
		Where:     conditionUUID(rows[0].UUID),
		Mutations: mutations,
	}}, nil
}

func (n *NBClient) deleteExternalIDKeys(ctx context.Context, table, object string, where []libovsdb.Condition, keys []string) error {
	return n.deleteMapKeys(ctx, table, object, colExternalIDs, where, keys)
}

func (n *NBClient) deleteMapKeys(ctx context.Context, table, object, column string, where []libovsdb.Condition, keys []string) error {
	keys = uniqueStrings(keys)
	if len(keys) == 0 {
		return nil
	}
	rows, err := n.selectRows(ctx, table, where, []string{colUUID}, object)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	results, err := n.db.transact(ctx, table, "patch", object, libovsdb.Operation{
		Op:    libovsdb.OperationMutate,
		Table: table,
		Where: conditionUUID(rowUUIDValue(rows[0])),
		Mutations: []libovsdb.Mutation{
			*libovsdb.NewMutation(column, libovsdb.MutateOperationDelete, ovsMapKeys(keys)),
		},
	})
	if err != nil {
		return err
	}
	return ensureAffected(results, []int{0}, dbOVNNorthbound, table, "patch", object)
}

func (b *VirtualNetworkBuilder) diff(ctx context.Context) (Diff, error) {
	desired := normalizeVirtualNetwork(b.spec)
	diff := Diff{Resource: "VirtualNetwork", Name: desired.Name}
	current, found, err := b.current(ctx)
	if err != nil {
		return Diff{}, err
	}
	if !found {
		diff.Changes = append(diff.Changes, DiffChange{Path: "/", Before: nil, After: desired})
		return diff, nil
	}
	appendFieldDiff(&diff, "cidrs", current.CIDRs, desired.CIDRs)
	appendFieldDiff(&diff, "gateway", current.Gateway, desired.Gateway)
	appendFieldDiff(&diff, "owner", current.Owner, desired.Owner)
	appendFieldDiff(&diff, "labels", current.Labels, desired.Labels)
	if desired.DNS.Name != "" || len(desired.DNS.Records) > 0 {
		appendFieldDiff(&diff, "dns", normalizeLogicalSwitchDNS(current.DNS), normalizeLogicalSwitchDNS(desired.DNS))
	}
	return diff, nil
}

func (b *VirtualNetworkBuilder) current(ctx context.Context) (VirtualNetwork, bool, error) {
	if b.ref == nil || b.ref.client == nil || b.ref.client.db == nil {
		return VirtualNetwork{}, false, nil
	}
	current, err := b.ref.Get(ctx)
	if err != nil {
		if IsKind(err, ErrorNotFound) {
			return VirtualNetwork{}, false, nil
		}
		return VirtualNetwork{}, false, err
	}
	return normalizeVirtualNetwork(*current), true, nil
}

func (b *LogicalSwitchDNSBuilder) reconcileOVSDB(ctx context.Context) error {
	return b.reconcileOVSDBWithOwner(ctx, b.spec.Owner, b.spec.Labels)
}

func (b *LogicalSwitchDNSBuilder) reconcileOVSDBWithOwner(ctx context.Context, owner OwnerRef, labels Labels) error {
	externalIDs, err := intentExternalIDs("LogicalSwitchDNS", b.spec.Name, owner, labels)
	if err != nil {
		return err
	}
	dns := b.ref.client.DNS(b.spec.Name).Ensure()
	for domain, ips := range b.spec.RecordMap() {
		dns.WithRecord(domain, strings.Join(ips, " "))
	}
	for key, value := range externalIDs {
		dns.WithExternalID(key, value)
	}
	return dns.Execute(ctx)
}

func (b *LogicalSwitchDNSBuilder) diff(ctx context.Context) (Diff, error) {
	desired := normalizeLogicalSwitchDNS(b.spec)
	diff := Diff{Resource: "LogicalSwitchDNS", Name: desired.Name}
	current, found, err := b.current(ctx)
	if err != nil {
		return Diff{}, err
	}
	if !found {
		diff.Changes = append(diff.Changes, DiffChange{Path: "/", Before: nil, After: desired})
		return diff, nil
	}
	appendFieldDiff(&diff, "records", dnsRecordComparable(current.Records), dnsRecordComparable(desired.Records))
	appendFieldDiff(&diff, "owner", current.Owner, desired.Owner)
	appendFieldDiff(&diff, "labels", current.Labels, desired.Labels)
	return diff, nil
}

func (b *LogicalSwitchDNSBuilder) current(ctx context.Context) (LogicalSwitchDNS, bool, error) {
	if b.ref == nil || b.ref.client == nil || b.ref.client.db == nil {
		return LogicalSwitchDNS{}, false, nil
	}
	current, err := b.ref.Get(ctx)
	if err != nil {
		if IsKind(err, ErrorNotFound) {
			return LogicalSwitchDNS{}, false, nil
		}
		return LogicalSwitchDNS{}, false, err
	}
	return normalizeLogicalSwitchDNS(*current), true, nil
}

func (b *WorkloadAttachmentBuilder) reconcileOVSDB(ctx context.Context) error {
	externalIDs, err := intentExternalIDs("WorkloadAttachment", b.spec.Name, b.spec.Owner, b.spec.Labels)
	if err != nil {
		return err
	}
	if b.spec.Workload != "" {
		externalIDs[ExternalIDPrefix+"workload"] = b.spec.Workload
	}
	if b.spec.InterfaceName != "" {
		externalIDs[ExternalIDPrefix+"interface"] = b.spec.InterfaceName
	}
	if b.spec.Network != "" {
		externalIDs[ExternalIDPrefix+"network"] = b.spec.Network
	}
	port := b.ref.client.LogicalSwitch(b.spec.Network).Ensure().AddPort(b.spec.Name)
	if b.spec.MAC != "" && len(b.spec.IPs) == 1 {
		port.WithAddress(b.spec.MAC, b.spec.IPs[0])
	} else {
		if b.spec.MAC != "" {
			port.WithMac(b.spec.MAC)
		}
		for _, ip := range b.spec.IPs {
			if b.spec.MAC != "" {
				port.WithAddress(b.spec.MAC, ip)
			}
		}
	}
	for key, value := range externalIDs {
		port.WithExternalID(key, value)
	}
	return port.Execute(ctx)
}

func (b *WorkloadAttachmentBuilder) reconcileLocalOVS(ctx context.Context) error {
	if b.spec.LocalOVS.Empty() {
		return nil
	}
	externalIDs, err := intentExternalIDs("WorkloadAttachment", b.spec.Name, b.spec.Owner, b.spec.Labels)
	if err != nil {
		return err
	}
	externalIDs[ExternalIDPrefix+"network"] = b.spec.Network
	if b.spec.Workload != "" {
		externalIDs[ExternalIDPrefix+"workload"] = b.spec.Workload
	}
	if b.spec.InterfaceName != "" {
		externalIDs[ExternalIDPrefix+"logical-interface"] = b.spec.InterfaceName
	}
	portName := b.localOVSPortName()
	interfaceName := b.localOVSInterfaceName()
	if err := b.validateLocalOVSTarget(ctx); err != nil {
		return err
	}
	port := b.ref.ovs.Bridge(b.spec.LocalOVS.Bridge).Ensure().AddPort(portName).
		WithInterfaceName(interfaceName).
		WithInterfaceExternalID("iface-id", b.spec.Name)
	for key, value := range externalIDs {
		port.WithExternalID(key, value)
		port.WithInterfaceExternalID(key, value)
	}
	if b.spec.LocalOVS.InterfaceType != "" {
		port.WithInterfaceType(b.spec.LocalOVS.InterfaceType)
	}
	for key, value := range b.spec.LocalOVS.Options {
		port.WithInterfaceOption(key, value)
	}
	return port.Execute(ctx)
}

func (b *WorkloadAttachmentBuilder) validateLocalOVSTarget(ctx context.Context) error {
	if b.spec.LocalOVS.Empty() {
		return nil
	}
	if b.ref == nil || b.ref.ovs == nil || b.ref.ovs.db == nil {
		return ErrBackendUnavailable
	}
	portName := b.localOVSPortName()
	interfaceName := b.localOVSInterfaceName()
	if _, err := b.ref.ovs.GetBridge(ctx, b.spec.LocalOVS.Bridge); err != nil {
		return err
	}
	existingPorts, err := b.ref.ovs.selectPorts(ctx, portName)
	if err != nil {
		return err
	}
	if len(existingPorts) > 0 && !ovsResourceOwnedBy(existingPorts[0].ExternalIDs, "WorkloadAttachment", b.spec.Name) {
		return wrap(ErrorOwnershipViolation, dbOpenVSwitch, tablePort, "ensure", portName, "port is already managed by another owner", nil)
	}
	iface, err := b.ref.ovs.GetInterface(ctx, interfaceName)
	if err != nil {
		if IsKind(err, ErrorNotFound) {
			return nil
		}
		return err
	}
	if !ovsResourceOwnedBy(iface.ExternalIDs, "WorkloadAttachment", b.spec.Name) {
		return wrap(ErrorOwnershipViolation, dbOpenVSwitch, tableInterface, "ensure", interfaceName, "interface is already managed by another owner", nil)
	}
	if iface.ExternalIDs["iface-id"] != "" && iface.ExternalIDs["iface-id"] != b.spec.Name {
		return wrap(ErrorOwnershipViolation, dbOpenVSwitch, tableInterface, "ensure", interfaceName, "interface iface-id does not match workload attachment", nil)
	}
	return nil
}

func (b *WorkloadAttachmentBuilder) localOVSPortName() string {
	if b.spec.LocalOVS.PortName != "" {
		return b.spec.LocalOVS.PortName
	}
	return b.spec.Name
}

func (b *WorkloadAttachmentBuilder) localOVSInterfaceName() string {
	if b.spec.LocalOVS.InterfaceName != "" {
		return b.spec.LocalOVS.InterfaceName
	}
	return b.localOVSPortName()
}

func ovsResourceOwnedBy(externalIDs map[string]string, kind, name string) bool {
	return externalIDs[ExternalIDManagedByKey] == "ovnflow" && externalIDs[ExternalIDKindKey] == kind && externalIDs[ExternalIDNameKey] == name
}

func (b *WorkloadAttachmentBuilder) diff(ctx context.Context) (Diff, error) {
	desired := normalizeWorkloadAttachment(b.spec)
	diff := Diff{Resource: "WorkloadAttachment", Name: desired.Name}
	current, found, err := b.current(ctx)
	if err != nil {
		return Diff{}, err
	}
	if !found {
		diff.Changes = append(diff.Changes, DiffChange{Path: "/", Before: nil, After: desired})
		return diff, nil
	}
	appendFieldDiff(&diff, "network", current.Network, desired.Network)
	appendFieldDiff(&diff, "workload", current.Workload, desired.Workload)
	appendFieldDiff(&diff, "interface", current.InterfaceName, desired.InterfaceName)
	appendFieldDiff(&diff, "mac", current.MAC, desired.MAC)
	appendFieldDiff(&diff, "ips", current.IPs, desired.IPs)
	appendFieldDiff(&diff, "localOVS", current.LocalOVS, desired.LocalOVS)
	appendFieldDiff(&diff, "owner", current.Owner, desired.Owner)
	appendFieldDiff(&diff, "labels", current.Labels, desired.Labels)
	return diff, nil
}

func (b *WorkloadAttachmentBuilder) current(ctx context.Context) (WorkloadAttachment, bool, error) {
	if b.ref == nil || b.ref.client == nil || b.ref.client.db == nil {
		return WorkloadAttachment{}, false, nil
	}
	current, err := b.ref.Get(ctx)
	if err != nil {
		if IsKind(err, ErrorNotFound) {
			return WorkloadAttachment{}, false, nil
		}
		return WorkloadAttachment{}, false, err
	}
	return normalizeWorkloadAttachment(*current), true, nil
}

func (b *ProviderNetworkBuilder) validateTargets(ctx context.Context) error {
	desired := normalizeProviderNetwork(b.spec)
	if b.ref == nil || b.ref.client == nil || b.ref.client.db == nil || b.ref.ovs == nil || b.ref.ovs.db == nil {
		return ErrBackendUnavailable
	}
	if _, err := b.ref.ovs.GetBridge(ctx, desired.Bridge); err != nil {
		return err
	}
	mappings, err := b.ref.ovs.GetBridgeMappings(ctx)
	if err != nil {
		return err
	}
	root, err := b.ref.ovs.GetOpenVSwitch(ctx)
	if err != nil {
		return err
	}
	markerOwner := root.ExternalIDs[providerNetworkMappingOwnerKey(desired.PhysicalNetwork)]
	if current := mappings[desired.PhysicalNetwork]; current != "" {
		if markerOwner == "" {
			return wrap(ErrorOwnershipViolation, dbOpenVSwitch, tableOpenVSwitch, "ensure", desired.PhysicalNetwork, "bridge mapping is not managed by ovnflow", nil)
		}
		if markerOwner != desired.Name {
			return wrap(ErrorOwnershipViolation, dbOpenVSwitch, tableOpenVSwitch, "ensure", desired.PhysicalNetwork, "bridge mapping marker belongs to another provider network", nil)
		}
	}
	if markerOwner != "" && markerOwner != desired.Name {
		return wrap(ErrorOwnershipViolation, dbOpenVSwitch, tableOpenVSwitch, "ensure", desired.PhysicalNetwork, "bridge mapping marker belongs to another provider network", nil)
	}
	ports, err := b.ref.client.ListLogicalSwitchPorts(ctx)
	if err != nil {
		return err
	}
	for _, port := range ports {
		if port.Name != desired.LocalnetPort {
			continue
		}
		if !providerNetworkLocalnetOwnedBy(port.ExternalIDs, desired.Name) {
			return wrap(ErrorOwnershipViolation, dbOVNNorthbound, tableLogicalSwitchPort, "ensure", desired.LocalnetPort, "localnet port is already managed by another owner", nil)
		}
	}
	return nil
}

func (b *ProviderNetworkBuilder) reconcileOVSDB(ctx context.Context) error {
	desired := normalizeProviderNetwork(b.spec)
	externalIDs, err := intentExternalIDs("ProviderNetwork", desired.Name, desired.Owner, desired.Labels)
	if err != nil {
		return err
	}
	externalIDs[ExternalIDPrefix+"physical-network"] = desired.PhysicalNetwork
	externalIDs[ExternalIDPrefix+"logical-switch"] = desired.LogicalSwitch
	externalIDs[ExternalIDPrefix+"bridge"] = desired.Bridge
	if err := b.ref.client.LogicalSwitch(desired.LogicalSwitch).Ensure().
		WithExternalID(ExternalIDManagedByKey, "ovnflow").
		AddLocalnetPort(desired.LocalnetPort, desired.PhysicalNetwork).
		WithExternalID(ExternalIDKindKey, "ProviderNetwork").
		WithExternalID(ExternalIDNameKey, desired.Name).
		Execute(ctx); err != nil {
		return err
	}
	port := b.ref.client.TableLogicalSwitchPort(desired.LocalnetPort).Update().
		WithType("localnet").
		MutateSet(colAddresses, "unknown").
		MutateMap(colOptions, map[string]string{"network_name": desired.PhysicalNetwork})
	for key, value := range externalIDs {
		port.WithExternalID(key, value)
	}
	if err := port.Execute(ctx); err != nil {
		return err
	}
	return b.setOwnedBridgeMapping(ctx, desired)
}

func (b *ProviderNetworkBuilder) setOwnedBridgeMapping(ctx context.Context, desired ProviderNetwork) error {
	root, err := b.ref.ovs.GetOpenVSwitch(ctx)
	if err != nil {
		return err
	}
	mappings, err := ParseBridgeMappings(root.ExternalIDs[ovsBridgeMappingsKey])
	if err != nil {
		return err
	}
	markerOwner := root.ExternalIDs[providerNetworkMappingOwnerKey(desired.PhysicalNetwork)]
	if markerOwner != "" && markerOwner != desired.Name {
		return wrap(ErrorOwnershipViolation, dbOpenVSwitch, tableOpenVSwitch, "ensure", desired.PhysicalNetwork, "bridge mapping marker belongs to another provider network", nil)
	}
	if current := mappings[desired.PhysicalNetwork]; current != "" {
		if markerOwner == "" {
			return wrap(ErrorOwnershipViolation, dbOpenVSwitch, tableOpenVSwitch, "ensure", desired.PhysicalNetwork, "bridge mapping is not managed by ovnflow", nil)
		}
		if markerOwner != desired.Name {
			return wrap(ErrorOwnershipViolation, dbOpenVSwitch, tableOpenVSwitch, "ensure", desired.PhysicalNetwork, "bridge mapping marker belongs to another provider network", nil)
		}
	}
	mappings[desired.PhysicalNetwork] = desired.Bridge
	formatted := FormatBridgeMappings(mappings)
	values := map[string]string{
		providerNetworkMappingOwnerKey(desired.PhysicalNetwork): desired.Name,
	}
	if formatted != "" {
		values[ovsBridgeMappingsKey] = formatted
	}
	results, err := b.ref.ovs.db.executor.Transact(ctx, libovsdb.Operation{
		Op:    libovsdb.OperationMutate,
		Table: tableOpenVSwitch,
		Where: conditionUUID(root.UUID),
		Mutations: []libovsdb.Mutation{
			*libovsdb.NewMutation(colExternalIDs, libovsdb.MutateOperationDelete, ovsMapKeys([]string{ovsBridgeMappingsKey, providerNetworkMappingOwnerKey(desired.PhysicalNetwork)})),
			*libovsdb.NewMutation(colExternalIDs, libovsdb.MutateOperationInsert, ovsMap(map[string]string{
				providerNetworkMappingOwnerKey(desired.PhysicalNetwork): values[providerNetworkMappingOwnerKey(desired.PhysicalNetwork)],
				ovsBridgeMappingsKey: values[ovsBridgeMappingsKey],
			})),
		},
	})
	if err != nil {
		return classifyTransactError(err, dbOpenVSwitch, tableOpenVSwitch, "ensure", desired.PhysicalNetwork)
	}
	return ensureAffected(results, []int{0}, dbOpenVSwitch, tableOpenVSwitch, "ensure", desired.PhysicalNetwork)
}

func (b *ProviderNetworkBuilder) diff(ctx context.Context) (Diff, error) {
	desired := normalizeProviderNetwork(b.spec)
	diff := Diff{Resource: "ProviderNetwork", Name: desired.Name}
	current, found, err := b.current(ctx)
	if err != nil {
		return Diff{}, err
	}
	if !found {
		diff.Changes = append(diff.Changes, DiffChange{Path: "/", Before: nil, After: desired})
		return diff, nil
	}
	appendFieldDiff(&diff, "physicalNetwork", current.PhysicalNetwork, desired.PhysicalNetwork)
	appendFieldDiff(&diff, "logicalSwitch", current.LogicalSwitch, desired.LogicalSwitch)
	appendFieldDiff(&diff, "localnetPort", current.LocalnetPort, desired.LocalnetPort)
	appendFieldDiff(&diff, "bridge", current.Bridge, desired.Bridge)
	appendFieldDiff(&diff, "owner", current.Owner, desired.Owner)
	appendFieldDiff(&diff, "labels", current.Labels, desired.Labels)
	return diff, nil
}

func (b *ProviderNetworkBuilder) current(ctx context.Context) (ProviderNetwork, bool, error) {
	if b.ref == nil || b.ref.client == nil || b.ref.client.db == nil || b.ref.ovs == nil || b.ref.ovs.db == nil {
		return ProviderNetwork{}, false, nil
	}
	current, err := b.ref.Get(ctx)
	if err != nil {
		if IsKind(err, ErrorNotFound) {
			return ProviderNetwork{}, false, nil
		}
		return ProviderNetwork{}, false, err
	}
	return normalizeProviderNetwork(*current), true, nil
}

func (b *SecurityPolicyBuilder) reconcileOVSDB(ctx context.Context) error {
	externalIDs, err := intentExternalIDs("SecurityPolicy", b.spec.Name, b.spec.Owner, b.spec.Labels)
	if err != nil {
		return err
	}
	if b.spec.Subject != "" {
		externalIDs[ExternalIDPrefix+"subject"] = b.spec.Subject
	}
	if b.ref != nil && b.ref.client != nil && b.ref.client.db != nil {
		if preOps, err := b.replaceSecurityPolicyACLOps(ctx); err != nil {
			return err
		} else if len(preOps) > 0 {
			row := libovsdb.Row{colName: b.spec.Name}
			nbSetUUIDSet(row, colACLs, nil)
			setRowMap(row, colExternalIDs, externalIDs)
			var mutations []libovsdb.Mutation
			nbAppendMapMutation(&mutations, colExternalIDs, externalIDs)
			return b.ref.client.executeNamedWithPreOps(ctx, tablePortGroup, b.spec.Name, nbModeEnsure, row, mutations, preOps)
		}
	}
	pg := b.ref.client.PortGroup(b.spec.Name).Ensure()
	for key, value := range externalIDs {
		pg.WithExternalID(key, value)
	}
	for i, rule := range b.spec.Rules {
		pg.WithACL(ruleDirection(rule), rulePriority(rule), ruleMatch(b.spec.Subject, rule), ruleAction(rule))
		for key, value := range externalIDs {
			pg.WithACLExternalID(key, value)
		}
		pg.WithACLExternalID(ExternalIDPrefix+"rule-index", strconv.Itoa(i))
		if rule.Name != "" {
			pg.WithACLExternalID(ExternalIDPrefix+"rule-name", rule.Name)
		}
	}
	return pg.Execute(ctx)
}

func (b *SecurityPolicyBuilder) replaceSecurityPolicyACLOps(ctx context.Context) ([]libovsdb.Operation, error) {
	current, err := b.ref.client.GetPortGroup(ctx, b.spec.Name)
	if err != nil {
		if IsKind(err, ErrorNotFound) {
			return nil, nil
		}
		return nil, err
	}
	acls, err := b.ref.client.selectACLsByUUID(ctx, current.ACLs, b.spec.Name)
	if err != nil {
		return nil, err
	}
	var removeRefs []string
	var ops []libovsdb.Operation
	for _, acl := range acls {
		if acl.ExternalIDs[ExternalIDKindKey] != "SecurityPolicy" || acl.ExternalIDs[ExternalIDNameKey] != b.spec.Name {
			continue
		}
		removeRefs = append(removeRefs, acl.UUID)
		ops = append(ops, libovsdb.Operation{
			Op:    libovsdb.OperationDelete,
			Table: tableACL,
			Where: conditionUUID(acl.UUID),
		})
	}
	if len(removeRefs) > 0 {
		ops = append([]libovsdb.Operation{{
			Op:    libovsdb.OperationMutate,
			Table: tablePortGroup,
			Where: conditionUUID(current.UUID),
			Mutations: []libovsdb.Mutation{
				*libovsdb.NewMutation(colACLs, libovsdb.MutateOperationDelete, uuidSet(removeRefs...)),
			},
		}}, ops...)
	}
	for i, rule := range b.spec.Rules {
		externalIDs, err := intentExternalIDs("SecurityPolicy", b.spec.Name, b.spec.Owner, b.spec.Labels)
		if err != nil {
			return nil, err
		}
		if b.spec.Subject != "" {
			externalIDs[ExternalIDPrefix+"subject"] = b.spec.Subject
		}
		externalIDs[ExternalIDPrefix+"rule-index"] = strconv.Itoa(i)
		if rule.Name != "" {
			externalIDs[ExternalIDPrefix+"rule-name"] = rule.Name
		}
		aclUUID := namedUUID("acl")
		ops = append(ops, libovsdb.Operation{
			Op:       libovsdb.OperationInsert,
			Table:    tableACL,
			UUIDName: aclUUID,
			Row: inlineACLRow(inlineACLSpec{
				direction:   ruleDirection(rule),
				priority:    rulePriority(rule),
				match:       ruleMatch(b.spec.Subject, rule),
				action:      ruleAction(rule),
				externalIDs: externalIDs,
			}),
		})
		ops = append(ops, libovsdb.Operation{
			Op:    libovsdb.OperationMutate,
			Table: tablePortGroup,
			Where: conditionUUID(current.UUID),
			Mutations: []libovsdb.Mutation{
				*libovsdb.NewMutation(colACLs, libovsdb.MutateOperationInsert, uuidSet(aclUUID)),
			},
		})
	}
	return ops, nil
}

func (b *SecurityPolicyBuilder) diff(ctx context.Context) (Diff, error) {
	desired := normalizeSecurityPolicy(b.spec)
	diff := Diff{Resource: "SecurityPolicy", Name: desired.Name}
	current, found, err := b.current(ctx)
	if err != nil {
		return Diff{}, err
	}
	if !found {
		diff.Changes = append(diff.Changes, DiffChange{Path: "/", Before: nil, After: desired})
		return diff, nil
	}
	appendFieldDiff(&diff, "subject", current.Subject, desired.Subject)
	appendFieldDiff(&diff, "rules", current.Rules, desired.Rules)
	appendFieldDiff(&diff, "owner", current.Owner, desired.Owner)
	appendFieldDiff(&diff, "labels", current.Labels, desired.Labels)
	return diff, nil
}

func (b *SecurityPolicyBuilder) current(ctx context.Context) (SecurityPolicy, bool, error) {
	if b.ref == nil || b.ref.client == nil || b.ref.client.db == nil {
		return SecurityPolicy{}, false, nil
	}
	current, err := b.ref.Get(ctx)
	if err != nil {
		if IsKind(err, ErrorNotFound) {
			return SecurityPolicy{}, false, nil
		}
		return SecurityPolicy{}, false, err
	}
	return normalizeSecurityPolicy(*current), true, nil
}

func virtualNetworkFromLogicalSwitch(ls *LogicalSwitch) *VirtualNetwork {
	if ls == nil {
		return nil
	}
	owner, labels := ownerAndLabelsFromExternalIDs(ls.ExternalIDs)
	vn := &VirtualNetwork{
		Name:   ls.Name,
		Owner:  owner,
		Labels: labels,
	}
	if subnet := ls.OtherConfig["subnet"]; subnet != "" {
		vn.CIDRs = []string{subnet}
	}
	if gateway := ls.OtherConfig["gateway"]; gateway != "" {
		vn.Gateway = gateway
	}
	return vn
}

func logicalSwitchDNSFromDNS(name string, dns *DNS) *LogicalSwitchDNS {
	if dns == nil {
		return nil
	}
	owner, labels := ownerAndLabelsFromExternalIDs(dns.ExternalIDs)
	out := &LogicalSwitchDNS{
		Name:    name,
		Owner:   owner,
		Labels:  labels,
		Records: make([]DNSRecord, 0, len(dns.Records)),
	}
	for domain, value := range dns.Records {
		out.Records = append(out.Records, DNSRecord{Domain: domain, IPs: strings.Fields(value)})
	}
	sort.Slice(out.Records, func(i, j int) bool { return out.Records[i].Domain < out.Records[j].Domain })
	return out
}

func workloadAttachmentFromLogicalSwitchPort(lsp *LogicalSwitchPort) *WorkloadAttachment {
	if lsp == nil {
		return nil
	}
	owner, labels := ownerAndLabelsFromExternalIDs(lsp.ExternalIDs)
	out := &WorkloadAttachment{
		Name:          lsp.Name,
		Network:       lsp.ExternalIDs[ExternalIDPrefix+"network"],
		Workload:      lsp.ExternalIDs[ExternalIDPrefix+"workload"],
		InterfaceName: lsp.ExternalIDs[ExternalIDPrefix+"interface"],
		Owner:         owner,
		Labels:        labels,
	}
	for _, address := range lsp.Addresses {
		fields := strings.Fields(address)
		if len(fields) > 0 && out.MAC == "" {
			out.MAC = fields[0]
		}
		if len(fields) > 1 {
			out.IPs = append(out.IPs, fields[1:]...)
		}
	}
	return out
}

func providerNetworkFromLocalnetPort(lsp *LogicalSwitchPort) *ProviderNetwork {
	if lsp == nil {
		return nil
	}
	owner, labels := ownerAndLabelsFromExternalIDs(lsp.ExternalIDs)
	network := &ProviderNetwork{
		Name:            lsp.ExternalIDs[ExternalIDNameKey],
		PhysicalNetwork: lsp.Options["network_name"],
		LogicalSwitch:   lsp.ExternalIDs[ExternalIDPrefix+"logical-switch"],
		LocalnetPort:    lsp.Name,
		Bridge:          lsp.ExternalIDs[ExternalIDPrefix+"bridge"],
		Owner:           owner,
		Labels:          labels,
	}
	if network.PhysicalNetwork == "" {
		network.PhysicalNetwork = lsp.ExternalIDs[ExternalIDPrefix+"physical-network"]
	}
	return network
}

func securityPolicyFromPortGroup(pg *PortGroup) *SecurityPolicy {
	if pg == nil {
		return nil
	}
	owner, labels := ownerAndLabelsFromExternalIDs(pg.ExternalIDs)
	return &SecurityPolicy{
		Name:    pg.Name,
		Subject: pg.ExternalIDs[ExternalIDPrefix+"subject"],
		Owner:   owner,
		Labels:  labels,
	}
}

func (n *NBClient) selectACLsByUUID(ctx context.Context, ids []string, object string) ([]ACL, error) {
	var out []ACL
	for _, id := range uniqueStrings(ids) {
		rows, err := n.selectRows(ctx, tableACL, conditionUUID(id), nbACLColumns(), object)
		if err != nil {
			return nil, err
		}
		if len(rows) == 0 {
			continue
		}
		out = append(out, *aclFromRow(rows[0]))
	}
	return out, nil
}

func securityRuleFromACL(acl ACL) SecurityRule {
	rule := SecurityRule{
		Name:      acl.ExternalIDs[ExternalIDPrefix+"rule-name"],
		Action:    acl.Action,
		Direction: acl.Direction,
		Protocol:  protocolFromACLMatch(acl.Match),
		CIDRs:     cidrsFromACLMatch(acl.Match),
		Ports:     portsFromACLMatch(acl.Match),
	}
	if strings.Contains(acl.Match, "ct.est") {
		rule.Established = true
	}
	return rule
}

func protocolFromACLMatch(match string) string {
	for _, protocol := range []string{"tcp", "udp", "icmp"} {
		for _, clause := range strings.Split(match, "&&") {
			if strings.TrimSpace(clause) == protocol {
				return protocol
			}
		}
	}
	return ""
}

func cidrsFromACLMatch(match string) []string {
	var out []string
	for _, clause := range strings.Split(match, "&&") {
		clause = strings.TrimSpace(clause)
		if strings.HasPrefix(clause, "ip4.src == ") {
			out = append(out, strings.TrimPrefix(clause, "ip4.src == "))
		}
	}
	return out
}

func portsFromACLMatch(match string) []int {
	var out []int
	for _, clause := range strings.Split(match, "&&") {
		clause = strings.TrimSpace(clause)
		if !strings.HasPrefix(clause, "tcp.dst == ") {
			continue
		}
		port, err := strconv.Atoi(strings.TrimPrefix(clause, "tcp.dst == "))
		if err == nil {
			out = append(out, port)
		}
	}
	return out
}

func ownerAndLabelsFromExternalIDs(externalIDs map[string]string) (OwnerRef, Labels) {
	owner := OwnerRef{
		Kind: externalIDs[ExternalIDOwnerKindKey],
		Name: externalIDs[ExternalIDOwnerNameKey],
		ID:   externalIDs[ExternalIDOwnerIDKey],
	}
	labels := Labels{}
	for key, value := range externalIDs {
		if label, ok := DecodeExternalIDLabelKey(key); ok {
			labels[label] = value
		}
	}
	if len(labels) == 0 {
		labels = nil
	}
	return owner, labels
}

func ovsMapKeys(keys []string) libovsdb.OvsSet {
	return stringSet(uniqueStrings(keys))
}

func appendFieldDiff(diff *Diff, path string, before, after any) {
	if reflect.DeepEqual(before, after) {
		return
	}
	diff.Changes = append(diff.Changes, DiffChange{Path: path, Before: before, After: after})
}

func patchLabels(current Labels, add Labels, remove []string) Labels {
	out := cloneLabels(current)
	for _, key := range remove {
		delete(out, key)
	}
	for key, value := range add {
		if out == nil {
			out = Labels{}
		}
		out[key] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func cloneLabels(in Labels) Labels {
	if len(in) == 0 {
		return nil
	}
	out := Labels{}
	for key, value := range in {
		out[key] = value
	}
	return out
}

func cloneVirtualNetwork(in *VirtualNetwork) VirtualNetwork {
	if in == nil {
		return VirtualNetwork{}
	}
	out := *in
	out.CIDRs = append([]string{}, in.CIDRs...)
	out.DNS = cloneLogicalSwitchDNS(&in.DNS)
	out.Labels = cloneLabels(in.Labels)
	return out
}

func cloneLogicalSwitchDNS(in *LogicalSwitchDNS) LogicalSwitchDNS {
	if in == nil {
		return LogicalSwitchDNS{}
	}
	out := *in
	out.Records = cloneDNSRecords(in.Records)
	out.Labels = cloneLabels(in.Labels)
	return out
}

func cloneWorkloadAttachment(in *WorkloadAttachment) WorkloadAttachment {
	if in == nil {
		return WorkloadAttachment{}
	}
	out := *in
	out.IPs = append([]string{}, in.IPs...)
	out.LocalOVS = cloneWorkloadLocalOVS(in.LocalOVS)
	out.Labels = cloneLabels(in.Labels)
	return out
}

func cloneProviderNetwork(in *ProviderNetwork) ProviderNetwork {
	if in == nil {
		return ProviderNetwork{}
	}
	out := *in
	out.Labels = cloneLabels(in.Labels)
	return out
}

func cloneWorkloadLocalOVS(in WorkloadLocalOVS) WorkloadLocalOVS {
	out := in
	out.Options = cloneStringMap(in.Options)
	return out
}

func cloneSecurityPolicy(in *SecurityPolicy) SecurityPolicy {
	if in == nil {
		return SecurityPolicy{}
	}
	out := *in
	out.Rules = cloneSecurityRules(in.Rules)
	out.Labels = cloneLabels(in.Labels)
	return out
}

func cloneDNSRecords(in []DNSRecord) []DNSRecord {
	if len(in) == 0 {
		return nil
	}
	out := make([]DNSRecord, len(in))
	for i := range in {
		out[i] = DNSRecord{Domain: in[i].Domain, IPs: append([]string{}, in[i].IPs...)}
	}
	return out
}

func cloneSecurityRules(in []SecurityRule) []SecurityRule {
	if len(in) == 0 {
		return nil
	}
	out := make([]SecurityRule, len(in))
	for i := range in {
		out[i] = in[i]
		out[i].CIDRs = append([]string{}, in[i].CIDRs...)
		out[i].Ports = append([]int{}, in[i].Ports...)
	}
	return out
}

func normalizeVirtualNetwork(in VirtualNetwork) VirtualNetwork {
	out := cloneVirtualNetwork(&in)
	out.CIDRs = uniqueStrings(out.CIDRs)
	sort.Strings(out.CIDRs)
	out.Labels = normalizeLabels(out.Labels)
	out.DNS = normalizeLogicalSwitchDNS(out.DNS)
	return out
}

func normalizeLogicalSwitchDNS(in LogicalSwitchDNS) LogicalSwitchDNS {
	out := cloneLogicalSwitchDNS(&in)
	out.Labels = normalizeLabels(out.Labels)
	recordMap := out.RecordMap()
	out.Records = make([]DNSRecord, 0, len(recordMap))
	for domain, ips := range recordMap {
		out.Records = append(out.Records, DNSRecord{Domain: domain, IPs: uniqueStrings(ips)})
	}
	sort.Slice(out.Records, func(i, j int) bool { return out.Records[i].Domain < out.Records[j].Domain })
	return out
}

func normalizeWorkloadAttachment(in WorkloadAttachment) WorkloadAttachment {
	out := cloneWorkloadAttachment(&in)
	out.IPs = uniqueStrings(out.IPs)
	sort.Strings(out.IPs)
	out.LocalOVS = normalizeWorkloadLocalOVS(out.LocalOVS, out.Name, out.InterfaceName)
	out.Labels = normalizeLabels(out.Labels)
	return out
}

func normalizeProviderNetwork(in ProviderNetwork) ProviderNetwork {
	out := cloneProviderNetwork(&in)
	if out.PhysicalNetwork == "" {
		out.PhysicalNetwork = out.Name
	}
	if out.LogicalSwitch == "" {
		out.LogicalSwitch = out.Name
	}
	if out.LocalnetPort == "" {
		out.LocalnetPort = out.Name + "-localnet"
	}
	out.Labels = normalizeLabels(out.Labels)
	return out
}

func normalizeWorkloadLocalOVS(in WorkloadLocalOVS, attachmentName, logicalInterface string) WorkloadLocalOVS {
	if in.Empty() {
		return WorkloadLocalOVS{}
	}
	out := cloneWorkloadLocalOVS(in)
	if out.PortName == "" {
		out.PortName = attachmentName
	}
	if out.InterfaceName == "" {
		out.InterfaceName = out.PortName
	}
	if len(out.Options) == 0 {
		out.Options = nil
	}
	return out
}

func normalizeSecurityPolicy(in SecurityPolicy) SecurityPolicy {
	out := cloneSecurityPolicy(&in)
	out.Labels = normalizeLabels(out.Labels)
	for i := range out.Rules {
		out.Rules[i].CIDRs = uniqueStrings(out.Rules[i].CIDRs)
		sort.Strings(out.Rules[i].CIDRs)
		sort.Ints(out.Rules[i].Ports)
	}
	sort.Slice(out.Rules, func(i, j int) bool {
		return securityRuleKey(out.Rules[i]) < securityRuleKey(out.Rules[j])
	})
	return out
}

func normalizeLabels(in Labels) Labels {
	if len(in) == 0 {
		return nil
	}
	return cloneLabels(in)
}

func dnsRecordComparable(records []DNSRecord) map[string][]string {
	out := map[string][]string{}
	for _, record := range records {
		out[record.Domain] = append([]string{}, record.IPs...)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func mergeDNSRecords(current, add []DNSRecord) []DNSRecord {
	if len(add) == 0 {
		return cloneDNSRecords(current)
	}
	out := cloneDNSRecords(current)
	out = append(out, cloneDNSRecords(add)...)
	return normalizeLogicalSwitchDNS(LogicalSwitchDNS{Records: out}).Records
}

func removeDNSRecordDomains(current []DNSRecord, remove []string) []DNSRecord {
	if len(remove) == 0 {
		return cloneDNSRecords(current)
	}
	deny := map[string]struct{}{}
	for _, domain := range remove {
		deny[domain] = struct{}{}
	}
	var out []DNSRecord
	for _, record := range current {
		if _, ok := deny[record.Domain]; ok {
			continue
		}
		out = append(out, DNSRecord{Domain: record.Domain, IPs: append([]string{}, record.IPs...)})
	}
	return out
}

func dnsDomainsAbsent(before, after []DNSRecord) []string {
	afterSet := map[string]struct{}{}
	for _, record := range after {
		afterSet[record.Domain] = struct{}{}
	}
	var out []string
	for _, record := range before {
		if _, ok := afterSet[record.Domain]; !ok {
			out = append(out, record.Domain)
		}
	}
	return uniqueStrings(out)
}

func labelDeleteKeys(remove []string, add Labels) []string {
	var out []string
	for _, label := range remove {
		if _, replaced := add[label]; replaced {
			continue
		}
		out = append(out, ExternalIDLabelKey(label))
	}
	return out
}

func removeStrings(current, remove []string) []string {
	if len(remove) == 0 {
		return append([]string{}, current...)
	}
	deny := map[string]struct{}{}
	for _, value := range remove {
		deny[value] = struct{}{}
	}
	out := make([]string, 0, len(current))
	for _, value := range current {
		if _, ok := deny[value]; ok {
			continue
		}
		out = append(out, value)
	}
	return out
}

func removeSecurityRules(current []SecurityRule, remove []string) []SecurityRule {
	if len(remove) == 0 {
		return cloneSecurityRules(current)
	}
	deny := map[string]struct{}{}
	for _, value := range remove {
		deny[value] = struct{}{}
	}
	out := make([]SecurityRule, 0, len(current))
	for _, rule := range current {
		if _, ok := deny[rule.Name]; rule.Name != "" && ok {
			continue
		}
		if _, ok := deny[securityRuleKey(rule)]; ok {
			continue
		}
		out = append(out, rule)
	}
	return out
}

func securityRuleKey(rule SecurityRule) string {
	return strings.Join([]string{
		rule.Name,
		rule.Action,
		rule.Direction,
		rule.Protocol,
		strings.Join(rule.CIDRs, ","),
		intSliceKey(rule.Ports),
		strconv.FormatBool(rule.Established),
	}, "|")
}

func intSliceKey(values []int) string {
	if len(values) == 0 {
		return ""
	}
	parts := make([]string, len(values))
	for i, value := range values {
		parts[i] = strconv.Itoa(value)
	}
	return strings.Join(parts, ",")
}

func intentExternalIDs(kind, name string, owner OwnerRef, labels Labels) (map[string]string, error) {
	if err := owner.Validate(); err != nil {
		return nil, &Error{Kind: ErrorOwnershipViolation, Operation: "reconcile", Object: name, Message: err.Error(), Err: err}
	}
	externalIDs, err := owner.ExternalIDs(labels)
	if err != nil {
		return nil, err
	}
	externalIDs[ExternalIDKindKey] = kind
	externalIDs[ExternalIDNameKey] = name
	return externalIDs, nil
}

func providerNetworkLocalnetOwnedBy(externalIDs map[string]string, name string) bool {
	return externalIDs[ExternalIDManagedByKey] == "ovnflow" && externalIDs[ExternalIDKindKey] == "ProviderNetwork" && externalIDs[ExternalIDNameKey] == name
}

func providerNetworkMappingOwnerKey(physicalNetwork string) string {
	return ExternalIDPrefix + "provider-network-mapping/" + base64.RawURLEncoding.EncodeToString([]byte(physicalNetwork))
}

func ruleDirection(rule SecurityRule) string {
	if rule.Direction != "" {
		return rule.Direction
	}
	return "to-lport"
}

func rulePriority(rule SecurityRule) int {
	if rule.Established {
		return 1002
	}
	return 1001
}

func ruleAction(rule SecurityRule) string {
	if rule.Action == "" {
		return "allow"
	}
	return rule.Action
}

func ruleMatch(subject string, rule SecurityRule) string {
	var clauses []string
	if subject != "" {
		clauses = append(clauses, `outport == @`+subject)
	}
	if rule.Protocol != "" {
		clauses = append(clauses, strings.ToLower(rule.Protocol))
	}
	for _, cidr := range rule.CIDRs {
		clauses = append(clauses, "ip4.src == "+cidr)
	}
	for _, port := range rule.Ports {
		clauses = append(clauses, "tcp.dst == "+strconv.Itoa(port))
	}
	if rule.Established {
		clauses = append(clauses, "ct.est")
	}
	if len(clauses) == 0 {
		return "1 == 1"
	}
	return strings.Join(clauses, " && ")
}

package ovnflow

import (
	"context"
	"net"
	"sort"
	"strconv"
	"strings"
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
	Owner         OwnerRef
	Labels        Labels
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
	return DryRunResult{Plan: plan, Diff: Diff{Resource: "VirtualNetwork", Name: b.spec.Name}}, nil
}

func (b *VirtualNetworkBuilder) Reconcile(ctx context.Context) (ReconcileResult, error) {
	plan, err := b.Plan(ctx)
	if err != nil {
		return ReconcileResult{}, err
	}
	if b.ref == nil || b.ref.client == nil || b.ref.client.db == nil {
		return ReconcileResult{Plan: plan, Applied: false}, nil
	}
	if err := b.reconcileOVSDB(ctx); err != nil {
		return ReconcileResult{}, err
	}
	return ReconcileResult{Plan: plan, Applied: true}, nil
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
	return DryRunResult{Plan: plan, Diff: Diff{Resource: "LogicalSwitchDNS", Name: b.spec.Name}}, nil
}

func (b *LogicalSwitchDNSBuilder) Reconcile(ctx context.Context) (ReconcileResult, error) {
	plan, err := b.Plan(ctx)
	if err != nil {
		return ReconcileResult{}, err
	}
	if b.ref == nil || b.ref.client == nil || b.ref.client.db == nil {
		return ReconcileResult{Plan: plan, Applied: false}, nil
	}
	if err := b.reconcileOVSDB(ctx); err != nil {
		return ReconcileResult{}, err
	}
	return ReconcileResult{Plan: plan, Applied: true}, nil
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
	return workloadAttachmentFromLogicalSwitchPort(lsp), nil
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
	return DryRunResult{Plan: plan, Diff: Diff{Resource: "WorkloadAttachment", Name: b.spec.Name}}, nil
}

func (b *WorkloadAttachmentBuilder) Reconcile(ctx context.Context) (ReconcileResult, error) {
	plan, err := b.Plan(ctx)
	if err != nil {
		return ReconcileResult{}, err
	}
	if b.ref == nil || b.ref.client == nil || b.ref.client.db == nil {
		return ReconcileResult{Plan: plan, Applied: false}, nil
	}
	if err := b.reconcileOVSDB(ctx); err != nil {
		return ReconcileResult{}, err
	}
	return ReconcileResult{Plan: plan, Applied: true}, nil
}

func (b *WorkloadAttachmentBuilder) Execute(ctx context.Context) error {
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
	return securityPolicyFromPortGroup(pg), nil
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
	return DryRunResult{Plan: plan, Diff: Diff{Resource: "SecurityPolicy", Name: b.spec.Name}}, nil
}

func (b *SecurityPolicyBuilder) Reconcile(ctx context.Context) (ReconcileResult, error) {
	plan, err := b.Plan(ctx)
	if err != nil {
		return ReconcileResult{}, err
	}
	if b.ref == nil || b.ref.client == nil || b.ref.client.db == nil {
		return ReconcileResult{Plan: plan, Applied: false}, nil
	}
	if err := b.reconcileOVSDB(ctx); err != nil {
		return ReconcileResult{}, err
	}
	return ReconcileResult{Plan: plan, Applied: true}, nil
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
	return w.Labels.Validate()
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
	return nil
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

func (b *SecurityPolicyBuilder) reconcileOVSDB(ctx context.Context) error {
	externalIDs, err := intentExternalIDs("SecurityPolicy", b.spec.Name, b.spec.Owner, b.spec.Labels)
	if err != nil {
		return err
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

func securityPolicyFromPortGroup(pg *PortGroup) *SecurityPolicy {
	if pg == nil {
		return nil
	}
	owner, labels := ownerAndLabelsFromExternalIDs(pg.ExternalIDs)
	return &SecurityPolicy{
		Name:   pg.Name,
		Owner:  owner,
		Labels: labels,
	}
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

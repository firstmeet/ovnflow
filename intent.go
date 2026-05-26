package ovnflow

import (
	"context"
	"net"
	"sort"
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

func (r *VirtualNetworkRef) Inspect(ctx context.Context) (InspectResult, error) {
	if err := validateName("virtual network", r.name); err != nil {
		return InspectResult{}, err
	}
	return InspectResult{Resource: "VirtualNetwork", Name: r.name, Status: "stub"}, nil
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
	return ReconcileResult{Plan: plan, Applied: false}, nil
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
	return &LogicalSwitchDNSBuilder{ref: r, spec: LogicalSwitchDNS{Name: r.name}}
}

func (b *LogicalSwitchDNSBuilder) AddRecord(domain string, ips ...string) *LogicalSwitchDNSBuilder {
	b.spec.Records = append(b.spec.Records, DNSRecord{Domain: domain, IPs: append([]string{}, ips...)})
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
	return ReconcileResult{Plan: plan, Applied: false}, nil
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
	return ReconcileResult{Plan: plan, Applied: false}, nil
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
	return ReconcileResult{Plan: plan, Applied: false}, nil
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
	return nil
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

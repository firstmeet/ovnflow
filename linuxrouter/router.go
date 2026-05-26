package linuxrouter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"net"
	"reflect"
	"sort"
	"strings"
	"sync"

	"github.com/firstmeet/ovnflow"
)

type Router struct {
	Name   string
	Spec   Spec
	Status Status
}

type Spec struct {
	Namespace  string
	Owner      ovnflow.OwnerRef
	Labels     ovnflow.Labels
	Interfaces []Interface
	Routes     []Route
	DNSMasq    DNSMasq
	NAT        NAT
	Firewall   Firewall
}

type Status struct {
	Exists            bool
	Namespace         string
	Interfaces        []InterfaceStatus
	Routes            []RouteStatus
	DNSMasq           DNSMasqStatus
	NATBackend        string
	InstalledNAT      []string
	InstalledFirewall []string
	ResourceVersion   string
	ObservedHash      string
	LastError         string
}

type InterfaceRole string

const (
	InterfaceLAN InterfaceRole = "lan"
	InterfaceWAN InterfaceRole = "wan"
)

type Interface struct {
	Name       string
	Role       InterfaceRole
	Bridge     string
	OVSPort    string
	Addresses  []string
	Gateway    string
	DHCPClient bool
}

type InterfaceStatus struct {
	Name      string
	Role      InterfaceRole
	Addresses []string
	Up        bool
}

type Route struct {
	Name        string
	Destination string
	Gateway     string
	Interface   string
}

type RouteStatus struct {
	Destination string
	Gateway     string
	Interface   string
}

type DNSMasq struct {
	Enabled    bool
	DHCPRanges []DHCPRange
	Servers    []string
	Leases     []StaticLease
	Hosts      []HostRecord
	PIDFile    string
	LeaseFile  string
	ConfigFile string
}

type DHCPRange struct {
	Start string
	End   string
	Lease string
}

type StaticLease struct {
	MAC  string
	IP   string
	Name string
}

type HostRecord struct {
	Domain string
	IPs    []string
}

type DNSMasqStatus struct {
	Running bool
	PID     int
}

type NAT struct {
	SNATRules       []SNATRule
	Masquerades     []MasqueradeRule
	DNATRules       []DNATRule
	PortForwards    []PortForwardRule
	DestinationMaps []DestinationMapRule
}

type SNATRule struct {
	Name         string
	SourceCIDR   string
	OutInterface string
	ToSource     string
}

type MasqueradeRule struct {
	Name         string
	SourceCIDR   string
	OutInterface string
}

type DNATRule struct {
	Name          string
	MatchAddress  string
	TargetAddress string
	InInterface   string
}

type PortForwardRule struct {
	Name        string
	Protocol    string
	InInterface string
	ListenIP    string
	ListenPort  int
	TargetIP    string
	TargetPort  int
}

type DestinationMapRule struct {
	Name          string
	MatchAddress  string
	TargetAddress string
	FromCIDR      string
	InInterface   string
	OutInterface  string
	SourceNAT     string
}

type Firewall struct {
	Rules []FirewallRule
}

type FirewallRule struct {
	Name      string
	Action    string
	Direction string
	Protocol  string
	CIDRs     []string
	Ports     []int
}

type Command struct {
	Program             string
	Args                []string
	IgnoreNotFound      bool
	IgnoreAlreadyExists bool
}

type Executor interface {
	Run(context.Context, Command) error
}

type Renderer interface {
	RenderApply(Router) ([]Command, error)
}

type Observer interface {
	Observe(context.Context, Router) (Status, error)
}

type ObserverFunc func(context.Context, Router) (Status, error)

func (f ObserverFunc) Observe(ctx context.Context, router Router) (Status, error) {
	return f(ctx, router)
}

type FakeExecutor struct {
	mu       sync.Mutex
	commands []Command
}

func (f *FakeExecutor) Run(ctx context.Context, cmd Command) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.commands = append(f.commands, cloneCommand(cmd))
	return nil
}

func (f *FakeExecutor) Snapshot() []Command {
	f.mu.Lock()
	defer f.mu.Unlock()
	return cloneCommands(f.commands)
}

type Client struct {
	mu       sync.RWMutex
	executor Executor
	renderer Renderer
	observer Observer
	store    map[string]Router
}

type PlatformClient interface {
	Router(name string) RouterRef
}

type RouterRef interface {
	Get(context.Context) (Router, error)
	Apply(context.Context, Router) error
	Patch(context.Context, Patch) (Router, error)
}

func NewClient(executor Executor, renderer Renderer) *Client {
	return NewObservedClient(executor, renderer, nil)
}

func NewObservedClient(executor Executor, renderer Renderer, observer Observer) *Client {
	if renderer == nil {
		renderer = CommandRenderer{}
	}
	if executor == nil {
		executor = &FakeExecutor{}
	}
	return &Client{executor: executor, renderer: renderer, observer: observer, store: map[string]Router{}}
}

func (c *Client) Router(name string) RouterRef {
	return &Ref{client: c, name: name}
}

func (c *Client) refreshStoredStatus(ctx context.Context, name string) (Router, error) {
	c.mu.RLock()
	router, ok := c.store[name]
	observer := c.observer
	c.mu.RUnlock()
	if !ok {
		return Router{}, ovnflow.ErrNotFound
	}
	router = cloneRouter(router)
	if observer == nil {
		return router, nil
	}
	status, err := observer.Observe(ctx, router)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return Router{}, err
		}
		status = cloneStatus(router.Status)
		status.Namespace = router.Spec.namespaceOrDefault(router.Name)
		status.LastError = err.Error()
	} else {
		status.LastError = ""
	}
	status = normalizeObservedStatus(status)
	router.Status = status

	c.mu.Lock()
	if current, ok := c.store[name]; ok {
		if reflect.DeepEqual(current.Spec, router.Spec) {
			current.Status = cloneStatus(status)
			c.store[name] = current
		}
	}
	c.mu.Unlock()
	return cloneRouter(router), nil
}

type Ref struct {
	client *Client
	name   string
}

func (r *Ref) Get(ctx context.Context) (Router, error) {
	if err := validateName("router", r.name); err != nil {
		return Router{}, err
	}
	if r.client == nil {
		return Router{}, ovnflow.ErrBackendUnavailable
	}
	return r.client.refreshStoredStatus(ctx, r.name)
}

func (r *Ref) Apply(ctx context.Context, router Router) error {
	if r.client == nil {
		return ovnflow.ErrBackendUnavailable
	}
	if router.Name == "" {
		router.Name = r.name
	} else if router.Name != r.name {
		return &ovnflow.Error{Kind: ovnflow.ErrorConflict, Operation: "apply", Object: router.Name, Message: "router name does not match reference"}
	}
	if err := router.Validate(); err != nil {
		return err
	}
	commands, err := r.client.renderer.RenderApply(router)
	if err != nil {
		return err
	}
	if err := func() error {
		r.client.mu.Lock()
		defer r.client.mu.Unlock()
		for _, command := range commands {
			if err := r.client.executor.Run(ctx, command); err != nil {
				return err
			}
		}
		r.client.store[router.Name] = cloneRouter(router)
		return nil
	}(); err != nil {
		return err
	}
	_, _ = r.client.refreshStoredStatus(ctx, router.Name)
	return nil
}

func (r *Ref) Patch(ctx context.Context, patch Patch) (Router, error) {
	if err := validateName("router", r.name); err != nil {
		return Router{}, err
	}
	if r.client == nil {
		return Router{}, ovnflow.ErrBackendUnavailable
	}
	var current Router
	if err := func() error {
		r.client.mu.Lock()
		defer r.client.mu.Unlock()
		stored, ok := r.client.store[r.name]
		if !ok {
			return ovnflow.ErrNotFound
		}
		current = cloneRouter(stored)
		if err := patch.Validate(current); err != nil {
			return err
		}
		if err := patch.ApplyTo(&current); err != nil {
			return err
		}
		if err := current.Validate(); err != nil {
			return err
		}
		commands, err := r.client.renderer.RenderApply(current)
		if err != nil {
			return err
		}
		for _, command := range commands {
			if err := r.client.executor.Run(ctx, command); err != nil {
				return err
			}
		}
		r.client.store[current.Name] = cloneRouter(current)
		return nil
	}(); err != nil {
		return Router{}, err
	}
	return r.client.refreshStoredStatus(ctx, current.Name)
}

type Patch struct {
	Preconditions PatchPreconditions
	Options       PatchOptions
	Interfaces    InterfacePatch
	Routes        RoutePatch
	DNSMasq       DNSMasqPatch
	NAT           NATPatch
	Firewall      FirewallPatch
}

type PatchPreconditions struct {
	ResourceVersion string
	ObservedHash    string
	Owner           ovnflow.OwnerRef
}

type PatchOptions struct {
	RequireOwnership bool
	IgnoreNotFound   bool
}

type InterfacePatch struct {
	Add     []Interface
	Replace []Interface
	Delete  []string
}

type RoutePatch struct {
	Add     []Route
	Replace []Route
	Delete  []string
}

type DNSMasqPatch struct {
	AddHosts    []HostRecord
	Replace     *DNSMasq
	DeleteHosts []string
}

type NATPatch struct {
	Add     NAT
	Replace *NAT
	Delete  NATDelete
}

type NATDelete struct {
	SNATRules       []string
	Masquerades     []string
	DNATRules       []string
	PortForwards    []string
	DestinationMaps []string
}

type FirewallPatch struct {
	AddRules    []FirewallRule
	Replace     *Firewall
	DeleteRules []string
}

func (p Patch) Validate(current Router) error {
	if p.Preconditions.ResourceVersion != "" && p.Preconditions.ResourceVersion != current.Status.ResourceVersion {
		return &ovnflow.Error{Kind: ovnflow.ErrorConflict, Operation: "patch", Object: current.Name, Message: "resource version precondition failed"}
	}
	if p.Preconditions.ObservedHash != "" && p.Preconditions.ObservedHash != current.Status.ObservedHash {
		return &ovnflow.Error{Kind: ovnflow.ErrorConflict, Operation: "patch", Object: current.Name, Message: "observed hash precondition failed"}
	}
	if p.Options.RequireOwnership {
		if err := p.Preconditions.Owner.Validate(); err != nil {
			return err
		}
		if current.Spec.Owner != p.Preconditions.Owner {
			return &ovnflow.Error{Kind: ovnflow.ErrorOwnershipViolation, Operation: "patch", Object: current.Name, Message: "owner precondition failed"}
		}
	}
	return nil
}

func (p Patch) ApplyTo(router *Router) error {
	if router == nil {
		return &ovnflow.Error{Kind: ovnflow.ErrorValidation, Operation: "patch", Object: "router", Message: "router must not be nil"}
	}
	next := cloneRouter(*router)
	if p.DNSMasq.Replace != nil {
		next.Spec.DNSMasq = *p.DNSMasq.Replace
	}
	if p.NAT.Replace != nil {
		next.Spec.NAT = *p.NAT.Replace
	}
	if p.Firewall.Replace != nil {
		next.Spec.Firewall = *p.Firewall.Replace
	}
	var err error
	next.Spec.Interfaces = replaceByName(next.Spec.Interfaces, p.Interfaces.Replace, func(v Interface) string { return v.Name })
	next.Spec.Interfaces, err = deleteByName(next.Spec.Interfaces, p.Interfaces.Delete, p.Options.IgnoreNotFound, "interface", func(v Interface) string { return v.Name })
	if err != nil {
		return err
	}
	next.Spec.Interfaces = append(next.Spec.Interfaces, p.Interfaces.Add...)
	next.Spec.Routes = replaceByName(next.Spec.Routes, p.Routes.Replace, func(v Route) string { return v.Name })
	next.Spec.Routes, err = deleteByName(next.Spec.Routes, p.Routes.Delete, p.Options.IgnoreNotFound, "route", func(v Route) string { return v.Name })
	if err != nil {
		return err
	}
	next.Spec.Routes = append(next.Spec.Routes, p.Routes.Add...)
	next.Spec.NAT.SNATRules, err = deleteByName(next.Spec.NAT.SNATRules, p.NAT.Delete.SNATRules, p.Options.IgnoreNotFound, "snat", func(v SNATRule) string { return v.StableName() })
	if err != nil {
		return err
	}
	next.Spec.NAT.Masquerades, err = deleteByName(next.Spec.NAT.Masquerades, p.NAT.Delete.Masquerades, p.Options.IgnoreNotFound, "masquerade", func(v MasqueradeRule) string { return v.StableName() })
	if err != nil {
		return err
	}
	next.Spec.NAT.DNATRules, err = deleteByName(next.Spec.NAT.DNATRules, p.NAT.Delete.DNATRules, p.Options.IgnoreNotFound, "dnat", func(v DNATRule) string { return v.StableName() })
	if err != nil {
		return err
	}
	next.Spec.NAT.PortForwards, err = deleteByName(next.Spec.NAT.PortForwards, p.NAT.Delete.PortForwards, p.Options.IgnoreNotFound, "port-forward", func(v PortForwardRule) string { return v.StableName() })
	if err != nil {
		return err
	}
	next.Spec.NAT.DestinationMaps, err = deleteByName(next.Spec.NAT.DestinationMaps, p.NAT.Delete.DestinationMaps, p.Options.IgnoreNotFound, "destination-map", func(v DestinationMapRule) string { return v.StableName() })
	if err != nil {
		return err
	}
	next.Spec.NAT.SNATRules = append(next.Spec.NAT.SNATRules, p.NAT.Add.SNATRules...)
	next.Spec.NAT.Masquerades = append(next.Spec.NAT.Masquerades, p.NAT.Add.Masquerades...)
	next.Spec.NAT.DNATRules = append(next.Spec.NAT.DNATRules, p.NAT.Add.DNATRules...)
	next.Spec.NAT.PortForwards = append(next.Spec.NAT.PortForwards, p.NAT.Add.PortForwards...)
	next.Spec.NAT.DestinationMaps = append(next.Spec.NAT.DestinationMaps, p.NAT.Add.DestinationMaps...)
	next.Spec.DNSMasq.Hosts, err = deleteByName(next.Spec.DNSMasq.Hosts, p.DNSMasq.DeleteHosts, p.Options.IgnoreNotFound, "dnsmasq-host", func(v HostRecord) string { return v.Domain })
	if err != nil {
		return err
	}
	next.Spec.DNSMasq.Hosts = append(next.Spec.DNSMasq.Hosts, p.DNSMasq.AddHosts...)
	next.Spec.Firewall.Rules, err = deleteByName(next.Spec.Firewall.Rules, p.Firewall.DeleteRules, p.Options.IgnoreNotFound, "firewall-rule", func(v FirewallRule) string { return v.Name })
	if err != nil {
		return err
	}
	next.Spec.Firewall.Rules = append(next.Spec.Firewall.Rules, p.Firewall.AddRules...)
	*router = cloneRouter(next)
	return nil
}

type Plan struct {
	Commands []Command
}

func (r Router) Plan(renderer Renderer) (Plan, error) {
	if renderer == nil {
		renderer = CommandRenderer{}
	}
	if err := r.Validate(); err != nil {
		return Plan{}, err
	}
	commands, err := renderer.RenderApply(r)
	if err != nil {
		return Plan{}, err
	}
	return Plan{Commands: commands}, nil
}

type CommandRenderer struct{}

func (CommandRenderer) RenderApply(router Router) ([]Command, error) {
	if err := inferNATInterfaces(&router.Spec); err != nil {
		return nil, err
	}
	commands := []Command{{Program: "ip", Args: []string{"netns", "ensure", router.Spec.namespaceOrDefault(router.Name)}}}
	for _, iface := range router.Spec.Interfaces {
		commands = append(commands, Command{Program: "ip", Args: []string{"link", "plan", iface.Name, string(iface.Role)}})
	}
	for _, rule := range router.Spec.NAT.Masquerades {
		commands = append(commands, Command{Program: "nat", Args: []string{"masquerade", rule.StableName(), rule.SourceCIDR, rule.OutInterface}})
	}
	for _, rule := range router.Spec.NAT.SNATRules {
		commands = append(commands, Command{Program: "nat", Args: []string{"snat", rule.StableName(), rule.SourceCIDR, rule.OutInterface, rule.ToSource}})
	}
	for _, rule := range router.Spec.NAT.PortForwards {
		commands = append(commands, Command{Program: "nat", Args: []string{"port-forward", rule.StableName(), rule.Protocol, rule.InInterface, rule.ListenIP, fmt.Sprint(rule.ListenPort), rule.TargetIP, fmt.Sprint(rule.TargetPort)}})
	}
	for _, rule := range router.Spec.NAT.DNATRules {
		commands = append(commands, Command{Program: "nat", Args: []string{"dnat", rule.StableName(), rule.InInterface, rule.MatchAddress, rule.TargetAddress}})
	}
	for _, rule := range router.Spec.NAT.DestinationMaps {
		commands = append(commands, Command{Program: "nat", Args: []string{"destination-map", rule.StableName(), rule.InInterface, rule.OutInterface, rule.MatchAddress, rule.TargetAddress, rule.FromCIDR, rule.SourceNAT}})
	}
	return commands, nil
}

func (r Router) Validate() error {
	if err := validateName("router", r.Name); err != nil {
		return err
	}
	if r.Spec.Owner.Kind != "" || r.Spec.Owner.Name != "" || r.Spec.Owner.ID != "" {
		if err := r.Spec.Owner.Validate(); err != nil {
			return err
		}
	}
	if err := r.Spec.Labels.Validate(); err != nil {
		return err
	}
	for _, iface := range r.Spec.Interfaces {
		if err := iface.Validate(); err != nil {
			return err
		}
	}
	for _, route := range r.Spec.Routes {
		if route.Destination != "" {
			if _, _, err := net.ParseCIDR(route.Destination); err != nil {
				return wrapValidation("route", route.Name, "invalid destination", err)
			}
		}
	}
	if err := r.Spec.DNSMasq.Validate(); err != nil {
		return err
	}
	if err := r.Spec.NAT.Validate(); err != nil {
		return err
	}
	return r.Spec.Firewall.Validate()
}

func (i Interface) Validate() error {
	if err := validateName("interface", i.Name); err != nil {
		return err
	}
	if i.Role != "" && i.Role != InterfaceLAN && i.Role != InterfaceWAN {
		return wrapValidation("interface", i.Name, "invalid role", nil)
	}
	for _, addr := range i.Addresses {
		if _, _, err := net.ParseCIDR(addr); err != nil {
			return wrapValidation("interface", i.Name, "invalid address", err)
		}
	}
	if i.Gateway != "" && net.ParseIP(i.Gateway) == nil {
		return wrapValidation("interface", i.Name, "invalid gateway", nil)
	}
	return nil
}

func (d DNSMasq) Validate() error {
	for _, dhcpRange := range d.DHCPRanges {
		if net.ParseIP(dhcpRange.Start) == nil {
			return wrapValidation("dnsmasq", dhcpRange.Start, "invalid dhcp range start", nil)
		}
		if net.ParseIP(dhcpRange.End) == nil {
			return wrapValidation("dnsmasq", dhcpRange.End, "invalid dhcp range end", nil)
		}
	}
	for _, server := range d.Servers {
		if !validDNSMasqServer(server) {
			return wrapValidation("dnsmasq", server, "invalid dns server", nil)
		}
	}
	for _, lease := range d.Leases {
		if _, err := net.ParseMAC(lease.MAC); err != nil {
			return wrapValidation("dnsmasq", lease.MAC, "invalid static lease mac", err)
		}
		if net.ParseIP(lease.IP) == nil {
			return wrapValidation("dnsmasq", lease.IP, "invalid static lease ip", nil)
		}
	}
	for _, host := range d.Hosts {
		if strings.TrimSpace(host.Domain) == "" {
			return wrapValidation("dnsmasq", host.Domain, "host domain must not be empty", nil)
		}
		if len(host.IPs) == 0 {
			return wrapValidation("dnsmasq", host.Domain, "host record requires at least one ip", nil)
		}
		for _, ip := range host.IPs {
			if net.ParseIP(ip) == nil {
				return wrapValidation("dnsmasq", host.Domain, "invalid host record ip", nil)
			}
		}
	}
	return nil
}

func (n NAT) Validate() error {
	names := map[string]bool{}
	for _, rule := range n.SNATRules {
		if err := validateRuleName(rule.StableName(), names); err != nil {
			return err
		}
		if _, _, err := net.ParseCIDR(rule.SourceCIDR); err != nil {
			return wrapValidation("nat", rule.StableName(), "invalid source cidr", err)
		}
		if net.ParseIP(rule.ToSource) == nil {
			return wrapValidation("nat", rule.StableName(), "invalid snat source", nil)
		}
	}
	for _, rule := range n.Masquerades {
		if err := validateRuleName(rule.StableName(), names); err != nil {
			return err
		}
		if _, _, err := net.ParseCIDR(rule.SourceCIDR); err != nil {
			return wrapValidation("nat", rule.StableName(), "invalid source cidr", err)
		}
	}
	for _, rule := range n.PortForwards {
		if err := validateRuleName(rule.StableName(), names); err != nil {
			return err
		}
		if !isOneOf(rule.Protocol, "tcp", "udp") {
			return wrapValidation("nat", rule.StableName(), "protocol must be tcp or udp", nil)
		}
		if rule.ListenIP != "" && net.ParseIP(rule.ListenIP) == nil {
			return wrapValidation("nat", rule.StableName(), "invalid listen ip", nil)
		}
		if !validPort(rule.ListenPort) || !validPort(rule.TargetPort) {
			return wrapValidation("nat", rule.StableName(), "ports must be between 1 and 65535", nil)
		}
		if net.ParseIP(rule.TargetIP) == nil {
			return wrapValidation("nat", rule.StableName(), "invalid target ip", nil)
		}
	}
	for _, rule := range n.DNATRules {
		if err := validateRuleName(rule.StableName(), names); err != nil {
			return err
		}
		if net.ParseIP(rule.MatchAddress) == nil || net.ParseIP(rule.TargetAddress) == nil {
			return wrapValidation("nat", rule.StableName(), "invalid dnat address", nil)
		}
	}
	for _, rule := range n.DestinationMaps {
		if err := validateRuleName(rule.StableName(), names); err != nil {
			return err
		}
		if net.ParseIP(rule.MatchAddress) == nil || net.ParseIP(rule.TargetAddress) == nil {
			return wrapValidation("nat", rule.StableName(), "invalid destination map address", nil)
		}
		if rule.FromCIDR != "" {
			if _, _, err := net.ParseCIDR(rule.FromCIDR); err != nil {
				return wrapValidation("nat", rule.StableName(), "invalid destination map source cidr", err)
			}
		}
		if rule.SourceNAT != "" && net.ParseIP(rule.SourceNAT) == nil {
			return wrapValidation("nat", rule.StableName(), "invalid destination map source nat", nil)
		}
	}
	return nil
}

func (f Firewall) Validate() error {
	names := map[string]bool{}
	for _, rule := range f.Rules {
		if err := validateRuleName(rule.Name, names); err != nil {
			return err
		}
		if strings.TrimSpace(rule.Action) != "" && !isOneOf(rule.Action, "allow", "drop", "reject") {
			return wrapValidation("firewall", rule.Name, "action must be allow, drop, or reject", nil)
		}
		if rule.Direction != "" && !isOneOf(rule.Direction, "ingress", "egress", "forward", "in", "out") {
			return wrapValidation("firewall", rule.Name, "invalid direction", nil)
		}
		if rule.Protocol != "" && !isOneOf(rule.Protocol, "tcp", "udp", "icmp", "icmpv6", "ip", "ipv4", "ipv6") {
			return wrapValidation("firewall", rule.Name, "invalid protocol", nil)
		}
		for _, cidr := range rule.CIDRs {
			if _, _, err := net.ParseCIDR(cidr); err != nil {
				return wrapValidation("firewall", rule.Name, "invalid cidr", err)
			}
		}
		for _, port := range rule.Ports {
			if !validPort(port) {
				return wrapValidation("firewall", rule.Name, "port must be between 1 and 65535", nil)
			}
		}
	}
	return nil
}

func (r SNATRule) StableName() string {
	if r.Name != "" {
		return r.Name
	}
	return stableRuleName("snat", r.SourceCIDR, r.OutInterface, r.ToSource)
}

func (r MasqueradeRule) StableName() string {
	if r.Name != "" {
		return r.Name
	}
	return stableRuleName("masquerade", r.SourceCIDR, r.OutInterface)
}

func (r DNATRule) StableName() string {
	if r.Name != "" {
		return r.Name
	}
	return stableRuleName("dnat", r.MatchAddress, r.TargetAddress, r.InInterface)
}

func (r PortForwardRule) StableName() string {
	if r.Name != "" {
		return r.Name
	}
	return stableRuleName("port-forward", r.Protocol, r.InInterface, r.ListenIP, fmt.Sprint(r.ListenPort), r.TargetIP, fmt.Sprint(r.TargetPort))
}

func (r DestinationMapRule) StableName() string {
	if r.Name != "" {
		return r.Name
	}
	return stableRuleName("destination-map", r.MatchAddress, r.TargetAddress, r.FromCIDR, r.InInterface, r.OutInterface, r.SourceNAT)
}

func stableRuleName(parts ...string) string {
	joined := strings.ToLower(strings.Join(parts, "-"))
	replacer := strings.NewReplacer("/", "-", ":", "-", ".", "-", " ", "-", "_", "-")
	return strings.Trim(replacer.Replace(joined), "-")
}

func inferNATInterfaces(spec *Spec) error {
	wans := roleInterfaces(spec.Interfaces, InterfaceWAN)
	lans := roleInterfaces(spec.Interfaces, InterfaceLAN)
	for i := range spec.NAT.Masquerades {
		if spec.NAT.Masquerades[i].OutInterface == "" {
			if len(wans) == 0 {
				return ambiguous("wan")
			}
			if len(wans) > 1 {
				return ambiguous("wan")
			}
			spec.NAT.Masquerades[i].OutInterface = wans[0]
		}
	}
	for i := range spec.NAT.SNATRules {
		if spec.NAT.SNATRules[i].OutInterface == "" {
			if len(wans) == 0 {
				return ambiguous("wan")
			}
			if len(wans) > 1 {
				return ambiguous("wan")
			}
			spec.NAT.SNATRules[i].OutInterface = wans[0]
		}
	}
	for i := range spec.NAT.PortForwards {
		if spec.NAT.PortForwards[i].InInterface == "" {
			if len(wans) == 0 {
				return ambiguous("wan")
			}
			if len(wans) > 1 {
				return ambiguous("wan")
			}
			spec.NAT.PortForwards[i].InInterface = wans[0]
		}
	}
	for i := range spec.NAT.DNATRules {
		if spec.NAT.DNATRules[i].InInterface == "" {
			if len(wans) == 0 {
				return ambiguous("wan")
			}
			if len(wans) > 1 {
				return ambiguous("wan")
			}
			spec.NAT.DNATRules[i].InInterface = wans[0]
		}
	}
	for i := range spec.NAT.DestinationMaps {
		if spec.NAT.DestinationMaps[i].InInterface == "" {
			if len(lans) == 0 {
				return ambiguous("lan")
			}
			if len(lans) > 1 {
				return ambiguous("lan")
			}
			spec.NAT.DestinationMaps[i].InInterface = lans[0]
		}
		if spec.NAT.DestinationMaps[i].OutInterface == "" {
			if len(wans) == 0 {
				return ambiguous("wan")
			}
			if len(wans) > 1 {
				return ambiguous("wan")
			}
			spec.NAT.DestinationMaps[i].OutInterface = wans[0]
		}
	}
	return nil
}

func replaceByName[T any](base, replacements []T, nameOf func(T) string) []T {
	for _, replacement := range replacements {
		name := nameOf(replacement)
		replaced := false
		for i, existing := range base {
			if nameOf(existing) == name {
				base[i] = replacement
				replaced = true
				break
			}
		}
		if !replaced {
			base = append(base, replacement)
		}
	}
	return base
}

func deleteByName[T any](base []T, names []string, ignoreMissing bool, kind string, nameOf func(T) string) ([]T, error) {
	if len(names) == 0 {
		return base, nil
	}
	remove := map[string]bool{}
	for _, name := range names {
		remove[name] = true
	}
	if !ignoreMissing {
		existing := map[string]bool{}
		for _, value := range base {
			existing[nameOf(value)] = true
		}
		for _, name := range names {
			if !existing[name] {
				return base, &ovnflow.Error{Kind: ovnflow.ErrorNotFound, Operation: "patch-delete", Object: kind + ":" + name, Message: kind + " not found"}
			}
		}
	}
	out := base[:0]
	for _, value := range base {
		if !remove[nameOf(value)] {
			out = append(out, value)
		}
	}
	return out, nil
}

func cloneRouter(in Router) Router {
	out := in
	out.Spec = cloneSpec(in.Spec)
	out.Status = cloneStatus(in.Status)
	return out
}

func cloneSpec(in Spec) Spec {
	out := in
	out.Labels = cloneLabels(in.Labels)
	out.Interfaces = cloneInterfaces(in.Interfaces)
	out.Routes = cloneRoutes(in.Routes)
	out.DNSMasq = cloneDNSMasq(in.DNSMasq)
	out.NAT = cloneNAT(in.NAT)
	out.Firewall = cloneFirewall(in.Firewall)
	return out
}

func cloneStatus(in Status) Status {
	out := in
	out.Interfaces = cloneInterfaceStatuses(in.Interfaces)
	out.Routes = cloneRouteStatuses(in.Routes)
	out.InstalledNAT = append([]string{}, in.InstalledNAT...)
	out.InstalledFirewall = append([]string{}, in.InstalledFirewall...)
	return out
}

func cloneLabels(in ovnflow.Labels) ovnflow.Labels {
	if len(in) == 0 {
		return nil
	}
	out := ovnflow.Labels{}
	for key, value := range in {
		out[key] = value
	}
	return out
}

func cloneInterfaces(in []Interface) []Interface {
	out := append([]Interface{}, in...)
	for i := range out {
		out[i].Addresses = append([]string{}, in[i].Addresses...)
	}
	return out
}

func cloneInterfaceStatuses(in []InterfaceStatus) []InterfaceStatus {
	out := append([]InterfaceStatus{}, in...)
	for i := range out {
		out[i].Addresses = append([]string{}, in[i].Addresses...)
	}
	return out
}

func cloneRoutes(in []Route) []Route {
	return append([]Route{}, in...)
}

func cloneRouteStatuses(in []RouteStatus) []RouteStatus {
	return append([]RouteStatus{}, in...)
}

func cloneDNSMasq(in DNSMasq) DNSMasq {
	out := in
	out.DHCPRanges = append([]DHCPRange{}, in.DHCPRanges...)
	out.Servers = append([]string{}, in.Servers...)
	out.Leases = append([]StaticLease{}, in.Leases...)
	out.Hosts = cloneHostRecords(in.Hosts)
	return out
}

func cloneHostRecords(in []HostRecord) []HostRecord {
	out := append([]HostRecord{}, in...)
	for i := range out {
		out[i].IPs = append([]string{}, in[i].IPs...)
	}
	return out
}

func cloneNAT(in NAT) NAT {
	return NAT{
		SNATRules:       append([]SNATRule{}, in.SNATRules...),
		Masquerades:     append([]MasqueradeRule{}, in.Masquerades...),
		DNATRules:       append([]DNATRule{}, in.DNATRules...),
		PortForwards:    append([]PortForwardRule{}, in.PortForwards...),
		DestinationMaps: append([]DestinationMapRule{}, in.DestinationMaps...),
	}
}

func cloneFirewall(in Firewall) Firewall {
	out := in
	out.Rules = append([]FirewallRule{}, in.Rules...)
	for i := range out.Rules {
		out.Rules[i].CIDRs = append([]string{}, in.Rules[i].CIDRs...)
		out.Rules[i].Ports = append([]int{}, in.Rules[i].Ports...)
	}
	return out
}

func cloneCommand(in Command) Command {
	out := in
	out.Args = append([]string{}, in.Args...)
	return out
}

func cloneCommands(in []Command) []Command {
	out := append([]Command{}, in...)
	for i := range out {
		out[i].Args = append([]string{}, in[i].Args...)
	}
	return out
}

func normalizeObservedStatus(status Status) Status {
	status.Interfaces = cloneInterfaceStatuses(status.Interfaces)
	status.Routes = cloneRouteStatuses(status.Routes)
	status.InstalledNAT = uniqueSortedStrings(status.InstalledNAT)
	status.InstalledFirewall = uniqueSortedStrings(status.InstalledFirewall)
	status.ObservedHash = observedHash(status)
	if status.ResourceVersion == "" {
		status.ResourceVersion = status.ObservedHash
	}
	return status
}

func observedHash(status Status) string {
	copy := status
	copy.ResourceVersion = ""
	copy.ObservedHash = ""
	copy.LastError = ""
	data, err := json.Marshal(copy)
	if err != nil {
		return ""
	}
	h := fnv.New64a()
	_, _ = h.Write(data)
	return fmt.Sprintf("%x", h.Sum64())
}

func uniqueSortedStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func roleInterfaces(interfaces []Interface, role InterfaceRole) []string {
	var out []string
	for _, iface := range interfaces {
		if iface.Role == role {
			out = append(out, iface.Name)
		}
	}
	sort.Strings(out)
	return out
}

func (s Spec) namespaceOrDefault(name string) string {
	if s.Namespace != "" {
		return s.Namespace
	}
	return "ovnflow-" + name
}

func validateRuleName(name string, names map[string]bool) error {
	if err := validateName("rule", name); err != nil {
		return err
	}
	if names[name] {
		return wrapValidation("rule", name, "duplicate stable rule name", nil)
	}
	names[name] = true
	return nil
}

func validateName(kind, name string) error {
	if strings.TrimSpace(name) == "" {
		return wrapValidation(kind, name, "name must not be empty", nil)
	}
	return nil
}

func validPort(port int) bool {
	return port >= 1 && port <= 65535
}

func validDNSMasqServer(server string) bool {
	server = strings.TrimSpace(server)
	return server != "" && !strings.ContainsAny(server, " \t\r\n")
}

func isOneOf(value string, allowed ...string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

func ambiguous(kind string) error {
	return &ovnflow.Error{Kind: ovnflow.ErrorAmbiguous, Operation: "plan", Object: kind, Message: "interface inference is ambiguous"}
}

func wrapValidation(kind, object, message string, err error) error {
	return &ovnflow.Error{Kind: ovnflow.ErrorValidation, Operation: "validate", Object: kind + ":" + object, Message: message, Err: err}
}

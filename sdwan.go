package ovnflow

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"sync"
)

type SDWANTransport string

const (
	SDWANTransportWireGuard SDWANTransport = "wireguard"
	SDWANTransportGeneve    SDWANTransport = "geneve"
	SDWANTransportVXLAN     SDWANTransport = "vxlan"
)

type SDWANLayer string

const (
	SDWANLayerL3 SDWANLayer = "l3"
	SDWANLayerL2 SDWANLayer = "l2"
)

type SDWANTopology string

const (
	SDWANTopologyPartialMesh SDWANTopology = "partial-mesh"
	SDWANTopologyHubSpoke    SDWANTopology = "hub-spoke"
	SDWANTopologyFullMesh    SDWANTopology = "full-mesh"
)

type SDWANBackend interface {
	GetSDWAN(context.Context, string) (*SDWANNetwork, error)
	ApplySDWAN(context.Context, SDWANNetwork, SDWANApplyPlan) error
	DeleteSDWAN(context.Context, string) error
}

type SDWANClient struct {
	backend SDWANBackend
}

type SDWANNetworkRef struct {
	client *SDWANClient
	name   string
}

type SDWANNetworkBuilder struct {
	once    useOnce
	ref     *SDWANNetworkRef
	network SDWANNetwork
}

type SDWANDeleteBuilder struct {
	once useOnce
	ref  *SDWANNetworkRef
}

type SDWANNetwork struct {
	Name      string
	Layer     SDWANLayer
	Topology  SDWANTopology
	Transport SDWANTransport
	Sites     []SDWANSite
	Links     []SDWANLink
	Policies  []SDWANPolicy
	Owner     OwnerRef
	Labels    Labels
	Status    SDWANStatus
}

type SDWANSite struct {
	Name       string
	Router     string
	CIDRs      []string
	Endpoint   string
	PublicKey  string
	Role       string
	Attributes map[string]string
	L2Segments []string
	Transit    bool
	Relay      bool
}

type SDWANLink struct {
	Name       string
	From       string
	To         string
	Transport  SDWANTransport
	EndpointA  string
	EndpointB  string
	AllowedIPs []string
	Cost       int
	Enabled    bool
}

type SDWANPolicy struct {
	Name        string
	Layer       SDWANLayer
	SourceSite  string
	DestSite    string
	SourceCIDRs []string
	DestCIDRs   []string
	PreferLinks []string
	AvoidLinks  []string
	Priority    int
}

type SDWANStatus struct {
	State        ResourceStatusState
	Sites        []SDWANSiteStatus
	Links        []SDWANLinkStatus
	Findings     []StatusFinding
	LastApplied  int
	ResourceHash string
}

type SDWANSiteStatus struct {
	Name  string
	Ready bool
}

type SDWANLinkStatus struct {
	Name      string
	From      string
	To        string
	Transport SDWANTransport
	Ready     bool
}

type SDWANApplyPlan struct {
	Network    string
	Operations []SDWANOperation
}

type SDWANOperation struct {
	Action      IntentAction
	Resource    string
	Name        string
	Description string
}

type InMemorySDWANBackend struct {
	mu       sync.RWMutex
	networks map[string]SDWANNetwork
	plans    map[string]SDWANApplyPlan
}

func (c *Client) SDWAN() *SDWANClient {
	if c == nil {
		return NewSDWANClient(nil)
	}
	c.sdwanMu.Lock()
	defer c.sdwanMu.Unlock()
	if c.sdwan == nil {
		c.sdwan = NewInMemorySDWANBackend()
	}
	return &SDWANClient{backend: c.sdwan}
}

func NewSDWANClient(backend SDWANBackend) *SDWANClient {
	if backend == nil {
		backend = NewInMemorySDWANBackend()
	}
	return &SDWANClient{backend: backend}
}

func NewInMemorySDWANBackend() *InMemorySDWANBackend {
	return &InMemorySDWANBackend{networks: map[string]SDWANNetwork{}, plans: map[string]SDWANApplyPlan{}}
}

func (c *SDWANClient) Network(name string) *SDWANNetworkRef {
	return &SDWANNetworkRef{client: c, name: name}
}

func (r *SDWANNetworkRef) Ensure() *SDWANNetworkBuilder {
	return &SDWANNetworkBuilder{ref: r, network: SDWANNetwork{
		Name:      r.name,
		Layer:     SDWANLayerL3,
		Topology:  SDWANTopologyPartialMesh,
		Transport: SDWANTransportWireGuard,
		Labels:    Labels{},
	}}
}

func (r *SDWANNetworkRef) Get(ctx context.Context) (*SDWANNetwork, error) {
	if err := validateName("sdwan network", r.name); err != nil {
		return nil, err
	}
	if r.client == nil || r.client.backend == nil {
		return nil, ErrBackendUnavailable
	}
	return r.client.backend.GetSDWAN(ctx, r.name)
}

func (r *SDWANNetworkRef) Delete() *SDWANDeleteBuilder {
	return &SDWANDeleteBuilder{ref: r}
}

func (b *SDWANNetworkBuilder) Layer3() *SDWANNetworkBuilder {
	b.network.Layer = SDWANLayerL3
	return b
}

func (b *SDWANNetworkBuilder) Layer2() *SDWANNetworkBuilder {
	b.network.Layer = SDWANLayerL2
	return b
}

func (b *SDWANNetworkBuilder) WithLayer(layer SDWANLayer) *SDWANNetworkBuilder {
	b.network.Layer = layer
	return b
}

func (b *SDWANNetworkBuilder) TopologyPartialMesh() *SDWANNetworkBuilder {
	b.network.Topology = SDWANTopologyPartialMesh
	return b
}

func (b *SDWANNetworkBuilder) TopologyFullMesh() *SDWANNetworkBuilder {
	b.network.Topology = SDWANTopologyFullMesh
	return b
}

func (b *SDWANNetworkBuilder) TopologyHubSpoke() *SDWANNetworkBuilder {
	b.network.Topology = SDWANTopologyHubSpoke
	return b
}

func (b *SDWANNetworkBuilder) WithTransport(transport SDWANTransport) *SDWANNetworkBuilder {
	b.network.Transport = transport
	return b
}

func (b *SDWANNetworkBuilder) AddSite(name string, site SDWANSite) *SDWANNetworkBuilder {
	site.Name = name
	b.network.Sites = replaceSDWANSite(b.network.Sites, site)
	return b
}

func (b *SDWANNetworkBuilder) AddLink(link SDWANLink) *SDWANNetworkBuilder {
	b.network.Links = replaceSDWANLink(b.network.Links, link)
	return b
}

func (b *SDWANNetworkBuilder) AddPolicy(name string, policy SDWANPolicy) *SDWANNetworkBuilder {
	policy.Name = name
	b.network.Policies = replaceSDWANPolicy(b.network.Policies, policy)
	return b
}

func (b *SDWANNetworkBuilder) WithOwner(kind, name string) *SDWANNetworkBuilder {
	b.network.Owner = OwnerRef{Kind: kind, Name: name}
	return b
}

func (b *SDWANNetworkBuilder) WithLabel(key, value string) *SDWANNetworkBuilder {
	if b.network.Labels == nil {
		b.network.Labels = Labels{}
	}
	b.network.Labels[key] = value
	return b
}

func (b *SDWANNetworkBuilder) Validate() error {
	return b.network.Validate()
}

func (b *SDWANNetworkBuilder) Plan(ctx context.Context) (Plan, error) {
	if err := b.Validate(); err != nil {
		return Plan{}, err
	}
	apply, err := b.ApplyPlan(ctx)
	if err != nil {
		return Plan{}, err
	}
	out := Plan{Operations: make([]PlannedOperation, 0, len(apply.Operations))}
	for _, op := range apply.Operations {
		out.Operations = append(out.Operations, PlannedOperation{Action: op.Action, Resource: op.Resource, Name: op.Name, Description: op.Description})
	}
	return out, nil
}

func (b *SDWANNetworkBuilder) ApplyPlan(ctx context.Context) (SDWANApplyPlan, error) {
	if err := b.Validate(); err != nil {
		return SDWANApplyPlan{}, err
	}
	network := normalizeSDWANNetwork(b.network)
	return planSDWANApply(network), nil
}

func (b *SDWANNetworkBuilder) DryRun(ctx context.Context) (DryRunResult, error) {
	plan, err := b.Plan(ctx)
	if err != nil {
		return DryRunResult{}, err
	}
	current, found, err := b.current(ctx)
	if err != nil {
		return DryRunResult{}, err
	}
	desired := normalizeSDWANNetwork(b.network)
	current.Status = SDWANStatus{}
	desired.Status = SDWANStatus{}
	diff := Diff{Resource: "SDWANNetwork", Name: desired.Name}
	if !found {
		diff.Changes = append(diff.Changes, DiffChange{Path: "/", Before: nil, After: desired})
	} else if !reflect.DeepEqual(current, desired) {
		diff.Changes = append(diff.Changes, DiffChange{Path: "/", Before: current, After: desired})
	}
	return DryRunResult{Plan: plan, Diff: diff}, nil
}

func (b *SDWANNetworkBuilder) Reconcile(ctx context.Context) (ReconcileResult, error) {
	dryRun, err := b.DryRun(ctx)
	if err != nil {
		return ReconcileResult{}, err
	}
	if dryRun.Diff.Empty() {
		return ReconcileResult{Plan: dryRun.Plan, Diff: dryRun.Diff, Applied: false}, nil
	}
	if b.ref == nil || b.ref.client == nil || b.ref.client.backend == nil {
		return ReconcileResult{}, ErrBackendUnavailable
	}
	applyPlan, err := b.ApplyPlan(ctx)
	if err != nil {
		return ReconcileResult{}, err
	}
	if err := b.ref.client.backend.ApplySDWAN(ctx, normalizeSDWANNetwork(b.network), applyPlan); err != nil {
		return ReconcileResult{}, err
	}
	return ReconcileResult{Plan: dryRun.Plan, Diff: dryRun.Diff, Applied: true}, nil
}

func (b *SDWANNetworkBuilder) Apply(ctx context.Context) error {
	return b.Execute(ctx)
}

func (b *SDWANNetworkBuilder) Execute(ctx context.Context) error {
	if !b.once.mark() {
		return wrap(ErrorValidation, "", "", "execute", b.network.Name, "builder already executed", nil)
	}
	_, err := b.Reconcile(ctx)
	return err
}

func (b *SDWANNetworkBuilder) current(ctx context.Context) (SDWANNetwork, bool, error) {
	if b.ref == nil || b.ref.client == nil || b.ref.client.backend == nil {
		return SDWANNetwork{}, false, nil
	}
	current, err := b.ref.client.backend.GetSDWAN(ctx, b.network.Name)
	if err != nil {
		if IsKind(err, ErrorNotFound) {
			return SDWANNetwork{}, false, nil
		}
		return SDWANNetwork{}, false, err
	}
	return normalizeSDWANNetwork(*current), true, nil
}

func (b *SDWANDeleteBuilder) Plan(ctx context.Context) (Plan, error) {
	if b == nil || b.ref == nil {
		return Plan{}, ErrBackendUnavailable
	}
	if err := validateName("sdwan network", b.ref.name); err != nil {
		return Plan{}, err
	}
	return Plan{Operations: []PlannedOperation{{Action: IntentActionDelete, Resource: "SDWANNetwork", Name: b.ref.name, Description: "delete SD-WAN network desired state"}}}, nil
}

func (b *SDWANDeleteBuilder) Execute(ctx context.Context) error {
	if !b.once.mark() {
		return wrap(ErrorValidation, "", "", "delete", b.ref.name, "builder already executed", nil)
	}
	if _, err := b.Plan(ctx); err != nil {
		return err
	}
	if b.ref.client == nil || b.ref.client.backend == nil {
		return ErrBackendUnavailable
	}
	return b.ref.client.backend.DeleteSDWAN(ctx, b.ref.name)
}

func (n SDWANNetwork) Validate() error {
	if err := validateName("sdwan network", n.Name); err != nil {
		return err
	}
	if n.Layer == "" {
		n.Layer = SDWANLayerL3
	}
	if !validSDWANLayer(n.Layer) {
		return wrap(ErrorValidation, "", "", "validate", n.Name, "unsupported SD-WAN layer", nil)
	}
	if n.Topology == "" {
		n.Topology = SDWANTopologyPartialMesh
	}
	if !validSDWANTopology(n.Topology) {
		return wrap(ErrorValidation, "", "", "validate", n.Name, "unsupported SD-WAN topology", nil)
	}
	if n.Transport == "" {
		n.Transport = SDWANTransportWireGuard
	}
	if !validSDWANTransport(n.Transport) {
		return wrap(ErrorValidation, "", "", "validate", n.Name, "unsupported SD-WAN transport", nil)
	}
	if n.Owner.Kind != "" || n.Owner.Name != "" || n.Owner.ID != "" {
		if err := n.Owner.Validate(); err != nil {
			return err
		}
	}
	if err := n.Labels.Validate(); err != nil {
		return err
	}
	if len(n.Sites) < 2 {
		return wrap(ErrorValidation, "", "", "validate", n.Name, "at least two SD-WAN sites are required", nil)
	}
	siteNames := map[string]bool{}
	for _, site := range n.Sites {
		if err := site.Validate(n.Layer); err != nil {
			return err
		}
		if siteNames[site.Name] {
			return wrap(ErrorConflict, "", "", "validate", site.Name, "duplicate SD-WAN site", nil)
		}
		siteNames[site.Name] = true
	}
	for _, link := range n.Links {
		if err := link.Validate(siteNames, n.Transport); err != nil {
			return err
		}
	}
	for _, policy := range n.Policies {
		if err := policy.Validate(siteNames); err != nil {
			return err
		}
	}
	return nil
}

func (s SDWANSite) Validate(layer SDWANLayer) error {
	if err := validateName("sdwan site", s.Name); err != nil {
		return err
	}
	if strings.TrimSpace(s.Router) == "" {
		return wrap(ErrorValidation, "", "", "validate", s.Name, "site router is required", nil)
	}
	if layer == SDWANLayerL3 && len(s.CIDRs) == 0 {
		return wrap(ErrorValidation, "", "", "validate", s.Name, "L3 site CIDRs are required", nil)
	}
	for _, cidr := range s.CIDRs {
		if _, err := parseIPv4Prefix(cidr, "validate"); err != nil {
			return err
		}
	}
	if err := Labels(s.Attributes).Validate(); err != nil {
		return err
	}
	return nil
}

func (l SDWANLink) Validate(sites map[string]bool, fallback SDWANTransport) error {
	if err := validateName("sdwan link", l.StableName()); err != nil {
		return err
	}
	if !sites[l.From] || !sites[l.To] {
		return wrap(ErrorValidation, "", "", "validate", l.StableName(), "link endpoints must reference known sites", nil)
	}
	if l.From == l.To {
		return wrap(ErrorValidation, "", "", "validate", l.StableName(), "link endpoints must be different", nil)
	}
	transport := l.Transport
	if transport == "" {
		transport = fallback
	}
	if !validSDWANTransport(transport) {
		return wrap(ErrorValidation, "", "", "validate", l.StableName(), "unsupported SD-WAN link transport", nil)
	}
	return nil
}

func (l SDWANLink) StableName() string {
	if l.Name != "" {
		return l.Name
	}
	a, b := l.From, l.To
	if b < a {
		a, b = b, a
	}
	return a + "--" + b
}

func (p SDWANPolicy) Validate(sites map[string]bool) error {
	if err := validateName("sdwan policy", p.Name); err != nil {
		return err
	}
	if p.SourceSite != "" && !sites[p.SourceSite] {
		return wrap(ErrorValidation, "", "", "validate", p.Name, "source site must reference a known site", nil)
	}
	if p.DestSite != "" && !sites[p.DestSite] {
		return wrap(ErrorValidation, "", "", "validate", p.Name, "destination site must reference a known site", nil)
	}
	for _, cidr := range append(append([]string{}, p.SourceCIDRs...), p.DestCIDRs...) {
		if _, err := parseIPv4Prefix(cidr, "validate"); err != nil {
			return err
		}
	}
	return nil
}

func planSDWANApply(network SDWANNetwork) SDWANApplyPlan {
	links := normalizeSDWANLinks(network)
	ops := []SDWANOperation{{
		Action:      IntentActionEnsure,
		Resource:    "SDWANNetwork",
		Name:        network.Name,
		Description: "reconcile SD-WAN desired state",
	}}
	for _, site := range network.Sites {
		ops = append(ops, SDWANOperation{Action: IntentActionEnsure, Resource: "SDWANSite", Name: site.Name, Description: "ensure SD-WAN site identity and route inventory"})
	}
	for _, link := range links {
		resource := "SDWANTunnel"
		switch link.Transport {
		case SDWANTransportWireGuard:
			resource = "WireGuardTunnel"
		case SDWANTransportGeneve, SDWANTransportVXLAN:
			resource = "OVSTunnel"
		}
		ops = append(ops, SDWANOperation{Action: IntentActionEnsure, Resource: resource, Name: link.StableName(), Description: "ensure partial-mesh SD-WAN tunnel"})
		if network.Layer == SDWANLayerL3 {
			ops = append(ops, SDWANOperation{Action: IntentActionEnsure, Resource: "RoutePolicy", Name: link.StableName(), Description: "ensure SD-WAN route and policy-routing state"})
		} else {
			ops = append(ops, SDWANOperation{Action: IntentActionEnsure, Resource: "OpenFlowRule", Name: link.StableName(), Description: "ensure L2 overlay forwarding flow"})
		}
	}
	for _, policy := range network.Policies {
		ops = append(ops, SDWANOperation{Action: IntentActionEnsure, Resource: "SDWANPolicy", Name: policy.Name, Description: "ensure SD-WAN traffic policy"})
	}
	return SDWANApplyPlan{Network: network.Name, Operations: ops}
}

func normalizeSDWANNetwork(in SDWANNetwork) SDWANNetwork {
	out := cloneSDWANNetwork(in)
	if out.Layer == "" {
		out.Layer = SDWANLayerL3
	}
	if out.Topology == "" {
		out.Topology = SDWANTopologyPartialMesh
	}
	if out.Transport == "" {
		out.Transport = SDWANTransportWireGuard
	}
	out.Sites = normalizeSDWANSites(out.Sites)
	out.Links = normalizeSDWANLinks(out)
	sort.Slice(out.Policies, func(i, j int) bool { return out.Policies[i].Name < out.Policies[j].Name })
	return out
}

func normalizeSDWANSites(in []SDWANSite) []SDWANSite {
	out := append([]SDWANSite{}, in...)
	for i := range out {
		out[i].CIDRs = uniqueStrings(out[i].CIDRs)
		out[i].L2Segments = uniqueStrings(out[i].L2Segments)
		out[i].Attributes = cloneStringMap(out[i].Attributes)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func normalizeSDWANLinks(network SDWANNetwork) []SDWANLink {
	links := append([]SDWANLink{}, network.Links...)
	if network.Topology == SDWANTopologyFullMesh || network.Topology == SDWANTopologyPartialMesh {
		sites := normalizeSDWANSites(network.Sites)
		for i := 0; i < len(sites); i++ {
			for j := i + 1; j < len(sites); j++ {
				links = replaceSDWANLink(links, SDWANLink{From: sites[i].Name, To: sites[j].Name, Transport: network.Transport, Enabled: true})
			}
		}
	}
	for i := range links {
		if links[i].Name == "" {
			links[i].Name = links[i].StableName()
		}
		if links[i].Transport == "" {
			links[i].Transport = network.Transport
		}
		if !links[i].Enabled {
			links[i].Enabled = true
		}
		links[i].AllowedIPs = uniqueStrings(links[i].AllowedIPs)
	}
	sort.Slice(links, func(i, j int) bool { return links[i].StableName() < links[j].StableName() })
	return links
}

func replaceSDWANSite(base []SDWANSite, site SDWANSite) []SDWANSite {
	for i := range base {
		if base[i].Name == site.Name {
			base[i] = site
			return base
		}
	}
	return append(base, site)
}

func replaceSDWANLink(base []SDWANLink, link SDWANLink) []SDWANLink {
	name := link.StableName()
	for i := range base {
		if base[i].StableName() == name {
			if link.Name == "" {
				link.Name = name
			}
			base[i] = link
			return base
		}
	}
	if link.Name == "" {
		link.Name = name
	}
	return append(base, link)
}

func replaceSDWANPolicy(base []SDWANPolicy, policy SDWANPolicy) []SDWANPolicy {
	for i := range base {
		if base[i].Name == policy.Name {
			base[i] = policy
			return base
		}
	}
	return append(base, policy)
}

func validSDWANLayer(value SDWANLayer) bool {
	return value == SDWANLayerL3 || value == SDWANLayerL2
}

func validSDWANTopology(value SDWANTopology) bool {
	return value == SDWANTopologyPartialMesh || value == SDWANTopologyHubSpoke || value == SDWANTopologyFullMesh
}

func validSDWANTransport(value SDWANTransport) bool {
	return value == SDWANTransportWireGuard || value == SDWANTransportGeneve || value == SDWANTransportVXLAN
}

func (b *InMemorySDWANBackend) GetSDWAN(_ context.Context, name string) (*SDWANNetwork, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	network, ok := b.networks[name]
	if !ok {
		return nil, wrap(ErrorNotFound, "", "", "get", name, "SD-WAN network not found", nil)
	}
	out := cloneSDWANNetwork(network)
	return &out, nil
}

func (b *InMemorySDWANBackend) ApplySDWAN(_ context.Context, network SDWANNetwork, plan SDWANApplyPlan) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.networks == nil {
		b.networks = map[string]SDWANNetwork{}
	}
	if b.plans == nil {
		b.plans = map[string]SDWANApplyPlan{}
	}
	network = normalizeSDWANNetwork(network)
	network.Status.State = ResourceStatusPresent
	network.Status.LastApplied++
	b.networks[network.Name] = cloneSDWANNetwork(network)
	b.plans[network.Name] = cloneSDWANApplyPlan(plan)
	return nil
}

func (b *InMemorySDWANBackend) DeleteSDWAN(_ context.Context, name string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.networks[name]; !ok {
		return wrap(ErrorNotFound, "", "", "delete", name, "SD-WAN network not found", nil)
	}
	delete(b.networks, name)
	delete(b.plans, name)
	return nil
}

func (b *InMemorySDWANBackend) LastPlan(name string) (SDWANApplyPlan, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	plan, ok := b.plans[name]
	return cloneSDWANApplyPlan(plan), ok
}

func cloneSDWANNetwork(in SDWANNetwork) SDWANNetwork {
	out := in
	out.Sites = append([]SDWANSite{}, in.Sites...)
	for i := range out.Sites {
		out.Sites[i].CIDRs = append([]string{}, in.Sites[i].CIDRs...)
		out.Sites[i].L2Segments = append([]string{}, in.Sites[i].L2Segments...)
		out.Sites[i].Attributes = cloneStringMap(in.Sites[i].Attributes)
	}
	out.Links = append([]SDWANLink{}, in.Links...)
	for i := range out.Links {
		out.Links[i].AllowedIPs = append([]string{}, in.Links[i].AllowedIPs...)
	}
	out.Policies = append([]SDWANPolicy{}, in.Policies...)
	for i := range out.Policies {
		out.Policies[i].SourceCIDRs = append([]string{}, in.Policies[i].SourceCIDRs...)
		out.Policies[i].DestCIDRs = append([]string{}, in.Policies[i].DestCIDRs...)
		out.Policies[i].PreferLinks = append([]string{}, in.Policies[i].PreferLinks...)
		out.Policies[i].AvoidLinks = append([]string{}, in.Policies[i].AvoidLinks...)
	}
	out.Labels = cloneLabels(in.Labels)
	out.Status.Sites = append([]SDWANSiteStatus{}, in.Status.Sites...)
	out.Status.Links = append([]SDWANLinkStatus{}, in.Status.Links...)
	out.Status.Findings = append([]StatusFinding{}, in.Status.Findings...)
	return out
}

func cloneSDWANApplyPlan(in SDWANApplyPlan) SDWANApplyPlan {
	return SDWANApplyPlan{Network: in.Network, Operations: append([]SDWANOperation{}, in.Operations...)}
}

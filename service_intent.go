package ovnflow

import (
	"context"
	"net"
	"sort"
	"strconv"
	"strings"
)

const networkServiceKind = "NetworkService"

type NetworkService struct {
	Name           string
	VIPs           []ServiceVIP
	Protocol       string
	AttachToRouter string
	Owner          OwnerRef
	Labels         Labels
}

type ServiceVIP struct {
	Address  string
	Port     int
	Backends []ServiceBackend
}

type ServiceBackend struct {
	Address string
	Port    int
}

type ServicePort struct {
	Protocol string
	Port     int
}

type ServicePatch struct {
	ReplaceVIPs    bool
	VIPs           []ServiceVIP
	AddVIPs        []ServiceVIP
	RemoveVIPs     []string
	Protocol       *string
	AttachToRouter *string
	Owner          *OwnerRef
	Labels         Labels
	RemoveLabels   []string
}

type NetworkServiceRef struct {
	client *NBClient
	name   string
}

func (n *NBClient) NetworkService(name string) *NetworkServiceRef {
	return &NetworkServiceRef{client: n, name: name}
}

func (r *NetworkServiceRef) Ensure() *NetworkServiceBuilder {
	return &NetworkServiceBuilder{ref: r, spec: NetworkService{Name: r.name, Labels: Labels{}}}
}

func (r *NetworkServiceRef) Get(ctx context.Context) (*NetworkService, error) {
	if err := validateName("network service", r.name); err != nil {
		return nil, err
	}
	if r.client == nil || r.client.db == nil {
		return nil, ErrBackendUnavailable
	}
	lb, err := r.client.GetLoadBalancer(ctx, r.name)
	if err != nil {
		return nil, err
	}
	return networkServiceFromLoadBalancer(lb), nil
}

func (r *NetworkServiceRef) Delete() *NetworkServiceDeleteBuilder {
	return &NetworkServiceDeleteBuilder{ref: r}
}

func (r *NetworkServiceRef) Apply(ctx context.Context, service NetworkService) error {
	if r.client == nil || r.client.db == nil {
		return ErrBackendUnavailable
	}
	if service.Name == "" {
		service.Name = r.name
	}
	if service.Name != r.name {
		return wrap(ErrorConflict, dbOVNNorthbound, tableLoadBalancer, "apply", service.Name, "network service name does not match reference", nil)
	}
	builder := &NetworkServiceBuilder{ref: r, spec: service}
	_, err := builder.Reconcile(ctx)
	return err
}

func (r *NetworkServiceRef) Patch(ctx context.Context, patch ServicePatch) (*NetworkService, error) {
	current, err := r.Get(ctx)
	if err != nil {
		return nil, err
	}
	next := cloneNetworkService(current)
	if patch.ReplaceVIPs {
		next.VIPs = cloneServiceVIPs(patch.VIPs)
	} else {
		next.VIPs = mergeServiceVIPs(next.VIPs, patch.AddVIPs)
	}
	next.VIPs = removeServiceVIPs(next.VIPs, patch.RemoveVIPs)
	if patch.Protocol != nil {
		next.Protocol = *patch.Protocol
	}
	if patch.AttachToRouter != nil {
		next.AttachToRouter = *patch.AttachToRouter
	}
	if patch.Owner != nil {
		next.Owner = *patch.Owner
	}
	next.Labels = patchLabels(next.Labels, patch.Labels, patch.RemoveLabels)
	if err := r.Apply(ctx, next); err != nil {
		return nil, err
	}
	if err := r.client.deleteExternalIDKeys(ctx, tableLoadBalancer, r.name, conditionName(r.name), labelDeleteKeys(patch.RemoveLabels, patch.Labels)); err != nil {
		return nil, err
	}
	return &next, nil
}

type NetworkServiceBuilder struct {
	ref  *NetworkServiceRef
	spec NetworkService
}

func (b *NetworkServiceBuilder) WithVIP(address string, port int, backends ...ServiceBackend) *NetworkServiceBuilder {
	b.spec.VIPs = append(b.spec.VIPs, ServiceVIP{Address: address, Port: port, Backends: cloneServiceBackends(backends)})
	return b
}

func (b *NetworkServiceBuilder) WithVIPString(vip string, backends ...string) *NetworkServiceBuilder {
	parsed, err := parseServiceVIP(vip, backends...)
	if err == nil {
		b.spec.VIPs = append(b.spec.VIPs, parsed)
	}
	return b
}

func (b *NetworkServiceBuilder) WithProtocol(protocol string) *NetworkServiceBuilder {
	b.spec.Protocol = protocol
	return b
}

func (b *NetworkServiceBuilder) AttachToRouter(name string) *NetworkServiceBuilder {
	b.spec.AttachToRouter = name
	return b
}

func (b *NetworkServiceBuilder) WithOwner(kind, name string) *NetworkServiceBuilder {
	b.spec.Owner = OwnerRef{Kind: kind, Name: name}
	return b
}

func (b *NetworkServiceBuilder) WithOwnerID(kind, id string) *NetworkServiceBuilder {
	b.spec.Owner = OwnerRef{Kind: kind, ID: id}
	return b
}

func (b *NetworkServiceBuilder) WithLabel(key, value string) *NetworkServiceBuilder {
	if b.spec.Labels == nil {
		b.spec.Labels = Labels{}
	}
	b.spec.Labels[key] = value
	return b
}

func (b *NetworkServiceBuilder) Validate() error {
	return b.spec.Validate()
}

func (b *NetworkServiceBuilder) Plan(ctx context.Context) (Plan, error) {
	if err := b.Validate(); err != nil {
		return Plan{}, err
	}
	return Plan{Operations: []PlannedOperation{{
		Action:      IntentActionEnsure,
		Resource:    networkServiceKind,
		Name:        b.spec.Name,
		Description: "validate and plan load balancer service intent",
	}}}, nil
}

func (b *NetworkServiceBuilder) DryRun(ctx context.Context) (DryRunResult, error) {
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

func (b *NetworkServiceBuilder) Reconcile(ctx context.Context) (ReconcileResult, error) {
	plan, err := b.Plan(ctx)
	if err != nil {
		return ReconcileResult{}, err
	}
	if err := b.requireExistingLoadBalancerOwned(ctx, "ensure"); err != nil {
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
	return ReconcileResult{Plan: plan, Diff: diff, Applied: true}, nil
}

func (b *NetworkServiceBuilder) Execute(ctx context.Context) error {
	_, err := b.Reconcile(ctx)
	return err
}

func (b *NetworkServiceBuilder) reconcileOVSDB(ctx context.Context) error {
	externalIDs, err := intentExternalIDs(networkServiceKind, b.spec.Name, b.spec.Owner, b.spec.Labels)
	if err != nil {
		return err
	}
	if err := b.deleteStaleVIPs(ctx); err != nil {
		return err
	}
	lb := b.ref.client.LoadBalancer(b.spec.Name).Ensure()
	for _, vip := range normalizeNetworkService(b.spec).VIPs {
		lb.WithVIP(vip.String(), vip.BackendsString())
	}
	if b.spec.Protocol != "" {
		lb.WithProtocol(strings.ToLower(b.spec.Protocol))
	}
	if b.spec.AttachToRouter != "" {
		lb.AttachToRouter(b.spec.AttachToRouter)
	}
	for key, value := range externalIDs {
		lb.WithExternalID(key, value)
	}
	return lb.Execute(ctx)
}

func (b *NetworkServiceBuilder) requireExistingLoadBalancerOwned(ctx context.Context, op string) error {
	if b == nil || b.ref == nil || b.ref.client == nil || b.ref.client.db == nil {
		return nil
	}
	lb, err := b.ref.client.GetLoadBalancer(ctx, b.spec.Name)
	if err != nil {
		if IsKind(err, ErrorNotFound) {
			return nil
		}
		return err
	}
	return requireV2OwnedExternalIDs(lb.ExternalIDs, networkServiceKind, b.spec.Name, dbOVNNorthbound, tableLoadBalancer, op, b.spec.Name)
}

func (b *NetworkServiceBuilder) deleteStaleVIPs(ctx context.Context) error {
	current, found, err := b.current(ctx)
	if err != nil || !found {
		return err
	}
	remove := staleServiceVIPKeys(current.VIPs, b.spec.VIPs)
	if len(remove) == 0 {
		return nil
	}
	return b.ref.client.deleteMapKeys(ctx, tableLoadBalancer, b.spec.Name, colVIPs, conditionName(b.spec.Name), remove)
}

func (b *NetworkServiceBuilder) diff(ctx context.Context) (Diff, error) {
	desired := normalizeNetworkService(b.spec)
	diff := Diff{Resource: networkServiceKind, Name: desired.Name}
	current, found, err := b.current(ctx)
	if err != nil {
		return Diff{}, err
	}
	if !found {
		diff.Changes = append(diff.Changes, DiffChange{Path: "/", Before: nil, After: desired})
		return diff, nil
	}
	appendFieldDiff(&diff, "vips", serviceVIPComparable(current.VIPs), serviceVIPComparable(desired.VIPs))
	appendFieldDiff(&diff, "protocol", current.Protocol, desired.Protocol)
	appendFieldDiff(&diff, "attachToRouter", current.AttachToRouter, desired.AttachToRouter)
	appendFieldDiff(&diff, "owner", current.Owner, desired.Owner)
	appendFieldDiff(&diff, "labels", current.Labels, desired.Labels)
	return diff, nil
}

func (b *NetworkServiceBuilder) current(ctx context.Context) (NetworkService, bool, error) {
	if b.ref == nil || b.ref.client == nil || b.ref.client.db == nil {
		return NetworkService{}, false, nil
	}
	current, err := b.ref.Get(ctx)
	if err != nil {
		if IsKind(err, ErrorNotFound) {
			return NetworkService{}, false, nil
		}
		return NetworkService{}, false, err
	}
	return normalizeNetworkService(*current), true, nil
}

type NetworkServiceDeleteBuilder struct {
	ref *NetworkServiceRef
}

func (b *NetworkServiceDeleteBuilder) Plan(ctx context.Context) (Plan, error) {
	if b == nil || b.ref == nil {
		return Plan{}, wrap(ErrorValidation, dbOVNNorthbound, tableLoadBalancer, "delete", "", "network service reference is required", nil)
	}
	if err := validateName("network service", b.ref.name); err != nil {
		return Plan{}, err
	}
	return Plan{Operations: []PlannedOperation{{
		Action:      IntentActionDelete,
		Resource:    networkServiceKind,
		Name:        b.ref.name,
		Description: "delete load balancer service intent",
	}}}, nil
}

func (b *NetworkServiceDeleteBuilder) Execute(ctx context.Context) error {
	if _, err := b.Plan(ctx); err != nil {
		return err
	}
	if b.ref.client == nil || b.ref.client.db == nil {
		return ErrBackendUnavailable
	}
	lb, err := b.ref.client.GetLoadBalancer(ctx, b.ref.name)
	if err != nil {
		return err
	}
	if err := requireV2OwnedExternalIDs(lb.ExternalIDs, networkServiceKind, b.ref.name, dbOVNNorthbound, tableLoadBalancer, "delete", b.ref.name); err != nil {
		return err
	}
	return b.ref.client.LoadBalancer(b.ref.name).Delete().Execute(ctx)
}

func (s NetworkService) Validate() error {
	if err := validateName("network service", s.Name); err != nil {
		return err
	}
	if s.Protocol != "" && !validServiceProtocol(s.Protocol) {
		return wrap(ErrorValidation, dbOVNNorthbound, tableLoadBalancer, "validate", s.Name, "protocol must be tcp, udp, or sctp", nil)
	}
	if s.AttachToRouter != "" {
		if err := validateName("logical router", s.AttachToRouter); err != nil {
			return err
		}
	}
	if len(s.VIPs) == 0 {
		return wrap(ErrorValidation, dbOVNNorthbound, tableLoadBalancer, "validate", s.Name, "at least one vip is required", nil)
	}
	for _, vip := range s.VIPs {
		if err := vip.Validate(); err != nil {
			return err
		}
	}
	if s.Owner.Kind != "" || s.Owner.Name != "" || s.Owner.ID != "" {
		if err := s.Owner.Validate(); err != nil {
			return err
		}
	}
	return s.Labels.Validate()
}

func (v ServiceVIP) Validate() error {
	if net.ParseIP(v.Address) == nil {
		return wrap(ErrorValidation, dbOVNNorthbound, tableLoadBalancer, "validate", v.Address, "invalid vip address", nil)
	}
	if !validServicePort(v.Port) {
		return wrap(ErrorValidation, dbOVNNorthbound, tableLoadBalancer, "validate", v.Address, "invalid vip port", nil)
	}
	if len(v.Backends) == 0 {
		return wrap(ErrorValidation, dbOVNNorthbound, tableLoadBalancer, "validate", v.String(), "at least one backend is required", nil)
	}
	for _, backend := range v.Backends {
		if err := backend.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func (v ServiceVIP) String() string {
	return net.JoinHostPort(v.Address, strconv.Itoa(v.Port))
}

func (v ServiceVIP) BackendsString() string {
	backends := cloneServiceBackends(v.Backends)
	sort.Slice(backends, func(i, j int) bool { return backends[i].String() < backends[j].String() })
	parts := make([]string, 0, len(backends))
	for _, backend := range backends {
		parts = append(parts, backend.String())
	}
	return strings.Join(parts, ",")
}

func (b ServiceBackend) Validate() error {
	if net.ParseIP(b.Address) == nil {
		return wrap(ErrorValidation, dbOVNNorthbound, tableLoadBalancer, "validate", b.Address, "invalid backend address", nil)
	}
	if !validServicePort(b.Port) {
		return wrap(ErrorValidation, dbOVNNorthbound, tableLoadBalancer, "validate", b.Address, "invalid backend port", nil)
	}
	return nil
}

func (b ServiceBackend) String() string {
	return net.JoinHostPort(b.Address, strconv.Itoa(b.Port))
}

func validServiceProtocol(protocol string) bool {
	switch strings.ToLower(protocol) {
	case "tcp", "udp", "sctp":
		return true
	default:
		return false
	}
}

func validServicePort(port int) bool {
	return port > 0 && port <= 65535
}

func parseServiceVIP(vip string, backends ...string) (ServiceVIP, error) {
	address, port, err := parseServiceEndpoint(vip)
	if err != nil {
		return ServiceVIP{}, err
	}
	out := ServiceVIP{Address: address, Port: port}
	for _, backend := range backends {
		backendAddress, backendPort, err := parseServiceEndpoint(backend)
		if err != nil {
			return ServiceVIP{}, err
		}
		out.Backends = append(out.Backends, ServiceBackend{Address: backendAddress, Port: backendPort})
	}
	return out, nil
}

func parseServiceEndpoint(endpoint string) (string, int, error) {
	host, rawPort, err := net.SplitHostPort(endpoint)
	if err != nil {
		return "", 0, wrap(ErrorValidation, dbOVNNorthbound, tableLoadBalancer, "validate", endpoint, "endpoint must be host:port", err)
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return "", 0, wrap(ErrorValidation, dbOVNNorthbound, tableLoadBalancer, "validate", endpoint, "invalid endpoint address", nil)
	}
	port, err := strconv.Atoi(rawPort)
	if err != nil || !validServicePort(port) {
		return "", 0, wrap(ErrorValidation, dbOVNNorthbound, tableLoadBalancer, "validate", endpoint, "invalid endpoint port", err)
	}
	return ip.String(), port, nil
}

func networkServiceFromLoadBalancer(lb *LoadBalancer) *NetworkService {
	if lb == nil {
		return nil
	}
	service := &NetworkService{
		Name: lb.Name,
		VIPs: serviceVIPsFromMap(lb.VIPs),
	}
	if lb.Protocol != nil {
		service.Protocol = strings.ToLower(*lb.Protocol)
	}
	service.Owner, service.Labels = ownerAndLabelsFromExternalIDs(lb.ExternalIDs)
	return service
}

func serviceVIPsFromMap(vips map[string]string) []ServiceVIP {
	if len(vips) == 0 {
		return nil
	}
	out := make([]ServiceVIP, 0, len(vips))
	for vip, rawBackends := range vips {
		parsed, err := parseServiceVIP(vip, strings.Split(rawBackends, ",")...)
		if err != nil {
			continue
		}
		out = append(out, parsed)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].String() < out[j].String() })
	return out
}

func staleServiceVIPKeys(current, desired []ServiceVIP) []string {
	want := map[string]struct{}{}
	for _, vip := range desired {
		want[vip.String()] = struct{}{}
	}
	var remove []string
	for _, vip := range current {
		key := vip.String()
		if _, ok := want[key]; !ok {
			remove = append(remove, key)
		}
	}
	sort.Strings(remove)
	return remove
}

func normalizeNetworkService(in NetworkService) NetworkService {
	out := cloneNetworkService(&in)
	out.Protocol = strings.ToLower(out.Protocol)
	out.Labels = normalizeLabels(out.Labels)
	for i := range out.VIPs {
		out.VIPs[i].Backends = cloneServiceBackends(out.VIPs[i].Backends)
		sort.Slice(out.VIPs[i].Backends, func(a, b int) bool { return out.VIPs[i].Backends[a].String() < out.VIPs[i].Backends[b].String() })
	}
	sort.Slice(out.VIPs, func(i, j int) bool { return out.VIPs[i].String() < out.VIPs[j].String() })
	return out
}

func cloneNetworkService(in *NetworkService) NetworkService {
	if in == nil {
		return NetworkService{}
	}
	out := *in
	out.VIPs = cloneServiceVIPs(in.VIPs)
	out.Labels = cloneLabels(in.Labels)
	return out
}

func cloneServiceVIPs(in []ServiceVIP) []ServiceVIP {
	if len(in) == 0 {
		return nil
	}
	out := make([]ServiceVIP, len(in))
	for i := range in {
		out[i] = in[i]
		out[i].Backends = cloneServiceBackends(in[i].Backends)
	}
	return out
}

func cloneServiceBackends(in []ServiceBackend) []ServiceBackend {
	if len(in) == 0 {
		return nil
	}
	out := make([]ServiceBackend, len(in))
	copy(out, in)
	return out
}

func mergeServiceVIPs(current, add []ServiceVIP) []ServiceVIP {
	out := cloneServiceVIPs(current)
	index := map[string]int{}
	for i := range out {
		index[out[i].String()] = i
	}
	for _, vip := range add {
		key := vip.String()
		if i, ok := index[key]; ok {
			out[i] = vip
			continue
		}
		index[key] = len(out)
		out = append(out, vip)
	}
	return out
}

func removeServiceVIPs(current []ServiceVIP, remove []string) []ServiceVIP {
	if len(remove) == 0 {
		return cloneServiceVIPs(current)
	}
	deny := map[string]struct{}{}
	for _, value := range remove {
		deny[value] = struct{}{}
	}
	out := make([]ServiceVIP, 0, len(current))
	for _, vip := range current {
		if _, ok := deny[vip.String()]; ok {
			continue
		}
		out = append(out, vip)
	}
	return out
}

func serviceVIPComparable(vips []ServiceVIP) map[string][]string {
	if len(vips) == 0 {
		return nil
	}
	out := map[string][]string{}
	for _, vip := range vips {
		parts := make([]string, 0, len(vip.Backends))
		for _, backend := range vip.Backends {
			parts = append(parts, backend.String())
		}
		sort.Strings(parts)
		out[vip.String()] = parts
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

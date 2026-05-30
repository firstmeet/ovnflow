package ovnflow

import (
	"context"
	"testing"
)

func TestSDWANPartialMeshPlanAndApply(t *testing.T) {
	backend := NewInMemorySDWANBackend()
	client := NewSDWANClient(backend)
	ctx := context.Background()

	err := client.Network("corp-wan").Ensure().
		Layer3().
		TopologyPartialMesh().
		WithTransport(SDWANTransportWireGuard).
		WithOwner("project", "alpha").
		AddSite("edge-a", SDWANSite{Router: "edge-a", CIDRs: []string{"10.10.0.0/16"}}).
		AddSite("edge-b", SDWANSite{Router: "edge-b", CIDRs: []string{"10.20.0.0/16"}}).
		AddSite("edge-c", SDWANSite{Router: "edge-c", CIDRs: []string{"10.30.0.0/16"}}).
		AddLink(SDWANLink{From: "edge-a", To: "edge-b"}).
		AddLink(SDWANLink{From: "edge-b", To: "edge-c"}).
		AddPolicy("low-latency", SDWANPolicy{SourceSite: "edge-a", DestSite: "edge-b", PreferLinks: []string{"edge-a--edge-b"}, Priority: 100}).
		Apply(ctx)
	if err != nil {
		t.Fatalf("Apply() = %v", err)
	}

	network, err := client.Network("corp-wan").Get(ctx)
	if err != nil {
		t.Fatalf("Get() = %v", err)
	}
	if len(network.Links) != 2 {
		t.Fatalf("links = %d, want 2 explicit partial-mesh links: %#v", len(network.Links), network.Links)
	}
	if network.Status.State != ResourceStatusPresent {
		t.Fatalf("status = %q, want present", network.Status.State)
	}
	plan, ok := backend.LastPlan("corp-wan")
	if !ok {
		t.Fatal("missing last plan")
	}
	if !hasSDWANOperation(plan, "WireGuardTunnel", "edge-a--edge-b") || !hasSDWANOperation(plan, "RoutePolicy", "edge-a--edge-b") {
		t.Fatalf("plan missing tunnel/route operations: %#v", plan.Operations)
	}
}

func TestSDWANLayer2PlansOpenFlowRules(t *testing.T) {
	client := NewSDWANClient(nil)
	plan, err := client.Network("l2-wan").Ensure().
		Layer2().
		TopologyPartialMesh().
		WithTransport(SDWANTransportGeneve).
		AddSite("edge-a", SDWANSite{Router: "edge-a", L2Segments: []string{"blue"}}).
		AddSite("edge-b", SDWANSite{Router: "edge-b", L2Segments: []string{"blue"}}).
		AddLink(SDWANLink{From: "edge-a", To: "edge-b"}).
		ApplyPlan(context.Background())
	if err != nil {
		t.Fatalf("ApplyPlan() = %v", err)
	}
	if !hasSDWANOperation(plan, "OVSTunnel", "edge-a--edge-b") {
		t.Fatalf("plan missing OVS tunnel: %#v", plan.Operations)
	}
	if !hasSDWANOperation(plan, "OpenFlowRule", "edge-a--edge-b") {
		t.Fatalf("plan missing L2 OpenFlow rule: %#v", plan.Operations)
	}
}

func TestSDWANValidation(t *testing.T) {
	tests := []struct {
		name    string
		builder *SDWANNetworkBuilder
	}{
		{
			name: "one site",
			builder: NewSDWANClient(nil).Network("wan").Ensure().
				AddSite("edge-a", SDWANSite{Router: "edge-a", CIDRs: []string{"10.0.0.0/24"}}),
		},
		{
			name: "unknown policy site",
			builder: NewSDWANClient(nil).Network("wan").Ensure().
				AddSite("edge-a", SDWANSite{Router: "edge-a", CIDRs: []string{"10.0.0.0/24"}}).
				AddSite("edge-b", SDWANSite{Router: "edge-b", CIDRs: []string{"10.1.0.0/24"}}).
				AddPolicy("bad", SDWANPolicy{SourceSite: "missing"}),
		},
		{
			name: "l3 missing cidr",
			builder: NewSDWANClient(nil).Network("wan").Ensure().
				AddSite("edge-a", SDWANSite{Router: "edge-a"}).
				AddSite("edge-b", SDWANSite{Router: "edge-b", CIDRs: []string{"10.1.0.0/24"}}),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.builder.Validate(); !IsKind(err, ErrorValidation) {
				t.Fatalf("Validate() = %v, want validation", err)
			}
		})
	}
}

func TestSDWANDryRunNoopAfterApply(t *testing.T) {
	client := NewSDWANClient(NewInMemorySDWANBackend())
	ctx := context.Background()
	ensure := func() *SDWANNetworkBuilder {
		return client.Network("wan").Ensure().
			AddSite("edge-a", SDWANSite{Router: "edge-a", CIDRs: []string{"10.0.0.0/24"}}).
			AddSite("edge-b", SDWANSite{Router: "edge-b", CIDRs: []string{"10.1.0.0/24"}})
	}
	if err := ensure().Apply(ctx); err != nil {
		t.Fatalf("Apply() = %v", err)
	}
	dryRun, err := ensure().DryRun(ctx)
	if err != nil {
		t.Fatalf("DryRun() = %v", err)
	}
	if !dryRun.Diff.Empty() {
		t.Fatalf("diff = %#v, want empty", dryRun.Diff)
	}
}

func TestClientSDWANReusesDefaultBackend(t *testing.T) {
	client := &Client{}
	ctx := context.Background()
	if err := client.SDWAN().Network("wan").Ensure().
		AddSite("edge-a", SDWANSite{Router: "edge-a", CIDRs: []string{"10.0.0.0/24"}}).
		AddSite("edge-b", SDWANSite{Router: "edge-b", CIDRs: []string{"10.1.0.0/24"}}).
		Apply(ctx); err != nil {
		t.Fatalf("Apply() = %v", err)
	}
	if _, err := client.SDWAN().Network("wan").Get(ctx); err != nil {
		t.Fatalf("Get() after fresh SDWAN() = %v", err)
	}
}

func TestSDWANFullMeshAutoPlansEveryPair(t *testing.T) {
	plan, err := NewSDWANClient(nil).Network("wan").Ensure().
		TopologyFullMesh().
		AddSite("edge-a", SDWANSite{Router: "edge-a", CIDRs: []string{"10.0.0.0/24"}}).
		AddSite("edge-b", SDWANSite{Router: "edge-b", CIDRs: []string{"10.1.0.0/24"}}).
		AddSite("edge-c", SDWANSite{Router: "edge-c", CIDRs: []string{"10.2.0.0/24"}}).
		ApplyPlan(context.Background())
	if err != nil {
		t.Fatalf("ApplyPlan() = %v", err)
	}
	for _, name := range []string{"edge-a--edge-b", "edge-a--edge-c", "edge-b--edge-c"} {
		if !hasSDWANOperation(plan, "WireGuardTunnel", name) {
			t.Fatalf("plan missing full-mesh tunnel %s: %#v", name, plan.Operations)
		}
	}
}

func TestSDWANFullMeshPreservesExplicitDisabledLink(t *testing.T) {
	network, err := NewSDWANClient(nil).Network("wan").Ensure().
		TopologyFullMesh().
		AddSite("edge-a", SDWANSite{Router: "edge-a", CIDRs: []string{"10.0.0.0/24"}}).
		AddSite("edge-b", SDWANSite{Router: "edge-b", CIDRs: []string{"10.1.0.0/24"}}).
		AddSite("edge-c", SDWANSite{Router: "edge-c", CIDRs: []string{"10.2.0.0/24"}}).
		AddLink(SDWANLink{From: "edge-a", To: "edge-b", Disabled: true}).
		networkForTest(context.Background())
	if err != nil {
		t.Fatalf("networkForTest() = %v", err)
	}
	if len(network.Links) != 3 {
		t.Fatalf("links = %d, want 3: %#v", len(network.Links), network.Links)
	}
	for _, link := range network.Links {
		if link.StableName() != "edge-a--edge-b" {
			continue
		}
		if !link.Disabled || link.Enabled {
			t.Fatalf("explicit disabled link was overwritten: %#v", link)
		}
	}
	plan := planSDWANApply(network)
	if hasSDWANOperation(plan, "WireGuardTunnel", "edge-a--edge-b") || hasSDWANOperation(plan, "RoutePolicy", "edge-a--edge-b") {
		t.Fatalf("disabled full-mesh link was planned: %#v", plan.Operations)
	}
	for _, name := range []string{"edge-a--edge-c", "edge-b--edge-c"} {
		if !hasSDWANOperation(plan, "WireGuardTunnel", name) {
			t.Fatalf("plan missing enabled full-mesh tunnel %s: %#v", name, plan.Operations)
		}
	}
}

func TestSDWANPathModeDefaultsToDirectAndNormalizesLinks(t *testing.T) {
	network, err := NewSDWANClient(nil).Network("wan").Ensure().
		AddSite("edge-a", SDWANSite{Router: "edge-a", CIDRs: []string{"10.0.0.0/24"}}).
		AddSite("edge-b", SDWANSite{Router: "edge-b", CIDRs: []string{"10.1.0.0/24"}}).
		AddLink(SDWANLink{From: "edge-a", To: "edge-b"}).
		networkForTest(context.Background())
	if err != nil {
		t.Fatalf("networkForTest() = %v", err)
	}
	if network.PathMode != SDWANPathModeDirect {
		t.Fatalf("network path mode = %q, want direct", network.PathMode)
	}
	if len(network.Links) != 1 || network.Links[0].PathMode != SDWANPathModeDirect {
		t.Fatalf("link path mode not defaulted to direct: %#v", network.Links)
	}
}

func TestSDWANHubSpokeAutoRelayPlansTransitLinks(t *testing.T) {
	network, err := NewSDWANClient(nil).Network("wan").Ensure().
		TopologyHubSpoke().
		PathModeAuto().
		AddSite("edge-r", SDWANSite{Router: "edge-r", CIDRs: []string{"10.255.0.0/24"}, Relay: true}).
		AddSite("edge-a", SDWANSite{Router: "edge-a", CIDRs: []string{"10.0.0.0/24"}}).
		AddSite("edge-b", SDWANSite{Router: "edge-b", CIDRs: []string{"10.1.0.0/24"}}).
		networkForTest(context.Background())
	if err != nil {
		t.Fatalf("networkForTest() = %v", err)
	}
	if len(network.Links) != 2 {
		t.Fatalf("links = %d, want relay-spoke links: %#v", len(network.Links), network.Links)
	}
	for _, name := range []string{"edge-a--edge-r", "edge-b--edge-r"} {
		link, ok := findSDWANLinkForTest(network.Links, name)
		if !ok {
			t.Fatalf("missing auto relay link %s: %#v", name, network.Links)
		}
		if link.PathMode != SDWANPathModeAuto {
			t.Fatalf("link %s path mode = %q, want auto", name, link.PathMode)
		}
	}
	plan := planSDWANApply(network)
	for _, name := range []string{"edge-a--edge-r", "edge-b--edge-r"} {
		if !hasSDWANOperation(plan, "SDWANPath", name) {
			t.Fatalf("plan missing SDWANPath %s: %#v", name, plan.Operations)
		}
	}
}

func TestSDWANAutoPathPreservesExplicitDirectFallback(t *testing.T) {
	network, err := NewSDWANClient(nil).Network("wan").Ensure().
		TopologyHubSpoke().
		PathModeAuto().
		AddSite("edge-r", SDWANSite{Router: "edge-r", CIDRs: []string{"10.255.0.0/24"}, Transit: true}).
		AddSite("edge-a", SDWANSite{Router: "edge-a", CIDRs: []string{"10.0.0.0/24"}}).
		AddSite("edge-b", SDWANSite{Router: "edge-b", CIDRs: []string{"10.1.0.0/24"}}).
		AddLink(SDWANLink{From: "edge-a", To: "edge-b", PathMode: SDWANPathModeDirect}).
		networkForTest(context.Background())
	if err != nil {
		t.Fatalf("networkForTest() = %v", err)
	}
	direct, ok := findSDWANLinkForTest(network.Links, "edge-a--edge-b")
	if !ok {
		t.Fatalf("missing explicit direct link: %#v", network.Links)
	}
	if direct.PathMode != SDWANPathModeDirect {
		t.Fatalf("direct link path mode = %q, want direct", direct.PathMode)
	}
	plan := planSDWANApply(network)
	if hasSDWANOperation(plan, "SDWANPath", "edge-a--edge-b") {
		t.Fatalf("direct link unexpectedly planned as fallback path: %#v", plan.Operations)
	}
	for _, name := range []string{"edge-a--edge-r", "edge-b--edge-r"} {
		if !hasSDWANOperation(plan, "SDWANPath", name) {
			t.Fatalf("plan missing relay fallback path %s: %#v", name, plan.Operations)
		}
	}
}

func TestSDWANPathModeRelayDoesNotMarkSpokeDirectLinksAsRelay(t *testing.T) {
	network, err := NewSDWANClient(nil).Network("wan").Ensure().
		PathModeRelay().
		AddSite("edge-r", SDWANSite{Router: "edge-r", CIDRs: []string{"10.255.0.0/24"}, Relay: true}).
		AddSite("edge-a", SDWANSite{Router: "edge-a", CIDRs: []string{"10.0.0.0/24"}}).
		AddSite("edge-b", SDWANSite{Router: "edge-b", CIDRs: []string{"10.1.0.0/24"}}).
		AddLink(SDWANLink{From: "edge-a", To: "edge-b"}).
		AddLink(SDWANLink{From: "edge-a", To: "edge-r"}).
		networkForTest(context.Background())
	if err != nil {
		t.Fatalf("networkForTest() = %v", err)
	}
	direct, ok := findSDWANLinkForTest(network.Links, "edge-a--edge-b")
	if !ok {
		t.Fatalf("missing spoke direct link: %#v", network.Links)
	}
	if direct.PathMode != SDWANPathModeDirect {
		t.Fatalf("spoke direct link path mode = %q, want direct", direct.PathMode)
	}
	relay, ok := findSDWANLinkForTest(network.Links, "edge-a--edge-r")
	if !ok {
		t.Fatalf("missing relay link: %#v", network.Links)
	}
	if relay.PathMode != SDWANPathModeRelay {
		t.Fatalf("relay link path mode = %q, want relay", relay.PathMode)
	}
}

func TestSDWANPathModeValidation(t *testing.T) {
	tests := []struct {
		name    string
		builder *SDWANNetworkBuilder
	}{
		{
			name: "bad network path mode",
			builder: NewSDWANClient(nil).Network("wan").Ensure().
				WithPathMode(SDWANPathMode("bad")).
				AddSite("edge-a", SDWANSite{Router: "edge-a", CIDRs: []string{"10.0.0.0/24"}}).
				AddSite("edge-b", SDWANSite{Router: "edge-b", CIDRs: []string{"10.1.0.0/24"}}),
		},
		{
			name: "bad link path mode",
			builder: NewSDWANClient(nil).Network("wan").Ensure().
				AddSite("edge-a", SDWANSite{Router: "edge-a", CIDRs: []string{"10.0.0.0/24"}}).
				AddSite("edge-b", SDWANSite{Router: "edge-b", CIDRs: []string{"10.1.0.0/24"}}).
				AddLink(SDWANLink{From: "edge-a", To: "edge-b", PathMode: SDWANPathMode("bad")}),
		},
		{
			name: "relay path without relay endpoint",
			builder: NewSDWANClient(nil).Network("wan").Ensure().
				AddSite("edge-a", SDWANSite{Router: "edge-a", CIDRs: []string{"10.0.0.0/24"}}).
				AddSite("edge-b", SDWANSite{Router: "edge-b", CIDRs: []string{"10.1.0.0/24"}}).
				AddLink(SDWANLink{From: "edge-a", To: "edge-b", PathMode: SDWANPathModeRelay}),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.builder.Validate(); !IsKind(err, ErrorValidation) {
				t.Fatalf("Validate() = %v, want validation", err)
			}
		})
	}
}

func TestSDWANInMemoryBackendPreservesObservedStatus(t *testing.T) {
	backend := NewInMemorySDWANBackend()
	ctx := context.Background()
	network := SDWANNetwork{
		Name:      "wan",
		Layer:     SDWANLayerL3,
		Topology:  SDWANTopologyPartialMesh,
		Transport: SDWANTransportWireGuard,
		Sites: []SDWANSite{
			{Name: "edge-a", Router: "edge-a", CIDRs: []string{"10.0.0.0/24"}},
			{Name: "edge-b", Router: "edge-b", CIDRs: []string{"10.1.0.0/24"}},
		},
		Status: SDWANStatus{
			Sites:        []SDWANSiteStatus{{Name: "edge-a", Ready: true}},
			ResourceHash: "observed",
		},
	}
	plan := planSDWANApply(normalizeSDWANNetwork(network))
	if err := backend.ApplySDWAN(ctx, network, plan); err != nil {
		t.Fatalf("ApplySDWAN(first) = %v", err)
	}
	if err := backend.ApplySDWAN(ctx, network, plan); err != nil {
		t.Fatalf("ApplySDWAN(second) = %v", err)
	}
	got, err := backend.GetSDWAN(ctx, "wan")
	if err != nil {
		t.Fatalf("GetSDWAN() = %v", err)
	}
	if got.Status.ResourceHash != "observed" || len(got.Status.Sites) != 1 || !got.Status.Sites[0].Ready {
		t.Fatalf("status not preserved: %#v", got.Status)
	}
	if got.Status.LastApplied != 2 {
		t.Fatalf("LastApplied = %d, want 2", got.Status.LastApplied)
	}
}

func TestClientSDWANBackendInjection(t *testing.T) {
	backend := NewInMemorySDWANBackend()
	client := (&Client{}).UseSDWANBackend(backend)
	ctx := context.Background()
	if err := client.SDWAN().Network("wan").Ensure().
		AddSite("edge-a", SDWANSite{Router: "edge-a", CIDRs: []string{"10.0.0.0/24"}}).
		AddSite("edge-b", SDWANSite{Router: "edge-b", CIDRs: []string{"10.1.0.0/24"}}).
		AddLink(SDWANLink{From: "edge-a", To: "edge-b"}).
		Apply(ctx); err != nil {
		t.Fatalf("Apply() = %v", err)
	}
	if _, ok := backend.LastPlan("wan"); !ok {
		t.Fatal("custom backend did not receive ApplySDWAN")
	}
}

func (b *SDWANNetworkBuilder) networkForTest(ctx context.Context) (SDWANNetwork, error) {
	plan, err := b.ApplyPlan(ctx)
	if err != nil {
		return SDWANNetwork{}, err
	}
	_ = plan
	return normalizeSDWANNetwork(b.network), nil
}

func hasSDWANOperation(plan SDWANApplyPlan, resource, name string) bool {
	for _, op := range plan.Operations {
		if op.Resource == resource && op.Name == name {
			return true
		}
	}
	return false
}

func findSDWANLinkForTest(links []SDWANLink, name string) (SDWANLink, bool) {
	for _, link := range links {
		if link.StableName() == name {
			return link, true
		}
	}
	return SDWANLink{}, false
}

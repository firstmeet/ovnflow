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
		AddPolicy("low-latency", SDWANPolicy{SourceSite: "edge-a", DestSite: "edge-b", PreferLinks: []string{"edge-a--edge-b"}, Priority: 100}).
		Apply(ctx)
	if err != nil {
		t.Fatalf("Apply() = %v", err)
	}

	network, err := client.Network("corp-wan").Get(ctx)
	if err != nil {
		t.Fatalf("Get() = %v", err)
	}
	if len(network.Links) != 3 {
		t.Fatalf("links = %d, want 3 partial-mesh links: %#v", len(network.Links), network.Links)
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

func hasSDWANOperation(plan SDWANApplyPlan, resource, name string) bool {
	for _, op := range plan.Operations {
		if op.Resource == resource && op.Name == name {
			return true
		}
	}
	return false
}

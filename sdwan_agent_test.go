package ovnflow

import (
	"context"
	"testing"
	"time"
)

func TestInMemorySDWANControlPlaneLifecycle(t *testing.T) {
	ctx := context.Background()
	cp := NewInMemorySDWANControlPlane()
	cp.clock = func() time.Time { return time.Unix(100, 0).UTC() }

	agent, err := cp.RegisterAgent(ctx, SDWANAgent{
		ID:   "agent-a",
		Site: "edge-a",
		Capabilities: SDWANAgentCapabilities{
			Transports: []SDWANTransport{SDWANTransportWireGuard},
			Layers:     []SDWANLayer{SDWANLayerL3},
			Features:   []string{SDWANAgentFeatureWireGuard, SDWANAgentFeatureLinuxRoute},
		},
		Labels: Labels{"rack": "r1"},
	})
	if err != nil {
		t.Fatalf("RegisterAgent() = %v", err)
	}
	if agent.Status.State != ResourceStatusPresent || agent.Status.Generation != 1 {
		t.Fatalf("agent status = %#v", agent.Status)
	}

	network := SDWANNetwork{
		Name:      "wan",
		Layer:     SDWANLayerL3,
		Transport: SDWANTransportWireGuard,
		Sites: []SDWANSite{
			{Name: "edge-a", Router: "edge-a", CIDRs: []string{"10.0.0.0/24"}},
			{Name: "edge-b", Router: "edge-b", CIDRs: []string{"10.1.0.0/24"}},
		},
		Links: []SDWANLink{{From: "edge-a", To: "edge-b"}},
	}
	plan := planSDWANApply(normalizeSDWANNetwork(network))
	assignment, err := cp.AssignSDWAN(ctx, "agent-a", network, plan)
	if err != nil {
		t.Fatalf("AssignSDWAN() = %v", err)
	}
	if assignment.Status.State != SDWANAssignmentPending || assignment.Generation != 1 {
		t.Fatalf("assignment = %#v", assignment)
	}
	if assignment.Plan.Operations[0].Resource != "SDWANNetwork" {
		t.Fatalf("assignment plan = %#v", assignment.Plan)
	}

	acked, err := cp.AckAssignment(ctx, assignment.ID, SDWANAssignmentStatus{State: SDWANAssignmentApplied, Message: "ok"})
	if err != nil {
		t.Fatalf("AckAssignment() = %v", err)
	}
	if acked.Status.State != SDWANAssignmentApplied || acked.Status.AppliedAt.IsZero() {
		t.Fatalf("acked status = %#v", acked.Status)
	}

	agent, err = cp.Heartbeat(ctx, SDWANAgentHeartbeat{
		AgentID: "agent-a",
		Observed: []SDWANLinkStatus{{
			Name:      "edge-a--edge-b",
			From:      "edge-a",
			To:        "edge-b",
			Transport: SDWANTransportWireGuard,
			Ready:     true,
		}},
		Attributes: map[string]string{"kernel": "6.x"},
	})
	if err != nil {
		t.Fatalf("Heartbeat() = %v", err)
	}
	if len(agent.Status.Observed) != 1 || !agent.Status.Observed[0].Ready || agent.Status.Attributes["kernel"] != "6.x" {
		t.Fatalf("heartbeat status = %#v", agent.Status)
	}
}

func TestSDWANControlPlaneRejectsUnsupportedAssignment(t *testing.T) {
	ctx := context.Background()
	cp := NewInMemorySDWANControlPlane()
	if _, err := cp.RegisterAgent(ctx, SDWANAgent{
		ID:           "agent-a",
		Site:         "edge-a",
		Capabilities: SDWANAgentCapabilities{Transports: []SDWANTransport{SDWANTransportWireGuard}},
	}); err != nil {
		t.Fatalf("RegisterAgent() = %v", err)
	}
	network := SDWANNetwork{
		Name:      "wan",
		Layer:     SDWANLayerL3,
		Transport: SDWANTransportVXLAN,
		Sites: []SDWANSite{
			{Name: "edge-a", Router: "edge-a", CIDRs: []string{"10.0.0.0/24"}},
			{Name: "edge-b", Router: "edge-b", CIDRs: []string{"10.1.0.0/24"}},
		},
		Links: []SDWANLink{{From: "edge-a", To: "edge-b"}},
	}
	_, err := cp.AssignSDWAN(ctx, "agent-a", network, planSDWANApply(normalizeSDWANNetwork(network)))
	if !IsKind(err, ErrorUnsupported) {
		t.Fatalf("AssignSDWAN() = %v, want unsupported", err)
	}
}

func TestSDWANControlPlaneRejectsInvalidAgentAndAssignmentStatus(t *testing.T) {
	ctx := context.Background()
	cp := NewInMemorySDWANControlPlane()
	if _, err := cp.RegisterAgent(ctx, SDWANAgent{
		ID:           "agent-a",
		Site:         "edge-a",
		Capabilities: SDWANAgentCapabilities{Transports: []SDWANTransport{SDWANTransportWireGuard}, Features: []string{""}},
	}); !IsKind(err, ErrorValidation) {
		t.Fatalf("RegisterAgent() = %v, want validation", err)
	}
	if _, err := cp.RegisterAgent(ctx, SDWANAgent{
		ID:           "agent-a",
		Site:         "edge-a",
		Capabilities: SDWANAgentCapabilities{Transports: []SDWANTransport{SDWANTransportWireGuard}},
	}); err != nil {
		t.Fatalf("RegisterAgent() = %v", err)
	}
	network := SDWANNetwork{
		Name:      "wan",
		Layer:     SDWANLayerL3,
		Transport: SDWANTransportWireGuard,
		Sites: []SDWANSite{
			{Name: "edge-a", Router: "edge-a", CIDRs: []string{"10.0.0.0/24"}},
			{Name: "edge-b", Router: "edge-b", CIDRs: []string{"10.1.0.0/24"}},
		},
		Links: []SDWANLink{{From: "edge-a", To: "edge-b"}},
	}
	assignment, err := cp.AssignSDWAN(ctx, "agent-a", network, planSDWANApply(normalizeSDWANNetwork(network)))
	if err != nil {
		t.Fatalf("AssignSDWAN() = %v", err)
	}
	_, err = cp.AckAssignment(ctx, assignment.ID, SDWANAssignmentStatus{State: SDWANAssignmentStatusState("mystery")})
	if !IsKind(err, ErrorValidation) {
		t.Fatalf("AckAssignment() = %v, want validation", err)
	}
}

func TestSDWANDisabledLinkDoesNotPlanRuntimeOperations(t *testing.T) {
	plan, err := NewSDWANClient(nil).Network("wan").Ensure().
		AddSite("edge-a", SDWANSite{Router: "edge-a", CIDRs: []string{"10.0.0.0/24"}}).
		AddSite("edge-b", SDWANSite{Router: "edge-b", CIDRs: []string{"10.1.0.0/24"}}).
		AddLink(SDWANLink{From: "edge-a", To: "edge-b", Disabled: true}).
		ApplyPlan(context.Background())
	if err != nil {
		t.Fatalf("ApplyPlan() = %v", err)
	}
	if hasSDWANOperation(plan, "WireGuardTunnel", "edge-a--edge-b") || hasSDWANOperation(plan, "RoutePolicy", "edge-a--edge-b") {
		t.Fatalf("disabled link produced runtime operations: %#v", plan.Operations)
	}
}

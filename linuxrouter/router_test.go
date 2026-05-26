package linuxrouter

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/firstmeet/ovnflow"
)

func TestNATStableRuleNames(t *testing.T) {
	a := MasqueradeRule{SourceCIDR: "10.0.0.0/24", OutInterface: "wan0"}.StableName()
	b := MasqueradeRule{SourceCIDR: "10.0.0.0/24", OutInterface: "wan0"}.StableName()
	if a == "" || a != b {
		t.Fatalf("stable names = %q and %q", a, b)
	}
	named := SNATRule{Name: "egress-a", SourceCIDR: "10.0.0.0/24", OutInterface: "wan0", ToSource: "192.0.2.10"}.StableName()
	if named != "egress-a" {
		t.Fatalf("explicit stable name = %q", named)
	}
}

func TestDuplicateGeneratedAndExplicitRuleNames(t *testing.T) {
	err := (NAT{Masquerades: []MasqueradeRule{
		{SourceCIDR: "10.0.0.0/24", OutInterface: "wan0"},
		{Name: "masquerade-10-0-0-0-24-wan0", SourceCIDR: "10.0.1.0/24", OutInterface: "wan0"},
	}}).Validate()
	if !ovnflow.IsKind(err, ovnflow.ErrorValidation) {
		t.Fatalf("error kind = %q for %v, want validation", ovnflow.KindOf(err), err)
	}
}

func TestSingleWANInferencePlansSNATDNATAndPortForward(t *testing.T) {
	router := Router{
		Name: "edge",
		Spec: Spec{
			Interfaces: []Interface{
				{Name: "lan0", Role: InterfaceLAN, Addresses: []string{"10.0.0.1/24"}},
				{Name: "wan0", Role: InterfaceWAN, DHCPClient: true},
			},
			NAT: NAT{
				SNATRules:    []SNATRule{{SourceCIDR: "10.0.0.0/24", ToSource: "192.0.2.10"}},
				DNATRules:    []DNATRule{{MatchAddress: "192.0.2.20", TargetAddress: "10.0.0.20"}},
				PortForwards: []PortForwardRule{{Protocol: "tcp", ListenPort: 8080, TargetIP: "10.0.0.10", TargetPort: 80}},
				Masquerades:  []MasqueradeRule{{SourceCIDR: "10.0.1.0/24"}},
			},
		},
	}
	plan, err := router.Plan(nil)
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	if len(plan.Commands) == 0 {
		t.Fatalf("expected planned commands")
	}
	found := map[string]bool{}
	for _, command := range plan.Commands {
		if command.Program == "nat" && len(command.Args) >= 4 && command.Args[3] == "wan0" {
			found[command.Args[0]] = true
		}
		if command.Program == "nat" && len(command.Args) >= 4 && command.Args[2] == "wan0" {
			found[command.Args[0]] = true
		}
	}
	for _, kind := range []string{"snat", "dnat", "port-forward", "masquerade"} {
		if !found[kind] {
			t.Fatalf("plan did not infer wan0 for %s: %#v", kind, plan.Commands)
		}
	}
}

func TestMultipleWANInferenceIsAmbiguous(t *testing.T) {
	router := Router{
		Name: "edge",
		Spec: Spec{
			Interfaces: []Interface{
				{Name: "wan0", Role: InterfaceWAN},
				{Name: "wan1", Role: InterfaceWAN},
			},
			NAT: NAT{Masquerades: []MasqueradeRule{{SourceCIDR: "10.0.0.0/24"}}},
		},
	}
	_, err := router.Plan(nil)
	if !ovnflow.IsKind(err, ovnflow.ErrorAmbiguous) {
		t.Fatalf("error kind = %q for %v, want ambiguous", ovnflow.KindOf(err), err)
	}
}

func TestZeroWANInferenceIsAmbiguous(t *testing.T) {
	router := Router{
		Name: "edge",
		Spec: Spec{NAT: NAT{PortForwards: []PortForwardRule{{Protocol: "tcp", ListenPort: 8080, TargetIP: "10.0.0.10", TargetPort: 80}}}},
	}
	_, err := router.Plan(nil)
	if !ovnflow.IsKind(err, ovnflow.ErrorAmbiguous) {
		t.Fatalf("error kind = %q for %v, want ambiguous", ovnflow.KindOf(err), err)
	}
}

func TestSingleLANAndWANInferDestinationMap(t *testing.T) {
	router := Router{
		Name: "edge",
		Spec: Spec{
			Interfaces: []Interface{
				{Name: "lan0", Role: InterfaceLAN},
				{Name: "wan0", Role: InterfaceWAN},
			},
			NAT: NAT{DestinationMaps: []DestinationMapRule{{
				MatchAddress:  "192.168.9.2",
				TargetAddress: "192.168.0.1",
				FromCIDR:      "10.0.0.0/24",
			}}},
		},
	}
	plan, err := router.Plan(nil)
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	found := false
	for _, command := range plan.Commands {
		if command.Program == "nat" && len(command.Args) >= 4 && command.Args[0] == "destination-map" && command.Args[2] == "lan0" && command.Args[3] == "wan0" {
			found = true
		}
	}
	if !found {
		t.Fatalf("plan did not infer lan0/wan0: %#v", plan.Commands)
	}
}

func TestMultipleLANInferenceIsAmbiguous(t *testing.T) {
	router := Router{
		Name: "edge",
		Spec: Spec{
			Interfaces: []Interface{
				{Name: "lan0", Role: InterfaceLAN},
				{Name: "lan1", Role: InterfaceLAN},
				{Name: "wan0", Role: InterfaceWAN},
			},
			NAT: NAT{DestinationMaps: []DestinationMapRule{{
				MatchAddress:  "192.168.9.2",
				TargetAddress: "192.168.0.1",
			}}},
		},
	}
	_, err := router.Plan(nil)
	if !ovnflow.IsKind(err, ovnflow.ErrorAmbiguous) {
		t.Fatalf("error kind = %q for %v, want ambiguous", ovnflow.KindOf(err), err)
	}
}

func TestZeroLANInferenceIsAmbiguous(t *testing.T) {
	router := Router{
		Name: "edge",
		Spec: Spec{
			Interfaces: []Interface{{Name: "wan0", Role: InterfaceWAN}},
			NAT: NAT{DestinationMaps: []DestinationMapRule{{
				MatchAddress:  "192.168.9.2",
				TargetAddress: "192.168.0.1",
			}}},
		},
	}
	_, err := router.Plan(nil)
	if !ovnflow.IsKind(err, ovnflow.ErrorAmbiguous) {
		t.Fatalf("error kind = %q for %v, want ambiguous", ovnflow.KindOf(err), err)
	}
}

func TestGetApplyPatchSkeleton(t *testing.T) {
	ctx := context.Background()
	fake := &FakeExecutor{}
	client := NewClient(fake, nil)
	ref := client.Router("edge")
	_, err := ref.Get(ctx)
	if !errors.Is(err, ovnflow.ErrNotFound) {
		t.Fatalf("Get before Apply error = %v, want not found", err)
	}
	router := Router{
		Name: "edge",
		Spec: Spec{Interfaces: []Interface{{Name: "wan0", Role: InterfaceWAN}}},
	}
	if err := ref.Apply(ctx, router); err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if len(fake.Snapshot()) == 0 {
		t.Fatalf("Apply did not render commands")
	}
	got, err := ref.Patch(ctx, Patch{NAT: NATPatch{Add: NAT{Masquerades: []MasqueradeRule{{SourceCIDR: "10.0.0.0/24"}}}}})
	if err != nil {
		t.Fatalf("Patch returned error: %v", err)
	}
	if len(got.Spec.NAT.Masquerades) != 1 {
		t.Fatalf("patched router = %#v", got)
	}
}

func TestApplyNameMismatchReturnsConflict(t *testing.T) {
	ref := NewClient(nil, nil).Router("edge")
	err := ref.Apply(context.Background(), Router{Name: "other"})
	if !ovnflow.IsKind(err, ovnflow.ErrorConflict) {
		t.Fatalf("error kind = %q for %v, want conflict", ovnflow.KindOf(err), err)
	}
}

func TestClientAndFakeExecutorAreConcurrencySafe(t *testing.T) {
	ctx := context.Background()
	fake := &FakeExecutor{}
	client := NewClient(fake, nil)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ref := client.Router("edge")
			_ = ref.Apply(ctx, Router{Name: "edge", Spec: Spec{Interfaces: []Interface{{Name: "wan0", Role: InterfaceWAN}}}})
			_, _ = ref.Get(ctx)
		}()
	}
	wg.Wait()
	if len(fake.Snapshot()) == 0 {
		t.Fatalf("expected recorded fake commands")
	}
}

func TestGetAndApplyDeepCopyRouterState(t *testing.T) {
	ctx := context.Background()
	client := NewClient(nil, nil)
	ref := client.Router("edge")
	original := Router{
		Name: "edge",
		Spec: Spec{
			Labels:     ovnflow.Labels{"env": "test"},
			Interfaces: []Interface{{Name: "wan0", Role: InterfaceWAN, Addresses: []string{"192.0.2.2/24"}}},
			DNSMasq:    DNSMasq{Hosts: []HostRecord{{Domain: "api.service", IPs: []string{"10.0.0.2"}}}},
			Firewall:   Firewall{Rules: []FirewallRule{{Name: "allow-web", CIDRs: []string{"10.0.0.0/24"}, Ports: []int{80}}}},
		},
		Status: Status{
			Interfaces:   []InterfaceStatus{{Name: "wan0", Addresses: []string{"192.0.2.2/24"}}},
			InstalledNAT: []string{"nat-a"},
		},
	}
	if err := ref.Apply(ctx, original); err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	original.Spec.Labels["env"] = "mutated"
	original.Spec.Interfaces[0].Addresses[0] = "198.51.100.2/24"
	original.Spec.DNSMasq.Hosts[0].IPs[0] = "10.0.0.99"
	original.Spec.Firewall.Rules[0].Ports[0] = 443

	got, err := ref.Get(ctx)
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if got.Spec.Labels["env"] != "test" || got.Spec.Interfaces[0].Addresses[0] != "192.0.2.2/24" || got.Spec.DNSMasq.Hosts[0].IPs[0] != "10.0.0.2" || got.Spec.Firewall.Rules[0].Ports[0] != 80 {
		t.Fatalf("stored router was mutated through caller-owned input: %#v", got)
	}
	got.Spec.Labels["env"] = "returned-mutated"
	got.Spec.Interfaces[0].Addresses[0] = "203.0.113.2/24"
	got.Status.InstalledNAT[0] = "mutated"

	again, err := ref.Get(ctx)
	if err != nil {
		t.Fatalf("second Get returned error: %v", err)
	}
	if again.Spec.Labels["env"] != "test" || again.Spec.Interfaces[0].Addresses[0] != "192.0.2.2/24" || again.Status.InstalledNAT[0] != "nat-a" {
		t.Fatalf("stored router was mutated through Get result: %#v", again)
	}
}

func TestOwnerAndLabelsValidate(t *testing.T) {
	router := Router{Name: "edge", Spec: Spec{
		Owner:  ovnflow.OwnerRef{Kind: "project"},
		Labels: ovnflow.Labels{"": "bad"},
	}}
	if !ovnflow.IsKind(router.Validate(), ovnflow.ErrorValidation) {
		t.Fatalf("invalid owner should fail validation")
	}
	router.Spec.Owner = ovnflow.OwnerRef{Kind: "project", Name: "alpha"}
	if !ovnflow.IsKind(router.Validate(), ovnflow.ErrorValidation) {
		t.Fatalf("invalid labels should fail validation")
	}
	router.Spec.Labels = ovnflow.Labels{"env": "test"}
	if err := router.Validate(); err != nil {
		t.Fatalf("valid owner and labels returned error: %v", err)
	}
}

func TestPatchPreconditionsAndOwnership(t *testing.T) {
	ctx := context.Background()
	ref := NewClient(nil, nil).Router("edge")
	router := Router{
		Name:   "edge",
		Spec:   Spec{Owner: ovnflow.OwnerRef{Kind: "project", Name: "alpha"}},
		Status: Status{ResourceVersion: "rv1", ObservedHash: "hash1"},
	}
	if err := ref.Apply(ctx, router); err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	_, err := ref.Patch(ctx, Patch{Preconditions: PatchPreconditions{ResourceVersion: "stale"}})
	if !ovnflow.IsKind(err, ovnflow.ErrorConflict) {
		t.Fatalf("stale resource version kind = %q for %v, want conflict", ovnflow.KindOf(err), err)
	}
	_, err = ref.Patch(ctx, Patch{
		Options:       PatchOptions{RequireOwnership: true},
		Preconditions: PatchPreconditions{Owner: ovnflow.OwnerRef{Kind: "project", Name: "other"}},
	})
	if !ovnflow.IsKind(err, ovnflow.ErrorOwnershipViolation) {
		t.Fatalf("ownership kind = %q for %v, want ownership_violation", ovnflow.KindOf(err), err)
	}
}

func TestDNSMasqHostRecordAllowsMultipleIPs(t *testing.T) {
	record := HostRecord{Domain: "api.service", IPs: []string{"10.0.0.2", "10.0.0.3"}}
	router := Router{Name: "edge", Spec: Spec{Interfaces: []Interface{{Name: "lan0", Role: InterfaceLAN}}, DNSMasq: DNSMasq{Enabled: true, Hosts: []HostRecord{record}}}}
	if err := router.Validate(); err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
}

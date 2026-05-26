package linuxrouter

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/firstmeet/ovnflow/v2"
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
				SourceNAT:     "192.0.2.10",
			}}},
		},
	}
	plan, err := router.Plan(nil)
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	found := false
	for _, command := range plan.Commands {
		if command.Program == "nat" && len(command.Args) == 8 && command.Args[0] == "destination-map" && command.Args[2] == "lan0" && command.Args[3] == "wan0" && command.Args[6] == "10.0.0.0/24" && command.Args[7] == "192.0.2.10" {
			found = true
		}
	}
	if !found {
		t.Fatalf("plan did not infer lan0/wan0: %#v", plan.Commands)
	}
}

func TestPortForwardRenderIncludesListenIP(t *testing.T) {
	router := Router{
		Name: "edge",
		Spec: Spec{
			Interfaces: []Interface{{Name: "wan0", Role: InterfaceWAN}},
			NAT: NAT{PortForwards: []PortForwardRule{{
				Name:        "web",
				Protocol:    "tcp",
				ListenIP:    "192.0.2.10",
				ListenPort:  8080,
				TargetIP:    "10.0.0.10",
				TargetPort:  80,
				InInterface: "wan0",
			}}},
		},
	}
	plan, err := router.Plan(nil)
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	for _, command := range plan.Commands {
		if command.Program == "nat" && command.Args[0] == "port-forward" {
			if len(command.Args) != 8 || command.Args[4] != "192.0.2.10" {
				t.Fatalf("port-forward args = %#v, want listen ip at arg 4", command.Args)
			}
			return
		}
	}
	t.Fatalf("missing port-forward command: %#v", plan.Commands)
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

func TestPatchDeleteMissingRuleReturnsNotFound(t *testing.T) {
	ctx := context.Background()
	ref := NewClient(nil, nil).Router("edge")
	if err := ref.Apply(ctx, Router{
		Name: "edge",
		Spec: Spec{
			Interfaces: []Interface{{Name: "wan0", Role: InterfaceWAN}},
			NAT:        NAT{Masquerades: []MasqueradeRule{{Name: "egress", SourceCIDR: "10.0.0.0/24", OutInterface: "wan0"}}},
		},
	}); err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	_, err := ref.Patch(ctx, Patch{NAT: NATPatch{Delete: NATDelete{Masquerades: []string{"missing"}}}})
	if !ovnflow.IsKind(err, ovnflow.ErrorNotFound) {
		t.Fatalf("delete missing error kind = %q for %v, want not_found", ovnflow.KindOf(err), err)
	}
}

func TestPatchDeleteMissingRuleCanIgnoreNotFound(t *testing.T) {
	ctx := context.Background()
	ref := NewClient(nil, nil).Router("edge")
	if err := ref.Apply(ctx, Router{Name: "edge", Spec: Spec{Interfaces: []Interface{{Name: "wan0", Role: InterfaceWAN}}}}); err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	got, err := ref.Patch(ctx, Patch{
		Options: PatchOptions{IgnoreNotFound: true},
		NAT:     NATPatch{Delete: NATDelete{Masquerades: []string{"missing"}}},
	})
	if err != nil {
		t.Fatalf("Patch returned error: %v", err)
	}
	if len(got.Spec.NAT.Masquerades) != 0 {
		t.Fatalf("unexpected NAT state: %#v", got.Spec.NAT)
	}
}

func TestPatchDeleteExistingResourcesByStableName(t *testing.T) {
	ctx := context.Background()
	ref := NewClient(nil, nil).Router("edge")
	router := Router{
		Name: "edge",
		Spec: Spec{
			Interfaces: []Interface{
				{Name: "lan0", Role: InterfaceLAN},
				{Name: "wan0", Role: InterfaceWAN},
				{Name: "wan1", Role: InterfaceWAN},
			},
			Routes: []Route{
				{Name: "default", Destination: "0.0.0.0/0", Gateway: "192.0.2.1"},
				{Name: "private", Destination: "10.0.0.0/8", Gateway: "10.0.0.1"},
			},
			DNSMasq: DNSMasq{Hosts: []HostRecord{
				{Domain: "api.service", IPs: []string{"10.0.0.10"}},
				{Domain: "db.service", IPs: []string{"10.0.0.11"}},
			}},
			NAT: NAT{
				SNATRules:       []SNATRule{{Name: "snat-a", SourceCIDR: "10.0.0.0/24", OutInterface: "wan0", ToSource: "192.0.2.10"}, {Name: "snat-b", SourceCIDR: "10.0.1.0/24", OutInterface: "wan1", ToSource: "192.0.2.11"}},
				Masquerades:     []MasqueradeRule{{Name: "masq-a", SourceCIDR: "10.0.2.0/24", OutInterface: "wan0"}, {Name: "masq-b", SourceCIDR: "10.0.3.0/24", OutInterface: "wan1"}},
				DNATRules:       []DNATRule{{Name: "dnat-a", MatchAddress: "192.0.2.20", TargetAddress: "10.0.0.20", InInterface: "wan0"}, {Name: "dnat-b", MatchAddress: "192.0.2.21", TargetAddress: "10.0.0.21", InInterface: "wan1"}},
				PortForwards:    []PortForwardRule{{Name: "pf-a", Protocol: "tcp", InInterface: "wan0", ListenPort: 8080, TargetIP: "10.0.0.30", TargetPort: 80}, {Name: "pf-b", Protocol: "udp", InInterface: "wan1", ListenPort: 5353, TargetIP: "10.0.0.31", TargetPort: 53}},
				DestinationMaps: []DestinationMapRule{{Name: "map-a", MatchAddress: "192.168.9.2", TargetAddress: "192.168.0.1", FromCIDR: "10.0.0.0/24", InInterface: "lan0", OutInterface: "wan0", SourceNAT: "192.0.2.10"}, {Name: "map-b", MatchAddress: "192.168.9.3", TargetAddress: "192.168.0.2", InInterface: "lan0", OutInterface: "wan1"}},
			},
			Firewall: Firewall{Rules: []FirewallRule{
				{Name: "allow-web", Action: "allow", Ports: []int{80}},
				{Name: "drop-ssh", Action: "drop", Ports: []int{22}},
			}},
		},
	}
	if err := ref.Apply(ctx, router); err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	got, err := ref.Patch(ctx, Patch{
		Interfaces: InterfacePatch{Delete: []string{"wan1"}},
		Routes:     RoutePatch{Delete: []string{"private"}},
		DNSMasq:    DNSMasqPatch{DeleteHosts: []string{"db.service"}},
		NAT: NATPatch{Delete: NATDelete{
			SNATRules:       []string{"snat-b"},
			Masquerades:     []string{"masq-b"},
			DNATRules:       []string{"dnat-b"},
			PortForwards:    []string{"pf-b"},
			DestinationMaps: []string{"map-b"},
		}},
		Firewall: FirewallPatch{DeleteRules: []string{"drop-ssh"}},
	})
	if err != nil {
		t.Fatalf("Patch returned error: %v", err)
	}
	if len(got.Spec.Interfaces) != 2 || len(got.Spec.Routes) != 1 || len(got.Spec.DNSMasq.Hosts) != 1 || len(got.Spec.Firewall.Rules) != 1 {
		t.Fatalf("unexpected patched router: %#v", got.Spec)
	}
	if len(got.Spec.NAT.SNATRules) != 1 || got.Spec.NAT.SNATRules[0].Name != "snat-a" ||
		len(got.Spec.NAT.Masquerades) != 1 || got.Spec.NAT.Masquerades[0].Name != "masq-a" ||
		len(got.Spec.NAT.DNATRules) != 1 || got.Spec.NAT.DNATRules[0].Name != "dnat-a" ||
		len(got.Spec.NAT.PortForwards) != 1 || got.Spec.NAT.PortForwards[0].Name != "pf-a" ||
		len(got.Spec.NAT.DestinationMaps) != 1 || got.Spec.NAT.DestinationMaps[0].Name != "map-a" {
		t.Fatalf("unexpected NAT after delete: %#v", got.Spec.NAT)
	}
}

func TestPatchApplyToFailureDoesNotPartiallyMutateRouter(t *testing.T) {
	router := Router{
		Name: "edge",
		Spec: Spec{
			Interfaces: []Interface{{Name: "wan0", Role: InterfaceWAN}},
			DNSMasq:    DNSMasq{Hosts: []HostRecord{{Domain: "api.service", IPs: []string{"10.0.0.10"}}}},
		},
	}
	err := (Patch{
		DNSMasq: DNSMasqPatch{
			Replace:     &DNSMasq{Hosts: []HostRecord{{Domain: "new.service", IPs: []string{"10.0.0.20"}}}},
			DeleteHosts: []string{"missing.service"},
		},
	}).ApplyTo(&router)
	if !ovnflow.IsKind(err, ovnflow.ErrorNotFound) {
		t.Fatalf("ApplyTo error kind = %q for %v, want not_found", ovnflow.KindOf(err), err)
	}
	if len(router.Spec.DNSMasq.Hosts) != 1 || router.Spec.DNSMasq.Hosts[0].Domain != "api.service" {
		t.Fatalf("router was partially mutated after failed ApplyTo: %#v", router)
	}
}

func TestPatchResultDoesNotAliasPatchPayload(t *testing.T) {
	ctx := context.Background()
	ref := NewClient(nil, nil).Router("edge")
	if err := ref.Apply(ctx, Router{Name: "edge", Spec: Spec{Interfaces: []Interface{{Name: "wan0", Role: InterfaceWAN}}}}); err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	ifaceAddresses := []string{"192.0.2.2/24"}
	hostIPs := []string{"10.0.0.10"}
	rulePorts := []int{80}
	got, err := ref.Patch(ctx, Patch{
		Interfaces: InterfacePatch{Add: []Interface{{Name: "lan0", Role: InterfaceLAN, Addresses: ifaceAddresses}}},
		DNSMasq:    DNSMasqPatch{AddHosts: []HostRecord{{Domain: "api.service", IPs: hostIPs}}},
		Firewall:   FirewallPatch{AddRules: []FirewallRule{{Name: "allow-web", Action: "allow", Ports: rulePorts}}},
	})
	if err != nil {
		t.Fatalf("Patch returned error: %v", err)
	}
	ifaceAddresses[0] = "198.51.100.2/24"
	hostIPs[0] = "10.0.0.99"
	rulePorts[0] = 443
	if got.Spec.Interfaces[1].Addresses[0] != "192.0.2.2/24" || got.Spec.DNSMasq.Hosts[0].IPs[0] != "10.0.0.10" || got.Spec.Firewall.Rules[0].Ports[0] != 80 {
		t.Fatalf("Patch result aliases caller-owned payload: %#v", got)
	}
	stored, err := ref.Get(ctx)
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if stored.Spec.Interfaces[1].Addresses[0] != "192.0.2.2/24" || stored.Spec.DNSMasq.Hosts[0].IPs[0] != "10.0.0.10" || stored.Spec.Firewall.Rules[0].Ports[0] != 80 {
		t.Fatalf("stored router aliases caller-owned payload: %#v", stored)
	}
}

func TestConcurrentPatchPreservesIndependentAdds(t *testing.T) {
	ctx := context.Background()
	ref := NewClient(nil, nil).Router("edge")
	if err := ref.Apply(ctx, Router{Name: "edge", Spec: Spec{Interfaces: []Interface{{Name: "wan0", Role: InterfaceWAN}}}}); err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for i := 0; i < 20; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := ref.Patch(ctx, Patch{NAT: NATPatch{Add: NAT{Masquerades: []MasqueradeRule{{
				Name:       fmt.Sprintf("egress-%02d", i),
				SourceCIDR: fmt.Sprintf("10.%d.0.0/16", i),
			}}}}})
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("Patch returned error: %v", err)
		}
	}
	got, err := ref.Get(ctx)
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if len(got.Spec.NAT.Masquerades) != 20 {
		t.Fatalf("patches lost updates: got %d masquerades, want 20: %#v", len(got.Spec.NAT.Masquerades), got.Spec.NAT.Masquerades)
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
			Firewall:   Firewall{Rules: []FirewallRule{{Name: "allow-web", Action: "allow", CIDRs: []string{"10.0.0.0/24"}, Ports: []int{80}}}},
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

func TestGetRefreshesObservedStatus(t *testing.T) {
	ctx := context.Background()
	observerCalls := 0
	client := NewObservedClient(nil, nil, ObserverFunc(func(context.Context, Router) (Status, error) {
		observerCalls++
		return Status{
			Exists:            true,
			Namespace:         "ovnflow-edge",
			Interfaces:        []InterfaceStatus{{Name: "wan0", Role: InterfaceWAN, Addresses: []string{"192.0.2.2/24"}, Up: true}},
			Routes:            []RouteStatus{{Destination: "0.0.0.0/0", Gateway: "192.0.2.1", Interface: "wan0"}},
			DNSMasq:           DNSMasqStatus{Running: true, PID: 1234},
			NATBackend:        ovnflow.NATBackendNFTables,
			InstalledNAT:      []string{"egress", "egress"},
			InstalledFirewall: []string{"allow-web"},
		}, nil
	}))
	ref := client.Router("edge")
	if err := ref.Apply(ctx, Router{Name: "edge", Spec: Spec{Interfaces: []Interface{{Name: "wan0", Role: InterfaceWAN}}}}); err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	got, err := ref.Get(ctx)
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if observerCalls == 0 {
		t.Fatalf("observer was not called")
	}
	if !got.Status.Exists || got.Status.Namespace != "ovnflow-edge" || got.Status.Interfaces[0].Role != InterfaceWAN || got.Status.Routes[0].Gateway != "192.0.2.1" || !got.Status.DNSMasq.Running {
		t.Fatalf("unexpected observed status: %#v", got.Status)
	}
	if got.Status.ObservedHash == "" || got.Status.ResourceVersion == "" {
		t.Fatalf("observed status missing hashes: %#v", got.Status)
	}
	if len(got.Status.InstalledNAT) != 1 || got.Status.InstalledNAT[0] != "egress" {
		t.Fatalf("installed nat was not normalized: %#v", got.Status.InstalledNAT)
	}
	got.Status.Interfaces[0].Addresses[0] = "mutated"
	again, err := ref.Get(ctx)
	if err != nil {
		t.Fatalf("second Get returned error: %v", err)
	}
	if again.Status.Interfaces[0].Addresses[0] != "192.0.2.2/24" {
		t.Fatalf("observed status aliases returned value: %#v", again.Status)
	}
}

func TestGetStoresObserverErrorInStatus(t *testing.T) {
	ctx := context.Background()
	client := NewObservedClient(nil, nil, ObserverFunc(func(context.Context, Router) (Status, error) {
		return Status{}, errors.New("probe failed")
	}))
	ref := client.Router("edge")
	if err := ref.Apply(ctx, Router{Name: "edge"}); err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	got, err := ref.Get(ctx)
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if got.Status.LastError == "" || got.Status.Namespace != "ovnflow-edge" {
		t.Fatalf("observer error was not captured in status: %#v", got.Status)
	}
}

func TestStaleObserverResultDoesNotOverwriteNewerSpec(t *testing.T) {
	ctx := context.Background()
	observing := make(chan struct{})
	release := make(chan struct{})
	client := NewObservedClient(nil, nil, ObserverFunc(func(context.Context, Router) (Status, error) {
		close(observing)
		<-release
		return Status{Exists: true, Namespace: "old-observation"}, nil
	}))
	ref := client.Router("edge")
	client.store["edge"] = Router{Name: "edge"}
	errs := make(chan error, 1)
	go func() {
		_, err := ref.Get(ctx)
		errs <- err
	}()
	<-observing
	client.mu.Lock()
	current := client.store["edge"]
	current.Spec.Labels = ovnflow.Labels{"version": "new"}
	client.store["edge"] = current
	client.mu.Unlock()
	close(release)
	if err := <-errs; err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	client.mu.RLock()
	stored := cloneRouter(client.store["edge"])
	client.mu.RUnlock()
	if stored.Spec.Labels["version"] != "new" {
		t.Fatalf("newer spec was overwritten: %#v", stored)
	}
	if stored.Status.Namespace == "old-observation" {
		t.Fatalf("stale observer status overwrote newer spec: %#v", stored.Status)
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

func TestDNSMasqServersAllowHostOrDomainSpecificSyntax(t *testing.T) {
	router := Router{Name: "edge", Spec: Spec{DNSMasq: DNSMasq{Servers: []string{"223.5.5.5", "/corp.local/10.0.0.53", "dns.internal"}}}}
	if err := router.Validate(); err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
}

func TestDNSMasqValidationRejectsInvalidInputs(t *testing.T) {
	tests := []DNSMasq{
		{DHCPRanges: []DHCPRange{{Start: "bad", End: "10.0.0.20"}}},
		{Servers: []string{""}},
		{Servers: []string{"dns server"}},
		{Leases: []StaticLease{{MAC: "bad", IP: "10.0.0.10"}}},
		{Leases: []StaticLease{{MAC: "00:11:22:33:44:55", IP: "bad"}}},
		{Hosts: []HostRecord{{Domain: "", IPs: []string{"10.0.0.10"}}}},
		{Hosts: []HostRecord{{Domain: "api.service"}}},
		{Hosts: []HostRecord{{Domain: "api.service", IPs: []string{"bad"}}}},
	}
	for _, dnsmasq := range tests {
		router := Router{Name: "edge", Spec: Spec{DNSMasq: dnsmasq}}
		if !ovnflow.IsKind(router.Validate(), ovnflow.ErrorValidation) {
			t.Fatalf("router with dnsmasq %#v should fail validation", dnsmasq)
		}
	}
}

func TestFirewallActionDefaultsToAllow(t *testing.T) {
	router := Router{Name: "edge", Spec: Spec{Firewall: Firewall{Rules: []FirewallRule{{Name: "web", Ports: []int{80}}}}}}
	if err := router.Validate(); err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
}

func TestFirewallValidationRejectsInvalidInputs(t *testing.T) {
	tests := []Firewall{
		{Rules: []FirewallRule{{Name: "bad-action", Action: "pass"}}},
		{Rules: []FirewallRule{{Name: "bad-direction", Action: "allow", Direction: "sideways"}}},
		{Rules: []FirewallRule{{Name: "bad-protocol", Action: "allow", Protocol: "sctp"}}},
		{Rules: []FirewallRule{{Name: "bad-cidr", Action: "allow", CIDRs: []string{"bad"}}}},
		{Rules: []FirewallRule{{Name: "bad-port", Action: "allow", Ports: []int{65536}}}},
	}
	for _, firewall := range tests {
		router := Router{Name: "edge", Spec: Spec{Firewall: firewall}}
		if !ovnflow.IsKind(router.Validate(), ovnflow.ErrorValidation) {
			t.Fatalf("router with firewall %#v should fail validation", firewall)
		}
	}
}

func TestNATValidationRejectsInvalidPortForward(t *testing.T) {
	tests := []PortForwardRule{
		{Name: "bad-proto", Protocol: "icmp", ListenPort: 8080, TargetIP: "10.0.0.10", TargetPort: 80},
		{Name: "bad-listen-ip", Protocol: "tcp", ListenIP: "bad", ListenPort: 8080, TargetIP: "10.0.0.10", TargetPort: 80},
		{Name: "bad-port", Protocol: "tcp", ListenPort: 70000, TargetIP: "10.0.0.10", TargetPort: 80},
	}
	for _, rule := range tests {
		router := Router{Name: "edge", Spec: Spec{NAT: NAT{PortForwards: []PortForwardRule{rule}}}}
		if !ovnflow.IsKind(router.Validate(), ovnflow.ErrorValidation) {
			t.Fatalf("router with port forward %#v should fail validation", rule)
		}
	}
}

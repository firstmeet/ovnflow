//go:build linux && integration

package linuxrouter

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"

	"github.com/firstmeet/ovnflow"
)

func TestIntegrationLinuxRouterNamespaceLifecycle(t *testing.T) {
	if !ovnflow.EnvGateEnabled(os.Getenv(ovnflow.EnvLinuxRouterChecks)) {
		t.Skip(ovnflow.EnvLinuxRouterChecks + " not enabled")
	}
	if os.Geteuid() != 0 {
		t.Fatal(ovnflow.EnvLinuxRouterChecks + " requires root or equivalent CAP_NET_ADMIN")
	}
	rawBackend := os.Getenv(ovnflow.EnvLinuxRouterNATBackend)
	if !ovnflow.ValidNATBackend(rawBackend) {
		t.Fatalf("invalid %s value %q", ovnflow.EnvLinuxRouterNATBackend, rawBackend)
	}
	backend := normalizedNATBackend(rawBackend)
	requireCommand(t, "ip")
	if backend == ovnflow.NATBackendIPTables {
		requireCommand(t, "iptables")
		requireCommand(t, "iptables-save")
	} else {
		requireCommand(t, "nft")
	}
	ns := "ovnflow-it-" + strconv.FormatInt(time.Now().UTC().UnixNano(), 10)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_ = (SystemExecutor{}).Run(cleanupCtx, Command{Program: "ip", Args: []string{"netns", "delete", ns}, IgnoreNotFound: true})
	})
	client := NewObservedClient(SystemExecutor{}, LinuxRenderer{NATBackend: backend}, LinuxObserver{NATBackend: backend})
	err := client.Router("edge").Apply(ctx, Router{
		Name: "edge",
		Spec: Spec{
			Namespace:  ns,
			Interfaces: []Interface{{Name: "lo", Role: InterfaceLAN, Addresses: []string{"127.0.0.1/8"}}},
			NAT: NAT{
				Masquerades:  []MasqueradeRule{{Name: "it-masq", SourceCIDR: "127.0.0.0/8", OutInterface: "lo"}},
				SNATRules:    []SNATRule{{Name: "it-snat", SourceCIDR: "127.0.0.0/8", OutInterface: "lo", ToSource: "127.0.0.1"}},
				DNATRules:    []DNATRule{{Name: "it-dnat", MatchAddress: "127.0.0.2", TargetAddress: "127.0.0.1", InInterface: "lo"}},
				PortForwards: []PortForwardRule{{Name: "it-pf", Protocol: "tcp", InInterface: "lo", ListenIP: "127.0.0.3", ListenPort: 8080, TargetIP: "127.0.0.1", TargetPort: 80}},
				DestinationMaps: []DestinationMapRule{{
					Name:          "it-map",
					MatchAddress:  "127.0.0.4",
					TargetAddress: "127.0.0.1",
					FromCIDR:      "127.0.0.0/8",
					InInterface:   "lo",
					OutInterface:  "lo",
					SourceNAT:     "127.0.0.1",
				}},
			},
			Firewall: Firewall{Rules: []FirewallRule{{Name: "it-allow-loopback", Action: "allow", Direction: "forward", Protocol: "tcp", Ports: []int{80}}}},
		},
	})
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if err := exec.CommandContext(ctx, "ip", "netns", "exec", ns, "ip", "link", "show", "lo").Run(); err != nil {
		t.Fatalf("namespace %s did not contain loopback: %v", ns, err)
	}
	got, err := client.Router("edge").Get(ctx)
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if !got.Status.Exists || got.Status.Namespace != ns || got.Status.ObservedHash == "" || got.Status.ResourceVersion == "" {
		t.Fatalf("unexpected observed status: %#v", got.Status)
	}
	foundLoopback := false
	for _, iface := range got.Status.Interfaces {
		if iface.Name == "lo" && iface.Up {
			foundLoopback = true
		}
	}
	if !foundLoopback {
		t.Fatalf("observed interfaces missing loopback: %#v", got.Status.Interfaces)
	}
	for _, name := range []string{"it-masq", "it-snat", "it-dnat", "it-pf", "it-map", "it-map-snat"} {
		if !hasString(got.Status.InstalledNAT, name) {
			t.Fatalf("observed NAT missing %s: %#v", name, got.Status.InstalledNAT)
		}
	}
	if !hasString(got.Status.InstalledFirewall, "it-allow-loopback") {
		t.Fatalf("observed firewall missing rule: %#v", got.Status.InstalledFirewall)
	}
}

func requireCommand(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Fatalf("%s not available: %v", name, err)
	}
}

func hasString(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

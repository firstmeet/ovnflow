//go:build linux

package linuxrouter

import (
	"context"
	"errors"
	"testing"

	"github.com/firstmeet/ovnflow"
)

func TestLinuxRendererRendersNamespaceInterfacesDNSMasqAndNFTables(t *testing.T) {
	router := Router{
		Name: "edge",
		Spec: Spec{
			Namespace: "ovnflow-edge",
			Interfaces: []Interface{
				{Name: "lan0", Role: InterfaceLAN, Bridge: "br-int", OVSPort: "edge-lan", Addresses: []string{"172.16.100.1/24"}},
				{Name: "wan0", Role: InterfaceWAN, Bridge: "br-ex", OVSPort: "edge-wan", DHCPClient: true},
			},
			Routes: []Route{{Name: "default", Destination: "0.0.0.0/0", Gateway: "172.17.100.1", Interface: "wan0"}},
			DNSMasq: DNSMasq{
				Enabled:    true,
				ConfigFile: "/run/ovnflow/edge/dnsmasq.conf",
				DHCPRanges: []DHCPRange{{Start: "172.16.100.100", End: "172.16.100.200", Lease: "12h"}},
				Servers:    []string{"223.5.5.5", "/corp.local/172.16.100.10"},
				Hosts:      []HostRecord{{Domain: "api.service", IPs: []string{"172.16.100.6", "172.16.100.7"}}},
			},
			NAT: NAT{
				Masquerades:     []MasqueradeRule{{Name: "masq-lan", SourceCIDR: "172.16.100.0/24", OutInterface: "wan0"}},
				SNATRules:       []SNATRule{{Name: "snat-lan", SourceCIDR: "172.16.100.0/24", OutInterface: "wan0", ToSource: "172.17.100.29"}},
				PortForwards:    []PortForwardRule{{Name: "web", Protocol: "tcp", InInterface: "wan0", ListenIP: "172.17.100.29", ListenPort: 8080, TargetIP: "172.16.100.6", TargetPort: 80}},
				DestinationMaps: []DestinationMapRule{{Name: "legacy", MatchAddress: "192.168.9.2", TargetAddress: "192.168.0.1", FromCIDR: "172.16.100.0/24", InInterface: "lan0", OutInterface: "wan0", SourceNAT: "172.17.100.29"}},
			},
			Firewall: Firewall{Rules: []FirewallRule{{Name: "allow-web", Action: "allow", Direction: "forward", Protocol: "tcp", CIDRs: []string{"172.16.100.0/24"}, Ports: []int{80}}}},
		},
	}
	commands, err := (LinuxRenderer{}).RenderApply(router)
	if err != nil {
		t.Fatalf("RenderApply returned error: %v", err)
	}
	assertCommand(t, commands, "ip", "netns", "add", "ovnflow-edge")
	if !commands[0].IgnoreAlreadyExists {
		t.Fatalf("namespace ensure command should ignore already-exists: %#v", commands[0])
	}
	assertCommandContains(t, commands, "ovs-vsctl", "external_ids:ovnflow.io/linux-router-ns=ovnflow-edge")
	assertCommandContains(t, commands, "ip", "dhclient", "-v", "wan0")
	assertCommandContains(t, commands, "ip", "dnsmasq", "--conf-file=/run/ovnflow/edge/dnsmasq.conf", "--host-record=api.service,172.16.100.6")
	assertCommandContains(t, commands, "ip", "nft", "delete", "table", "ip", "ovnflow_nat")
	assertCommandContains(t, commands, "ip", "nft", "add", "table", "ip", "ovnflow_nat")
	assertCommandContains(t, commands, "ip", "masquerade", "comment", `"ovnflow:masq-lan"`)
	assertCommandContains(t, commands, "ip", "snat", "to", "172.17.100.29")
	assertCommandContains(t, commands, "ip", "tcp", "dport", "8080", "dnat", "to", "172.16.100.6:80")
	assertCommandContains(t, commands, "ip", "saddr", "172.16.100.0/24", "iifname", "lan0", "ip", "daddr", "192.168.9.2", "dnat", "to", "192.168.0.1")
	assertCommandContains(t, commands, "ip", "table", "inet", "ovnflow_filter")
	assertCommandContains(t, commands, "ip", "accept", "comment", `"ovnflow:allow-web"`)
}

func TestLinuxRendererRendersIPTablesBackend(t *testing.T) {
	router := Router{
		Name: "edge",
		Spec: Spec{
			Interfaces: []Interface{{Name: "lan0", Role: InterfaceLAN}, {Name: "wan0", Role: InterfaceWAN}},
			NAT: NAT{
				Masquerades:     []MasqueradeRule{{Name: "masq", SourceCIDR: "172.16.100.0/24", OutInterface: "wan0"}},
				DestinationMaps: []DestinationMapRule{{Name: "legacy", MatchAddress: "192.168.9.2", TargetAddress: "192.168.0.1", FromCIDR: "172.16.100.0/24", InInterface: "lan0", OutInterface: "wan0", SourceNAT: "172.17.100.29"}},
			},
			Firewall: Firewall{Rules: []FirewallRule{{Name: "drop-ssh", Action: "drop", Protocol: "tcp", Ports: []int{22}}}},
		},
	}
	commands, err := (LinuxRenderer{NATBackend: ovnflow.NATBackendIPTables}).RenderApply(router)
	if err != nil {
		t.Fatalf("RenderApply returned error: %v", err)
	}
	assertCommandContains(t, commands, "ip", "iptables", "-t", "nat", "-A", "POSTROUTING", "-j", "MASQUERADE")
	assertCommandContains(t, commands, "ip", "iptables", "-t", "nat", "-A", "PREROUTING", "-d", "192.168.9.2", "-j", "DNAT", "--to-destination", "192.168.0.1")
	assertCommandContains(t, commands, "ip", "iptables", "-t", "nat", "-A", "POSTROUTING", "-d", "192.168.0.1", "-j", "SNAT", "--to-source", "172.17.100.29")
	assertCommandContains(t, commands, "ip", "iptables", "-A", "FORWARD", "-p", "tcp", "--dport", "22", "-j", "DROP")
}

func TestLinuxRendererRejectsInvalidBackend(t *testing.T) {
	_, err := (LinuxRenderer{NATBackend: "pf"}).RenderApply(Router{Name: "edge"})
	if !ovnflow.IsKind(err, ovnflow.ErrorValidation) {
		t.Fatalf("error kind = %q for %v, want validation", ovnflow.KindOf(err), err)
	}
}

func TestSystemExecutorClassifiesCommandFailures(t *testing.T) {
	err := (SystemExecutor{}).Run(context.Background(), Command{Program: "sh", Args: []string{"-c", "echo boom >&2; exit 7"}})
	if !ovnflow.IsKind(err, ovnflow.ErrorUnavailable) {
		t.Fatalf("error kind = %q for %v, want unavailable", ovnflow.KindOf(err), err)
	}
}

func TestSystemExecutorClassifiesCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := (SystemExecutor{}).Run(ctx, Command{Program: "sh", Args: []string{"-c", "sleep 1"}})
	if !ovnflow.IsKind(err, ovnflow.ErrorCanceled) && !errors.Is(err, context.Canceled) {
		t.Fatalf("error kind = %q for %v, want canceled", ovnflow.KindOf(err), err)
	}
}

func assertCommand(t *testing.T, commands []Command, program string, args ...string) {
	t.Helper()
	for _, command := range commands {
		if command.Program == program && equalStrings(command.Args, args) {
			return
		}
	}
	t.Fatalf("missing command %s %#v in %#v", program, args, commands)
}

func assertCommandContains(t *testing.T, commands []Command, program string, tokens ...string) {
	t.Helper()
	for _, command := range commands {
		if command.Program == program && containsSubsequence(command.Args, tokens) {
			return
		}
	}
	t.Fatalf("missing command %s containing %#v in %#v", program, tokens, commands)
}

func containsSubsequence(values, tokens []string) bool {
	if len(tokens) == 0 {
		return true
	}
	next := 0
	for _, value := range values {
		if value == tokens[next] {
			next++
			if next == len(tokens) {
				return true
			}
		}
	}
	return false
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

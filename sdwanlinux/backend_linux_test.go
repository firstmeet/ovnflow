//go:build linux

package sdwanlinux

import (
	"context"
	"strings"
	"testing"

	"github.com/firstmeet/ovnflow/v2"
)

type fakeOVS struct {
	tunnels []OVSTunnel
	deleted []OVSTunnel
}

func (f *fakeOVS) EnsureTunnel(_ context.Context, tunnel OVSTunnel) error {
	f.tunnels = append(f.tunnels, tunnel)
	return nil
}

func (f *fakeOVS) DeleteTunnel(_ context.Context, tunnel OVSTunnel) error {
	f.deleted = append(f.deleted, tunnel)
	return nil
}

type fakeOpenFlow struct {
	rules   []OpenFlowRule
	deleted []OpenFlowRule
}

func (f *fakeOpenFlow) EnsureRule(_ context.Context, rule OpenFlowRule) error {
	f.rules = append(f.rules, rule)
	return nil
}

func (f *fakeOpenFlow) DeleteRule(_ context.Context, rule OpenFlowRule) error {
	f.deleted = append(f.deleted, rule)
	return nil
}

func TestWireGuardBackendRendersLinuxCommands(t *testing.T) {
	exec := &FakeExecutor{}
	backend, err := NewBackend(Config{LocalSite: "edge-a", Executor: exec, InterfacePrefix: "of"})
	if err != nil {
		t.Fatalf("NewBackend() = %v", err)
	}
	network := ovnflow.SDWANNetwork{
		Name:      "wan",
		Layer:     ovnflow.SDWANLayerL3,
		Transport: ovnflow.SDWANTransportWireGuard,
		Sites: []ovnflow.SDWANSite{
			{Name: "edge-a", Router: "edge-a", CIDRs: []string{"10.0.0.0/24"}, Attributes: map[string]string{"wireguard_private_key_file": "/run/keys/edge-a"}},
			{Name: "edge-b", Router: "edge-b", CIDRs: []string{"10.1.0.0/24"}, Endpoint: "198.51.100.2:51820", PublicKey: "peer-key"},
		},
		Links: []ovnflow.SDWANLink{{From: "edge-a", To: "edge-b"}},
	}
	if err := backend.ApplySDWAN(context.Background(), network, ovnflow.SDWANApplyPlan{}); err != nil {
		t.Fatalf("ApplySDWAN() = %v", err)
	}
	commands := exec.Snapshot()
	if !hasCommand(commands, "ip", "link", "add") {
		t.Fatalf("missing ip link add command: %#v", commands)
	}
	if !hasCommand(commands, "wg", "set") {
		t.Fatalf("missing wg set command: %#v", commands)
	}
	if !hasCommand(commands, "ip", "route", "replace") {
		t.Fatalf("missing route replace command: %#v", commands)
	}
	if !hasCommand(commands, "ip", "rule", "add") {
		t.Fatalf("missing rule add command: %#v", commands)
	}
	if !hasCommand(commands, "ip", "rule", "del", "priority") {
		t.Fatalf("missing idempotent priority cleanup before rule add: %#v", commands)
	}
}

func TestWireGuardRelayPathIncludesSpokeCIDRs(t *testing.T) {
	exec := &FakeExecutor{}
	backend, err := NewBackend(Config{LocalSite: "edge-a", Executor: exec})
	if err != nil {
		t.Fatalf("NewBackend() = %v", err)
	}
	network := ovnflow.SDWANNetwork{
		Name:      "wan",
		Layer:     ovnflow.SDWANLayerL3,
		Transport: ovnflow.SDWANTransportWireGuard,
		PathMode:  ovnflow.SDWANPathModeRelay,
		Sites: []ovnflow.SDWANSite{
			{Name: "edge-a", Router: "edge-a", CIDRs: []string{"10.0.0.0/24"}},
			{Name: "edge-r", Router: "edge-r", CIDRs: []string{"10.255.0.0/24"}, Relay: true, PublicKey: "relay-key"},
			{Name: "edge-b", Router: "edge-b", CIDRs: []string{"10.1.0.0/24"}},
			{Name: "edge-c", Router: "edge-c", CIDRs: []string{"10.2.0.0/24"}},
		},
		Links: []ovnflow.SDWANLink{{From: "edge-r", To: "edge-a", PathMode: ovnflow.SDWANPathModeRelay}},
	}
	if err := backend.ApplySDWAN(context.Background(), network, ovnflow.SDWANApplyPlan{}); err != nil {
		t.Fatalf("ApplySDWAN() = %v", err)
	}
	commands := exec.Snapshot()
	if !hasCommandContaining(commands, "wg", "allowed-ips", "10.1.0.0/24") || !hasCommandContaining(commands, "wg", "allowed-ips", "10.2.0.0/24") {
		t.Fatalf("relay peer allowed-ips missing spoke CIDRs: %#v", commands)
	}
	if !hasCommand(commands, "ip", "route", "replace", "10.1.0.0/24") || !hasCommand(commands, "ip", "route", "replace", "10.2.0.0/24") {
		t.Fatalf("relay routes missing spoke CIDRs: %#v", commands)
	}
}

func TestWireGuardAutoPathPrefersDirectAndFallsBackForMissingSpokes(t *testing.T) {
	exec := &FakeExecutor{}
	backend, err := NewBackend(Config{LocalSite: "edge-a", Executor: exec})
	if err != nil {
		t.Fatalf("NewBackend() = %v", err)
	}
	network := ovnflow.SDWANNetwork{
		Name:      "wan",
		Layer:     ovnflow.SDWANLayerL3,
		Transport: ovnflow.SDWANTransportWireGuard,
		PathMode:  ovnflow.SDWANPathModeAuto,
		Sites: []ovnflow.SDWANSite{
			{Name: "edge-a", Router: "edge-a", CIDRs: []string{"10.0.0.0/24"}},
			{Name: "edge-r", Router: "edge-r", CIDRs: []string{"10.255.0.0/24"}, Transit: true, PublicKey: "relay-key"},
			{Name: "edge-b", Router: "edge-b", CIDRs: []string{"10.1.0.0/24"}, PublicKey: "direct-key"},
			{Name: "edge-c", Router: "edge-c", CIDRs: []string{"10.2.0.0/24"}},
		},
		Links: []ovnflow.SDWANLink{
			{From: "edge-a", To: "edge-b", PathMode: ovnflow.SDWANPathModeDirect},
			{From: "edge-r", To: "edge-a", PathMode: ovnflow.SDWANPathModeAuto},
		},
	}
	if err := backend.ApplySDWAN(context.Background(), network, ovnflow.SDWANApplyPlan{}); err != nil {
		t.Fatalf("ApplySDWAN() = %v", err)
	}
	commands := exec.Snapshot()
	if !hasCommand(commands, "ip", "route", "replace", "10.1.0.0/24") {
		t.Fatalf("direct route missing edge-b CIDR: %#v", commands)
	}
	if !hasCommand(commands, "ip", "route", "replace", "10.2.0.0/24") {
		t.Fatalf("relay fallback route missing edge-c CIDR: %#v", commands)
	}
	if countCommand(commands, "ip", "route", "replace", "10.1.0.0/24") != 1 {
		t.Fatalf("edge-b direct CIDR should not also be routed through relay: %#v", commands)
	}
	if !hasCommandContaining(commands, "wg", "allowed-ips", "10.2.0.0/24") {
		t.Fatalf("relay peer missing fallback CIDR: %#v", commands)
	}
}

func TestWireGuardDisabledDirectAutoPathFallsBackToRelay(t *testing.T) {
	exec := &FakeExecutor{}
	backend, err := NewBackend(Config{LocalSite: "edge-a", Executor: exec})
	if err != nil {
		t.Fatalf("NewBackend() = %v", err)
	}
	network := ovnflow.SDWANNetwork{
		Name:      "wan",
		Layer:     ovnflow.SDWANLayerL3,
		Transport: ovnflow.SDWANTransportWireGuard,
		PathMode:  ovnflow.SDWANPathModeAuto,
		Sites: []ovnflow.SDWANSite{
			{Name: "edge-a", Router: "edge-a", CIDRs: []string{"10.0.0.0/24"}},
			{Name: "edge-r", Router: "edge-r", CIDRs: []string{"10.255.0.0/24"}, Relay: true, PublicKey: "relay-key"},
			{Name: "edge-b", Router: "edge-b", CIDRs: []string{"10.1.0.0/24"}, PublicKey: "direct-key"},
		},
		Links: []ovnflow.SDWANLink{
			{From: "edge-a", To: "edge-b", PathMode: ovnflow.SDWANPathModeDirect, Disabled: true},
			{From: "edge-r", To: "edge-a", PathMode: ovnflow.SDWANPathModeAuto},
		},
	}
	if err := backend.ApplySDWAN(context.Background(), network, ovnflow.SDWANApplyPlan{}); err != nil {
		t.Fatalf("ApplySDWAN() = %v", err)
	}
	commands := exec.Snapshot()
	if !hasCommandContaining(commands, "wg", "allowed-ips", "10.1.0.0/24") {
		t.Fatalf("disabled direct CIDR did not fall back to relay: %#v", commands)
	}
	if !hasCommand(commands, "ip", "link", "delete") {
		t.Fatalf("disabled direct link did not request cleanup: %#v", commands)
	}
}

func TestDisabledDirectCleanupRunsBeforeRelayFallbackInstall(t *testing.T) {
	exec := &FakeExecutor{}
	backend, err := NewBackend(Config{LocalSite: "edge-a", Executor: exec})
	if err != nil {
		t.Fatalf("NewBackend() = %v", err)
	}
	network := ovnflow.SDWANNetwork{
		Name:      "wan",
		Layer:     ovnflow.SDWANLayerL3,
		Transport: ovnflow.SDWANTransportWireGuard,
		PathMode:  ovnflow.SDWANPathModeAuto,
		Sites: []ovnflow.SDWANSite{
			{Name: "edge-a", Router: "edge-a", CIDRs: []string{"10.0.0.0/24"}},
			{Name: "edge-r", Router: "edge-r", CIDRs: []string{"10.255.0.0/24"}, Relay: true, PublicKey: "relay-key"},
			{Name: "edge-b", Router: "edge-b", CIDRs: []string{"10.1.0.0/24"}, PublicKey: "direct-key"},
		},
		Links: []ovnflow.SDWANLink{
			{Name: "z-relay", From: "edge-a", To: "edge-r", PathMode: ovnflow.SDWANPathModeAuto},
			{Name: "a-direct", From: "edge-a", To: "edge-b", PathMode: ovnflow.SDWANPathModeDirect, Disabled: true},
		},
	}
	if err := backend.ApplySDWAN(context.Background(), network, ovnflow.SDWANApplyPlan{}); err != nil {
		t.Fatalf("ApplySDWAN() = %v", err)
	}
	commands := exec.Snapshot()
	deleteIndex := firstCommandIndex(commands, "ip", "link", "delete")
	relayRouteIndex := firstCommandIndex(commands, "ip", "route", "replace", "10.1.0.0/24")
	if deleteIndex < 0 || relayRouteIndex < 0 {
		t.Fatalf("missing direct cleanup or relay fallback route: %#v", commands)
	}
	if deleteIndex > relayRouteIndex {
		t.Fatalf("direct cleanup ran after relay fallback install: delete=%d route=%d commands=%#v", deleteIndex, relayRouteIndex, commands)
	}
}

func TestOVSTunnelAndOpenFlowManagersAreCalled(t *testing.T) {
	exec := &FakeExecutor{}
	ovs := &fakeOVS{}
	of := &fakeOpenFlow{}
	backend, err := NewBackend(Config{LocalSite: "edge-a", Executor: exec, OVS: ovs, OpenFlow: of})
	if err != nil {
		t.Fatalf("NewBackend() = %v", err)
	}
	network := ovnflow.SDWANNetwork{
		Name:      "wan",
		Layer:     ovnflow.SDWANLayerL2,
		Transport: ovnflow.SDWANTransportGeneve,
		Labels:    ovnflow.Labels{"ovs_bridge": "br-test"},
		Sites: []ovnflow.SDWANSite{
			{Name: "edge-a", Router: "edge-a", L2Segments: []string{"blue"}},
			{Name: "edge-b", Router: "edge-b", L2Segments: []string{"blue"}, Endpoint: "203.0.113.2"},
		},
		Links: []ovnflow.SDWANLink{{From: "edge-a", To: "edge-b", Attributes: map[string]string{"key": "100", "in_port": "1", "eth_type": "0x0800", "ipv4_dst": "10.0.0.10/32"}}},
	}
	if err := backend.ApplySDWAN(context.Background(), network, ovnflow.SDWANApplyPlan{}); err != nil {
		t.Fatalf("ApplySDWAN() = %v", err)
	}
	if len(ovs.tunnels) != 1 {
		t.Fatalf("ovs tunnels = %#v", ovs.tunnels)
	}
	if ovs.tunnels[0].Type != "geneve" || ovs.tunnels[0].RemoteIP != "203.0.113.2" || ovs.tunnels[0].ExternalID[ExternalIDNetworkKey] != "wan" {
		t.Fatalf("unexpected tunnel = %#v", ovs.tunnels[0])
	}
	if len(of.rules) != 1 || of.rules[0].Bridge != "br-test" {
		t.Fatalf("openflow rules = %#v", of.rules)
	}
	if of.rules[0].Cookie == 0 {
		t.Fatalf("openflow rule missing owned cookie: %#v", of.rules[0])
	}
	if of.rules[0].Match.InPort == nil || *of.rules[0].Match.InPort != 1 || of.rules[0].Match.IPv4Dst != "10.0.0.10/32" {
		t.Fatalf("openflow rule missing explicit match: %#v", of.rules[0].Match)
	}
}

func TestOpenFlowManagerRequiresExplicitMatch(t *testing.T) {
	exec := &FakeExecutor{}
	ovs := &fakeOVS{}
	of := &fakeOpenFlow{}
	backend, err := NewBackend(Config{LocalSite: "edge-a", Executor: exec, OVS: ovs, OpenFlow: of})
	if err != nil {
		t.Fatalf("NewBackend() = %v", err)
	}
	network := ovnflow.SDWANNetwork{
		Name:      "wan",
		Layer:     ovnflow.SDWANLayerL2,
		Transport: ovnflow.SDWANTransportGeneve,
		Sites: []ovnflow.SDWANSite{
			{Name: "edge-a", Router: "edge-a", L2Segments: []string{"blue"}},
			{Name: "edge-b", Router: "edge-b", L2Segments: []string{"blue"}, Endpoint: "203.0.113.2"},
		},
		Links: []ovnflow.SDWANLink{{From: "edge-a", To: "edge-b"}},
	}
	err = backend.ApplySDWAN(context.Background(), network, ovnflow.SDWANApplyPlan{})
	if !ovnflow.IsKind(err, ovnflow.ErrorValidation) {
		t.Fatalf("ApplySDWAN() = %v, want validation", err)
	}
}

func TestDisabledLinksAreNotApplied(t *testing.T) {
	exec := &FakeExecutor{}
	ovs := &fakeOVS{}
	backend, err := NewBackend(Config{LocalSite: "edge-a", Executor: exec, OVS: ovs})
	if err != nil {
		t.Fatalf("NewBackend() = %v", err)
	}
	network := ovnflow.SDWANNetwork{
		Name:      "wan",
		Layer:     ovnflow.SDWANLayerL2,
		Transport: ovnflow.SDWANTransportVXLAN,
		Sites: []ovnflow.SDWANSite{
			{Name: "edge-a", Router: "edge-a", L2Segments: []string{"blue"}},
			{Name: "edge-b", Router: "edge-b", L2Segments: []string{"blue"}, Endpoint: "203.0.113.2"},
		},
		Links: []ovnflow.SDWANLink{{From: "edge-a", To: "edge-b", Disabled: true}},
	}
	if err := backend.ApplySDWAN(context.Background(), network, ovnflow.SDWANApplyPlan{}); err != nil {
		t.Fatalf("ApplySDWAN() = %v", err)
	}
	if len(ovs.tunnels) != 0 || len(exec.Snapshot()) != 0 {
		t.Fatalf("disabled link was applied: tunnels=%#v commands=%#v", ovs.tunnels, exec.Snapshot())
	}
}

func TestDisabledLinkCleansExistingResources(t *testing.T) {
	exec := &FakeExecutor{}
	ovs := &fakeOVS{}
	backend, err := NewBackend(Config{LocalSite: "edge-a", Executor: exec, OVS: ovs})
	if err != nil {
		t.Fatalf("NewBackend() = %v", err)
	}
	enabled := ovnflow.SDWANNetwork{
		Name:      "wan",
		Layer:     ovnflow.SDWANLayerL2,
		Transport: ovnflow.SDWANTransportVXLAN,
		Sites: []ovnflow.SDWANSite{
			{Name: "edge-a", Router: "edge-a", L2Segments: []string{"blue"}},
			{Name: "edge-b", Router: "edge-b", L2Segments: []string{"blue"}, Endpoint: "203.0.113.2"},
		},
		Links: []ovnflow.SDWANLink{{From: "edge-a", To: "edge-b"}},
	}
	if err := backend.ApplySDWAN(context.Background(), enabled, ovnflow.SDWANApplyPlan{}); err != nil {
		t.Fatalf("ApplySDWAN(enabled) = %v", err)
	}
	disabled := enabled
	disabled.Links[0].Disabled = true
	if err := backend.ApplySDWAN(context.Background(), disabled, ovnflow.SDWANApplyPlan{}); err != nil {
		t.Fatalf("ApplySDWAN(disabled) = %v", err)
	}
	if len(ovs.deleted) != 1 {
		t.Fatalf("disabled link did not delete existing tunnel: %#v", ovs.deleted)
	}
}

func TestWireGuardDeleteCleansRouteRulesBeforeInterface(t *testing.T) {
	exec := &FakeExecutor{}
	backend, err := NewBackend(Config{LocalSite: "edge-a", Executor: exec, RouteTable: 51821})
	if err != nil {
		t.Fatalf("NewBackend() = %v", err)
	}
	network := ovnflow.SDWANNetwork{
		Name:      "wan",
		Layer:     ovnflow.SDWANLayerL3,
		Transport: ovnflow.SDWANTransportWireGuard,
		Sites: []ovnflow.SDWANSite{
			{Name: "edge-a", Router: "edge-a", CIDRs: []string{"10.0.0.0/24"}},
			{Name: "edge-b", Router: "edge-b", CIDRs: []string{"10.1.0.0/24"}, PublicKey: "peer-key"},
		},
		Links: []ovnflow.SDWANLink{{From: "edge-a", To: "edge-b"}},
	}
	if err := backend.ApplySDWAN(context.Background(), network, ovnflow.SDWANApplyPlan{}); err != nil {
		t.Fatalf("ApplySDWAN() = %v", err)
	}
	if err := backend.DeleteSDWAN(context.Background(), "wan"); err != nil {
		t.Fatalf("DeleteSDWAN() = %v", err)
	}
	commands := exec.Snapshot()
	if !hasCommand(commands, "ip", "rule", "del") {
		t.Fatalf("missing ip rule del command: %#v", commands)
	}
	if !hasCommand(commands, "ip", "route", "del") {
		t.Fatalf("missing ip route del command: %#v", commands)
	}
	if !hasCommand(commands, "ip", "link", "delete") {
		t.Fatalf("missing ip link delete command: %#v", commands)
	}
}

func TestGeneratedInterfaceNamesAvoidTruncationCollisions(t *testing.T) {
	backend, err := NewBackend(Config{LocalSite: "edge-a"})
	if err != nil {
		t.Fatalf("NewBackend() = %v", err)
	}
	linkA := ovnflow.SDWANLink{From: "edge-a", To: "edge-bbbbbbbbbbbbbbbbb1", Transport: ovnflow.SDWANTransportWireGuard}
	linkB := ovnflow.SDWANLink{From: "edge-a", To: "edge-bbbbbbbbbbbbbbbbb2", Transport: ovnflow.SDWANTransportWireGuard}
	network := ovnflow.SDWANNetwork{Name: "network-with-a-long-common-prefix"}
	ifaceA := backend.wireGuardInterface(network, linkA)
	ifaceB := backend.wireGuardInterface(network, linkB)
	if ifaceA == ifaceB {
		t.Fatalf("interface names collided: %s", ifaceA)
	}
	if len(ifaceA) > 15 || len(ifaceB) > 15 {
		t.Fatalf("interface names exceed Linux IFNAMSIZ limit: %q %q", ifaceA, ifaceB)
	}
}

func TestOVSTunnelOwnershipRequiresAllMarkers(t *testing.T) {
	tunnel := OVSTunnel{
		Network:   "wan",
		Link:      "edge-a--edge-b",
		LocalSite: "edge-a",
		Bridge:    "br-test",
		Port:      "port-test",
		Type:      "geneve",
		RemoteIP:  "203.0.113.2",
	}
	if ovsTunnelOwnedBy(map[string]string{ovnflow.ExternalIDManagedByKey: "ovnflow"}, tunnel) {
		t.Fatal("partial external_ids unexpectedly passed ownership check")
	}
	if !ovsTunnelOwnedBy(ownedExternalIDs("wan", "edge-a", "edge-a--edge-b"), tunnel) {
		t.Fatal("complete external_ids did not pass ownership check")
	}
}

func hasCommand(commands []Command, program string, args ...string) bool {
	for _, command := range commands {
		if command.Program != program || len(command.Args) < len(args) {
			continue
		}
		ok := true
		for i, arg := range args {
			if command.Args[i] != arg {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

func hasCommandContaining(commands []Command, program string, args ...string) bool {
	for _, command := range commands {
		if command.Program != program {
			continue
		}
		matched := true
		for _, want := range args {
			found := false
			for _, got := range command.Args {
				if got == want || containsArgValue(got, want) {
					found = true
					break
				}
			}
			if !found {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}

func countCommand(commands []Command, program string, args ...string) int {
	count := 0
	for _, command := range commands {
		if command.Program != program || len(command.Args) < len(args) {
			continue
		}
		ok := true
		for i, arg := range args {
			if command.Args[i] != arg {
				ok = false
				break
			}
		}
		if ok {
			count++
		}
	}
	return count
}

func firstCommandIndex(commands []Command, program string, args ...string) int {
	for index, command := range commands {
		if command.Program != program || len(command.Args) < len(args) {
			continue
		}
		ok := true
		for i, arg := range args {
			if command.Args[i] != arg {
				ok = false
				break
			}
		}
		if ok {
			return index
		}
	}
	return -1
}

func containsArgValue(got, want string) bool {
	if got == want {
		return true
	}
	for _, part := range []string{",", ";"} {
		if strings.Contains(got, part) {
			for _, value := range strings.Split(got, part) {
				if value == want {
					return true
				}
			}
		}
	}
	return false
}

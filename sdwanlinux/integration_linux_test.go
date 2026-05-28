//go:build linux && integration

package sdwanlinux

import (
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/firstmeet/ovnflow/v2"
	"github.com/ovn-kubernetes/libovsdb/ovsdb"
)

func TestIntegrationSDWANOVSTunnelLifecycle(t *testing.T) {
	if !ovnflow.EnvGateEnabled(os.Getenv(ovnflow.EnvSDWANBackendChecks)) || !ovnflow.EnvGateEnabled(os.Getenv(ovnflow.EnvOVSTunnelChecks)) {
		t.Skipf("set %s=1 and %s=1 to run SD-WAN OVS tunnel integration checks", ovnflow.EnvSDWANBackendChecks, ovnflow.EnvOVSTunnelChecks)
	}
	ovsAddr := strings.TrimSpace(os.Getenv(ovnflow.EnvOVSAddr))
	if ovsAddr == "" {
		skipOrFailIntegration(t, "set %s to run SD-WAN OVS tunnel integration checks", ovnflow.EnvOVSAddr)
	}
	nbAddr := strings.TrimSpace(os.Getenv(ovnflow.EnvOVNNBAddr))
	sbAddr := strings.TrimSpace(os.Getenv(ovnflow.EnvOVNSBAddr))
	if nbAddr == "" || sbAddr == "" {
		skipOrFailIntegration(t, "set %s and %s alongside %s because the SDK connects all configured OVSDB clients", ovnflow.EnvOVNNBAddr, ovnflow.EnvOVNSBAddr, ovnflow.EnvOVSAddr)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	client, err := ovnflow.Connect(ctx, ovnflow.Config{
		OVSAddr:   ovsAddr,
		OVNNBAddr: nbAddr,
		OVNSBAddr: sbAddr,
	})
	if err != nil {
		skipOrFailIntegration(t, "OVSDB endpoint %s is not reachable for SD-WAN OVS tunnel integration checks: %v", ovsAddr, err)
	}
	t.Cleanup(client.Close)

	suffix := integrationSuffix()
	bridge := envOrDefault(ovnflow.EnvTestBridge, "br-ovnflow-it") + "-sdwan-" + suffix
	networkName := "ovnflow-it-sdwan-" + suffix
	linkName := "edge-a--edge-b"
	portName := clampName("ofwtn"+suffix, 15)
	backend, err := NewBackend(Config{
		LocalSite: "edge-a",
		Executor:  &FakeExecutor{},
		OVS:       NewOVSManager(client.LocalOVS()),
	})
	if err != nil {
		t.Fatalf("NewBackend() = %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_ = backend.DeleteSDWAN(cleanupCtx, networkName)
		_ = client.LocalOVS().Bridge(bridge).Delete().Execute(cleanupCtx)
	})

	network := ovnflow.SDWANNetwork{
		Name:      networkName,
		Layer:     ovnflow.SDWANLayerL2,
		Transport: ovnflow.SDWANTransportGeneve,
		Labels:    ovnflow.Labels{"ovs_bridge": bridge},
		Sites: []ovnflow.SDWANSite{
			{Name: "edge-a", Router: "edge-a", L2Segments: []string{"blue"}},
			{Name: "edge-b", Router: "edge-b", L2Segments: []string{"blue"}, Endpoint: "192.0.2.20"},
		},
		Links: []ovnflow.SDWANLink{{
			Name:      linkName,
			From:      "edge-a",
			To:        "edge-b",
			Transport: ovnflow.SDWANTransportGeneve,
			Attributes: map[string]string{
				"bridge":    bridge,
				"port":      portName,
				"remote_ip": "192.0.2.20",
				"key":       "100",
			},
		}},
	}
	if err := backend.ApplySDWAN(ctx, network, ovnflow.SDWANApplyPlan{}); err != nil {
		t.Fatalf("ApplySDWAN() = %v", err)
	}
	iface, err := client.LocalOVS().GetInterface(ctx, portName)
	if err != nil {
		t.Fatalf("GetInterface(%s) = %v", portName, err)
	}
	if iface.Type != "geneve" {
		t.Fatalf("Interface type = %q, want geneve", iface.Type)
	}
	if iface.Options["remote_ip"] != "192.0.2.20" || iface.Options["key"] != "100" {
		t.Fatalf("Interface options = %#v, want remote_ip/key", iface.Options)
	}
	if iface.ExternalIDs[ExternalIDNetworkKey] != networkName || iface.ExternalIDs[ExternalIDLinkKey] != linkName || iface.ExternalIDs[ovnflow.ExternalIDKindKey] != "SDWAN" {
		t.Fatalf("Interface external_ids missing SD-WAN ownership: %#v", iface.ExternalIDs)
	}
	port, err := client.LocalOVS().GetPort(ctx, portName)
	if err != nil {
		t.Fatalf("GetPort(%s) = %v", portName, err)
	}
	if port.ExternalIDs[ExternalIDNetworkKey] != networkName || port.ExternalIDs[ExternalIDLinkKey] != linkName {
		t.Fatalf("Port external_ids missing SD-WAN ownership: %#v", port.ExternalIDs)
	}

	if err := backend.DeleteSDWAN(ctx, networkName); err != nil {
		t.Fatalf("DeleteSDWAN() = %v", err)
	}
	if _, err := client.LocalOVS().GetInterface(ctx, portName); !ovnflow.IsKind(err, ovnflow.ErrorNotFound) {
		t.Fatalf("GetInterface(%s) after delete = %v, want not found", portName, err)
	}
}

func TestIntegrationSDWANOpenFlowHookLifecycle(t *testing.T) {
	if !ovnflow.EnvGateEnabled(os.Getenv(ovnflow.EnvSDWANBackendChecks)) ||
		!ovnflow.EnvGateEnabled(os.Getenv(ovnflow.EnvOVSTunnelChecks)) ||
		!ovnflow.EnvGateEnabled(os.Getenv(ovnflow.EnvOpenFlowChecks)) {
		t.Skipf("set %s=1, %s=1, and %s=1 to run SD-WAN OpenFlow hook integration checks", ovnflow.EnvSDWANBackendChecks, ovnflow.EnvOVSTunnelChecks, ovnflow.EnvOpenFlowChecks)
	}
	ovsAddr := strings.TrimSpace(os.Getenv(ovnflow.EnvOVSAddr))
	nbAddr := strings.TrimSpace(os.Getenv(ovnflow.EnvOVNNBAddr))
	sbAddr := strings.TrimSpace(os.Getenv(ovnflow.EnvOVNSBAddr))
	openFlowAddr := strings.TrimSpace(os.Getenv(ovnflow.EnvOpenFlowAddr))
	if ovsAddr == "" || nbAddr == "" || sbAddr == "" || openFlowAddr == "" {
		skipOrFailIntegration(t, "set %s, %s, %s, and %s to run SD-WAN OpenFlow hook integration checks", ovnflow.EnvOVSAddr, ovnflow.EnvOVNNBAddr, ovnflow.EnvOVNSBAddr, ovnflow.EnvOpenFlowAddr)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	client, err := ovnflow.Connect(ctx, ovnflow.Config{OVSAddr: ovsAddr, OVNNBAddr: nbAddr, OVNSBAddr: sbAddr})
	if err != nil {
		skipOrFailIntegration(t, "OVSDB endpoint %s is not reachable for SD-WAN OpenFlow hook checks: %v", ovsAddr, err)
	}
	t.Cleanup(client.Close)

	suffix := integrationSuffix()
	bridge := envOrDefault(ovnflow.EnvTestBridge, "br-ovnflow-it") + "-sdwan-of-" + suffix
	networkName := "ovnflow-it-sdwan-of-" + suffix
	linkName := "edge-a--edge-b"
	portName := clampName("ofwto"+suffix, 15)
	controllerTarget := controllerTargetFromEndpoint(openFlowAddr)
	if controllerTarget == "" {
		skipOrFailIntegration(t, "invalid %s %q", ovnflow.EnvOpenFlowAddr, openFlowAddr)
	}
	if err := client.LocalOVS().Bridge(bridge).Ensure().WithControllerTarget(controllerTarget).Execute(ctx); err != nil {
		t.Fatalf("configure OVS bridge OpenFlow controller: %v", err)
	}
	if err := client.LocalOVS().TableBy("Bridge", "name", bridge).Update().
		WithOptionalColumn("protocols", ovsSet("OpenFlow13", "OpenFlow15")).
		Execute(ctx); err != nil {
		t.Fatalf("configure OVS bridge OpenFlow protocols: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_ = client.LocalOVS().Bridge(bridge).Delete().Execute(cleanupCtx)
	})

	of := client.OpenFlow().WithEndpoint(openFlowAddr).WithVersions(ovnflow.OpenFlow15, ovnflow.OpenFlow13)
	backend, err := NewBackend(Config{
		LocalSite: "edge-a",
		Executor:  &FakeExecutor{},
		OVS:       NewOVSManager(client.LocalOVS()),
		OpenFlow:  NewOpenFlowManager(of),
	})
	if err != nil {
		t.Fatalf("NewBackend() = %v", err)
	}
	probeSession, err := dialOpenFlowEventually(ctx, of)
	if err != nil {
		skipOrFailIntegration(t, "OpenFlow endpoint %s is not reachable after configuring %s on bridge %s: %v", openFlowAddr, controllerTarget, bridge, err)
	}
	_ = probeSession.Close()
	network := ovnflow.SDWANNetwork{
		Name:      networkName,
		Layer:     ovnflow.SDWANLayerL2,
		Transport: ovnflow.SDWANTransportGeneve,
		Labels:    ovnflow.Labels{"ovs_bridge": bridge},
		Sites: []ovnflow.SDWANSite{
			{Name: "edge-a", Router: "edge-a", L2Segments: []string{"blue"}},
			{Name: "edge-b", Router: "edge-b", L2Segments: []string{"blue"}, Endpoint: "192.0.2.21"},
		},
		Links: []ovnflow.SDWANLink{{
			Name:      linkName,
			From:      "edge-a",
			To:        "edge-b",
			Transport: ovnflow.SDWANTransportGeneve,
			Attributes: map[string]string{
				"bridge":    bridge,
				"port":      portName,
				"remote_ip": "192.0.2.21",
				"key":       "101",
				"eth_type":  "0x0800",
				"ipv4_dst":  "10.200.0.10/32",
			},
		}},
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_ = backend.DeleteSDWAN(cleanupCtx, networkName)
	})
	if err := backend.ApplySDWAN(ctx, network, ovnflow.SDWANApplyPlan{}); err != nil {
		t.Fatalf("ApplySDWAN() = %v", err)
	}
	rule := backend.openFlowRule(normalizeNetwork(network), normalizeNetwork(network).Links[0])
	session, err := dialOpenFlowEventually(ctx, of)
	if err != nil {
		skipOrFailIntegration(t, "OpenFlow endpoint %s is not reachable after configuring %s on bridge %s: %v", openFlowAddr, controllerTarget, bridge, err)
	}
	if !openFlowDumpContainsCookie(t, ctx, session, rule.Cookie) {
		_ = session.Close()
		t.Fatalf("live OpenFlow dump did not contain SD-WAN hook cookie %#x", rule.Cookie)
	}
	_ = session.Close()

	if err := backend.DeleteSDWAN(ctx, networkName); err != nil {
		t.Fatalf("DeleteSDWAN() = %v", err)
	}
	session, err = dialOpenFlowEventually(ctx, of)
	if err != nil {
		t.Fatalf("dial OpenFlow endpoint after SD-WAN hook delete: %v", err)
	}
	defer session.Close()
	if openFlowDumpContainsCookie(t, ctx, session, rule.Cookie) {
		t.Fatalf("live OpenFlow dump still contains SD-WAN hook cookie %#x", rule.Cookie)
	}
}

func TestIntegrationSDWANWireGuardLinuxRouteLifecycle(t *testing.T) {
	if !ovnflow.EnvGateEnabled(os.Getenv(ovnflow.EnvSDWANBackendChecks)) ||
		!ovnflow.EnvGateEnabled(os.Getenv(ovnflow.EnvSDWANPrivilegedChecks)) ||
		!ovnflow.EnvGateEnabled(os.Getenv(ovnflow.EnvWireGuardChecks)) ||
		!ovnflow.EnvGateEnabled(os.Getenv(ovnflow.EnvLinuxRouteChecks)) {
		t.Skipf("set %s=1, %s=1, %s=1, and %s=1 to run privileged SD-WAN WireGuard/route checks",
			ovnflow.EnvSDWANBackendChecks, ovnflow.EnvSDWANPrivilegedChecks, ovnflow.EnvWireGuardChecks, ovnflow.EnvLinuxRouteChecks)
	}
	if os.Geteuid() != 0 {
		t.Fatal("privileged SD-WAN WireGuard/route checks require root or equivalent CAP_NET_ADMIN")
	}
	requireCommand(t, "ip")
	requireCommand(t, "wg")

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	suffix := integrationSuffix()
	networkName := "ofw" + suffix
	keyPath := t.TempDir() + "/wg.key"
	privateKey, _ := wireGuardKeyPair(t, ctx)
	_, remotePublicKey := wireGuardKeyPair(t, ctx)
	if err := os.WriteFile(keyPath, []byte(privateKey+"\n"), 0o600); err != nil {
		t.Fatalf("write WireGuard private key: %v", err)
	}

	backend, err := NewBackend(Config{
		LocalSite:       "edge-a",
		InterfacePrefix: "of",
		RouteTable:      51821,
	})
	if err != nil {
		t.Fatalf("NewBackend() = %v", err)
	}
	link := ovnflow.SDWANLink{
		From:       "edge-a",
		To:         "edge-b",
		Transport:  ovnflow.SDWANTransportWireGuard,
		AllowedIPs: []string{"198.51.100.0/32"},
	}
	network := ovnflow.SDWANNetwork{
		Name:      networkName,
		Layer:     ovnflow.SDWANLayerL3,
		Transport: ovnflow.SDWANTransportWireGuard,
		Sites: []ovnflow.SDWANSite{
			{Name: "edge-a", Router: "edge-a", CIDRs: []string{"10.0.0.0/24"}, Attributes: map[string]string{"wireguard_private_key_file": keyPath}},
			{Name: "edge-b", Router: "edge-b", CIDRs: []string{"198.51.100.0/32"}, Endpoint: "127.0.0.1:51820", PublicKey: remotePublicKey},
		},
		Links: []ovnflow.SDWANLink{link},
	}
	iface := backend.wireGuardInterface(network, link)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_ = backend.DeleteSDWAN(cleanupCtx, networkName)
		_ = (SystemExecutor{}).Run(cleanupCtx, Command{Program: "ip", Args: []string{"link", "delete", iface}, IgnoreNotFound: true})
	})

	if err := backend.ApplySDWAN(ctx, network, ovnflow.SDWANApplyPlan{}); err != nil {
		t.Fatalf("ApplySDWAN() = %v", err)
	}
	if output, err := exec.CommandContext(ctx, "ip", "link", "show", "dev", iface).CombinedOutput(); err != nil {
		t.Fatalf("ip link show dev %s: %v: %s", iface, err, string(output))
	}
	if output, err := exec.CommandContext(ctx, "wg", "show", iface).CombinedOutput(); err != nil {
		t.Fatalf("wg show %s: %v: %s", iface, err, string(output))
	}
	if output, err := exec.CommandContext(ctx, "ip", "route", "show", "table", "51821", "198.51.100.0/32").CombinedOutput(); err != nil || !strings.Contains(string(output), iface) {
		t.Fatalf("route table missing %s route via %s: err=%v output=%s", "198.51.100.0/32", iface, err, string(output))
	}
	if output, err := exec.CommandContext(ctx, "ip", "rule", "show").CombinedOutput(); err != nil || !strings.Contains(string(output), "lookup 51821") {
		t.Fatalf("policy rule lookup 51821 missing: err=%v output=%s", err, string(output))
	}

	if err := backend.DeleteSDWAN(ctx, networkName); err != nil {
		t.Fatalf("DeleteSDWAN() = %v", err)
	}
	if output, err := exec.CommandContext(ctx, "ip", "link", "show", "dev", iface).CombinedOutput(); err == nil {
		t.Fatalf("WireGuard interface %s still exists after delete: %s", iface, string(output))
	}
}

func requireCommand(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Fatalf("%s not available: %v", name, err)
	}
}

func wireGuardKeyPair(t *testing.T, ctx context.Context) (string, string) {
	t.Helper()
	privateBytes, err := exec.CommandContext(ctx, "wg", "genkey").Output()
	if err != nil {
		t.Fatalf("wg genkey: %v", err)
	}
	privateKey := strings.TrimSpace(string(privateBytes))
	cmd := exec.CommandContext(ctx, "wg", "pubkey")
	cmd.Stdin = strings.NewReader(privateKey + "\n")
	publicBytes, err := cmd.Output()
	if err != nil {
		t.Fatalf("wg pubkey: %v", err)
	}
	return privateKey, strings.TrimSpace(string(publicBytes))
}

func envOrDefault(name, fallback string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	return value
}

func skipOrFailIntegration(t *testing.T, format string, args ...any) {
	t.Helper()
	if ovnflow.EnvGateEnabled(os.Getenv(ovnflow.EnvRequireIntegration)) || ovnflow.EnvGateEnabled(os.Getenv("CI")) {
		t.Fatalf(format, args...)
	}
	t.Skipf(format, args...)
}

func integrationSuffix() string {
	return strings.ToLower(strconv.FormatInt(time.Now().UTC().UnixNano(), 36))
}

func controllerTargetFromEndpoint(endpoint string) string {
	if strings.HasPrefix(endpoint, "tcp:") {
		parts := strings.Split(strings.TrimPrefix(endpoint, "tcp:"), ":")
		if len(parts) == 2 {
			return "ptcp:" + parts[1] + ":0.0.0.0"
		}
	}
	return ""
}

func dialOpenFlowEventually(ctx context.Context, client *ovnflow.OpenFlowClient) (*ovnflow.OpenFlowSession, error) {
	var lastErr error
	deadline := time.Now().Add(5 * time.Second)
	for {
		session, err := client.Dial(ctx)
		if err == nil {
			return session, nil
		}
		lastErr = err
		if time.Now().After(deadline) || ctx.Err() != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, lastErr
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func openFlowDumpContainsCookie(t *testing.T, ctx context.Context, session *ovnflow.OpenFlowSession, cookie uint64) bool {
	t.Helper()
	replies, err := session.DumpFlows(ctx, ovnflow.OpenFlowFlowStatsRequest{Cookie: cookie, CookieMask: ^uint64(0)})
	if err != nil {
		t.Fatalf("dump live OpenFlow rules: %v", err)
	}
	want := make([]byte, 8)
	binary.BigEndian.PutUint64(want, cookie)
	for _, reply := range replies {
		_, _, body, err := ovnflow.ParseOpenFlowMultipartReply(reply)
		if err != nil {
			t.Fatalf("parse live OpenFlow multipart reply: %v", err)
		}
		if bytes.Contains(body, want) {
			return true
		}
	}
	return false
}

func ovsSet(values ...any) any {
	set, _ := ovsdb.NewOvsSet(values)
	return set
}

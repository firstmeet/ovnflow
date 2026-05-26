//go:build linux && integration

package linuxrouter

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/firstmeet/ovnflow/v2"
	"golang.org/x/sys/unix"
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
	requireCommand(t, "dnsmasq")
	ns := "ovnflow-it-" + strconv.FormatInt(time.Now().UTC().UnixNano(), 10)
	runDir := t.TempDir()
	dnsmasqPort := 1053
	dnsmasqConfig := filepath.Join(runDir, "dnsmasq.conf")
	dnsmasqPID := filepath.Join(runDir, "dnsmasq.pid")
	dnsmasqLease := filepath.Join(runDir, "dnsmasq.leases")
	if err := os.WriteFile(dnsmasqConfig, []byte(fmt.Sprintf("port=%d\nno-resolv\nno-hosts\ninterface=lo\nlisten-address=127.0.0.1\nbind-interfaces\n", dnsmasqPort)), 0o600); err != nil {
		t.Fatalf("write dnsmasq config: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		killPIDFile(dnsmasqPID)
		_ = (SystemExecutor{}).Run(cleanupCtx, Command{Program: "ip", Args: []string{"netns", "delete", ns}, IgnoreNotFound: true})
		if err := runReadOnly(cleanupCtx, "ip", "netns", "exec", ns, "true"); err == nil || !commandMissingNamespace(err) {
			t.Errorf("namespace %s still exists after cleanup: %v", ns, err)
		}
	})
	client := NewObservedClient(SystemExecutor{}, LinuxRenderer{NATBackend: backend}, LinuxObserver{NATBackend: backend})
	err := client.Router("edge").Apply(ctx, Router{
		Name: "edge",
		Spec: Spec{
			Namespace:  ns,
			Interfaces: []Interface{{Name: "lo", Role: InterfaceLAN, Addresses: []string{"127.0.0.1/8"}}},
			DNSMasq: DNSMasq{
				Enabled:    true,
				ConfigFile: dnsmasqConfig,
				PIDFile:    dnsmasqPID,
				LeaseFile:  dnsmasqLease,
				Hosts: []HostRecord{{
					Domain: "api.service",
					IPs:    []string{"127.0.0.10", "127.0.0.11"},
				}},
			},
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
	got, err := waitRouterStatus(ctx, client, "edge", func(router Router) bool {
		return router.Status.DNSMasq.Running && len(router.Status.InstalledNAT) >= 6 && len(router.Status.InstalledFirewall) >= 1
	})
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
	if !got.Status.DNSMasq.Running || got.Status.DNSMasq.PID <= 0 {
		t.Fatalf("observed dnsmasq missing running process: %#v", got.Status.DNSMasq)
	}
	if !processHasArgs(got.Status.DNSMasq.PID, "--host-record=api.service,127.0.0.10", "--host-record=api.service,127.0.0.11") {
		t.Fatalf("dnsmasq process %d missing expected multi-IP host-record arguments", got.Status.DNSMasq.PID)
	}
	answers, err := waitAInNamespace(ctx, ns, "api.service", dnsmasqPort, []string{"127.0.0.10", "127.0.0.11"})
	if err != nil {
		if isOperationNotPermitted(err) {
			t.Logf("skipping UDP dnsmasq query in this runner: %v", err)
			answers = nil
		} else {
			t.Fatalf("dnsmasq query returned error: %v", err)
		}
	}
	if err == nil {
		for _, ip := range []string{"127.0.0.10", "127.0.0.11"} {
			if !hasString(answers, ip) {
				t.Fatalf("dnsmasq answers missing %s: %#v", ip, answers)
			}
		}
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

func TestDNSQueryHelperProcess(t *testing.T) {
	if os.Getenv("OVNFLOW_LINUXROUTER_DNS_QUERY_HELPER") != "1" {
		return
	}
	domain := os.Getenv("OVNFLOW_LINUXROUTER_DNS_QUERY_DOMAIN")
	port, err := strconv.Atoi(os.Getenv("OVNFLOW_LINUXROUTER_DNS_QUERY_PORT"))
	if domain == "" || err != nil || port <= 0 {
		fmt.Fprintln(os.Stderr, "invalid DNS query helper input")
		os.Exit(2)
	}
	answers, err := queryA(domain, port)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	for _, answer := range answers {
		fmt.Fprintln(os.Stdout, answer)
	}
	os.Exit(0)
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

func waitRouterStatus(ctx context.Context, client *Client, name string, ready func(Router) bool) (Router, error) {
	var last Router
	var lastErr error
	deadline := time.Now().Add(5 * time.Second)
	for {
		last, lastErr = client.Router(name).Get(ctx)
		if lastErr == nil && ready(last) {
			return last, nil
		}
		if time.Now().After(deadline) {
			if lastErr != nil {
				return Router{}, lastErr
			}
			return last, nil
		}
		select {
		case <-ctx.Done():
			return Router{}, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func killPIDFile(path string) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return
	}
	pid, err := strconv.Atoi(string(bytesTrimSpace(data)))
	if err != nil || pid <= 0 {
		return
	}
	_ = unix.Kill(pid, unix.SIGTERM)
	deadline := time.Now().Add(2 * time.Second)
	for processAlive(pid) && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if processAlive(pid) {
		_ = unix.Kill(pid, unix.SIGKILL)
	}
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := unix.Kill(pid, 0)
	return err == nil || err == unix.EPERM
}

func processHasArgs(pid int, values ...string) bool {
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "cmdline"))
	if err != nil {
		return false
	}
	cmdline := string(data)
	for _, value := range values {
		if !containsNULSeparatedValue(cmdline, value) {
			return false
		}
	}
	return true
}

func containsNULSeparatedValue(cmdline, value string) bool {
	start := 0
	for i := 0; i <= len(cmdline); i++ {
		if i != len(cmdline) && cmdline[i] != 0 {
			continue
		}
		if cmdline[start:i] == value {
			return true
		}
		start = i + 1
	}
	return false
}

func waitAInNamespace(ctx context.Context, ns, domain string, port int, expected []string) ([]string, error) {
	deadline := time.Now().Add(5 * time.Second)
	var lastAnswers []string
	var lastErr error
	for {
		lastAnswers, lastErr = queryAInNamespace(ctx, ns, domain, port)
		if lastErr == nil {
			all := true
			for _, ip := range expected {
				if !hasString(lastAnswers, ip) {
					all = false
					break
				}
			}
			if all {
				return lastAnswers, nil
			}
		}
		if time.Now().After(deadline) {
			if lastErr != nil {
				return nil, lastErr
			}
			return lastAnswers, fmt.Errorf("DNS answers missing expected records: got %#v want %#v", lastAnswers, expected)
		}
		select {
		case <-ctx.Done():
			if lastErr != nil {
				return nil, lastErr
			}
			return lastAnswers, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func queryAInNamespace(ctx context.Context, ns, domain string, port int) ([]string, error) {
	cmd := exec.CommandContext(ctx, "ip", "netns", "exec", ns, os.Args[0], "-test.run=TestDNSQueryHelperProcess")
	cmd.Env = append(os.Environ(),
		"OVNFLOW_LINUXROUTER_DNS_QUERY_HELPER=1",
		"OVNFLOW_LINUXROUTER_DNS_QUERY_DOMAIN="+domain,
		"OVNFLOW_LINUXROUTER_DNS_QUERY_PORT="+strconv.Itoa(port),
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	var answers []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			answers = append(answers, line)
		}
	}
	return answers, nil
}

func isOperationNotPermitted(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "operation not permitted")
}

func queryA(domain string, port int) ([]string, error) {
	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_DGRAM|unix.SOCK_CLOEXEC, unix.IPPROTO_UDP)
	if err != nil {
		return nil, err
	}
	defer unix.Close(fd)
	timeout := unix.NsecToTimeval((2 * time.Second).Nanoseconds())
	_ = unix.SetsockoptTimeval(fd, unix.SOL_SOCKET, unix.SO_RCVTIMEO, &timeout)
	_ = unix.SetsockoptTimeval(fd, unix.SOL_SOCKET, unix.SO_SNDTIMEO, &timeout)
	queryID := uint16(0x4f4e)
	query, err := buildDNSAQuery(queryID, domain)
	if err != nil {
		return nil, err
	}
	addr := &unix.SockaddrInet4{Port: port, Addr: [4]byte{127, 0, 0, 1}}
	if err := unix.Sendto(fd, query, 0, addr); err != nil {
		return nil, err
	}
	buf := make([]byte, 512)
	n, _, err := unix.Recvfrom(fd, buf, 0)
	if err != nil {
		return nil, err
	}
	return parseDNSAResponse(queryID, buf[:n])
}

func buildDNSAQuery(id uint16, domain string) ([]byte, error) {
	msg := make([]byte, 12, 64)
	binary.BigEndian.PutUint16(msg[0:2], id)
	binary.BigEndian.PutUint16(msg[2:4], 0x0100)
	binary.BigEndian.PutUint16(msg[4:6], 1)
	for _, label := range splitDomain(domain) {
		if len(label) == 0 || len(label) > 63 {
			return nil, fmt.Errorf("invalid DNS label %q", label)
		}
		msg = append(msg, byte(len(label)))
		msg = append(msg, label...)
	}
	msg = append(msg, 0, 0, 1, 0, 1)
	return msg, nil
}

func parseDNSAResponse(id uint16, msg []byte) ([]string, error) {
	if len(msg) < 12 {
		return nil, fmt.Errorf("short DNS response")
	}
	if binary.BigEndian.Uint16(msg[0:2]) != id {
		return nil, fmt.Errorf("unexpected DNS response id")
	}
	if rcode := msg[3] & 0x0f; rcode != 0 {
		return nil, fmt.Errorf("DNS response rcode %d", rcode)
	}
	qd := int(binary.BigEndian.Uint16(msg[4:6]))
	an := int(binary.BigEndian.Uint16(msg[6:8]))
	offset := 12
	var err error
	for i := 0; i < qd; i++ {
		offset, err = skipDNSName(msg, offset)
		if err != nil {
			return nil, err
		}
		offset += 4
		if offset > len(msg) {
			return nil, fmt.Errorf("truncated DNS question")
		}
	}
	var answers []string
	for i := 0; i < an; i++ {
		offset, err = skipDNSName(msg, offset)
		if err != nil {
			return nil, err
		}
		if offset+10 > len(msg) {
			return nil, fmt.Errorf("truncated DNS answer")
		}
		typ := binary.BigEndian.Uint16(msg[offset : offset+2])
		class := binary.BigEndian.Uint16(msg[offset+2 : offset+4])
		rdlen := int(binary.BigEndian.Uint16(msg[offset+8 : offset+10]))
		offset += 10
		if offset+rdlen > len(msg) {
			return nil, fmt.Errorf("truncated DNS rdata")
		}
		if typ == 1 && class == 1 && rdlen == 4 {
			answers = append(answers, net.IPv4(msg[offset], msg[offset+1], msg[offset+2], msg[offset+3]).String())
		}
		offset += rdlen
	}
	return answers, nil
}

func skipDNSName(msg []byte, offset int) (int, error) {
	for {
		if offset >= len(msg) {
			return 0, fmt.Errorf("truncated DNS name")
		}
		length := int(msg[offset])
		if length&0xc0 == 0xc0 {
			if offset+1 >= len(msg) {
				return 0, fmt.Errorf("truncated DNS compression pointer")
			}
			return offset + 2, nil
		}
		offset++
		if length == 0 {
			return offset, nil
		}
		if length&0xc0 != 0 || offset+length > len(msg) {
			return 0, fmt.Errorf("invalid DNS name")
		}
		offset += length
	}
}

func splitDomain(domain string) []string {
	var labels []string
	start := 0
	for i := 0; i <= len(domain); i++ {
		if i != len(domain) && domain[i] != '.' {
			continue
		}
		labels = append(labels, domain[start:i])
		start = i + 1
	}
	return labels
}

func bytesTrimSpace(data []byte) []byte {
	start, end := 0, len(data)
	for start < end && (data[start] == ' ' || data[start] == '\n' || data[start] == '\r' || data[start] == '\t') {
		start++
	}
	for end > start && (data[end-1] == ' ' || data[end-1] == '\n' || data[end-1] == '\r' || data[end-1] == '\t') {
		end--
	}
	return data[start:end]
}

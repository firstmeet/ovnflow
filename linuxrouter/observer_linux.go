//go:build linux

package linuxrouter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/firstmeet/ovnflow"
)

type LinuxObserver struct {
	NATBackend string
}

var ownedCommentPattern = regexp.MustCompile(`ovnflow:([A-Za-z0-9._/@:+-]+)`)

func (o LinuxObserver) Observe(ctx context.Context, router Router) (Status, error) {
	ns := router.Spec.namespaceOrDefault(router.Name)
	status := Status{Namespace: ns, NATBackend: normalizedNATBackend(o.NATBackend)}
	if err := runReadOnly(ctx, "ip", "netns", "exec", ns, "true"); err != nil {
		if commandMissingNamespace(err) {
			status.Exists = false
			return status, nil
		}
		return status, err
	}
	status.Exists = true

	ifaces, err := observeInterfaces(ctx, ns, router.Spec.Interfaces)
	if err != nil {
		return status, err
	}
	status.Interfaces = ifaces

	routes, err := observeRoutes(ctx, ns)
	if err != nil {
		return status, err
	}
	status.Routes = routes

	dnsmasq, err := observeDNSMasq(router.Spec.DNSMasq, ns)
	if err != nil {
		return status, err
	}
	status.DNSMasq = dnsmasq

	backend, installedNAT, installedFirewall, err := o.observeOwnedRules(ctx, ns)
	if err != nil {
		return status, err
	}
	if backend != "" {
		status.NATBackend = backend
	}
	status.InstalledNAT = installedNAT
	status.InstalledFirewall = installedFirewall
	return status, nil
}

func observeInterfaces(ctx context.Context, ns string, specs []Interface) ([]InterfaceStatus, error) {
	out, err := commandOutput(ctx, "ip", "netns", "exec", ns, "ip", "-j", "addr", "show")
	if err != nil {
		return nil, err
	}
	statuses, err := parseIPAddrJSON(out, interfaceRoles(specs))
	if err != nil {
		return nil, err
	}
	return statuses, nil
}

func observeRoutes(ctx context.Context, ns string) ([]RouteStatus, error) {
	out, err := commandOutput(ctx, "ip", "netns", "exec", ns, "ip", "-j", "route", "show", "table", "main")
	if err != nil {
		return nil, err
	}
	routes, err := parseIPRouteJSON(out)
	if err != nil {
		return nil, err
	}
	return routes, nil
}

func observeDNSMasq(spec DNSMasq, ns string) (DNSMasqStatus, error) {
	if !spec.Enabled || strings.TrimSpace(spec.PIDFile) == "" {
		return DNSMasqStatus{}, nil
	}
	data, err := os.ReadFile(filepath.Clean(spec.PIDFile))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return DNSMasqStatus{}, nil
		}
		return DNSMasqStatus{}, classifyObserveError("dnsmasq", spec.PIDFile, err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return DNSMasqStatus{}, &ovnflow.Error{Kind: ovnflow.ErrorUnavailable, Operation: "observe", Object: spec.PIDFile, Message: "invalid dnsmasq pidfile", Err: err}
	}
	comm, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "comm"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return DNSMasqStatus{}, nil
		}
		return DNSMasqStatus{}, classifyObserveError("dnsmasq", spec.PIDFile, err)
	}
	if strings.TrimSpace(string(comm)) != "dnsmasq" {
		return DNSMasqStatus{}, nil
	}
	if !processInNamespace(pid, ns) {
		return DNSMasqStatus{}, nil
	}
	return DNSMasqStatus{Running: true, PID: pid}, nil
}

func (o LinuxObserver) observeOwnedRules(ctx context.Context, ns string) (string, []string, []string, error) {
	backend := normalizedNATBackend(o.NATBackend)
	if backend == ovnflow.NATBackendIPTables {
		nat, firewall, err := observeIPTablesRules(ctx, ns)
		return ovnflow.NATBackendIPTables, nat, firewall, err
	}
	nftNAT, nftFirewall, nftErr := observeNFTRules(ctx, ns)
	if backend == ovnflow.NATBackendNFTables {
		return ovnflow.NATBackendNFTables, nftNAT, nftFirewall, nftErr
	}
	if nftErr == nil && (len(nftNAT) > 0 || len(nftFirewall) > 0) {
		return ovnflow.NATBackendNFTables, nftNAT, nftFirewall, nil
	}
	iptNAT, iptFirewall, iptErr := observeIPTablesRules(ctx, ns)
	if iptErr == nil && (len(iptNAT) > 0 || len(iptFirewall) > 0) {
		return ovnflow.NATBackendIPTables, iptNAT, iptFirewall, nil
	}
	if nftErr == nil {
		return ovnflow.NATBackendAuto, nftNAT, nftFirewall, nil
	}
	if iptErr == nil {
		return ovnflow.NATBackendAuto, iptNAT, iptFirewall, nil
	}
	return ovnflow.NATBackendAuto, nil, nil, nftErr
}

func observeNFTRules(ctx context.Context, ns string) ([]string, []string, error) {
	natOut, natErr := commandOutput(ctx, "ip", "netns", "exec", ns, "nft", "-j", "list", "table", "ip", "ovnflow_nat")
	if natErr != nil && !commandMissingTable(natErr) {
		return nil, nil, natErr
	}
	firewallOut, firewallErr := commandOutput(ctx, "ip", "netns", "exec", ns, "nft", "-j", "list", "table", "inet", "ovnflow_filter")
	if firewallErr != nil && !commandMissingTable(firewallErr) {
		return nil, nil, firewallErr
	}
	return parseOwnedComments(natOut), parseOwnedComments(firewallOut), nil
}

func observeIPTablesRules(ctx context.Context, ns string) ([]string, []string, error) {
	natOut, natErr := commandOutput(ctx, "ip", "netns", "exec", ns, "iptables-save", "-t", "nat")
	if natErr != nil && !commandMissingTable(natErr) {
		return nil, nil, natErr
	}
	firewallOut, firewallErr := commandOutput(ctx, "ip", "netns", "exec", ns, "iptables-save")
	if firewallErr != nil && !commandMissingTable(firewallErr) {
		return nil, nil, firewallErr
	}
	return parseIPTablesTableComments(natOut, "nat"), parseIPTablesTableComments(firewallOut, "filter"), nil
}

func parseIPAddrJSON(data []byte, roles map[string]InterfaceRole) ([]InterfaceStatus, error) {
	var rows []struct {
		IfName   string   `json:"ifname"`
		Oper     string   `json:"operstate"`
		Flags    []string `json:"flags"`
		AddrInfo []struct {
			Local     string `json:"local"`
			PrefixLen int    `json:"prefixlen"`
		} `json:"addr_info"`
	}
	if err := json.Unmarshal(data, &rows); err != nil {
		return nil, &ovnflow.Error{Kind: ovnflow.ErrorUnavailable, Operation: "observe", Object: "ip addr", Message: "invalid ip addr json", Err: err}
	}
	statuses := make([]InterfaceStatus, 0, len(rows))
	for _, row := range rows {
		if row.IfName == "" {
			continue
		}
		status := InterfaceStatus{Name: row.IfName, Role: roles[row.IfName], Up: strings.EqualFold(row.Oper, "UP") || contains(row.Flags, "UP")}
		for _, addr := range row.AddrInfo {
			if addr.Local == "" || addr.PrefixLen == 0 {
				continue
			}
			status.Addresses = append(status.Addresses, fmt.Sprintf("%s/%d", addr.Local, addr.PrefixLen))
		}
		statuses = append(statuses, status)
	}
	sortInterfaceStatuses(statuses)
	return statuses, nil
}

func parseIPRouteJSON(data []byte) ([]RouteStatus, error) {
	var rows []struct {
		Dst     string `json:"dst"`
		Gateway string `json:"gateway"`
		Dev     string `json:"dev"`
	}
	if err := json.Unmarshal(data, &rows); err != nil {
		return nil, &ovnflow.Error{Kind: ovnflow.ErrorUnavailable, Operation: "observe", Object: "ip route", Message: "invalid ip route json", Err: err}
	}
	routes := make([]RouteStatus, 0, len(rows))
	for _, row := range rows {
		dst := strings.TrimSpace(row.Dst)
		if dst == "" || dst == "default" {
			dst = "0.0.0.0/0"
		}
		routes = append(routes, RouteStatus{Destination: dst, Gateway: row.Gateway, Interface: row.Dev})
	}
	sortRouteStatuses(routes)
	return routes, nil
}

func parseOwnedComments(data []byte) []string {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	matches := ownedCommentPattern.FindAllStringSubmatch(string(data), -1)
	var names []string
	for _, match := range matches {
		if len(match) == 2 {
			names = append(names, match[1])
		}
	}
	return uniqueSortedStrings(names)
}

func parseIPTablesTableComments(data []byte, table string) []string {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	table = strings.ToLower(strings.TrimSpace(table))
	inTable := false
	var names []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "*") {
			inTable = strings.EqualFold(strings.TrimPrefix(line, "*"), table)
			continue
		}
		if line == "COMMIT" {
			inTable = false
			continue
		}
		if !inTable {
			continue
		}
		names = append(names, parseOwnedComments([]byte(line))...)
	}
	return uniqueSortedStrings(names)
}

func commandOutput(ctx context.Context, program string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, program, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, commandError(program, strings.TrimSpace(stderr.String()), err)
	}
	return out, nil
}

func runReadOnly(ctx context.Context, program string, args ...string) error {
	cmd := exec.CommandContext(ctx, program, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return commandError(program, strings.TrimSpace(stderr.String()), err)
	}
	return nil
}

func commandError(program, message string, err error) error {
	if message == "" {
		message = err.Error()
	}
	return &ovnflow.Error{Kind: errorKindForExec(err), Operation: "observe", Object: program, Message: message, Err: err}
}

func classifyObserveError(op, object string, err error) error {
	return &ovnflow.Error{Kind: ovnflow.ErrorUnavailable, Operation: "observe-" + op, Object: object, Message: err.Error(), Err: err}
}

func commandMissingNamespace(err error) bool {
	var ovnErr *ovnflow.Error
	if !errors.As(err, &ovnErr) {
		return false
	}
	message := strings.ToLower(ovnErr.Message)
	return strings.Contains(message, "cannot open network namespace") ||
		strings.Contains(message, "no such file") ||
		strings.Contains(message, "not found") ||
		strings.Contains(message, "invalid netns")
}

func commandMissingTable(err error) bool {
	var ovnErr *ovnflow.Error
	if !errors.As(err, &ovnErr) {
		return false
	}
	message := strings.ToLower(ovnErr.Message)
	return strings.Contains(message, "no such file") ||
		strings.Contains(message, "no such table") ||
		strings.Contains(message, "table") && strings.Contains(message, "does not exist") ||
		strings.Contains(message, "does a matching rule exist")
}

func processInNamespace(pid int, ns string) bool {
	processNS, err := os.Readlink(filepath.Join("/proc", strconv.Itoa(pid), "ns", "net"))
	if err != nil {
		return false
	}
	for _, path := range []string{filepath.Join("/run/netns", ns), filepath.Join("/var/run/netns", ns)} {
		nsLink, err := os.Readlink(path)
		if err == nil && nsLink == processNS {
			return true
		}
	}
	return false
}

func interfaceRoles(specs []Interface) map[string]InterfaceRole {
	roles := map[string]InterfaceRole{}
	for _, spec := range specs {
		if spec.Name != "" {
			roles[spec.Name] = spec.Role
		}
	}
	return roles
}

func normalizedNATBackend(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ovnflow.NATBackendAuto
	}
	return value
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if strings.EqualFold(candidate, value) {
			return true
		}
	}
	return false
}

func sortInterfaceStatuses(statuses []InterfaceStatus) {
	for i := range statuses {
		sortStrings(statuses[i].Addresses)
	}
	sort.Slice(statuses, func(i, j int) bool { return statuses[i].Name < statuses[j].Name })
}

func sortRouteStatuses(routes []RouteStatus) {
	sort.Slice(routes, func(i, j int) bool {
		if routes[i].Destination != routes[j].Destination {
			return routes[i].Destination < routes[j].Destination
		}
		if routes[i].Gateway != routes[j].Gateway {
			return routes[i].Gateway < routes[j].Gateway
		}
		return routes[i].Interface < routes[j].Interface
	})
}

func sortStrings(values []string) {
	sort.Strings(values)
}

//go:build linux

package linuxrouter

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"os/exec"
	"strconv"
	"strings"

	"github.com/firstmeet/ovnflow/v2"
)

type SystemExecutor struct{}

func (SystemExecutor) Run(ctx context.Context, cmd Command) error {
	if strings.TrimSpace(cmd.Program) == "" {
		return &ovnflow.Error{Kind: ovnflow.ErrorValidation, Operation: "exec", Object: cmd.Program, Message: "program must not be empty"}
	}
	execCmd := exec.CommandContext(ctx, cmd.Program, cmd.Args...)
	var stderr bytes.Buffer
	execCmd.Stderr = &stderr
	if err := execCmd.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if cmd.IgnoreNotFound && commandNotFound(message) {
			return nil
		}
		if cmd.IgnoreAlreadyExists && commandAlreadyExists(message) {
			return nil
		}
		return &ovnflow.Error{Kind: errorKindForExec(err), Operation: "exec", Object: cmd.Program, Message: message, Err: err}
	}
	return nil
}

type LinuxRenderer struct {
	NATBackend string
}

func (r LinuxRenderer) RenderApply(router Router) ([]Command, error) {
	if err := inferNATInterfaces(&router.Spec); err != nil {
		return nil, err
	}
	backend := strings.ToLower(strings.TrimSpace(r.NATBackend))
	if backend == "" {
		backend = ovnflow.NATBackendAuto
	}
	if !ovnflow.ValidNATBackend(backend) {
		return nil, &ovnflow.Error{Kind: ovnflow.ErrorValidation, Operation: "render", Object: backend, Message: "invalid NAT backend"}
	}
	ns := router.Spec.namespaceOrDefault(router.Name)
	var commands []Command
	commands = append(commands, Command{Program: "ip", Args: []string{"netns", "add", ns}, IgnoreAlreadyExists: true})
	for _, iface := range router.Spec.Interfaces {
		commands = append(commands, renderInterfaceCommands(router.Name, ns, iface)...)
	}
	for _, route := range router.Spec.Routes {
		commands = append(commands, renderRouteCommand(ns, route))
	}
	if router.Spec.DNSMasq.Enabled {
		commands = append(commands, renderDNSMasqCommand(ns, router)...)
	}
	scope := routerRuleScope(router.Name, ns)
	commands = append(commands, renderNATCommands(ns, scope, backend, router.Spec.NAT)...)
	commands = append(commands, renderFirewallCommands(ns, scope, backend, router.Spec.Firewall)...)
	return commands, nil
}

func renderInterfaceCommands(routerName, ns string, iface Interface) []Command {
	var commands []Command
	if iface.Bridge != "" && iface.OVSPort != "" {
		commands = append(commands,
			linuxRouterOVSOwnershipGuardCommand("Port", iface.OVSPort, ns),
			linuxRouterOVSOwnershipGuardCommand("Interface", iface.OVSPort, ns),
		)
		commands = append(commands, Command{Program: "ovs-vsctl", Args: []string{
			"--may-exist", "add-port", iface.Bridge, iface.OVSPort,
			"--", "set", "Port", iface.OVSPort,
			"external_ids:ovnflow.io/managed-by=ovnflow",
			"external_ids:ovnflow.io/api-version=v2",
			"external_ids:ovnflow.io/kind=LinuxRouter",
			"external_ids:ovnflow.io/name=" + routerName,
			"external_ids:ovnflow.io/linux-router-ns=" + ns,
			"external_ids:ovnflow.io/linux-router-iface=" + iface.Name,
			"--", "set", "Interface", iface.OVSPort,
			"type=internal",
			"external_ids:ovnflow.io/managed-by=ovnflow",
			"external_ids:ovnflow.io/api-version=v2",
			"external_ids:ovnflow.io/kind=LinuxRouter",
			"external_ids:ovnflow.io/name=" + routerName,
			"external_ids:ovnflow.io/linux-router-ns=" + ns,
			"external_ids:ovnflow.io/linux-router-iface=" + iface.Name,
		}})
		commands = append(commands,
			Command{Program: "ip", Args: []string{"link", "set", iface.OVSPort, "netns", ns}, IgnoreNotFound: true},
			Command{Program: "ip", Args: []string{"netns", "exec", ns, "ip", "link", "set", iface.OVSPort, "name", iface.Name}, IgnoreNotFound: true},
		)
	}
	commands = append(commands, Command{Program: "ip", Args: []string{"netns", "exec", ns, "ip", "link", "set", iface.Name, "up"}})
	for _, address := range iface.Addresses {
		commands = append(commands, Command{Program: "ip", Args: []string{"netns", "exec", ns, "ip", "addr", "replace", address, "dev", iface.Name}})
	}
	if iface.Gateway != "" {
		commands = append(commands, Command{Program: "ip", Args: []string{"netns", "exec", ns, "ip", "route", "replace", "default", "via", iface.Gateway, "dev", iface.Name}})
	}
	if iface.DHCPClient {
		commands = append(commands, Command{Program: "ip", Args: []string{"netns", "exec", ns, "dhclient", "-v", iface.Name}})
	}
	return commands
}

func renderRouteCommand(ns string, route Route) Command {
	args := []string{"netns", "exec", ns, "ip", "route", "replace", route.Destination}
	if route.Gateway != "" {
		args = append(args, "via", route.Gateway)
	}
	if route.Interface != "" {
		args = append(args, "dev", route.Interface)
	}
	return Command{Program: "ip", Args: args}
}

func renderDNSMasqCommand(ns string, router Router) []Command {
	args := []string{"netns", "exec", ns, "dnsmasq"}
	if router.Spec.DNSMasq.ConfigFile != "" {
		args = append(args, "--conf-file="+router.Spec.DNSMasq.ConfigFile)
	}
	if router.Spec.DNSMasq.PIDFile != "" {
		args = append(args, "--pid-file="+router.Spec.DNSMasq.PIDFile)
	}
	if router.Spec.DNSMasq.LeaseFile != "" {
		args = append(args, "--dhcp-leasefile="+router.Spec.DNSMasq.LeaseFile)
	}
	for _, server := range router.Spec.DNSMasq.Servers {
		args = append(args, "--server="+server)
	}
	for _, dhcpRange := range router.Spec.DNSMasq.DHCPRanges {
		args = append(args, "--dhcp-range="+strings.Join([]string{dhcpRange.Start, dhcpRange.End, dhcpRange.Lease}, ","))
	}
	for _, lease := range router.Spec.DNSMasq.Leases {
		args = append(args, "--dhcp-host="+strings.Join([]string{lease.MAC, lease.IP, lease.Name}, ","))
	}
	for _, host := range router.Spec.DNSMasq.Hosts {
		for _, ip := range host.IPs {
			args = append(args, "--host-record="+host.Domain+","+ip)
		}
	}
	return []Command{{Program: "ip", Args: args}}
}

func renderNATCommands(ns, scope, backend string, nat NAT) []Command {
	if backend == ovnflow.NATBackendIPTables {
		return renderIPTablesNATCommands(ns, scope, nat)
	}
	return renderNFTNATCommands(ns, scope, nat)
}

func renderNFTNATCommands(ns, scope string, nat NAT) []Command {
	table := nftNATTable(scope)
	commands := []Command{
		{Program: "ip", Args: []string{"netns", "exec", ns, "nft", "delete", "table", "ip", table}, IgnoreNotFound: true},
		{Program: "ip", Args: []string{"netns", "exec", ns, "nft", "add", "table", "ip", table}},
		{Program: "ip", Args: []string{"netns", "exec", ns, "nft", "add", "chain", "ip", table, "prerouting", "{", "type", "nat", "hook", "prerouting", "priority", "dstnat", ";", "}"}},
		{Program: "ip", Args: []string{"netns", "exec", ns, "nft", "add", "chain", "ip", table, "postrouting", "{", "type", "nat", "hook", "postrouting", "priority", "srcnat", ";", "}"}},
	}
	for _, rule := range nat.Masquerades {
		commands = append(commands, nftRule(ns, table, "postrouting", "ip", "saddr", rule.SourceCIDR, "oifname", rule.OutInterface, "masquerade", "comment", scopedNftComment(scope, rule.StableName())))
	}
	for _, rule := range nat.SNATRules {
		commands = append(commands, nftRule(ns, table, "postrouting", "ip", "saddr", rule.SourceCIDR, "oifname", rule.OutInterface, "snat", "to", rule.ToSource, "comment", scopedNftComment(scope, rule.StableName())))
	}
	for _, rule := range nat.DNATRules {
		commands = append(commands, nftRule(ns, table, "prerouting", "iifname", rule.InInterface, "ip", "daddr", rule.MatchAddress, "dnat", "to", rule.TargetAddress, "comment", scopedNftComment(scope, rule.StableName())))
	}
	for _, rule := range nat.PortForwards {
		match := []string{"iifname", rule.InInterface}
		if rule.ListenIP != "" {
			match = append(match, "ip", "daddr", rule.ListenIP)
		}
		match = append(match, rule.Protocol, "dport", fmt.Sprint(rule.ListenPort), "dnat", "to", fmt.Sprintf("%s:%d", rule.TargetIP, rule.TargetPort), "comment", scopedNftComment(scope, rule.StableName()))
		commands = append(commands, nftRule(ns, table, append([]string{"prerouting"}, match...)...))
	}
	for _, rule := range nat.DestinationMaps {
		match := []string{"iifname", rule.InInterface, "ip", "daddr", rule.MatchAddress}
		if rule.FromCIDR != "" {
			match = append([]string{"ip", "saddr", rule.FromCIDR}, match...)
		}
		commands = append(commands, nftRule(ns, table, append([]string{"prerouting"}, append(match, "dnat", "to", rule.TargetAddress, "comment", scopedNftComment(scope, rule.StableName()))...)...))
		if rule.SourceNAT != "" {
			commands = append(commands, nftRule(ns, table, "postrouting", "ip", "daddr", rule.TargetAddress, "snat", "to", rule.SourceNAT, "comment", scopedNftComment(scope, rule.StableName()+"-snat")))
		}
	}
	return commands
}

func renderIPTablesNATCommands(ns, scope string, nat NAT) []Command {
	commands := []Command{iptablesCleanupOwnedRules(ns, scope, "nat")}
	for _, rule := range nat.Masquerades {
		commands = append(commands, iptablesReplaceRule(ns, "-t", "nat", "-A", "POSTROUTING", "-s", rule.SourceCIDR, "-o", rule.OutInterface, "-m", "comment", "--comment", scopedIPTablesComment(scope, rule.StableName()), "-j", "MASQUERADE")...)
	}
	for _, rule := range nat.SNATRules {
		commands = append(commands, iptablesReplaceRule(ns, "-t", "nat", "-A", "POSTROUTING", "-s", rule.SourceCIDR, "-o", rule.OutInterface, "-m", "comment", "--comment", scopedIPTablesComment(scope, rule.StableName()), "-j", "SNAT", "--to-source", rule.ToSource)...)
	}
	for _, rule := range nat.DNATRules {
		commands = append(commands, iptablesReplaceRule(ns, "-t", "nat", "-A", "PREROUTING", "-i", rule.InInterface, "-d", rule.MatchAddress, "-m", "comment", "--comment", scopedIPTablesComment(scope, rule.StableName()), "-j", "DNAT", "--to-destination", rule.TargetAddress)...)
	}
	for _, rule := range nat.PortForwards {
		args := []string{"-t", "nat", "-A", "PREROUTING", "-i", rule.InInterface, "-p", rule.Protocol, "--dport", fmt.Sprint(rule.ListenPort)}
		if rule.ListenIP != "" {
			args = append(args, "-d", rule.ListenIP)
		}
		args = append(args, "-m", "comment", "--comment", scopedIPTablesComment(scope, rule.StableName()), "-j", "DNAT", "--to-destination", fmt.Sprintf("%s:%d", rule.TargetIP, rule.TargetPort))
		commands = append(commands, iptablesReplaceRule(ns, args...)...)
	}
	for _, rule := range nat.DestinationMaps {
		args := []string{"-t", "nat", "-A", "PREROUTING", "-i", rule.InInterface, "-d", rule.MatchAddress}
		if rule.FromCIDR != "" {
			args = append(args, "-s", rule.FromCIDR)
		}
		args = append(args, "-m", "comment", "--comment", scopedIPTablesComment(scope, rule.StableName()), "-j", "DNAT", "--to-destination", rule.TargetAddress)
		commands = append(commands, iptablesReplaceRule(ns, args...)...)
		if rule.SourceNAT != "" {
			commands = append(commands, iptablesReplaceRule(ns, "-t", "nat", "-A", "POSTROUTING", "-d", rule.TargetAddress, "-m", "comment", "--comment", scopedIPTablesComment(scope, rule.StableName()+"-snat"), "-j", "SNAT", "--to-source", rule.SourceNAT)...)
		}
	}
	return commands
}

func renderFirewallCommands(ns, scope, backend string, firewall Firewall) []Command {
	if backend == ovnflow.NATBackendIPTables {
		commands := []Command{iptablesCleanupOwnedRules(ns, scope, "filter")}
		for _, rule := range firewall.Rules {
			commands = append(commands, iptablesReplaceFirewallRule(ns, scope, rule)...)
		}
		return commands
	}
	table := nftFilterTable(scope)
	commands := []Command{
		{Program: "ip", Args: []string{"netns", "exec", ns, "nft", "delete", "table", "inet", table}, IgnoreNotFound: true},
	}
	if len(firewall.Rules) == 0 {
		return commands
	}
	commands = append(commands,
		Command{Program: "ip", Args: []string{"netns", "exec", ns, "nft", "add", "table", "inet", table}},
	)
	for _, chain := range firewallChains(firewall) {
		commands = append(commands, Command{Program: "ip", Args: []string{"netns", "exec", ns, "nft", "add", "chain", "inet", table, chain, "{", "type", "filter", "hook", chain, "priority", "filter", ";", "}"}})
	}
	for _, rule := range firewall.Rules {
		commands = append(commands, nftFirewallRule(ns, table, scope, rule))
	}
	return commands
}

func nftRule(ns, table string, args ...string) Command {
	return Command{Program: "ip", Args: append([]string{"netns", "exec", ns, "nft", "add", "rule", "ip", table}, args...)}
}

func iptablesRule(ns string, args ...string) Command {
	return Command{Program: "ip", Args: append([]string{"netns", "exec", ns, "iptables"}, args...)}
}

func iptablesReplaceRule(ns string, args ...string) []Command {
	deleteArgs := append([]string{}, args...)
	for i, arg := range deleteArgs {
		if arg == "-A" {
			deleteArgs[i] = "-D"
			break
		}
	}
	deleteRule := iptablesRule(ns, deleteArgs...)
	deleteRule.IgnoreNotFound = true
	return []Command{deleteRule, iptablesRule(ns, args...)}
}

func nftFirewallRule(ns, table, scope string, rule FirewallRule) Command {
	args := []string{"netns", "exec", ns, "nft", "add", "rule", "inet", table, firewallChain(rule)}
	args = append(args, nftFirewallMatches(rule)...)
	args = append(args, firewallVerdict(rule), "comment", scopedNftComment(scope, rule.Name))
	return Command{Program: "ip", Args: args}
}

func iptablesReplaceFirewallRule(ns, scope string, rule FirewallRule) []Command {
	args := iptablesFirewallArgs(scope, rule)
	deleteArgs := append([]string{}, args...)
	for i, arg := range deleteArgs {
		if arg == "-A" {
			deleteArgs[i] = "-D"
			break
		}
	}
	deleteRule := iptablesRule(ns, deleteArgs...)
	deleteRule.IgnoreNotFound = true
	return []Command{deleteRule, iptablesRule(ns, args...)}
}

func iptablesFirewallArgs(scope string, rule FirewallRule) []string {
	args := []string{"-A", strings.ToUpper(firewallChain(rule))}
	for _, cidr := range rule.CIDRs {
		args = append(args, "-s", cidr)
	}
	if rule.Protocol != "" {
		args = append(args, "-p", strings.ToLower(rule.Protocol))
	}
	for _, port := range rule.Ports {
		args = append(args, "--dport", fmt.Sprint(port))
	}
	return append(args, "-m", "comment", "--comment", scopedIPTablesComment(scope, rule.Name), "-j", strings.ToUpper(firewallVerdict(rule)))
}

func iptablesCleanupOwnedRules(ns, scope, table string) Command {
	script := `prefix="$1"; iptables-save` + iptablesSaveTableArg(table) + ` | awk -v prefix="$prefix" '` + iptablesOwnedRuleAWK(table) + `' | while IFS= read -r rule; do iptables ` + iptablesTableArg(table) + ` $rule || true; done`
	return Command{Program: "ip", Args: []string{"netns", "exec", ns, "sh", "-c", script, "ovnflow-iptables-cleanup", scopedRulePrefix(scope)}, IgnoreNotFound: true}
}

func iptablesSaveTableArg(table string) string {
	if table == "nat" {
		return " -t nat"
	}
	return ""
}

func iptablesTableArg(table string) string {
	if table == "nat" {
		return "-t nat"
	}
	return ""
}

func iptablesOwnedRuleAWK(table string) string {
	if table == "nat" {
		return `index($0, prefix) { sub(/^-A /,"-D "); print }`
	}
	return `BEGIN { in_filter=0 } /^\*filter$/ { in_filter=1; next } /^\*/ { in_filter=0 } /^COMMIT$/ { in_filter=0 } in_filter && index($0, prefix) { sub(/^-A /,"-D "); print }`
}

func linuxRouterOVSOwnershipGuardCommand(table, name, ns string) Command {
	script := `table="$1"; name="$2"; ns="$3"; ` +
		`if ovs-vsctl --data=bare --no-heading --columns=_uuid find "$table" name="$name" | grep -q .; then ` +
		`managed=$(ovs-vsctl --if-exists get "$table" "$name" external_ids:ovnflow.io/managed-by 2>/dev/null | tr -d '"'); ` +
		`kind=$(ovs-vsctl --if-exists get "$table" "$name" external_ids:ovnflow.io/kind 2>/dev/null | tr -d '"'); ` +
		`owner_ns=$(ovs-vsctl --if-exists get "$table" "$name" external_ids:ovnflow.io/linux-router-ns 2>/dev/null | tr -d '"'); ` +
		`if [ "$managed" != "ovnflow" ] || [ "$kind" != "LinuxRouter" ] || [ "$owner_ns" != "$ns" ]; then ` +
		`echo "ownership violation: $table $name is not managed by ovnflow LinuxRouter namespace $ns" >&2; exit 77; fi; fi`
	return Command{Program: "sh", Args: []string{"-c", script, "ovnflow-linuxrouter-guard", table, name, ns}}
}

func nftFirewallMatches(rule FirewallRule) []string {
	var args []string
	for _, cidr := range rule.CIDRs {
		args = append(args, "ip", "saddr", cidr)
	}
	if rule.Protocol != "" {
		args = append(args, strings.ToLower(rule.Protocol))
	}
	for _, port := range rule.Ports {
		args = append(args, "dport", fmt.Sprint(port))
	}
	return args
}

func firewallChain(rule FirewallRule) string {
	switch strings.ToLower(strings.TrimSpace(rule.Direction)) {
	case "ingress", "in":
		return "input"
	case "egress", "out":
		return "output"
	default:
		return "forward"
	}
}

func firewallChains(firewall Firewall) []string {
	seen := map[string]bool{}
	var out []string
	for _, rule := range firewall.Rules {
		chain := firewallChain(rule)
		if seen[chain] {
			continue
		}
		seen[chain] = true
		out = append(out, chain)
	}
	return out
}

func firewallVerdict(rule FirewallRule) string {
	switch strings.ToLower(strings.TrimSpace(rule.Action)) {
	case "", "allow":
		return "accept"
	case "reject":
		return "reject"
	default:
		return "drop"
	}
}

func nftNATTable(scope string) string {
	return "ovnflow_nat_" + scope
}

func nftFilterTable(scope string) string {
	return "ovnflow_filter_" + scope
}

func scopedNftComment(scope, name string) string {
	return strconv.Quote(scopedIPTablesComment(scope, name))
}

func scopedIPTablesComment(scope, name string) string {
	return scopedRulePrefix(scope) + name
}

func scopedRulePrefix(scope string) string {
	return "ovnflow:" + scope + ":"
}

func routerRuleScope(routerName, ns string) string {
	scope := sanitizeRuleScope(routerName)
	if scope == "" {
		scope = sanitizeRuleScope(ns)
	}
	if scope == "" {
		scope = "router"
	}
	hash := shortScopeHash(routerName + "\x00" + ns)
	maxPrefix := 48 - len(hash) - 1
	if len(scope) > maxPrefix {
		scope = strings.TrimRight(scope[:maxPrefix], "_")
	}
	if scope == "" {
		scope = "router"
	}
	return scope + "_" + hash
}

func sanitizeRuleScope(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		valid := r >= 'a' && r <= 'z' || r >= '0' && r <= '9'
		if valid {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash && b.Len() > 0 {
			b.WriteByte('_')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "_")
	return out
}

func shortScopeHash(value string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(value))
	return fmt.Sprintf("%08x", h.Sum32())
}

func commandNotFound(message string) bool {
	message = strings.ToLower(message)
	return strings.Contains(message, "no such file") ||
		strings.Contains(message, "not found") ||
		strings.Contains(message, "does not exist") ||
		strings.Contains(message, "cannot find device") ||
		strings.Contains(message, "no such table") ||
		strings.Contains(message, "bad rule") && strings.Contains(message, "matching rule")
}

func commandAlreadyExists(message string) bool {
	message = strings.ToLower(message)
	return strings.Contains(message, "file exists") ||
		strings.Contains(message, "already exists")
}

func errorKindForExec(err error) ovnflow.ErrorKind {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 77 {
		return ovnflow.ErrorOwnershipViolation
	}
	switch {
	case errors.Is(err, context.Canceled):
		return ovnflow.ErrorCanceled
	case errors.Is(err, context.DeadlineExceeded):
		return ovnflow.ErrorTimeout
	default:
		return ovnflow.ErrorUnavailable
	}
}

//go:build linux

package sdwanlinux

import (
	"context"
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/firstmeet/ovnflow/v2"
)

const (
	ExternalIDNetworkKey = ovnflow.ExternalIDPrefix + "sdwan-network"
	ExternalIDSiteKey    = ovnflow.ExternalIDPrefix + "sdwan-site"
	ExternalIDLinkKey    = ovnflow.ExternalIDPrefix + "sdwan-link"
)

type Command struct {
	Program             string
	Args                []string
	IgnoreNotFound      bool
	IgnoreAlreadyExists bool
}

type Executor interface {
	Run(context.Context, Command) error
}

type OVSManager interface {
	EnsureTunnel(context.Context, OVSTunnel) error
	DeleteTunnel(context.Context, OVSTunnel) error
}

type OpenFlowManager interface {
	EnsureRule(context.Context, OpenFlowRule) error
	DeleteRule(context.Context, OpenFlowRule) error
}

type Backend struct {
	mu          sync.RWMutex
	localSite   string
	executor    Executor
	ovs         OVSManager
	openflow    OpenFlowManager
	ifacePrefix string
	routeTable  int
	networks    map[string]ovnflow.SDWANNetwork
	plans       map[string]ovnflow.SDWANApplyPlan
}

type Config struct {
	LocalSite       string
	Executor        Executor
	OVS             OVSManager
	OpenFlow        OpenFlowManager
	InterfacePrefix string
	RouteTable      int
}

type FakeExecutor struct {
	mu       sync.Mutex
	commands []Command
}

func (f *FakeExecutor) Run(_ context.Context, cmd Command) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.commands = append(f.commands, cloneCommand(cmd))
	return nil
}

func (f *FakeExecutor) Snapshot() []Command {
	f.mu.Lock()
	defer f.mu.Unlock()
	return cloneCommands(f.commands)
}

type OVSTunnel struct {
	Network    string
	Link       string
	LocalSite  string
	RemoteSite string
	Bridge     string
	Port       string
	Type       string
	RemoteIP   string
	Key        string
	DstPort    string
	ExternalID map[string]string
}

type OpenFlowRule struct {
	Network   string
	Link      string
	Bridge    string
	RuleName  string
	TableID   uint8
	Priority  uint16
	Cookie    uint64
	Match     ovnflow.OpenFlowMatch
	Actions   []ovnflow.OpenFlowAction
	Transport ovnflow.SDWANTransport
}

func NewBackend(cfg Config) (*Backend, error) {
	if err := ovnflow.Labels(map[string]string{"local-site": cfg.LocalSite}).Validate(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.LocalSite) == "" {
		return nil, &ovnflow.Error{Kind: ovnflow.ErrorValidation, Operation: "create", Object: "sdwanlinux", Message: "local site is required"}
	}
	executor := cfg.Executor
	if executor == nil {
		executor = SystemExecutor{}
	}
	ifacePrefix := strings.TrimSpace(cfg.InterfacePrefix)
	if ifacePrefix == "" {
		ifacePrefix = "ofwan"
	}
	routeTable := cfg.RouteTable
	if routeTable == 0 {
		routeTable = 51820
	}
	return &Backend{
		localSite:   cfg.LocalSite,
		executor:    executor,
		ovs:         cfg.OVS,
		openflow:    cfg.OpenFlow,
		ifacePrefix: ifacePrefix,
		routeTable:  routeTable,
		networks:    map[string]ovnflow.SDWANNetwork{},
		plans:       map[string]ovnflow.SDWANApplyPlan{},
	}, nil
}

func MustNewBackend(cfg Config) *Backend {
	backend, err := NewBackend(cfg)
	if err != nil {
		panic(err)
	}
	return backend
}

func (b *Backend) GetSDWAN(_ context.Context, name string) (*ovnflow.SDWANNetwork, error) {
	if b == nil {
		return nil, ovnflow.ErrBackendUnavailable
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	network, ok := b.networks[name]
	if !ok {
		return nil, &ovnflow.Error{Kind: ovnflow.ErrorNotFound, Operation: "get", Object: name, Message: "SD-WAN network not found"}
	}
	out := cloneNetwork(network)
	return &out, nil
}

func (b *Backend) ApplySDWAN(ctx context.Context, network ovnflow.SDWANNetwork, plan ovnflow.SDWANApplyPlan) error {
	if b == nil || b.executor == nil {
		return ovnflow.ErrBackendUnavailable
	}
	if err := network.Validate(); err != nil {
		return err
	}
	network = normalizeNetwork(network)
	site, ok := findSite(network.Sites, b.localSite)
	if !ok {
		return &ovnflow.Error{Kind: ovnflow.ErrorValidation, Operation: "apply", Object: b.localSite, Message: "local site is not part of SD-WAN network"}
	}
	for _, cmd := range b.renderSiteCommands(network, site) {
		if err := b.executor.Run(ctx, cmd); err != nil {
			return err
		}
	}
	for _, link := range network.Links {
		if !linkTouchesSite(link, b.localSite) {
			continue
		}
		remote, ok := findSite(network.Sites, remoteSiteName(link, b.localSite))
		if !ok {
			return &ovnflow.Error{Kind: ovnflow.ErrorValidation, Operation: "apply", Object: link.StableName(), Message: "remote site not found"}
		}
		if link.Disabled || !link.Enabled {
			if err := b.deleteLink(ctx, network, link); err != nil {
				return err
			}
			continue
		}
		switch link.Transport {
		case ovnflow.SDWANTransportWireGuard:
			for _, cmd := range b.renderWireGuardCommands(network, site, remote, link) {
				if err := b.executor.Run(ctx, cmd); err != nil {
					return err
				}
			}
		case ovnflow.SDWANTransportGeneve, ovnflow.SDWANTransportVXLAN:
			if err := b.ensureOVSTunnel(ctx, network, site, remote, link); err != nil {
				return err
			}
			if network.Layer == ovnflow.SDWANLayerL2 {
				if err := b.ensureOpenFlowRule(ctx, network, link); err != nil {
					return err
				}
			}
		}
		if network.Layer == ovnflow.SDWANLayerL3 {
			for _, cmd := range b.renderRouteCommands(network, remote, link) {
				if err := b.executor.Run(ctx, cmd); err != nil {
					return err
				}
			}
		}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.networks == nil {
		b.networks = map[string]ovnflow.SDWANNetwork{}
	}
	if b.plans == nil {
		b.plans = map[string]ovnflow.SDWANApplyPlan{}
	}
	status := network.Status
	if current, ok := b.networks[network.Name]; ok {
		status = current.Status
	}
	status.State = ovnflow.ResourceStatusPresent
	status.LastApplied++
	network.Status = status
	b.networks[network.Name] = cloneNetwork(network)
	b.plans[network.Name] = clonePlan(plan)
	return nil
}

func (b *Backend) deleteLink(ctx context.Context, network ovnflow.SDWANNetwork, link ovnflow.SDWANLink) error {
	switch link.Transport {
	case ovnflow.SDWANTransportWireGuard:
		if network.Layer == ovnflow.SDWANLayerL3 {
			for _, cmd := range b.renderRouteDeleteCommands(network, remoteSite(network, link, b.localSite), link) {
				if err := b.executor.Run(ctx, cmd); err != nil {
					return err
				}
			}
		}
		return b.executor.Run(ctx, Command{Program: "ip", Args: []string{"link", "delete", b.wireGuardInterface(network, link)}, IgnoreNotFound: true})
	case ovnflow.SDWANTransportGeneve, ovnflow.SDWANTransportVXLAN:
		if b.openflow != nil {
			if err := b.openflow.DeleteRule(ctx, b.openFlowRule(network, link)); err != nil {
				return err
			}
		}
		if b.ovs != nil {
			if err := b.ovs.DeleteTunnel(ctx, b.ovsTunnel(network, localSite(network, b.localSite), remoteSite(network, link, b.localSite), link)); err != nil {
				return err
			}
		}
	}
	return nil
}

func (b *Backend) DeleteSDWAN(ctx context.Context, name string) error {
	if b == nil {
		return ovnflow.ErrBackendUnavailable
	}
	b.mu.RLock()
	network, ok := b.networks[name]
	b.mu.RUnlock()
	if !ok {
		return &ovnflow.Error{Kind: ovnflow.ErrorNotFound, Operation: "delete", Object: name, Message: "SD-WAN network not found"}
	}
	for _, link := range network.Links {
		if !linkTouchesSite(link, b.localSite) {
			continue
		}
		if err := b.deleteLink(ctx, network, link); err != nil {
			return err
		}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.networks, name)
	delete(b.plans, name)
	return nil
}

func (b *Backend) LastPlan(name string) (ovnflow.SDWANApplyPlan, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	plan, ok := b.plans[name]
	return clonePlan(plan), ok
}

func (b *Backend) renderSiteCommands(network ovnflow.SDWANNetwork, site ovnflow.SDWANSite) []Command {
	return nil
}

func (b *Backend) renderWireGuardCommands(network ovnflow.SDWANNetwork, local, remote ovnflow.SDWANSite, link ovnflow.SDWANLink) []Command {
	iface := b.wireGuardInterface(network, link)
	privateKeyFile := firstNonEmpty(local.Attributes["wireguard_private_key_file"], local.Attributes["private_key_file"])
	listenPort := firstNonEmpty(local.Attributes["wireguard_listen_port"], link.Attributes["listen_port"])
	commands := []Command{
		{Program: "ip", Args: []string{"link", "add", iface, "type", "wireguard"}, IgnoreAlreadyExists: true},
	}
	if privateKeyFile != "" {
		args := []string{"set", iface, "private-key", privateKeyFile}
		if listenPort != "" {
			args = append(args, "listen-port", listenPort)
		}
		commands = append(commands, Command{Program: "wg", Args: args})
	}
	peerArgs := []string{"set", iface, "peer", remote.PublicKey}
	if psk := remote.Attributes["wireguard_preshared_key_file"]; psk != "" {
		peerArgs = append(peerArgs, "preshared-key", psk)
	}
	endpoint := endpointForRemote(link, b.localSite)
	if endpoint == "" {
		endpoint = remote.Endpoint
	}
	if endpoint != "" {
		peerArgs = append(peerArgs, "endpoint", endpoint)
	}
	allowedIPs := allowedIPsForRemote(remote, link)
	if len(allowedIPs) > 0 {
		peerArgs = append(peerArgs, "allowed-ips", strings.Join(allowedIPs, ","))
	}
	if remote.PublicKey != "" {
		commands = append(commands, Command{Program: "wg", Args: peerArgs})
	}
	if address := firstNonEmpty(local.Attributes["wireguard_address"], local.Attributes["wg_address"]); address != "" {
		commands = append(commands, Command{Program: "ip", Args: []string{"addr", "replace", address, "dev", iface}})
	}
	commands = append(commands, Command{Program: "ip", Args: []string{"link", "set", iface, "up"}})
	return commands
}

func (b *Backend) renderRouteCommands(network ovnflow.SDWANNetwork, remote ovnflow.SDWANSite, link ovnflow.SDWANLink) []Command {
	iface := b.tunnelInterface(network, link)
	var commands []Command
	for _, cidr := range allowedIPsForRemote(remote, link) {
		priority := strconv.Itoa(routeRulePriority(network, link, cidr))
		commands = append(commands, Command{Program: "ip", Args: []string{"route", "replace", cidr, "dev", iface, "table", strconv.Itoa(b.routeTable)}})
		commands = append(commands, Command{Program: "ip", Args: []string{"rule", "del", "priority", priority}, IgnoreNotFound: true})
		commands = append(commands, Command{Program: "ip", Args: []string{"rule", "add", "priority", priority, "to", cidr, "lookup", strconv.Itoa(b.routeTable)}, IgnoreAlreadyExists: true})
	}
	return commands
}

func (b *Backend) renderRouteDeleteCommands(network ovnflow.SDWANNetwork, remote ovnflow.SDWANSite, link ovnflow.SDWANLink) []Command {
	iface := b.tunnelInterface(network, link)
	var commands []Command
	for _, cidr := range allowedIPsForRemote(remote, link) {
		priority := strconv.Itoa(routeRulePriority(network, link, cidr))
		commands = append(commands, Command{Program: "ip", Args: []string{"rule", "del", "priority", priority}, IgnoreNotFound: true})
		commands = append(commands, Command{Program: "ip", Args: []string{"rule", "del", "to", cidr, "lookup", strconv.Itoa(b.routeTable)}, IgnoreNotFound: true})
		commands = append(commands, Command{Program: "ip", Args: []string{"route", "del", cidr, "dev", iface, "table", strconv.Itoa(b.routeTable)}, IgnoreNotFound: true})
	}
	return commands
}

func (b *Backend) ensureOVSTunnel(ctx context.Context, network ovnflow.SDWANNetwork, local, remote ovnflow.SDWANSite, link ovnflow.SDWANLink) error {
	if b.ovs == nil {
		return ovnflow.ErrBackendUnavailable
	}
	return b.ovs.EnsureTunnel(ctx, b.ovsTunnel(network, local, remote, link))
}

func (b *Backend) ensureOpenFlowRule(ctx context.Context, network ovnflow.SDWANNetwork, link ovnflow.SDWANLink) error {
	if b.openflow == nil {
		return nil
	}
	rule := b.openFlowRule(network, link)
	if !openFlowMatchConfigured(rule.Match) {
		return &ovnflow.Error{Kind: ovnflow.ErrorValidation, Operation: "ensure", Object: rule.RuleName, Message: "SD-WAN OpenFlow rule requires an explicit match attribute"}
	}
	return b.openflow.EnsureRule(ctx, rule)
}

func (b *Backend) ovsTunnel(network ovnflow.SDWANNetwork, local, remote ovnflow.SDWANSite, link ovnflow.SDWANLink) OVSTunnel {
	attrs := cloneMap(link.Attributes)
	bridge := firstNonEmpty(attrs["bridge"], local.Attributes["ovs_bridge"], network.Labels["ovs_bridge"])
	if bridge == "" {
		bridge = "br-int"
	}
	port := firstNonEmpty(attrs["port"], b.tunnelInterface(network, link))
	remoteIP := firstNonEmpty(attrs["remote_ip"], remote.Attributes["tunnel_ip"], remote.Endpoint)
	return OVSTunnel{
		Network:    network.Name,
		Link:       link.StableName(),
		LocalSite:  local.Name,
		RemoteSite: remote.Name,
		Bridge:     bridge,
		Port:       port,
		Type:       string(link.Transport),
		RemoteIP:   remoteIP,
		Key:        attrs["key"],
		DstPort:    attrs["dst_port"],
		ExternalID: ownedExternalIDs(network.Name, local.Name, link.StableName()),
	}
}

func (b *Backend) openFlowRule(network ovnflow.SDWANNetwork, link ovnflow.SDWANLink) OpenFlowRule {
	attrs := link.Attributes
	bridge := firstNonEmpty(attrs["bridge"], network.Labels["ovs_bridge"], "br-int")
	table := uint8(0)
	if raw := attrs["openflow_table"]; raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed >= 0 && parsed <= 255 {
			table = uint8(parsed)
		}
	}
	priority := uint16(100)
	if raw := attrs["openflow_priority"]; raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed >= 0 && parsed <= 65535 {
			priority = uint16(parsed)
		}
	}
	outPort := uint32(0xfffffffb)
	if raw := attrs["output_port"]; raw != "" {
		if parsed, err := strconv.ParseUint(raw, 10, 32); err == nil {
			outPort = uint32(parsed)
		}
	}
	return OpenFlowRule{
		Network:   network.Name,
		Link:      link.StableName(),
		Bridge:    bridge,
		RuleName:  "sdwan-" + network.Name + "-" + link.StableName(),
		TableID:   table,
		Priority:  priority,
		Cookie:    sdwanOpenFlowCookie(network, link),
		Match:     openFlowMatchFromAttributes(attrs),
		Transport: link.Transport,
		Actions:   []ovnflow.OpenFlowAction{{Type: ovnflow.OpenFlowActionOutput, Port: outPort}},
	}
}

func (b *Backend) wireGuardInterface(network ovnflow.SDWANNetwork, link ovnflow.SDWANLink) string {
	return interfaceName(b.ifacePrefix, "wg", network.Name, link.StableName())
}

func (b *Backend) tunnelInterface(network ovnflow.SDWANNetwork, link ovnflow.SDWANLink) string {
	switch link.Transport {
	case ovnflow.SDWANTransportWireGuard:
		return b.wireGuardInterface(network, link)
	default:
		return interfaceName(b.ifacePrefix, "tn", network.Name, link.StableName())
	}
}

func (b *Backend) siteInterface(network ovnflow.SDWANNetwork, site ovnflow.SDWANSite) string {
	return interfaceName(b.ifacePrefix, "st", network.Name, site.Name)
}

type sdkOVSManager struct {
	ovs *ovnflow.OVSClient
}

func NewOVSManager(ovs *ovnflow.OVSClient) OVSManager {
	return sdkOVSManager{ovs: ovs}
}

func (m sdkOVSManager) EnsureTunnel(ctx context.Context, tunnel OVSTunnel) error {
	if m.ovs == nil {
		return ovnflow.ErrBackendUnavailable
	}
	if existing, err := m.ovs.GetPort(ctx, tunnel.Port); err == nil {
		if !ovsTunnelOwnedBy(existing.ExternalIDs, tunnel) {
			return &ovnflow.Error{Kind: ovnflow.ErrorOwnershipViolation, Operation: "ensure", Object: tunnel.Port, Message: "OVS tunnel port already exists and is not owned by this SD-WAN link"}
		}
	} else if !ovnflow.IsKind(err, ovnflow.ErrorNotFound) {
		return err
	}
	port := m.ovs.Bridge(tunnel.Bridge).Ensure().
		WithExternalID(ovnflow.ExternalIDManagedByKey, "ovnflow").
		AddPort(tunnel.Port).
		WithInterfaceType(tunnel.Type).
		WithInterfaceOption("remote_ip", tunnel.RemoteIP)
	if tunnel.Key != "" {
		port.WithInterfaceOption("key", tunnel.Key)
	}
	if tunnel.DstPort != "" {
		port.WithInterfaceOption("dst_port", tunnel.DstPort)
	}
	for _, key := range sortedKeys(tunnel.ExternalID) {
		port.WithExternalID(key, tunnel.ExternalID[key])
		port.WithInterfaceExternalID(key, tunnel.ExternalID[key])
	}
	return port.Execute(ctx)
}

func (m sdkOVSManager) DeleteTunnel(ctx context.Context, tunnel OVSTunnel) error {
	if m.ovs == nil {
		return ovnflow.ErrBackendUnavailable
	}
	port, err := m.ovs.GetPort(ctx, tunnel.Port)
	if err != nil {
		if ovnflow.IsKind(err, ovnflow.ErrorNotFound) {
			return nil
		}
		return err
	}
	if !ovsTunnelOwnedBy(port.ExternalIDs, tunnel) {
		return &ovnflow.Error{Kind: ovnflow.ErrorOwnershipViolation, Operation: "delete", Object: tunnel.Port, Message: "OVS tunnel port is not owned by this SD-WAN link"}
	}
	return m.ovs.Bridge(tunnel.Bridge).DeletePort(tunnel.Port).Execute(ctx)
}

func ovsTunnelOwnedBy(externalIDs map[string]string, tunnel OVSTunnel) bool {
	return externalIDs[ovnflow.ExternalIDManagedByKey] == "ovnflow" &&
		externalIDs[ovnflow.ExternalIDKindKey] == "SDWAN" &&
		externalIDs[ExternalIDNetworkKey] == tunnel.Network &&
		externalIDs[ExternalIDSiteKey] == tunnel.LocalSite &&
		externalIDs[ExternalIDLinkKey] == tunnel.Link
}

type sdkOpenFlowManager struct {
	client *ovnflow.OpenFlowClient
}

func NewOpenFlowManager(client *ovnflow.OpenFlowClient) OpenFlowManager {
	return sdkOpenFlowManager{client: client}
}

func (m sdkOpenFlowManager) EnsureRule(ctx context.Context, rule OpenFlowRule) error {
	if m.client == nil {
		return ovnflow.ErrBackendUnavailable
	}
	return retryOpenFlow(ctx, func() error {
		builder := m.client.Bridge(rule.Bridge).EnsureFlow(rule.RuleName).Table(rule.TableID).Priority(rule.Priority)
		if rule.Cookie != 0 {
			builder.Cookie(rule.Cookie)
		}
		applyOpenFlowMatch(builder, rule.Match)
		for _, action := range rule.Actions {
			if action.Type == ovnflow.OpenFlowActionOutput {
				builder.Actions().Output(action.Port)
			}
		}
		return builder.Execute(ctx)
	})
}

func (m sdkOpenFlowManager) DeleteRule(ctx context.Context, rule OpenFlowRule) error {
	if m.client == nil {
		return ovnflow.ErrBackendUnavailable
	}
	return retryOpenFlow(ctx, func() error {
		builder := m.client.Bridge(rule.Bridge).DeleteFlow(rule.RuleName)
		if rule.Cookie != 0 {
			builder.Cookie(rule.Cookie).CookieMask(^uint64(0))
		}
		return builder.Execute(ctx)
	})
}

func retryOpenFlow(ctx context.Context, fn func() error) error {
	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		if err := fn(); err != nil {
			lastErr = err
			if !ovnflow.IsKind(err, ovnflow.ErrorUnavailable) && !ovnflow.IsKind(err, ovnflow.ErrorTimeout) {
				return err
			}
		} else {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(attempt+1) * 100 * time.Millisecond):
		}
	}
	return lastErr
}

func normalizeNetwork(network ovnflow.SDWANNetwork) ovnflow.SDWANNetwork {
	backend := ovnflow.NewInMemorySDWANBackend()
	_ = backend.ApplySDWAN(context.Background(), network, ovnflow.SDWANApplyPlan{})
	out, err := backend.GetSDWAN(context.Background(), network.Name)
	if err != nil {
		return network
	}
	return *out
}

func findSite(sites []ovnflow.SDWANSite, name string) (ovnflow.SDWANSite, bool) {
	for _, site := range sites {
		if site.Name == name {
			return site, true
		}
	}
	return ovnflow.SDWANSite{}, false
}

func localSite(network ovnflow.SDWANNetwork, name string) ovnflow.SDWANSite {
	site, _ := findSite(network.Sites, name)
	return site
}

func remoteSite(network ovnflow.SDWANNetwork, link ovnflow.SDWANLink, local string) ovnflow.SDWANSite {
	site, _ := findSite(network.Sites, remoteSiteName(link, local))
	return site
}

func linkTouchesSite(link ovnflow.SDWANLink, site string) bool {
	return link.From == site || link.To == site
}

func remoteSiteName(link ovnflow.SDWANLink, local string) string {
	if link.From == local {
		return link.To
	}
	return link.From
}

func allowedIPsForRemote(remote ovnflow.SDWANSite, link ovnflow.SDWANLink) []string {
	if len(link.AllowedIPs) > 0 {
		return uniqueSorted(link.AllowedIPs)
	}
	return uniqueSorted(remote.CIDRs)
}

func endpointForRemote(link ovnflow.SDWANLink, local string) string {
	if link.From == local {
		return link.EndpointB
	}
	return link.EndpointA
}

func ownedExternalIDs(network, site, link string) map[string]string {
	return map[string]string{
		ovnflow.ExternalIDManagedByKey:  "ovnflow",
		ovnflow.ExternalIDAPIVersionKey: "v2",
		ovnflow.ExternalIDKindKey:       "SDWAN",
		ovnflow.ExternalIDNameKey:       network,
		ExternalIDNetworkKey:            network,
		ExternalIDSiteKey:               site,
		ExternalIDLinkKey:               link,
	}
}

func cloneNetwork(in ovnflow.SDWANNetwork) ovnflow.SDWANNetwork {
	backend := ovnflow.NewInMemorySDWANBackend()
	_ = backend.ApplySDWAN(context.Background(), in, ovnflow.SDWANApplyPlan{})
	out, err := backend.GetSDWAN(context.Background(), in.Name)
	if err != nil {
		return in
	}
	out.Status = in.Status
	return *out
}

func clonePlan(in ovnflow.SDWANApplyPlan) ovnflow.SDWANApplyPlan {
	return ovnflow.SDWANApplyPlan{Network: in.Network, Operations: append([]ovnflow.SDWANOperation{}, in.Operations...)}
}

func cloneCommand(in Command) Command {
	out := in
	out.Args = append([]string{}, in.Args...)
	return out
}

func cloneCommands(in []Command) []Command {
	out := append([]Command{}, in...)
	for i := range out {
		out[i].Args = append([]string{}, in[i].Args...)
	}
	return out
}

func cloneMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := map[string]string{}
	for key, value := range in {
		out[key] = value
	}
	return out
}

func uniqueSorted(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func sortedKeys(values map[string]string) []string {
	var keys []string
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func interfaceName(prefix, role, network, name string) string {
	hash := shortHash(network + "\x00" + name)
	base := shortName(network + name)
	maxBase := 15 - len(prefix) - len(role) - 1 - len(hash)
	if maxBase < 0 {
		maxBase = 0
	}
	if len(base) > maxBase {
		base = base[:maxBase]
	}
	return clampName(prefix+role+base+"-"+hash, 15)
}

func shortName(value string) string {
	value = strings.ToLower(value)
	var b strings.Builder
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	out := b.String()
	if out == "" {
		out = fmt.Sprintf("%x", len(value))
	}
	return out
}

func shortHash(value string) string {
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(value))
	return strconv.FormatUint(uint64(hash.Sum32()), 36)
}

func routeRulePriority(network ovnflow.SDWANNetwork, link ovnflow.SDWANLink, cidr string) int {
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(network.Name))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write([]byte(link.StableName()))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write([]byte(cidr))
	return 20000 + int(binary.BigEndian.Uint32(hash.Sum(nil))%10000)
}

func sdwanOpenFlowCookie(network ovnflow.SDWANNetwork, link ovnflow.SDWANLink) uint64 {
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(network.Name))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write([]byte(link.StableName()))
	return 0x0f0f000000000000 | (hash.Sum64() & 0x0000ffffffffffff)
}

func openFlowMatchFromAttributes(attrs map[string]string) ovnflow.OpenFlowMatch {
	match := ovnflow.OpenFlowMatch{}
	if raw := attrs["in_port"]; raw != "" {
		if parsed, err := strconv.ParseUint(raw, 10, 32); err == nil {
			value := uint32(parsed)
			match.InPort = &value
		}
	}
	if raw := attrs["eth_type"]; raw != "" {
		if parsed, err := strconv.ParseUint(raw, 0, 16); err == nil {
			value := uint16(parsed)
			match.EthType = &value
		}
	}
	match.IPv4Src = attrs["ipv4_src"]
	match.IPv4Dst = attrs["ipv4_dst"]
	if raw := attrs["ip_proto"]; raw != "" {
		if parsed, err := strconv.ParseUint(raw, 10, 8); err == nil {
			value := uint8(parsed)
			match.IPProto = &value
		}
	}
	if raw := attrs["tcp_src"]; raw != "" {
		if parsed, err := strconv.ParseUint(raw, 10, 16); err == nil {
			value := uint16(parsed)
			match.TCPSrc = &value
		}
	}
	if raw := attrs["tcp_dst"]; raw != "" {
		if parsed, err := strconv.ParseUint(raw, 10, 16); err == nil {
			value := uint16(parsed)
			match.TCPDst = &value
		}
	}
	if raw := attrs["udp_src"]; raw != "" {
		if parsed, err := strconv.ParseUint(raw, 10, 16); err == nil {
			value := uint16(parsed)
			match.UDPSrc = &value
		}
	}
	if raw := attrs["udp_dst"]; raw != "" {
		if parsed, err := strconv.ParseUint(raw, 10, 16); err == nil {
			value := uint16(parsed)
			match.UDPDst = &value
		}
	}
	return match
}

func openFlowMatchConfigured(match ovnflow.OpenFlowMatch) bool {
	return match.InPort != nil ||
		match.Metadata != nil ||
		match.EthSrc != "" ||
		match.EthDst != "" ||
		match.EthType != nil ||
		match.VLANVID != nil ||
		match.IPProto != nil ||
		match.IPv4Src != "" ||
		match.IPv4Dst != "" ||
		match.TCPSrc != nil ||
		match.TCPDst != nil ||
		match.UDPSrc != nil ||
		match.UDPDst != nil
}

func applyOpenFlowMatch(builder *ovnflow.OpenFlowRuleBuilder, match ovnflow.OpenFlowMatch) {
	if match.InPort != nil {
		builder.InPort(*match.InPort)
	}
	if match.EthType != nil {
		builder.EthType(*match.EthType)
	}
	if match.IPv4Src != "" {
		builder.IPv4Src(match.IPv4Src)
	}
	if match.IPv4Dst != "" {
		builder.IPv4Dst(match.IPv4Dst)
	}
	if match.IPProto != nil {
		builder.IPProto(*match.IPProto)
	}
	if match.TCPSrc != nil {
		builder.TCPSrc(*match.TCPSrc)
	}
	if match.TCPDst != nil {
		builder.TCPDst(*match.TCPDst)
	}
	if match.UDPSrc != nil {
		builder.UDPSrc(*match.UDPSrc)
	}
	if match.UDPDst != nil {
		builder.UDPDst(*match.UDPDst)
	}
}

func clampName(value string, max int) string {
	if max <= 0 || len(value) <= max {
		return value
	}
	return value[:max]
}

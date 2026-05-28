package sdkcheck

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/firstmeet/ovnflow/v2"
)

const (
	DefaultPrefix = "ovnflow-sdkcheck-"
	DefaultBridge = "br-ovnflow-sdkcheck"
)

type Options struct {
	OVSAddr   string
	OVNNBAddr string
	OVNSBAddr string
	Prefix    string
	Bridge    string
	Timeout   time.Duration
}

type Report struct {
	Steps []Step
}

type Step struct {
	Name     string
	Duration time.Duration
	Err      error
}

func OptionsFromEnv() Options {
	return Options{
		OVSAddr:   strings.TrimSpace(os.Getenv("OVNFLOW_OVS_ADDR")),
		OVNNBAddr: strings.TrimSpace(os.Getenv("OVNFLOW_OVN_NB_ADDR")),
		OVNSBAddr: strings.TrimSpace(os.Getenv("OVNFLOW_OVN_SB_ADDR")),
		Prefix:    envOrDefault("OVNFLOW_SDKCHECK_PREFIX", DefaultPrefix),
		Bridge:    envOrDefault("OVNFLOW_SDKCHECK_BRIDGE", DefaultBridge),
		Timeout:   45 * time.Second,
	}
}

func Run(ctx context.Context, opts Options) Report {
	if opts.Prefix == "" {
		opts.Prefix = DefaultPrefix
	}
	if opts.Bridge == "" {
		opts.Bridge = DefaultBridge
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 45 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	r := runner{opts: opts}
	var client *ovnflow.Client
	r.step("config/connect", func(ctx context.Context) error {
		if opts.OVSAddr == "" || opts.OVNNBAddr == "" || opts.OVNSBAddr == "" {
			return fmt.Errorf("missing endpoints: set OVNFLOW_OVS_ADDR, OVNFLOW_OVN_NB_ADDR, OVNFLOW_OVN_SB_ADDR")
		}
		t := withEnv("OVNFLOW_OVS_ADDR", opts.OVSAddr, func() ovnflow.Config { return ovnflow.ConfigFromEnv() })
		if t.OVSAddr != opts.OVSAddr {
			return fmt.Errorf("ConfigFromEnv OVSAddr = %q, want %q", t.OVSAddr, opts.OVSAddr)
		}
		cfg := ovnflow.Config{OVSAddr: opts.OVSAddr, OVNNBAddr: opts.OVNNBAddr, OVNSBAddr: opts.OVNSBAddr}
		var err error
		client, err = ovnflow.Connect(ctx, cfg)
		if err != nil {
			return err
		}
		_ = client.OVN().NB()
		_ = client.OVN().SB()
		_ = client.LocalOVS()
		_ = client.OpenFlow()
		_ = client.SDWAN()
		return nil
	})
	if client == nil {
		return r.report
	}
	defer client.Close()

	r.step("errors", func(ctx context.Context) error {
		err := context.DeadlineExceeded
		if got := ovnflow.KindOf(err); got != ovnflow.ErrorTimeout {
			return fmt.Errorf("KindOf(deadline) = %q, want %q", got, ovnflow.ErrorTimeout)
		}
		if !ovnflow.IsKind(context.Canceled, ovnflow.ErrorCanceled) {
			return errors.New("IsKind(context.Canceled, ErrorCanceled) = false")
		}
		return nil
	})
	r.step("northbound typed lifecycle", func(ctx context.Context) error { return checkNorthbound(ctx, client, opts) })
	r.step("southbound typed/runtime surface", func(ctx context.Context) error { return checkSouthbound(ctx, client, opts) })
	r.step("openvswitch typed lifecycle", func(ctx context.Context) error { return checkOVS(ctx, client, opts) })
	r.step("runtime fluent api", func(ctx context.Context) error { return checkRuntime(ctx, client, opts) })
	r.step("openflow and sdwan api surface", func(ctx context.Context) error { return checkOpenFlowAndSDWAN(ctx, client, opts) })
	return r.report
}

func (r Report) Err() error {
	var failed []string
	for _, step := range r.Steps {
		if step.Err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", step.Name, step.Err))
		}
	}
	if len(failed) == 0 {
		return nil
	}
	return errors.New(strings.Join(failed, "\n"))
}

func (r Report) SortedSteps() []Step {
	out := append([]Step{}, r.Steps...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

type runner struct {
	opts   Options
	report Report
}

func (r *runner) step(name string, fn func(context.Context) error) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), r.opts.Timeout)
	defer cancel()
	r.report.Steps = append(r.report.Steps, Step{Name: name, Duration: time.Since(start), Err: fn(ctx)})
}

func checkNorthbound(ctx context.Context, client *ovnflow.Client, opts Options) error {
	nb := client.OVN().NB()
	p := opts.Prefix + "nb-"
	ls := p + "ls"
	lsp := p + "lsp"
	lr := p + "lr"
	lrp := p + "lrp"
	aclMatch := "outport == \"" + p + "vm\""
	natIP := "10.220.0.0/24"
	lb := p + "lb"
	dhcp := "10.220.0.0/24"
	dns := p + "dns"
	qosMatch := "ip4.src == 10.220.0.10"
	meter := p + "meter"
	meterBand := p + "meter-band"
	pg := p + "pg"
	addrSet := p + "as"
	gw := p + "gw"
	ha := p + "ha"
	hag := p + "hag"
	bfdIP := "10.220.0.2"

	cleanupNB(ctx, nb, ls, lsp, lr, lrp, aclMatch, natIP, lb, dhcp, dns, qosMatch, meter, meterBand, pg, addrSet, gw, ha, hag, bfdIP)
	defer cleanupNB(context.Background(), nb, ls, lsp, lr, lrp, aclMatch, natIP, lb, dhcp, dns, qosMatch, meter, meterBand, pg, addrSet, gw, ha, hag, bfdIP)

	if err := nb.LogicalSwitch(ls).Ensure().WithSubnet("10.220.0.0/24").WithExternalID("sdkcheck", "true").AddPort(lsp).WithMac("00:11:22:33:44:66").WithIP("10.220.0.10").WithExternalID("sdkcheck", "true").Execute(ctx); err != nil {
		return fmt.Errorf("logical switch/port ensure: %w", err)
	}
	if err := nb.LogicalSwitch(ls).Ensure().WithSubnet("10.220.0.0/24").Execute(ctx); err != nil {
		return fmt.Errorf("logical switch repeat ensure: %w", err)
	}
	if got, err := nb.GetLogicalSwitch(ctx, ls); err != nil || got.Name != ls {
		return fmt.Errorf("get logical switch = %#v, %w", got, err)
	}
	if got, err := nb.GetLogicalSwitchPort(ctx, lsp); err != nil || got.Name != lsp {
		return fmt.Errorf("get logical switch port = %#v, %w", got, err)
	}
	if rows, err := nb.ListLogicalSwitches(ctx); err != nil || !hasLS(rows, ls) {
		return fmt.Errorf("list logical switches found=%t err=%w", hasLS(rows, ls), err)
	}
	if _, err := nb.ListLogicalSwitchPorts(ctx); err != nil {
		return fmt.Errorf("list logical switch ports: %w", err)
	}

	if err := nb.LogicalRouter(lr).Ensure().WithOption("requested-tnl-key", "220").WithExternalID("sdkcheck", "true").Execute(ctx); err != nil {
		return fmt.Errorf("logical router ensure: %w", err)
	}
	if err := nb.LogicalRouterPort(lrp).Ensure().
		AttachToRouter(lr).
		WithMAC("00:00:5e:00:53:22").
		WithNetwork("10.220.0.1/24").
		WithGatewayChassis(gw, "gw-"+p, 20).
		WithGatewayChassisExternalID("sdkcheck", "true").
		WithGatewayChassisOption("role", "test").
		WithHAChassisGroup(hag).
		WithHAChassis(ha, 30).
		WithHAChassisExternalID("sdkcheck", "true").
		WithHAChassisGroupExternalID("sdkcheck", "true").
		WithPeer("peer-"+lrp).
		WithEnabled(true).
		WithIPv6Prefix("2001:db8:220::/64").
		WithIPv6RAConfig("send_periodic", "true").
		WithOption("gateway_mtu", "1400").
		WithExternalID("sdkcheck", "true").
		Execute(ctx); err != nil {
		return fmt.Errorf("logical router port availability ensure: %w", err)
	}
	if _, err := nb.GetLogicalRouter(ctx, lr); err != nil {
		return fmt.Errorf("get logical router: %w", err)
	}
	if _, err := nb.GetLogicalRouterPort(ctx, lrp); err != nil {
		return fmt.Errorf("get logical router port: %w", err)
	}
	if _, err := nb.GetGatewayChassis(ctx, gw); err != nil {
		return fmt.Errorf("get gateway chassis: %w", err)
	}
	if _, err := nb.GetHAChassis(ctx, ha); err != nil {
		return fmt.Errorf("get ha chassis: %w", err)
	}
	if _, err := nb.GetHAChassisGroup(ctx, hag); err != nil {
		return fmt.Errorf("get ha chassis group: %w", err)
	}

	if err := nb.ACLByMatch("to-lport", 1001, aclMatch).Ensure().WithAction("allow").WithLog(true).WithSeverity("info").WithLabel(7).WithTier(0).WithOption("apply-after-lb", "false").WithExternalID("sdkcheck", "true").Execute(ctx); err != nil {
		return fmt.Errorf("acl ensure: %w", err)
	}
	if _, err := nb.Table("ACL").List(ctx); err != nil {
		return fmt.Errorf("list acl after standalone ensure: %w", err)
	}
	if err := nb.NATByLogicalIP("snat", natIP).Ensure().AttachToRouter(lr).WithExternalIP("192.0.2.220").WithExternalPortRange("").WithMatch("ip4").WithPriority(10).WithOption("stateless", "false").WithExternalID("sdkcheck", "true").Execute(ctx); err != nil {
		return fmt.Errorf("nat ensure: %w", err)
	}
	if _, err := nb.GetNAT(ctx, "snat", natIP); err != nil {
		return fmt.Errorf("get nat: %w", err)
	}
	if err := nb.LoadBalancer(lb).Ensure().AttachToRouter(lr).WithVIP("192.0.2.221:80", "10.220.0.10:80").WithProtocol("tcp").WithSelectionField("ip_src").WithIPPortMapping("10.220.0.10", "192.0.2.221").WithOption("reject", "false").WithExternalID("sdkcheck", "true").Execute(ctx); err != nil {
		return fmt.Errorf("load balancer ensure: %w", err)
	}
	if _, err := nb.GetLoadBalancer(ctx, lb); err != nil {
		return fmt.Errorf("get load balancer: %w", err)
	}
	if err := nb.DHCPOptions(dhcp).Ensure().WithOption("router", "10.220.0.1").WithExternalID("sdkcheck", "true").Execute(ctx); err != nil {
		return fmt.Errorf("dhcp options ensure: %w", err)
	}
	if _, err := nb.GetDHCPOptions(ctx, dhcp); err != nil {
		return fmt.Errorf("get dhcp options: %w", err)
	}
	if err := nb.DNS(dns).Ensure().WithRecord("vm.sdkcheck.test", "10.220.0.10").WithOption("ttl", "30").WithExternalID("sdkcheck", "true").Execute(ctx); err != nil {
		return fmt.Errorf("dns ensure: %w", err)
	}
	if _, err := nb.GetDNS(ctx, dns); err != nil {
		return fmt.Errorf("get dns: %w", err)
	}
	if err := nb.QoSByMatch("from-lport", 100, qosMatch).Ensure().AttachToSwitch(ls).WithDSCP(42).WithRate(1000).WithBurst(2000).WithExternalID("sdkcheck", "true").Execute(ctx); err != nil {
		return fmt.Errorf("qos ensure: %w", err)
	}
	if _, err := nb.GetQoS(ctx, "from-lport", 100, qosMatch); err != nil {
		return fmt.Errorf("get qos: %w", err)
	}
	if err := nb.Meter(meter).Ensure().WithUnit("kbps").WithFair(true).WithNamedBand(meterBand, "drop", 100).WithBandExternalID("sdkcheck", "true").WithExternalID("sdkcheck", "true").Execute(ctx); err != nil {
		return fmt.Errorf("meter ensure: %w", err)
	}
	if _, err := nb.GetMeter(ctx, meter); err != nil {
		return fmt.Errorf("get meter: %w", err)
	}
	if _, err := nb.GetMeterBand(ctx, meterBand); err != nil {
		return fmt.Errorf("get meter band: %w", err)
	}
	pgACLMatch := "outport == \"" + p + "pg\""
	if err := nb.PortGroup(pg).Ensure().WithACL("to-lport", 1002, pgACLMatch, "allow").WithACLExternalID("sdkcheck", "true").WithExternalID("sdkcheck", "true").Execute(ctx); err != nil {
		return fmt.Errorf("port group ensure: %w", err)
	}
	if _, err := nb.GetPortGroup(ctx, pg); err != nil {
		return fmt.Errorf("get port group: %w", err)
	}
	if _, err := nb.GetACL(ctx, "to-lport", 1002, pgACLMatch); err != nil {
		return fmt.Errorf("get attached port-group acl: %w", err)
	}
	if err := nb.AddressSet(addrSet).Ensure().WithAddress("10.220.0.10").WithAddresses("10.220.0.11").WithExternalID("sdkcheck", "true").Execute(ctx); err != nil {
		return fmt.Errorf("address set ensure: %w", err)
	}
	if _, err := nb.GetAddressSet(ctx, addrSet); err != nil {
		return fmt.Errorf("get address set: %w", err)
	}
	if err := nb.BFD(lrp, bfdIP).Ensure().WithMinTx(100).WithMinRx(100).WithDetectMult(3).WithStatus("admin_down").WithOption("check", "true").WithExternalID("sdkcheck", "true").Execute(ctx); err != nil {
		return fmt.Errorf("bfd ensure: %w", err)
	}
	if _, err := nb.GetBFD(ctx, lrp, bfdIP); err != nil {
		return fmt.Errorf("get bfd: %w", err)
	}
	return nil
}

func checkSouthbound(ctx context.Context, client *ovnflow.Client, opts Options) error {
	sb := client.OVN().SB()
	p := opts.Prefix + "sb-"
	sbGlobalRows, err := sb.SBGlobal().List(ctx)
	if err != nil {
		return fmt.Errorf("list sb global: %w", err)
	}
	if len(sbGlobalRows) == 0 {
		return errors.New("SB_Global has no rows")
	}
	sbGlobalUUID := rowString(sbGlobalRows[0]["_uuid"])
	if sbGlobalUUID == "" {
		return errors.New("SB_Global row has no UUID")
	}
	if err := sb.TableBy("SB_Global", "_uuid", sbGlobalUUID).Update().WithExternalID("sdkcheck", "true").Execute(ctx); err != nil {
		return fmt.Errorf("sb runtime sb_global update: %w", err)
	}
	defer func() {
		_ = sb.TableBy("SB_Global", "_uuid", sbGlobalUUID).Update().DeleteMap("external_ids", map[string]string{"sdkcheck": "true"}).Execute(context.Background())
	}()

	if _, err := sb.ListChassis(ctx); err != nil {
		return fmt.Errorf("list chassis: %w", err)
	}
	if _, err := sb.ListPortBindings(ctx); err != nil {
		return fmt.Errorf("list port bindings: %w", err)
	}
	if _, err := sb.ListDatapaths(ctx); err != nil {
		return fmt.Errorf("list datapaths: %w", err)
	}
	if _, err := sb.ListLogicalFlows(ctx); err != nil {
		return fmt.Errorf("list logical flows: %w", err)
	}
	if _, err := sb.ListMACBindings(ctx); err != nil {
		return fmt.Errorf("list mac bindings: %w", err)
	}
	if _, err := sb.ListFDB(ctx); err != nil {
		return fmt.Errorf("list fdb: %w", err)
	}
	if _, err := sb.ListMulticastGroups(ctx); err != nil {
		return fmt.Errorf("list multicast groups: %w", err)
	}
	if _, err := sb.ListServiceMonitors(ctx); err != nil {
		return fmt.Errorf("list service monitors: %w", err)
	}
	if _, err := sb.ListRBACRoles(ctx); err != nil {
		return fmt.Errorf("list rbac roles: %w", err)
	}
	if _, err := sb.ListRBACPermissions(ctx); err != nil {
		return fmt.Errorf("list rbac permissions: %w", err)
	}
	if rows, err := sb.SBGlobal().List(ctx); err != nil || len(rows) == 0 {
		return fmt.Errorf("list sb runtime sb_global rows=%d err=%w", len(rows), err)
	}
	if _, err := sb.ListMeters(ctx); err != nil {
		return fmt.Errorf("list meters: %w", err)
	}
	if _, err := sb.ListMeterBands(ctx); err != nil {
		return fmt.Errorf("list meter bands: %w", err)
	}
	if _, err := sb.ListDNS(ctx); err != nil {
		return fmt.Errorf("list dns: %w", err)
	}
	if _, err := sb.ListBFD(ctx); err != nil {
		return fmt.Errorf("list bfd: %w", err)
	}
	if err := expectNotFound(sb.GetMeter(ctx, p+"missing-meter")); err != nil {
		return fmt.Errorf("get missing sb meter: %w", err)
	}
	if err := expectNotFound(sb.GetChassis(ctx, p+"missing-chassis")); err != nil {
		return fmt.Errorf("get missing chassis: %w", err)
	}
	if err := expectNotFound(sb.GetPortBinding(ctx, p+"missing-port")); err != nil {
		return fmt.Errorf("get missing port binding: %w", err)
	}
	if err := expectNotFound(sb.GetDatapath(ctx, 987654)); err != nil {
		return fmt.Errorf("get missing datapath: %w", err)
	}
	if err := expectNotFound(sb.GetDatapathByUUID(ctx, "00000000-0000-0000-0000-000000000001")); err != nil {
		return fmt.Errorf("get missing datapath uuid: %w", err)
	}
	if err := expectNotFound(sb.GetLogicalFlow(ctx, "00000000-0000-0000-0000-000000000002")); err != nil {
		return fmt.Errorf("get missing logical flow: %w", err)
	}
	if err := expectNotFound(sb.GetMACBinding(ctx, p+"missing-port", "192.0.2.10")); err != nil {
		return fmt.Errorf("get missing mac binding: %w", err)
	}
	if err := expectNotFound(sb.GetFDB(ctx, "00:00:00:00:00:01", 999)); err != nil {
		return fmt.Errorf("get missing fdb: %w", err)
	}
	if err := expectNotFound(sb.GetMulticastGroup(ctx, "00000000-0000-0000-0000-000000000003", 32768)); err != nil {
		return fmt.Errorf("get missing multicast group: %w", err)
	}
	if err := expectNotFound(sb.GetServiceMonitor(ctx, p+"missing-port", "192.0.2.20", "tcp", 80)); err != nil {
		return fmt.Errorf("get missing service monitor: %w", err)
	}
	if err := expectNotFound(sb.GetRBACRole(ctx, p+"missing-role")); err != nil {
		return fmt.Errorf("get missing rbac role: %w", err)
	}
	if err := expectNotFound(sb.GetRBACPermission(ctx, "00000000-0000-0000-0000-000000000004")); err != nil {
		return fmt.Errorf("get missing rbac permission: %w", err)
	}
	if err := expectNotFound(sb.GetMeterBand(ctx, "00000000-0000-0000-0000-000000000005")); err != nil {
		return fmt.Errorf("get missing meter band: %w", err)
	}
	if err := expectNotFound(sb.GetDNS(ctx, "00000000-0000-0000-0000-000000000006")); err != nil {
		return fmt.Errorf("get missing dns: %w", err)
	}
	if err := expectNotFound(sb.GetBFD(ctx, p+"missing-port", "192.0.2.30", 49152, 2)); err != nil {
		return fmt.Errorf("get missing bfd: %w", err)
	}

	cancelWatch, err := checkSBWatches(ctx, sb)
	if err != nil {
		return err
	}
	cancelWatch()
	return nil
}

func checkOVS(ctx context.Context, client *ovnflow.Client, opts Options) error {
	ovs := client.LocalOVS()
	p := opts.Prefix + "ovs-"
	bridge := opts.Bridge
	port := p + "port"
	iface := p + "iface"
	manager := "ptcp:16640:127.0.0.1"
	qos := p + "qos"
	queue := p + "queue"
	mirror := p + "mirror"
	flowTable := p + "flow-table"
	netflow := p + "netflow"
	sflow := p + "sflow"
	ipfix := p + "ipfix"
	autoAttach := p + "auto-attach"

	cleanupOVS(ctx, ovs, bridge, port, manager, qos, queue)
	defer cleanupOVS(context.Background(), ovs, bridge, port, manager, qos, queue)

	if err := ovs.Manager(manager).Ensure().WithManager(manager).WithExternalID("sdkcheck", "true").Execute(ctx); err != nil {
		return fmt.Errorf("manager ensure: %w", err)
	}
	if err := ovs.QoS(qos).Ensure().WithQoSType("linux-htb").WithExternalID("sdkcheck", "true").Execute(ctx); err != nil {
		return fmt.Errorf("qos table ensure: %w", err)
	}
	if err := ovs.Queue(queue).Ensure().WithQueueDSCP(10).WithQueueOtherConfig("max-rate", "1000000").WithExternalID("sdkcheck", "true").Execute(ctx); err != nil {
		return fmt.Errorf("queue table ensure: %w", err)
	}
	if err := ovs.Bridge(bridge).Ensure().
		WithControllerTarget("tcp:127.0.0.1:6653").
		WithFailMode("secure").
		WithDatapathType("system").
		WithExternalID("sdkcheck", "true").
		AddPort(port).
		WithInterfaceName(iface).
		WithInterfaceType("internal").
		WithOption("tag", "10").
		WithInterfaceOption("peer", "sdkcheck").
		WithInterfaceExternalID("sdkcheck", "true").
		WithExternalID("sdkcheck", "true").
		Execute(ctx); err != nil {
		return fmt.Errorf("bridge/port/interface ensure: %w", err)
	}
	if err := ovs.Bridge(bridge).Ensure().
		WithExternalID("sdkcheck", "true").
		WithMirror(mirror, func(b *ovnflow.TableBuilder) { b.WithMirrorSelectAll().WithExternalID("sdkcheck", "true") }).
		WithFlowTable(0, flowTable, func(b *ovnflow.TableBuilder) { b.WithExternalID("sdkcheck", "true") }).
		WithNetFlow(netflow, func(b *ovnflow.TableBuilder) {
			b.WithSamplingTarget("127.0.0.1:2055").WithColumn("engine_type", 1).WithColumn("engine_id", 7).WithColumn("active_timeout", 30).WithExternalID("sdkcheck", "true")
		}).
		WithSFlow(sflow, func(b *ovnflow.TableBuilder) {
			b.WithSamplingTarget("127.0.0.1:6343").WithColumn("agent", "lo").WithColumn("header", 128).WithColumn("sampling", 64).WithColumn("polling", 10).WithExternalID("sdkcheck", "true")
		}).
		WithIPFIX(ipfix, func(b *ovnflow.TableBuilder) {
			b.WithSamplingTarget("127.0.0.1:4739").WithColumn("sampling", 256).WithExternalID("sdkcheck", "true")
		}).
		WithAutoAttach(autoAttach, func(b *ovnflow.TableBuilder) {
			b.WithColumn("system_description", "sdkcheck").WithColumn("mappings", []any{"map", []any{[]any{100, 200}}})
		}).
		Execute(ctx); err != nil {
		return fmt.Errorf("bridge advanced config ensure: %w", err)
	}
	if got, err := ovs.GetBridge(ctx, bridge); err != nil || got.Name != bridge {
		return fmt.Errorf("get bridge = %#v, %w", got, err)
	}
	if got, err := ovs.GetPort(ctx, port); err != nil || got.Name != port {
		return fmt.Errorf("get port = %#v, %w", got, err)
	}
	if got, err := ovs.GetInterface(ctx, iface); err != nil || got.Name != iface {
		return fmt.Errorf("get interface = %#v, %w", got, err)
	}
	if _, err := ovs.ListBridges(ctx); err != nil {
		return fmt.Errorf("list bridges: %w", err)
	}
	if _, err := ovs.ListPorts(ctx); err != nil {
		return fmt.Errorf("list ports: %w", err)
	}
	if _, err := ovs.ListInterfaces(ctx); err != nil {
		return fmt.Errorf("list interfaces: %w", err)
	}
	if err := watchCancel(ctx, "ovs bridges", func(c context.Context) (<-chan ovnflow.RowEvent, <-chan error) { return ovs.WatchBridges(c) }); err != nil {
		return err
	}
	if err := watchCancel(ctx, "ovs ports", func(c context.Context) (<-chan ovnflow.RowEvent, <-chan error) { return ovs.WatchPorts(c) }); err != nil {
		return err
	}
	if err := watchCancel(ctx, "ovs interfaces", func(c context.Context) (<-chan ovnflow.RowEvent, <-chan error) { return ovs.WatchInterfaces(c) }); err != nil {
		return err
	}
	if err := ovs.Bridge(bridge).DeletePort(port).Execute(ctx); err != nil {
		return fmt.Errorf("delete port: %w", err)
	}
	return nil
}

func checkRuntime(ctx context.Context, client *ovnflow.Client, opts Options) error {
	nb := client.OVN().NB()
	name := opts.Prefix + "runtime-as"
	if err := nb.AddressSet(name).Delete().Execute(ctx); err != nil && !ovnflow.IsKind(err, ovnflow.ErrorNotFound) {
		return fmt.Errorf("cleanup runtime address set: %w", err)
	}
	if err := nb.TableBy("Address_Set", "name", name).Create().WithName(name).WithAddressSetAddresses("192.0.2.1").WithExternalID("sdkcheck", "true").WithOptionalColumn("unknown_optional_sdkcheck", "noop").Execute(ctx); err != nil {
		return fmt.Errorf("runtime create: %w", err)
	}
	if err := nb.TableBy("Address_Set", "name", name).Ensure().MutateSet("addresses", "192.0.2.2").MutateMap("external_ids", map[string]string{"sdkcheck2": "true"}).Execute(ctx); err != nil {
		return fmt.Errorf("runtime ensure/mutate: %w", err)
	}
	row, err := nb.TableBy("Address_Set", "name", name).Get(ctx)
	if err != nil {
		return fmt.Errorf("runtime get: %w", err)
	}
	if row["name"] == nil {
		return errors.New("runtime get missing name")
	}
	rows, err := nb.Table("Address_Set").Where("name", name).List(ctx)
	if err != nil {
		return fmt.Errorf("runtime where/list: %w", err)
	}
	if len(rows) != 1 {
		return fmt.Errorf("runtime list rows = %d, want 1", len(rows))
	}
	if err := nb.TableBy("Address_Set", "name", name).Update().DeleteMap("external_ids", map[string]string{"sdkcheck2": "true"}).DeleteSet("addresses", "192.0.2.2").Execute(ctx); err != nil {
		return fmt.Errorf("runtime delete map/set: %w", err)
	}
	if err := watchCancel(ctx, "nb runtime address set", func(c context.Context) (<-chan ovnflow.RowEvent, <-chan error) {
		return nb.Table("Address_Set").Watch(c)
	}); err != nil {
		return err
	}
	if err := nb.TableBy("Address_Set", "name", name).Delete().Execute(ctx); err != nil {
		return fmt.Errorf("runtime delete: %w", err)
	}
	return nil
}

func checkOpenFlowAndSDWAN(ctx context.Context, client *ovnflow.Client, opts Options) error {
	dryRun, err := client.OpenFlow().
		WithEndpoint("tcp:127.0.0.1:6653").
		Bridge(opts.Bridge).
		EnsureFlow(opts.Prefix + "allow-web").
		InPort(1).
		EthType(0x0800).
		IPv4Dst("10.20.0.10/32").
		TCPDst(80).
		Actions().Output(2).
		DryRun(ctx)
	if err != nil {
		return fmt.Errorf("openflow dry-run: %w", err)
	}
	if len(dryRun.Plan.Operations) != 1 {
		return fmt.Errorf("openflow plan operations = %d, want 1", len(dryRun.Plan.Operations))
	}
	sdwanPlan, err := client.SDWAN().Network(opts.Prefix+"wan").Ensure().
		Layer3().
		TopologyPartialMesh().
		WithTransport(ovnflow.SDWANTransportWireGuard).
		AddSite(opts.Prefix+"edge-a", ovnflow.SDWANSite{Router: opts.Prefix + "edge-a", CIDRs: []string{"10.10.0.0/16"}}).
		AddSite(opts.Prefix+"edge-b", ovnflow.SDWANSite{Router: opts.Prefix + "edge-b", CIDRs: []string{"10.20.0.0/16"}}).
		AddLink(ovnflow.SDWANLink{From: opts.Prefix + "edge-a", To: opts.Prefix + "edge-b"}).
		ApplyPlan(ctx)
	if err != nil {
		return fmt.Errorf("sdwan apply plan: %w", err)
	}
	if len(sdwanPlan.Operations) < 4 {
		return fmt.Errorf("sdwan operations = %d, want at least 4", len(sdwanPlan.Operations))
	}
	return nil
}

func cleanupNB(ctx context.Context, nb *ovnflow.NBClient, ls, lsp, lr, lrp, aclMatch, natIP, lb, dhcp, dns, qosMatch, meter, meterBand, pg, addrSet, gw, ha, hag, bfdIP string) {
	_ = nb.BFD(lrp, bfdIP).Delete().Execute(ctx)
	_ = nb.AddressSet(addrSet).Delete().Execute(ctx)
	_ = nb.PortGroup(pg).Delete().Execute(ctx)
	_ = nb.Meter(meter).Delete().Execute(ctx)
	_ = nb.MeterBand(meterBand).Delete().Execute(ctx)
	_ = nb.QoSByMatch("from-lport", 100, qosMatch).Delete().Execute(ctx)
	_ = nb.DNS(dns).Delete().Execute(ctx)
	_ = nb.DHCPOptions(dhcp).Delete().Execute(ctx)
	_ = nb.LoadBalancer(lb).Delete().Execute(ctx)
	_ = nb.NATByLogicalIP("snat", natIP).Delete().Execute(ctx)
	_ = nb.ACLByMatch("to-lport", 1001, aclMatch).Delete().Execute(ctx)
	_ = nb.LogicalRouterPort(lrp).Delete().Execute(ctx)
	_ = nb.LogicalRouter(lr).Delete().Execute(ctx)
	_ = nb.LogicalSwitch(ls).Delete().Execute(ctx)
	_ = nb.GatewayChassis(gw).Delete().Execute(ctx)
	_ = nb.HAChassis(ha).Delete().Execute(ctx)
	_ = nb.HAChassisGroup(hag).Delete().Execute(ctx)
}

func cleanupOVS(ctx context.Context, ovs *ovnflow.OVSClient, bridge, port, manager, qos, queue string) {
	_ = ovs.Bridge(bridge).DeletePort(port).Execute(ctx)
	_ = ovs.Bridge(bridge).Delete().Execute(ctx)
	_ = ovs.Manager(manager).Delete().Execute(ctx)
	_ = ovs.QoS(qos).Delete().Execute(ctx)
	_ = ovs.Queue(queue).Delete().Execute(ctx)
}

func checkSBWatches(ctx context.Context, sb *ovnflow.SBClient) (func(), error) {
	cancels := []context.CancelFunc{}
	cancelOne := func(fn func(context.Context) (<-chan struct{}, <-chan error)) error {
		watchCtx, cancel := context.WithCancel(ctx)
		cancels = append(cancels, cancel)
		_, errs := fn(watchCtx)
		cancel()
		select {
		case err := <-errs:
			if err != nil && !ovnflow.IsKind(err, ovnflow.ErrorCanceled) {
				return err
			}
		case <-time.After(300 * time.Millisecond):
		}
		return nil
	}
	wrapRows := func(fn func(context.Context) (<-chan ovnflow.RowEvent, <-chan error)) func(context.Context) (<-chan struct{}, <-chan error) {
		return func(ctx context.Context) (<-chan struct{}, <-chan error) {
			events, errs := fn(ctx)
			done := make(chan struct{})
			go func() {
				defer close(done)
				_, _ = <-events
			}()
			return done, errs
		}
	}
	if err := cancelOne(wrapRows(func(c context.Context) (<-chan ovnflow.RowEvent, <-chan error) {
		return sb.WatchTable(c, "Port_Binding")
	})); err != nil {
		return nil, fmt.Errorf("watch table: %w", err)
	}
	if err := watchTypedSB(ctx, "WatchPortBindings", sb.WatchPortBindings); err != nil {
		return nil, err
	}
	if err := watchTypedSB(ctx, "WatchChassis", sb.WatchChassis); err != nil {
		return nil, err
	}
	if err := watchTypedSB(ctx, "WatchDatapaths", sb.WatchDatapaths); err != nil {
		return nil, err
	}
	if err := watchTypedSB(ctx, "WatchLogicalFlows", sb.WatchLogicalFlows); err != nil {
		return nil, err
	}
	if err := watchTypedSB(ctx, "WatchMACBindings", sb.WatchMACBindings); err != nil {
		return nil, err
	}
	if err := watchTypedSB(ctx, "WatchFDB", sb.WatchFDB); err != nil {
		return nil, err
	}
	if err := watchTypedSB(ctx, "WatchMulticastGroups", sb.WatchMulticastGroups); err != nil {
		return nil, err
	}
	if err := watchTypedSB(ctx, "WatchServiceMonitors", sb.WatchServiceMonitors); err != nil {
		return nil, err
	}
	if err := watchTypedSB(ctx, "WatchRBACRoles", sb.WatchRBACRoles); err != nil {
		return nil, err
	}
	if err := watchTypedSB(ctx, "WatchRBACPermissions", sb.WatchRBACPermissions); err != nil {
		return nil, err
	}
	if err := watchTypedSB(ctx, "WatchMeters", sb.WatchMeters); err != nil {
		return nil, err
	}
	if err := watchTypedSB(ctx, "WatchMeterBands", sb.WatchMeterBands); err != nil {
		return nil, err
	}
	if err := watchTypedSB(ctx, "WatchDNS", sb.WatchDNS); err != nil {
		return nil, err
	}
	if err := watchTypedSB(ctx, "WatchBFD", sb.WatchBFD); err != nil {
		return nil, err
	}
	return func() {
		for _, cancel := range cancels {
			cancel()
		}
	}, nil
}

func watchTypedSB[T any](ctx context.Context, name string, fn func(context.Context) (<-chan T, <-chan error)) error {
	watchCtx, cancel := context.WithCancel(ctx)
	events, errs := fn(watchCtx)
	cancel()
	select {
	case <-events:
	case err := <-errs:
		if err != nil && !ovnflow.IsKind(err, ovnflow.ErrorCanceled) {
			return fmt.Errorf("%s: %w", name, err)
		}
	case <-time.After(300 * time.Millisecond):
	}
	return nil
}

func watchCancel(ctx context.Context, name string, fn func(context.Context) (<-chan ovnflow.RowEvent, <-chan error)) error {
	watchCtx, cancel := context.WithCancel(ctx)
	events, errs := fn(watchCtx)
	cancel()
	select {
	case <-events:
	case err := <-errs:
		if err != nil && !ovnflow.IsKind(err, ovnflow.ErrorCanceled) {
			return fmt.Errorf("%s watch: %w", name, err)
		}
	case <-time.After(500 * time.Millisecond):
	}
	return nil
}

func expectNotFound[T any](_ T, err error) error {
	if err == nil {
		return errors.New("got nil, want not_found")
	}
	if !ovnflow.IsKind(err, ovnflow.ErrorNotFound) {
		return err
	}
	return nil
}

func hasLS(rows []ovnflow.LogicalSwitch, name string) bool {
	for _, row := range rows {
		if row.Name == name {
			return true
		}
	}
	return false
}

func rowString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case fmt.Stringer:
		return typed.String()
	case map[string]any:
		if id, ok := typed["GoUUID"].(string); ok {
			return id
		}
	case []any:
		if len(typed) == 2 {
			if id, ok := typed[1].(string); ok {
				return id
			}
		}
	}
	return ""
}

func envOrDefault(name, fallback string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	return value
}

func withEnv[T any](name, value string, fn func() T) T {
	old, had := os.LookupEnv(name)
	_ = os.Setenv(name, value)
	defer func() {
		if had {
			_ = os.Setenv(name, old)
		} else {
			_ = os.Unsetenv(name)
		}
	}()
	return fn()
}

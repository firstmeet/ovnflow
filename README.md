# ovnflow

`ovnflow` is a fluent Go SDK for OVN and Open vSwitch. The SDK core uses
[`libovsdb`](https://github.com/ovn-kubernetes/libovsdb) for production OVSDB
connections, runtime schema discovery, watches, and transactions.

```powershell
go get github.com/firstmeet/ovnflow/v2
```

The current SDK surface covers:

| Area | Coverage |
| --- | --- |
| OVN Northbound | logical switch/port plus router, router port, ACL, NAT, load balancer, DHCP, DNS, QoS, meter, port group, address set, OVN gateway, HA chassis group, and BFD table builders |
| OVN Southbound | typed list/get/watch for chassis, port binding, datapath, logical flow, MAC/FDB, multicast, service monitor, RBAC, meter, DNS, and BFD |
| Open_vSwitch | bridge/port/interface lifecycle plus controller, manager, mirror, QoS, queue, flow table, NetFlow, sFlow, IPFIX, SSL, and AutoAttach fluent table APIs |
| OpenFlow | native OpenFlow 1.5/1.3 negotiation, message codec, flow add/delete/dump primitives, and fluent owned-rule builders without shelling out to `ovs-ofctl` |
| SD-WAN | open Site/Link/Tunnel/Policy primitives with explicit Partial Mesh links, Hub-Spoke/Full Mesh planning, L2/L3 overlay modes, WireGuard/Geneve/VXLAN transports, and pluggable Apply backends |
| v2 intent | platform-neutral `VirtualNetwork`, `LogicalSwitchDNS`, `WorkloadAttachment`, `ProviderNetwork`, `SecurityPolicy`, `NetworkService`, and `QoSPolicy` with owner/label metadata, dry-run/reconcile, typed get/inspect, and delete helpers |
| IPAM | pure Go IPv4 CIDR planning, gateway/reserved/excluded address handling, allocation, release, availability, and overlap checks without running a persistent IPAM service |
| LinuxRouter | optional Linux-only namespace router model with DNSMasq, SNAT/MASQUERADE/DNAT/port-forward/destination-map, firewall rules, fake executor tests, and a Linux command backend |
| Diagnostics | read-only `Diagnostics().Doctor` checks for OVSDB connectivity, schema, table counts, port bindings, localnet ports, and OVS bridge mappings; `Diagnostics().AuditOwnership`, `NetworkStatus`, `ProviderNetworkStatus`, `WorkloadPath`, `CleanupPlan`, and `AdoptPlan` report owned resources, orphan/reference risks, and safe planning data |
| Runtime | schema-aware `TableRef` create/ensure/update/delete/get/list/watch with optional columns and map/set mutations |

```go
ctx := context.Background()
client, err := ovnflow.Connect(ctx, ovnflow.ConfigFromEnv())
if err != nil {
    return err
}
defer client.Close()

err = client.OVN().NB().
    LogicalSwitch("ls-web").
    Create().
    WithSubnet("192.168.1.0/24").
    AddPort("port-vm1").
    WithMac("00:11:22:33:44:55").
    WithIP("192.168.1.10").
    Execute(ctx)
if err != nil {
    return err
}

err = client.LocalOVS().
    Bridge("br-ovnflow-it").
    AddPort("vnet0").
    WithInterfaceType("internal").
    WithExternalID("vm-id", "uuid-1234").
    Execute(ctx)
if err != nil {
    return err
}

err = client.LocalOVS().
    Bridge("br-ovnflow-it").
    Ensure().
    WithMirror("mirror-web", func(m *ovnflow.TableBuilder) {
        m.WithMirrorSelectAll().
            WithExternalID("owner", "web")
    }).
    WithNetFlow("nf-web", func(nf *ovnflow.TableBuilder) {
        nf.WithSamplingTarget("127.0.0.1:2055").
            WithExternalID("owner", "web")
    }).
    WithIPFIX("ipfix-web", func(ipfix *ovnflow.TableBuilder) {
        ipfix.WithSamplingTarget("127.0.0.1:4739")
    }).
    Execute(ctx)
if err != nil {
    return err
}

err = client.OpenFlow().
    WithEndpoint("tcp:127.0.0.1:6653").
    Bridge("br-ovnflow-it").
    EnsureFlow("allow-web").
    InPort(1).
    EthType(0x0800).
    IPv4Dst("10.20.0.10/32").
    TCPDst(80).
    Actions().Output(2).
    Execute(ctx)
if err != nil {
    return err
}

err = client.OVN().NB().
    VirtualNetwork("net-web").
    Ensure().
    WithCIDR("10.20.0.0/24").
    WithOwner("project", "alpha").
    WithDNS("net-web-dns", func(d *ovnflow.LogicalSwitchDNSBuilder) {
        d.AddRecord("api.service", "10.20.0.10", "10.20.0.11")
    }).
    Execute(ctx)
if err != nil {
    return err
}

err = client.SDWAN().
    Network("corp-wan").
    Ensure().
    Layer3().
    TopologyPartialMesh().
    WithTransport(ovnflow.SDWANTransportWireGuard).
    AddSite("edge-a", ovnflow.SDWANSite{Router: "edge-a", CIDRs: []string{"10.10.0.0/16"}}).
    AddSite("edge-b", ovnflow.SDWANSite{Router: "edge-b", CIDRs: []string{"10.20.0.0/16"}}).
    AddLink(ovnflow.SDWANLink{From: "edge-a", To: "edge-b"}).
    Apply(ctx)
if err != nil {
    return err
}

network, err := client.OVN().NB().VirtualNetwork("net-web").Get(ctx)
if err != nil {
    return err
}
_ = network

pool := ovnflow.IPAMPool{
    CIDR:     "10.20.0.0/24",
    Reserved: []string{"10.20.0.10"},
}
nextIP, err := pool.Allocate()
if err != nil {
    return err
}
_ = nextIP

err = client.OVN().NB().
    NetworkService("svc-web").
    Ensure().
    WithProtocol("tcp").
    WithOwner("project", "alpha").
    WithVIP("192.0.2.10", 80,
        ovnflow.ServiceBackend{Address: "10.20.0.10", Port: 8080},
        ovnflow.ServiceBackend{Address: "10.20.0.11", Port: 8080},
    ).
    Execute(ctx)
if err != nil {
    return err
}

err = client.OVN().NB().
    QoSPolicy("qos-web").
    Ensure().
    WithOwner("project", "alpha").
    AddRule(ovnflow.QoSRule{
        Name:      "limit-web",
        Direction: "from-lport",
        Priority:  100,
        Match:     `inport == "web-port"`,
        Rate:      1000000,
        Burst:     200000,
    }).
    Execute(ctx)
if err != nil {
    return err
}

err = client.ProviderNetwork("public-uplink").
    Ensure().
    WithPhysicalNetwork("physnet-public").
    OnLogicalSwitch("ls-public").
    WithLocalnetPort("ln-public").
    UseBridge("br-ex").
    WithOwner("project", "alpha").
    Execute(ctx)
if err != nil {
    return err
}

routerClient := linuxrouter.NewPlatformClientWithOVS(client.LocalOVS())
err = routerClient.Router("edge").Apply(ctx, linuxrouter.Router{
    Spec: linuxrouter.Spec{
        Namespace: "ovnflow-edge",
        Interfaces: []linuxrouter.Interface{{
            Name:    "lan0",
            Role:    linuxrouter.InterfaceLAN,
            Bridge:  "br-int",
            OVSPort: "edge-lan",
            Addresses: []string{"172.16.100.1/24"},
            InterfaceExternalIDs: map[string]string{
                "iface-id": "nsr-router-switch-00000001",
            },
        }},
    },
})
if err != nil {
    return err
}

report, err := client.Diagnostics().Doctor(ctx, ovnflow.DoctorOptions{})
if err != nil {
    return err
}
for _, finding := range report.Findings {
    log.Printf("%s %s: %s", finding.Severity, finding.Component, finding.Message)
}

audit, err := client.Diagnostics().AuditOwnership(ctx, ovnflow.OwnershipAuditOptions{
    Owner: ovnflow.OwnerRef{Kind: "project", Name: "alpha"},
})
if err != nil {
    return err
}
for _, finding := range audit.Findings {
    log.Printf("%s %s: %s", finding.Severity, finding.Code, finding.Message)
}

status, err := client.NetworkStatus(ctx, "net-web")
if err != nil {
    return err
}
_ = status

cleanup, err := client.CleanupPlan(ctx, ovnflow.CleanupPlanOptions{
    Owner: ovnflow.OwnerRef{Kind: "project", Name: "alpha"},
})
if err != nil {
    return err
}
_ = cleanup
```

The SDK stays neutral: platform concepts such as tenants, organizations,
departments, users, approval flows, quotas, schedulers, and HA election belong
in the caller's control plane. Map them onto `OwnerRef`, `Labels`, and ordinary
metadata when calling ovnflow.

Normal tests are local and dependency-free:

```powershell
go test ./...
```

Integration tests connect to OVN/OVS OVSDB services over TCP:

```powershell
$env:OVNFLOW_OVS_ADDR="tcp:172.27.192.120:6640"
$env:OVNFLOW_OVN_NB_ADDR="tcp:172.27.192.120:6641"
$env:OVNFLOW_OVN_SB_ADDR="tcp:172.27.192.120:6642"
go test -tags=integration ./...
```

Optional v1.0 readiness checks are also integration-tagged. They are read-only
and validate the NB, SB, and OVS runtime schemas:

```powershell
$env:OVNFLOW_V1_SCHEMA_CHECKS="1"
go test -tags=integration ./...
```

v2 schema and mutation gates are also available:

```powershell
$env:OVNFLOW_V2_SCHEMA_CHECKS="1"
$env:OVNFLOW_V2_MUTATION_CHECKS="1"
go test -tags=integration ./...
```

CI and release validation set `OVNFLOW_REQUIRE_INTEGRATION=1`, which turns
missing or unreachable endpoints into test failures instead of skips.

Runnable examples live under `examples/`:

```powershell
go run ./examples/logical_switch
go run ./examples/local_ovs
go run ./examples/southbound_watch
```

See [Windows + WSL integration tests](docs/windows-wsl-integration.md) for WSL
listener setup, safety settings, and Docker/CI notes.

See [v1.0 hardening](docs/v1.0-hardening.md) and
[API stability](docs/api-stability.md) for the current release gates and stable
surface. The [v0.1 scope](docs/v0.1-scope.md) and
[v0.2 scope](docs/v0.2-scope.md) documents are historical compatibility notes.
The delivered v2.0.0 high-level, platform-neutral intent APIs are recorded in
[v2.0 acceptance](docs/v2.0-plan.md). The v2.1 implementation boundary is in
[v2.1 plan](docs/v2.1-plan.md). The native OpenFlow and SD-WAN foundation
boundary is in [v2.2 plan](docs/v2.2-plan.md). Future v2.x candidates and
deeper hardening work are tracked in [roadmap](docs/roadmap.md).

# ovnflow

`ovnflow` is a fluent Go SDK for OVN and Open vSwitch. The SDK core uses
[`libovsdb`](https://github.com/ovn-kubernetes/libovsdb) for production OVSDB
connections, runtime schema discovery, watches, and transactions.

```powershell
go get github.com/firstmeet/ovnflow
```

The current SDK surface covers:

| Area | Coverage |
| --- | --- |
| OVN Northbound | logical switch/port plus router, router port, ACL, NAT, load balancer, DHCP, DNS, QoS, meter, port group, address set, gateway/HA/BFD builders |
| OVN Southbound | typed list/get/watch for chassis, port binding, datapath, logical flow, MAC/FDB, multicast, service monitor, RBAC, meter, DNS, and BFD |
| Open_vSwitch | bridge/port/interface lifecycle plus controller, manager, mirror, QoS, queue, flow table, NetFlow, sFlow, IPFIX, SSL, and AutoAttach fluent table APIs |
| v2 intent | platform-neutral `VirtualNetwork`, `LogicalSwitchDNS`, `WorkloadAttachment`, and `SecurityPolicy` with owner/label metadata, dry-run/reconcile, typed get/inspect, and delete helpers |
| LinuxRouter | optional Linux-only namespace router model with DNSMasq, SNAT/MASQUERADE/DNAT/port-forward/destination-map, firewall rules, fake executor tests, and a Linux command backend |
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

network, err := client.OVN().NB().VirtualNetwork("net-web").Get(ctx)
if err != nil {
    return err
}
_ = network
```

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
Future high-level, platform-neutral intent APIs are tracked in
[roadmap](docs/roadmap.md), with the v2.0.0 execution plan in
[v2.0 plan](docs/v2.0-plan.md).

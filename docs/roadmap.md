# Roadmap

This document tracks candidate work for future `ovnflow` versions.

## Open intent APIs

Future high-level APIs should stay platform-neutral. `ovnflow` should expose
general networking intent, ownership, and metadata primitives instead of
embedding private business concepts such as organization, department, or user.

Candidate public concepts:

- `VirtualNetwork`: a higher-level wrapper around logical switches, subnets,
  DHCP/DNS, default ACLs, and related network metadata.
- `LogicalSwitchDNS`: OVN DNS records attached to logical switches for
  network-local service names, including multiple IP addresses per domain.
- `Workload` or `Endpoint`: a generic attachment target for VMs, pods, hosts,
  or other compute instances.
- `OwnerRef`: a neutral owner tuple such as kind/name or kind/id, suitable for
  tenants, projects, users, namespaces, or accounts.
- `Labels`: portable key/value metadata for platform-specific grouping,
  ownership, search, cleanup, and quota integration.

Example direction:

```go
err := client.VirtualNetwork("web-net").
    Ensure().
    WithCIDR("10.20.1.0/24").
    WithGateway("10.20.1.1").
    WithDHCP().
    WithOwner("user", "user-001").
    WithLabel("org", "org-001").
    WithLabel("department", "dept-frontend").
    Execute(ctx)
```

Workload attachment should follow the same rule:

```go
err := client.VirtualNetwork("web-net").
    AttachWorkload("vm-1001", "eth0").
    WithInterface("vnet42").
    WithMAC("00:16:3e:11:22:33").
    WithIP("10.20.1.10").
    WithOwner("user", "user-001").
    WithLabel("org", "org-001").
    Execute(ctx)
```

The core SDK should translate these neutral concepts into OVN/OVS resources and
`external_ids`. Private platform packages can map their own organization,
department, user, project, namespace, or account model onto these primitives.
DNS intent may target OVN logical switch DNS records or LinuxRouter dnsmasq host
records depending on the selected network backend.

## Future hardening

- Broaden mutation readback for advanced OVN Northbound tables.
- Add longer real watch lifecycle stress tests.
- Add a multi-version OVN/OVS schema compatibility matrix.
- Add diagnostic APIs that correlate OVN Northbound intent, Southbound runtime
  state, local OVS interfaces, and flow-level troubleshooting output.

## Next implementation candidates

The next feature work should add open, reusable networking primitives instead
of private platform workflows:

- `VirtualNetwork`: compose logical switches, subnets, DHCP/DNS defaults, and
  default ACL policy without binding to a private tenant model.
- `LogicalSwitchDNS`: manage OVN DNS records attached to logical switches for
  in-network service discovery, including one domain mapped to multiple IP
  addresses.
- `WorkloadAttachment`: attach a VM, pod, host, or other workload by keeping
  OVN logical switch ports and local OVS interfaces in sync.
- `ProviderNetwork`: manage OVN localnet ports and OVS bridge mappings for
  reusable physical-network access without embedding platform-specific network
  ownership models.
- `LinuxRouter`: provide the Linux namespace router primitive described below.
- `NAT`: reusable outbound SNAT/masquerade, inbound DNAT/port-forward, and
  destination-address mapping primitives.
- `DNSMasq`: managed DHCP ranges, static leases, DNS forwarding, and host
  records.
- `Firewall`: allow/drop rules, CIDR rules, port rules, and
  established/related connection handling.
- `SecurityPolicy`: a platform-neutral wrapper over ACL, Port Group, and
  Address Set primitives.
- `IPAM helpers`: CIDR validation, reserved addresses, basic allocation, and
  conflict checks without becoming a full IPAM service.
- `Reconcile`, `Diff`, and `DryRun`: preview and compare desired state before
  writing.
- `Status` and `Inspect`: query virtual networks, workload attachments,
  routers, OVS interfaces, and runtime state.
- `Diagnostics`: correlate OVN Northbound intent, Southbound bindings, local
  OVS state, Linux router state, NAT/firewall rules, and flow troubleshooting.
- `Resource ownership`: reusable `OwnerRef`, `Labels`, and `external_ids`
  helpers for querying, grouping, cleanup, and controller reconciliation.
- `Safe cleanup`: SDK-owned resource pruning and orphan/reference checks.
- `Test harness`: WSL/Docker/OVS/OVN endpoint checks, schema checks,
  dependency checks, and permission checks.

Out of scope for the foundation library: organization/department/user models,
billing, quota packages, HA leader election, node scheduling policy, private
database schemas, approval workflows, and other platform-specific control-plane
decisions.

## Linux namespace router

Future Linux-only APIs may provide a local software router built from Linux
network namespaces, OVS ports, dnsmasq, and firewall/NAT rules. This should be
an optional host-networking module, separate from the OVN/OVS OVSDB core API.
HA orchestration, leader election, failover, VIP movement, and keepalived-style
behavior are intentionally out of scope for this SDK layer and should be
implemented by the caller's control plane.

Candidate capabilities:

- Create and delete a managed Linux network namespace.
- Attach LAN and WAN interfaces through OVS bridges or veth pairs.
- Configure LAN addresses, routes, DHCP service, and DNS forwarding through
  managed dnsmasq config and pid files.
- Manage dnsmasq host records and service names so workloads behind a Linux
  router can be reached through configured domains, including multiple A
  records for the same domain.
- Configure WAN addressing either statically or through a DHCP client inside
  the namespace.
- Include NAT as a first-class router module: outbound SNAT, MASQUERADE,
  inbound DNAT, port forwarding, and destination-address mapping through
  nftables or iptables, with safe cleanup of only SDK-owned rules.
- Give every mutable NAT rule a stable name or ID so it can be queried,
  updated, or deleted independently. API names should make it clear when an
  argument is a rule name rather than an address.
- Infer NAT interfaces only when doing so is unambiguous: a single WAN may be
  used as the default outbound interface, and a single LAN may be used as the
  default inbound interface. If multiple candidates exist, return a typed
  ambiguous-interface error instead of guessing.
- Support both builder-style one-shot operations and object-style updates:
  `Get` a router, modify its spec, then `Apply` or `Patch` it.

Example direction:

```go
err := client.LinuxRouter("edge-rtr").
    Ensure().
    AddLAN("lan0").
        AttachOVS("br-int", "edge-rtr-lan").
        WithAddress("10.20.1.1/24").
        Done().
    AddWAN("wan0").
        AttachOVS("br-ex", "edge-rtr-wan").
        WithDHCPClient().
        Done().
    WithDNSMasq().
        WithDHCPRange("10.20.1.100", "10.20.1.200", "12h").
        WithDNSServer("223.5.5.5").
        Done().
    WithNAT().
        Masquerade("10.20.1.0/24", "wan0").
        ForwardTCP("wan0", 8080, "10.20.1.6", 80).
        Done().
    Execute(ctx)
```

Inbound port forwarding should expose a WAN listener and translate it to a LAN
endpoint. For example, traffic to `172.17.100.2:8080` on the WAN interface can
be DNATed to `172.16.100.6:80` behind the router:

```go
WithNAT().
    Masquerade("172.16.100.0/24", "wan0").
    ForwardTCP("wan0", 8080, "172.16.100.6", 80)
```

Source NAT should support both a fixed source address and interface-based
masquerade:

```go
WithNAT().
    SNAT("172.16.100.0/24", "wan0", "172.17.100.29").
    Masquerade("172.16.100.0/24", "wan0")
```

Destination mapping should support traffic from the LAN to a virtual address
that is translated to a physical-side address, optionally with source NAT for
return-path safety:

```go
WithNAT().
    EnsureDestinationMap("legacy-service").
        MatchAddress("192.168.9.2").
        TargetAddress("192.168.0.1").
        FromCIDR("172.16.100.0/24").
        InInterface("lan0").
        OutInterface("wan0").
        WithSourceNAT("172.17.100.29").
        Done()
```

WAN interfaces should also support static configuration:

```go
AddWAN("wan0").
    AttachOVS("br-ex", "edge-rtr-wan").
    WithAddress("172.16.10.2/24").
    WithGateway("172.16.10.1")
```

This API should require Linux privileges such as root or `CAP_NET_ADMIN`, use
Linux build tags, and avoid deleting namespaces, OVS ports, dnsmasq processes,
or firewall rules that were not created and labeled by `ovnflow`. It should not
start or manage keepalived, elect active/standby nodes, or decide failover
policy.

The object model should support UI and controller workflows:

```go
router, err := client.LinuxRouter("edge-rtr").Get(ctx)
if err != nil {
    return err
}

router.Spec.NAT.Masquerades = append(router.Spec.NAT.Masquerades, ovnflow.MasqueradeRule{
    SourceCIDR:   "172.16.100.0/24",
    OutInterface: "wan0",
})

err = client.LinuxRouter("edge-rtr").Apply(ctx, router)
```

Use `Apply` for full desired-state reconciliation and module-specific builders
or `Patch` for incremental changes, so adding NAT or DNSMasq to an existing
router does not unintentionally remove unrelated router modules.

`Get` should return a detailed spec and observed status, not just existence.
For Linux routers this should include managed interfaces, routes, DNSMasq
configuration, NAT rules grouped by type, firewall rules, process state, and
last observed errors where available:

```go
router, err := client.LinuxRouter("edge-rtr").Get(ctx)
if err != nil {
    return err
}

for _, rule := range router.Spec.NAT.SNATRules {
    fmt.Println(rule.SourceCIDR, rule.OutInterface, rule.ToSource)
}
for _, rule := range router.Spec.NAT.DNATRules {
    fmt.Println(rule.MatchAddress, rule.TargetAddress)
}
for _, rule := range router.Spec.NAT.PortForwards {
    fmt.Println(rule.Protocol, rule.ListenPort, rule.TargetIP, rule.TargetPort)
}
```

Status should report runtime observations separately from desired config, such
as namespace existence, interface addresses, DHCP lease details, dnsmasq PID,
NAT backend in use, and whether SDK-owned rules are currently installed.

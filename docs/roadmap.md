# Roadmap

This document tracks candidate work for future `ovnflow` versions. The core
rule remains the same: high-level APIs should expose open networking
primitives, ownership, and metadata instead of embedding private business
concepts such as organization, department, user, billing, approval workflow, HA
election, node scheduling policy, or private database schemas.

## Delivered in v2.0.0

v2.0.0 establishes the open intent layer recorded in
[`v2.0-plan.md`](v2.0-plan.md):

- `OwnerRef`, `Labels`, and reserved `external_ids` helpers for ownership,
  grouping, cleanup, and reconciliation.
- `VirtualNetwork` for logical switch, subnet, gateway, DNS, metadata, diff,
  dry-run, reconcile, get, inspect, apply, patch, and delete workflows.
- `LogicalSwitchDNS` for OVN logical-switch DNS records, including one domain
  mapped to multiple IP addresses.
- `WorkloadAttachment` for VM, pod, host, or other workload attachment through
  OVN logical switch ports and optional local OVS Port/Interface metadata.
- `ProviderNetwork` for OVN localnet ports and Open_vSwitch bridge mappings
  without owning or deleting the physical bridge by default.
- `SecurityPolicy` over ACL, Port Group, and Address Set primitives using
  neutral allow/drop, CIDR, port, and established/related semantics.
- `LinuxRouter` in the Linux-only `linuxrouter` package for managed network
  namespaces, LAN/WAN interfaces, static or DHCP WAN addressing, routes,
  DNSMasq, SNAT, MASQUERADE, DNAT, port forwarding, destination-address
  mapping, firewall rules, Get/Apply/Patch, and detailed status readback.
- Diagnostics foundations: OVSDB Doctor and ownership audit for connectivity,
  schema, table counts, port bindings, localnet ports, OVS bridge mappings,
  owned resources, incomplete ownership markers, and orphan/reference risks.

Private platform packages can map their own tenant, project, namespace,
account, organization, department, or user models onto `OwnerRef` and `Labels`.
Those business models stay outside the foundation SDK.

## Delivered in v2.1.0

- `IPAM helpers` for pure Go IPv4 planning, default gateway handling,
  reserved/excluded addresses, allocation, release, availability, and CIDR
  overlap checks. This is intentionally not a persistent IPAM service.
- `NetworkService` intent over OVN `Load_Balancer` VIPs and backend sets with
  owner/label metadata, dry-run, reconcile, get, patch, and owned-only delete.
- `QoSPolicy` intent over OVN `QoS` rules with owner/label metadata,
  desired-rule cleanup, map-field reconciliation, dry-run, reconcile, get, and
  owned-only delete.
- Read-only status aggregation through network, provider-network, and workload
  path summaries that degrade into typed findings when a backend is missing.
- Read-only cleanup and adopt/import plans built from ownership audit data.
  Execution remains out of scope for this planning layer.

## Delivered in v2.2.0

- Native OpenFlow 1.5/1.3 protocol foundations for hello/version negotiation,
  features request/reply, flow-mod, multipart flow dump envelopes, protocol
  errors, OXM match encoding/parsing, output/set-field actions, and native
  socket I/O without `ovs-ofctl`.
- `client.OpenFlow()` bridge-scoped fluent builders for owned flow add/delete
  with deterministic cookies, exact named-flow deletes, and optional OVS
  controller endpoint setup.
- SD-WAN open primitives for Site, Link, Policy, L2/L3 overlay mode, explicit
  Partial Mesh links, Hub-Spoke/Full Mesh planning, WireGuard/Geneve/VXLAN
  transports, dry-run, Apply, Get, Delete, and pluggable backends.

## v2.x Candidates

The next feature work should build on the delivered v2 primitives:

- `Diagnostics`: correlate OVN Northbound intent, Southbound bindings, local
  OVS interfaces, LinuxRouter state, NAT/firewall rules, and flow-level
  troubleshooting output.
- Executable safe cleanup: explicit SDK-owned pruning for orphaned logical
  switch ports, DNS rows, ACLs, Port Groups, OVS Ports/Interfaces, LinuxRouter
  rules, and incomplete references. Automatic pruning should remain owned-only
  and opt-in.
- Executable `Adopt` / `Import`: explicit, audited adoption of existing
  OVN/OVS resources into ovnflow ownership, separate from normal `Ensure`
  semantics.
- Router-aware status aggregation and LinuxRouter diagnostics summaries for
  UI/controller workflows.
- Batched planning and execution for large network, port, ACL, DNS, and
  provider-network changes.
- Service and load-balancer hardening over health metadata and richer port
  mappings.
- Live OpenFlow integration tests against a real OVS bridge controller,
  including add/delete/dump, protocol error replies, and version fallback.
- Production SD-WAN backends for Linux routes, WireGuard, OVS tunnel rows, and
  OpenFlow rule installation.
- OVS QoS/Queue high-level intent to complement the OVN QoS policy intent.
- Traffic mirror, capture, and debug-flow helpers over OVS mirror/sampling and
  OVN/SB runtime state.

## LinuxRouter Hardening

LinuxRouter HA orchestration, leader election, failover, VIP movement, and
keepalived-style behavior remain out of scope for this SDK layer and should be
implemented by the caller's control plane.

Future LinuxRouter hardening candidates:

- Packet-level NAT and firewall translation tests across realistic LAN/WAN
  topologies.
- OVS port movement stress tests for LinuxRouter LAN/WAN attachments.
- DHCP client lease negotiation and lease readback tests for WAN interfaces.
- Longer dnsmasq lifecycle, reload, and repeated Apply/Patch/Delete tests.
- Connection tracking read-only diagnostics for NAT session troubleshooting.
- Multi-WAN and policy routing helpers with explicit route metrics and
  source-based routing, without implementing HA or node scheduling decisions.

## Compatibility Hardening

- Broaden mutation readback for advanced OVN Northbound tables.
- Add longer real watch lifecycle stress tests.
- Add a multi-version OVN/OVS schema compatibility matrix.
- Keep expanding WSL/Docker/OVS/OVN endpoint checks, dependency checks, and
  permission checks for local and CI environments.

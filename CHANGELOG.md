# Changelog

All notable changes to `ovnflow` are tracked here.

## v2.4.2 - 2026-06-30

### Fixed

- OVSDB fluent operations now classify disconnected transport errors such as
  `not connected`, `connection refused`, `broken pipe`, and
  `transport is closing` as `ErrorUnavailable` instead of generic conflicts.
- OVSDB select, non-insert ensure/update phases, and delete transactions now
  rebuild the underlying libovsdb client and retry once after a detected
  disconnect.

### Hardened

- Open_vSwitch helpers now route through the shared transaction layer, so
  Bridge, Port, Interface, Manager, and Open_vSwitch root operations share the
  same disconnect handling.
- The Open_vSwitch runtime schema declaration now includes common metadata
  columns used by the SDK on Bridge, Port, and Interface rows.

## v2.4.1 - 2026-06-26

### Fixed

- OVSDB connection configuration now accepts comma-separated endpoint lists and
  passes each endpoint to libovsdb, allowing OVS, OVN Northbound, and OVN
  Southbound clients to try multiple database endpoints.

### Hardened

- OVSDB endpoint list parsing now trims whitespace and rejects empty entries
  before opening a client connection.
- Windows/WSL integration configuration validation now checks every
  comma-separated endpoint, so mixed non-`tcp:` entries cannot bypass the
  integration-test safety guard.

## v2.4.0 - 2026-05-30

### Added

- SD-WAN path modes for direct P2P, relay, transit, and auto fallback while
  keeping empty path mode compatible with the v2.3 direct behavior.
- Hub/relay aware WireGuard route and AllowedIPs planning in `sdwanlinux`, so
  auto mode prefers enabled direct links and uses relay/transit links only for
  missing or disabled direct paths.

### Hardened

- Release validation now includes a Windows unit job, proving ordinary tests do
  not require Linux tools, root, WireGuard, OVS, WSL, or Docker.

## v2.3.0 - 2026-05-29

### Added

- Live OpenFlow endpoint integration gate against real `ovs-vswitchd` bridge
  controllers, covering add, dump, delete, and named-cookie verification.
- Linux-only `sdwanlinux` backend with WireGuard, Linux route/policy-rule, OVS
  Geneve/VXLAN tunnel, and optional OpenFlow rule hooks.
- SD-WAN agent/control-plane primitives for agent registration, capabilities,
  heartbeat, assignment, and ACK/status.
- SD-WAN site/link attributes and disabled-link semantics without changing the
  existing `SDWANBackend` interface.

### Hardened

- Docker integration now starts `ovs-vswitchd` and exposes OpenFlow `6653`.
- OVS tunnel cleanup verifies ovnflow SD-WAN ownership markers before deleting
  tunnel ports.

## v2.2.0 - 2026-05-28

### Added

- Native OpenFlow 1.5/1.3 codec and client primitives for hello/features,
  flow-mod, multipart flow dump, errors, OXM matches, and output/set-field
  actions.
- `client.OpenFlow()` fluent owned-rule builders for bridge-scoped add/delete
  flows with cookie ownership boundaries and optional OVS controller
  auto-configuration.
- SD-WAN foundation APIs for Site, Link, Policy, L2/L3 overlay mode,
  explicit Partial Mesh links, Hub-Spoke/Full Mesh planning,
  WireGuard/Geneve/VXLAN transports, dry-run, Apply, Get, and Delete over a
  pluggable backend.

### Hardened

- OpenFlow flow stats requests now include the required pad before cookie
  fields.
- OpenFlow set-field actions now advertise aligned action lengths.
- OpenFlow match parsing now round-trips VLAN VID, masked metadata, IPv4,
  TCP, and UDP fields.
- Fluent `DeleteFlow(name)` now targets the deterministic full cookie for that
  named flow.
- In-memory SD-WAN apply preserves observed status fields and increments
  `LastApplied` instead of resetting it.

### Boundaries

- OpenFlow is implemented as native protocol code, not `ovs-ofctl` shelling.
- SD-WAN remains an open foundation layer; private tenant/user models,
  scheduler decisions, HA election, and full controller policy loops stay in the
  caller's control plane.

## v2.1.0 - 2026-05-28

### Added

- Pure Go IPv4 `IPAMPool` helpers for planning, reserved/excluded addresses,
  allocation, release, availability, and overlap checks.
- `NetworkService` intent over OVN `Load_Balancer` VIPs and backends with
  owner/label metadata, dry-run, reconcile, get, patch, and owned-only delete.
- `QoSPolicy` intent over OVN `QoS` rules with owner/label metadata, stale-rule
  cleanup, bandwidth/action map reconciliation, dry-run, reconcile, get, and
  owned-only delete.
- Read-only `NetworkStatus`, `ProviderNetworkStatus`, and `WorkloadPath`
  summaries.
- Read-only `CleanupPlan` and `AdoptPlan` built from ownership audit data.
- v2.1 Service/QoS scenarios in the integration mutation gate.

### Hardened

- Service and QoS intent writes reject foreign existing rows before reconcile.
- Service reconcile deletes stale VIP keys for owned load balancers.
- QoS reconcile deletes stale owned rules and clears stale bandwidth/action map
  keys.
- IPAM rejects excluded gateways and reserved addresses.

## v2.0.1 - 2026-05-27

### Added

- LinuxRouter interfaces can now attach custom OVS `external_ids` to both
  Port and Interface rows through `PortExternalIDs` and
  `InterfaceExternalIDs`.
- Linux builds now expose `linuxrouter.NewPlatformClientWithOVS`, allowing
  callers to inject `client.LocalOVS()` so LinuxRouter manages OVS
  Port/Interface rows through the SDK OVSDB API while keeping namespace,
  DNSMasq, NAT, and firewall operations on the Linux command backend.

### Fixed

- LinuxRouter OVS Interfaces can now set `external_ids:iface-id`, allowing
  namespace router interfaces to bind to OVN logical switch ports.
- Custom LinuxRouter OVS external IDs are validated and cannot override
  reserved `ovnflow.io/*` ownership metadata.

## v2.0.0 - 2026-05-27

### Added

- Go semantic import path for v2: `github.com/firstmeet/ovnflow/v2`.
- Platform-neutral intent APIs for `VirtualNetwork`, `LogicalSwitchDNS`,
  `WorkloadAttachment`, `ProviderNetwork`, and `SecurityPolicy`.
- Neutral ownership metadata through `OwnerRef`, `Labels`, and reserved
  `ovnflow.io/` external IDs.
- Intent `DryRun`, `Reconcile`, `Get`, `Inspect`, and guarded `Delete`
  helpers.
- Read-only diagnostics with `Diagnostics().Doctor` and
  `Diagnostics().AuditOwnership`.
- Optional Linux-only `linuxrouter` package for namespace routers, DNSMasq,
  SNAT, MASQUERADE, DNAT, port forwarding, destination mapping, firewall
  rules, object-style `Get`/`Apply`/`Patch`, and runtime observation.

### Hardened

- v2 delete paths require ovnflow ownership markers before deleting resources.
- Safe cleanup is audit-first and owned-only; automatic pruning is not enabled
  by default.
- Provider network, workload attachment, and security policy reconciliation
  preserve foreign resources and report typed ownership violations.
- GitHub Actions cover unit, vet, staticcheck, govulncheck, race, Docker
  OVN/OVS integration, v2 mutation checks, external SDK import checks, and
  release-blocking privileged LinuxRouter checks.

## v1.0.0 - 2026-05-24

### Added

- Stable module path: `github.com/firstmeet/ovnflow`.
- Fluent OVN Northbound builders for the primary topology, policy, service,
  DHCP/DNS, QoS, meter, grouping, HA, gateway, and BFD tables.
- Typed OVN Southbound list/get/watch APIs for the main runtime tables.
- Fluent Open_vSwitch table APIs for bridge, port, interface, controller,
  manager, mirror, QoS, queue, flow table, NetFlow, sFlow, IPFIX, SSL, and
  AutoAttach configuration.
- GitHub release workflow for `v*` tags, including static analysis,
  vulnerability scanning, race, integration, and mutation gates.

### Hardened

- Runtime schema checks now cover the primary Southbound API surface.
- Generic `Ensure` falls back to update/mutate when a concurrent insert wins
  the create race.
- Watch subscription cleanup exits when a subscription closes before its parent
  context is canceled.
- Watch manager shutdown now closes subscriptions and pollers when the SDK
  client closes.
- Watch subscriptions deliver the initial snapshot before buffered live events
  for that subscription.
- NB delete paths now clean UUID set and map references by selecting actual
  referrer rows before mutating them.
- Delete cleanup now distinguishes scalar UUID references from set/map UUID
  references and reports `ErrorConflict` for scalar strong references instead
  of issuing invalid mutate operations.
- Delete cleanup now handles same-table UUID set/map referrers while ignoring
  the target row itself.
- OVS bridge and port delete paths now keep shared Port and Interface rows
  instead of deleting objects still referenced by other rows.
- OVS Bridge advanced config rows preserve external map/set values on repeated
  ensure calls and keep multiple Mirror/Flow_Table references on new bridges.
- OVS Manager ensure repairs the root `Open_vSwitch.manager_options` reference
  after concurrent duplicate-create races.
- CI now includes `go vet`, `staticcheck`, `govulncheck`, and Linux race
  testing on a patched Go toolchain.

### Compatibility

- v0.1 logical switch/port and local OVS bridge/port APIs remain source
  compatible.

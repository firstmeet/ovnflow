# Changelog

All notable changes to `ovnflow` are tracked here.

## v2.1.0 - pending release

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

# v2.0.0 interface freeze

This document records the first v2.0.0 interface-freeze PR scope and the
follow-up implementation progress. The initial freeze added public shapes and
fake planning only. Later v2 work added real OVSDB reconciliation, a Linux-only
command backend, runtime LinuxRouter observation, and a release-blocking
privileged LinuxRouter smoke gate.

## Frozen foundations

- `OwnerRef` and `Labels` are platform-neutral metadata primitives.
- Reserved `external_ids` keys use the `ovnflow.io/` prefix.
- Label keys are stored under `ovnflow.io/label/<base64url-no-padding>`, so
  arbitrary caller label keys can be represented without colliding with fixed
  reserved keys.
- New typed error kinds are available for v2 control flow: `unsupported`,
  `permission_denied`, `ambiguous`, `ownership_violation`, and
  `backend_unavailable`.

## Intent APIs

The root package now exposes intent APIs for:

- `VirtualNetwork`
- `LogicalSwitchDNS`
- `WorkloadAttachment`
- `SecurityPolicy`
- `Plan`, `Diff`, `DryRunResult`, `ReconcileResult`, and `InspectResult`

Builder entry points hang off `NBClient`, validate fields, provide plan and
dry-run surfaces, and reconcile through OVN Northbound when a real client is
available. No-client/fake paths still report `Applied: false`, which keeps unit
tests independent from OVN/OVS.

`LogicalSwitchDNS` and LinuxRouter dnsmasq host records model one domain with
multiple IP addresses directly instead of forcing callers to encode records as
flat strings.

v2 intent delete paths are guarded by ovnflow ownership markers before they
remove resources. They require `managed-by=ovnflow`, `api-version=v2`, matching
`kind/name`, and a complete owner marker. `Diagnostics().AuditOwnership` is the
read-only foundation for safe cleanup: it lists owned resources and reports
common orphan/reference risks without mutating NB or OVS.

## LinuxRouter boundary

LinuxRouter lives in the `linuxrouter` package. It defines `Spec`, `Status`,
interfaces, routes, DNSMasq, NAT, firewall, `Get`, `Apply`, and `Patch`
surfaces, plus executor, renderer, and observer interfaces for host command
implementations.

The current implementation keeps ordinary tests fake-safe while Linux builds can
use a host command backend:

- `CommandRenderer` renders planned commands for tests and review.
- `FakeExecutor` records commands.
- Linux builds expose `SystemExecutor` and `LinuxRenderer`; `NewPlatformClient`
  uses them to render and execute `ip`, `ovs-vsctl`, `dnsmasq`, `nft`, and
  `iptables` commands.
- Non-Linux builds return typed `unsupported` errors through the same interface.
- Linux builds now attach a `LinuxObserver` to the platform client. `Get`
  observes namespace existence, interface addresses, routes, dnsmasq pidfile
  state, NAT backend, SDK-owned NAT rules, SDK-owned firewall rules, observed
  hash, and resource version.

NAT rule types include stable names for SNAT, MASQUERADE, DNAT, port forwarding,
and destination mapping. Single WAN or LAN inference is allowed only when there
is exactly one candidate; otherwise planning returns the typed `ambiguous`
error.

The current privileged smoke gate proves namespace lifecycle, loopback
configuration, nftables and iptables owned rule installation, and detailed
runtime status readback under `OVNFLOW_LINUX_ROUTER_CHECKS=1`. Broader
end-to-end checks are still tracked for later hardening: OVS port movement,
dnsmasq process management, packet-level NAT translation, and rule cleanup
reconciliation.

## CI gate placeholders

The first freeze also reserves environment constants for later CI expansion:

- `OVNFLOW_V2_SCHEMA_CHECKS`
- `OVNFLOW_V2_MUTATION_CHECKS`
- `OVNFLOW_LINUX_ROUTER_CHECKS`
- `OVNFLOW_NAT_BACKEND=auto|nftables|iptables`

`release.yml` includes a privileged LinuxRouter matrix for nftables and
iptables, so the GitHub Release job waits for both backends before publishing.

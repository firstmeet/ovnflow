# v2.0.0 interface freeze

This document records the first v2.0.0 interface-freeze PR scope and the
follow-up implementation progress. The initial freeze added public shapes and
fake planning only. Later v2 work has started real OVSDB reconciliation and a
Linux-only command backend, but privileged end-to-end LinuxRouter validation is
still tracked separately.

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

## LinuxRouter boundary

LinuxRouter lives in the `linuxrouter` package. It defines `Spec`, `Status`,
interfaces, routes, DNSMasq, NAT, firewall, `Get`, `Apply`, and `Patch`
surfaces, plus executor and renderer interfaces for future host command
implementations.

The current implementation keeps ordinary tests fake-safe while Linux builds can
use a host command backend:

- `CommandRenderer` renders planned commands for tests and review.
- `FakeExecutor` records commands.
- Linux builds expose `SystemExecutor` and `LinuxRenderer`; `NewPlatformClient`
  uses them to render and execute `ip`, `ovs-vsctl`, `dnsmasq`, `nft`, and
  `iptables` commands.
- Non-Linux builds return typed `unsupported` errors through the same interface.

NAT rule types include stable names for SNAT, MASQUERADE, DNAT, port forwarding,
and destination mapping. Single WAN or LAN inference is allowed only when there
is exactly one candidate; otherwise planning returns the typed `ambiguous`
error.

LinuxRouter still needs privileged integration tests before release completion:
real namespace lifecycle, OVS port movement, dnsmasq process management,
nftables/iptables rule installation, rule cleanup, and detailed runtime status
must be proven under `OVNFLOW_LINUX_ROUTER_CHECKS=1`.

## CI gate placeholders

The first freeze also reserves environment constants for later CI expansion:

- `OVNFLOW_V2_SCHEMA_CHECKS`
- `OVNFLOW_V2_MUTATION_CHECKS`
- `OVNFLOW_LINUX_ROUTER_CHECKS`
- `OVNFLOW_NAT_BACKEND=auto|nftables|iptables`

These are constants and validation helpers only. The interface-freeze PR does
not add privileged CI jobs or invoke host-networking tools.

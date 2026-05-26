# v2.0.0 interface freeze

This document records the first v2.0.0 interface-freeze PR scope. It adds
public shapes and fake planning only; it does not start dnsmasq, enter network
namespaces, write nftables or iptables rules, or perform new OVSDB mutations.

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

The root package now exposes skeletons for:

- `VirtualNetwork`
- `LogicalSwitchDNS`
- `WorkloadAttachment`
- `SecurityPolicy`
- `Plan`, `Diff`, `DryRunResult`, `ReconcileResult`, and `InspectResult`

Builder entry points hang off `NBClient` and currently validate fields and
return no-op plans. `Reconcile` intentionally reports `Applied: false` until
the OVSDB-backed implementation phase.

`LogicalSwitchDNS` and LinuxRouter dnsmasq host records model one domain with
multiple IP addresses directly instead of forcing callers to encode records as
flat strings.

## LinuxRouter boundary

LinuxRouter lives in the `linuxrouter` package. It defines `Spec`, `Status`,
interfaces, routes, DNSMasq, NAT, firewall, `Get`, `Apply`, and `Patch`
surfaces, plus executor and renderer interfaces for future host command
implementations.

The current implementation is deliberately fake-safe:

- `CommandRenderer` renders planned commands for tests and review.
- `FakeExecutor` records commands.
- `NewPlatformClient` returns a usable fake-safe client on Linux builds.
- Non-Linux builds return typed `unsupported` errors through the same interface.

NAT rule types include stable names for SNAT, MASQUERADE, DNAT, port forwarding,
and destination mapping. Single WAN or LAN inference is allowed only when there
is exactly one candidate; otherwise planning returns the typed `ambiguous`
error.

## CI gate placeholders

The first freeze also reserves environment constants for later CI expansion:

- `OVNFLOW_V2_SCHEMA_CHECKS`
- `OVNFLOW_V2_MUTATION_CHECKS`
- `OVNFLOW_LINUX_ROUTER_CHECKS`
- `OVNFLOW_NAT_BACKEND=auto|nftables|iptables`

These are constants and validation helpers only. The interface-freeze PR does
not add privileged CI jobs or invoke host-networking tools.

# ovnflow v2.3.0

`ovnflow` v2.3.0 completes the first production-facing SD-WAN backend layer and
adds a live OpenFlow endpoint gate while keeping business orchestration outside
the SDK.

Install after release with:

```sh
go get github.com/firstmeet/ovnflow/v2@v2.3.0
```

## Highlights

- Added live OpenFlow integration against real `ovs-vswitchd` bridge controller
  endpoints: add a native flow, dump flow stats, delete it, and verify the
  named cookie is gone.
- Added the Linux-only `sdwanlinux` backend for WireGuard command execution,
  Linux route/policy-rule installation, OVS Geneve/VXLAN tunnel rows, and
  optional L2 OpenFlow rule installation.
- Added open SD-WAN agent/control-plane primitives for registration,
  capabilities, heartbeat, assignment, and assignment ACK/status.
- Added SD-WAN site/link attributes and disabled-link semantics without
  changing the existing `SDWANBackend` interface.

## Boundaries

- Default SD-WAN remains in-memory; production callers explicitly inject the
  `sdwanlinux` backend or their own backend.
- HA election, failover policy, tenant/user models, scheduling, approval, and
  private controller loops remain caller responsibilities.
- Ordinary tests still do not require OVS, WSL, Docker, WireGuard, root, or
  Linux tools.

## Validation

- `go test -count=1 ./...`
- `go vet ./...`
- `go -C tools/sdkcheck test -count=1 ./...`
- `GOOS=linux GOARCH=amd64 go test -c ./sdwanlinux`
- Docker OVN/OVS integration, live OpenFlow endpoint checks, SD-WAN OVS tunnel
  and OpenFlow hook checks, mutation gates, privileged SD-WAN WireGuard/route checks, and
  privileged LinuxRouter matrix in GitHub Actions.

---

# ovnflow v2.2.0

`ovnflow` v2.2.0 adds the native flow-programming and SD-WAN foundation layer
while keeping controller-specific policy outside the SDK.

Install after release with:

```sh
go get github.com/firstmeet/ovnflow/v2@v2.2.0
```

## Highlights

- Added a pure Go OpenFlow 1.5/1.3 codec and thin client for hello/version
  negotiation, features request/reply, flow-mod, multipart flow dump envelopes,
  protocol errors, OXM basic matches, output actions, and set-field actions.
- Added `client.OpenFlow()` bridge-scoped fluent owned-rule builders with
  deterministic ovnflow cookie ownership and optional OVS controller endpoint
  setup.
- Added SD-WAN foundation APIs for Site, Link, Policy, L2/L3 overlay mode,
  explicit Partial Mesh links, Hub-Spoke/Full Mesh planning,
  WireGuard/Geneve/VXLAN transports, dry-run, Apply, Get, Delete, and
  pluggable backends.
- Added a public SD-WAN backend injection path through `Config.SDWAN`,
  `Client.UseSDWANBackend`, and `NewSDWANClient(customBackend)`.
- Hardened OpenFlow wire encoding for flow stats requests, set-field action
  alignment, OXM VLAN parsing, and exact named-flow deletes.

## Boundaries

- OpenFlow is native socket protocol code and does not shell out to
  `ovs-ofctl`.
- SD-WAN remains an open foundation layer. Private tenant/user models,
  schedulers, HA election, and full controller policy loops stay in the
  caller's control plane.
- Live OpenFlow bridge-controller integration tests and production SD-WAN
  backends are future hardening items; v2.2.0 ships the protocol, planning
  model, fluent API, and backend contract.

## Validation

- `go test -count=1 ./...`
- `go vet ./...`
- `go -C tools/sdkcheck test -count=1 ./...`
- Docker OVN/OVS integration and mutation gates in GitHub Actions
- Release workflow privileged LinuxRouter matrix for `auto`, `nftables`, and
  `iptables`

---

# ovnflow v2.1.0

`ovnflow` v2.1.0 expands the open v2 foundation with pure planning helpers,
service and QoS intents, and read-only operational plans.

Install after release with:

```sh
go get github.com/firstmeet/ovnflow/v2@v2.1.0
```

## Highlights

- Added pure Go IPv4 `IPAMPool` helpers for CIDR planning, gateway selection,
  reserved/excluded addresses, allocation, release, availability, and overlap
  checks. This does not persist leases or run an IPAM service.
- Added `NetworkService` intent over OVN `Load_Balancer` VIPs and backend
  sets, with owner/label metadata, dry-run, reconcile, get, patch, and
  owned-only delete.
- Added `QoSPolicy` intent over OVN `QoS` rules, with owner/label metadata,
  stale rule cleanup, bandwidth/action map reconciliation, dry-run, reconcile,
  get, and owned-only delete.
- Added read-only `NetworkStatus`, `ProviderNetworkStatus`, and `WorkloadPath`
  summaries that degrade into typed findings when backends are missing.
- Added read-only `CleanupPlan` and `AdoptPlan` from ownership audit data.
  These plans do not delete or adopt resources by default.
- Added v2.1 Service/QoS coverage to the integration mutation gate.

## Boundaries

- No tenant, organization, department, user, quota, approval, scheduler, or HA
  election model is embedded in the SDK.
- Persistent IPAM, executable adoption, executable cleanup, OVS QoS/Queue
  intent, multi-WAN routing, and LinuxRouter HA remain future work or caller
  control-plane responsibilities.

## Validation

- `go test -count=1 ./...`
- `go vet ./...`
- `go test -race -count=1 ./...`
- `go -C tools/sdkcheck test -count=1 ./...`
- Integration mutation gate with `OVNFLOW_V2_MUTATION_CHECKS=1`

---

# ovnflow v2.0.1

`ovnflow` v2.0.1 is a LinuxRouter patch release focused on OVN logical switch
integration for namespace router interfaces.

Install with:

```sh
go get github.com/firstmeet/ovnflow/v2@v2.0.1
```

## Highlights

- `linuxrouter.Interface` now supports `PortExternalIDs` and
  `InterfaceExternalIDs`.
- LinuxRouter can set OVS Interface metadata such as
  `external_ids:iface-id=<logical-switch-port>`, allowing router namespace
  interfaces to bind to OVN logical switch ports.
- Linux builds now expose `linuxrouter.NewPlatformClientWithOVS`, which uses an
  injected `client.LocalOVS()` to manage OVS Port/Interface rows through the SDK
  OVSDB API.
- Linux namespace, address, route, DNSMasq, NAT, and firewall work continues to
  use the existing Linux command backend.
- Custom LinuxRouter OVS external IDs are validated and cannot override
  reserved `ovnflow.io/*` ownership metadata.

## Validation

- `go test ./linuxrouter ./...`
- `go vet ./...`
- `GOOS=windows GOARCH=amd64 go test -c ./linuxrouter`
- GitHub Actions `test` workflow on `main`

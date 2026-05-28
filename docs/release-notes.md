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

# ovnflow v1.0.0

`ovnflow` v1.0.0 is the first stable release candidate for the fluent OVN and
Open vSwitch SDK.

Install with:

```sh
go get github.com/firstmeet/ovnflow@v1.0.0
```

## Highlights

- Fluent OVN Northbound lifecycle builders for logical switches, routers,
  ports, ACLs, NAT, load balancers, DHCP, DNS, QoS, meters, port groups,
  address sets, gateway chassis, HA chassis, and BFD.
- Typed OVN Southbound list/get/watch APIs for the primary runtime tables.
- Fluent Open_vSwitch configuration APIs for bridges, ports, interfaces,
  controllers, managers, mirrors, QoS, queues, flow tables, NetFlow, sFlow,
  IPFIX, SSL, and AutoAttach.
- Runtime schema checks with typed errors for missing required tables or
  columns and graceful degradation for optional columns.
- Map/set updates use OVSDB mutations by default to preserve keys and values
  owned by other controllers.
- Integration tests support Windows-to-WSL endpoints and Linux Docker Compose.

## Validation

- `go test ./...`
- `go vet ./...`
- `go test -tags=integration ./...`
- GitHub Actions unit and integration workflows

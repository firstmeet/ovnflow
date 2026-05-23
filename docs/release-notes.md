# ovnflow v1.0.0

`ovnflow` v1.0.0 is the first stable release for the fluent OVN and Open
vSwitch SDK.

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
- Bridge-level Open_vSwitch helpers attach Mirror, Flow_Table, NetFlow, sFlow,
  IPFIX, and AutoAttach rows in the same transaction, so non-root OVS tables do
  not get garbage-collected before being referenced.
- Runtime schema checks with typed errors for missing required tables or
  columns and graceful degradation for optional columns.
- Map/set updates use OVSDB mutations by default to preserve keys and values
  owned by other controllers.
- OVS Bridge advanced config references use UUID-safe reads, mutate set/map
  references, and update scalar optional references such as `netflow`, `sflow`,
  `ipfix`, and `auto_attach`.
- OVS Bridge advanced config ensure calls preserve existing map/set values,
  retain multiple Mirror/Flow_Table references, and avoid deleting shared Port
  or Interface rows.
- Watch subscriptions and pollers shut down when the SDK client closes.
- Integration tests support Windows-to-WSL endpoints and Linux Docker Compose.

## Validation

- `go test ./...`
- `go vet ./...`
- `go run honnef.co/go/tools/cmd/staticcheck@latest ./...`
- `GOTOOLCHAIN=go1.25.10 go run golang.org/x/vuln/cmd/govulncheck@latest ./...`
- `go test -run Example ./...`
- `go test -bench=. -benchmem ./...`
- `OVNFLOW_REQUIRE_INTEGRATION=1 go test -tags=integration ./...`
- `OVNFLOW_REQUIRE_INTEGRATION=1 OVNFLOW_V1_MUTATION_CHECKS=1 go test -tags=integration -run TestIntegrationV1MutationScenariosAreEnvGated ./...`
- GitHub Actions unit, race, integration, mutation, and release workflows

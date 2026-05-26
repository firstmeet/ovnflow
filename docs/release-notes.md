# ovnflow v2.0.0

`ovnflow` v2.0.0 adds open, platform-neutral intent APIs on top of the fluent
OVN and Open vSwitch SDK, plus an optional Linux namespace router module.

Install with:

```sh
go get github.com/firstmeet/ovnflow/v2@v2.0.0
```

## Highlights

- Go semantic import path for v2: `github.com/firstmeet/ovnflow/v2`.
- Platform-neutral intent APIs for virtual networks, logical-switch DNS,
  workload attachments, provider networks, and security policies.
- Neutral `OwnerRef` and `Labels` metadata instead of private tenant,
  organization, department, or user models.
- Intent `DryRun`, `Reconcile`, `Get`, `Inspect`, and guarded `Delete`
  surfaces.
- OVN logical switch DNS records support one domain mapped to multiple IP
  addresses.
- `ProviderNetwork` keeps OVN localnet ports and OVS bridge mappings aligned.
- `WorkloadAttachment` keeps OVN logical switch ports and local OVS interfaces
  aligned when an OVS client is available.
- `Diagnostics().Doctor` checks OVSDB connectivity, schema, table counts, port
  bindings, localnet ports, and bridge mappings.
- `Diagnostics().AuditOwnership` reports ovnflow-owned resources,
  incomplete ownership markers, missing references, and common orphan risks
  without mutating the databases.
- Optional Linux-only `linuxrouter` package for namespace routers, DNSMasq,
  SNAT, MASQUERADE, DNAT, port forwarding, destination mapping, firewall rules,
  object-style `Get`/`Apply`/`Patch`, and runtime status observation.
- v2 delete paths require complete ovnflow ownership markers before deleting
  resources and preserve foreign resources.
- The privileged LinuxRouter release gate validates namespace lifecycle,
  dnsmasq process/readback, multi-address DNS answers, owned NAT/firewall rule
  installation for nftables and iptables, and cleanup boundaries. Full
  packet-level NAT/firewall behavior tests remain future hardening.

## Validation

- `go test ./...`
- `go vet ./...`
- `go run honnef.co/go/tools/cmd/staticcheck@latest ./...`
- `GOTOOLCHAIN=go1.25.10 go run golang.org/x/vuln/cmd/govulncheck@latest ./...`
- `go test -run Example ./...`
- `go test -bench=. -benchmem ./...`
- `go -C tools/sdkcheck test -count=1 ./...`
- `OVNFLOW_REQUIRE_INTEGRATION=1 go test -tags=integration ./...`
- `OVNFLOW_REQUIRE_INTEGRATION=1 OVNFLOW_V2_MUTATION_CHECKS=1 go test -tags=integration -run TestIntegrationV2MutationGateIsEnvGated ./...`
- `OVNFLOW_LINUX_ROUTER_CHECKS=1 OVNFLOW_NAT_BACKEND=auto|nftables|iptables sudo -E go test -tags=integration ./linuxrouter`
- GitHub Actions unit, race, integration, mutation, SDK import, privileged
  LinuxRouter, and release workflows

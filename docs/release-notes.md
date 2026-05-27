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

# ovnflow

`ovnflow` is a fluent Go SDK for OVN and Open vSwitch. The SDK core uses
[`libovsdb`](https://github.com/ovn-kubernetes/libovsdb) for production OVSDB
connections, schema monitoring, typed models, watches, and transactions.

```go
ctx := context.Background()
client, err := ovnflow.Connect(ctx, ovnflow.ConfigFromEnv())
if err != nil {
    return err
}
defer client.Close()

err = client.OVN().NB().
    LogicalSwitch("ls-web").
    Create().
    WithSubnet("192.168.1.0/24").
    AddPort("port-vm1").
    WithMac("00:11:22:33:44:55").
    WithIP("192.168.1.10").
    Execute(ctx)
if err != nil {
    return err
}

err = client.LocalOVS().
    Bridge("br-ovnflow-it").
    AddPort("vnet0").
    WithInterfaceType("internal").
    WithExternalID("vm-id", "uuid-1234").
    Execute(ctx)
if err != nil {
    return err
}
```

Normal tests are local and dependency-free:

```powershell
go test ./...
```

Integration tests connect to OVN/OVS OVSDB services over TCP:

```powershell
$env:OVNFLOW_OVS_ADDR="tcp:172.27.192.120:6640"
$env:OVNFLOW_OVN_NB_ADDR="tcp:172.27.192.120:6641"
$env:OVNFLOW_OVN_SB_ADDR="tcp:172.27.192.120:6642"
go test -tags=integration ./...
```

See [Windows + WSL integration tests](docs/windows-wsl-integration.md) for WSL
listener setup, safety settings, and Docker/CI notes.

See [v0.1 scope](docs/v0.1-scope.md) for the current API coverage and
acceptance matrix.

# Windows + WSL integration tests

Normal tests do not require WSL, Docker, OVN, or OVS:

```powershell
go test ./...
```

Integration tests are enabled explicitly with the `integration` build tag. On
Windows, run the Go test process on Windows and connect to the OVN/OVS services
running inside WSL Ubuntu 22.04 over TCP.

```powershell
$env:OVNFLOW_OVS_ADDR="tcp:172.27.192.120:6640"
$env:OVNFLOW_OVN_NB_ADDR="tcp:172.27.192.120:6641"
$env:OVNFLOW_OVN_SB_ADDR="tcp:172.27.192.120:6642"
go test -tags=integration ./...
```

Do not hard-code the WSL IP address in tests. WSL can change its address after a
restart, so pass the address through environment variables.

## WSL service checks

Inside WSL, confirm that the OVSDB endpoints are listening on the WSL network
interface or on all interfaces, not only on `127.0.0.1`:

```bash
ss -lntp | grep -E '6640|6641|6642'
sudo ovs-vsctl set-manager ptcp:6640:0.0.0.0
ovn-nbctl set-connection ptcp:6641:0.0.0.0
ovn-sbctl set-connection ptcp:6642:0.0.0.0
```

From Windows, verify TCP reachability before running integration tests:

```powershell
Test-NetConnection 172.27.192.120 -Port 6640
Test-NetConnection 172.27.192.120 -Port 6641
Test-NetConnection 172.27.192.120 -Port 6642
```

If any endpoint is missing or unreachable, integration tests skip and print the
WSL commands above.

## Test resources

The integration tests create only prefixed resources and remove them after each
run:

- default resource prefix: `ovnflow-it-`
- default OVS bridge: `br-ovnflow-it`

Override them when needed:

```powershell
$env:OVNFLOW_TEST_PREFIX="ovnflow-it-dev-"
$env:OVNFLOW_TEST_BRIDGE="br-ovnflow-it"
```

The tests refuse to target `br-int` unless explicitly allowed:

```powershell
$env:OVNFLOW_TEST_BRIDGE="br-int"
$env:OVNFLOW_ALLOW_BR_INT="1"
```

Use a dedicated bridge for normal development. `br-int` is usually managed by
OVN and should not be used as an integration-test scratch bridge.

## Docker role

Windows does not need Docker for these tests. Docker is reserved for WSL or Linux
CI, where a compose file can start a disposable OVN/OVS environment that exposes
the same TCP ports:

- Open_vSwitch OVSDB: `6640`
- OVN Northbound: `6641`
- OVN Southbound: `6642`

The same Go test code works for a long-running WSL setup and for a disposable
Docker setup because both modes are configured only through endpoint
environment variables.

From WSL or Linux CI:

```bash
docker compose up -d
OVNFLOW_OVS_ADDR=tcp:127.0.0.1:6640 \
OVNFLOW_OVN_NB_ADDR=tcp:127.0.0.1:6641 \
OVNFLOW_OVN_SB_ADDR=tcp:127.0.0.1:6642 \
go test -tags=integration ./...
docker compose down -v
```

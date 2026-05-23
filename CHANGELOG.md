# Changelog

All notable changes to `ovnflow` are tracked here.

## v1.0.0 - pending release approval

### Added

- Stable module path: `github.com/firstmeet/ovnflow`.
- Fluent OVN Northbound builders for the primary topology, policy, service,
  DHCP/DNS, QoS, meter, grouping, HA, gateway, and BFD tables.
- Typed OVN Southbound list/get/watch APIs for the main runtime tables.
- Fluent Open_vSwitch table APIs for bridge, port, interface, controller,
  manager, mirror, QoS, queue, flow table, NetFlow, sFlow, IPFIX, SSL, and
  AutoAttach configuration.
- GitHub release workflow for `v*` tags.

### Hardened

- Runtime schema checks now cover the primary Southbound API surface.
- Generic `Ensure` falls back to update/mutate when a concurrent insert wins
  the create race.
- Watch subscription cleanup exits when a subscription closes before its parent
  context is canceled.
- Watch subscriptions deliver the initial snapshot before buffered live events
  for that subscription.
- NB delete paths now clean UUID set and map references by selecting actual
  referrer rows before mutating them.
- Delete cleanup now distinguishes scalar UUID references from set/map UUID
  references and reports `ErrorConflict` for scalar strong references instead
  of issuing invalid mutate operations.
- OVS bridge and port delete paths now keep shared Port and Interface rows
  instead of deleting objects still referenced by other rows.
- CI now includes `go vet` and Linux race testing.

### Compatibility

- v0.1 logical switch/port and local OVS bridge/port APIs remain source
  compatible.

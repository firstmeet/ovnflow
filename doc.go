// Package ovnflow provides a fluent Go SDK for OVN and Open vSwitch.
//
// The SDK uses github.com/ovn-kubernetes/libovsdb for all production OVSDB
// connections, schema discovery, and transactions. The stable API covers
// distributed-virtualization control-plane paths that are
// painful to express with shell commands: OVN Northbound topology, policy,
// service, DHCP/DNS, QoS, meter, group, HA, gateway, and BFD builders; OVN
// Southbound typed reads and watches for the main runtime tables; and local
// Open_vSwitch bridge, port, interface, controller, manager, mirror, QoS, queue,
// sampling, SSL, and AutoAttach configuration.
//
// Dynamic TableRef helpers remain available for version-specific schema columns.
//
// Normal tests are local and do not require OVN, OVS, WSL, or Docker.
// Integration tests are enabled explicitly with the integration build tag and
// read TCP endpoints from environment variables.
package ovnflow

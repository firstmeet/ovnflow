// Package ovnflow provides a fluent Go SDK for OVN and Open vSwitch.
//
// The SDK uses github.com/ovn-kubernetes/libovsdb for all production OVSDB
// connections, schema monitoring, typed cache reads, and transactions. The
// v0.1 API focuses on the distributed-virtualization control-plane paths that
// are painful to express with shell commands: OVN Northbound logical switch and
// logical switch port writes, OVN Southbound typed reads and watches, and local
// Open_vSwitch bridge/port/interface writes.
//
// The v0.2 API extends that surface to Northbound routers, router ports, ACLs,
// NAT, load balancers, DHCP, DNS, QoS, meters, groups, HA/BFD helpers;
// Southbound typed list/get/watch APIs for the main runtime tables; and local
// Open_vSwitch fluent table APIs for controller, manager, mirror, QoS, queue,
// sampling, SSL, and AutoAttach configuration. Dynamic TableRef helpers remain
// available for version-specific schema columns.
//
// Normal tests are local and do not require OVN, OVS, WSL, or Docker.
// Integration tests are enabled explicitly with the integration build tag and
// read TCP endpoints from environment variables.
package ovnflow

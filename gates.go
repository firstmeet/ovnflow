package ovnflow

import "strings"

const (
	EnvV2SchemaChecks        = "OVNFLOW_V2_SCHEMA_CHECKS"
	EnvV2MutationChecks      = "OVNFLOW_V2_MUTATION_CHECKS"
	EnvLinuxRouterChecks     = "OVNFLOW_LINUX_ROUTER_CHECKS"
	EnvLinuxRouterNATBackend = "OVNFLOW_NAT_BACKEND"
	EnvOpenFlowChecks        = "OVNFLOW_OPENFLOW_CHECKS"
	EnvSDWANBackendChecks    = "OVNFLOW_SDWAN_BACKEND_CHECKS"
	EnvSDWANPrivilegedChecks = "OVNFLOW_SDWAN_PRIVILEGED_CHECKS"
	EnvWireGuardChecks       = "OVNFLOW_WIREGUARD_CHECKS"
	EnvOVSTunnelChecks       = "OVNFLOW_OVS_TUNNEL_CHECKS"
	EnvLinuxRouteChecks      = "OVNFLOW_LINUX_ROUTE_CHECKS"

	NATBackendAuto     = "auto"
	NATBackendNFTables = "nftables"
	NATBackendIPTables = "iptables"
)

func EnvGateEnabled(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func ValidNATBackend(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", NATBackendAuto, NATBackendNFTables, NATBackendIPTables:
		return true
	default:
		return false
	}
}

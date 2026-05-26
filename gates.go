package ovnflow

import "strings"

const (
	EnvV2SchemaChecks        = "OVNFLOW_V2_SCHEMA_CHECKS"
	EnvV2MutationChecks      = "OVNFLOW_V2_MUTATION_CHECKS"
	EnvLinuxRouterChecks     = "OVNFLOW_LINUX_ROUTER_CHECKS"
	EnvLinuxRouterNATBackend = "OVNFLOW_NAT_BACKEND"

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

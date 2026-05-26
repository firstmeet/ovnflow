package ovnflow

import "testing"

func TestV2GateConstantsAndHelpers(t *testing.T) {
	if EnvV2SchemaChecks != "OVNFLOW_V2_SCHEMA_CHECKS" || EnvV2MutationChecks != "OVNFLOW_V2_MUTATION_CHECKS" || EnvLinuxRouterChecks != "OVNFLOW_LINUX_ROUTER_CHECKS" {
		t.Fatalf("unexpected v2 gate constants")
	}
	if !EnvGateEnabled("true") || !EnvGateEnabled("1") || EnvGateEnabled("false") {
		t.Fatalf("EnvGateEnabled did not parse expected values")
	}
	if !ValidNATBackend(NATBackendAuto) || !ValidNATBackend(NATBackendNFTables) || !ValidNATBackend(NATBackendIPTables) || ValidNATBackend("pf") {
		t.Fatalf("ValidNATBackend did not validate expected values")
	}
}

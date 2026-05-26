//go:build integration

package ovnflow

import (
	"os"
	"testing"
)

func TestIntegrationV2ReadinessAreEnvGated(t *testing.T) {
	if !EnvGateEnabled(os.Getenv(EnvV2SchemaChecks)) {
		t.Skip(EnvV2SchemaChecks + " not enabled")
	}
	cfg := ConfigFromEnv()
	if cfg.OVNNBAddr == "" || cfg.OVNSBAddr == "" || cfg.OVSAddr == "" {
		t.Fatalf("%s requires OVN/OVS endpoints in integration env", EnvV2SchemaChecks)
	}
}

func TestIntegrationV2MutationGateIsEnvGated(t *testing.T) {
	if !EnvGateEnabled(os.Getenv(EnvV2MutationChecks)) {
		t.Skip(EnvV2MutationChecks + " not enabled")
	}
	cfg := ConfigFromEnv()
	if cfg.OVNNBAddr == "" || cfg.OVSAddr == "" {
		t.Fatalf("%s requires OVN NB and OVS endpoints in integration env", EnvV2MutationChecks)
	}
}

func TestIntegrationLinuxRouterGateIsEnvGated(t *testing.T) {
	if !EnvGateEnabled(os.Getenv(EnvLinuxRouterChecks)) {
		t.Skip(EnvLinuxRouterChecks + " not enabled")
	}
	if !ValidNATBackend(os.Getenv(EnvLinuxRouterNATBackend)) {
		t.Fatalf("invalid %s value %q", EnvLinuxRouterNATBackend, os.Getenv(EnvLinuxRouterNATBackend))
	}
}

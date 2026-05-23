package ovnflow

import (
	"reflect"
	"testing"
)

func TestLoadIntegrationConfigFromEnvDefaults(t *testing.T) {
	t.Setenv(EnvOVSAddr, "")
	t.Setenv(EnvOVNNBAddr, "")
	t.Setenv(EnvOVNSBAddr, "")
	t.Setenv(EnvTestResourcePrefix, "")
	t.Setenv(EnvTestBridge, "")
	t.Setenv(EnvAllowBRInt, "")

	cfg := LoadIntegrationConfigFromEnv()
	if cfg.ResourcePrefix != DefaultIntegrationResourcePrefix {
		t.Fatalf("ResourcePrefix = %q, want %q", cfg.ResourcePrefix, DefaultIntegrationResourcePrefix)
	}
	if cfg.BridgeName != DefaultIntegrationBridge {
		t.Fatalf("BridgeName = %q, want %q", cfg.BridgeName, DefaultIntegrationBridge)
	}
	if cfg.AllowBRInt {
		t.Fatal("AllowBRInt = true, want false")
	}

	wantMissing := []string{EnvOVSAddr, EnvOVNNBAddr, EnvOVNSBAddr}
	if got := cfg.MissingEndpoints(); !reflect.DeepEqual(got, wantMissing) {
		t.Fatalf("MissingEndpoints() = %v, want %v", got, wantMissing)
	}
}

func TestLoadIntegrationConfigFromEnvOverrides(t *testing.T) {
	t.Setenv(EnvOVSAddr, "tcp:172.27.192.120:6640")
	t.Setenv(EnvOVNNBAddr, "tcp:172.27.192.120:6641")
	t.Setenv(EnvOVNSBAddr, "tcp:172.27.192.120:6642")
	t.Setenv(EnvTestResourcePrefix, "case-")
	t.Setenv(EnvTestBridge, "br-case")
	t.Setenv(EnvAllowBRInt, "yes")

	cfg := LoadIntegrationConfigFromEnv()
	if cfg.OVSAddr != "tcp:172.27.192.120:6640" {
		t.Fatalf("OVSAddr = %q", cfg.OVSAddr)
	}
	if cfg.OVNNBAddr != "tcp:172.27.192.120:6641" {
		t.Fatalf("OVNNBAddr = %q", cfg.OVNNBAddr)
	}
	if cfg.OVNSBAddr != "tcp:172.27.192.120:6642" {
		t.Fatalf("OVNSBAddr = %q", cfg.OVNSBAddr)
	}
	if cfg.ResourcePrefix != "case-" {
		t.Fatalf("ResourcePrefix = %q", cfg.ResourcePrefix)
	}
	if cfg.BridgeName != "br-case" {
		t.Fatalf("BridgeName = %q", cfg.BridgeName)
	}
	if !cfg.AllowBRInt {
		t.Fatal("AllowBRInt = false, want true")
	}
	if got := cfg.MissingEndpoints(); len(got) != 0 {
		t.Fatalf("MissingEndpoints() = %v, want none", got)
	}
}

func TestIntegrationConfigRejectsBRIntUnlessExplicit(t *testing.T) {
	cfg := IntegrationConfig{
		OVSAddr:        "tcp:127.0.0.1:6640",
		OVNNBAddr:      "tcp:127.0.0.1:6641",
		OVNSBAddr:      "tcp:127.0.0.1:6642",
		ResourcePrefix: DefaultIntegrationResourcePrefix,
		BridgeName:     "br-int",
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() succeeded, want br-int safety error")
	}

	cfg.AllowBRInt = true
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
}

func TestIntegrationConfigRejectsNonTCPEndpoints(t *testing.T) {
	cfg := IntegrationConfig{
		OVSAddr:        "unix:/var/run/openvswitch/db.sock",
		OVNNBAddr:      "tcp:127.0.0.1:6641",
		OVNSBAddr:      "tcp:127.0.0.1:6642",
		ResourcePrefix: DefaultIntegrationResourcePrefix,
		BridgeName:     DefaultIntegrationBridge,
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() succeeded with unix endpoint, want error")
	}

	cfg.OVSAddr = "ssl:127.0.0.1:6640"
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() succeeded with ssl endpoint, want error")
	}

	cfg.OVSAddr = "tcp:127.0.0.1:6640"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() with tcp endpoints = %v", err)
	}
}

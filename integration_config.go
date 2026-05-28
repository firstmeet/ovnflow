package ovnflow

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

const (
	// EnvOVSAddr points at the Open_vSwitch OVSDB endpoint, for example
	// tcp:172.27.192.120:6640.
	EnvOVSAddr = "OVNFLOW_OVS_ADDR"

	// EnvOVNNBAddr points at the OVN Northbound OVSDB endpoint, for example
	// tcp:172.27.192.120:6641.
	EnvOVNNBAddr = "OVNFLOW_OVN_NB_ADDR"

	// EnvOVNSBAddr points at the OVN Southbound OVSDB endpoint, for example
	// tcp:172.27.192.120:6642.
	EnvOVNSBAddr = "OVNFLOW_OVN_SB_ADDR"

	// EnvOpenFlowAddr points at an OpenFlow controller endpoint exposed by OVS,
	// for example tcp:127.0.0.1:6653.
	EnvOpenFlowAddr = "OVNFLOW_OPENFLOW_ADDR"

	// EnvTestResourcePrefix controls the prefix used for all integration-test
	// rows created in OVN and OVS.
	EnvTestResourcePrefix = "OVNFLOW_TEST_PREFIX"

	// EnvTestBridge controls the dedicated OVS bridge used by integration tests.
	EnvTestBridge = "OVNFLOW_TEST_BRIDGE"

	// EnvAllowBRInt must be set to a truthy value before integration tests are
	// allowed to target br-int directly.
	EnvAllowBRInt = "OVNFLOW_ALLOW_BR_INT"

	// EnvRequireIntegration turns endpoint/SDK connection skips into failures.
	// CI and release gates should enable it so regressions cannot hide behind
	// skipped integration tests.
	EnvRequireIntegration = "OVNFLOW_REQUIRE_INTEGRATION"
)

const (
	DefaultIntegrationResourcePrefix = "ovnflow-it-"
	DefaultIntegrationBridge         = "br-ovnflow-it"
)

// IntegrationConfig contains the environment-driven settings shared by all
// integration tests. It deliberately avoids hard-coded WSL addresses because
// WSL IP addresses can change after restart.
type IntegrationConfig struct {
	OVSAddr        string
	OVNNBAddr      string
	OVNSBAddr      string
	OpenFlowAddr   string
	ResourcePrefix string
	BridgeName     string
	AllowBRInt     bool
	Require        bool
}

// LoadIntegrationConfigFromEnv reads the Windows + WSL integration-test
// configuration from environment variables.
func LoadIntegrationConfigFromEnv() IntegrationConfig {
	return IntegrationConfig{
		OVSAddr:        strings.TrimSpace(os.Getenv(EnvOVSAddr)),
		OVNNBAddr:      strings.TrimSpace(os.Getenv(EnvOVNNBAddr)),
		OVNSBAddr:      strings.TrimSpace(os.Getenv(EnvOVNSBAddr)),
		OpenFlowAddr:   strings.TrimSpace(os.Getenv(EnvOpenFlowAddr)),
		ResourcePrefix: envOrDefault(EnvTestResourcePrefix, DefaultIntegrationResourcePrefix),
		BridgeName:     envOrDefault(EnvTestBridge, DefaultIntegrationBridge),
		AllowBRInt:     parseEnvBool(os.Getenv(EnvAllowBRInt)),
		Require:        parseEnvBool(os.Getenv(EnvRequireIntegration)),
	}
}

// MissingEndpoints returns the required endpoint environment variables that are
// not configured. Integration tests should skip when this list is non-empty.
func (c IntegrationConfig) MissingEndpoints() []string {
	var missing []string
	if strings.TrimSpace(c.OVSAddr) == "" {
		missing = append(missing, EnvOVSAddr)
	}
	if strings.TrimSpace(c.OVNNBAddr) == "" {
		missing = append(missing, EnvOVNNBAddr)
	}
	if strings.TrimSpace(c.OVNSBAddr) == "" {
		missing = append(missing, EnvOVNSBAddr)
	}
	return missing
}

// ShouldRequireEndpoints reports whether integration endpoint failures should
// fail the test process instead of being reported as skips.
func (c IntegrationConfig) ShouldRequireEndpoints() bool {
	return c.Require || parseEnvBool(os.Getenv("CI"))
}

// Validate rejects configuration that could accidentally target production-like
// OVS resources.
func (c IntegrationConfig) Validate() error {
	if strings.TrimSpace(c.ResourcePrefix) == "" {
		return errors.New("integration resource prefix must not be empty")
	}
	if strings.TrimSpace(c.BridgeName) == "" {
		return errors.New("integration bridge name must not be empty")
	}
	if strings.EqualFold(strings.TrimSpace(c.BridgeName), "br-int") && !c.AllowBRInt {
		return fmt.Errorf("%s=br-int requires %s=1", EnvTestBridge, EnvAllowBRInt)
	}
	for name, endpoint := range map[string]string{
		EnvOVSAddr:      c.OVSAddr,
		EnvOVNNBAddr:    c.OVNNBAddr,
		EnvOVNSBAddr:    c.OVNSBAddr,
		EnvOpenFlowAddr: c.OpenFlowAddr,
	} {
		if strings.TrimSpace(endpoint) != "" && !strings.HasPrefix(strings.TrimSpace(endpoint), "tcp:") {
			return fmt.Errorf("%s must use a tcp: endpoint for Windows/WSL integration tests", name)
		}
	}
	return nil
}

func envOrDefault(name, fallback string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	return value
}

func parseEnvBool(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "t", "true", "y", "yes", "on":
		return true
	default:
		return false
	}
}

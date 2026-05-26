//go:build linux && integration

package linuxrouter

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/firstmeet/ovnflow"
)

func TestIntegrationLinuxRouterNamespaceLifecycle(t *testing.T) {
	if !ovnflow.EnvGateEnabled(os.Getenv(ovnflow.EnvLinuxRouterChecks)) {
		t.Skip(ovnflow.EnvLinuxRouterChecks + " not enabled")
	}
	if os.Geteuid() != 0 {
		t.Skip(ovnflow.EnvLinuxRouterChecks + " requires root or equivalent CAP_NET_ADMIN")
	}
	requireCommand(t, "ip")
	backend := os.Getenv(ovnflow.EnvLinuxRouterNATBackend)
	if !ovnflow.ValidNATBackend(backend) {
		t.Fatalf("invalid %s value %q", ovnflow.EnvLinuxRouterNATBackend, backend)
	}
	ns := "ovnflow-it-" + time.Now().UTC().Format("20060102150405")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_ = (SystemExecutor{}).Run(cleanupCtx, Command{Program: "ip", Args: []string{"netns", "delete", ns}, IgnoreNotFound: true})
	})
	client := NewClient(SystemExecutor{}, LinuxRenderer{NATBackend: backend})
	err := client.Router("edge").Apply(ctx, Router{
		Name: "edge",
		Spec: Spec{
			Namespace:  ns,
			Interfaces: []Interface{{Name: "lo"}},
		},
	})
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if err := exec.CommandContext(ctx, "ip", "netns", "exec", ns, "ip", "link", "show", "lo").Run(); err != nil {
		t.Fatalf("namespace %s did not contain loopback: %v", ns, err)
	}
}

func requireCommand(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Skipf("%s not available: %v", name, err)
	}
}

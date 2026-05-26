//go:build linux

package linuxrouter

import (
	"os"

	"github.com/firstmeet/ovnflow/v2"
)

func NewPlatformClient() PlatformClient {
	backend := os.Getenv(ovnflow.EnvLinuxRouterNATBackend)
	return NewObservedClient(SystemExecutor{}, LinuxRenderer{NATBackend: backend}, LinuxObserver{NATBackend: backend})
}

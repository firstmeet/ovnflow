//go:build linux

package linuxrouter

import (
	"os"

	"github.com/firstmeet/ovnflow"
)

func NewPlatformClient() PlatformClient {
	return NewClient(SystemExecutor{}, LinuxRenderer{NATBackend: os.Getenv(ovnflow.EnvLinuxRouterNATBackend)})
}

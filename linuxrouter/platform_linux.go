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

func NewPlatformClientWithOVS(ovs *ovnflow.OVSClient) PlatformClient {
	backend := os.Getenv(ovnflow.EnvLinuxRouterNATBackend)
	return newObservedClient(
		SystemExecutor{},
		LinuxRenderer{NATBackend: backend, OVSDBManaged: true},
		LinuxObserver{NATBackend: backend},
		sdkRouterOVSManager{ovs: sdkOVSClient{ovs: ovs}},
	)
}

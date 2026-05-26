package linuxrouter

import "github.com/firstmeet/ovnflow"

func unsupported(name string) error {
	return &ovnflow.Error{
		Kind:      ovnflow.ErrorUnsupported,
		Operation: "linuxrouter",
		Object:    name,
		Message:   "LinuxRouter is only supported on linux builds",
	}
}

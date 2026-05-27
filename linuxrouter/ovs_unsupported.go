//go:build !linux

package linuxrouter

import "context"

type routerOVSManager interface {
	EnsureRouter(context.Context, Router) error
}

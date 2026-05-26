//go:build !linux

package linuxrouter

import "context"

type UnsupportedClient struct{}

func NewPlatformClient() PlatformClient {
	return &UnsupportedClient{}
}

func (UnsupportedClient) Router(name string) RouterRef {
	return &UnsupportedRef{name: name}
}

type UnsupportedRef struct {
	name string
}

func (r *UnsupportedRef) Get(context.Context) (Router, error) {
	return Router{}, unsupported(r.name)
}

func (r *UnsupportedRef) Apply(context.Context, Router) error {
	return unsupported(r.name)
}

func (r *UnsupportedRef) Patch(context.Context, Patch) (Router, error) {
	return Router{}, unsupported(r.name)
}

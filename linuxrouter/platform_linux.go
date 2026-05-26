//go:build linux

package linuxrouter

func NewPlatformClient() PlatformClient {
	return NewClient(nil, nil)
}

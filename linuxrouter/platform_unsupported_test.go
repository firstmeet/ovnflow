//go:build !linux

package linuxrouter

import (
	"context"
	"testing"

	"github.com/firstmeet/ovnflow"
)

func TestPlatformClientUnsupportedOnNonLinux(t *testing.T) {
	err := NewPlatformClient().Router("edge").Apply(context.Background(), Router{Name: "edge"})
	if !ovnflow.IsKind(err, ovnflow.ErrorUnsupported) {
		t.Fatalf("error kind = %q for %v, want unsupported", ovnflow.KindOf(err), err)
	}
}

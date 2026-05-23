package ovnflow

import (
	"context"
	"testing"
)

func TestLogicalSwitchBuilderValidationFailuresStayLocal(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name string
		run  func() error
	}{
		{
			name: "blank logical switch",
			run: func() error {
				return (&NBClient{}).LogicalSwitch(" ").Create().Execute(ctx)
			},
		},
		{
			name: "invalid subnet",
			run: func() error {
				return (&NBClient{}).LogicalSwitch("ls").Create().WithSubnet("not-cidr").Execute(ctx)
			},
		},
		{
			name: "blank switch external id key",
			run: func() error {
				return (&NBClient{}).LogicalSwitch("ls").Create().WithExternalID(" ", "value").Execute(ctx)
			},
		},
		{
			name: "blank port name",
			run: func() error {
				return (&NBClient{}).LogicalSwitch("ls").Create().AddPort(" ").Execute(ctx)
			},
		},
		{
			name: "invalid port mac",
			run: func() error {
				return (&NBClient{}).LogicalSwitch("ls").Create().AddPort("lsp").WithMac("bad-mac").Execute(ctx)
			},
		},
		{
			name: "invalid port ip",
			run: func() error {
				return (&NBClient{}).LogicalSwitch("ls").Create().AddPort("lsp").WithIP("999.999.999.999").Execute(ctx)
			},
		},
		{
			name: "blank port external id key",
			run: func() error {
				return (&NBClient{}).LogicalSwitch("ls").Create().AddPort("lsp").WithExternalID("", "value").Execute(ctx)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.run()
			if !IsKind(err, ErrorValidation) {
				t.Fatalf("error kind = %q for %v, want %q", KindOf(err), err, ErrorValidation)
			}
		})
	}
}

func TestOVSBridgeBuilderValidationFailuresStayLocal(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name string
		run  func() error
	}{
		{
			name: "blank bridge",
			run: func() error {
				return (&OVSClient{}).Bridge(" ").Ensure().Execute(ctx)
			},
		},
		{
			name: "blank bridge external id key",
			run: func() error {
				return (&OVSClient{}).Bridge("br-test").Ensure().WithExternalID("", "value").Execute(ctx)
			},
		},
		{
			name: "blank port",
			run: func() error {
				return (&OVSClient{}).Bridge("br-test").AddPort(" ").Execute(ctx)
			},
		},
		{
			name: "blank interface name",
			run: func() error {
				return (&OVSClient{}).Bridge("br-test").AddPort("p0").WithInterfaceName(" ").Execute(ctx)
			},
		},
		{
			name: "blank port external id key",
			run: func() error {
				return (&OVSClient{}).Bridge("br-test").AddPort("p0").WithExternalID(" ", "value").Execute(ctx)
			},
		},
		{
			name: "blank delete port",
			run: func() error {
				return (&OVSClient{}).Bridge("br-test").DeletePort(" ").Execute(ctx)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.run()
			if !IsKind(err, ErrorValidation) {
				t.Fatalf("error kind = %q for %v, want %q", KindOf(err), err, ErrorValidation)
			}
		})
	}
}

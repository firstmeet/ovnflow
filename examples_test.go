package ovnflow_test

import (
	"context"

	"github.com/ovnflow/ovnflow"
)

func Example_logicalSwitchCreate() {
	ctx := context.Background()
	client, err := ovnflow.Connect(ctx, ovnflow.Config{
		OVSAddr:   "tcp:127.0.0.1:6640",
		OVNNBAddr: "tcp:127.0.0.1:6641",
		OVNSBAddr: "tcp:127.0.0.1:6642",
	})
	if err != nil {
		return
	}
	defer client.Close()

	_ = client.OVN().NB().
		LogicalSwitch("ls-web").
		Create().
		WithSubnet("192.168.1.0/24").
		AddPort("port-vm1").
		WithMac("00:11:22:33:44:55").
		WithIP("192.168.1.10").
		Execute(ctx)
}

func Example_localOVSAddInternalPort() {
	ctx := context.Background()
	client, err := ovnflow.Connect(ctx, ovnflow.ConfigFromEnv())
	if err != nil {
		return
	}
	defer client.Close()

	_ = client.LocalOVS().
		Bridge("br-ovnflow-it").
		AddPort("vnet0").
		WithInterfaceType("internal").
		WithExternalID("vm-id", "uuid-1234").
		Execute(ctx)
}

func Example_ovnSBWatchPortBindings() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client, err := ovnflow.Connect(ctx, ovnflow.ConfigFromEnv())
	if err != nil {
		return
	}
	defer client.Close()

	events, errs := client.OVN().SB().WatchPortBindings(ctx)
	select {
	case <-events:
	case <-errs:
	}
}

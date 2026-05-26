package ovnflow_test

import (
	"context"

	"github.com/firstmeet/ovnflow"
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

func Example_v2IntentLifecycle() {
	ctx := context.Background()
	client, err := ovnflow.Connect(ctx, ovnflow.ConfigFromEnv())
	if err != nil {
		return
	}
	defer client.Close()

	nb := client.OVN().NB()
	_ = nb.VirtualNetwork("net-web").
		Ensure().
		WithCIDR("10.20.0.0/24").
		WithGateway("10.20.0.1").
		WithOwner("project", "alpha").
		WithLabel("env", "prod").
		WithDNS("net-web-dns", func(d *ovnflow.LogicalSwitchDNSBuilder) {
			d.AddRecord("api.service", "10.20.0.10", "10.20.0.11")
		}).
		Execute(ctx)

	_ = nb.WorkloadAttachment("vm-1001-eth0").
		Ensure().
		OnNetwork("net-web").
		WithWorkload("vm-1001").
		WithInterface("eth0").
		WithMAC("00:16:3e:11:22:33").
		WithIP("10.20.0.10").
		WithOwner("project", "alpha").
		Execute(ctx)

	_ = nb.SecurityPolicy("pg-web").
		Ensure().
		ForSubject("pg-web").
		WithOwner("project", "alpha").
		AddRule(ovnflow.SecurityRule{
			Name:     "allow-http",
			Action:   "allow",
			Protocol: "tcp",
			CIDRs:    []string{"10.20.0.0/24"},
			Ports:    []int{80},
		}).
		Execute(ctx)

	_, _ = nb.VirtualNetwork("net-web").Get(ctx)
	_ = nb.WorkloadAttachment("vm-1001-eth0").Delete(ctx)
	_ = nb.LogicalSwitchDNS("net-web-dns").Delete(ctx)
	_ = nb.SecurityPolicy("pg-web").Delete(ctx)
	_ = nb.VirtualNetwork("net-web").Delete(ctx)
}

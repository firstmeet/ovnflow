package main

import (
	"context"
	"log"

	"github.com/ovnflow/ovnflow"
)

func main() {
	ctx := context.Background()
	client, err := ovnflow.Connect(ctx, ovnflow.ConfigFromEnv())
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	err = client.OVN().NB().
		LogicalSwitch("ls-web").
		Ensure().
		WithSubnet("192.168.1.0/24").
		WithExternalID("app", "web").
		AddPort("port-vm1").
		WithMac("00:11:22:33:44:55").
		WithIP("192.168.1.10").
		Execute(ctx)
	if err != nil {
		log.Fatal(err)
	}
}

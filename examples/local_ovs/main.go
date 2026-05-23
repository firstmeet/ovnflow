package main

import (
	"context"
	"log"

	"github.com/firstmeet/ovnflow"
)

func main() {
	ctx := context.Background()
	client, err := ovnflow.Connect(ctx, ovnflow.ConfigFromEnv())
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	err = client.LocalOVS().
		Bridge("br-ovnflow-it").
		AddPort("vnet0").
		WithInterfaceType("internal").
		WithExternalID("vm-id", "uuid-1234").
		Execute(ctx)
	if err != nil {
		log.Fatal(err)
	}
}

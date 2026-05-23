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

	err = client.LocalOVS().
		Bridge("br-ovnflow-it").
		Ensure().
		WithMirror("mirror-vnet0", func(mirror *ovnflow.TableBuilder) {
			mirror.WithMirrorSelectAll().
				WithExternalID("vm-id", "uuid-1234")
		}).
		WithNetFlow("nf-vnet0", func(netflow *ovnflow.TableBuilder) {
			netflow.WithSamplingTarget("127.0.0.1:2055").
				WithExternalID("vm-id", "uuid-1234")
		}).
		WithIPFIX("ipfix-vnet0", func(ipfix *ovnflow.TableBuilder) {
			ipfix.WithSamplingTarget("127.0.0.1:4739")
		}).
		Execute(ctx)
	if err != nil {
		log.Fatal(err)
	}
}

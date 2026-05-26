package main

import (
	"context"
	"fmt"
	"log"

	"github.com/firstmeet/ovnflow/v2"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client, err := ovnflow.Connect(ctx, ovnflow.ConfigFromEnv())
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	events, errs := client.OVN().SB().WatchPortBindings(ctx)
	for {
		select {
		case event, ok := <-events:
			if !ok {
				return
			}
			if event.New != nil {
				fmt.Printf("%s %s\n", event.Type, event.New.LogicalPort)
			}
		case err := <-errs:
			if err != nil {
				log.Fatal(err)
			}
		}
	}
}

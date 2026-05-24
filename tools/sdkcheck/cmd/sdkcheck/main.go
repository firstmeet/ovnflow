package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/firstmeet/ovnflow-sdkcheck"
)

func main() {
	opts := sdkcheck.OptionsFromEnv()
	flag.StringVar(&opts.OVSAddr, "ovs", opts.OVSAddr, "Open_vSwitch OVSDB endpoint")
	flag.StringVar(&opts.OVNNBAddr, "nb", opts.OVNNBAddr, "OVN Northbound OVSDB endpoint")
	flag.StringVar(&opts.OVNSBAddr, "sb", opts.OVNSBAddr, "OVN Southbound OVSDB endpoint")
	flag.StringVar(&opts.Prefix, "prefix", opts.Prefix, "test resource prefix")
	flag.StringVar(&opts.Bridge, "bridge", opts.Bridge, "dedicated OVS bridge name")
	flag.DurationVar(&opts.Timeout, "timeout", opts.Timeout, "timeout per check step")
	flag.Parse()

	report := sdkcheck.Run(context.Background(), opts)
	failed := false
	for _, step := range report.Steps {
		status := "PASS"
		if step.Err != nil {
			status = "FAIL"
			failed = true
		}
		fmt.Printf("%-4s %-34s %s\n", status, step.Name, step.Duration.Round(time.Millisecond))
		if step.Err != nil {
			fmt.Printf("     %v\n", step.Err)
		}
	}
	if failed {
		os.Exit(1)
	}
}

package sdkcheck

import (
	"context"
	"testing"
)

func TestSDKInterfaces(t *testing.T) {
	opts := OptionsFromEnv()
	if opts.OVSAddr == "" || opts.OVNNBAddr == "" || opts.OVNSBAddr == "" {
		t.Skip("set OVNFLOW_OVS_ADDR, OVNFLOW_OVN_NB_ADDR, and OVNFLOW_OVN_SB_ADDR to run SDK interface checks")
	}
	report := Run(context.Background(), opts)
	for _, step := range report.Steps {
		step := step
		t.Run(step.Name, func(t *testing.T) {
			if step.Err != nil {
				t.Fatal(step.Err)
			}
		})
	}
	if err := report.Err(); err != nil {
		t.Fatal(err)
	}
}

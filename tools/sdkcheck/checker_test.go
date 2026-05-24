package sdkcheck

import (
	"context"
	"testing"
)

func TestSDKInterfaces(t *testing.T) {
	report := Run(context.Background(), OptionsFromEnv())
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

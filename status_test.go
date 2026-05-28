package ovnflow

import (
	"context"
	"testing"
)

func TestNetworkStatusNilClientReturnsTypedDegraded(t *testing.T) {
	status, err := ((*Client)(nil)).NetworkStatus(context.Background(), "net-a")
	if err != nil {
		t.Fatalf("NetworkStatus returned error: %v", err)
	}
	if status == nil || status.State != ResourceStatusDegraded || !status.Degraded || !status.ReadOnly {
		t.Fatalf("status = %#v, want degraded read-only status", status)
	}
	if status.Network != nil {
		t.Fatalf("network = %#v, want nil", status.Network)
	}
	if status.Doctor == nil || status.Ownership == nil {
		t.Fatalf("diagnostics not populated: %#v", status)
	}
	if !statusHasFinding(status.Findings, "backend_unavailable") {
		t.Fatalf("findings = %#v, want backend_unavailable", status.Findings)
	}
}

func TestProviderAndWorkloadStatusNilBackendsDoNotPanic(t *testing.T) {
	client := &Client{}
	provider, err := client.ProviderNetworkStatus(context.Background(), "public")
	if err != nil {
		t.Fatalf("ProviderNetworkStatus returned error: %v", err)
	}
	if provider.State != ResourceStatusDegraded || !provider.Degraded || provider.Network != nil {
		t.Fatalf("provider status = %#v, want degraded without network", provider)
	}
	if provider.Doctor == nil || provider.Ownership == nil {
		t.Fatalf("provider diagnostics not populated: %#v", provider)
	}

	workload, err := client.WorkloadPath(context.Background(), "att-a")
	if err != nil {
		t.Fatalf("WorkloadPath returned error: %v", err)
	}
	if workload.State != ResourceStatusDegraded || !workload.Degraded || workload.Attachment != nil {
		t.Fatalf("workload status = %#v, want degraded without attachment", workload)
	}
	if workload.Doctor == nil || workload.Ownership == nil {
		t.Fatalf("workload diagnostics not populated: %#v", workload)
	}
}

func statusHasFinding(findings []StatusFinding, code string) bool {
	for _, finding := range findings {
		if finding.Code == code {
			return true
		}
	}
	return false
}

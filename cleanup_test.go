package ovnflow

import (
	"context"
	"testing"
)

func TestCleanupPlanFromAuditClassifiesOwnedAndRiskyCandidates(t *testing.T) {
	report := cleanupPlanAuditReport()

	plan := NewCleanupPlanFromAudit(report, CleanupPlanOptions{})
	if !plan.ReadOnly {
		t.Fatalf("cleanup plan must be read-only")
	}
	if len(plan.Candidates) != 3 {
		t.Fatalf("candidates = %#v, want 3", plan.Candidates)
	}

	owned := cleanupCandidateByName(plan.Candidates, "net-a")
	if owned == nil || !owned.Owned || !owned.MarkerComplete || owned.Risk != CleanupRiskLow || owned.RecommendedAction != CleanupActionDeleteOwned {
		t.Fatalf("owned candidate = %#v", owned)
	}
	incomplete := cleanupCandidateByName(plan.Candidates, "att-a")
	if incomplete == nil || !incomplete.Owned || incomplete.MarkerComplete || incomplete.Risk != CleanupRiskHigh || incomplete.RecommendedAction != CleanupActionReview {
		t.Fatalf("incomplete candidate = %#v", incomplete)
	}
	unowned := cleanupCandidateByName(plan.Candidates, "legacy")
	if unowned == nil || unowned.Owned || unowned.MarkerComplete || unowned.RecommendedAction != CleanupActionReview {
		t.Fatalf("unowned candidate = %#v", unowned)
	}

	executable := plan.ExecutableCandidates()
	if len(executable) != 1 || executable[0].Name != "net-a" {
		t.Fatalf("executable = %#v, want only fully owned net-a", executable)
	}
}

func TestAdoptPlanFromAuditClassifiesImportAndMarkerCompletion(t *testing.T) {
	report := cleanupPlanAuditReport()

	plan := NewAdoptPlanFromAudit(report, AdoptPlanOptions{})
	if !plan.ReadOnly || len(plan.Candidates) != 3 {
		t.Fatalf("adopt plan = %#v", plan)
	}
	if got := adoptCandidateByName(plan.Candidates, "net-a"); got == nil || got.RecommendedAction != AdoptActionImport || got.Risk != CleanupRiskLow {
		t.Fatalf("net-a adopt candidate = %#v", got)
	}
	if got := adoptCandidateByName(plan.Candidates, "att-a"); got == nil || got.RecommendedAction != AdoptActionCompleteMarker || got.Risk != CleanupRiskMedium {
		t.Fatalf("att-a adopt candidate = %#v", got)
	}
	if got := adoptCandidateByName(plan.Candidates, "legacy"); got == nil || got.RecommendedAction != CleanupActionReview || got.Risk != CleanupRiskHigh {
		t.Fatalf("legacy adopt candidate = %#v", got)
	}

	executable := plan.ExecutableCandidates()
	if len(executable) != 1 || executable[0].Name != "net-a" {
		t.Fatalf("executable = %#v, want only fully owned net-a", executable)
	}
}

func TestClientCleanupPlanNilBackendReturnsDegradedPlan(t *testing.T) {
	plan, err := ((*Client)(nil)).CleanupPlan(context.Background(), CleanupPlanOptions{})
	if err != nil {
		t.Fatalf("CleanupPlan returned error: %v", err)
	}
	if !plan.ReadOnly || len(plan.Candidates) != 0 {
		t.Fatalf("plan = %#v, want empty read-only plan", plan)
	}
	if !cleanupPlanHasFinding(plan, "client_unavailable") {
		t.Fatalf("findings = %#v, want client_unavailable", plan.Findings)
	}
}

func cleanupPlanAuditReport() *OwnershipAuditReport {
	return &OwnershipAuditReport{
		Resources: []OwnershipAuditResource{
			{
				Database:    dbOVNNorthbound,
				Table:       tableLogicalSwitch,
				UUID:        "ls-uuid",
				Name:        "net-a",
				Kind:        "VirtualNetwork",
				Owner:       OwnerRef{Kind: "project", Name: "alpha"},
				ExternalIDs: testOwnedExternalIDs("VirtualNetwork", "net-a"),
			},
			{
				Database: dbOVNNorthbound,
				Table:    tableLogicalSwitchPort,
				UUID:     "lsp-uuid",
				Name:     "att-a",
				Kind:     "WorkloadAttachment",
				Owner:    OwnerRef{Kind: "project"},
				ExternalIDs: map[string]string{
					ExternalIDManagedByKey:  "ovnflow",
					ExternalIDAPIVersionKey: "v2",
					ExternalIDKindKey:       "WorkloadAttachment",
					ExternalIDNameKey:       "att-a",
					ExternalIDOwnerKindKey:  "project",
				},
			},
			{
				Database: dbOVNNorthbound,
				Table:    tableLogicalSwitch,
				UUID:     "legacy-uuid",
				Name:     "legacy",
				Kind:     "VirtualNetwork",
				ExternalIDs: map[string]string{
					ExternalIDKindKey: "VirtualNetwork",
					ExternalIDNameKey: "legacy",
				},
			},
		},
		Findings: []OwnershipAuditFinding{{Severity: DoctorWarning, Code: "owned_resource_missing_owner", Message: "owned resource is missing a complete owner marker"}},
	}
}

func cleanupCandidateByName(candidates []CleanupCandidate, name string) *CleanupCandidate {
	for i := range candidates {
		if candidates[i].Name == name {
			return &candidates[i]
		}
	}
	return nil
}

func adoptCandidateByName(candidates []AdoptCandidate, name string) *AdoptCandidate {
	for i := range candidates {
		if candidates[i].Name == name {
			return &candidates[i]
		}
	}
	return nil
}

func cleanupPlanHasFinding(plan *CleanupPlan, code string) bool {
	for _, finding := range plan.Findings {
		if finding.Code == code {
			return true
		}
	}
	return false
}

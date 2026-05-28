package ovnflow

import "context"

type CleanupAction string

const (
	CleanupActionKeep         CleanupAction = "keep"
	CleanupActionReview       CleanupAction = "review"
	CleanupActionDeleteOwned  CleanupAction = "delete_owned"
	AdoptActionImport         CleanupAction = "import"
	AdoptActionCompleteMarker CleanupAction = "complete_marker"
)

type CleanupRisk string

const (
	CleanupRiskLow    CleanupRisk = "low"
	CleanupRiskMedium CleanupRisk = "medium"
	CleanupRiskHigh   CleanupRisk = "high"
)

type CleanupPlanOptions struct {
	Audit *OwnershipAuditReport
	Owner OwnerRef
	Kinds []string
	Names []string
}

type AdoptPlanOptions struct {
	Audit *OwnershipAuditReport
	Owner OwnerRef
	Kinds []string
	Names []string
}

type CleanupCandidate struct {
	Database          string                 `json:"database"`
	Table             string                 `json:"table"`
	UUID              string                 `json:"uuid,omitempty"`
	Kind              string                 `json:"kind,omitempty"`
	Name              string                 `json:"name,omitempty"`
	Owner             OwnerRef               `json:"owner"`
	Owned             bool                   `json:"owned"`
	MarkerComplete    bool                   `json:"marker_complete"`
	Risk              CleanupRisk            `json:"risk"`
	RecommendedAction CleanupAction          `json:"recommended_action"`
	Reasons           []string               `json:"reasons,omitempty"`
	Resource          OwnershipAuditResource `json:"resource"`
}

type CleanupPlan struct {
	Candidates []CleanupCandidate      `json:"candidates"`
	Findings   []OwnershipAuditFinding `json:"findings,omitempty"`
	ReadOnly   bool                    `json:"read_only"`
}

type AdoptCandidate struct {
	Database          string                 `json:"database"`
	Table             string                 `json:"table"`
	UUID              string                 `json:"uuid,omitempty"`
	Kind              string                 `json:"kind,omitempty"`
	Name              string                 `json:"name,omitempty"`
	Owner             OwnerRef               `json:"owner"`
	Owned             bool                   `json:"owned"`
	MarkerComplete    bool                   `json:"marker_complete"`
	Risk              CleanupRisk            `json:"risk"`
	RecommendedAction CleanupAction          `json:"recommended_action"`
	Reasons           []string               `json:"reasons,omitempty"`
	Resource          OwnershipAuditResource `json:"resource"`
}

type AdoptPlan struct {
	Candidates []AdoptCandidate        `json:"candidates"`
	Findings   []OwnershipAuditFinding `json:"findings,omitempty"`
	ReadOnly   bool                    `json:"read_only"`
}

func (c *Client) CleanupPlan(ctx context.Context, opts CleanupPlanOptions) (*CleanupPlan, error) {
	audit := opts.Audit
	if audit == nil {
		var err error
		audit, err = (&Diagnostics{client: c}).AuditOwnership(ctx, OwnershipAuditOptions{Owner: opts.Owner, Kinds: opts.Kinds, Names: opts.Names})
		if err != nil {
			return nil, err
		}
	}
	return NewCleanupPlanFromAudit(audit, opts), nil
}

func (c *Client) AdoptPlan(ctx context.Context, opts AdoptPlanOptions) (*AdoptPlan, error) {
	audit := opts.Audit
	if audit == nil {
		var err error
		audit, err = (&Diagnostics{client: c}).AuditOwnership(ctx, OwnershipAuditOptions{Owner: opts.Owner, Kinds: opts.Kinds, Names: opts.Names})
		if err != nil {
			return nil, err
		}
	}
	return NewAdoptPlanFromAudit(audit, opts), nil
}

func NewCleanupPlanFromAudit(audit *OwnershipAuditReport, opts CleanupPlanOptions) *CleanupPlan {
	plan := &CleanupPlan{ReadOnly: true}
	if audit == nil {
		plan.Findings = append(plan.Findings, OwnershipAuditFinding{
			Severity: DoctorWarning,
			Code:     "audit_unavailable",
			Message:  "ownership audit report is nil",
		})
		return plan
	}
	plan.Findings = append(plan.Findings, audit.Findings...)
	for _, resource := range audit.Resources {
		if !cleanupResourceInScope(resource, opts.Owner, opts.Kinds, opts.Names) {
			continue
		}
		plan.Candidates = append(plan.Candidates, cleanupCandidateFromResource(resource))
	}
	return plan
}

func NewAdoptPlanFromAudit(audit *OwnershipAuditReport, opts AdoptPlanOptions) *AdoptPlan {
	plan := &AdoptPlan{ReadOnly: true}
	if audit == nil {
		plan.Findings = append(plan.Findings, OwnershipAuditFinding{
			Severity: DoctorWarning,
			Code:     "audit_unavailable",
			Message:  "ownership audit report is nil",
		})
		return plan
	}
	plan.Findings = append(plan.Findings, audit.Findings...)
	for _, resource := range audit.Resources {
		if !cleanupResourceInScope(resource, opts.Owner, opts.Kinds, opts.Names) {
			continue
		}
		plan.Candidates = append(plan.Candidates, adoptCandidateFromResource(resource))
	}
	return plan
}

func (p *CleanupPlan) ExecutableCandidates() []CleanupCandidate {
	if p == nil {
		return nil
	}
	out := []CleanupCandidate{}
	for _, candidate := range p.Candidates {
		if candidate.Owned && candidate.MarkerComplete && candidate.RecommendedAction == CleanupActionDeleteOwned {
			out = append(out, candidate)
		}
	}
	return out
}

func (p *AdoptPlan) ExecutableCandidates() []AdoptCandidate {
	if p == nil {
		return nil
	}
	out := []AdoptCandidate{}
	for _, candidate := range p.Candidates {
		if candidate.Owned && candidate.MarkerComplete && candidate.RecommendedAction == AdoptActionImport {
			out = append(out, candidate)
		}
	}
	return out
}

func cleanupCandidateFromResource(resource OwnershipAuditResource) CleanupCandidate {
	owned := resource.ExternalIDs[ExternalIDManagedByKey] == "ovnflow"
	complete := completeOwnedMarker(resource.ExternalIDs, resource.Kind, resource.Name)
	candidate := CleanupCandidate{
		Database:       resource.Database,
		Table:          resource.Table,
		UUID:           resource.UUID,
		Kind:           resource.Kind,
		Name:           resource.Name,
		Owner:          resource.Owner,
		Owned:          owned,
		MarkerComplete: complete,
		Resource:       resource,
	}
	if owned && complete {
		candidate.Risk = CleanupRiskLow
		candidate.RecommendedAction = CleanupActionDeleteOwned
		candidate.Reasons = append(candidate.Reasons, "owned v2 marker is complete")
		return candidate
	}
	candidate.Risk = CleanupRiskHigh
	candidate.RecommendedAction = CleanupActionReview
	candidate.Reasons = append(candidate.Reasons, cleanupMarkerReasons(resource.ExternalIDs, resource.Kind, resource.Name)...)
	return candidate
}

func adoptCandidateFromResource(resource OwnershipAuditResource) AdoptCandidate {
	owned := resource.ExternalIDs[ExternalIDManagedByKey] == "ovnflow"
	complete := completeOwnedMarker(resource.ExternalIDs, resource.Kind, resource.Name)
	candidate := AdoptCandidate{
		Database:       resource.Database,
		Table:          resource.Table,
		UUID:           resource.UUID,
		Kind:           resource.Kind,
		Name:           resource.Name,
		Owner:          resource.Owner,
		Owned:          owned,
		MarkerComplete: complete,
		Resource:       resource,
	}
	switch {
	case owned && complete:
		candidate.Risk = CleanupRiskLow
		candidate.RecommendedAction = AdoptActionImport
		candidate.Reasons = append(candidate.Reasons, "owned v2 marker is complete")
	case owned:
		candidate.Risk = CleanupRiskMedium
		candidate.RecommendedAction = AdoptActionCompleteMarker
		candidate.Reasons = append(candidate.Reasons, cleanupMarkerReasons(resource.ExternalIDs, resource.Kind, resource.Name)...)
	default:
		candidate.Risk = CleanupRiskHigh
		candidate.RecommendedAction = CleanupActionReview
		candidate.Reasons = append(candidate.Reasons, "resource is not marked as ovnflow-managed")
	}
	return candidate
}

func completeOwnedMarker(externalIDs map[string]string, kind, name string) bool {
	return externalIDs[ExternalIDManagedByKey] == "ovnflow" &&
		externalIDs[ExternalIDAPIVersionKey] == "v2" &&
		kind != "" &&
		name != "" &&
		externalIDs[ExternalIDKindKey] == kind &&
		externalIDs[ExternalIDNameKey] == name &&
		externalIDs[ExternalIDOwnerKindKey] != "" &&
		(externalIDs[ExternalIDOwnerNameKey] != "" || externalIDs[ExternalIDOwnerIDKey] != "")
}

func cleanupMarkerReasons(externalIDs map[string]string, kind, name string) []string {
	reasons := []string{}
	if externalIDs[ExternalIDManagedByKey] != "ovnflow" {
		reasons = append(reasons, "missing ovnflow managed-by marker")
	}
	if externalIDs[ExternalIDAPIVersionKey] != "v2" {
		reasons = append(reasons, "missing ovnflow v2 api-version marker")
	}
	if kind == "" || externalIDs[ExternalIDKindKey] != kind {
		reasons = append(reasons, "missing or mismatched kind marker")
	}
	if name == "" || externalIDs[ExternalIDNameKey] != name {
		reasons = append(reasons, "missing or mismatched name marker")
	}
	if externalIDs[ExternalIDOwnerKindKey] == "" || (externalIDs[ExternalIDOwnerNameKey] == "" && externalIDs[ExternalIDOwnerIDKey] == "") {
		reasons = append(reasons, "missing complete owner marker")
	}
	return reasons
}

func cleanupResourceInScope(resource OwnershipAuditResource, owner OwnerRef, kinds, names []string) bool {
	if ownerFilterSet(owner) && !OwnershipMatches(resource.ExternalIDs, owner) {
		return false
	}
	if len(kinds) > 0 && !stringInSet(resource.Kind, kinds) {
		return false
	}
	if len(names) > 0 && !stringInSet(resource.Name, names) && !stringInSet(resource.ExternalIDs[ExternalIDNameKey], names) {
		return false
	}
	return true
}

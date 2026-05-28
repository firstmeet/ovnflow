package ovnflow

import "context"

type ResourceStatusState string

const (
	ResourceStatusPresent  ResourceStatusState = "present"
	ResourceStatusMissing  ResourceStatusState = "missing"
	ResourceStatusDegraded ResourceStatusState = "degraded"
)

type StatusFinding struct {
	Severity  DoctorSeverity    `json:"severity"`
	Component string            `json:"component,omitempty"`
	Code      string            `json:"code"`
	Message   string            `json:"message"`
	Details   map[string]string `json:"details,omitempty"`
}

type NetworkStatus struct {
	Name      string                `json:"name"`
	State     ResourceStatusState   `json:"state"`
	Network   *VirtualNetwork       `json:"network,omitempty"`
	Doctor    *DoctorReport         `json:"doctor,omitempty"`
	Ownership *OwnershipAuditReport `json:"ownership,omitempty"`
	Findings  []StatusFinding       `json:"findings,omitempty"`
	Degraded  bool                  `json:"degraded"`
	ReadOnly  bool                  `json:"read_only"`
}

type ProviderNetworkStatus struct {
	Name      string                `json:"name"`
	State     ResourceStatusState   `json:"state"`
	Network   *ProviderNetwork      `json:"network,omitempty"`
	Doctor    *DoctorReport         `json:"doctor,omitempty"`
	Ownership *OwnershipAuditReport `json:"ownership,omitempty"`
	Findings  []StatusFinding       `json:"findings,omitempty"`
	Degraded  bool                  `json:"degraded"`
	ReadOnly  bool                  `json:"read_only"`
}

type WorkloadPathStatus struct {
	Name       string                `json:"name"`
	State      ResourceStatusState   `json:"state"`
	Attachment *WorkloadAttachment   `json:"attachment,omitempty"`
	Doctor     *DoctorReport         `json:"doctor,omitempty"`
	Ownership  *OwnershipAuditReport `json:"ownership,omitempty"`
	Findings   []StatusFinding       `json:"findings,omitempty"`
	Degraded   bool                  `json:"degraded"`
	ReadOnly   bool                  `json:"read_only"`
}

func (c *Client) NetworkStatus(ctx context.Context, name string) (*NetworkStatus, error) {
	out := &NetworkStatus{Name: name, State: ResourceStatusPresent, ReadOnly: true}
	if err := validateName("virtual network", name); err != nil {
		return nil, err
	}
	if c == nil || c.nb == nil {
		out.markDegraded("virtual_network", "backend_unavailable", "OVN Northbound client is unavailable", nil)
	} else {
		network, err := (&NBClient{db: c.nb}).VirtualNetwork(name).Get(ctx)
		out.Network = network
		out.recordGetError("virtual_network", err)
	}
	out.collectDiagnostics(ctx, c, OwnershipAuditOptions{Kinds: []string{"VirtualNetwork"}, Names: []string{name}})
	return out, nil
}

func (c *Client) ProviderNetworkStatus(ctx context.Context, name string) (*ProviderNetworkStatus, error) {
	out := &ProviderNetworkStatus{Name: name, State: ResourceStatusPresent, ReadOnly: true}
	if err := validateName("provider network", name); err != nil {
		return nil, err
	}
	if c == nil || c.nb == nil {
		out.markDegraded("provider_network", "backend_unavailable", "OVN Northbound client is unavailable", nil)
	} else {
		network, err := c.ProviderNetwork(name).Get(ctx)
		out.Network = network
		out.recordGetError("provider_network", err)
	}
	out.collectDiagnostics(ctx, c, OwnershipAuditOptions{Kinds: []string{"ProviderNetwork"}, Names: []string{name}})
	return out, nil
}

func (c *Client) WorkloadPath(ctx context.Context, name string) (*WorkloadPathStatus, error) {
	out := &WorkloadPathStatus{Name: name, State: ResourceStatusPresent, ReadOnly: true}
	if err := validateName("workload attachment", name); err != nil {
		return nil, err
	}
	if c == nil || c.nb == nil {
		out.markDegraded("workload_attachment", "backend_unavailable", "OVN Northbound client is unavailable", nil)
	} else {
		attachment, err := c.WorkloadAttachment(name).Get(ctx)
		out.Attachment = attachment
		out.recordGetError("workload_attachment", err)
	}
	out.collectDiagnostics(ctx, c, OwnershipAuditOptions{Kinds: []string{"WorkloadAttachment"}, Names: []string{name}})
	return out, nil
}

func (s *NetworkStatus) markDegraded(component, code, message string, details map[string]string) {
	s.State = ResourceStatusDegraded
	s.Degraded = true
	s.Findings = append(s.Findings, StatusFinding{Severity: DoctorWarning, Component: component, Code: code, Message: message, Details: details})
}

func (s *NetworkStatus) recordGetError(component string, err error) {
	if err == nil {
		return
	}
	if IsKind(err, ErrorNotFound) {
		s.State = ResourceStatusMissing
		s.Findings = append(s.Findings, statusFindingFromError(DoctorInfo, component, "not_found", err))
		return
	}
	s.markDegraded(component, "read_failed", "could not read resource status", map[string]string{"error": err.Error()})
}

func (s *NetworkStatus) collectDiagnostics(ctx context.Context, c *Client, opts OwnershipAuditOptions) {
	doctor, audit, findings := collectStatusDiagnostics(ctx, c, opts)
	s.Doctor = doctor
	s.Ownership = audit
	for _, finding := range findings {
		s.markDegraded(finding.Component, finding.Code, finding.Message, finding.Details)
	}
}

func (s *ProviderNetworkStatus) markDegraded(component, code, message string, details map[string]string) {
	s.State = ResourceStatusDegraded
	s.Degraded = true
	s.Findings = append(s.Findings, StatusFinding{Severity: DoctorWarning, Component: component, Code: code, Message: message, Details: details})
}

func (s *ProviderNetworkStatus) recordGetError(component string, err error) {
	if err == nil {
		return
	}
	if IsKind(err, ErrorNotFound) {
		s.State = ResourceStatusMissing
		s.Findings = append(s.Findings, statusFindingFromError(DoctorInfo, component, "not_found", err))
		return
	}
	s.markDegraded(component, "read_failed", "could not read resource status", map[string]string{"error": err.Error()})
}

func (s *ProviderNetworkStatus) collectDiagnostics(ctx context.Context, c *Client, opts OwnershipAuditOptions) {
	doctor, audit, findings := collectStatusDiagnostics(ctx, c, opts)
	s.Doctor = doctor
	s.Ownership = audit
	for _, finding := range findings {
		s.markDegraded(finding.Component, finding.Code, finding.Message, finding.Details)
	}
}

func (s *WorkloadPathStatus) markDegraded(component, code, message string, details map[string]string) {
	s.State = ResourceStatusDegraded
	s.Degraded = true
	s.Findings = append(s.Findings, StatusFinding{Severity: DoctorWarning, Component: component, Code: code, Message: message, Details: details})
}

func (s *WorkloadPathStatus) recordGetError(component string, err error) {
	if err == nil {
		return
	}
	if IsKind(err, ErrorNotFound) {
		s.State = ResourceStatusMissing
		s.Findings = append(s.Findings, statusFindingFromError(DoctorInfo, component, "not_found", err))
		return
	}
	s.markDegraded(component, "read_failed", "could not read resource status", map[string]string{"error": err.Error()})
}

func (s *WorkloadPathStatus) collectDiagnostics(ctx context.Context, c *Client, opts OwnershipAuditOptions) {
	doctor, audit, findings := collectStatusDiagnostics(ctx, c, opts)
	s.Doctor = doctor
	s.Ownership = audit
	for _, finding := range findings {
		s.markDegraded(finding.Component, finding.Code, finding.Message, finding.Details)
	}
}

func collectStatusDiagnostics(ctx context.Context, c *Client, opts OwnershipAuditOptions) (*DoctorReport, *OwnershipAuditReport, []StatusFinding) {
	var findings []StatusFinding
	doctor, err := (&Diagnostics{client: c}).Doctor(ctx, DoctorOptions{})
	if err != nil {
		findings = append(findings, statusFindingFromError(DoctorWarning, "diagnostics", "doctor_failed", err))
	}
	audit, err := (&Diagnostics{client: c}).AuditOwnership(ctx, opts)
	if err != nil {
		findings = append(findings, statusFindingFromError(DoctorWarning, "diagnostics", "ownership_audit_failed", err))
	}
	return doctor, audit, findings
}

func statusFindingFromError(severity DoctorSeverity, component, code string, err error) StatusFinding {
	details := map[string]string{"error": err.Error()}
	if kind := KindOf(err); kind != "" {
		details["kind"] = string(kind)
	}
	return StatusFinding{
		Severity:  severity,
		Component: component,
		Code:      code,
		Message:   err.Error(),
		Details:   details,
	}
}

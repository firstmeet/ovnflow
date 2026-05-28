package ovnflow

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	SDWANAgentFeatureWireGuard  = "wireguard"
	SDWANAgentFeatureLinuxRoute = "linux-route"
	SDWANAgentFeatureOVSTunnel  = "ovs-tunnel"
	SDWANAgentFeatureOpenFlow   = "openflow"
)

type SDWANAgent struct {
	ID           string
	Site         string
	Endpoint     string
	Capabilities SDWANAgentCapabilities
	Labels       Labels
	Status       SDWANAgentStatus
}

type SDWANAgentCapabilities struct {
	Transports []SDWANTransport
	Layers     []SDWANLayer
	Features   []string
	Attributes map[string]string
}

type SDWANAgentStatus struct {
	State      ResourceStatusState
	Message    string
	LastSeen   time.Time
	Observed   []SDWANLinkStatus
	Findings   []StatusFinding
	Generation int
	Attributes map[string]string
}

type SDWANAgentHeartbeat struct {
	AgentID    string
	Site       string
	Status     SDWANAgentStatus
	Observed   []SDWANLinkStatus
	Attributes map[string]string
	At         time.Time
}

type SDWANAssignmentStatusState string

const (
	SDWANAssignmentPending  SDWANAssignmentStatusState = "pending"
	SDWANAssignmentApplied  SDWANAssignmentStatusState = "applied"
	SDWANAssignmentRejected SDWANAssignmentStatusState = "rejected"
	SDWANAssignmentFailed   SDWANAssignmentStatusState = "failed"
)

type SDWANAssignment struct {
	ID         string
	AgentID    string
	Network    string
	Site       string
	Generation int
	Desired    SDWANNetwork
	Plan       SDWANApplyPlan
	Status     SDWANAssignmentStatus
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type SDWANAssignmentStatus struct {
	State      SDWANAssignmentStatusState
	Message    string
	AppliedAt  time.Time
	Findings   []StatusFinding
	Attributes map[string]string
}

type SDWANControlPlane interface {
	RegisterAgent(context.Context, SDWANAgent) (*SDWANAgent, error)
	Heartbeat(context.Context, SDWANAgentHeartbeat) (*SDWANAgent, error)
	GetAgent(context.Context, string) (*SDWANAgent, error)
	ListAgents(context.Context) ([]SDWANAgent, error)
	AssignSDWAN(context.Context, string, SDWANNetwork, SDWANApplyPlan) (*SDWANAssignment, error)
	GetAssignment(context.Context, string) (*SDWANAssignment, error)
	ListAssignments(context.Context, string) ([]SDWANAssignment, error)
	AckAssignment(context.Context, string, SDWANAssignmentStatus) (*SDWANAssignment, error)
}

type InMemorySDWANControlPlane struct {
	mu          sync.RWMutex
	clock       func() time.Time
	agents      map[string]SDWANAgent
	assignments map[string]SDWANAssignment
	generations map[string]int
}

func NewInMemorySDWANControlPlane() *InMemorySDWANControlPlane {
	return &InMemorySDWANControlPlane{
		clock:       time.Now,
		agents:      map[string]SDWANAgent{},
		assignments: map[string]SDWANAssignment{},
		generations: map[string]int{},
	}
}

func (c *InMemorySDWANControlPlane) RegisterAgent(_ context.Context, agent SDWANAgent) (*SDWANAgent, error) {
	if err := agent.Validate(); err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ensure()
	now := c.now()
	current := c.agents[agent.ID]
	agent.Capabilities = normalizeSDWANAgentCapabilities(agent.Capabilities)
	agent.Labels = cloneLabels(agent.Labels)
	agent.Status.Attributes = cloneStringMap(agent.Status.Attributes)
	agent.Status.Observed = cloneSDWANLinkStatuses(agent.Status.Observed)
	agent.Status.Findings = append([]StatusFinding{}, agent.Status.Findings...)
	if agent.Status.State == "" {
		agent.Status.State = ResourceStatusPresent
	}
	if agent.Status.LastSeen.IsZero() {
		agent.Status.LastSeen = now
	}
	if !reflect.DeepEqual(current.Capabilities, agent.Capabilities) ||
		current.Site != agent.Site ||
		current.Endpoint != agent.Endpoint {
		agent.Status.Generation = current.Status.Generation + 1
	} else {
		agent.Status.Generation = current.Status.Generation
	}
	c.agents[agent.ID] = cloneSDWANAgent(agent)
	out := cloneSDWANAgent(agent)
	return &out, nil
}

func (c *InMemorySDWANControlPlane) Heartbeat(_ context.Context, hb SDWANAgentHeartbeat) (*SDWANAgent, error) {
	if err := validateName("sdwan agent", hb.AgentID); err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ensure()
	agent, ok := c.agents[hb.AgentID]
	if !ok {
		return nil, wrap(ErrorNotFound, "", "", "heartbeat", hb.AgentID, "SD-WAN agent not found", nil)
	}
	if hb.Site != "" && hb.Site != agent.Site {
		return nil, wrap(ErrorConflict, "", "", "heartbeat", hb.AgentID, "heartbeat site does not match registered agent", nil)
	}
	if hb.At.IsZero() {
		hb.At = c.now()
	}
	status := hb.Status
	if status.State == "" {
		status.State = ResourceStatusPresent
	}
	status.LastSeen = hb.At
	status.Observed = cloneSDWANLinkStatuses(hb.Observed)
	status.Attributes = mergeStringMaps(status.Attributes, hb.Attributes)
	status.Generation = agent.Status.Generation
	agent.Status = status
	c.agents[agent.ID] = cloneSDWANAgent(agent)
	out := cloneSDWANAgent(agent)
	return &out, nil
}

func (c *InMemorySDWANControlPlane) GetAgent(_ context.Context, id string) (*SDWANAgent, error) {
	if err := validateName("sdwan agent", id); err != nil {
		return nil, err
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	agent, ok := c.agents[id]
	if !ok {
		return nil, wrap(ErrorNotFound, "", "", "get", id, "SD-WAN agent not found", nil)
	}
	out := cloneSDWANAgent(agent)
	return &out, nil
}

func (c *InMemorySDWANControlPlane) ListAgents(context.Context) ([]SDWANAgent, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]SDWANAgent, 0, len(c.agents))
	for _, agent := range c.agents {
		out = append(out, cloneSDWANAgent(agent))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (c *InMemorySDWANControlPlane) AssignSDWAN(_ context.Context, agentID string, network SDWANNetwork, plan SDWANApplyPlan) (*SDWANAssignment, error) {
	if err := validateName("sdwan agent", agentID); err != nil {
		return nil, err
	}
	if err := network.Validate(); err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ensure()
	agent, ok := c.agents[agentID]
	if !ok {
		return nil, wrap(ErrorNotFound, "", "", "assign", agentID, "SD-WAN agent not found", nil)
	}
	if !agentCanServe(agent, network) {
		return nil, wrap(ErrorUnsupported, "", "", "assign", agentID, "agent capabilities do not satisfy SD-WAN network", nil)
	}
	key := agentID + "/" + network.Name
	c.generations[key]++
	now := c.now()
	assignment := SDWANAssignment{
		ID:         key,
		AgentID:    agentID,
		Network:    network.Name,
		Site:       agent.Site,
		Generation: c.generations[key],
		Desired:    normalizeSDWANNetwork(network),
		Plan:       cloneSDWANApplyPlan(plan),
		Status:     SDWANAssignmentStatus{State: SDWANAssignmentPending},
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	c.assignments[key] = cloneSDWANAssignment(assignment)
	out := cloneSDWANAssignment(assignment)
	return &out, nil
}

func (c *InMemorySDWANControlPlane) GetAssignment(_ context.Context, id string) (*SDWANAssignment, error) {
	if err := validateName("sdwan assignment", id); err != nil {
		return nil, err
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	assignment, ok := c.assignments[id]
	if !ok {
		return nil, wrap(ErrorNotFound, "", "", "get", id, "SD-WAN assignment not found", nil)
	}
	out := cloneSDWANAssignment(assignment)
	return &out, nil
}

func (c *InMemorySDWANControlPlane) ListAssignments(_ context.Context, agentID string) ([]SDWANAssignment, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	var out []SDWANAssignment
	for _, assignment := range c.assignments {
		if agentID != "" && assignment.AgentID != agentID {
			continue
		}
		out = append(out, cloneSDWANAssignment(assignment))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (c *InMemorySDWANControlPlane) AckAssignment(_ context.Context, id string, status SDWANAssignmentStatus) (*SDWANAssignment, error) {
	if err := validateName("sdwan assignment", id); err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	assignment, ok := c.assignments[id]
	if !ok {
		return nil, wrap(ErrorNotFound, "", "", "ack", id, "SD-WAN assignment not found", nil)
	}
	if err := status.Validate(); err != nil {
		return nil, err
	}
	if status.State == SDWANAssignmentApplied && status.AppliedAt.IsZero() {
		status.AppliedAt = c.now()
	}
	status.Findings = append([]StatusFinding{}, status.Findings...)
	status.Attributes = cloneStringMap(status.Attributes)
	assignment.Status = status
	assignment.UpdatedAt = c.now()
	c.assignments[id] = cloneSDWANAssignment(assignment)
	out := cloneSDWANAssignment(assignment)
	return &out, nil
}

func (a SDWANAgent) Validate() error {
	if err := validateName("sdwan agent", a.ID); err != nil {
		return err
	}
	if err := validateName("sdwan site", a.Site); err != nil {
		return err
	}
	if err := a.Labels.Validate(); err != nil {
		return err
	}
	if err := Labels(a.Status.Attributes).Validate(); err != nil {
		return err
	}
	if len(a.Capabilities.Transports) == 0 {
		return wrap(ErrorValidation, "", "", "validate", a.ID, "agent must advertise at least one transport", nil)
	}
	for _, transport := range a.Capabilities.Transports {
		if !validSDWANTransport(transport) {
			return wrap(ErrorValidation, "", "", "validate", a.ID, "agent advertises unsupported transport", nil)
		}
	}
	for _, layer := range a.Capabilities.Layers {
		if !validSDWANLayer(layer) {
			return wrap(ErrorValidation, "", "", "validate", a.ID, "agent advertises unsupported layer", nil)
		}
	}
	if err := Labels(a.Capabilities.Attributes).Validate(); err != nil {
		return err
	}
	return validateSDWANAgentFeatures(a.ID, a.Capabilities.Features)
}

func (s SDWANAgentStatus) Validate() error {
	return Labels(s.Attributes).Validate()
}

func (s SDWANAssignmentStatus) Validate() error {
	if s.State == "" {
		return wrap(ErrorValidation, "", "", "validate", "sdwan assignment status", "assignment status state is required", nil)
	}
	switch s.State {
	case SDWANAssignmentPending, SDWANAssignmentApplied, SDWANAssignmentRejected, SDWANAssignmentFailed:
	default:
		return wrap(ErrorValidation, "", "", "validate", "sdwan assignment status", "unsupported assignment status state", nil)
	}
	return Labels(s.Attributes).Validate()
}

func (c *InMemorySDWANControlPlane) ensure() {
	if c.clock == nil {
		c.clock = time.Now
	}
	if c.agents == nil {
		c.agents = map[string]SDWANAgent{}
	}
	if c.assignments == nil {
		c.assignments = map[string]SDWANAssignment{}
	}
	if c.generations == nil {
		c.generations = map[string]int{}
	}
}

func (c *InMemorySDWANControlPlane) now() time.Time {
	if c.clock == nil {
		return time.Now()
	}
	return c.clock()
}

func normalizeSDWANAgentCapabilities(in SDWANAgentCapabilities) SDWANAgentCapabilities {
	out := in
	out.Transports = uniqueSDWANTransports(out.Transports)
	out.Layers = uniqueSDWANLayers(out.Layers)
	out.Features = uniqueStrings(out.Features)
	out.Attributes = cloneStringMap(out.Attributes)
	return out
}

func agentCanServe(agent SDWANAgent, network SDWANNetwork) bool {
	caps := normalizeSDWANAgentCapabilities(agent.Capabilities)
	hasTransport := false
	for _, transport := range caps.Transports {
		if transport == network.Transport {
			hasTransport = true
			break
		}
	}
	if !hasTransport {
		return false
	}
	if len(caps.Layers) == 0 {
		return true
	}
	for _, layer := range caps.Layers {
		if layer == network.Layer {
			return true
		}
	}
	return false
}

func validateSDWANAgentFeatures(agentID string, values []string) error {
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			return wrap(ErrorValidation, "", "", "validate", agentID, "agent feature must not be empty", nil)
		}
	}
	return nil
}

func uniqueSDWANTransports(values []SDWANTransport) []SDWANTransport {
	seen := map[SDWANTransport]bool{}
	var out []SDWANTransport
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func uniqueSDWANLayers(values []SDWANLayer) []SDWANLayer {
	seen := map[SDWANLayer]bool{}
	var out []SDWANLayer
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func cloneSDWANAgent(in SDWANAgent) SDWANAgent {
	out := in
	out.Capabilities = normalizeSDWANAgentCapabilities(in.Capabilities)
	out.Labels = cloneLabels(in.Labels)
	out.Status.Observed = cloneSDWANLinkStatuses(in.Status.Observed)
	out.Status.Findings = append([]StatusFinding{}, in.Status.Findings...)
	out.Status.Attributes = cloneStringMap(in.Status.Attributes)
	return out
}

func cloneSDWANLinkStatuses(in []SDWANLinkStatus) []SDWANLinkStatus {
	return append([]SDWANLinkStatus{}, in...)
}

func cloneSDWANAssignment(in SDWANAssignment) SDWANAssignment {
	out := in
	out.Desired = cloneSDWANNetwork(in.Desired)
	out.Plan = cloneSDWANApplyPlan(in.Plan)
	out.Status.Findings = append([]StatusFinding{}, in.Status.Findings...)
	out.Status.Attributes = cloneStringMap(in.Status.Attributes)
	return out
}

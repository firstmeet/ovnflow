package ovnflow

import (
	"context"
	"sort"
	"strconv"
	"strings"
)

const qosPolicyExternalID = ExternalIDPrefix + "policy"
const qosRuleExternalID = ExternalIDPrefix + "rule"

// QoSPolicy is an intent-level QoS policy composed of OVN QoS rules.
type QoSPolicy struct {
	Name   string
	Rules  []QoSRule
	Owner  OwnerRef
	Labels Labels
}

// QoSRule describes one OVN QoS rule without exposing OVN transaction details.
type QoSRule struct {
	Name      string
	Direction string
	Priority  int
	Match     string
	Rate      int
	Burst     int
	DSCP      *int
	Mark      *int
	Switch    string
}

// QoSPolicyPatch describes incremental QoS policy edits.
type QoSPolicyPatch struct {
	ReplaceRules bool
	Rules        []QoSRule
	AddRules     []QoSRule
	RemoveRules  []string
	Owner        *OwnerRef
	Labels       Labels
	RemoveLabels []string
}

// QoSPolicyRef identifies a QoS policy intent by name.
type QoSPolicyRef struct {
	client *NBClient
	name   string
}

// QoSPolicyBuilder builds QoS policy intent operations.
type QoSPolicyBuilder struct {
	ref  *QoSPolicyRef
	spec QoSPolicy
}

func (n *NBClient) QoSPolicy(name string) *QoSPolicyRef {
	return &QoSPolicyRef{client: n, name: name}
}

func (r *QoSPolicyRef) Ensure() *QoSPolicyBuilder {
	return &QoSPolicyBuilder{ref: r, spec: QoSPolicy{Name: r.name, Labels: Labels{}}}
}

func (r *QoSPolicyRef) Apply(ctx context.Context, policy QoSPolicy) error {
	if r.client == nil || r.client.db == nil {
		return ErrBackendUnavailable
	}
	if policy.Name == "" {
		policy.Name = r.name
	}
	if policy.Name != r.name {
		return wrap(ErrorConflict, dbOVNNorthbound, tableQoS, "apply", policy.Name, "qos policy name does not match reference", nil)
	}
	builder := &QoSPolicyBuilder{ref: r, spec: policy}
	_, err := builder.Reconcile(ctx)
	return err
}

func (r *QoSPolicyRef) Get(ctx context.Context) (*QoSPolicy, error) {
	if err := validateName("qos policy", r.name); err != nil {
		return nil, err
	}
	if r.client == nil || r.client.db == nil {
		return nil, ErrBackendUnavailable
	}
	rows, err := r.client.selectRows(ctx, tableQoS, nbExternalIDCondition(qosPolicyExternalID, r.name), nbQoSColumns(), r.name)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, wrap(ErrorNotFound, dbOVNNorthbound, tableQoS, "get", r.name, "qos policy not found", nil)
	}
	policy := QoSPolicy{Name: r.name}
	for i, row := range rows {
		qos := qosFromRow(row)
		rule := qosRuleFromQoS(qos)
		policy.Rules = append(policy.Rules, rule)
		if i == 0 {
			policy.Owner, policy.Labels = ownerAndLabelsFromExternalIDs(qos.ExternalIDs)
		}
	}
	sortQoSRules(policy.Rules)
	return &policy, nil
}

func (r *QoSPolicyRef) Delete(ctx context.Context) error {
	if err := validateName("qos policy", r.name); err != nil {
		return err
	}
	if r.client == nil || r.client.db == nil {
		return ErrBackendUnavailable
	}
	rows, err := r.client.selectRows(ctx, tableQoS, nbExternalIDCondition(qosPolicyExternalID, r.name), nbQoSColumns(), r.name)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return wrap(ErrorNotFound, dbOVNNorthbound, tableQoS, "delete", r.name, "qos policy not found", nil)
	}
	for _, row := range rows {
		qos := qosFromRow(row)
		if err := requireQoSPolicyOwned(qos, r.name, "delete"); err != nil {
			return err
		}
		rule := qosRuleFromQoS(qos)
		if err := r.client.QoSByMatch(rule.Direction, rule.Priority, rule.Match).Delete().Execute(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (r *QoSPolicyRef) Patch(ctx context.Context, patch QoSPolicyPatch) (*QoSPolicy, error) {
	current, err := r.Get(ctx)
	if err != nil {
		return nil, err
	}
	next := normalizeQoSPolicy(*current)
	if patch.ReplaceRules {
		next.Rules = append([]QoSRule{}, patch.Rules...)
	} else {
		next.Rules = mergeQoSRules(next.Rules, patch.AddRules)
	}
	next.Rules = removeQoSRules(next.Rules, patch.RemoveRules)
	if patch.Owner != nil {
		next.Owner = *patch.Owner
	}
	next.Labels = patchLabels(next.Labels, patch.Labels, patch.RemoveLabels)
	if err := r.Apply(ctx, next); err != nil {
		return nil, err
	}
	for _, rule := range next.Rules {
		if err := r.client.deleteExternalIDKeys(ctx, tableQoS, r.name, r.client.QoSByMatch(rule.Direction, rule.Priority, rule.Match).conditions(), labelDeleteKeys(patch.RemoveLabels, patch.Labels)); err != nil {
			return nil, err
		}
	}
	return &next, nil
}

func (b *QoSPolicyBuilder) WithOwner(kind, name string) *QoSPolicyBuilder {
	b.spec.Owner = OwnerRef{Kind: kind, Name: name}
	return b
}

func (b *QoSPolicyBuilder) WithLabel(key, value string) *QoSPolicyBuilder {
	if b.spec.Labels == nil {
		b.spec.Labels = Labels{}
	}
	b.spec.Labels[key] = value
	return b
}

func (b *QoSPolicyBuilder) AddRule(rule QoSRule) *QoSPolicyBuilder {
	b.spec.Rules = append(b.spec.Rules, rule)
	return b
}

func (b *QoSPolicyBuilder) Validate() error {
	return b.spec.Validate()
}

func (b *QoSPolicyBuilder) Plan(ctx context.Context) (Plan, error) {
	if err := b.Validate(); err != nil {
		return Plan{}, err
	}
	return Plan{Operations: []PlannedOperation{{
		Action:      IntentActionEnsure,
		Resource:    "QoSPolicy",
		Name:        b.spec.Name,
		Description: "validate and plan OVN QoS policy intent",
	}}}, nil
}

func (b *QoSPolicyBuilder) DryRun(ctx context.Context) (DryRunResult, error) {
	plan, err := b.Plan(ctx)
	if err != nil {
		return DryRunResult{}, err
	}
	diff, err := b.diff(ctx)
	if err != nil {
		return DryRunResult{}, err
	}
	return DryRunResult{Plan: plan, Diff: diff}, nil
}

func (b *QoSPolicyBuilder) Reconcile(ctx context.Context) (ReconcileResult, error) {
	plan, err := b.Plan(ctx)
	if err != nil {
		return ReconcileResult{}, err
	}
	if err := b.requireExistingQoSRulesOwned(ctx); err != nil {
		return ReconcileResult{}, err
	}
	diff, err := b.diff(ctx)
	if err != nil {
		return ReconcileResult{}, err
	}
	if b.ref == nil || b.ref.client == nil || b.ref.client.db == nil {
		return ReconcileResult{Plan: plan, Diff: diff, Applied: false}, nil
	}
	if diff.Empty() {
		return ReconcileResult{Plan: plan, Diff: diff, Applied: false}, nil
	}
	if err := b.reconcileOVSDB(ctx); err != nil {
		return ReconcileResult{}, err
	}
	return ReconcileResult{Plan: plan, Diff: diff, Applied: true}, nil
}

func (b *QoSPolicyBuilder) Execute(ctx context.Context) error {
	_, err := b.Reconcile(ctx)
	return err
}

func (p QoSPolicy) Validate() error {
	if err := validateName("qos policy", p.Name); err != nil {
		return err
	}
	if p.Owner.Kind != "" || p.Owner.Name != "" || p.Owner.ID != "" {
		if err := p.Owner.Validate(); err != nil {
			return err
		}
	}
	if err := p.Labels.Validate(); err != nil {
		return err
	}
	for _, rule := range p.Rules {
		if err := rule.Validate(p.Name); err != nil {
			return err
		}
	}
	return nil
}

func (r QoSRule) Validate(policy string) error {
	object := r.Name
	if object == "" {
		object = policy
	}
	if strings.TrimSpace(r.Direction) == "" {
		return wrap(ErrorValidation, dbOVNNorthbound, tableQoS, "validate", object, "direction is required", nil)
	}
	if r.Priority < 0 || r.Priority > 32767 {
		return wrap(ErrorValidation, dbOVNNorthbound, tableQoS, "validate", object, "priority must be between 0 and 32767", nil)
	}
	if strings.TrimSpace(r.Match) == "" {
		return wrap(ErrorValidation, dbOVNNorthbound, tableQoS, "validate", object, "match is required", nil)
	}
	if r.Rate < 0 || r.Burst < 0 {
		return wrap(ErrorValidation, dbOVNNorthbound, tableQoS, "validate", object, "rate and burst must be non-negative", nil)
	}
	if r.DSCP != nil && (*r.DSCP < 0 || *r.DSCP > 63) {
		return wrap(ErrorValidation, dbOVNNorthbound, tableQoS, "validate", object, "dscp must be between 0 and 63", nil)
	}
	if strings.TrimSpace(r.Switch) != "" {
		if err := validateName("logical switch", r.Switch); err != nil {
			return err
		}
	}
	return nil
}

func (b *QoSPolicyBuilder) reconcileOVSDB(ctx context.Context) error {
	externalIDs, err := qosPolicyExternalIDs(b.spec.Name, b.spec.Owner, b.spec.Labels)
	if err != nil {
		return err
	}
	if err := b.deleteStaleRules(ctx); err != nil {
		return err
	}
	for _, rule := range b.spec.Rules {
		ruleIDs := mergeStringMaps(externalIDs, map[string]string{qosPolicyExternalID: b.spec.Name})
		if rule.Name != "" {
			ruleIDs[qosRuleExternalID] = rule.Name
		}
		builder := b.ref.client.QoSByMatch(rule.Direction, rule.Priority, rule.Match).Ensure()
		if rule.Switch != "" {
			builder.AttachToSwitch(rule.Switch)
		}
		if rule.Rate > 0 {
			builder.WithRate(rule.Rate)
		}
		if rule.Burst > 0 {
			builder.WithBurst(rule.Burst)
		}
		if rule.DSCP != nil {
			builder.WithDSCP(*rule.DSCP)
		}
		if rule.Mark != nil {
			builder.WithMark(*rule.Mark)
		}
		for _, key := range sortedMapKeys(ruleIDs) {
			builder.WithExternalID(key, ruleIDs[key])
		}
		if err := b.clearQoSRuleMaps(ctx, rule); err != nil {
			return err
		}
		if err := builder.Execute(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (b *QoSPolicyBuilder) requireExistingQoSRulesOwned(ctx context.Context) error {
	if b == nil || b.ref == nil || b.ref.client == nil || b.ref.client.db == nil {
		return nil
	}
	for _, rule := range b.spec.Rules {
		qos, err := b.ref.client.GetQoS(ctx, rule.Direction, rule.Priority, rule.Match)
		if err != nil {
			if IsKind(err, ErrorNotFound) {
				continue
			}
			return err
		}
		if err := requireQoSPolicyOwned(qos, b.spec.Name, "ensure"); err != nil {
			return err
		}
	}
	return nil
}

func (b *QoSPolicyBuilder) clearQoSRuleMaps(ctx context.Context, rule QoSRule) error {
	qos, err := b.ref.client.GetQoS(ctx, rule.Direction, rule.Priority, rule.Match)
	if err != nil {
		if IsKind(err, ErrorNotFound) {
			return nil
		}
		return err
	}
	if err := requireQoSPolicyOwned(qos, b.spec.Name, "ensure"); err != nil {
		return err
	}
	var deleteBandwidth []string
	if rule.Rate <= 0 {
		if _, ok := qos.Bandwidth["rate"]; ok {
			deleteBandwidth = append(deleteBandwidth, "rate")
		}
	}
	if rule.Burst <= 0 {
		if _, ok := qos.Bandwidth["burst"]; ok {
			deleteBandwidth = append(deleteBandwidth, "burst")
		}
	}
	if err := b.ref.client.deleteMapKeys(ctx, tableQoS, b.spec.Name, colBandwidth, b.ref.client.QoSByMatch(rule.Direction, rule.Priority, rule.Match).conditions(), deleteBandwidth); err != nil {
		return err
	}
	var deleteAction []string
	if rule.DSCP == nil {
		if _, ok := qos.Action["dscp"]; ok {
			deleteAction = append(deleteAction, "dscp")
		}
	}
	if rule.Mark == nil {
		if _, ok := qos.Action["mark"]; ok {
			deleteAction = append(deleteAction, "mark")
		}
	}
	return b.ref.client.deleteMapKeys(ctx, tableQoS, b.spec.Name, colAction, b.ref.client.QoSByMatch(rule.Direction, rule.Priority, rule.Match).conditions(), deleteAction)
}

func (b *QoSPolicyBuilder) deleteStaleRules(ctx context.Context) error {
	current, found, err := b.current(ctx)
	if err != nil || !found {
		return err
	}
	desired := normalizeQoSPolicy(b.spec)
	keep := map[string]struct{}{}
	for _, rule := range desired.Rules {
		keep[qosRuleIdentity(rule)] = struct{}{}
	}
	for _, rule := range current.Rules {
		if _, ok := keep[qosRuleIdentity(rule)]; ok {
			continue
		}
		qos, err := b.ref.client.GetQoS(ctx, rule.Direction, rule.Priority, rule.Match)
		if err != nil {
			if IsKind(err, ErrorNotFound) {
				continue
			}
			return err
		}
		if err := requireQoSPolicyOwned(qos, b.spec.Name, "delete"); err != nil {
			return err
		}
		if err := b.ref.client.QoSByMatch(rule.Direction, rule.Priority, rule.Match).Delete().Execute(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (b *QoSPolicyBuilder) diff(ctx context.Context) (Diff, error) {
	desired := normalizeQoSPolicy(b.spec)
	diff := Diff{Resource: "QoSPolicy", Name: desired.Name}
	current, found, err := b.current(ctx)
	if err != nil {
		return Diff{}, err
	}
	if !found {
		diff.Changes = append(diff.Changes, DiffChange{Path: "/", Before: nil, After: desired})
		return diff, nil
	}
	appendFieldDiff(&diff, "rules", current.Rules, desired.Rules)
	appendFieldDiff(&diff, "owner", current.Owner, desired.Owner)
	appendFieldDiff(&diff, "labels", current.Labels, desired.Labels)
	return diff, nil
}

func (b *QoSPolicyBuilder) current(ctx context.Context) (QoSPolicy, bool, error) {
	if b.ref == nil || b.ref.client == nil || b.ref.client.db == nil {
		return QoSPolicy{}, false, nil
	}
	current, err := b.ref.Get(ctx)
	if err != nil {
		if IsKind(err, ErrorNotFound) {
			return QoSPolicy{}, false, nil
		}
		return QoSPolicy{}, false, err
	}
	return normalizeQoSPolicy(*current), true, nil
}

func qosPolicyExternalIDs(kindName string, owner OwnerRef, labels Labels) (map[string]string, error) {
	externalIDs, err := intentExternalIDs("QoSPolicy", kindName, owner, labels)
	if err != nil {
		return nil, err
	}
	return externalIDs, nil
}

func requireQoSPolicyOwned(qos *QoS, policy, op string) error {
	if qos == nil {
		return wrap(ErrorNotFound, dbOVNNorthbound, tableQoS, op, policy, "qos row not found", nil)
	}
	return requireV2OwnedExternalIDs(qos.ExternalIDs, "QoSPolicy", policy, dbOVNNorthbound, tableQoS, op, policy)
}

func qosRuleIdentity(rule QoSRule) string {
	return rule.Direction + "\x00" + strconv.Itoa(rule.Priority) + "\x00" + rule.Match
}

func qosRuleFromQoS(qos *QoS) QoSRule {
	if qos == nil {
		return QoSRule{}
	}
	rule := QoSRule{
		Name:      qos.ExternalIDs[qosRuleExternalID],
		Direction: qos.Direction,
		Priority:  qos.Priority,
		Match:     qos.Match,
		Rate:      qos.Bandwidth["rate"],
		Burst:     qos.Bandwidth["burst"],
	}
	if value, ok := qos.Action["dscp"]; ok {
		rule.DSCP = intPtr(value)
	}
	if value, ok := qos.Action["mark"]; ok {
		rule.Mark = intPtr(value)
	}
	return rule
}

func normalizeQoSPolicy(in QoSPolicy) QoSPolicy {
	out := in
	out.Labels = cloneLabels(in.Labels)
	out.Rules = append([]QoSRule{}, in.Rules...)
	sortQoSRules(out.Rules)
	return out
}

func sortQoSRules(rules []QoSRule) {
	sort.Slice(rules, func(i, j int) bool {
		if rules[i].Direction != rules[j].Direction {
			return rules[i].Direction < rules[j].Direction
		}
		if rules[i].Priority != rules[j].Priority {
			return rules[i].Priority < rules[j].Priority
		}
		if rules[i].Match != rules[j].Match {
			return rules[i].Match < rules[j].Match
		}
		return rules[i].Name < rules[j].Name
	})
}

func mergeQoSRules(current, add []QoSRule) []QoSRule {
	out := append([]QoSRule{}, current...)
	index := map[string]int{}
	for i, rule := range out {
		index[qosPatchRuleKey(rule)] = i
	}
	for _, rule := range add {
		key := qosPatchRuleKey(rule)
		if i, ok := index[key]; ok {
			out[i] = rule
			continue
		}
		index[key] = len(out)
		out = append(out, rule)
	}
	sortQoSRules(out)
	return out
}

func removeQoSRules(current []QoSRule, remove []string) []QoSRule {
	if len(remove) == 0 {
		return append([]QoSRule{}, current...)
	}
	deny := map[string]struct{}{}
	for _, value := range remove {
		deny[value] = struct{}{}
	}
	out := make([]QoSRule, 0, len(current))
	for _, rule := range current {
		if _, ok := deny[qosPatchRuleKey(rule)]; ok {
			continue
		}
		if rule.Name != "" {
			if _, ok := deny[rule.Name]; ok {
				continue
			}
		}
		out = append(out, rule)
	}
	sortQoSRules(out)
	return out
}

func qosPatchRuleKey(rule QoSRule) string {
	if rule.Name != "" {
		return rule.Name
	}
	return qosRuleIdentity(rule)
}

func sortedMapKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func intPtr(value int) *int {
	return &value
}

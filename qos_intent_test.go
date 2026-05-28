package ovnflow

import (
	"context"
	"reflect"
	"sync"
	"testing"

	libovsdb "github.com/ovn-kubernetes/libovsdb/ovsdb"
)

func TestQoSPolicyValidation(t *testing.T) {
	dscp := 64
	tests := []struct {
		name string
		spec QoSPolicy
	}{
		{name: "direction", spec: QoSPolicy{Name: "p", Rules: []QoSRule{{Priority: 1, Match: "ip"}}}},
		{name: "priority low", spec: QoSPolicy{Name: "p", Rules: []QoSRule{{Direction: "from-lport", Priority: -1, Match: "ip"}}}},
		{name: "priority high", spec: QoSPolicy{Name: "p", Rules: []QoSRule{{Direction: "from-lport", Priority: 32768, Match: "ip"}}}},
		{name: "match", spec: QoSPolicy{Name: "p", Rules: []QoSRule{{Direction: "from-lport", Priority: 1}}}},
		{name: "rate", spec: QoSPolicy{Name: "p", Rules: []QoSRule{{Direction: "from-lport", Priority: 1, Match: "ip", Rate: -1}}}},
		{name: "burst", spec: QoSPolicy{Name: "p", Rules: []QoSRule{{Direction: "from-lport", Priority: 1, Match: "ip", Burst: -1}}}},
		{name: "dscp", spec: QoSPolicy{Name: "p", Rules: []QoSRule{{Direction: "from-lport", Priority: 1, Match: "ip", DSCP: &dscp}}}},
		{name: "owner", spec: QoSPolicy{Name: "p", Owner: OwnerRef{Kind: "project"}, Rules: []QoSRule{{Direction: "from-lport", Priority: 1, Match: "ip"}}}},
		{name: "label", spec: QoSPolicy{Name: "p", Labels: Labels{"": "bad"}, Rules: []QoSRule{{Direction: "from-lport", Priority: 1, Match: "ip"}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.spec.Validate(); !IsKind(err, ErrorValidation) {
				t.Fatalf("Validate() = %v, want ErrorValidation", err)
			}
		})
	}
}

func TestQoSPolicyPlanAndDryRun(t *testing.T) {
	builder := (&NBClient{}).QoSPolicy("web-qos").Ensure().
		AddRule(QoSRule{Direction: "from-lport", Priority: 100, Match: `inport == "web"`})

	plan, err := builder.Plan(context.Background())
	if err != nil {
		t.Fatalf("Plan() = %v", err)
	}
	if len(plan.Operations) != 1 || plan.Operations[0].Resource != "QoSPolicy" || plan.Operations[0].Name != "web-qos" {
		t.Fatalf("plan = %#v, want QoSPolicy operation", plan)
	}

	dryRun, err := (&NBClient{}).QoSPolicy("web-qos").Ensure().
		AddRule(QoSRule{Direction: "from-lport", Priority: 100, Match: `inport == "web"`}).
		DryRun(context.Background())
	if err != nil {
		t.Fatalf("DryRun() = %v", err)
	}
	if dryRun.Diff.Empty() || dryRun.Diff.Resource != "QoSPolicy" || dryRun.Diff.Name != "web-qos" {
		t.Fatalf("dry run diff = %#v, want creation diff", dryRun.Diff)
	}
}

func TestQoSPolicyWritesExternalIDs(t *testing.T) {
	db := testNBDBClient(t)
	rec := &nbRecordingExecutor{results: []libovsdb.OperationResult{
		{Rows: nil},
		{},
		{},
		{Rows: nil},
		{},
		{Count: 1},
	}}
	db.executor = rec
	dscp := 46

	err := (&NBClient{db: db}).QoSPolicy("gold").Ensure().
		WithOwner("project", "alpha").
		WithLabel("tier", "gold").
		AddRule(QoSRule{Name: "web", Direction: "from-lport", Priority: 100, Match: `inport == "web"`, Rate: 1000, DSCP: &dscp}).
		Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute() = %v", err)
	}
	insert := findRecordedOp(rec.ops, libovsdb.OperationInsert, tableQoS)
	if insert == nil {
		t.Fatalf("ops missing QoS insert: %#v", rec.ops)
	}
	externalIDs := rowStringMapValue(insert.Row, colExternalIDs)
	want := map[string]string{
		ExternalIDManagedByKey:     "ovnflow",
		ExternalIDAPIVersionKey:    "v2",
		ExternalIDKindKey:          "QoSPolicy",
		ExternalIDNameKey:          "gold",
		ExternalIDOwnerKindKey:     "project",
		ExternalIDOwnerNameKey:     "alpha",
		ExternalIDLabelKey("tier"): "gold",
		qosPolicyExternalID:        "gold",
		qosRuleExternalID:          "web",
	}
	for key, value := range want {
		if externalIDs[key] != value {
			t.Fatalf("external_ids[%q] = %q, want %q: %#v", key, externalIDs[key], value, externalIDs)
		}
	}
}

func TestQoSPolicyRuleMapsToQoSBuilderParameters(t *testing.T) {
	db := testNBDBClient(t)
	db.schema.schema.Tables[tableLogicalSwitch].Columns[colQoSRules] = columnSchemaFromJSON(t, `{"type":{"key":{"type":"uuid","refTable":"QoS"},"min":0,"max":"unlimited"}}`)
	rec := &nbRecordingExecutor{results: []libovsdb.OperationResult{
		{Rows: nil},
		{Rows: nil},
		{Rows: nil},
		{Rows: nil},
		{},
		{Count: 1},
	}}
	db.executor = rec
	dscp := 32
	mark := 7

	err := (&NBClient{db: db}).QoSPolicy("tenant").Ensure().
		WithOwner("project", "alpha").
		AddRule(QoSRule{
			Name:      "egress",
			Direction: "from-lport",
			Priority:  200,
			Match:     `outport == "uplink"`,
			Rate:      1000,
			Burst:     2000,
			DSCP:      &dscp,
			Mark:      &mark,
			Switch:    "ls0",
		}).
		Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute() = %v", err)
	}
	insert := findRecordedOp(rec.ops, libovsdb.OperationInsert, tableQoS)
	if insert == nil {
		t.Fatalf("ops missing QoS insert: %#v", rec.ops)
	}
	if insert.Row[colDirection] != "from-lport" || insert.Row[colPriority] != 200 || insert.Row[colMatch] != `outport == "uplink"` {
		t.Fatalf("QoS identity row = %#v", insert.Row)
	}
	if got, want := nbIntMapValue(insert.Row, colBandwidth), map[string]int{"rate": 1000, "burst": 2000}; !reflect.DeepEqual(got, want) {
		t.Fatalf("bandwidth = %#v, want %#v", got, want)
	}
	if got, want := nbIntMapValue(insert.Row, colAction), map[string]int{"dscp": 32, "mark": 7}; !reflect.DeepEqual(got, want) {
		t.Fatalf("action = %#v, want %#v", got, want)
	}
	mutate := findRecordedOp(rec.ops, libovsdb.OperationMutate, tableLogicalSwitch)
	if mutate == nil || len(mutate.Where) != 1 || mutate.Where[0].Value != "ls0" {
		t.Fatalf("missing switch attach mutate for ls0: %#v", rec.ops)
	}
}

func TestQoSPolicyEnsureRejectsForeignExistingRule(t *testing.T) {
	db := testNBDBClient(t)
	db.executor = &qosRecordingExecutor{results: []libovsdb.OperationResult{{Rows: []libovsdb.Row{{
		colUUID:        "qos-uuid",
		colDirection:   "from-lport",
		colPriority:    100,
		colMatch:       `inport == "web"`,
		colExternalIDs: ovsMap(map[string]string{"owner": "someone-else"}),
	}}}}}

	err := (&NBClient{db: db}).QoSPolicy("gold").Ensure().
		WithOwner("project", "alpha").
		AddRule(QoSRule{Name: "web", Direction: "from-lport", Priority: 100, Match: `inport == "web"`}).
		Execute(context.Background())
	if !IsKind(err, ErrorOwnershipViolation) {
		t.Fatalf("Execute foreign QoS error = %v, want ownership violation", err)
	}
}

func TestQoSPolicyDeleteRequiresV2Ownership(t *testing.T) {
	db := testNBDBClient(t)
	db.executor = &qosRecordingExecutor{results: []libovsdb.OperationResult{{Rows: []libovsdb.Row{{
		colUUID:        "qos-uuid",
		colDirection:   "from-lport",
		colPriority:    100,
		colMatch:       `inport == "web"`,
		colExternalIDs: ovsMap(map[string]string{qosPolicyExternalID: "gold"}),
	}}}}}

	err := (&NBClient{db: db}).QoSPolicy("gold").Delete(context.Background())
	if !IsKind(err, ErrorOwnershipViolation) {
		t.Fatalf("Delete weak marker error = %v, want ownership violation", err)
	}
}

func TestQoSPolicyReconcileDeletesStaleOwnedRules(t *testing.T) {
	db := testNBDBClient(t)
	ids, err := intentExternalIDs("QoSPolicy", "gold", OwnerRef{Kind: "project", Name: "alpha"}, nil)
	if err != nil {
		t.Fatalf("intentExternalIDs returned error: %v", err)
	}
	current := libovsdb.Row{
		colUUID:        "qos-stale",
		colDirection:   "from-lport",
		colPriority:    90,
		colMatch:       `inport == "old"`,
		colExternalIDs: ovsMap(mergeStringMaps(ids, map[string]string{qosPolicyExternalID: "gold", qosRuleExternalID: "old"})),
	}
	rec := &qosRecordingExecutor{results: []libovsdb.OperationResult{
		{Rows: nil},
		{Rows: []libovsdb.Row{current}},
		{Rows: []libovsdb.Row{current}},
		{Rows: []libovsdb.Row{current}},
		{Rows: []libovsdb.Row{{colUUID: "qos-stale"}}},
		{Count: 1},
		{Rows: nil},
		{},
	}}
	db.executor = rec

	err = (&NBClient{db: db}).QoSPolicy("gold").Ensure().
		WithOwner("project", "alpha").
		AddRule(QoSRule{Name: "web", Direction: "from-lport", Priority: 100, Match: `inport == "web"`}).
		Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if findRecordedOp(rec.ops, libovsdb.OperationDelete, tableQoS) == nil {
		t.Fatalf("ops did not delete stale QoS rule: %#v", rec.ops)
	}
}

func TestQoSPolicyPatchReplacesRulesAndRemovesLabels(t *testing.T) {
	db := testNBDBClient(t)
	ids, err := intentExternalIDs("QoSPolicy", "gold", OwnerRef{Kind: "project", Name: "alpha"}, Labels{"old": "label"})
	if err != nil {
		t.Fatalf("intentExternalIDs returned error: %v", err)
	}
	current := libovsdb.Row{
		colUUID:        "qos-current",
		colDirection:   "from-lport",
		colPriority:    90,
		colMatch:       `inport == "old"`,
		colExternalIDs: ovsMap(mergeStringMaps(ids, map[string]string{qosPolicyExternalID: "gold", qosRuleExternalID: "old"})),
	}
	rec := &qosRecordingExecutor{results: []libovsdb.OperationResult{
		{Rows: []libovsdb.Row{current}},
		{Rows: nil},
		{Rows: []libovsdb.Row{current}},
		{Rows: []libovsdb.Row{current}},
		{Rows: []libovsdb.Row{current}},
		{Rows: []libovsdb.Row{{colUUID: "qos-current"}}},
		{Count: 1},
		{Rows: nil},
		{},
		{Rows: []libovsdb.Row{{colUUID: "qos-new"}}},
		{Count: 1},
	}}
	db.executor = rec

	next, err := (&NBClient{db: db}).QoSPolicy("gold").Patch(context.Background(), QoSPolicyPatch{
		ReplaceRules: true,
		Rules:        []QoSRule{{Name: "new", Direction: "from-lport", Priority: 100, Match: `inport == "new"`}},
		RemoveLabels: []string{"old"},
	})
	if err != nil {
		t.Fatalf("Patch returned error: %v", err)
	}
	if len(next.Rules) != 1 || next.Rules[0].Name != "new" {
		t.Fatalf("next rules = %#v", next.Rules)
	}
	if findRecordedOp(rec.ops, libovsdb.OperationDelete, tableQoS) == nil {
		t.Fatalf("patch did not delete replaced stale rule: %#v", rec.ops)
	}
}

type qosRecordingExecutor struct {
	mu      sync.Mutex
	ops     []libovsdb.Operation
	results []libovsdb.OperationResult
}

func (r *qosRecordingExecutor) Transact(_ context.Context, ops ...libovsdb.Operation) ([]libovsdb.OperationResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ops = append(r.ops, ops...)
	if r.results != nil {
		if len(r.results) < len(ops) {
			out := append([]libovsdb.OperationResult{}, r.results...)
			r.results = nil
			for len(out) < len(ops) {
				out = append(out, libovsdb.OperationResult{Count: 1})
			}
			return out, nil
		}
		out := append([]libovsdb.OperationResult{}, r.results[:len(ops)]...)
		r.results = r.results[len(ops):]
		return out, nil
	}
	return []libovsdb.OperationResult{{Count: 1}}, nil
}

func (r *qosRecordingExecutor) List(context.Context, any) error {
	return nil
}

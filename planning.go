package ovnflow

import "context"

type IntentAction string

const (
	IntentActionEnsure  IntentAction = "ensure"
	IntentActionDelete  IntentAction = "delete"
	IntentActionInspect IntentAction = "inspect"
	IntentActionPatch   IntentAction = "patch"
)

type PlannedOperation struct {
	Action      IntentAction
	Resource    string
	Name        string
	Description string
}

type Plan struct {
	Operations []PlannedOperation
	Warnings   []string
}

func (p Plan) Empty() bool {
	return len(p.Operations) == 0 && len(p.Warnings) == 0
}

type DiffChange struct {
	Path   string
	Before any
	After  any
}

type Diff struct {
	Resource string
	Name     string
	Changes  []DiffChange
}

type DryRunResult struct {
	Plan Plan
	Diff Diff
}

type ReconcileResult struct {
	Plan    Plan
	Applied bool
}

type InspectResult struct {
	Resource string
	Name     string
	Status   any
}

type IntentPlanner interface {
	Plan(context.Context) (Plan, error)
}

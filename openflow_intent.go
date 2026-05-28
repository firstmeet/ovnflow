package ovnflow

import (
	"context"
	"hash/fnv"
	"strings"
)

const (
	openFlowCookieNamespace uint64 = 0x0f0f000000000000
	openFlowCookieMask      uint64 = 0xffff000000000000
)

type OpenFlowBridgeRef struct {
	client *OpenFlowClient
	name   string
}

type OpenFlowRuleRef struct {
	bridge *OpenFlowBridgeRef
	name   string
}

type OpenFlowRuleBuilder struct {
	once   useOnce
	ref    *OpenFlowRuleRef
	flow   OpenFlowFlow
	delete bool
}

type OpenFlowActionsBuilder struct {
	parent *OpenFlowRuleBuilder
}

func (r *OpenFlowBridgeRef) EnsureFlow(name string) *OpenFlowRuleBuilder {
	ref := &OpenFlowRuleRef{bridge: r, name: name}
	return &OpenFlowRuleBuilder{ref: ref, flow: OpenFlowFlow{
		Name:       name,
		Bridge:     r.name,
		Cookie:     openFlowCookieForName(r.name, name),
		CookieMask: openFlowCookieMask,
		TableID:    0,
		Priority:   100,
		Labels:     Labels{},
	}}
}

func (r *OpenFlowBridgeRef) DeleteFlow(name string) *OpenFlowRuleBuilder {
	builder := r.EnsureFlow(name)
	builder.delete = true
	builder.flow.TableID = 0xff
	builder.flow.CookieMask = ^uint64(0)
	return builder
}

func (b *OpenFlowRuleBuilder) Table(id uint8) *OpenFlowRuleBuilder {
	b.flow.TableID = id
	return b
}

func (b *OpenFlowRuleBuilder) Priority(priority uint16) *OpenFlowRuleBuilder {
	b.flow.Priority = priority
	return b
}

func (b *OpenFlowRuleBuilder) Cookie(cookie uint64) *OpenFlowRuleBuilder {
	b.flow.Cookie = cookie
	return b
}

func (b *OpenFlowRuleBuilder) CookieMask(mask uint64) *OpenFlowRuleBuilder {
	b.flow.CookieMask = mask
	return b
}

func (b *OpenFlowRuleBuilder) InPort(port uint32) *OpenFlowRuleBuilder {
	b.flow.Match.InPort = &port
	return b
}

func (b *OpenFlowRuleBuilder) EthType(value uint16) *OpenFlowRuleBuilder {
	b.flow.Match.EthType = &value
	return b
}

func (b *OpenFlowRuleBuilder) IPv4Src(cidr string) *OpenFlowRuleBuilder {
	b.flow.Match.IPv4Src = cidr
	return b
}

func (b *OpenFlowRuleBuilder) IPv4Dst(cidr string) *OpenFlowRuleBuilder {
	b.flow.Match.IPv4Dst = cidr
	return b
}

func (b *OpenFlowRuleBuilder) IPProto(proto uint8) *OpenFlowRuleBuilder {
	b.flow.Match.IPProto = &proto
	return b
}

func (b *OpenFlowRuleBuilder) TCPSrc(port uint16) *OpenFlowRuleBuilder {
	b.flow.Match.TCPSrc = &port
	return b
}

func (b *OpenFlowRuleBuilder) TCPDst(port uint16) *OpenFlowRuleBuilder {
	b.flow.Match.TCPDst = &port
	return b
}

func (b *OpenFlowRuleBuilder) UDPSrc(port uint16) *OpenFlowRuleBuilder {
	b.flow.Match.UDPSrc = &port
	return b
}

func (b *OpenFlowRuleBuilder) UDPDst(port uint16) *OpenFlowRuleBuilder {
	b.flow.Match.UDPDst = &port
	return b
}

func (b *OpenFlowRuleBuilder) WithOwner(kind, name string) *OpenFlowRuleBuilder {
	b.flow.Owner = OwnerRef{Kind: kind, Name: name}
	return b
}

func (b *OpenFlowRuleBuilder) WithLabel(key, value string) *OpenFlowRuleBuilder {
	if b.flow.Labels == nil {
		b.flow.Labels = Labels{}
	}
	b.flow.Labels[key] = value
	return b
}

func (b *OpenFlowRuleBuilder) Actions() *OpenFlowActionsBuilder {
	return &OpenFlowActionsBuilder{parent: b}
}

func (a *OpenFlowActionsBuilder) Output(port uint32) *OpenFlowRuleBuilder {
	a.parent.flow.Actions = append(a.parent.flow.Actions, OpenFlowAction{Type: OpenFlowActionOutput, Port: port})
	return a.parent
}

func (a *OpenFlowActionsBuilder) SetField(field, value string) *OpenFlowRuleBuilder {
	a.parent.flow.Actions = append(a.parent.flow.Actions, OpenFlowAction{Type: OpenFlowActionSetField, Field: field, Value: value})
	return a.parent
}

func (b *OpenFlowRuleBuilder) Validate() error {
	if b == nil || b.ref == nil || b.ref.bridge == nil || b.ref.bridge.client == nil {
		return ErrBackendUnavailable
	}
	if err := validateName("openflow bridge", b.flow.Bridge); err != nil {
		return err
	}
	if err := validateName("openflow flow", b.flow.Name); err != nil {
		return err
	}
	if b.flow.Owner.Kind != "" || b.flow.Owner.Name != "" || b.flow.Owner.ID != "" {
		if err := b.flow.Owner.Validate(); err != nil {
			return err
		}
	}
	if err := b.flow.Labels.Validate(); err != nil {
		return err
	}
	if !b.delete && len(b.flow.Actions) == 0 && len(b.flow.Instructions) == 0 {
		return wrap(ErrorValidation, dbOpenFlow, "", "validate", b.flow.Name, "at least one OpenFlow action or instruction is required", nil)
	}
	if b.flow.Cookie&openFlowCookieMask != openFlowCookieNamespace {
		return wrap(ErrorValidation, dbOpenFlow, "", "validate", b.flow.Name, "OpenFlow cookie must stay in the ovnflow ownership namespace", nil)
	}
	if b.flow.CookieMask == 0 {
		b.flow.CookieMask = openFlowCookieMask
	}
	return nil
}

func (b *OpenFlowRuleBuilder) Plan(ctx context.Context) (Plan, error) {
	if err := b.Validate(); err != nil {
		return Plan{}, err
	}
	action := IntentActionEnsure
	description := "validate and apply native OpenFlow rule"
	if b.delete {
		action = IntentActionDelete
		description = "delete owned native OpenFlow rule"
	}
	plan := Plan{Operations: []PlannedOperation{{
		Action:      action,
		Resource:    "OpenFlowRule",
		Name:        b.flow.Name,
		Description: description,
	}}}
	if b.ref.bridge.client.config.AutoConfigureBridgeController {
		plan.Operations = append([]PlannedOperation{{
			Action:      IntentActionEnsure,
			Resource:    "OpenFlowController",
			Name:        b.flow.Bridge,
			Description: "configure bridge controller target before OpenFlow connect",
		}}, plan.Operations...)
	}
	return plan, nil
}

func (b *OpenFlowRuleBuilder) DryRun(ctx context.Context) (DryRunResult, error) {
	plan, err := b.Plan(ctx)
	if err != nil {
		return DryRunResult{}, err
	}
	after := b.flow
	if b.delete {
		return DryRunResult{Plan: plan, Diff: Diff{Resource: "OpenFlowRule", Name: b.flow.Name, Changes: []DiffChange{{Path: "/", Before: b.flow, After: nil}}}}, nil
	}
	return DryRunResult{Plan: plan, Diff: Diff{Resource: "OpenFlowRule", Name: b.flow.Name, Changes: []DiffChange{{Path: "/", Before: nil, After: after}}}}, nil
}

func (b *OpenFlowRuleBuilder) Execute(ctx context.Context) error {
	if !b.once.mark() {
		return wrap(ErrorValidation, dbOpenFlow, "", "execute", b.flow.Name, "builder already executed", nil)
	}
	if _, err := b.Plan(ctx); err != nil {
		return err
	}
	client := b.ref.bridge.client
	if client.config.AutoConfigureBridgeController {
		if err := client.ConfigureBridgeController(ctx, b.flow.Bridge); err != nil {
			return err
		}
	}
	session, err := client.Dial(ctx)
	if err != nil {
		return err
	}
	defer session.Close()
	if b.delete {
		return session.DeleteFlow(ctx, b.flow)
	}
	return session.AddFlow(ctx, b.flow)
}

func (b *OpenFlowRuleBuilder) Flow() OpenFlowFlow {
	return cloneOpenFlowFlow(b.flow)
}

func openFlowCookieForName(bridge, name string) uint64 {
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(strings.ToLower(bridge)))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write([]byte(strings.ToLower(name)))
	return openFlowCookieNamespace | (hash.Sum64() & ^openFlowCookieMask)
}

func cloneOpenFlowFlow(in OpenFlowFlow) OpenFlowFlow {
	out := in
	out.Instructions = append([]OpenFlowInstruction{}, in.Instructions...)
	out.Actions = append([]OpenFlowAction{}, in.Actions...)
	out.Labels = cloneLabels(in.Labels)
	return out
}

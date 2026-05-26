package ovnflow

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestOwnerExternalIDsEncodeReservedLabels(t *testing.T) {
	ids, err := (OwnerRef{Kind: "project", Name: "alpha"}).ExternalIDs(Labels{"team/name": "net"})
	if err != nil {
		t.Fatalf("ExternalIDs returned error: %v", err)
	}
	key := ExternalIDLabelKey("team/name")
	if ids[ExternalIDManagedByKey] != "ovnflow" || ids[ExternalIDOwnerKindKey] != "project" || ids[key] != "net" {
		t.Fatalf("external ids = %#v", ids)
	}
	decoded, ok := DecodeExternalIDLabelKey(key)
	if !ok || decoded != "team/name" {
		t.Fatalf("DecodeExternalIDLabelKey = %q, %v", decoded, ok)
	}
	if !IsReservedExternalIDKey(key) {
		t.Fatalf("%q should be reserved", key)
	}
}

func TestIntentValidationFailures(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{name: "virtual network cidr", err: (VirtualNetwork{Name: "net", CIDRs: []string{"bad"}}).Validate()},
		{name: "dns ip", err: (LogicalSwitchDNS{Records: []DNSRecord{{Domain: "svc.local", IPs: []string{"bad"}}}}).Validate()},
		{name: "workload mac", err: (WorkloadAttachment{Name: "att", Network: "net", MAC: "bad"}).Validate()},
		{name: "workload owner", err: (WorkloadAttachment{Name: "att", Network: "net", Owner: OwnerRef{Kind: "project"}}).Validate()},
		{name: "security cidr", err: (SecurityPolicy{Name: "pol", Rules: []SecurityRule{{Action: "allow", CIDRs: []string{"bad"}}}}).Validate()},
		{name: "security owner", err: (SecurityPolicy{Name: "pol", Owner: OwnerRef{Kind: "project"}}).Validate()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !IsKind(tt.err, ErrorValidation) {
				t.Fatalf("error kind = %q for %v, want validation", KindOf(tt.err), tt.err)
			}
		})
	}
}

func TestLogicalSwitchDNSAllowsMultipleIPsPerDomain(t *testing.T) {
	dns := LogicalSwitchDNS{Records: []DNSRecord{
		{Domain: "api.service", IPs: []string{"10.0.0.2"}},
		{Domain: "api.service", IPs: []string{"10.0.0.3", "10.0.0.1"}},
	}}
	got := dns.RecordMap()["api.service"]
	want := []string{"10.0.0.1", "10.0.0.2", "10.0.0.3"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("record ips = %#v, want %#v", got, want)
	}
}

func TestVirtualNetworkNoopPlanAndReconcile(t *testing.T) {
	builder := (&NBClient{}).VirtualNetwork("net-a").
		Ensure().
		WithCIDR("10.0.0.0/24").
		WithGateway("10.0.0.1").
		WithDNS("net-a", func(d *LogicalSwitchDNSBuilder) {
			d.AddRecord("api.service", "10.0.0.2", "10.0.0.3")
		})
	dryRun, err := builder.DryRun(context.Background())
	if err != nil {
		t.Fatalf("DryRun returned error: %v", err)
	}
	if len(dryRun.Plan.Operations) != 1 || dryRun.Plan.Operations[0].Resource != "VirtualNetwork" {
		t.Fatalf("dry run plan = %#v", dryRun.Plan)
	}
	reconciled, err := builder.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}
	if reconciled.Applied {
		t.Fatalf("Reconcile applied real changes in interface-freeze stub")
	}
}

func TestAttachmentAndPolicyBuildersAreSymmetric(t *testing.T) {
	ctx := context.Background()
	attachment := (&NBClient{}).WorkloadAttachment("att-a").
		Ensure().
		OnNetwork("net-a").
		WithInterface("eth0").
		WithMAC("00:16:3e:11:22:33").
		WithIP("10.0.0.10")
	if _, err := attachment.DryRun(ctx); err != nil {
		t.Fatalf("attachment DryRun returned error: %v", err)
	}
	if reconciled, err := attachment.Reconcile(ctx); err != nil || reconciled.Applied {
		t.Fatalf("attachment Reconcile = %#v, %v", reconciled, err)
	}
	if err := attachment.Execute(ctx); err != nil {
		t.Fatalf("attachment Execute returned error: %v", err)
	}

	policy := (&NBClient{}).SecurityPolicy("allow-web").
		Ensure().
		ForSubject("net-a").
		AddRule(SecurityRule{Action: "allow", CIDRs: []string{"10.0.0.0/24"}, Ports: []int{80}})
	if _, err := policy.DryRun(ctx); err != nil {
		t.Fatalf("policy DryRun returned error: %v", err)
	}
	if reconciled, err := policy.Reconcile(ctx); err != nil || reconciled.Applied {
		t.Fatalf("policy Reconcile = %#v, %v", reconciled, err)
	}
	if err := policy.Execute(ctx); err != nil {
		t.Fatalf("policy Execute returned error: %v", err)
	}
}

func TestTypedErrorsIncludeV2Kinds(t *testing.T) {
	err := &Error{Kind: ErrorAmbiguous}
	if !errors.Is(err, ErrAmbiguous) {
		t.Fatalf("errors.Is did not match ambiguous")
	}
}

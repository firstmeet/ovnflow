package ovnflow

import "testing"

func TestPlanIPAllocationDefaultGateway(t *testing.T) {
	pool := IPAMPool{CIDR: "10.42.0.0/29"}
	plan, err := PlanIPAllocation(pool)
	if err != nil {
		t.Fatalf("PlanIPAllocation() error = %v", err)
	}
	if plan.Gateway != "10.42.0.1" {
		t.Fatalf("gateway = %q, want %q", plan.Gateway, "10.42.0.1")
	}
	if plan.Next != "10.42.0.2" {
		t.Fatalf("next = %q, want %q", plan.Next, "10.42.0.2")
	}
	if plan.Available != 5 {
		t.Fatalf("available = %d, want 5", plan.Available)
	}
}

func TestIPAMReservedAddresses(t *testing.T) {
	pool := IPAMPool{
		CIDR:     "10.42.1.0/29",
		Reserved: []string{"10.42.1.2"},
	}
	if _, err := pool.Allocate("10.42.1.2"); !IsKind(err, ErrorConflict) {
		t.Fatalf("Allocate(reserved) error = %v, want ErrorConflict", err)
	}
	ip, err := pool.Allocate()
	if err != nil {
		t.Fatalf("Allocate() error = %v", err)
	}
	if ip != "10.42.1.3" {
		t.Fatalf("Allocate() = %q, want %q", ip, "10.42.1.3")
	}
}

func TestIPAMSequentialAllocation(t *testing.T) {
	pool := IPAMPool{CIDR: "10.42.2.0/29"}
	for _, want := range []string{"10.42.2.2", "10.42.2.3", "10.42.2.4"} {
		got, err := pool.Allocate()
		if err != nil {
			t.Fatalf("Allocate() error = %v", err)
		}
		if got != want {
			t.Fatalf("Allocate() = %q, want %q", got, want)
		}
	}
}

func TestIPAMSpecificAllocation(t *testing.T) {
	pool := IPAMPool{CIDR: "10.42.3.0/29"}
	ip, err := pool.Allocate("10.42.3.5")
	if err != nil {
		t.Fatalf("Allocate(specific) error = %v", err)
	}
	if ip != "10.42.3.5" {
		t.Fatalf("Allocate(specific) = %q, want %q", ip, "10.42.3.5")
	}
	if _, err := pool.Allocate("10.42.3.5"); !IsKind(err, ErrorConflict) {
		t.Fatalf("Allocate(duplicate) error = %v, want ErrorConflict", err)
	}
}

func TestIPAMRelease(t *testing.T) {
	pool := IPAMPool{CIDR: "10.42.4.0/29"}
	ip, err := pool.Allocate()
	if err != nil {
		t.Fatalf("Allocate() error = %v", err)
	}
	if err := pool.Release(ip); err != nil {
		t.Fatalf("Release() error = %v", err)
	}
	if _, err := pool.Allocate(ip); err != nil {
		t.Fatalf("Allocate(released) error = %v", err)
	}
	if err := pool.Release("10.42.4.6"); !IsKind(err, ErrorNotFound) {
		t.Fatalf("Release(unallocated) error = %v, want ErrorNotFound", err)
	}
}

func TestIPAMCIDRConflict(t *testing.T) {
	pool := IPAMPool{
		CIDR:     "10.42.5.0/28",
		Excluded: []string{"10.42.5.8/30"},
	}
	overlaps, err := pool.Overlaps("10.42.5.12/30")
	if err != nil {
		t.Fatalf("Overlaps() error = %v", err)
	}
	if !overlaps {
		t.Fatal("Overlaps() = false, want true")
	}
	if _, err := pool.Allocate("10.42.5.9"); !IsKind(err, ErrorConflict) {
		t.Fatalf("Allocate(excluded) error = %v, want ErrorConflict", err)
	}
	bad := IPAMPool{CIDR: "10.42.5.0/28", Excluded: []string{"10.42.6.0/30"}}
	if _, err := PlanIPAllocation(bad); !IsKind(err, ErrorConflict) {
		t.Fatalf("PlanIPAllocation(non-overlapping exclusion) error = %v, want ErrorConflict", err)
	}
}

func TestIPAMPoolExhaustion(t *testing.T) {
	pool := IPAMPool{CIDR: "10.42.6.0/30"}
	if ip, err := pool.Allocate(); err != nil || ip != "10.42.6.2" {
		t.Fatalf("Allocate() = %q, %v; want 10.42.6.2, nil", ip, err)
	}
	_, err := pool.Allocate()
	if !IsKind(err, ErrorNotFound) {
		t.Fatalf("Allocate(exhausted) error = %v, want ErrorNotFound", err)
	}
	available, err := pool.Available()
	if err != nil {
		t.Fatalf("Available() error = %v", err)
	}
	if available != 0 {
		t.Fatalf("Available() = %d, want 0", available)
	}
}

func TestIPAMNoUsableHostsFor31And32(t *testing.T) {
	for _, cidr := range []string{"10.42.6.0/31", "10.42.6.1/32"} {
		t.Run(cidr, func(t *testing.T) {
			if _, err := PlanIPAllocation(IPAMPool{CIDR: cidr}); !IsKind(err, ErrorConflict) {
				t.Fatalf("PlanIPAllocation(%s) error = %v, want ErrorConflict", cidr, err)
			}
		})
	}
}

func TestIPAMGatewayAndReservedCannotBeExcluded(t *testing.T) {
	pool := IPAMPool{CIDR: "10.42.6.0/29", Excluded: []string{"10.42.6.0/30"}}
	if _, err := PlanIPAllocation(pool); !IsKind(err, ErrorConflict) {
		t.Fatalf("PlanIPAllocation(excluded gateway) error = %v, want ErrorConflict", err)
	}

	pool = IPAMPool{CIDR: "10.42.6.0/29", Gateway: "10.42.6.1", Reserved: []string{"10.42.6.5"}, Excluded: []string{"10.42.6.4/31"}}
	if _, err := PlanIPAllocation(pool); !IsKind(err, ErrorConflict) {
		t.Fatalf("PlanIPAllocation(excluded reserved) error = %v, want ErrorConflict", err)
	}
}

func TestIPAMIPv6Unsupported(t *testing.T) {
	pool := IPAMPool{CIDR: "fd00::/64"}
	if _, err := PlanIPAllocation(pool); !IsKind(err, ErrorUnsupported) {
		t.Fatalf("PlanIPAllocation(IPv6) error = %v, want ErrorUnsupported", err)
	}
	ipv4Pool := IPAMPool{CIDR: "10.42.7.0/29"}
	if _, err := ipv4Pool.Allocate("fd00::10"); !IsKind(err, ErrorUnsupported) {
		t.Fatalf("Allocate(IPv6) error = %v, want ErrorUnsupported", err)
	}
}

func TestIPAMContainsAndAvailable(t *testing.T) {
	pool := IPAMPool{
		CIDR:      "10.42.8.0/29",
		Reserved:  []string{"10.42.8.2"},
		Allocated: []string{"10.42.8.3"},
		Excluded:  []string{"10.42.8.4/31"},
	}
	contains, err := pool.Contains("10.42.8.6")
	if err != nil {
		t.Fatalf("Contains() error = %v", err)
	}
	if !contains {
		t.Fatal("Contains() = false, want true")
	}
	available, err := pool.Available()
	if err != nil {
		t.Fatalf("Available() error = %v", err)
	}
	if available != 1 {
		t.Fatalf("Available() = %d, want 1", available)
	}
}

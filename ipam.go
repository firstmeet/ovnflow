package ovnflow

import (
	"fmt"
	"net/netip"
	"sort"
)

const ipamTable = "ipam"

// IPAMPool describes an in-memory IPv4 address pool for preflight planning.
//
// Gateway defaults to the first usable address in CIDR when omitted. Reserved
// and Excluded addresses are never allocated. Allocated is updated by Allocate
// and Release, but no state is persisted outside this struct.
type IPAMPool struct {
	CIDR      string
	Gateway   string
	Reserved  []string
	Allocated []string
	Excluded  []string
}

// IPAllocationPlan is a snapshot of an IPAMPool's effective allocation state.
type IPAllocationPlan struct {
	PoolCIDR  string
	Gateway   string
	Reserved  []string
	Allocated []string
	Excluded  []string
	Available int
	Next      string
}

// NewIPAMPool returns a pool with the requested CIDR. The pool is validated by
// planning and allocation helpers so callers can build it incrementally.
func NewIPAMPool(cidr string) *IPAMPool {
	return &IPAMPool{CIDR: cidr}
}

// PlanIPAllocation validates pool and returns its effective gateway, exclusions,
// availability count, and next sequential allocation candidate.
func PlanIPAllocation(pool IPAMPool) (IPAllocationPlan, error) {
	state, err := pool.state("plan")
	if err != nil {
		return IPAllocationPlan{}, err
	}
	next, err := state.nextAvailable()
	if err != nil && !IsKind(err, ErrorNotFound) {
		return IPAllocationPlan{}, err
	}
	available, err := state.available()
	if err != nil {
		return IPAllocationPlan{}, err
	}
	plan := IPAllocationPlan{
		PoolCIDR:  state.prefix.String(),
		Gateway:   state.gateway.String(),
		Reserved:  cloneStrings(pool.Reserved),
		Allocated: cloneStrings(pool.Allocated),
		Excluded:  cloneStrings(pool.Excluded),
		Available: available,
	}
	if err == nil {
		plan.Next = next.String()
	}
	return plan, nil
}

// Allocate reserves the requested IPv4 address, or the next sequential address
// when requested is omitted. The returned address is appended to pool.Allocated.
func (pool *IPAMPool) Allocate(requested ...string) (string, error) {
	if pool == nil {
		return "", wrap(ErrorValidation, "", ipamTable, "allocate", "", "pool must not be nil", nil)
	}
	if len(requested) > 1 {
		return "", wrap(ErrorValidation, "", ipamTable, "allocate", "", "at most one requested IP is supported", nil)
	}
	state, err := pool.state("allocate")
	if err != nil {
		return "", err
	}
	var ip netip.Addr
	if len(requested) == 0 || requested[0] == "" {
		ip, err = state.nextAvailable()
		if err != nil {
			return "", err
		}
	} else {
		ip, err = parseIPv4Addr(requested[0], "allocate")
		if err != nil {
			return "", err
		}
		if err := state.ensureAvailable(ip); err != nil {
			return "", err
		}
	}
	pool.Allocated = append(pool.Allocated, ip.String())
	return ip.String(), nil
}

// Release removes ip from pool.Allocated.
func (pool *IPAMPool) Release(ip string) error {
	if pool == nil {
		return wrap(ErrorValidation, "", ipamTable, "release", "", "pool must not be nil", nil)
	}
	parsed, err := parseIPv4Addr(ip, "release")
	if err != nil {
		return err
	}
	for i, allocated := range pool.Allocated {
		current, err := parseIPv4Addr(allocated, "release")
		if err != nil {
			return err
		}
		if current == parsed {
			pool.Allocated = append(pool.Allocated[:i], pool.Allocated[i+1:]...)
			return nil
		}
	}
	return wrap(ErrorNotFound, "", ipamTable, "release", parsed.String(), "IP is not allocated", nil)
}

// Contains reports whether ip is inside pool.CIDR.
func (pool IPAMPool) Contains(ip string) (bool, error) {
	state, err := pool.state("contains")
	if err != nil {
		return false, err
	}
	parsed, err := parseIPv4Addr(ip, "contains")
	if err != nil {
		return false, err
	}
	return state.prefix.Contains(parsed), nil
}

// Available reports the number of currently allocatable IPv4 addresses.
func (pool IPAMPool) Available() (int, error) {
	state, err := pool.state("available")
	if err != nil {
		return 0, err
	}
	return state.available()
}

// Overlaps reports whether cidr intersects pool.CIDR.
func (pool IPAMPool) Overlaps(cidr string) (bool, error) {
	state, err := pool.state("overlaps")
	if err != nil {
		return false, err
	}
	other, err := parseIPv4Prefix(cidr, "overlaps")
	if err != nil {
		return false, err
	}
	return prefixesOverlap(state.prefix, other), nil
}

type ipamState struct {
	prefix    netip.Prefix
	gateway   netip.Addr
	reserved  map[netip.Addr]struct{}
	allocated map[netip.Addr]struct{}
	excluded  []netip.Prefix
}

type ipRange struct {
	start uint32
	end   uint32
}

func (pool IPAMPool) state(op string) (ipamState, error) {
	prefix, err := parseIPv4Prefix(pool.CIDR, op)
	if err != nil {
		return ipamState{}, err
	}
	start, end := usableRange(prefix)
	if start > end {
		return ipamState{}, wrap(ErrorConflict, "", ipamTable, op, prefix.String(), "pool has no usable IPv4 addresses", nil)
	}
	state := ipamState{
		prefix:    prefix,
		reserved:  make(map[netip.Addr]struct{}),
		allocated: make(map[netip.Addr]struct{}),
	}
	if pool.Gateway == "" {
		state.gateway = uint32ToAddr(start)
	} else {
		state.gateway, err = parseIPv4Addr(pool.Gateway, op)
		if err != nil {
			return ipamState{}, err
		}
		if err := validateUsable(prefix, state.gateway, op, "gateway"); err != nil {
			return ipamState{}, err
		}
	}
	for _, raw := range pool.Reserved {
		ip, err := parseIPv4Addr(raw, op)
		if err != nil {
			return ipamState{}, err
		}
		if err := validateUsable(prefix, ip, op, "reserved IP"); err != nil {
			return ipamState{}, err
		}
		state.reserved[ip] = struct{}{}
	}
	for _, raw := range pool.Allocated {
		ip, err := parseIPv4Addr(raw, op)
		if err != nil {
			return ipamState{}, err
		}
		if err := validateUsable(prefix, ip, op, "allocated IP"); err != nil {
			return ipamState{}, err
		}
		if _, ok := state.allocated[ip]; ok {
			return ipamState{}, wrap(ErrorConflict, "", ipamTable, op, ip.String(), "IP is allocated more than once", nil)
		}
		if state.isReserved(ip) {
			return ipamState{}, wrap(ErrorConflict, "", ipamTable, op, ip.String(), "allocated IP is reserved", nil)
		}
		state.allocated[ip] = struct{}{}
	}
	for _, raw := range pool.Excluded {
		excluded, err := parseIPv4Prefix(raw, op)
		if err != nil {
			return ipamState{}, err
		}
		if !prefixesOverlap(prefix, excluded) {
			return ipamState{}, wrap(ErrorConflict, "", ipamTable, op, excluded.String(), "excluded CIDR does not overlap pool", nil)
		}
		state.excluded = append(state.excluded, excluded)
	}
	if state.inExcluded(state.gateway) {
		return ipamState{}, wrap(ErrorConflict, "", ipamTable, op, state.gateway.String(), "gateway IP is excluded", nil)
	}
	for ip := range state.reserved {
		if state.inExcluded(ip) {
			return ipamState{}, wrap(ErrorConflict, "", ipamTable, op, ip.String(), "reserved IP is excluded", nil)
		}
	}
	for ip := range state.allocated {
		if state.inExcluded(ip) {
			return ipamState{}, wrap(ErrorConflict, "", ipamTable, op, ip.String(), "allocated IP is excluded", nil)
		}
	}
	return state, nil
}

func (state ipamState) nextAvailable() (netip.Addr, error) {
	start, end := usableRange(state.prefix)
	for current := start; current <= end; current++ {
		ip := uint32ToAddr(current)
		if state.isAvailable(ip) {
			return ip, nil
		}
		if current == ^uint32(0) {
			break
		}
	}
	return netip.Addr{}, wrap(ErrorNotFound, "", ipamTable, "allocate", state.prefix.String(), "pool exhausted", nil)
}

func (state ipamState) ensureAvailable(ip netip.Addr) error {
	if err := validateUsable(state.prefix, ip, "allocate", "requested IP"); err != nil {
		return err
	}
	if state.isReserved(ip) {
		return wrap(ErrorConflict, "", ipamTable, "allocate", ip.String(), "IP is reserved", nil)
	}
	if _, ok := state.allocated[ip]; ok {
		return wrap(ErrorConflict, "", ipamTable, "allocate", ip.String(), "IP is already allocated", nil)
	}
	if state.inExcluded(ip) {
		return wrap(ErrorConflict, "", ipamTable, "allocate", ip.String(), "IP is excluded", nil)
	}
	return nil
}

func (state ipamState) isAvailable(ip netip.Addr) bool {
	return validateUsable(state.prefix, ip, "allocate", "IP") == nil &&
		!state.isReserved(ip) &&
		!state.inExcluded(ip) &&
		!state.isAllocated(ip)
}

func (state ipamState) isAllocated(ip netip.Addr) bool {
	_, ok := state.allocated[ip]
	return ok
}

func (state ipamState) isReserved(ip netip.Addr) bool {
	if ip == state.gateway {
		return true
	}
	_, ok := state.reserved[ip]
	return ok
}

func (state ipamState) inExcluded(ip netip.Addr) bool {
	for _, excluded := range state.excluded {
		if excluded.Contains(ip) {
			return true
		}
	}
	return false
}

func (state ipamState) available() (int, error) {
	start, end := usableRange(state.prefix)
	if start > end {
		return 0, nil
	}
	blocked := []ipRange{{start: addrToUint32(state.gateway), end: addrToUint32(state.gateway)}}
	for ip := range state.reserved {
		blocked = append(blocked, ipRange{start: addrToUint32(ip), end: addrToUint32(ip)})
	}
	for ip := range state.allocated {
		blocked = append(blocked, ipRange{start: addrToUint32(ip), end: addrToUint32(ip)})
	}
	for _, excluded := range state.excluded {
		exStart, exEnd := prefixRange(excluded)
		if exEnd < start || exStart > end {
			continue
		}
		blocked = append(blocked, ipRange{start: max32(start, exStart), end: min32(end, exEnd)})
	}
	merged := mergeRanges(blocked)
	total := uint64(end) - uint64(start) + 1
	for _, item := range merged {
		total -= uint64(item.end) - uint64(item.start) + 1
	}
	if total > uint64(^uint(0)>>1) {
		return 0, wrap(ErrorConflict, "", ipamTable, "available", state.prefix.String(), "available count exceeds int range", nil)
	}
	return int(total), nil
}

func parseIPv4Prefix(raw, op string) (netip.Prefix, error) {
	prefix, err := netip.ParsePrefix(raw)
	if err != nil {
		return netip.Prefix{}, wrap(ErrorValidation, "", ipamTable, op, raw, "invalid CIDR", err)
	}
	prefix = prefix.Masked()
	if !prefix.Addr().Is4() {
		return netip.Prefix{}, wrap(ErrorUnsupported, "", ipamTable, op, raw, "IPv6 IPAM is not supported", nil)
	}
	return prefix, nil
}

func parseIPv4Addr(raw, op string) (netip.Addr, error) {
	ip, err := netip.ParseAddr(raw)
	if err != nil {
		return netip.Addr{}, wrap(ErrorValidation, "", ipamTable, op, raw, "invalid IP", err)
	}
	if !ip.Is4() {
		return netip.Addr{}, wrap(ErrorUnsupported, "", ipamTable, op, raw, "IPv6 IPAM is not supported", nil)
	}
	return ip, nil
}

func validateUsable(prefix netip.Prefix, ip netip.Addr, op, label string) error {
	if !prefix.Contains(ip) {
		return wrap(ErrorValidation, "", ipamTable, op, ip.String(), fmt.Sprintf("%s is outside pool CIDR", label), nil)
	}
	start, end := usableRange(prefix)
	value := addrToUint32(ip)
	if value < start || value > end {
		return wrap(ErrorValidation, "", ipamTable, op, ip.String(), fmt.Sprintf("%s is not a usable host address", label), nil)
	}
	return nil
}

func usableRange(prefix netip.Prefix) (uint32, uint32) {
	start, end := prefixRange(prefix)
	if end-start+1 <= 2 {
		return 1, 0
	}
	return start + 1, end - 1
}

func prefixRange(prefix netip.Prefix) (uint32, uint32) {
	start := addrToUint32(prefix.Addr())
	hostBits := 32 - prefix.Bits()
	var size uint64 = 1
	size <<= hostBits
	end := start + uint32(size-1)
	return start, end
}

func prefixesOverlap(a, b netip.Prefix) bool {
	aStart, aEnd := prefixRange(a)
	bStart, bEnd := prefixRange(b)
	return aStart <= bEnd && bStart <= aEnd
}

func addrToUint32(ip netip.Addr) uint32 {
	bytes := ip.As4()
	return uint32(bytes[0])<<24 | uint32(bytes[1])<<16 | uint32(bytes[2])<<8 | uint32(bytes[3])
}

func uint32ToAddr(ip uint32) netip.Addr {
	return netip.AddrFrom4([4]byte{byte(ip >> 24), byte(ip >> 16), byte(ip >> 8), byte(ip)})
}

func mergeRanges(ranges []ipRange) []ipRange {
	if len(ranges) == 0 {
		return nil
	}
	sort.Slice(ranges, func(i, j int) bool {
		if ranges[i].start == ranges[j].start {
			return ranges[i].end < ranges[j].end
		}
		return ranges[i].start < ranges[j].start
	})
	merged := []ipRange{ranges[0]}
	for _, item := range ranges[1:] {
		last := &merged[len(merged)-1]
		if item.start <= last.end+1 {
			if item.end > last.end {
				last.end = item.end
			}
			continue
		}
		merged = append(merged, item)
	}
	return merged
}

func cloneStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}

func min32(a, b uint32) uint32 {
	if a < b {
		return a
	}
	return b
}

func max32(a, b uint32) uint32 {
	if a > b {
		return a
	}
	return b
}

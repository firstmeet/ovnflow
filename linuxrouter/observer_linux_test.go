//go:build linux

package linuxrouter

import (
	"testing"

	"github.com/firstmeet/ovnflow/v2"
)

func TestParseIPAddrJSON(t *testing.T) {
	data := []byte(`[
		{"ifname":"lo","operstate":"UNKNOWN","flags":["LOOPBACK","UP"],"addr_info":[{"local":"127.0.0.1","prefixlen":8}]},
		{"ifname":"wan0","operstate":"UP","flags":["BROADCAST","UP"],"addr_info":[{"local":"192.0.2.2","prefixlen":24},{"local":"2001:db8::2","prefixlen":64}]}
	]`)
	got, err := parseIPAddrJSON(data, map[string]InterfaceRole{"wan0": InterfaceWAN})
	if err != nil {
		t.Fatalf("parseIPAddrJSON returned error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("interface count = %d, want 2: %#v", len(got), got)
	}
	if got[1].Name != "wan0" || got[1].Role != InterfaceWAN || !got[1].Up || len(got[1].Addresses) != 2 {
		t.Fatalf("wan0 status = %#v", got[1])
	}
}

func TestParseIPRouteJSON(t *testing.T) {
	data := []byte(`[
		{"dst":"default","gateway":"192.0.2.1","dev":"wan0"},
		{"dst":"10.0.0.0/24","dev":"lan0"}
	]`)
	got, err := parseIPRouteJSON(data)
	if err != nil {
		t.Fatalf("parseIPRouteJSON returned error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("route count = %d, want 2: %#v", len(got), got)
	}
	if got[0].Destination != "0.0.0.0/0" || got[0].Gateway != "192.0.2.1" || got[0].Interface != "wan0" {
		t.Fatalf("default route = %#v", got[0])
	}
}

func TestParseScopedOwnedComments(t *testing.T) {
	data := []byte(`table ip ovnflow_nat {
		chain postrouting {
			ip saddr 10.0.0.0/24 oifname "wan0" masquerade comment "ovnflow:edge:egress"
			ip daddr 192.168.0.1 snat to 192.0.2.10 comment "ovnflow:edge:legacy-snat"
			ip saddr 10.1.0.0/24 oifname "wan0" masquerade comment "ovnflow:other:egress"
		}
	}
	-A POSTROUTING -m comment --comment ovnflow:edge:egress -j MASQUERADE`)
	got := parseScopedOwnedComments(data, "edge")
	if len(got) != 2 || got[0] != "egress" || got[1] != "legacy-snat" {
		t.Fatalf("owned comments = %#v", got)
	}
}

func TestParseIPTablesTableCommentsFiltersByTable(t *testing.T) {
	data := []byte(`*filter
-A FORWARD -m comment --comment ovnflow:edge:allow-web -j ACCEPT
-A FORWARD -m comment --comment ovnflow:other:allow-web -j ACCEPT
COMMIT
*nat
-A POSTROUTING -m comment --comment ovnflow:edge:egress -j MASQUERADE
-A POSTROUTING -m comment --comment ovnflow:other:egress -j MASQUERADE
COMMIT`)
	got := parseIPTablesTableComments(data, "nat", "edge")
	if len(got) != 1 || got[0] != "egress" {
		t.Fatalf("nat comments = %#v", got)
	}
	got = parseIPTablesTableComments(data, "filter", "edge")
	if len(got) != 1 || got[0] != "allow-web" {
		t.Fatalf("filter comments = %#v", got)
	}
}

func TestNormalizedNATBackend(t *testing.T) {
	if got := normalizedNATBackend(""); got != ovnflow.NATBackendAuto {
		t.Fatalf("empty backend = %q", got)
	}
	if got := normalizedNATBackend("IPTABLES"); got != ovnflow.NATBackendIPTables {
		t.Fatalf("iptables backend = %q", got)
	}
}

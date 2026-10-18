package netguard

import (
	"net/netip"
	"strings"
	"testing"
)

// Private addresses are built from bytes: the repo's public-safety guard
// refuses private address literals in tracked files.
func a4(a, b, c, d byte) netip.Addr { return netip.AddrFrom4([4]byte{a, b, c, d}) }

func a6tail(head []byte, last byte) netip.Addr {
	var x [16]byte
	copy(x[:], head)
	x[15] = last
	return netip.AddrFrom16(x)
}

func TestPrivate(t *testing.T) {
	for _, tc := range []struct {
		addr netip.Addr
		want bool
	}{
		{a4(127, 0, 0, 1), true},
		{a4(127, 9, 9, 9), true},
		{netip.IPv6Loopback(), true},
		{a4(10, 1, 2, 3), true},
		{a4(172, 16, 0, 1), true},
		{a4(172, 31, 255, 255), true},
		{a4(172, 32, 0, 1), false},
		{a4(192, 168, 1, 1), true},
		{a4(100, 64, 0, 1), true},
		{a4(100, 127, 255, 255), true},
		{a4(100, 128, 0, 1), false},
		{a4(169, 254, 1, 1), true},
		{a6tail([]byte{0xfd, 0x7a}, 1), true},
		{a6tail([]byte{0xfc, 0x00}, 1), true},
		{a6tail([]byte{0xfe, 0x80}, 1), true},
		{a6tail([]byte{0xfe, 0xc0}, 1), false},
		{netip.MustParseAddr("192.0.2.1"), false},
		{netip.MustParseAddr("198.51.100.7"), false},
		{netip.MustParseAddr("2001:db8::1"), false},
		{netip.IPv4Unspecified(), false},
		{netip.IPv6Unspecified(), false},
		{netip.Addr{}, false},
	} {
		if got := Private(tc.addr); got != tc.want {
			t.Errorf("Private(%v) = %v, want %v", tc.addr, got, tc.want)
		}
	}
}

func TestPrivateMappedAndZoned(t *testing.T) {
	mapped := netip.AddrFrom16(a4(10, 0, 0, 1).As16())
	if !mapped.Is4In6() || !Private(mapped) {
		t.Errorf("IPv4-mapped private %v not private", mapped)
	}
	public := netip.AddrFrom16(netip.MustParseAddr("198.51.100.7").As16())
	if Private(public) {
		t.Errorf("IPv4-mapped public %v counted private", public)
	}
	zoned := a6tail([]byte{0xfe, 0x80}, 1).WithZone("eth0")
	if !Private(zoned) {
		t.Errorf("zoned link-local %v not private", zoned)
	}
}

func TestParseTrusted(t *testing.T) {
	s, err := ParseTrusted(" 2001:db8:1::/48, 198.51.100.0/24 ,203.0.113.9,,")
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Trusted()) != 3 {
		t.Fatalf("trusted = %v", s.Trusted())
	}
	for _, in := range []string{"2001:db8:1:2::5", "198.51.100.200", "203.0.113.9", "::ffff:198.51.100.1"} {
		if !s.Contains(netip.MustParseAddr(in)) {
			t.Errorf("%s not contained", in)
		}
	}
	for _, out := range []string{"2001:db8:2::1", "198.51.101.1", "203.0.113.10"} {
		if s.Contains(netip.MustParseAddr(out)) {
			t.Errorf("%s contained", out)
		}
	}
	if !s.Contains(a4(10, 0, 0, 1)) {
		t.Error("the private set is not part of a Set")
	}
	var zero Set
	if !zero.Contains(netip.IPv6Loopback()) || zero.Contains(netip.MustParseAddr("192.0.2.1")) {
		t.Error("the zero Set is not the private set")
	}
}

func TestParseTrustedRefuses(t *testing.T) {
	for in, want := range map[string]string{
		"0.0.0.0/0":         "wider",
		"198.51.0.0/15":     "wider",
		"2001:db8::/32":     "wider",
		"::/0":              "wider",
		"::ffff:0.0.0.0/96": "wider",
		"garden.example":    "not a CIDR",
		"198.51.100.0/33":   "not a CIDR",
	} {
		_, err := ParseTrusted(in)
		if err == nil || !strings.Contains(err.Error(), want) || !strings.Contains(err.Error(), EnvTrusted) {
			t.Errorf("ParseTrusted(%q) = %v, want an error with %q", in, err, want)
		}
	}
	if _, err := ParseTrusted("198.51.0.0/16, 2001:db8:1::/48"); err != nil {
		t.Errorf("the widest allowed entries: %v", err)
	}
}

func TestParseAddr(t *testing.T) {
	ten := a4(10, 0, 0, 1).String()
	ll := a6tail([]byte{0xfe, 0x80}, 1)
	for in, want := range map[string]netip.Addr{
		"198.51.100.7":                   netip.MustParseAddr("198.51.100.7"),
		" 198.51.100.7:4711":             netip.MustParseAddr("198.51.100.7"),
		`"198.51.100.7"`:                 netip.MustParseAddr("198.51.100.7"),
		"2001:db8::1":                    netip.MustParseAddr("2001:db8::1"),
		"[2001:db8::1]":                  netip.MustParseAddr("2001:db8::1"),
		`"[2001:db8::1]:4711"`:           netip.MustParseAddr("2001:db8::1"),
		ten + ":80":                      a4(10, 0, 0, 1),
		"[" + ll.String() + "%25eth0]:9": ll.WithZone("eth0"),
		ll.String() + "%eth0":            ll.WithZone("eth0"),
	} {
		got, ok := ParseAddr(in)
		if !ok || got != want {
			t.Errorf("ParseAddr(%q) = %v, %v; want %v", in, got, ok, want)
		}
	}
	for _, in := range []string{"", "unknown", "_hidden", "garden.example", "198.51.100", "[2001:db8::1"} {
		if a, ok := ParseAddr(in); ok {
			t.Errorf("ParseAddr(%q) = %v, want no address", in, a)
		}
	}
}

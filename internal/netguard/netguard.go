// Package netguard tells private network addresses from the rest. The hub
// refuses requests from outside the private set and the bridge refuses a hub
// outside it (docs/api.md, Private network only).
//
// The set is built from named prefixes, not from net.IP.IsPrivate, which
// leaves out the shared address space Tailscale uses and is not meant for
// access checks. The prefixes are written as bytes so no private address
// appears as a literal in the source.
package netguard

import (
	"fmt"
	"net/netip"
	"strings"
)

// EnvTrusted is the variable both the hub and the bridge read for the
// operator's extra networks.
const EnvTrusted = "MEAT_MODE_TRUSTED_NETWORKS"

// The widest trusted network one entry may name.
const (
	MinBits4 = 16
	MinBits6 = 48
)

func v4(a, b byte, bits int) netip.Prefix {
	return netip.PrefixFrom(netip.AddrFrom4([4]byte{a, b, 0, 0}), bits)
}

func v6(a, b byte, bits int) netip.Prefix {
	return netip.PrefixFrom(netip.AddrFrom16([16]byte{a, b}), bits)
}

// privatePrefixes is the private set.
var privatePrefixes = []netip.Prefix{
	v4(127, 0, 8), // loopback
	netip.PrefixFrom(netip.IPv6Loopback(), 128),
	v4(10, 0, 8),       // RFC 1918
	v4(172, 16, 12),    // RFC 1918
	v4(192, 168, 16),   // RFC 1918
	v4(100, 64, 10),    // RFC 6598 shared space, Tailscale's addresses
	v4(169, 254, 16),   // IPv4 link-local
	v6(0xfc, 0x00, 7),  // unique local, Tailscale's IPv6 among them
	v6(0xfe, 0x80, 10), // IPv6 link-local
}

// Normalize drops an IPv6 zone and unwraps an IPv4-mapped IPv6 address, so
// ::ffff:a.b.c.d is judged as a.b.c.d.
func Normalize(a netip.Addr) netip.Addr {
	return a.WithZone("").Unmap()
}

// Private reports whether a is in the private set: loopback, RFC 1918, the
// RFC 6598 shared space, IPv4 and IPv6 link-local, and unique local IPv6.
func Private(a netip.Addr) bool {
	if !a.IsValid() {
		return false
	}
	a = Normalize(a)
	for _, p := range privatePrefixes {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// Set is the private set plus the operator's trusted networks. The zero Set
// is the private set alone.
type Set struct {
	trusted []netip.Prefix
}

// ParseTrusted reads a comma-separated list of CIDRs or bare addresses, the
// value of MEAT_MODE_TRUSTED_NETWORKS. An entry wider than /16 for IPv4 or
// /48 for IPv6 is an error naming it, so the setting cannot open the hub to
// the whole internet.
func ParseTrusted(s string) (Set, error) {
	var set Set
	for e := range strings.SplitSeq(s, ",") {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		var p netip.Prefix
		if strings.Contains(e, "/") {
			var err error
			p, err = netip.ParsePrefix(e)
			if err != nil {
				return Set{}, fmt.Errorf("%s entry %q is not a CIDR such as 198.51.100.0/24 or 2001:db8:1::/48", EnvTrusted, e)
			}
			if p.Addr().Is4In6() {
				if p.Bits() < 96 {
					return Set{}, fmt.Errorf("%s entry %q is an IPv4-mapped prefix wider than /16; write it as IPv4", EnvTrusted, e)
				}
				p = netip.PrefixFrom(p.Addr().Unmap(), p.Bits()-96)
			}
		} else {
			a, err := netip.ParseAddr(e)
			if err != nil {
				return Set{}, fmt.Errorf("%s entry %q is not a CIDR such as 198.51.100.0/24 or an address", EnvTrusted, e)
			}
			a = Normalize(a)
			p = netip.PrefixFrom(a, a.BitLen())
		}
		if p.Addr().Is4() && p.Bits() < MinBits4 || p.Addr().Is6() && p.Bits() < MinBits6 {
			return Set{}, fmt.Errorf("%s entry %q is wider than /%d for IPv4 or /%d for IPv6; name only your own network",
				EnvTrusted, e, MinBits4, MinBits6)
		}
		set.trusted = append(set.trusted, p.Masked())
	}
	return set, nil
}

// Trusted is the operator's extra networks, as parsed.
func (s Set) Trusted() []netip.Prefix { return s.trusted }

// Contains reports whether a is private or inside a trusted network.
func (s Set) Contains(a netip.Addr) bool {
	if !a.IsValid() {
		return false
	}
	if Private(a) {
		return true
	}
	a = Normalize(a)
	for _, p := range s.trusted {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// ParseAddr reads one address as it appears in a peer address or a
// forwarding header: a bare address, address:port, [IPv6] or [IPv6]:port,
// optionally in double quotes, with an optional IPv6 zone. It reports false
// for anything else, such as a host name or "unknown".
func ParseAddr(s string) (netip.Addr, bool) {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(strings.TrimPrefix(s, `"`), `"`)
	// Forwarded percent-encodes the zone of an IPv6 address.
	s = strings.ReplaceAll(s, "%25", "%")
	if a, err := netip.ParseAddr(s); err == nil {
		return a, true
	}
	if ap, err := netip.ParseAddrPort(s); err == nil {
		return ap.Addr(), true
	}
	if strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]") {
		if a, err := netip.ParseAddr(s[1 : len(s)-1]); err == nil {
			return a, true
		}
	}
	return netip.Addr{}, false
}

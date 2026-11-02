package netguard

import (
	"context"
	"fmt"
	"net"
	"net/netip"
)

// Resolver looks up the addresses of a host name.
type Resolver interface {
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}

// Dialer dials a hub only at an address inside Allowed. It resolves the host
// itself, refuses it when any address is outside the set, and dials one of the
// checked addresses, so a name that answers differently the second time
// cannot move the connection. The bridge and the terminal viewer both dial
// the hub through it.
type Dialer struct {
	Allowed Set
	// Resolver looks up host names; nil is the system resolver.
	Resolver Resolver
	// Refused builds the error for a host with an address outside Allowed;
	// nil gives a *RefusedError. It names what would have been sent, so the
	// user knows why the refusal matters.
	Refused func(host string, ip netip.Addr) error
}

// RefusedError is a hub host with an address outside the allowed set.
type RefusedError struct {
	Host string
	IP   netip.Addr
}

func (e *RefusedError) Error() string {
	return fmt.Sprintf("refusing the hub at %s: %s is outside the private network; "+
		"set MEAT_MODE_TRUSTED_NETWORKS if this is your own network", e.Host, e.IP)
}

// DialContext has the signature of http.Transport.DialContext.
func (d Dialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	var ips []netip.Addr
	if ip, err := netip.ParseAddr(host); err == nil {
		ips = []netip.Addr{ip}
	} else {
		var r Resolver = net.DefaultResolver
		if d.Resolver != nil {
			r = d.Resolver
		}
		if ips, err = r.LookupNetIP(ctx, "ip", host); err != nil {
			return nil, err
		}
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("the hub's host %s has no address", host)
	}
	for _, ip := range ips {
		if !d.Allowed.Contains(ip) {
			if d.Refused != nil {
				return nil, d.Refused(host, Normalize(ip))
			}
			return nil, &RefusedError{Host: host, IP: Normalize(ip)}
		}
	}
	var nd net.Dialer
	var last error
	for _, ip := range ips {
		c, err := nd.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if err == nil {
			return c, nil
		}
		last = err
	}
	return nil, last
}

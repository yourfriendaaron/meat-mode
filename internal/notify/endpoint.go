package notify

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/coder/safedial"

	"github.com/yourfriendaaron/meat-mode/internal/store"
)

// maxEndpoint is the longest app endpoint the hub stores, in bytes.
const maxEndpoint = 1000

// sendTimeout bounds one push, connection and answer included.
const sendTimeout = 10 * time.Second

// ErrBlockedEndpoint is returned for an app endpoint that breaks the
// endpoint rule (docs/api.md, Devices).
var ErrBlockedEndpoint = fmt.Errorf("%w: the push server must be an https URL at a public address, or be named in MEAT_MODE_PUSH_ALLOW", store.ErrInvalid)

// EndpointGuard checks app endpoints, which name any UnifiedPush server, so
// that a registration cannot make the hub send requests into its own
// network. Every address a host resolves to must be global, unless the
// operator trusts it through MEAT_MODE_PUSH_ALLOW. Sends dial the address
// that was checked, so DNS that changes after the check cannot reach an
// internal one.
type EndpointGuard struct {
	hosts map[string]bool // host names trusted as they are, lower case
	opts  []safedial.Option
	// lookup resolves a host name; tests replace it.
	lookup func(ctx context.Context, host string) ([]netip.Addr, error)
	// base is the transport sends clone; tests give it their certificate.
	base *http.Transport
}

// NewEndpointGuard trusts the host names in hosts exactly, whatever they
// resolve to, and the addresses inside cidrs.
func NewEndpointGuard(hosts []string, cidrs []netip.Prefix) *EndpointGuard {
	g := &EndpointGuard{hosts: map[string]bool{}, lookup: lookupHost}
	for _, h := range hosts {
		g.hosts[strings.ToLower(h)] = true
	}
	if len(cidrs) > 0 {
		g.opts = append(g.opts, safedial.WithAllowedPrefixes(cidrs...))
	}
	return g
}

func lookupHost(ctx context.Context, host string) ([]netip.Addr, error) {
	return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
}

// parse checks an endpoint's form: https, no user info, a host, at most
// maxEndpoint bytes.
func parseEndpoint(endpoint string) (*url.URL, error) {
	if len(endpoint) > maxEndpoint {
		return nil, ErrBlockedEndpoint
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Opaque != "" || u.Hostname() == "" {
		return nil, ErrBlockedEndpoint
	}
	return u, nil
}

// Check applies the endpoint rule to endpoint, resolving its host.
func (g *EndpointGuard) Check(ctx context.Context, endpoint string) error {
	u, err := parseEndpoint(endpoint)
	if err != nil {
		return err
	}
	host := strings.ToLower(u.Hostname())
	if g.hosts[host] {
		return nil
	}
	ips, err := g.resolve(ctx, host)
	if err != nil {
		return fmt.Errorf("%w: the push server's name does not resolve", store.ErrInvalid)
	}
	for _, ip := range ips {
		if err := safedial.CheckAddr(ip, g.opts...); err != nil {
			return ErrBlockedEndpoint
		}
	}
	return nil
}

// resolve returns host's addresses, or host itself when it is an address.
func (g *EndpointGuard) resolve(ctx context.Context, host string) ([]netip.Addr, error) {
	if ip, err := netip.ParseAddr(host); err == nil {
		return []netip.Addr{ip}, nil
	}
	ips, err := g.lookup(ctx, host)
	if err == nil && len(ips) == 0 {
		err = fmt.Errorf("no addresses for %q", host)
	}
	return ips, err
}

// Client is the HTTP client app sends use: every connection is resolved and
// checked here, then made to the checked address; redirects are not
// followed, and a send gives up after sendTimeout.
func (g *EndpointGuard) Client() *http.Client {
	var t *http.Transport
	if g.base != nil {
		t = g.base.Clone()
	} else {
		t = http.DefaultTransport.(*http.Transport).Clone()
	}
	t.Proxy = nil
	t.DialTLSContext = nil
	t.DialContext = g.dial
	return &http.Client{Transport: t, Timeout: sendTimeout, CheckRedirect: noRedirects}
}

// noRedirects hands a redirect back as the answer, which counts as a failed
// send.
func noRedirects(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

// dial resolves addr's host with the guard's resolver and connects to the
// first address that answers. A trusted host name skips the address policy;
// every other address must pass it, and safedial checks it again at the
// socket.
func (g *EndpointGuard) dial(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	ips, err := g.resolve(ctx, host)
	if err != nil {
		return nil, err
	}
	d := &net.Dialer{Timeout: sendTimeout}
	dial := d.DialContext
	if !g.hosts[strings.ToLower(host)] {
		for _, ip := range ips {
			if err := safedial.CheckAddr(ip, g.opts...); err != nil {
				return nil, err
			}
		}
		dial = safedial.NewDialContext(d, g.opts...)
	}
	var errs []error
	for _, ip := range ips {
		c, err := dial(ctx, network, net.JoinHostPort(ip.Unmap().String(), port))
		if err == nil {
			return c, nil
		}
		errs = append(errs, err)
	}
	return nil, errors.Join(errs...)
}

// blocked reports whether err is the guard refusing an address.
func blocked(err error) bool {
	var b *safedial.BlockedError
	return errors.As(err, &b) || errors.Is(err, ErrBlockedEndpoint)
}

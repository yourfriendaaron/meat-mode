package hub

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/api"
	"github.com/yourfriendaaron/meat-mode/internal/netguard"
)

// The public DNS check (docs/api.md, Private network only).
const (
	dnsTimeout = 5 * time.Second // for one check, every resolver at once
	dnsEvery   = time.Hour       // between checks after start
)

// publicResolvers are Cloudflare, Google and Quad9, over IPv4 and IPv6.
var publicResolvers = []string{
	"1.1.1.1:53", "8.8.8.8:53", "9.9.9.9:53",
	"[2606:4700:4700::1111]:53", "[2001:4860:4860::8888]:53", "[2620:fe::fe]:53",
}

// dnsLookup returns the A and AAAA records public DNS holds for host. A
// name without records, NXDOMAIN or none of either type, returns no
// addresses and no error; an error means no resolver answered.
type dnsLookup func(ctx context.Context, host string) ([]netip.Addr, error)

// lookupPublic asks every public resolver at once and takes the first
// answer, within dnsTimeout. It asks them directly rather than through the
// system's resolver, which may be MagicDNS or a LAN's own DNS.
func lookupPublic(ctx context.Context, host string) ([]netip.Addr, error) {
	ctx, cancel := context.WithTimeout(ctx, dnsTimeout)
	defer cancel()
	// A trailing dot keeps the resolver from trying the system's search
	// domains.
	name := strings.TrimSuffix(host, ".") + "."
	type result struct {
		addrs []netip.Addr
		err   error
	}
	results := make(chan result, len(publicResolvers))
	for _, server := range publicResolvers {
		r := &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, network, server)
			},
		}
		go func() {
			addrs, err := r.LookupNetIP(ctx, "ip", name)
			results <- result{addrs, err}
		}()
	}
	var last error
	for range publicResolvers {
		res := <-results
		var dnsErr *net.DNSError
		switch {
		case res.err == nil:
			return res.addrs, nil
		case errors.As(res.err, &dnsErr) && dnsErr.IsNotFound:
			return nil, nil
		}
		last = res.err
	}
	return nil, last
}

// checkPublicDNS checks the host of publicURL against public DNS. It sets
// every field but CheckedAt. A host that is localhost or an IP address is
// judged without a lookup.
func checkPublicDNS(ctx context.Context, publicURL string, set netguard.Set, lookup dnsLookup) (api.PublicDNS, error) {
	u, err := url.Parse(publicURL)
	if err != nil || u.Hostname() == "" {
		return api.PublicDNS{State: api.DNSSkipped}, nil
	}
	host := strings.ToLower(u.Hostname())
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return api.PublicDNS{State: api.DNSSkipped}, nil
	}
	if a, err := netip.ParseAddr(host); err == nil {
		if set.Contains(a) {
			return api.PublicDNS{State: api.DNSSkipped}, nil
		}
		return api.PublicDNS{State: api.DNSPublic, Addresses: []string{netguard.Normalize(a).String()}}, nil
	}
	addrs, err := lookup(ctx, host)
	if err != nil {
		return api.PublicDNS{State: api.DNSUnchecked}, err
	}
	out := api.PublicDNS{State: api.DNSPrivate}
	for _, a := range addrs {
		if !set.Contains(a) {
			out.State = api.DNSPublic
			out.Addresses = append(out.Addresses, netguard.Normalize(a).String())
		}
	}
	return out, nil
}

// publicDNSError is the message that stops the hub at start.
func publicDNSError(publicURL string, d api.PublicDNS) error {
	return fmt.Errorf("MEAT_MODE_PUBLIC_URL's host %s has a public address in DNS (%s), so anyone on the internet can find this hub. "+
		"Turn Tailscale Funnel off or remove the public DNS record, then start the hub again. "+
		"Funnel's public records can linger for a few hours after it is turned off",
		hostOf(publicURL), strings.Join(d.Addresses, ", "))
}

func hostOf(publicURL string) string {
	if u, err := url.Parse(publicURL); err == nil && u.Hostname() != "" {
		return u.Hostname()
	}
	return publicURL
}

// runPublicDNSCheck checks MEAT_MODE_PUBLIC_URL and keeps the result for
// publicDNS. It logs when no resolver answered, unless ctx has ended.
func (s *Server) runPublicDNSCheck(ctx context.Context) api.PublicDNS {
	d, err := checkPublicDNS(ctx, s.cfg.PublicURL, s.guard.net.set, s.lookup)
	now := s.now().UTC()
	d.CheckedAt = &now
	s.dnsMu.Lock()
	s.dns = d
	s.dnsMu.Unlock()
	if d.State == api.DNSUnchecked && ctx.Err() == nil {
		s.log.Warn("could not check MEAT_MODE_PUBLIC_URL against public DNS: no public resolver answered",
			"host", hostOf(s.cfg.PublicURL), "err", err)
	}
	return d
}

// watchPublicDNS checks again every s.dnsEvery until ctx ends. A public
// address is logged and shown in Settings; it does not stop the hub, since
// a stale record or a home domain pointing at a router that forwards
// nothing is common.
func (s *Server) watchPublicDNS(ctx context.Context) {
	t := time.NewTicker(s.dnsEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if d := s.runPublicDNSCheck(ctx); d.State == api.DNSPublic {
			s.log.Warn("MEAT_MODE_PUBLIC_URL's host has a public address in DNS, so anyone on the internet can find this hub: "+
				"turn Tailscale Funnel off or remove the public DNS record",
				"host", hostOf(s.cfg.PublicURL), "addresses", strings.Join(d.Addresses, ", "))
		}
	}
}

// publicDNS is the latest public DNS check of MEAT_MODE_PUBLIC_URL
// (docs/api.md, Private network only).
func (s *Server) publicDNS() api.PublicDNS {
	s.dnsMu.Lock()
	defer s.dnsMu.Unlock()
	d := s.dns
	d.Addresses = append([]string(nil), d.Addresses...)
	return d
}

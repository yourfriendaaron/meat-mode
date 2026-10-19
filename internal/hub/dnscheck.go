package hub

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/dns/dnsmessage"

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
// answer, within dnsTimeout.
func lookupPublic(ctx context.Context, host string) ([]netip.Addr, error) {
	return lookupVia(ctx, publicResolvers, host)
}

// errNotFound is a resolver's NXDOMAIN.
var errNotFound = errors.New("no such host")

// lookupVia asks each of servers at once and takes the first answer. It
// sends its own queries rather than going through the system's resolver,
// which may be MagicDNS or a LAN's own DNS, or through net.Resolver, which
// answers a name in /etc/hosts from that file and never asks DNS.
func lookupVia(ctx context.Context, servers []string, host string) ([]netip.Addr, error) {
	ctx, cancel := context.WithTimeout(ctx, dnsTimeout)
	defer cancel()
	// A fully qualified name keeps the system's search domains out of it.
	name, err := dnsmessage.NewName(strings.TrimSuffix(host, ".") + ".")
	if err != nil {
		return nil, err
	}
	type result struct {
		addrs []netip.Addr
		err   error
	}
	results := make(chan result, len(servers))
	for _, server := range servers {
		go func() {
			addrs, err := askServer(ctx, server, name)
			results <- result{addrs, err}
		}()
	}
	var last error
	for range servers {
		res := <-results
		switch {
		case res.err == nil:
			return res.addrs, nil
		case errors.Is(res.err, errNotFound):
			return nil, nil
		}
		last = res.err
	}
	return nil, last
}

// askServer asks one resolver for the A and AAAA records of name.
func askServer(ctx context.Context, server string, name dnsmessage.Name) ([]netip.Addr, error) {
	var out []netip.Addr
	for _, t := range []dnsmessage.Type{dnsmessage.TypeA, dnsmessage.TypeAAAA} {
		addrs, err := query(ctx, server, name, t)
		if err != nil {
			return nil, err
		}
		out = append(out, addrs...)
	}
	return out, nil
}

// query sends one question over UDP, and again over TCP when the answer
// comes back truncated.
func query(ctx context.Context, server string, name dnsmessage.Name, t dnsmessage.Type) ([]netip.Addr, error) {
	id := uint16(rand.Uint32())
	q := dnsmessage.Question{Name: name, Type: t, Class: dnsmessage.ClassINET}
	msg, err := (&dnsmessage.Message{
		Header:    dnsmessage.Header{ID: id, RecursionDesired: true},
		Questions: []dnsmessage.Question{q},
	}).Pack()
	if err != nil {
		return nil, err
	}
	resp, err := exchange(ctx, "udp", server, msg)
	if err == nil && resp.Truncated {
		resp, err = exchange(ctx, "tcp", server, msg)
	}
	if err != nil {
		return nil, err
	}
	if resp.ID != id || len(resp.Questions) != 1 || resp.Questions[0] != q {
		return nil, fmt.Errorf("%s answered a different question", server)
	}
	switch resp.RCode {
	case dnsmessage.RCodeSuccess:
	case dnsmessage.RCodeNameError:
		return nil, errNotFound
	default:
		return nil, fmt.Errorf("%s answered %s", server, resp.RCode)
	}
	var out []netip.Addr
	for _, a := range resp.Answers {
		switch b := a.Body.(type) {
		case *dnsmessage.AResource:
			out = append(out, netip.AddrFrom4(b.A))
		case *dnsmessage.AAAAResource:
			out = append(out, netip.AddrFrom16(b.AAAA))
		}
	}
	return out, nil
}

// exchange sends msg to server and reads one reply. Over TCP each message
// carries a two-byte length first.
func exchange(ctx context.Context, network, server string, msg []byte) (*dnsmessage.Message, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, network, server)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		conn.SetDeadline(deadline)
	}
	buf := make([]byte, 65535)
	var n int
	if network == "tcp" {
		if _, err := conn.Write(binary.BigEndian.AppendUint16(nil, uint16(len(msg)))); err != nil {
			return nil, err
		}
		if _, err := conn.Write(msg); err != nil {
			return nil, err
		}
		var size [2]byte
		if _, err := io.ReadFull(conn, size[:]); err != nil {
			return nil, err
		}
		n = int(binary.BigEndian.Uint16(size[:]))
		if _, err := io.ReadFull(conn, buf[:n]); err != nil {
			return nil, err
		}
	} else {
		if _, err := conn.Write(msg); err != nil {
			return nil, err
		}
		if n, err = conn.Read(buf); err != nil {
			return nil, err
		}
	}
	var resp dnsmessage.Message
	if err := resp.Unpack(buf[:n]); err != nil {
		return nil, err
	}
	return &resp, nil
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

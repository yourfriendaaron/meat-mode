package hub

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/netip"
	"reflect"
	"testing"

	"golang.org/x/net/dns/dnsmessage"
)

// dnsServer is a resolver on loopback, over UDP and TCP on one port. answer
// builds the reply to each question; the header's ID and the question are
// copied in unless answer sets them.
type dnsServer struct {
	addr   string
	answer func(q dnsmessage.Question, tcp bool) dnsmessage.Message
}

func newDNSServer(t *testing.T, answer func(q dnsmessage.Question, tcp bool) dnsmessage.Message) *dnsServer {
	t.Helper()
	// The TCP port that matches a free UDP port may be taken, so try a few.
	var pc net.PacketConn
	var ln net.Listener
	var err error
	for range 20 {
		if pc, err = net.ListenPacket("udp", "127.0.0.1:0"); err != nil {
			t.Fatal(err)
		}
		if ln, err = net.Listen("tcp", pc.LocalAddr().String()); err == nil {
			break
		}
		pc.Close()
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pc.Close(); ln.Close() })
	s := &dnsServer{addr: pc.LocalAddr().String(), answer: answer}
	go func() {
		buf := make([]byte, 65535)
		for {
			n, from, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			if out := s.reply(buf[:n], false); out != nil {
				pc.WriteTo(out, from)
			}
		}
	}()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				var size [2]byte
				if _, err := io.ReadFull(conn, size[:]); err != nil {
					return
				}
				msg := make([]byte, binary.BigEndian.Uint16(size[:]))
				if _, err := io.ReadFull(conn, msg); err != nil {
					return
				}
				if out := s.reply(msg, true); out != nil {
					conn.Write(append(binary.BigEndian.AppendUint16(nil, uint16(len(out))), out...))
				}
			}()
		}
	}()
	return s
}

func (s *dnsServer) reply(msg []byte, tcp bool) []byte {
	var req dnsmessage.Message
	if err := req.Unpack(msg); err != nil || len(req.Questions) != 1 {
		return nil
	}
	resp := s.answer(req.Questions[0], tcp)
	resp.Response = true
	if resp.ID == 0 {
		resp.ID = req.ID
	}
	if resp.Questions == nil {
		resp.Questions = req.Questions
	}
	out, err := resp.Pack()
	if err != nil {
		return nil
	}
	return out
}

func rrHeader(q dnsmessage.Question, t dnsmessage.Type) dnsmessage.ResourceHeader {
	return dnsmessage.ResourceHeader{Name: q.Name, Type: t, Class: dnsmessage.ClassINET, TTL: 60}
}

// records answers A and AAAA questions with v4 and v6, after a CNAME.
func records(v4 [4]byte, v6 [16]byte) func(dnsmessage.Question, bool) dnsmessage.Message {
	return func(q dnsmessage.Question, _ bool) dnsmessage.Message {
		target := dnsmessage.MustNewName("edge.example.net.")
		m := dnsmessage.Message{Answers: []dnsmessage.Resource{
			{Header: rrHeader(q, dnsmessage.TypeCNAME), Body: &dnsmessage.CNAMEResource{CNAME: target}},
		}}
		switch q.Type {
		case dnsmessage.TypeA:
			m.Answers = append(m.Answers, dnsmessage.Resource{Header: rrHeader(q, q.Type), Body: &dnsmessage.AResource{A: v4}})
		case dnsmessage.TypeAAAA:
			m.Answers = append(m.Answers, dnsmessage.Resource{Header: rrHeader(q, q.Type), Body: &dnsmessage.AAAAResource{AAAA: v6}})
		}
		return m
	}
}

func rcode(c dnsmessage.RCode) func(dnsmessage.Question, bool) dnsmessage.Message {
	return func(dnsmessage.Question, bool) dnsmessage.Message {
		return dnsmessage.Message{Header: dnsmessage.Header{RCode: c}}
	}
}

var (
	docV4 = netip.MustParseAddr("203.0.113.7")
	docV6 = netip.MustParseAddr("2001:db8::7")
)

func TestLookupViaReadsAAndAAAA(t *testing.T) {
	s := newDNSServer(t, records(docV4.As4(), docV6.As16()))
	got, err := lookupVia(context.Background(), []string{s.addr}, "hub.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if want := []netip.Addr{docV4, docV6}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// A name in /etc/hosts is still asked of DNS: net.Resolver would answer
// localhost from that file without a query.
func TestLookupViaSkipsTheHostsFile(t *testing.T) {
	s := newDNSServer(t, rcode(dnsmessage.RCodeNameError))
	got, err := lookupVia(context.Background(), []string{s.addr}, "localhost")
	if err != nil || got != nil {
		t.Fatalf("got %v, %v; want no addresses and no error", got, err)
	}
}

func TestLookupViaNoRecords(t *testing.T) {
	s := newDNSServer(t, func(dnsmessage.Question, bool) dnsmessage.Message { return dnsmessage.Message{} })
	got, err := lookupVia(context.Background(), []string{s.addr}, "hub.example.com")
	if err != nil || got != nil {
		t.Fatalf("got %v, %v; want no addresses and no error", got, err)
	}
}

func TestLookupViaRetriesTruncatedOverTCP(t *testing.T) {
	full := records(docV4.As4(), docV6.As16())
	s := newDNSServer(t, func(q dnsmessage.Question, tcp bool) dnsmessage.Message {
		if !tcp {
			return dnsmessage.Message{Header: dnsmessage.Header{Truncated: true}}
		}
		return full(q, tcp)
	})
	got, err := lookupVia(context.Background(), []string{s.addr}, "hub.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if want := []netip.Addr{docV4, docV6}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestLookupViaTakesAnyAnswer(t *testing.T) {
	failing := newDNSServer(t, rcode(dnsmessage.RCodeServerFailure))
	working := newDNSServer(t, records(docV4.As4(), docV6.As16()))
	got, err := lookupVia(context.Background(), []string{failing.addr, working.addr}, "hub.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %v, want the working resolver's two addresses", got)
	}
}

func TestLookupViaRefusesAMismatchedReply(t *testing.T) {
	for name, answer := range map[string]func(dnsmessage.Question, bool) dnsmessage.Message{
		"server failure": rcode(dnsmessage.RCodeServerFailure),
		"other id": func(dnsmessage.Question, bool) dnsmessage.Message {
			return dnsmessage.Message{Header: dnsmessage.Header{ID: 1}}
		},
		"other question": func(q dnsmessage.Question, _ bool) dnsmessage.Message {
			q.Name = dnsmessage.MustNewName("other.example.com.")
			return dnsmessage.Message{Questions: []dnsmessage.Question{q}}
		},
	} {
		t.Run(name, func(t *testing.T) {
			s := newDNSServer(t, answer)
			got, err := lookupVia(context.Background(), []string{s.addr}, "hub.example.com")
			if err == nil || errors.Is(err, errNotFound) {
				t.Fatalf("got %v, %v; want an error", got, err)
			}
		})
	}
}

package netguard

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"time"
	"unicode"
)

// Client returns a copy of base, keeping settings such as its timeout, whose
// connections all go through dial. It never follows a redirect, which would
// carry the token, cookie or password to wherever the Location points, and
// never uses a proxy from the environment, which would dial for it. The
// bridge and the terminal viewer both reach the hub through one.
func Client(base http.Client, dial func(ctx context.Context, network, addr string) (net.Conn, error)) *http.Client {
	base.Transport = &http.Transport{
		Proxy:                 nil,
		DialContext:           dial,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		IdleConnTimeout:       90 * time.Second,
		ForceAttemptHTTP2:     true,
	}
	base.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &base
}

// MaxMessageRunes is the most of the hub's text an error message carries;
// anything longer is cut, so a huge body cannot flood the terminal or the
// log.
const MaxMessageRunes = 2000

// SafeMessage makes one line of the hub's text safe to print: no control
// character, bidi control or invisible format character survives, and it
// stops at MaxMessageRunes runes.
func SafeMessage(s string) string {
	var b strings.Builder
	n := 0
	for _, r := range s {
		if n >= MaxMessageRunes {
			break
		}
		if unicode.IsControl(r) || unicode.Is(unicode.Bidi_Control, r) || unicode.Is(unicode.Cf, r) {
			continue
		}
		b.WriteRune(r)
		n++
	}
	return strings.TrimSpace(b.String())
}

// joiner is the zero width joiner, the one format character Printable
// keeps, since emoji sequences need it.
const joiner = '\u200d'

// Printable makes the hub's text safe to print as part of the CLI's
// output: no control character, bidi control or invisible format character
// other than the zero width joiner survives, so the text cannot move the
// cursor, change the terminal's state or reorder what it shows. lines keeps
// line feeds, for text such as a note that holds several.
func Printable(s string, lines bool) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n' && lines, r == joiner:
			return r
		case unicode.IsControl(r), unicode.Is(unicode.Bidi_Control, r), unicode.Is(unicode.Cf, r):
			return -1
		}
		return r
	}, s)
}

// HubMessage is the hub's {"error"} text, or the body as it is, made safe
// to print.
func HubMessage(data []byte) string {
	var e struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(data, &e) == nil && e.Error != "" {
		return SafeMessage(e.Error)
	}
	return SafeMessage(string(data))
}

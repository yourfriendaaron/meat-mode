package notify

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"
)

// headerSafe replaces control characters with spaces. Titles come from bead
// titles, which anyone with write access to a repo can set, and a line
// break in a header value would fail the whole request.
func headerSafe(v string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, v)
}

// tagSafe makes an item key safe as the Tags header. ntfy splits the value
// on commas, so a key would arrive as several tags, and it rejects over 512
// bytes with HTTP 400; a repo name may hold commas and be up to 512 bytes
// itself. Commas become underscores and the value is cut to the limit on a
// rune boundary.
func tagSafe(v string) string {
	const limit = 512
	v = strings.ReplaceAll(v, ",", "_")
	if len(v) <= limit {
		return v
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(v[cut]) {
		cut--
	}
	return v[:cut]
}

// Ntfy posts messages to an ntfy topic.
type Ntfy struct {
	URL    string // server base URL
	Topic  string
	Token  string // optional bearer token
	Client *http.Client
}

// Notify implements Notifier.
func (n *Ntfy) Notify(ctx context.Context, m Message) error {
	u := strings.TrimRight(n.URL, "/") + "/" + n.Topic
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, strings.NewReader(m.Body))
	if err != nil {
		return err
	}
	set := func(k, v string) { req.Header.Set(k, headerSafe(v)) }
	set("Title", m.Title)
	if m.URL != "" {
		set("Click", m.URL)
	}
	set("Priority", "4")
	if m.Tag != "" {
		set("Tags", tagSafe(m.Tag))
	}
	if a := ntfyActions(m.Actions); a != "" {
		set("Actions", a)
	}
	if n.Token != "" {
		set("Authorization", "Bearer "+n.Token)
	}
	c := n.Client
	if c == nil {
		c = http.DefaultClient
	}
	resp, err := c.Do(req)
	if err != nil {
		return fmt.Errorf("ntfy: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("ntfy: status %d", resp.StatusCode)
	}
	return nil
}

// ntfyActions writes a message's buttons as ntfy's Actions header: each one
// with a URL becomes a view action. Retry has none, since ntfy cannot post
// an action to the hub as the user, and is left out.
func ntfyActions(as []Action) string {
	var out []string
	for _, a := range as {
		if a.URL == "" {
			continue
		}
		// ntfy splits on commas and semicolons outside quotes.
		out = append(out, "view, "+ntfyQuote(a.Title)+", "+ntfyQuote(a.URL)+", clear=true")
	}
	return strings.Join(out, "; ")
}

// ntfyQuote quotes v when it holds a comma, semicolon or quote. ntfy's parser takes
// a backslash-escaped quote inside quotes.
func ntfyQuote(v string) string {
	if !strings.ContainsAny(v, ",;\"'") {
		return v
	}
	return `"` + strings.ReplaceAll(v, `"`, `\"`) + `"`
}

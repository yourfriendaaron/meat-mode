package notify

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
)

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
	req.Header.Set("Title", m.Title)
	if m.URL != "" {
		req.Header.Set("Click", m.URL)
	}
	req.Header.Set("Priority", "4")
	if m.Tag != "" {
		req.Header.Set("Tags", m.Tag)
	}
	if n.Token != "" {
		req.Header.Set("Authorization", "Bearer "+n.Token)
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

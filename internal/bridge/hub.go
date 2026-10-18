package bridge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/api"
	"github.com/yourfriendaaron/meat-mode/internal/netguard"
)

// Hub is a client for the hub's bridge API.
type Hub struct {
	URL   string
	Token string
	// HTTP supplies the client's settings such as its timeout. Its
	// transport is never used: every connection goes through dial.
	HTTP *http.Client
	// Allowed is the private set plus the operator's trusted networks; the
	// bridge dials the hub only at an address inside it.
	Allowed netguard.Set
	// Resolver looks up the hub's host name; nil is the system resolver.
	Resolver Resolver
}

// Resolver looks up the addresses of a host name.
type Resolver interface {
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}

// HTTPError is a non-2xx reply from the hub.
type HTTPError struct {
	Status  int
	Message string
}

func (e *HTTPError) Error() string {
	if e.Status == http.StatusUnauthorized {
		return "the hub rejected this bridge's token; pair again with a new token"
	}
	return fmt.Sprintf("hub replied %d: %s", e.Status, e.Message)
}

// client never follows redirects: one would carry the bearer token to
// wherever the Location points.
func (h *Hub) client() *http.Client {
	c := &http.Client{Timeout: 30 * time.Second}
	if h.HTTP != nil {
		cp := *h.HTTP
		c = &cp
	}
	c.Transport = &http.Transport{
		Proxy:                 nil, // a proxy from the environment would dial for us
		DialContext:           h.dial,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		ForceAttemptHTTP2:     true,
	}
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return c
}

// dial resolves the hub's host, refuses it when any address is outside the
// allowed set, and dials one of the checked addresses itself, so a name that
// answers differently the second time cannot move the connection.
func (h *Hub) dial(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	var ips []netip.Addr
	if ip, err := netip.ParseAddr(host); err == nil {
		ips = []netip.Addr{ip}
	} else {
		var r Resolver = net.DefaultResolver
		if h.Resolver != nil {
			r = h.Resolver
		}
		if ips, err = r.LookupNetIP(ctx, "ip", host); err != nil {
			return nil, err
		}
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("the hub's host %s has no address", host)
	}
	for _, ip := range ips {
		if !h.Allowed.Contains(ip) {
			return nil, fmt.Errorf("refusing the hub at %s: %s is outside the private network, "+
				"and the bridge sends its token only to a hub on one; "+
				"set MEAT_MODE_TRUSTED_NETWORKS if this is your own network", host, netguard.Normalize(ip))
		}
	}
	var d net.Dialer
	var last error
	for _, ip := range ips {
		c, err := d.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if err == nil {
			return c, nil
		}
		last = err
	}
	return nil, last
}

func (h *Hub) do(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(h.URL, "/")+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+h.Token)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := h.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode/100 == 3 {
		return fmt.Errorf("the hub replied %d with a redirect to %q; the bridge does not follow redirects, so set the hub URL to the final address",
			resp.StatusCode, resp.Header.Get("Location"))
	}
	if resp.StatusCode/100 != 2 {
		var e struct {
			Error string `json:"error"`
		}
		msg := strings.TrimSpace(string(data))
		if json.Unmarshal(data, &e) == nil && e.Error != "" {
			msg = e.Error
		}
		return &HTTPError{Status: resp.StatusCode, Message: msg}
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}

// Hello greets the hub and learns this bridge's name.
func (h *Hub) Hello(ctx context.Context, hi api.Hello) (api.HelloReply, error) {
	var r api.HelloReply
	err := h.do(ctx, http.MethodPost, "/bridge/v1/hello", hi, &r)
	return r, err
}

// Settings reads the name and time zone the hub keeps. Unlike Hello it does
// not count as the bridge being connected.
func (h *Hub) Settings(ctx context.Context) (api.Settings, error) {
	var r api.Settings
	err := h.do(ctx, http.MethodGet, "/bridge/v1/settings", nil, &r)
	return r, err
}

// PutRepos replaces this bridge's repo list.
func (h *Hub) PutRepos(ctx context.Context, repos []api.Repo) error {
	return h.do(ctx, http.MethodPut, "/bridge/v1/repos", repos, nil)
}

// PutItems replaces the human items of one repo.
func (h *Hub) PutItems(ctx context.Context, repo string, items []api.Item) error {
	return h.do(ctx, http.MethodPut, "/bridge/v1/items?repo="+url.QueryEscape(repo), items, nil)
}

// Actions returns this bridge's pending actions, oldest first.
func (h *Hub) Actions(ctx context.Context) ([]api.Action, error) {
	var a []api.Action
	err := h.do(ctx, http.MethodGet, "/bridge/v1/actions", nil, &a)
	return a, err
}

// Result reports how an action went.
func (h *Hub) Result(ctx context.Context, id string, r api.ActionResult) error {
	return h.do(ctx, http.MethodPost, "/bridge/v1/actions/"+url.PathEscape(id)+"/result", r, nil)
}

// AddPersonal adds a to-do to the hub's personal list.
func (h *Hub) AddPersonal(ctx context.Context, t api.PersonalTodo) (api.PersonalTodoReply, error) {
	var r api.PersonalTodoReply
	err := h.do(ctx, http.MethodPost, "/bridge/v1/personal", t, &r)
	return r, err
}

// PersonalItem reads a personal to-do this machine's token added.
func (h *Hub) PersonalItem(ctx context.Context, id string) (api.Item, error) {
	var it api.Item
	err := h.do(ctx, http.MethodGet, "/bridge/v1/personal/"+url.PathEscape(id), nil, &it)
	return it, err
}

// ChangePersonal changes a personal to-do this machine's token added.
func (h *Hub) ChangePersonal(ctx context.Context, id string, c api.PersonalChange) (api.Action, error) {
	var a api.Action
	err := h.do(ctx, http.MethodPost, "/bridge/v1/personal/"+url.PathEscape(id)+"/actions", c, &a)
	return a, err
}

// PutRuns replaces this bridge's run list.
func (h *Hub) PutRuns(ctx context.Context, runs []api.RunReport) error {
	return h.do(ctx, http.MethodPut, "/bridge/v1/runs", runs, nil)
}

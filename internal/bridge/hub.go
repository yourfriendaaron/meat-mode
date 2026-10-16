package bridge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

// Hub is a client for the hub's bridge API.
type Hub struct {
	URL   string
	Token string
	HTTP  *http.Client
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

func (h *Hub) client() *http.Client {
	if h.HTTP != nil {
		return h.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
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

// PutRepos replaces this bridge's repo list.
func (h *Hub) PutRepos(ctx context.Context, repos []api.Repo) error {
	return h.do(ctx, http.MethodPut, "/bridge/v1/repos", repos, nil)
}

// PutItems replaces the human items of one repo.
func (h *Hub) PutItems(ctx context.Context, repo string, items []api.Item) error {
	return h.do(ctx, http.MethodPut, "/bridge/v1/repos/"+url.PathEscape(repo)+"/items", items, nil)
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

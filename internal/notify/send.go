package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/SherClockHolmes/webpush-go"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

// Send results (docs/api.md, Devices).
const (
	messageTTL     = 86400              // seconds a push service keeps a reminder for an offline device
	defaultHold    = time.Minute        // a 429 without Retry-After
	maxHold        = time.Hour          // the longest Retry-After the hub honours
	failingRemoval = 3 * 24 * time.Hour // no delivery and only failures for this long removes a subscription
)

// retryDelays are the waits before each retry of a message that met a server
// error or a network failure; after the last, the message is given up.
var retryDelays = []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute}

// errHeld is a send skipped because its push service asked the hub to wait.
var errHeld = errors.New("web push: the push service asked to wait (429)")

// pushBudget is the most bytes a marshalled Web Push payload may be. Web
// Push caps the whole message at 4096 bytes and the encryption overhead
// eats into that, so the hub keeps the JSON itself under this.
const pushBudget = 3800

// pushPayload is the JSON a service worker or the app receives for m.
// actions and key are there only when m has actions. A payload over budget
// is trimmed, its body before its title; url, key, tag, channel and the
// actions always stay whole, because the receiver needs them.
func pushPayload(m Message) []byte {
	type payload struct {
		Title   string   `json:"title"`
		Body    string   `json:"body"`
		URL     string   `json:"url"`
		Tag     string   `json:"tag"`
		Channel string   `json:"channel"`
		Actions []Action `json:"actions,omitempty"`
		Key     string   `json:"key,omitempty"`
	}
	p := payload{Title: m.Title, Body: m.Body, URL: m.URL, Tag: m.Tag, Channel: m.Channel}
	if p.Channel == "" {
		p.Channel = ChannelReminders
	}
	if len(m.Actions) > 0 {
		p.Actions, p.Key = m.Actions, m.Key
	}
	b := marshalPayload(p)
	for len(b) > pushBudget {
		switch {
		case p.Body != "":
			p.Body = shortenRunes(p.Body)
		case p.Title != "":
			p.Title = shortenRunes(p.Title)
		default:
			// url and key alone are over budget; send them whole anyway.
			return b
		}
		b = marshalPayload(p)
	}
	return b
}

// marshalPayload is json.Marshal without HTML escaping: a key may hold <, >
// and &, and < for each would make it six times longer.
func marshalPayload(p any) []byte {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(p)
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n"))
}

// shortenRunes cuts s to about half its runes and marks the cut with an
// ellipsis, so repeated cuts converge on the empty string without ever
// splitting a rune.
func shortenRunes(s string) string {
	rs := []rune(s)
	n := len(rs) / 2
	if n <= 1 {
		// "a…" would halve to itself.
		return ""
	}
	return string(rs[:n]) + "…"
}

// deliver pushes payload to t and handles the push service's answer:
//
//   - 2xx: delivered; the device's lastPush is set.
//   - 404, 410, or an endpoint that no longer passes its rule: the
//     subscription is removed. That counts as not delivered without being
//     an error.
//   - 413: logged; the subscription stays.
//   - 429: every send to the endpoint waits until its Retry-After.
//   - 5xx and network errors: the send failed.
//
// With retry, a failed or held message is tried again later (retryDelays),
// attempt counting the retries made so far. A subscription with no delivery
// and only failures for failingRemoval is removed.
func (r *Registry) deliver(ctx context.Context, t target, payload []byte, ttl, attempt int, retry bool) (bool, error) {
	now := r.now()
	if until, held := r.held(t.endpoint, now); held {
		if retry {
			r.retryAt(until.Sub(now), t.id, payload, attempt)
		}
		return false, errHeld
	}
	client := r.Client
	if t.kind == api.DeviceApp {
		if _, err := parseEndpoint(t.endpoint); err != nil {
			return false, r.remove(ctx, t)
		}
		client = r.Guard.Client()
	} else if !r.allowed(t.endpoint) {
		// Stored before endpoints were checked; never send to it.
		return false, r.remove(ctx, t)
	}
	opts := &webpush.Options{
		Subscriber:      r.subject,
		VAPIDPublicKey:  r.keys.PublicKey,
		VAPIDPrivateKey: r.keys.PrivateKey,
		TTL:             ttl,
		Urgency:         webpush.UrgencyHigh,
	}
	if client != nil {
		opts.HTTPClient = client
	}
	sub := &webpush.Subscription{Endpoint: t.endpoint, Keys: webpush.Keys{P256dh: t.p256dh, Auth: t.auth}}
	resp, err := webpush.SendNotificationWithContext(ctx, payload, sub, opts)
	if err != nil {
		if blocked(err) {
			// Its host now resolves to an address the rule refuses.
			return false, r.remove(ctx, t)
		}
		r.failed(ctx, t, payload, attempt, retry)
		return false, fmt.Errorf("web push: %w", err)
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	resp.Body.Close()
	switch code := resp.StatusCode; {
	case code/100 == 2:
		_, err := r.db.ExecContext(ctx, `UPDATE push_subscriptions SET last_push = ?, failing_since = NULL WHERE endpoint = ?`,
			r.now().Unix(), t.endpoint)
		return true, err
	case code == http.StatusNotFound || code == http.StatusGone:
		return false, r.remove(ctx, t)
	case code == http.StatusRequestEntityTooLarge:
		r.log.Warn("a push service refused a notification as too large", "device", t.id, "bytes", len(payload))
		return false, fmt.Errorf("web push: status %d", code)
	case code == http.StatusTooManyRequests:
		wait := retryAfter(resp.Header.Get("Retry-After"), now)
		r.mu.Lock()
		r.holds[t.endpoint] = now.Add(wait)
		r.mu.Unlock()
		if retry && attempt < len(retryDelays) {
			r.retryAt(wait, t.id, payload, attempt+1)
		}
		return false, fmt.Errorf("web push: status %d", code)
	default:
		// A 5xx, ntfy's 507 among them, is retried; any other answer, such as
		// a redirect the hub does not follow, is not.
		r.failed(ctx, t, payload, attempt, retry && code/100 == 5)
		return false, fmt.Errorf("web push: status %d", code)
	}
}

// held reports whether sends to endpoint wait until a time after now.
func (r *Registry) held(endpoint string, now time.Time) (time.Time, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	until, ok := r.holds[endpoint]
	if ok && !until.After(now) {
		delete(r.holds, endpoint)
		return until, false
	}
	return until, ok
}

// retryAfter reads a Retry-After value, in seconds or as a date, capped at
// maxHold; defaultHold when it is absent or unreadable.
func retryAfter(v string, now time.Time) time.Duration {
	var d time.Duration
	if n, err := strconv.Atoi(v); err == nil {
		d = time.Duration(n) * time.Second
	} else if t, err := http.ParseTime(v); err == nil {
		d = t.Sub(now)
	}
	if d <= 0 {
		return defaultHold
	}
	return min(d, maxHold)
}

// remove drops a subscription its push service no longer knows, or whose
// endpoint the hub no longer accepts.
func (r *Registry) remove(ctx context.Context, t target) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM push_subscriptions WHERE endpoint = ?`, t.endpoint)
	return err
}

// failed records a failed send, removes a subscription that has only failed
// for failingRemoval, and schedules the next retry when there is one.
func (r *Registry) failed(ctx context.Context, t target, payload []byte, attempt int, retry bool) {
	now := r.now().Unix()
	if _, err := r.db.ExecContext(ctx, `UPDATE push_subscriptions SET failing_since = coalesce(failing_since, ?) WHERE endpoint = ?`,
		now, t.endpoint); err != nil {
		r.log.Warn("record a failed push", "device", t.id, "err", err)
	}
	res, err := r.db.ExecContext(ctx, `DELETE FROM push_subscriptions WHERE endpoint = ? AND failing_since <= ?`,
		t.endpoint, now-int64(failingRemoval/time.Second))
	if err != nil {
		r.log.Warn("remove a failing subscription", "device", t.id, "err", err)
	} else if n, _ := res.RowsAffected(); n > 0 {
		r.log.Info("removed a subscription whose pushes have failed for 3 days", "device", t.id)
		return
	}
	if !retry {
		return
	}
	if attempt >= len(retryDelays) {
		r.log.Warn("gave up on a push after its retries", "device", t.id)
		return
	}
	r.retryAt(retryDelays[attempt], t.id, payload, attempt+1)
}

// retryAt sends payload again to the device id after d, if it is still
// there and confirmed. Its keys and endpoint are read again then, so a
// renewal in between is used.
func (r *Registry) retryAt(d time.Duration, id string, payload []byte, attempt int) {
	r.after(d, func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*sendTimeout)
		defer cancel()
		subs, err := r.query(ctx, `WHERE confirmed = 1 AND id = ?`, id)
		if err != nil {
			r.log.Warn("retry a push", "device", id, "err", err)
			return
		}
		if len(subs) == 0 {
			return
		}
		if _, err := r.deliver(ctx, subs[0], payload, messageTTL, attempt, true); err != nil && !errors.Is(err, errHeld) {
			r.log.Warn("a retried push failed", "device", id, "attempt", attempt, "err", err)
		}
	})
}

package notify

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"
)

// decoded is the payload a service worker reads.
type decoded struct {
	Title   string   `json:"title"`
	Body    string   `json:"body"`
	URL     string   `json:"url"`
	Tag     string   `json:"tag"`
	Key     string   `json:"key"`
	Actions []Action `json:"actions"`
}

// maxKey is an item key at the documented maximum lengths: a 32-byte bridge
// name, a 512-byte repo path and a 128-byte bead id of two-byte characters.
func maxKey() string {
	return strings.Repeat("a", 31) + "1/" + strings.Repeat("é", 256) + "/" + strings.Repeat("é", 64)
}

func TestPushPayloadFitsALongestItem(t *testing.T) {
	key := maxKey()
	link := "https://app.example.com/?item=" + url.QueryEscape(key)
	m := Message{
		Title: strings.Repeat("あ", 1000),
		Body:  strings.Repeat("い", 600),
		URL:   link,
		Tag:   "agent:0123456789abcdef",
		Key:   key,
		Actions: []Action{
			{Action: ActionReply, Title: "Reply"},
			{Action: ActionOpen, Title: "Open"},
			{Action: ActionRetry, Title: "Retry"},
		},
	}
	b := pushPayload(m)
	if len(b) > 3800 {
		t.Fatalf("payload of a longest item is %d bytes", len(b))
	}
	var p decoded
	if err := json.Unmarshal(b, &p); err != nil {
		t.Fatal(err)
	}
	if p.URL != link || p.Key != key {
		t.Errorf("url %q or key lost; key is %d bytes", p.URL, len(p.Key))
	}
	if len(p.Actions) != 3 || p.Actions[0].Action != ActionReply || p.Actions[1].Action != ActionOpen || p.Actions[2].Action != ActionRetry {
		t.Errorf("actions %+v", p.Actions)
	}
	if p.Title == m.Title {
		t.Errorf("title was not shortened")
	}
	if !strings.HasSuffix(p.Title, "…") {
		t.Errorf("a shortened title has no ellipsis: %q", p.Title[len(p.Title)-20:])
	}
}

func TestPushPayloadTrimsBodyBeforeTitle(t *testing.T) {
	m := Message{
		Title: "planner replied",
		Body:  strings.Repeat("x", 5000),
		URL:   "https://app.example.com/?item=laptop%2Fgarden%2Fgd-1",
		Tag:   "agent:0123456789abcdef",
	}
	b := pushPayload(m)
	if len(b) > pushBudget {
		t.Fatalf("payload is %d bytes", len(b))
	}
	var p decoded
	if err := json.Unmarshal(b, &p); err != nil {
		t.Fatal(err)
	}
	if p.Title != m.Title {
		t.Errorf("title was trimmed: %q", p.Title)
	}
	if p.Body == m.Body || !strings.HasSuffix(p.Body, "…") {
		t.Errorf("body was not shortened with an ellipsis: %q", p.Body[:40])
	}

	// A payload already within budget goes out unchanged.
	small := Message{Title: "Water the plants", Body: "Due now", URL: m.URL, Tag: "t"}
	var q decoded
	if err := json.Unmarshal(pushPayload(small), &q); err != nil {
		t.Fatal(err)
	}
	if q.Title != small.Title || q.Body != small.Body || q.URL != small.URL || q.Tag != small.Tag {
		t.Errorf("a small payload was changed: %+v", q)
	}
}

func TestPushPayloadKeepsAnEscapedKeyInBudget(t *testing.T) {
	// <, > and & are allowed in names; HTML escaping would make each six bytes.
	key := strings.Repeat("a", 31) + "1/" + strings.Repeat("<&>", 170) + "/" + strings.Repeat("&", 128)
	link := "https://app.example.com/?item=" + url.QueryEscape(key)
	m := Message{Title: "Alex replied", Body: "Water the plants", URL: link, Tag: "agent:0123456789abcdef", Key: key,
		Actions: []Action{{Action: ActionReply, Title: "Reply"}}}
	b := pushPayload(m)
	if len(b) > 3800 {
		t.Fatalf("payload with an escaped key is %d bytes", len(b))
	}
	var p decoded
	if err := json.Unmarshal(b, &p); err != nil {
		t.Fatal(err)
	}
	if p.Key != key || p.URL != link {
		t.Errorf("key or url changed")
	}
}

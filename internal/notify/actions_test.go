package notify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func agentMessage() Message {
	return Message{
		Title: "planner replied", Body: "Water the plants",
		URL: "https://app.example.com/?item=laptop%2Fgarden%2Fgd-1", Tag: "agent:laptop/garden/gd-1",
		Key: "laptop/garden/gd-1", Channel: ChannelAgents,
		Actions: []Action{
			{Action: ActionReply, Title: "Reply", URL: "https://app.example.com/?item=laptop%2Fgarden%2Fgd-1&reply=1"},
			{Action: ActionOpen, Title: "Open", URL: "https://app.example.com/?item=laptop%2Fgarden%2Fgd-1"},
			{Action: ActionRetry, Title: "Retry"},
		},
	}
}

func TestPushPayload(t *testing.T) {
	var got map[string]any
	if err := json.Unmarshal(pushPayload(agentMessage()), &got); err != nil {
		t.Fatal(err)
	}
	want := `{"actions":[{"action":"reply","title":"Reply"},{"action":"open","title":"Open"},{"action":"retry","title":"Retry"}],` +
		`"body":"Water the plants","channel":"agents","key":"laptop/garden/gd-1","tag":"agent:laptop/garden/gd-1","title":"planner replied",` +
		`"url":"https://app.example.com/?item=laptop%2Fgarden%2Fgd-1"}`
	if b, _ := json.Marshal(got); string(b) != want {
		t.Fatalf("payload\n%s\nwant\n%s", b, want)
	}

	// A grouped reminder carries neither actions nor key, and a message
	// that names no channel is a reminder.
	m := Message{Title: "Water the plants", Body: "Due now", URL: "https://app.example.com/?item=a", Tag: "a", Key: "a"}
	if b := string(pushPayload(m)); b != `{"title":"Water the plants","body":"Due now","url":"https://app.example.com/?item=a","tag":"a","channel":"reminders"}` {
		t.Fatalf("reminder payload %s", b)
	}

	// A reminder for one item carries Done and Snooze with its key.
	m.Actions = []Action{{Action: ActionDone, Title: "Done"}, {Action: ActionSnooze, Title: "Snooze 1 hour"}}
	if b := string(pushPayload(m)); b != `{"title":"Water the plants","body":"Due now","url":"https://app.example.com/?item=a","tag":"a","channel":"reminders",`+
		`"actions":[{"action":"done","title":"Done"},{"action":"snooze","title":"Snooze 1 hour"}],"key":"a"}` {
		t.Fatalf("reminder payload %s", b)
	}
	// ntfy cannot post them, so it gets no buttons for them.
	if a := ntfyActions(m.Actions); a != "" {
		t.Fatalf("ntfy actions for a reminder: %q", a)
	}
}

func TestNtfyActions(t *testing.T) {
	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) { got = q.Header }))
	defer srv.Close()
	n := &Ntfy{URL: srv.URL, Topic: "plants"}
	if err := n.Notify(context.Background(), agentMessage()); err != nil {
		t.Fatal(err)
	}
	want := "view, Reply, https://app.example.com/?item=laptop%2Fgarden%2Fgd-1&reply=1, clear=true; " +
		"view, Open, https://app.example.com/?item=laptop%2Fgarden%2Fgd-1, clear=true"
	if a := got.Get("Actions"); a != want {
		t.Fatalf("Actions = %q\nwant %q", a, want)
	}
	if got.Get("Title") != "planner replied" || got.Get("Click") != "https://app.example.com/?item=laptop%2Fgarden%2Fgd-1" {
		t.Fatalf("headers %v", got)
	}

	// A value with a comma is quoted; a reminder has no Actions header.
	if a := ntfyActions([]Action{{Action: ActionOpen, Title: "Open, now", URL: "https://app.example.com/"}}); a != `view, "Open, now", https://app.example.com/, clear=true` {
		t.Fatalf("quoted = %q", a)
	}
	if err := n.Notify(context.Background(), Message{Title: "Water the plants"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["Actions"]; ok {
		t.Fatal("a reminder sent an Actions header")
	}
}

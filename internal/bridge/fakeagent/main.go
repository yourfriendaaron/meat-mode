// Command fakeagent stands in for Claude Code in the bridge's tests. It
// speaks the same stream-json on stdin and stdout as `claude -p
// --input-format stream-json --output-format stream-json --verbose
// --replay-user-messages`, takes --resume and --max-budget-usd, and really
// runs bd, so the bead changes as it would with a real agent. No model is
// called.
//
// It echoes each stdin message as a user line with isReplay, at its next
// tool step. Each turn does what words in its messages say. The first
// message of a run is the bridge's prompt, so for it the words are the
// newest comment that names one of the words below on the bead it names
// with `bd show <id>`, else the bead's title. A follow-up the bridge writes
// as a comment before the fake reads the bead then cannot hide the note
// that started the run. Words:
//
//	reply   write a note with bd comment, then end the turn
//	close   write a note and close the bead with bd close
//	slow    take a few tool steps first, FAKEAGENT_STEP apart (default 500ms), so a follow-up lands mid-turn
//	usage   report a rejected rate limit, then end the turn with a usage-limit error
//	budget  end the turn with the max-budget error
//	fail    end the turn with an error result
//	crash   exit 1 mid-turn, without a result
//	hang    never end the turn; it ignores SIGTERM and starts a child that also waits forever
//
// With none of them it reads the bead and ends the turn without changing it.
// When stdin closes it exits 0.
//
// Build it with `go build -o <dir> ./internal/bridge/fakeagent`.
package main

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
	"time"
)

var (
	out     = json.NewEncoder(os.Stdout)
	session string
	toolN   int
	beadRe  = regexp.MustCompile("bd show ([A-Za-z0-9][A-Za-z0-9._-]*)")
	bead    string
)

func main() {
	resume := ""
	for i, a := range os.Args[1:] {
		if a == "--resume" && i+2 < len(os.Args) {
			resume = os.Args[i+2]
		}
	}
	session = resume
	if session == "" {
		b := make([]byte, 16)
		_, _ = rand.Read(b)
		x := hex.EncodeToString(b)
		session = x[:8] + "-" + x[8:12] + "-" + x[12:16] + "-" + x[16:20] + "-" + x[20:]
	}
	cwd, _ := os.Getwd()
	emit(map[string]any{"type": "system", "subtype": "init", "cwd": cwd, "session_id": session,
		"tools": []string{"Bash", "Edit", "Read"}, "model": "fake", "permissionMode": "auto",
		"claude_code_version": "fake", "fake_resume": resume})

	in := make(chan string)
	go func() {
		sc := bufio.NewScanner(os.Stdin)
		sc.Buffer(make([]byte, 0, 64<<10), 16<<20)
		for sc.Scan() {
			var m struct {
				Type    string `json:"type"`
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			}
			if json.Unmarshal(sc.Bytes(), &m) == nil && m.Type == "user" {
				in <- m.Message.Content
			}
		}
		close(in)
	}()

	first := true
	for text := range in {
		words := replay(text)
		if first {
			// The prompt's own words are the bridge's, not the script.
			if m := beadRe.FindStringSubmatch(text); m != nil {
				bead = m[1]
			}
			words = beadWords()
			first = false
		}
		turn(words, in)
	}
}

// turn does one turn of work, folding in messages that arrive at its tool
// steps, and ends it with a result line.
func turn(words string, in chan string) {
	has := func(w string) bool { return strings.Contains(" "+words+" ", " "+w+" ") }
	step := func(name string, input map[string]any, result string) {
		toolN++
		id := fmt.Sprintf("toolu_fake%d", toolN)
		emit(map[string]any{"type": "assistant", "session_id": session, "message": map[string]any{
			"type": "message", "role": "assistant",
			"content": []any{map[string]any{"type": "tool_use", "id": id, "name": name, "input": input}}}})
		emit(map[string]any{"type": "user", "session_id": session, "message": map[string]any{
			"role": "user", "content": []any{map[string]any{"tool_use_id": id, "type": "tool_result", "content": result, "is_error": false}}}})
		// The tool boundary: messages sent meanwhile are read now.
		for {
			select {
			case text, ok := <-in:
				if !ok {
					return
				}
				words += " " + replay(text)
				continue
			default:
			}
			break
		}
	}
	emit(map[string]any{"type": "rate_limit_event", "session_id": session, "rate_limit_info": map[string]any{
		"status": rateStatus(has("usage")), "resetsAt": time.Now().Add(3 * time.Hour).Unix(), "rateLimitType": "five_hour"}})
	step("Bash", map[string]any{"command": "bd show " + bead, "description": "Read the bead"}, bd("show", "--", bead))
	if has("slow") {
		pause := 500 * time.Millisecond
		if d, err := time.ParseDuration(os.Getenv("FAKEAGENT_STEP")); err == nil {
			pause = d
		}
		for _, s := range []struct {
			name  string
			input map[string]any
		}{
			{"Edit", map[string]any{"file_path": "notes.txt", "old_string": "", "new_string": "Water the plants"}},
			{"Bash", map[string]any{"command": "go test ./...", "description": "Run the tests"}},
			{"Read", map[string]any{"file_path": "notes.txt"}},
		} {
			time.Sleep(pause)
			step(s.name, s.input, "ok")
		}
	}
	switch {
	case has("usage"):
		result(true, "success", "You've hit your usage limit · resets 3pm")
	case has("budget"):
		result(true, "error_max_budget_usd", "")
	case has("fail"):
		result(true, "error_during_execution", "Something went wrong")
	case has("crash"):
		os.Exit(1)
	case has("hang"):
		signal.Ignore(syscall.SIGTERM)
		step("Bash", map[string]any{"command": "sleep 600"}, "")
		_ = exec.Command("sleep", "600").Start()
		select {}
	case has("close"):
		step("Bash", map[string]any{"command": "bd comment " + bead + " Done"}, bd("comment", "--", bead, "Done: watered."))
		step("Bash", map[string]any{"command": "bd close " + bead}, bd("close", "--", bead))
		result(false, "success", "Closed the bead.")
	case has("reply"):
		step("Bash", map[string]any{"command": "bd comment " + bead + " Question"}, bd("comment", "--", bead, "Which plants?"))
		result(false, "success", "Asked a question.")
	default:
		result(false, "success", "Read the bead.")
	}
}

// replay echoes a received message, as --replay-user-messages does, and
// returns its words.
func replay(text string) string {
	emit(map[string]any{"type": "user", "session_id": session, "isReplay": true,
		"message": map[string]any{"role": "user", "content": text}})
	return strings.ToLower(text)
}

func result(isErr bool, subtype, text string) {
	emit(map[string]any{"type": "result", "subtype": subtype, "is_error": isErr, "result": text, "session_id": session,
		"total_cost_usd": 0.01, "num_turns": toolN, "terminal_reason": "completed", "permission_denials": []any{}})
}

func rateStatus(rejected bool) string {
	if rejected {
		return "rejected"
	}
	return "allowed"
}

// scriptWords are the words a turn acts on.
var scriptWords = []string{"reply", "close", "slow", "usage", "budget", "fail", "crash", "hang"}

// beadWords are the bead's newest comment that names a script word, else its
// title, in lower case.
func beadWords() string {
	if bead == "" {
		return ""
	}
	var list []struct {
		Title    string `json:"title"`
		Comments []struct {
			Text string `json:"text"`
		} `json:"comments"`
	}
	raw := bd("show", "--include-comments", "--json", "--", bead)
	if i := strings.IndexByte(raw, '['); i < 0 || json.Unmarshal([]byte(raw[i:]), &list) != nil || len(list) == 0 {
		return ""
	}
	c := list[0].Comments
	for i := len(c) - 1; i >= 0; i-- {
		words := strings.ToLower(c[i].Text)
		for _, w := range scriptWords {
			if strings.Contains(" "+words+" ", " "+w+" ") {
				return words
			}
		}
	}
	return strings.ToLower(list[0].Title)
}

func bd(args ...string) string {
	cmd := exec.Command("bd", args...)
	cmd.Env = append(os.Environ(), "BD_NON_INTERACTIVE=1", "NO_COLOR=1")
	b, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Fprintf(os.Stderr, "bd %v: %v\n%s", args, err, b)
	}
	return string(b)
}

func emit(v any) {
	_ = out.Encode(v)
}

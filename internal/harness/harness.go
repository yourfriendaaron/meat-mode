// Package harness is the interface the bridge runs coding agents through.
// Each harness (Claude Code, Codex, ...) is a package under
// internal/harness/<name> that implements Harness and registers itself;
// docs/harnesses.md is the contract.
package harness

import (
	"context"
	"regexp"
)

// Budget modes: how a harness's spending cap is enforced.
const (
	BudgetNative  = "native"  // the harness stops itself at the cap it is given
	BudgetMetered = "metered" // it reports cost as it goes; the bridge stops it at the cap
	BudgetNone    = "none"    // it reports no cost; a run has the timeout only
)

// Budgets are the budget modes, in order.
var Budgets = []string{BudgetNative, BudgetMetered, BudgetNone}

// Tool kinds a harness maps its tool calls onto, for the run's activity line.
const (
	ToolShell  = "shell"  // a shell command; Tool.Command holds it
	ToolEdit   = "edit"   // a change to an existing file
	ToolWrite  = "write"  // a new or rewritten file
	ToolRead   = "read"   // reading a file
	ToolSearch = "search" // searching or listing files
	ToolWeb    = "web"    // a web search or fetch
	ToolAgent  = "agent"  // starting a helper agent
	ToolPlan   = "plan"   // updating its own plan or to-do list
	ToolOther  = "other"  // anything else
)

// ToolKinds are the tool kinds, in order.
var ToolKinds = []string{ToolShell, ToolEdit, ToolWrite, ToolRead, ToolSearch, ToolWeb, ToolAgent, ToolPlan, ToolOther}

// Info is what a harness declares, fixed for its life.
type Info struct {
	// Name is the registry key, lower case: the value of
	// MEAT_MODE_AGENT_HARNESS that picks it.
	Name string
	// Title is the name the app shows, such as "Claude Code".
	Title string
	// InstructionFile is the file in a repo the harness reads standing
	// instructions from, such as AGENTS.md. The app names it in the
	// instruction text for agents.
	InstructionFile string
	// Budget is one of the Budget modes.
	Budget string
	// Steer is true when a note can go into a live run (Harness.Note).
	// Otherwise the bridge holds notes until the turn ends and resumes the
	// session with them.
	Steer bool
	// EnvMarkers are the variables the harness sets in its own sessions,
	// which a run the bridge starts must not inherit. A name ending in "*"
	// matches every variable with that prefix.
	EnvMarkers []string
}

// Spec is one launch of the harness's process.
type Spec struct {
	Dir string // the repo the agent works in; also the process's working directory
	// Prompt is the first message: the tracker's prompt for a new run, or
	// the held notes when the bridge resumes after a turn.
	Prompt string
	// Session is the session to resume, or "" to start a new one. The
	// bridge passes only ids ValidSession accepts.
	Session string
	// BudgetUSD is the run's spending cap. A native harness passes it on;
	// the others ignore it.
	BudgetUSD float64
}

// Launch is the process to start.
type Launch struct {
	Args  []string // the program and its arguments, with no shell; Args[0] is the program
	Stdin []byte   // written to stdin first; may be empty
	// KeepStdin keeps stdin open after Stdin for notes. It must be true
	// exactly when Info().Steer is; otherwise the bridge closes stdin after
	// writing Stdin, which some harnesses wait for before they start.
	KeepStdin bool
}

// Harness is one coding agent the bridge can run.
type Harness interface {
	Info() Info
	// Check returns the version of the harness's program, or an error when
	// it is missing or too old.
	Check(ctx context.Context) (string, error)
	// Launch returns the process for one run or resume.
	Launch(s Spec) (Launch, error)
	// Note returns the stdin bytes that put a note into a live run. The
	// bridge calls it only when Info().Steer is true.
	Note(text string) []byte
	// NewDecoder returns a decoder for one process's stdout.
	NewDecoder() Decoder
}

// Decoder reads one process's stdout, one line at a time. It may keep state
// between lines; a new process gets a new decoder.
type Decoder interface {
	// Line returns what one line says, in order. A line it does not know,
	// or that is not valid for its format, returns nil; it never panics.
	Line(raw []byte) []Event
}

// Event is one thing a line of output says. Exactly one field is set,
// except that Session may come with any other.
type Event struct {
	// Session is the harness's session id, when the line names it.
	Session string `json:"session,omitempty"`
	// Tool is a tool call the agent started.
	Tool *Tool `json:"tool,omitempty"`
	// Read is how many more of the messages the bridge wrote to stdin the
	// harness has taken in with this line (an increment, such as 1 for each
	// echoed message), for a harness that says so. When a harness never
	// says, a turn's end counts every message sent before it as read.
	Read int `json:"read,omitempty"`
	// CostUSD is spending this line adds to the run, for a metered harness.
	CostUSD float64 `json:"costUsd,omitempty"`
	// Limit says the agent hit its usage limit.
	Limit *Limit `json:"limit,omitempty"`
	// End says the turn is over.
	End *End `json:"end,omitempty"`
}

// Tool is one tool call. The bridge shows no more than the first word or
// two of Command and the base name of File.
type Tool struct {
	Kind    string `json:"kind"`              // one of ToolKinds
	Command string `json:"command,omitempty"` // ToolShell: the command line
	File    string `json:"file,omitempty"`    // the file it reads or writes, as the harness gave it
}

// Limit is a usage limit the agent hit.
type Limit struct {
	ResetsAt int64 `json:"resetsAt,omitempty"` // Unix seconds when it resets; 0 when not known
}

// End is the end of a turn.
type End struct {
	Error  bool   `json:"error,omitempty"`  // the turn failed
	Budget bool   `json:"budget,omitempty"` // a native harness stopped at its cap
	Text   string `json:"text,omitempty"`   // the final message or error text
}

var sessionRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// ValidSession reports whether s is a session id the bridge may resume.
// The id comes from the agent's own output and becomes an argument, so
// anything that could read as a flag, a separate argument or more than an
// id is refused.
func ValidSession(s string) bool { return sessionRE.MatchString(s) }

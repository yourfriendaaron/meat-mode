package bridge

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/yourfriendaaron/meat-mode/internal/api"
	"github.com/yourfriendaaron/meat-mode/internal/tracker"
)

// RunsEvery is the least time between two run lists sent to the hub.
const RunsEvery = time.Second

// DefaultGrace is how long an agent has to end after SIGTERM before the
// bridge sends SIGKILL.
const DefaultGrace = 5 * time.Second

// keepAsks is how many applied ask ids a bead's record keeps, so a replayed
// ask never starts a second run.
const keepAsks = 50

// errNoGroups is the start failure where the bridge cannot end an agent's
// process group (see proc_other.go).
var errNoGroups = errors.New("agent runs need Linux or macOS")

// maxDetail caps a run's detail sentence.
const maxDetail = 300

// AgentRunner runs agents on tasks, one run per task, and keeps what it knows in
// runs.json in Dir: each bead's session id (so the next ask resumes the
// conversation), its lifetime run count, its latest run and the process of a
// live one. Worktrees live in Dir/worktrees and each run's raw stream in
// Dir/logs.
type AgentRunner struct {
	Config AgentConfig
	Dir    string
	Log    *slog.Logger
	Now    func() time.Time
	Grace  time.Duration // DefaultGrace when zero

	mu      sync.Mutex
	records map[string]*beadRecord // by runKey
	live    map[string]*liveRun
	sets    map[string]map[string]string // repo -> the bridge's fingerprints of its last pushed set
	authors map[string]bool              // who the app writes as; see appAuthor
	touched map[string]bool              // repos whose beads a run end changed
	dirty   bool                         // the run list changed since the last send
	sentAt  time.Time                    // when the run list was last sent
	changed chan struct{}
	gone    bool // tests: the process "died"; nothing is written any more
	wg      sync.WaitGroup
}

// beadRecord is what the runner keeps about one bead.
type beadRecord struct {
	Repo     string      `json:"repo"`
	BeadID   string      `json:"beadId"`
	Session  string      `json:"session,omitempty"`  // the agent's session, for --resume
	Count    int         `json:"count"`              // lifetime runs; nothing resets it
	Run      *api.Run    `json:"run,omitempty"`      // the latest run, while the bead is listed
	Process  *procRecord `json:"process,omitempty"`  // while a run is live
	Asks     []string    `json:"asks,omitempty"`     // ask action ids applied, newest last
	Worktree string      `json:"worktree,omitempty"` // where its runs work
}

// procRecord is a live run's process. Its pid is also its process group.
type procRecord struct {
	PID   int    `json:"pid"`
	Start string `json:"start,omitempty"` // processStart at launch
}

// liveRun is a run whose process the runner started and has not reaped.
type liveRun struct {
	key  string
	rec  *beadRecord
	run  *api.Run // rec.Run
	repo Repo
	pid  int

	inbox       chan []byte // stdin lines; closed to close stdin
	inboxClosed bool
	written     int  // user messages sent, the prompt included
	replayed    int  // of those, echoed back (isReplay)
	sawReplay   bool // the command echoes messages at all
	result      *streamLine
	usage       string // detail when the agent hit a usage limit
	budget      bool
	badSession  string        // the last session id refused, to warn once per value
	ending      string        // a reason the runner ended it for: stopped, timeout, restarted
	again       *askRequest   // an ask that came while it was closing
	exited      chan struct{} // closed when the process is reaped
	done        chan struct{} // closed once the run settled
	timer       *time.Timer
	log         *os.File
}

// askRequest is one ask whose note, if any, is already on the bead.
type askRequest struct {
	ActionID string
	Repo     Repo
	BeadID   string
	Text     string // the note; "" for none
	Author   string
	Allowed  bool   // the repo's agents switch is on
	Creator  string // the bead's created_by
}

func runKey(repo, bead string) string { return repo + "\x00" + bead }

// OpenAgentRunner loads the runner's journal from dir and ends any run a previous
// bridge left live: its process group is killed if it is still there, and
// the run shows stopped with reason restarted.
func OpenAgentRunner(dir string, cfg AgentConfig, log *slog.Logger) (*AgentRunner, error) {
	if len(cfg.Command) == 0 {
		return nil, errors.New("no agent command is set")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	r := &AgentRunner{Config: cfg, Dir: dir, Log: log}
	if err := r.load(); err != nil {
		return nil, err
	}
	r.recover()
	return r, nil
}

func (r *AgentRunner) init() {
	if r.records == nil {
		r.records = map[string]*beadRecord{}
	}
	if r.live == nil {
		r.live = map[string]*liveRun{}
		r.sets = map[string]map[string]string{}
		r.authors = map[string]bool{}
		r.touched = map[string]bool{}
		r.changed = make(chan struct{}, 1)
	}
}

func (r *AgentRunner) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func (r *AgentRunner) log() *slog.Logger {
	if r.Log != nil {
		return r.Log
	}
	return slog.Default()
}

func (r *AgentRunner) grace() time.Duration {
	if r.Grace > 0 {
		return r.Grace
	}
	return DefaultGrace
}

func (r *AgentRunner) path() string { return filepath.Join(r.Dir, "runs.json") }

func (r *AgentRunner) load() error {
	r.init()
	b, err := os.ReadFile(r.path())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var file struct {
		Beads []*beadRecord `json:"beads"`
	}
	if err := json.Unmarshal(b, &file); err != nil {
		// Starting over would reset every bead's run count.
		return fmt.Errorf("read %s: %w", r.path(), err)
	}
	for _, rec := range file.Beads {
		r.records[runKey(rec.Repo, rec.BeadID)] = rec
	}
	return nil
}

// save writes the journal atomically. Callers hold mu.
func (r *AgentRunner) save() error {
	if r.gone {
		return nil
	}
	recs := make([]*beadRecord, 0, len(r.records))
	for _, rec := range r.records {
		recs = append(recs, rec)
	}
	slices.SortFunc(recs, func(a, b *beadRecord) int {
		return strings.Compare(runKey(a.Repo, a.BeadID), runKey(b.Repo, b.BeadID))
	})
	b, err := json.MarshalIndent(map[string]any{"beads": recs}, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(r.path(), append(b, '\n'), 0o600)
}

// saveOrLog saves and logs a failure. Callers hold mu.
func (r *AgentRunner) saveOrLog() {
	if err := r.save(); err != nil {
		r.log().Error("cannot write the agent journal", "file", r.path(), "err", err)
	}
}

// recover ends the runs a previous bridge process left live.
func (r *AgentRunner) recover() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, rec := range r.records {
		if rec.Process != nil {
			p := rec.Process
			switch {
			case !processAlive(p.PID):
				// The leader is gone. Its group id cannot be reused while
				// anything it started is left in the group, so this only
				// reaches the run's own leftovers.
				_ = killGroup(p.PID)
			case p.Start == "" || processStart(p.PID) == p.Start:
				_ = killGroup(p.PID)
			default:
				r.log().Warn("an agent run's process id now belongs to another program; leaving it", "pid", p.PID)
			}
			rec.Process = nil
		}
		if rec.Run != nil && rec.Run.State == api.RunWorking {
			r.settleRun(rec.Run, api.RunStopped, api.ReasonRestarted, restartedDetail)
			r.log().Info("ended an agent run the bridge left behind", "repo", rec.Repo, "bead", rec.BeadID)
		}
	}
	r.dirty = true
	r.saveOrLog()
}

const restartedDetail = "The bridge stopped while the agent was working."

func (r *AgentRunner) record(repo, bead string) *beadRecord {
	k := runKey(repo, bead)
	rec := r.records[k]
	if rec == nil {
		rec = &beadRecord{Repo: repo, BeadID: bead}
		r.records[k] = rec
	}
	return rec
}

// notify marks the run list changed. Callers hold mu.
func (r *AgentRunner) notify() {
	r.dirty = true
	select {
	case r.changed <- struct{}{}:
	default:
	}
}

// Changed is signalled when the run list changed.
func (r *AgentRunner) Changed() <-chan struct{} {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.init()
	return r.changed
}

// NoteAuthor records a name the app writes notes as, so its notes are never
// taken for the agent's.
func (r *AgentRunner) NoteAuthor(name string) {
	if name == "" {
		return
	}
	r.mu.Lock()
	r.init()
	r.authors[name] = true
	r.mu.Unlock()
}

// appAuthor reports whether name is one the app writes as: an action's
// author, or the "<name>-app" form the hub gives every note. Callers hold mu.
func (r *AgentRunner) appAuthor(name string) bool {
	return r.authors[name] || name == defaultActor || strings.HasSuffix(name, "-app")
}

// agentName is the name shown for a bead's agent: its creator, unless the app
// created it, else the configured name. Callers hold mu.
func (r *AgentRunner) agentName(creator string) string {
	if creator == "" || r.appAuthor(creator) {
		return r.Config.Name
	}
	return creator
}

// Ask applies an ask whose note is already on the bead. On a live run the
// note goes to the agent; otherwise a run starts, unless a guard refuses it,
// and then the run shows stopped with that reason. An ask id seen before
// changes nothing. A repo whose tracker runs no agents is ignored.
func (r *AgentRunner) Ask(req askRequest) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.init()
	if r.gone || agentOf(req.Repo.Tracker) == nil {
		return
	}
	if req.Author != "" {
		r.authors[req.Author] = true
	}
	rec := r.record(req.Repo.Name, req.BeadID)
	// A second ledger beside the replay journal: replay hands an ask to the
	// runner even when the journal returns a cached result (the hub never
	// heard it), so this is what keeps that ask from starting a second run.
	if slices.Contains(rec.Asks, req.ActionID) {
		return
	}
	rec.Asks = append(rec.Asks, req.ActionID)
	if n := len(rec.Asks); n > keepAsks {
		rec.Asks = slices.Clone(rec.Asks[n-keepAsks:])
	}
	if lr := r.live[runKey(req.Repo.Name, req.BeadID)]; lr != nil {
		if req.Text == "" {
			r.saveOrLog()
			return
		}
		if !lr.inboxClosed {
			select {
			case lr.inbox <- userLine(agentOf(req.Repo.Tracker).FollowUp(req.BeadID, req.Text)):
				lr.written++
				lr.run.Queued = max(0, lr.written-lr.replayed)
				r.notify()
				r.saveOrLog()
				return
			default:
			}
		}
		// It is finishing: start again once it has ended.
		lr.again = &req
		r.saveOrLog()
		return
	}
	r.start(req, rec)
}

// start starts a run on rec, or records why it cannot. Callers hold mu.
func (r *AgentRunner) start(req askRequest, rec *beadRecord) {
	run := &api.Run{ID: newRunID(), Agent: r.agentName(req.Creator), StartedAt: r.now().UTC(), Count: rec.Count}
	rec.Run = run
	refuse := func(reason, detail string) {
		r.settleRun(run, api.RunStopped, reason, detail)
		r.notify()
		r.saveOrLog()
		r.log().Info("agent run refused", "repo", req.Repo.Name, "bead", req.BeadID, "reason", reason)
	}
	switch {
	case !req.Allowed:
		refuse(api.ReasonNotAllowed, "Agents are switched off for this repo on this machine.")
		return
	case rec.Count >= r.Config.RunsPerTask:
		refuse(api.ReasonRunLimit, fmt.Sprintf("This task has used all %d of its agent runs.", r.Config.RunsPerTask))
		return
	case len(r.live) >= r.Config.Concurrent:
		refuse(api.ReasonBusy, fmt.Sprintf("%s already running on this machine, the most it allows.", agents(len(r.live))))
		return
	}
	// The count is on disk before anything starts, so no crash can give a
	// bead a run it never counted.
	rec.Count++
	run.Count = rec.Count
	run.State = api.RunWorking
	run.Activity = "Starting"
	if err := r.save(); err != nil {
		r.log().Error("cannot write the agent journal", "file", r.path(), "err", err)
		r.settleRun(run, api.RunStopped, api.ReasonFailed, "The bridge could not write its agent journal.")
		r.notify()
		return
	}
	lr, err := r.launch(req, rec, run)
	if err != nil {
		r.log().Warn("agent run failed to start", "repo", req.Repo.Name, "bead", req.BeadID, "err", err)
		r.settleRun(run, api.RunStopped, api.ReasonFailed, clipText(err.Error(), maxDetail))
		r.notify()
		r.saveOrLog()
		return
	}
	r.live[lr.key] = lr
	rec.Process = &procRecord{PID: lr.pid, Start: processStart(lr.pid)}
	r.notify()
	r.saveOrLog()
	r.log().Info("agent run started", "repo", req.Repo.Name, "bead", req.BeadID, "run", run.ID, "count", run.Count)
}

func agents(n int) string {
	if n == 1 {
		return "1 agent is"
	}
	return strconv.Itoa(n) + " agents are"
}

// launch starts the agent process in the bead's worktree. Callers hold mu.
func (r *AgentRunner) launch(req askRequest, rec *beadRecord, run *api.Run) (*liveRun, error) {
	if agentProcAttr() == nil {
		return nil, errNoGroups
	}
	wt, cwd, err := r.worktree(req.Repo, req.BeadID)
	if err != nil {
		return nil, err
	}
	if rec.Worktree != wt {
		// A new worktree has none of the old conversation's files.
		rec.Session = ""
	}
	rec.Worktree = wt
	logDir := r.logDir(req.Repo.Name, req.BeadID)
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		return nil, fmt.Errorf("cannot make the run log folder: %w", err)
	}
	logf, err := os.OpenFile(filepath.Join(logDir, run.ID+".jsonl"), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("cannot write the run log: %w", err)
	}
	errf, err := os.OpenFile(filepath.Join(logDir, run.ID+".stderr"), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		logf.Close()
		return nil, fmt.Errorf("cannot write the run log: %w", err)
	}
	defer errf.Close() // the child keeps its own copy
	cmd := exec.Command(r.Config.Command[0], r.Config.args(rec.Session)...)
	cmd.Dir = cwd
	ag := agentOf(req.Repo.Tracker)
	cmd.Env = agentEnv(os.Environ(), run.Agent, ag)
	cmd.SysProcAttr = agentProcAttr()
	cmd.Stderr = errf
	out, outW, err := os.Pipe()
	if err != nil {
		logf.Close()
		return nil, err
	}
	// The child writes straight to the pipe, so Wait never waits on a
	// grandchild that kept stdout open.
	cmd.Stdout = outW
	stdin, err := cmd.StdinPipe()
	if err != nil {
		logf.Close()
		out.Close()
		outW.Close()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		logf.Close()
		out.Close()
		outW.Close()
		return nil, fmt.Errorf("the agent command could not start: %w", err)
	}
	outW.Close()
	lr := &liveRun{
		key: runKey(req.Repo.Name, req.BeadID), rec: rec, run: run, repo: req.Repo, pid: cmd.Process.Pid,
		inbox: make(chan []byte, 64), exited: make(chan struct{}), done: make(chan struct{}), log: logf,
	}
	lr.inbox <- userLine(ag.Prompt(req.BeadID))
	lr.written = 1
	lr.timer = time.AfterFunc(r.Config.Timeout, func() { r.end(lr, api.ReasonTimeout) })
	r.wg.Add(1)
	go r.watch(lr, cmd, out, stdin)
	return lr, nil
}

func (r *AgentRunner) logDir(repo, bead string) string {
	return filepath.Join(r.Dir, "logs", filepath.FromSlash(repo), bead)
}

// watch feeds stdin, reads the stream until the process ends, and settles
// the run.
func (r *AgentRunner) watch(lr *liveRun, cmd *exec.Cmd, out *os.File, stdin io.WriteCloser) {
	defer r.wg.Done()
	go func() {
		var err error
		for line := range lr.inbox {
			if err == nil {
				_, err = stdin.Write(line)
			}
		}
		stdin.Close()
	}()
	read := make(chan struct{})
	go func() {
		defer close(read)
		sc := bufio.NewScanner(out)
		sc.Buffer(make([]byte, 0, 64<<10), 16<<20)
		for sc.Scan() {
			line := sc.Bytes()
			_, _ = lr.log.Write(line)
			_, _ = lr.log.Write([]byte{'\n'})
			r.onLine(lr, line)
		}
		_, _ = io.Copy(lr.log, out) // past a line too long to read, keep the raw rest
	}()
	waitErr := cmd.Wait()
	close(lr.exited)
	// Sweep up whatever it left in its group, so nothing outlives the run.
	_ = killGroup(lr.pid)
	select {
	case <-read:
	case <-time.After(5 * time.Second):
		out.Close() // something outside the group still holds the pipe
		<-read
	}
	out.Close()
	lr.log.Close()
	r.settle(lr, waitErr)
}

// streamLine is the part of a stream-json line the runner reads.
type streamLine struct {
	Type      string `json:"type"`
	Subtype   string `json:"subtype"`
	SessionID string `json:"session_id"`
	IsReplay  bool   `json:"isReplay"`
	Message   *struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
	RateLimit *struct {
		Status   string `json:"status"`
		ResetsAt int64  `json:"resetsAt"`
	} `json:"rate_limit_info"`
	IsError bool    `json:"is_error"`
	Result  string  `json:"result"`
	Cost    float64 `json:"total_cost_usd"`
}

// onLine reads one line of the agent's stream. Unknown lines are skipped.
func (r *AgentRunner) onLine(lr *liveRun, raw []byte) {
	var m streamLine
	if json.Unmarshal(raw, &m) != nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if m.SessionID != "" && lr.rec.Session != m.SessionID {
		if ValidSessionID(m.SessionID) {
			lr.rec.Session = m.SessionID
			r.saveOrLog()
		} else if lr.badSession != m.SessionID {
			// The id becomes the argument of --resume on the next run, so a
			// malformed one is ignored, never stored.
			lr.badSession = m.SessionID
			r.log().Warn("ignoring a malformed session id from the agent; the next run starts fresh",
				"repo", lr.repo.Name, "bead", lr.rec.BeadID, "session", m.SessionID)
		}
	}
	switch m.Type {
	case "user":
		if m.IsReplay {
			lr.replayed++
			lr.sawReplay = true
			r.setQueued(lr)
		}
	case "assistant":
		if m.Message == nil {
			return
		}
		var content []struct {
			Type  string          `json:"type"`
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
		}
		_ = json.Unmarshal(m.Message.Content, &content)
		for _, c := range content {
			if c.Type == "tool_use" {
				if a := describeTool(c.Name, c.Input, agentOf(lr.repo.Tracker)); a != lr.run.Activity {
					lr.run.Activity = a
					r.notify()
				}
			}
		}
	case "rate_limit_event":
		if m.RateLimit != nil && m.RateLimit.Status != "allowed" && m.RateLimit.Status != "allowed_warning" {
			lr.usage = usageDetail(m.RateLimit.ResetsAt)
		}
	case "result":
		lr.result = &m
		if strings.Contains(m.Subtype, "budget") {
			lr.budget = true
		}
		if m.IsError && lr.usage == "" && looksLikeUsageLimit(m.Result) {
			lr.usage = usageDetail(0)
		}
		if !lr.sawReplay {
			// A command that never echoes its input cannot say what it
			// read; a finished turn read everything sent before it.
			lr.replayed = lr.written
		}
		r.setQueued(lr)
		// The turn is over. With nothing on its way, closing stdin lets the
		// agent exit; after an error or a limit, the run is over either way
		// (a queued note stays on the bead for the next ask).
		if lr.written <= lr.replayed || m.IsError || lr.usage != "" || lr.budget {
			r.closeInbox(lr)
		}
	}
}

func (r *AgentRunner) setQueued(lr *liveRun) {
	if q := max(0, lr.written-lr.replayed); q != lr.run.Queued {
		lr.run.Queued = q
		r.notify()
	}
}

func (r *AgentRunner) closeInbox(lr *liveRun) {
	if !lr.inboxClosed {
		lr.inboxClosed = true
		close(lr.inbox)
	}
}

func looksLikeUsageLimit(s string) bool {
	s = strings.ToLower(s)
	return strings.Contains(s, "usage limit") || strings.Contains(s, "limit reached") || strings.Contains(s, "hit your limit")
}

func usageDetail(resetsAt int64) string {
	if resetsAt <= 0 {
		return "The agent hit its usage limit."
	}
	t := time.Unix(resetsAt, 0).Local()
	return "The agent hit its usage limit. It resets at " + t.Format("15:04 MST on 2 Jan") + "."
}

// end makes the agent end: stdin closes, the group gets SIGTERM, and SIGKILL
// after the grace time.
func (r *AgentRunner) end(lr *liveRun, reason string) {
	r.mu.Lock()
	if lr.ending == "" {
		lr.ending = reason
	}
	r.closeInbox(lr)
	r.mu.Unlock()
	_ = terminateGroup(lr.pid)
	select {
	case <-lr.exited:
	case <-time.After(r.grace()):
		_ = killGroup(lr.pid)
	}
}

// settle records how a run ended, once its process was reaped.
func (r *AgentRunner) settle(lr *liveRun, waitErr error) {
	r.mu.Lock()
	lr.timer.Stop()
	ending, result, usage, budget := lr.ending, lr.result, lr.usage, lr.budget
	r.mu.Unlock()

	state, reason, detail := api.RunStopped, "", ""
	switch {
	case ending == api.ReasonStopped:
		reason = ending
	case ending == api.ReasonTimeout:
		reason, detail = ending, "It ran past the time limit of "+humanDuration(r.Config.Timeout)+"."
	case ending == api.ReasonRestarted:
		reason, detail = ending, restartedDetail
	case usage != "":
		reason, detail = api.ReasonUsageLimit, usage
	case budget:
		reason, detail = api.ReasonBudget, "It spent its budget of $"+strconv.FormatFloat(r.Config.BudgetUSD, 'f', 2, 64)+" for one run."
	case result == nil:
		reason, detail = api.ReasonCrashed, "The agent process ended before it finished ("+exitText(waitErr)+")."
	case result.IsError:
		reason, detail = api.ReasonFailed, "The agent reported an error."
		if t := strings.TrimSpace(result.Result); t != "" {
			detail = "The agent reported an error: " + clipText(t, maxDetail-30)
		}
	default:
		state, reason, detail = r.outcome(lr)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.settleRun(lr.run, state, reason, detail)
	r.closeInbox(lr)
	lr.rec.Process = nil
	delete(r.live, lr.key)
	r.touched[lr.repo.Name] = true
	r.notify()
	r.saveOrLog()
	close(lr.done)
	r.log().Info("agent run ended", "repo", lr.repo.Name, "bead", lr.rec.BeadID, "run", lr.run.ID, "state", state, "reason", reason)
	if again := lr.again; again != nil && ending == "" && !r.gone {
		r.start(*again, lr.rec)
	}
}

// outcome reads the bead after a run that finished its turn: closed is
// done, a note from the agent since the run started is waiting, and
// anything else is no reply.
func (r *AgentRunner) outcome(lr *liveRun) (state, reason, detail string) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	i, err := lr.repo.Tracker.Show(ctx, lr.repo.Dir, lr.rec.BeadID)
	if err != nil {
		return api.RunStopped, api.ReasonNoReply, "The bridge could not read the task after the run."
	}
	if i.Status == tracker.StatusClosed {
		return api.RunDone, "", ""
	}
	since := lr.run.StartedAt.Truncate(time.Second)
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, c := range i.Comments {
		if !c.CreatedAt.Before(since) && c.Author != "" && !r.appAuthor(c.Author) {
			return api.RunWaiting, "", ""
		}
	}
	return api.RunStopped, api.ReasonNoReply, "The agent ended without a note or closing the task."
}

func (r *AgentRunner) settleRun(run *api.Run, state, reason, detail string) {
	end := r.now().UTC()
	run.State, run.Reason, run.Detail, run.EndedAt, run.Queued = state, reason, detail, &end, 0
}

func exitText(err error) string {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		if ee.ExitCode() >= 0 {
			return "exit status " + strconv.Itoa(ee.ExitCode())
		}
		return "killed by a signal"
	}
	if err != nil {
		return err.Error()
	}
	return "exit status 0"
}

func humanDuration(d time.Duration) string {
	switch {
	case d >= time.Hour && d%time.Hour == 0:
		return plural(int(d/time.Hour), "hour")
	case d >= time.Minute && d%time.Minute == 0:
		return plural(int(d/time.Minute), "minute")
	case d >= time.Second && d%time.Second == 0:
		return plural(int(d/time.Second), "second")
	}
	return d.String()
}

func plural(n int, unit string) string {
	if n == 1 {
		return "1 " + unit
	}
	return strconv.Itoa(n) + " " + unit + "s"
}

func clipText(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n-1]) + "…"
}

func newRunID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Stop ends the bead's live run and returns once it has ended. With no live
// run it changes nothing.
func (r *AgentRunner) Stop(repo, bead string) {
	r.mu.Lock()
	r.init()
	lr := r.live[runKey(repo, bead)]
	if lr != nil {
		lr.again = nil
	}
	r.mu.Unlock()
	if lr == nil {
		return
	}
	r.end(lr, api.ReasonStopped)
	<-lr.done
}

// Shutdown ends every live run as restarted and waits for them, for a bridge
// that is stopping.
func (r *AgentRunner) Shutdown() {
	r.mu.Lock()
	r.init()
	var lrs []*liveRun
	for _, lr := range r.live {
		lr.again = nil
		lrs = append(lrs, lr)
	}
	r.mu.Unlock()
	for _, lr := range lrs {
		go r.end(lr, api.ReasonRestarted)
	}
	for _, lr := range lrs {
		<-lr.done
	}
}

// Listed tells the runner a repo's set as last sent to the hub: the bridge's
// fingerprints by bead id. The runner keeps set and only reads it, so the
// caller must replace it, never change it. A settled run on a bead that left
// the set is dropped, and its worktree removed when Git calls it clean (see
// removeWorktree).
func (r *AgentRunner) Listed(repo string, set map[string]string) {
	r.mu.Lock()
	r.init()
	r.sets[repo] = set
	var gone []*beadRecord
	for k, rec := range r.records {
		if _, in := set[rec.BeadID]; rec.Repo != repo || in || r.live[k] != nil {
			continue
		}
		if rec.Run != nil {
			rec.Run = nil
			r.notify()
		}
		if rec.Worktree != "" {
			gone = append(gone, rec)
		}
	}
	if len(gone) == 0 {
		r.saveOrLog()
		r.mu.Unlock()
		return
	}
	r.mu.Unlock()
	for _, rec := range gone {
		removed := r.removeWorktree(rec.Worktree, rec.BeadID)
		r.mu.Lock()
		if removed && r.live[runKey(rec.Repo, rec.BeadID)] == nil {
			rec.Worktree, rec.Session = "", ""
		}
		r.mu.Unlock()
	}
	r.mu.Lock()
	r.saveOrLog()
	r.mu.Unlock()
}

// putRuns sends a run list to the hub.
type putRuns func(context.Context, []api.RunReport) error

// Flush sends the run list through put when it changed and RunsEvery has
// passed since the last send, or always when force is set. After a failed
// send the list counts as changed, so the next Flush sends it again.
func (r *AgentRunner) Flush(ctx context.Context, put putRuns, force bool) error {
	r.mu.Lock()
	r.init()
	if !force && (!r.dirty || r.now().Sub(r.sentAt) < RunsEvery) {
		r.mu.Unlock()
		return nil
	}
	list := r.reports()
	r.mu.Unlock()
	return r.send(ctx, put, list)
}

// ReportApplying sends the run list through put now, naming actionID on the
// run of bead in repo, so the hub holds the run as the action left it before
// the action's result arrives and never counts the action twice.
func (r *AgentRunner) ReportApplying(ctx context.Context, put putRuns, actionID, repo, bead string) error {
	r.mu.Lock()
	r.init()
	list := r.reports()
	r.mu.Unlock()
	for i := range list {
		if list[i].Repo == repo && list[i].BeadID == bead {
			list[i].Applying = actionID
		}
	}
	return r.send(ctx, put, list)
}

// WaitTurn returns once RunsEvery has passed since the last send, or when ctx
// ends.
func (r *AgentRunner) WaitTurn(ctx context.Context) {
	r.mu.Lock()
	wait := RunsEvery - r.now().Sub(r.sentAt)
	r.mu.Unlock()
	if wait <= 0 {
		return
	}
	select {
	case <-ctx.Done():
	case <-time.After(wait):
	}
}

// send puts list and records the outcome. The bridge sends from one
// goroutine, so sends never overlap; the lock is not held during put so a
// slow hub never holds up the runs.
func (r *AgentRunner) send(ctx context.Context, put putRuns, list []api.RunReport) error {
	err := put(ctx, list)
	r.mu.Lock()
	defer r.mu.Unlock()
	if err != nil {
		r.dirty = true
		return err
	}
	r.sentAt = r.now()
	return nil
}

// reports returns the run list for the hub and marks it sent: every live
// run, and the latest settled run of each bead still in its repo's set (or
// of a repo not yet listed since the bridge started). Callers hold mu.
func (r *AgentRunner) reports() []api.RunReport {
	r.dirty = false
	out := []api.RunReport{}
	for k, rec := range r.records {
		if rec.Run == nil {
			continue
		}
		set, known := r.sets[rec.Repo]
		if _, in := set[rec.BeadID]; r.live[k] == nil && known && !in {
			continue
		}
		out = append(out, api.RunReport{Repo: rec.Repo, BeadID: rec.BeadID, Run: *rec.Run})
	}
	slices.SortFunc(out, func(a, b api.RunReport) int {
		return strings.Compare(runKey(a.Repo, a.BeadID), runKey(b.Repo, b.BeadID))
	})
	return out
}

// TakeTouched returns the repos whose beads a run end changed since the last
// call.
func (r *AgentRunner) TakeTouched() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.init()
	out := make([]string, 0, len(r.touched))
	for name := range r.touched {
		out = append(out, name)
	}
	clear(r.touched)
	slices.Sort(out)
	return out
}

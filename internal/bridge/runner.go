package bridge

import (
	"bufio"
	"cmp"
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
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/yourfriendaaron/meat-mode/internal/api"
	"github.com/yourfriendaaron/meat-mode/internal/harness"
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
	Config  AgentConfig
	Harness harness.Harness // what runs the agents
	Dir     string
	Log     *slog.Logger
	Now     func() time.Time
	Grace   time.Duration // DefaultGrace when zero

	mu      sync.Mutex
	records map[string]*beadRecord // by runKey
	live    map[string]*liveRun
	sets    map[string]map[string]string // repo -> the bridge's fingerprints of its last pushed set
	authors map[string]bool              // who the app writes as; see appAuthor
	touched map[string]bool              // repos whose beads a run end changed
	// removing holds the beads whose worktree Collect is removing, by
	// runKey, and waiting an ask for one, which starts once it is gone.
	removing map[string]bool
	waiting  map[string]askRequest
	kept     map[string]string // worktree -> why Collect last kept it, to log each reason once
	dirty    bool              // the run list changed since the last send
	sentAt   time.Time         // when the run list was last sent
	changed  chan struct{}
	gone     bool // tests: the process "died"; nothing is written any more
	// unavailable is why the harness's last check failed; asks are refused
	// while it is set.
	unavailable error
	wg          sync.WaitGroup
}

// beadRecord is what the runner keeps about one bead.
type beadRecord struct {
	Repo     string      `json:"repo"`
	BeadID   string      `json:"beadId"`
	Session  string      `json:"session,omitempty"`  // the agent's session, to resume
	Harness  string      `json:"harness,omitempty"`  // the harness Session belongs to; "" for claude
	Count    int         `json:"count"`              // lifetime runs; nothing resets it
	Run      *api.Run    `json:"run,omitempty"`      // the latest run, while the bead is listed
	Process  *procRecord `json:"process,omitempty"`  // while a run is live
	Asks     []string    `json:"asks,omitempty"`     // ask action ids applied, newest last
	Worktree string      `json:"worktree,omitempty"` // where its runs work
	// Idle is when its latest run ended, kept after Run is dropped.
	Idle *time.Time `json:"idle,omitempty"`
	// Main is the repo's main checkout, where the bead's branch is, so the
	// branch can go after its worktree.
	Main string `json:"main,omitempty"`
}

// procRecord is a live run's process. Its pid is also its process group.
type procRecord struct {
	PID   int    `json:"pid"`
	Start string `json:"start,omitempty"` // processStart at launch
}

// liveRun is a run the runner started and has not settled. A harness
// without Steer can take more than one process for it: the turn that ended
// well is resumed with the notes held meanwhile.
type liveRun struct {
	key  string
	rec  *beadRecord
	run  *api.Run // rec.Run
	repo Repo
	ag   tracker.Agent
	cwd  string
	proc *agentProc // the process now running, or the last one
	// limits are the time limit and budget the run started with; a change
	// in the app reaches only runs that start after it.
	limits Limits

	held       []string      // notes waiting for the turn to end, without Steer
	cost       float64       // spending so far, summed for a metered harness
	overBudget bool          // a metered run passed its cap
	startErr   error         // why a resume could not start
	badSession string        // the last session id refused, to warn once per value
	ending     string        // a reason the runner ended it for: stopped, timeout, restarted, budget
	again      *askRequest   // an ask that came while it was closing
	settling   bool          // its last process exited and it is settling
	done       chan struct{} // closed once the run settled
	timer      *time.Timer
	log        *os.File // every process's raw output
}

// agentProc is one process of a run.
type agentProc struct {
	pid         int
	inbox       chan []byte // stdin writes; closed to close stdin
	inboxClosed bool
	written     int  // messages sent on stdin, the prompt included
	read        int  // of those, taken in, as the harness says
	sawRead     bool // the harness says what it read at all
	end         *harness.End
	usage       string        // detail when the agent hit a usage limit
	exited      chan struct{} // closed when the process is reaped
}

// wentWell reports whether p's turn ended without an error, a limit or the
// budget, so held notes may resume it.
func (p *agentProc) wentWell() bool {
	return p.end != nil && !p.end.Error && !p.end.Budget && p.usage == ""
}

// askRequest is one ask whose note, if any, is already on the bead.
type askRequest struct {
	ActionID string
	Repo     Repo
	BeadID   string
	Text     string // the note; "" for none
	Author   string
	Allowed  bool // the repo's agents switch is on
}

func runKey(repo, bead string) string { return repo + "\x00" + bead }

// OpenAgentRunner loads the runner's journal from dir and ends any run a previous
// bridge left live: its process group is killed if it is still there, and
// the run shows stopped with reason restarted.
func OpenAgentRunner(dir string, cfg AgentConfig, h harness.Harness, log *slog.Logger) (*AgentRunner, error) {
	if h == nil {
		return nil, errors.New("no agent harness is set")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	r := &AgentRunner{Config: cfg, Harness: h, Dir: dir, Log: log}
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
		r.removing = map[string]bool{}
		r.waiting = map[string]askRequest{}
		r.kept = map[string]string{}
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

// agentName is the name shown for the agent that runs: the configured name,
// else the harness's title. The bead's creator is not it: a bead a person or
// another agent raised still runs in this machine's harness.
func (r *AgentRunner) agentName() string {
	return cmp.Or(strings.TrimSpace(r.Config.Name), r.Harness.Info().Title, "agent")
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
		text := agentOf(req.Repo.Tracker).FollowUp(req.BeadID, req.Text)
		p := lr.proc
		switch {
		case lr.settling:
		case r.Harness.Info().Steer:
			if !p.inboxClosed {
				select {
				case p.inbox <- r.Harness.Note(text):
					p.written++
					lr.run.Queued = max(0, p.written-p.read)
					r.notify()
					r.saveOrLog()
					return
				default:
				}
			}
		case p.end == nil || p.wentWell():
			// The note waits for the turn to end, then resumes it.
			lr.held = append(lr.held, text)
			lr.run.Queued = len(lr.held)
			r.notify()
			r.saveOrLog()
			return
		}
		// It is finishing: start again once it has ended.
		lr.again = &req
		r.saveOrLog()
		return
	}
	if k := runKey(req.Repo.Name, req.BeadID); r.removing[k] {
		// Collect is removing its worktree: start once that is done.
		r.waiting[k] = req
		r.saveOrLog()
		return
	}
	r.start(req, rec)
}

// start starts a run on rec, or records why it cannot. Callers hold mu.
func (r *AgentRunner) start(req askRequest, rec *beadRecord) {
	run := &api.Run{ID: newRunID(), Agent: r.agentName(), StartedAt: r.now().UTC(), Count: rec.Count}
	rec.Run = run
	refuse := func(reason, detail string) {
		r.settleRun(run, api.RunStopped, reason, detail)
		r.notify()
		r.saveOrLog()
		r.log().Info("agent run refused", "repo", req.Repo.Name, "bead", req.BeadID, "reason", reason)
	}
	limits := r.Config.Limits()
	switch {
	case r.unavailable != nil:
		refuse(api.ReasonFailed, clipText(r.Harness.Info().Title+" is not available on this machine: "+r.unavailable.Error(), maxDetail))
		return
	case !req.Allowed:
		refuse(api.ReasonNotAllowed, "Agents are switched off for this repo on this machine.")
		return
	case rec.Count >= limits.RunsPerTask:
		refuse(api.ReasonRunLimit, fmt.Sprintf("This task has used all %d of its agent runs.", limits.RunsPerTask))
		return
	case len(r.live) >= limits.Concurrent:
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

// launch starts the run's first process in the bead's worktree. Callers
// hold mu.
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
	if common, err := git(wt, "rev-parse", "--path-format=absolute", "--git-common-dir"); err == nil {
		rec.Main = filepath.Dir(common)
	}
	logDir := r.logDir(req.Repo.Name, req.BeadID)
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		return nil, fmt.Errorf("cannot make the run log folder: %w", err)
	}
	logf, err := os.OpenFile(filepath.Join(logDir, run.ID+".jsonl"), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("cannot write the run log: %w", err)
	}
	ag := agentOf(req.Repo.Tracker)
	lr := &liveRun{
		key: runKey(req.Repo.Name, req.BeadID), rec: rec, run: run, repo: req.Repo, ag: ag, cwd: cwd,
		done: make(chan struct{}), log: logf, limits: r.Config.Limits(),
	}
	if err := r.spawn(lr, ag.Prompt(req.BeadID)); err != nil {
		logf.Close()
		return nil, err
	}
	lr.timer = time.AfterFunc(lr.limits.Timeout, func() { r.end(lr, api.ReasonTimeout) })
	return lr, nil
}

// legacyHarness is the harness of a session recorded before runs.json named
// one.
const legacyHarness = "claude"

// spawn starts one process of lr with prompt, resuming the bead's session
// when the configured harness recorded it. Callers hold mu.
func (r *AgentRunner) spawn(lr *liveRun, prompt string) error {
	name := r.Harness.Info().Name
	session := lr.rec.Session
	if cmp.Or(lr.rec.Harness, legacyHarness) != name {
		// Another harness's session means nothing to this one.
		session = ""
	}
	l, err := r.Harness.Launch(harness.Spec{Dir: lr.cwd, Prompt: prompt, Session: session, BudgetUSD: lr.limits.BudgetUSD})
	if err != nil {
		return fmt.Errorf("the agent command could not be made: %w", err)
	}
	if len(l.Args) == 0 {
		return errors.New("the agent harness gave no command")
	}
	logDir := r.logDir(lr.repo.Name, lr.rec.BeadID)
	errf, err := os.OpenFile(filepath.Join(logDir, lr.run.ID+".stderr"), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("cannot write the run log: %w", err)
	}
	defer errf.Close() // the child keeps its own copy
	cmd := exec.Command(l.Args[0], l.Args[1:]...)
	cmd.Dir = lr.cwd
	cmd.Env = agentEnv(os.Environ(), lr.run.Agent, lr.ag)
	cmd.SysProcAttr = agentProcAttr()
	cmd.Stderr = errf
	out, outW, err := os.Pipe()
	if err != nil {
		return err
	}
	// The child writes straight to the pipe, so Wait never waits on a
	// grandchild that kept stdout open.
	cmd.Stdout = outW
	stdin, err := cmd.StdinPipe()
	if err != nil {
		out.Close()
		outW.Close()
		return err
	}
	if err := cmd.Start(); err != nil {
		out.Close()
		outW.Close()
		return fmt.Errorf("the agent command could not start: %w", err)
	}
	outW.Close()
	p := &agentProc{pid: cmd.Process.Pid, inbox: make(chan []byte, 64), exited: make(chan struct{})}
	if len(l.Stdin) > 0 {
		p.inbox <- l.Stdin
		p.written = 1
	}
	if !l.KeepStdin {
		closeInbox(p)
	}
	lr.proc = p
	lr.rec.Process = &procRecord{PID: p.pid, Start: processStart(p.pid)}
	r.wg.Add(1)
	go r.watch(lr, p, cmd, out, stdin)
	return nil
}

func (r *AgentRunner) logDir(repo, bead string) string {
	return filepath.Join(r.Dir, "logs", filepath.FromSlash(repo), bead)
}

// watch feeds stdin and reads the output until the process ends, then
// resumes or settles the run.
func (r *AgentRunner) watch(lr *liveRun, p *agentProc, cmd *exec.Cmd, out *os.File, stdin io.WriteCloser) {
	defer r.wg.Done()
	go func() {
		var err error
		for line := range p.inbox {
			if err == nil {
				_, err = stdin.Write(line)
			}
		}
		stdin.Close()
	}()
	read := make(chan struct{})
	go func() {
		defer close(read)
		dec := r.Harness.NewDecoder()
		sc := bufio.NewScanner(out)
		sc.Buffer(make([]byte, 0, 64<<10), 16<<20)
		for sc.Scan() {
			line := sc.Bytes()
			_, _ = lr.log.Write(line)
			_, _ = lr.log.Write([]byte{'\n'})
			if evs := dec.Line(line); len(evs) > 0 {
				r.onEvents(lr, p, evs)
			}
		}
		_, _ = io.Copy(lr.log, out) // past a line too long to read, keep the raw rest
	}()
	waitErr := cmd.Wait()
	close(p.exited)
	// Sweep up whatever it left in its group, so nothing outlives the run.
	_ = killGroup(p.pid)
	select {
	case <-read:
	case <-time.After(5 * time.Second):
		out.Close() // something outside the group still holds the pipe
		<-read
	}
	out.Close()
	r.turnOver(lr, p, waitErr)
}

// turnOver follows a process's exit: with notes held and a turn that ended
// well, the session resumes with them in a new process, in the same run;
// otherwise the run settles.
func (r *AgentRunner) turnOver(lr *liveRun, p *agentProc, waitErr error) {
	r.mu.Lock()
	if lr.ending == "" && !r.gone && len(lr.held) > 0 && p.wentWell() {
		prompt := strings.Join(lr.held, "\n\n")
		lr.held = nil
		err := r.spawn(lr, prompt)
		if err == nil {
			lr.run.Queued = 0
			r.notify()
			r.saveOrLog()
			r.mu.Unlock()
			r.log().Info("agent run resumed with held notes", "repo", lr.repo.Name, "bead", lr.rec.BeadID, "run", lr.run.ID)
			return
		}
		r.log().Warn("agent run failed to resume", "repo", lr.repo.Name, "bead", lr.rec.BeadID, "err", err)
		lr.startErr = err
	}
	lr.settling = true
	r.mu.Unlock()
	r.settle(lr, p, waitErr)
}

// onEvents applies what one line of the agent's output says.
func (r *AgentRunner) onEvents(lr *liveRun, p *agentProc, evs []harness.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	info := r.Harness.Info()
	for _, e := range evs {
		if e.Session != "" {
			r.setSession(lr, e.Session)
		}
		switch {
		case e.Tool != nil:
			if a := describeTool(*e.Tool, lr.ag); a != lr.run.Activity {
				lr.run.Activity = a
				r.notify()
			}
		case e.Read > 0:
			p.read += e.Read
			p.sawRead = true
			r.setQueued(lr, p)
		case e.CostUSD > 0:
			if info.Budget != harness.BudgetMetered {
				continue
			}
			lr.cost += e.CostUSD
			if lr.cost > lr.limits.BudgetUSD && !lr.overBudget {
				lr.overBudget = true
				go r.end(lr, api.ReasonBudget)
			}
		case e.Limit != nil:
			// A later limit without a reset time keeps the one already known.
			if p.usage == "" || e.Limit.ResetsAt > 0 {
				p.usage = usageDetail(e.Limit.ResetsAt)
			}
		case e.End != nil:
			p.end = e.End
			if !p.sawRead {
				// A harness that never says what it read cannot; a finished
				// turn read everything sent before it.
				p.read = p.written
			}
			r.setQueued(lr, p)
			// The turn is over. With nothing on its way, closing stdin lets the
			// agent exit; after an error or a limit, the run is over either way
			// (a queued note stays on the bead for the next ask).
			if p.written <= p.read || !p.wentWell() {
				closeInbox(p)
			}
		}
	}
}

// setSession stores a session id the agent named, when it is one the bridge
// may resume. Callers hold mu.
func (r *AgentRunner) setSession(lr *liveRun, id string) {
	name := r.Harness.Info().Name
	if lr.rec.Session == id && cmp.Or(lr.rec.Harness, legacyHarness) == name {
		return
	}
	if harness.ValidSession(id) {
		lr.rec.Session, lr.rec.Harness = id, name
		r.saveOrLog()
	} else if lr.badSession != id {
		// The id becomes an argument of the next run, so a malformed one
		// is ignored, never stored.
		lr.badSession = id
		r.log().Warn("ignoring a malformed session id from the agent; the next run starts fresh",
			"repo", lr.repo.Name, "bead", lr.rec.BeadID, "session", id)
	}
}

// setQueued counts the notes a steering harness has not read. Callers hold
// mu.
func (r *AgentRunner) setQueued(lr *liveRun, p *agentProc) {
	if !r.Harness.Info().Steer {
		return
	}
	if q := max(0, p.written-p.read); q != lr.run.Queued {
		lr.run.Queued = q
		r.notify()
	}
}

func closeInbox(p *agentProc) {
	if !p.inboxClosed {
		p.inboxClosed = true
		close(p.inbox)
	}
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
	p := lr.proc
	closeInbox(p)
	r.mu.Unlock()
	_ = terminateGroup(p.pid)
	select {
	case <-p.exited:
	case <-time.After(r.grace()):
		_ = killGroup(p.pid)
	}
}

// settle records how a run ended, once its last process was reaped.
func (r *AgentRunner) settle(lr *liveRun, p *agentProc, waitErr error) {
	r.mu.Lock()
	lr.timer.Stop()
	ending, end, usage, startErr := lr.ending, p.end, p.usage, lr.startErr
	r.mu.Unlock()
	lr.log.Close()

	state, reason, detail, said := api.RunStopped, "", "", ""
	switch {
	case ending == api.ReasonStopped:
		reason = ending
	case ending == api.ReasonTimeout:
		reason, detail = ending, "It ran past the time limit of "+humanDuration(lr.limits.Timeout)+"."
	case ending == api.ReasonRestarted:
		reason, detail = ending, restartedDetail
	case ending == api.ReasonBudget:
		reason, detail = ending, budgetDetail(lr.limits)
	case startErr != nil:
		reason, detail = api.ReasonFailed, clipText(startErr.Error(), maxDetail)
	case usage != "":
		reason, detail = api.ReasonUsageLimit, usage
	case end != nil && end.Budget:
		reason, detail = api.ReasonBudget, budgetDetail(lr.limits)
	case end == nil:
		reason, detail = api.ReasonCrashed, "The agent process ended before it finished ("+exitText(waitErr)+")."
	case end.Error:
		reason, detail = api.ReasonFailed, "The agent reported an error."
		if t := strings.TrimSpace(end.Text); t != "" {
			detail = "The agent reported an error: " + clipText(t, maxDetail-30)
		}
	default:
		state, reason, detail, said = r.outcome(lr)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.settleRun(lr.run, state, reason, detail)
	// The last step ("Writing a note") is over; a settled run shows what the
	// agent said instead, or nothing.
	lr.run.Activity = said
	closeInbox(p)
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

func budgetDetail(l Limits) string {
	return "It spent its budget of $" + strconv.FormatFloat(l.BudgetUSD, 'f', 2, 64) + " for one run."
}

// CheckHarness runs the harness's check and returns it as the hello names
// it. While the check fails, asks are refused and syncing carries on.
func (r *AgentRunner) CheckHarness(ctx context.Context) *api.HarnessInfo {
	v, err := r.Harness.Check(ctx)
	r.mu.Lock()
	r.unavailable = err
	cfg := r.Config
	r.mu.Unlock()
	if err != nil {
		v = ""
		r.log().Warn("the agent harness is not available, so asks will fail; see docs/harnesses.md",
			"harness", r.Harness.Info().Name, "err", err)
	}
	return harnessInfo(r.Harness, cfg, v)
}

// AgentConfig is the runner's configuration as it stands.
func (r *AgentRunner) AgentConfig() AgentConfig {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.Config
}

// SetSaved takes the limits the app set. Runs that start from now on keep
// them; a live run keeps the limits it started with.
func (r *AgentRunner) SetSaved(s SavedLimits) {
	r.mu.Lock()
	r.Config.Saved = s
	r.mu.Unlock()
}

// outcome reads the bead after a run that finished its turn: closed is
// done, a note from the agent since the run started is waiting, and
// anything else is no reply. said is the first line of the agent's latest
// note in the run, "" when it wrote none.
func (r *AgentRunner) outcome(lr *liveRun) (state, reason, detail, said string) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	i, err := lr.repo.Tracker.Show(ctx, lr.repo.Dir, lr.rec.BeadID)
	if err != nil {
		return api.RunStopped, api.ReasonNoReply, "The bridge could not read the task after the run.", ""
	}
	since := lr.run.StartedAt.Truncate(time.Second)
	r.mu.Lock()
	replied := false
	for _, c := range i.Comments {
		if !c.CreatedAt.Before(since) && c.Author != "" && !r.appAuthor(c.Author) {
			replied, said = true, firstLine(c.Text)
		}
	}
	r.mu.Unlock()
	switch {
	case i.Status == tracker.StatusClosed:
		return api.RunDone, "", "", said
	case replied:
		return api.RunWaiting, "", "", said
	}
	return api.RunStopped, api.ReasonNoReply, "The agent ended without a note or closing the task.", ""
}

// blockMark is a Markdown heading, quote or list mark at a line's start.
var blockMark = regexp.MustCompile(`^(#+|>|[-*+]|\d+[.)])\s+`)

// inlineMark is Markdown's bold and code marks.
var inlineMark = strings.NewReplacer("**", "", "__", "", "`", "")

// firstLine is a note's first line with text, without Markdown's marks,
// clipped to an activity line.
func firstLine(text string) string {
	for _, l := range strings.Split(text, "\n") {
		l = inlineMark.Replace(blockMark.ReplaceAllString(strings.TrimSpace(l), ""))
		if l = strings.Join(strings.Fields(l), " "); l != "" {
			return clip(l)
		}
	}
	return ""
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
// the set is dropped, and its worktree removed (see Collect).
func (r *AgentRunner) Listed(repo string, set map[string]string) {
	r.mu.Lock()
	r.init()
	r.sets[repo] = set
	gone := false
	for k, rec := range r.records {
		if _, in := set[rec.BeadID]; rec.Repo != repo || in || r.live[k] != nil {
			continue
		}
		if rec.Run != nil {
			rec.Idle = rec.Run.EndedAt
			rec.Run = nil
			r.notify()
		}
		gone = gone || rec.Worktree != ""
	}
	r.saveOrLog()
	r.mu.Unlock()
	if gone {
		r.Collect()
	}
}

// collectJob is one worktree Collect removes.
type collectJob struct {
	key, path, bead string
	rec             *beadRecord // nil for a folder the journal does not name
	left            bool        // the bead left its repo's synced set
}

// Collect removes the worktrees no run needs, when removeWorktree finds
// them safe to remove: a bead's once it has left its repo's synced set; a
// listed bead's once its latest run ended KeepWorktrees ago; and a folder
// under Dir/worktrees that the journal does not name, once it is that old.
// A listed bead keeps its branch and its session, so its next ask makes the
// worktree again at the same path on the same branch and resumes the
// conversation. An ask that comes while its worktree is being removed starts
// once that is done.
func (r *AgentRunner) Collect() {
	r.mu.Lock()
	r.init()
	if r.gone {
		r.mu.Unlock()
		return
	}
	now, keep := r.now(), cmp.Or(r.Config.KeepWorktrees, DefaultAgentConfig().KeepWorktrees)
	root := filepath.Join(r.Dir, "worktrees")
	var jobs []collectJob
	named := map[string]bool{}
	for k, rec := range r.records {
		if rec.Worktree == "" {
			continue
		}
		named[filepath.Clean(rec.Worktree)] = true
		if r.live[k] != nil || r.removing[k] {
			continue
		}
		set, synced := r.sets[rec.Repo]
		_, in := set[rec.BeadID]
		left := synced && !in
		if !left && (!isDir(rec.Worktree) || now.Sub(idleSince(rec)) < keep) {
			continue
		}
		r.removing[k] = true
		jobs = append(jobs, collectJob{key: k, path: rec.Worktree, bead: rec.BeadID, rec: rec, left: left})
	}
	for _, o := range unnamedWorktrees(root, named) {
		k := runKey(o.repo, o.bead)
		if r.live[k] != nil || r.removing[k] || now.Sub(o.made) < keep {
			continue
		}
		r.removing[k] = true
		jobs = append(jobs, collectJob{key: k, path: o.path, bead: o.bead})
	}
	r.mu.Unlock()
	for _, j := range jobs {
		r.collected(j, r.removeWorktree(j))
	}
	r.mu.Lock()
	removeEmptyDirs(root)
	r.mu.Unlock()
}

// collected ends j's removal: a bead that left the set forgets its removed
// worktree and session, and an ask that waited on the removal starts.
func (r *AgentRunner) collected(j collectJob, removed bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.removing, j.key)
	if removed && j.left {
		j.rec.Worktree, j.rec.Session, j.rec.Main = "", "", ""
	}
	if req, ok := r.waiting[j.key]; ok {
		delete(r.waiting, j.key)
		if !r.gone {
			r.start(req, r.record(req.Repo.Name, req.BeadID))
		}
	}
	r.saveOrLog()
}

// idleSince is when rec's latest run ended; the zero time when no run of it
// is known to have ended.
func idleSince(rec *beadRecord) time.Time {
	switch {
	case rec.Run != nil && rec.Run.EndedAt != nil:
		return *rec.Run.EndedAt
	case rec.Run == nil && rec.Idle != nil:
		return *rec.Idle
	}
	return time.Time{}
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

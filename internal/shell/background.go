package shell

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"slices"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// MaxFinishedJobs bounds how many finished jobs keep their output
	// readable. The oldest are dropped first.
	MaxFinishedJobs = 250
	// CompletedJobRetention is how long a finished job's output stays
	// readable.
	CompletedJobRetention = 8 * time.Hour
	// KillTimeout bounds how long Kill waits for a cancelled shell to
	// return. It only has to cover process teardown, which the exec
	// handlers bound themselves.
	KillTimeout = 5 * time.Second
)

// BackgroundShell represents a shell running in the background.
type BackgroundShell struct {
	ID          string
	Command     string
	Description string
	Shell       *Shell
	WorkingDir  string
	ctx         context.Context
	cancel      context.CancelFunc
	stdout      *cappedBuffer
	stderr      *cappedBuffer
	done        chan struct{}
	exitErr     error
	startedAt   time.Time
	completedAt atomic.Int64 // Unix nanoseconds when the job completed (0 if still running)
	// backgrounded records that the job was handed to the caller as a
	// background job rather than returned synchronously. Only those jobs
	// notify anyone when they finish.
	backgrounded atomic.Bool
	// cancelled records that the job was stopped on purpose. Killing a job
	// is not news: the caller asked for it, and its output is gone.
	cancelled atomic.Bool
}

// BackgroundShellManager manages background shell instances.
type BackgroundShellManager struct {
	mu     sync.Mutex
	shells map[string]*BackgroundShell
	// onComplete runs when a backgrounded job finishes, so the UI can tell
	// the user. Nil when nobody is listening.
	onComplete func(*BackgroundShell)
}

var (
	backgroundManager     *BackgroundShellManager
	backgroundManagerOnce sync.Once
	idCounter             atomic.Uint64
)

// newBackgroundShellManager creates a new BackgroundShellManager instance.
func newBackgroundShellManager() *BackgroundShellManager {
	return &BackgroundShellManager{shells: map[string]*BackgroundShell{}}
}

// GetBackgroundShellManager returns the singleton background shell manager.
func GetBackgroundShellManager() *BackgroundShellManager {
	backgroundManagerOnce.Do(func() {
		backgroundManager = newBackgroundShellManager()
	})
	return backgroundManager
}

// Start creates and starts a new background shell with the given command,
// unless maxJobs jobs are already running.
func (m *BackgroundShellManager) Start(ctx context.Context, workingDir string, blockFuncs []BlockFunc, command, description string, maxJobs int) (*BackgroundShell, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if running := m.prune(); running >= maxJobs {
		return nil, fmt.Errorf("maximum number of running background jobs (%d) reached. Please terminate or wait for some jobs to complete", maxJobs)
	}

	id := fmt.Sprintf("%03X", idCounter.Add(1))

	shell := NewShell(&Options{
		WorkingDir: workingDir,
		BlockFuncs: blockFuncs,
	})

	shellCtx, cancel := context.WithCancel(ctx)

	bgShell := &BackgroundShell{
		ID:          id,
		Command:     command,
		Description: description,
		WorkingDir:  workingDir,
		Shell:       shell,
		ctx:         shellCtx,
		cancel:      cancel,
		stdout:      newCappedBuffer(maxKeptOutput),
		stderr:      newCappedBuffer(maxKeptOutput),
		done:        make(chan struct{}),
		startedAt:   time.Now(),
	}

	m.shells[id] = bgShell

	go func() {
		defer close(bgShell.done)

		err := shell.ExecStream(shellCtx, command, bgShell.stdout, bgShell.stderr)

		bgShell.exitErr = err
		bgShell.completedAt.Store(time.Now().UnixNano())

		if bgShell.backgrounded.Load() && !bgShell.cancelled.Load() {
			if fn := m.completionHandler(); fn != nil {
				fn(bgShell)
			}
		}
	}()

	return bgShell, nil
}

// prune drops finished jobs that are past retention or beyond
// MaxFinishedJobs, and reports how many jobs are still running. Callers
// must hold m.mu.
func (m *BackgroundShellManager) prune() (running int) {
	cutoff := time.Now().Add(-CompletedJobRetention).UnixNano()
	var finished []*BackgroundShell
	for id, shell := range m.shells {
		switch at := shell.completedAt.Load(); {
		case at == 0:
			running++
		case at < cutoff:
			delete(m.shells, id)
		default:
			finished = append(finished, shell)
		}
	}

	if over := len(finished) - MaxFinishedJobs; over > 0 {
		slices.SortFunc(finished, func(a, b *BackgroundShell) int {
			return cmp.Compare(a.completedAt.Load(), b.completedAt.Load())
		})
		for _, shell := range finished[:over] {
			delete(m.shells, shell.ID)
		}
	}
	return running
}

// take removes and returns the shell with the given ID.
func (m *BackgroundShellManager) take(id string) (*BackgroundShell, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	shell, ok := m.shells[id]
	if !ok {
		return nil, fmt.Errorf("background shell not found: %s", id)
	}
	delete(m.shells, id)
	return shell, nil
}

// Get retrieves a background shell by ID.
func (m *BackgroundShellManager) Get(id string) (*BackgroundShell, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	shell, ok := m.shells[id]
	return shell, ok
}

// Remove stops tracking a background shell without terminating it.
func (m *BackgroundShellManager) Remove(id string) error {
	_, err := m.take(id)
	return err
}

// Kill terminates a background shell by ID.
//
// The wait is bounded: a command whose process cannot be reaped (an unkillable
// child, a wedged interpreter) must not block the caller — the job_kill tool and
// the agent's own cancel path both go through here, so hanging would freeze the
// turn with no way out but killing Crush. A shell that does not stop in time
// goes back into the manager so a later job_kill can retry it.
func (m *BackgroundShellManager) Kill(id string) error {
	return m.killWithin(id, KillTimeout)
}

// killWithin is Kill with an explicit wait, so the timeout path stays testable
// without sleeping for the real budget.
func (m *BackgroundShellManager) killWithin(id string, wait time.Duration) error {
	shell, err := m.take(id)
	if err != nil {
		return err
	}
	shell.cancelled.Store(true)
	shell.cancel()
	select {
	case <-shell.done:
		return nil
	case <-time.After(wait):
		m.mu.Lock()
		m.shells[id] = shell
		m.mu.Unlock()
		return fmt.Errorf("background shell %s did not stop within %s; it is still running and may need to be stopped by hand", id, wait)
	}
}

// BackgroundShellInfo contains information about a background shell.
type BackgroundShellInfo struct {
	ID          string
	Command     string
	Description string
}

// List returns all background shell IDs.
func (m *BackgroundShellManager) List() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Collect(maps.Keys(m.shells))
}

// KillAll terminates all background shells. The provided context bounds how
// long the function waits for each shell to exit.
func (m *BackgroundShellManager) KillAll(ctx context.Context) {
	m.mu.Lock()
	shells := m.shells
	m.shells = map[string]*BackgroundShell{}
	m.mu.Unlock()

	var wg sync.WaitGroup
	for _, shell := range shells {
		wg.Go(func() {
			shell.cancelled.Store(true)
			shell.cancel()
			select {
			case <-shell.done:
			case <-ctx.Done():
			}
		})
	}
	wg.Wait()
}

// GetOutput returns the current output of a background shell.
func (bs *BackgroundShell) GetOutput() (stdout string, stderr string, done bool, err error) {
	select {
	case <-bs.done:
		return bs.stdout.String(), bs.stderr.String(), true, bs.exitErr
	default:
		return bs.stdout.String(), bs.stderr.String(), false, nil
	}
}

// IsDone checks if the background shell has finished execution.
func (bs *BackgroundShell) IsDone() bool {
	select {
	case <-bs.done:
		return true
	default:
		return false
	}
}

// Done returns a channel that is closed when the background shell exits.
func (bs *BackgroundShell) Done() <-chan struct{} {
	return bs.done
}

// Wait blocks until the background shell completes.
func (bs *BackgroundShell) Wait() {
	<-bs.done
}

func (bs *BackgroundShell) WaitContext(ctx context.Context) bool {
	select {
	case <-bs.done:
		return true
	case <-ctx.Done():
		return false
	}
}

// SetCompletionHandler registers fn to run when a backgrounded job finishes.
// Replacing the handler at runtime is allowed; the manager serializes access.
func (m *BackgroundShellManager) SetCompletionHandler(fn func(*BackgroundShell)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onComplete = fn
}

// completionHandler returns the current completion handler.
func (m *BackgroundShellManager) completionHandler() func(*BackgroundShell) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.onComplete
}

// MarkBackgrounded records that the job was handed to the caller as a
// background job. Only such jobs notify anyone when they finish: a command
// that returned synchronously already delivered its result.
func (bs *BackgroundShell) MarkBackgrounded() {
	bs.backgrounded.Store(true)
}

// WasBackgrounded reports whether the job was handed to the caller as a
// background job.
func (bs *BackgroundShell) WasBackgrounded() bool {
	return bs.backgrounded.Load()
}

// ExitStatus reports the job's exit code, or -1 while it is still running.
func (bs *BackgroundShell) ExitStatus() int {
	if bs.completedAt.Load() == 0 {
		return -1
	}
	return ExitCode(bs.exitErr)
}

// Elapsed reports how long the job has been running, or how long it ran before
// finishing.
func (bs *BackgroundShell) Elapsed() time.Duration {
	end := time.Now()
	if at := bs.completedAt.Load(); at != 0 {
		end = time.Unix(0, at)
	}
	return end.Sub(bs.startedAt)
}

// IdleFor reports how long the job has produced no output. A job that has not
// written anything yet is idle since it started.
//
// This is the only progress signal available for a command that is still
// running: a large idle time next to a completed-looking command is what tells
// "stuck or waiting on input" apart from "busy but quiet". It cannot tell the
// two apart on its own, so callers must report it rather than act on it.
func (bs *BackgroundShell) IdleFor() time.Duration {
	last := bs.startedAt
	if t := bs.stdout.LastWrite(); t.After(last) {
		last = t
	}
	if t := bs.stderr.LastWrite(); t.After(last) {
		last = t
	}
	return time.Since(last)
}

// OutputWritten reports how many bytes stdout and stderr have carried in total.
func (bs *BackgroundShell) OutputWritten() int64 {
	return bs.stdout.Written() + bs.stderr.Written()
}

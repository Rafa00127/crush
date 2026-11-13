package shell

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBackgroundShellManager_Start(t *testing.T) {
	t.Skip("Skipping this until I figure out why its flaky")
	t.Parallel()

	ctx := t.Context()
	workingDir := t.TempDir()
	manager := newBackgroundShellManager()

	bgShell, err := manager.Start(ctx, workingDir, nil, "echo 'hello world'", "", testMaxJobs)
	if err != nil {
		t.Fatalf("failed to start background shell: %v", err)
	}

	if bgShell.ID == "" {
		t.Error("expected shell ID to be non-empty")
	}

	// Wait for the command to complete
	bgShell.Wait()

	stdout, stderr, done, err := bgShell.GetOutput()
	if !done {
		t.Error("expected shell to be done")
	}

	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}

	if !strings.Contains(stdout, "hello world") {
		t.Errorf("expected stdout to contain 'hello world', got: %s", stdout)
	}

	if stderr != "" {
		t.Errorf("expected empty stderr, got: %s", stderr)
	}
}

func TestBackgroundShellManager_Get(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	workingDir := t.TempDir()
	manager := newBackgroundShellManager()

	bgShell, err := manager.Start(ctx, workingDir, nil, "echo 'test'", "", testMaxJobs)
	if err != nil {
		t.Fatalf("failed to start background shell: %v", err)
	}

	// Retrieve the shell
	retrieved, ok := manager.Get(bgShell.ID)
	if !ok {
		t.Error("expected to find the background shell")
	}

	if retrieved.ID != bgShell.ID {
		t.Errorf("expected shell ID %s, got %s", bgShell.ID, retrieved.ID)
	}

	// Clean up
	manager.Kill(bgShell.ID)
}

func TestBackgroundShellManager_Kill(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	workingDir := t.TempDir()
	manager := newBackgroundShellManager()

	// Start a long-running command
	bgShell, err := manager.Start(ctx, workingDir, nil, "sleep 10", "", testMaxJobs)
	if err != nil {
		t.Fatalf("failed to start background shell: %v", err)
	}

	// Kill it
	err = manager.Kill(bgShell.ID)
	if err != nil {
		t.Errorf("failed to kill background shell: %v", err)
	}

	// Verify it's no longer in the manager
	_, ok := manager.Get(bgShell.ID)
	if ok {
		t.Error("expected shell to be removed after kill")
	}

	// Verify the shell is done
	if !bgShell.IsDone() {
		t.Error("expected shell to be done after kill")
	}
}

func TestBackgroundShellManager_KillNonExistent(t *testing.T) {
	t.Parallel()

	manager := newBackgroundShellManager()

	err := manager.Kill("non-existent-id")
	if err == nil {
		t.Error("expected error when killing non-existent shell")
	}
}

func TestBackgroundShell_IsDone(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	workingDir := t.TempDir()
	manager := newBackgroundShellManager()

	bgShell, err := manager.Start(ctx, workingDir, nil, "echo 'quick'", "", testMaxJobs)
	if err != nil {
		t.Fatalf("failed to start background shell: %v", err)
	}

	// Wait for the command to complete (Windows is slower to spin up).
	require.Eventually(t, bgShell.IsDone, 5*time.Second, 50*time.Millisecond, "expected shell to be done")

	// Clean up
	manager.Kill(bgShell.ID)
}

func TestBackgroundShell_WithBlockFuncs(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	workingDir := t.TempDir()
	manager := newBackgroundShellManager()

	blockFuncs := []BlockFunc{
		CommandsBlocker([]string{"curl", "wget"}),
	}

	bgShell, err := manager.Start(ctx, workingDir, blockFuncs, "curl example.com", "", testMaxJobs)
	if err != nil {
		t.Fatalf("failed to start background shell: %v", err)
	}

	// Wait for the command to complete
	bgShell.Wait()

	stdout, stderr, done, execErr := bgShell.GetOutput()
	if !done {
		t.Error("expected shell to be done")
	}

	// The command should have been blocked
	output := stdout + stderr
	if !strings.Contains(output, "is a dangerous command") && execErr == nil {
		t.Errorf("expected command to be blocked, got stdout: %s, stderr: %s, err: %v", stdout, stderr, execErr)
	}

	// Clean up
	manager.Kill(bgShell.ID)
}

func TestBackgroundShellManager_List(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping flacky test on windows")
	}

	t.Parallel()

	ctx := t.Context()
	workingDir := t.TempDir()
	manager := newBackgroundShellManager()

	// Start two shells
	bgShell1, err := manager.Start(ctx, workingDir, nil, "sleep 1", "", testMaxJobs)
	if err != nil {
		t.Fatalf("failed to start first background shell: %v", err)
	}

	bgShell2, err := manager.Start(ctx, workingDir, nil, "sleep 1", "", testMaxJobs)
	if err != nil {
		t.Fatalf("failed to start second background shell: %v", err)
	}

	ids := manager.List()

	// Check that both shells are in the list
	found1 := false
	found2 := false
	for _, id := range ids {
		if id == bgShell1.ID {
			found1 = true
		}
		if id == bgShell2.ID {
			found2 = true
		}
	}

	if !found1 {
		t.Errorf("expected to find shell %s in list", bgShell1.ID)
	}
	if !found2 {
		t.Errorf("expected to find shell %s in list", bgShell2.ID)
	}

	// Clean up
	manager.Kill(bgShell1.ID)
	manager.Kill(bgShell2.ID)
}

func TestBackgroundShellManager_KillAll(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	workingDir := t.TempDir()
	manager := newBackgroundShellManager()

	// Start multiple long-running shells
	shell1, err := manager.Start(ctx, workingDir, nil, "sleep 10", "", testMaxJobs)
	if err != nil {
		t.Fatalf("failed to start shell 1: %v", err)
	}

	shell2, err := manager.Start(ctx, workingDir, nil, "sleep 10", "", testMaxJobs)
	if err != nil {
		t.Fatalf("failed to start shell 2: %v", err)
	}

	shell3, err := manager.Start(ctx, workingDir, nil, "sleep 10", "", testMaxJobs)
	if err != nil {
		t.Fatalf("failed to start shell 3: %v", err)
	}

	// Verify shells are running
	if shell1.IsDone() || shell2.IsDone() || shell3.IsDone() {
		t.Error("shells should not be done yet")
	}

	// Kill all shells
	manager.KillAll(t.Context())

	// Verify all shells are done
	if !shell1.IsDone() {
		t.Error("shell1 should be done after KillAll")
	}
	if !shell2.IsDone() {
		t.Error("shell2 should be done after KillAll")
	}
	if !shell3.IsDone() {
		t.Error("shell3 should be done after KillAll")
	}

	// Verify they're removed from the manager
	if _, ok := manager.Get(shell1.ID); ok {
		t.Error("shell1 should be removed from manager")
	}
	if _, ok := manager.Get(shell2.ID); ok {
		t.Error("shell2 should be removed from manager")
	}
	if _, ok := manager.Get(shell3.ID); ok {
		t.Error("shell3 should be removed from manager")
	}

	// Verify list is empty (or doesn't contain our shells)
	ids := manager.List()
	for _, id := range ids {
		if id == shell1.ID || id == shell2.ID || id == shell3.ID {
			t.Errorf("shell %s should not be in list after KillAll", id)
		}
	}
}

func TestBackgroundShellManager_KillAll_Timeout(t *testing.T) {
	t.Parallel()

	// XXX: can't use synctest here - causes --race to trip.

	workingDir := t.TempDir()
	manager := newBackgroundShellManager()

	// Start a shell that traps signals and ignores cancellation.
	_, err := manager.Start(t.Context(), workingDir, nil, "trap '' TERM INT; sleep 60", "", testMaxJobs)
	require.NoError(t, err)

	// Short timeout to test the timeout path.
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	t.Cleanup(cancel)

	start := time.Now()
	manager.KillAll(ctx)

	elapsed := time.Since(start)

	// Must return promptly after timeout, not hang for 60 seconds.
	require.Less(t, elapsed, 2*time.Second)
}

func TestBackgroundShell_WaitContext_Completed(t *testing.T) {
	t.Parallel()

	done := make(chan struct{})
	close(done)

	bgShell := &BackgroundShell{done: done}

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	t.Cleanup(cancel)

	require.True(t, bgShell.WaitContext(ctx))
}

func TestBackgroundShell_WaitContext_Canceled(t *testing.T) {
	t.Parallel()

	bgShell := &BackgroundShell{done: make(chan struct{})}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	require.False(t, bgShell.WaitContext(ctx))
}

const testMaxJobs = 50

// track registers a fake job with the manager. A zero completedAt means the job
// counts as still running.
func track(m *BackgroundShellManager, id string, completedAt time.Time) {
	bgShell := &BackgroundShell{
		ID:     id,
		done:   make(chan struct{}),
		stdout: newCappedBuffer(maxKeptOutput),
		stderr: newCappedBuffer(maxKeptOutput),
		cancel: func() {},
	}
	if !completedAt.IsZero() {
		bgShell.completedAt.Store(completedAt.UnixNano())
		close(bgShell.done)
	}
	m.shells[id] = bgShell
}

func TestBackgroundShellManager_FinishedJobsDoNotBlockStart(t *testing.T) {
	t.Parallel()

	manager := newBackgroundShellManager()
	for i := range testMaxJobs {
		track(manager, fmt.Sprintf("done-%d", i), time.Now())
	}

	bgShell, err := manager.Start(t.Context(), t.TempDir(), nil, "echo hi", "", testMaxJobs)
	require.NoError(t, err)
	require.NotEmpty(t, bgShell.ID)
	bgShell.Wait()
}

func TestBackgroundShellManager_RunningJobsBlockStart(t *testing.T) {
	t.Parallel()

	manager := newBackgroundShellManager()
	track(manager, "running-0", time.Time{})
	track(manager, "running-1", time.Time{})

	_, err := manager.Start(t.Context(), t.TempDir(), nil, "echo hi", "", 2)
	require.ErrorContains(t, err, "maximum number of running background jobs (2)")

	bgShell, err := manager.Start(t.Context(), t.TempDir(), nil, "echo hi", "", 3)
	require.NoError(t, err, "a higher limit admits the job")
	bgShell.Wait()
}

// Output stays readable for the retention period and no longer.
func TestBackgroundShellManager_DropsStaleFinishedJobs(t *testing.T) {
	t.Parallel()

	manager := newBackgroundShellManager()
	now := time.Now()
	track(manager, "stale", now.Add(-CompletedJobRetention).Add(-time.Second))
	track(manager, "fresh", now)

	bgShell, err := manager.Start(t.Context(), t.TempDir(), nil, "echo hi", "", testMaxJobs)
	require.NoError(t, err)
	bgShell.Wait()

	_, ok := manager.Get("stale")
	require.False(t, ok, "a job past its retention window is dropped")
	_, ok = manager.Get("fresh")
	require.True(t, ok, "a recently finished job stays readable")
}

func TestBackgroundShellManager_PruneDropsOldestFinished(t *testing.T) {
	t.Parallel()

	manager := newBackgroundShellManager()
	now := time.Now()
	for i := range MaxFinishedJobs + 1 {
		track(manager, fmt.Sprintf("done-%d", i), now.Add(time.Duration(i)*time.Second))
	}
	track(manager, "running", time.Time{})

	bgShell, err := manager.Start(t.Context(), t.TempDir(), nil, "sleep 1", "", testMaxJobs)
	require.NoError(t, err)

	require.Len(t, manager.List(), MaxFinishedJobs+2)
	_, ok := manager.Get("done-0")
	require.False(t, ok, "oldest finished job should have been dropped")
	_, ok = manager.Get("done-1")
	require.True(t, ok, "only the excess is dropped")
	_, ok = manager.Get("running")
	require.True(t, ok, "running jobs must never be dropped")

	require.NoError(t, manager.Kill(bgShell.ID))
}

func TestBackgroundShellActivity(t *testing.T) {
	t.Parallel()

	bs := &BackgroundShell{
		stdout:    newCappedBuffer(64),
		stderr:    newCappedBuffer(64),
		done:      make(chan struct{}),
		startedAt: time.Now().Add(-3 * time.Second),
	}

	require.InDelta(t, 3.0, bs.Elapsed().Seconds(), 0.5)
	require.InDelta(t, 3.0, bs.IdleFor().Seconds(), 0.5,
		"a job that has produced nothing is idle since it started")
	require.Equal(t, int64(0), bs.OutputWritten())

	bs.stdout.Write([]byte("abc"))
	require.InDelta(t, 0, bs.IdleFor().Seconds(), 0.5, "writing resets the idle clock")
	require.Equal(t, int64(3), bs.OutputWritten())

	bs.completedAt.Store(time.Now().Add(-time.Second).UnixNano())
	require.InDelta(t, 2.0, bs.Elapsed().Seconds(), 0.5,
		"a finished job reports how long it ran, not how long ago it started")
}

// A shell that will not stop must not block the caller: job_kill and the
// agent's own cancel path both wait here, so a hang would freeze the turn.
func TestBackgroundShellManager_KillTimeout(t *testing.T) {
	t.Parallel()

	manager := newBackgroundShellManager()
	track(manager, "wedged", time.Time{})

	start := time.Now()
	err := manager.killWithin("wedged", 50*time.Millisecond)
	require.ErrorContains(t, err, "did not stop within")
	require.Less(t, time.Since(start), time.Second, "Kill must give up on its own")

	_, ok := manager.Get("wedged")
	require.True(t, ok, "a shell that outlived the kill stays reachable for a retry")
}

// Only jobs that were handed to the caller as background jobs notify anyone:
// a command that returned its output synchronously already delivered its
// result, and a notification for it would be noise.
func TestBackgroundShellManager_NotifiesOnlyBackgroundedJobs(t *testing.T) {
	t.Parallel()

	manager := newBackgroundShellManager()
	var finished []string
	var mu sync.Mutex
	manager.SetCompletionHandler(func(job *BackgroundShell) {
		mu.Lock()
		finished = append(finished, job.ID)
		mu.Unlock()
	})

	// Synchronous: finishes before the caller hands it off.
	syncJob, err := manager.Start(t.Context(), t.TempDir(), nil, "echo sync", "", testMaxJobs)
	require.NoError(t, err)
	syncJob.Wait()

	// Backgrounded: marked before it finishes.
	bgJob, err := manager.Start(t.Context(), t.TempDir(), nil, "echo bg", "", testMaxJobs)
	require.NoError(t, err)
	bgJob.MarkBackgrounded()
	bgJob.Wait()

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(finished) == 1
	}, 3*time.Second, 10*time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, []string{bgJob.ID}, finished)
	require.True(t, bgJob.WasBackgrounded())
	require.False(t, syncJob.WasBackgrounded())
	require.Equal(t, 0, bgJob.ExitStatus())
}

// Killing a job is not news: the caller asked for it, and its output is gone,
// so no completion notification may follow.
func TestBackgroundShellManager_KilledJobDoesNotNotify(t *testing.T) {
	t.Parallel()

	manager := newBackgroundShellManager()
	var mu sync.Mutex
	notified := 0
	manager.SetCompletionHandler(func(*BackgroundShell) {
		mu.Lock()
		notified++
		mu.Unlock()
	})

	job, err := manager.Start(t.Context(), t.TempDir(), nil, "sleep 20", "", testMaxJobs)
	require.NoError(t, err)
	job.MarkBackgrounded()

	require.NoError(t, manager.Kill(job.ID))

	mu.Lock()
	defer mu.Unlock()
	require.Zero(t, notified, "a killed job must not announce that it finished")
}

package tools

import (
	"context"
	"fmt"
	"testing"
	"time"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/shell"
	"github.com/stretchr/testify/require"
)

func TestJobStatusText(t *testing.T) {
	t.Parallel()

	t.Run("running reports idle and captured bytes", func(t *testing.T) {
		t.Parallel()
		require.Equal(t,
			"running (elapsed 3m2s, last output 58s ago, 1.2 KB captured)",
			jobStatusText(3*time.Minute+2*time.Second, 58*time.Second, 1200, false),
		)
	})

	t.Run("done drops the idle figure", func(t *testing.T) {
		t.Parallel()
		require.Equal(t,
			"completed (elapsed 2s, 44 B captured)",
			jobStatusText(2*time.Second, time.Minute, 44, true),
		)
	})

	t.Run("sub-second durations collapse to zero", func(t *testing.T) {
		t.Parallel()
		require.Equal(t,
			"running (elapsed 0s, last output 0s ago, 0 B captured)",
			jobStatusText(200*time.Millisecond, 10*time.Millisecond, 0, false),
		)
	})
}

func TestRedirectTarget(t *testing.T) {
	t.Parallel()

	cases := []struct {
		command string
		want    string
	}{
		{`python -u run.py > out/run.log 2>&1`, "out/run.log"},
		{`python -u run.py >> out/run.log`, "out/run.log"},
		{`build.sh &> build.log`, "build.log"},
		{`make 2> err.log`, "err.log"},
		{`python -u run.py > "out/my log.txt" 2>&1`, "out/my log.txt"},
		{`python -u run.py 2>&1 | tail -5`, ""},
		{`python -u run.py`, ""},
		{`grep "a > b" file.txt`, ""},
		// Only the last statement's redirects matter: the earlier ones are
		// setup, and the command that keeps running writes to its own
		// streams.
		{"cat > script.py <<'EOF'\nprint(1)\nEOF\npython -u script.py", ""},
		{`cd /tmp && python -u run.py > out.log`, "out.log"},
		{`a > x; b > y`, "y"},
	}
	for _, tc := range cases {
		require.Equal(t, tc.want, redirectTarget(tc.command), tc.command)
	}
}

func TestQuietJobNote(t *testing.T) {
	t.Parallel()

	command := `python -u run.py > out/run.log 2>&1`
	require.Contains(t, redirectNote(command, 0, false), "out/run.log",
		"a silent running job that redirects its output names the file")
	require.Contains(t, redirectNote(command, 0, true), "wrote its output to", "a finished job points at the file holding the result")
	require.Empty(t, redirectNote(command, 12, false), "a job with output needs no note")
	require.Empty(t, redirectNote("python -u run.py", 0, false), "no redirect, no note")
}

func TestJobOutputListsJobs(t *testing.T) {
	t.Parallel()

	manager := shell.GetBackgroundShellManager()
	bgShell, err := manager.Start(t.Context(), t.TempDir(), nil, "echo listed-marker", "", testMaxJobs)
	require.NoError(t, err)
	bgShell.Wait()

	tool := NewJobOutputTool(t.TempDir(), 15*time.Second)
	resp, err := tool.Run(context.Background(), fantasy.ToolCall{ID: "call_1", Name: JobOutputToolName, Input: `{}`})
	require.NoError(t, err)
	require.Contains(t, resp.Content, "background job(s)")
	require.Contains(t, resp.Content, bgShell.ID)
	require.Contains(t, resp.Content, "completed")
}

// A silent job whose command redirects its output must say where that output
// went: "no output" alone is indistinguishable from a stuck command.
func TestJobOutputNamesRedirectTarget(t *testing.T) {
	t.Parallel()

	manager := shell.GetBackgroundShellManager()
	bgShell, err := manager.Start(t.Context(), t.TempDir(), nil, "sleep 3 > out.log 2>&1", "", testMaxJobs)
	require.NoError(t, err)
	t.Cleanup(func() { _ = manager.Kill(bgShell.ID) })

	tool := NewJobOutputTool(t.TempDir(), 15*time.Second)
	resp, err := tool.Run(context.Background(), fantasy.ToolCall{
		ID:    "call_2",
		Name:  JobOutputToolName,
		Input: fmt.Sprintf(`{"shell_id":%q}`, bgShell.ID),
	})
	require.NoError(t, err)
	require.Contains(t, resp.Content, "Status: running")
	require.Contains(t, resp.Content, "redirects its output to \"out.log\"")
}

// job_output never blocks, whatever is asking: a background job must not be
// able to hold the turn open.
func TestJobOutputDoesNotBlockOnRunningJob(t *testing.T) {
	t.Parallel()

	manager := shell.GetBackgroundShellManager()
	bgShell, err := manager.Start(t.Context(), t.TempDir(), nil, "sleep 20", "", testMaxJobs)
	require.NoError(t, err)
	t.Cleanup(func() { _ = manager.Kill(bgShell.ID) })

	tool := NewJobOutputTool(t.TempDir(), 15*time.Second)
	start := time.Now()
	resp, err := tool.Run(context.Background(), fantasy.ToolCall{
		ID:    "call_3",
		Name:  JobOutputToolName,
		Input: fmt.Sprintf(`{"shell_id":%q}`, bgShell.ID),
	})
	require.NoError(t, err)
	require.Less(t, time.Since(start), 5*time.Second, "reading a running job must return at once")
	require.Contains(t, resp.Content, "Status: running")
}

// wait=true blocks for a short job and returns its output.
func TestJobOutputWaitReturnsOnCompletion(t *testing.T) {
	t.Parallel()

	manager := shell.GetBackgroundShellManager()
	bgShell, err := manager.Start(t.Context(), t.TempDir(), nil, "echo waited-output", "", testMaxJobs)
	require.NoError(t, err)

	tool := NewJobOutputTool(t.TempDir(), 5*time.Second)
	resp, err := tool.Run(context.Background(), fantasy.ToolCall{
		ID:    "call_wait",
		Name:  JobOutputToolName,
		Input: fmt.Sprintf(`{"shell_id":%q,"wait":true}`, bgShell.ID),
	})
	require.NoError(t, err)
	require.Contains(t, resp.Content, "Status: completed")
	require.Contains(t, resp.Content, "waited-output")
	require.NotContains(t, resp.Content, "returned early")
}

// A wait that outlives its limit returns the output so far and says so,
// instead of holding the turn open the way an unbounded wait did.
func TestJobOutputWaitIsBounded(t *testing.T) {
	t.Parallel()

	manager := shell.GetBackgroundShellManager()
	bgShell, err := manager.Start(t.Context(), t.TempDir(), nil, "sleep 30", "", testMaxJobs)
	require.NoError(t, err)
	t.Cleanup(func() { _ = manager.Kill(bgShell.ID) })

	tool := NewJobOutputTool(t.TempDir(), 300*time.Millisecond)
	start := time.Now()
	resp, err := tool.Run(context.Background(), fantasy.ToolCall{
		ID:    "call_bounded",
		Name:  JobOutputToolName,
		Input: fmt.Sprintf(`{"shell_id":%q,"wait":true}`, bgShell.ID),
	})
	require.NoError(t, err)
	require.Less(t, time.Since(start), 5*time.Second, "the wait must give up on its own")
	require.Contains(t, resp.Content, "Status: running")
	require.Contains(t, resp.Content, "returned early")
}

// A zero limit disables waiting entirely.
func TestJobOutputWaitDisabledReturnsAtOnce(t *testing.T) {
	t.Parallel()

	manager := shell.GetBackgroundShellManager()
	bgShell, err := manager.Start(t.Context(), t.TempDir(), nil, "sleep 30", "", testMaxJobs)
	require.NoError(t, err)
	t.Cleanup(func() { _ = manager.Kill(bgShell.ID) })

	tool := NewJobOutputTool(t.TempDir(), 0)
	start := time.Now()
	resp, err := tool.Run(context.Background(), fantasy.ToolCall{
		ID:    "call_disabled",
		Name:  JobOutputToolName,
		Input: fmt.Sprintf(`{"shell_id":%q,"wait":true}`, bgShell.ID),
	})
	require.NoError(t, err)
	require.Less(t, time.Since(start), 2*time.Second)
	require.Contains(t, resp.Content, "Status: running")
	require.Contains(t, resp.Content, "Waiting is disabled by configuration",
		"the caller must be told why its wait did nothing")
}

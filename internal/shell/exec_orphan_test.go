package shell

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const (
	// orphanHelperEnv selects the role of the helper process started by the
	// test below.
	orphanHelperEnv = "CRUSH_SHELL_ORPHAN_HELPER"
	// orphanSleeperEnv marks the helper copy that has to outlive its parent.
	orphanSleeperEnv = "CRUSH_SHELL_ORPHAN_SLEEPER"
	// orphanReleaseEnv is the file the orphaned copy waits on, so the test
	// can let it go once its assertions are done.
	orphanReleaseEnv = "CRUSH_SHELL_ORPHAN_RELEASE"
	// orphanSleeperLifetime bounds how long the orphaned copy holds the
	// output pipe. Only the exec handler's WaitDelay can release the command
	// sooner.
	orphanSleeperLifetime = 10 * time.Second
	// orphanSleeperPoll is how often the orphaned copy and the test look for
	// each other's files.
	orphanSleeperPoll = 25 * time.Millisecond
)

// TestExecOrphanHelper is not a test of its own: it is the process that
// TestExecHandlerWaitDelay runs through the interpreter. It skips in ordinary
// test runs.
func TestExecOrphanHelper(t *testing.T) {
	switch {
	case os.Getenv(orphanSleeperEnv) != "":
		holdOrphanPipe()
		os.Exit(0)
	case os.Getenv(orphanHelperEnv) != "spawner":
		t.Skip("helper process for TestExecHandlerWaitDelay")
	}

	// A copy that inherits this process's stdout and stderr keeps the
	// command's output pipe open after this process is gone, which is the
	// orphaned-child case from https://go.dev/issue/23019.
	orphan := exec.Command(os.Args[0], "-test.run=TestExecOrphanHelper")
	orphan.Env = append(os.Environ(), orphanSleeperEnv+"=1")
	orphan.Stdout = os.Stdout
	orphan.Stderr = os.Stderr
	require.NoError(t, orphan.Start())

	fmt.Fprintln(os.Stderr, "boom")
	os.Exit(3)
}

// holdOrphanPipe keeps the inherited output pipe open until the test releases
// it, so the process never outlives the run and leaves neither a locked test
// binary nor a locked temporary directory behind.
func holdOrphanPipe() {
	release := os.Getenv(orphanReleaseEnv)
	deadline := time.Now().Add(orphanSleeperLifetime)
	for release != "" && time.Now().Before(deadline) {
		if _, err := os.Stat(release); err == nil {
			break
		}
		time.Sleep(orphanSleeperPoll)
	}
	_ = os.WriteFile(release+".done", nil, 0o600)
}

// TestExecHandlerWaitDelay checks that a command which exits while a child
// still holds its output pipe is reported as finished, with its own exit
// status, shortly after exiting. Without a WaitDelay the command looks like it
// is still running until that child exits — the reason a failed bash command
// could sit in the background as "running" forever.
func TestExecHandlerWaitDelay(t *testing.T) {
	t.Parallel()

	sh := NewShell(&Options{WorkingDir: testWorkingDir(t), Env: orphanEnv(t)})

	var out strings.Builder
	start := time.Now()
	err := sh.ExecStream(t.Context(), orphanHelperCommandLine(), &out, &out)
	elapsed := time.Since(start)

	require.Equal(t, 3, ExitCode(err), "exit status of the command")
	require.Contains(t, out.String(), "boom", "output written before the command exited")
	require.Less(t, elapsed, orphanSleeperLifetime/2,
		"a finished command must not wait for an orphaned child holding its output pipe")
}

// TestDispatchShebangWaitDelay covers the same case for a path-prefixed script
// with a shebang line, which is executed by the dispatch middleware rather than
// by the exec handlers.
func TestDispatchShebangWaitDelay(t *testing.T) {
	t.Parallel()

	exe := os.Args[0]
	if strings.ContainsAny(exe, " \t") {
		t.Skip("a shebang line cannot carry an interpreter path with spaces")
	}

	// The body is never parsed: the shebang branch execs the interpreter
	// directly, so the file only has to start with a valid shebang.
	script := filepath.Join(t.TempDir(), "orphan-script")
	shebang := fmt.Sprintf("#!%s -test.run=TestExecOrphanHelper\n", exe)
	require.NoError(t, os.WriteFile(script, []byte(shebang), 0o755))

	sh := NewShell(&Options{WorkingDir: testWorkingDir(t), Env: orphanEnv(t)})

	var out strings.Builder
	start := time.Now()
	err := sh.ExecStream(t.Context(), fmt.Sprintf("'%s'", script), &out, &out)
	elapsed := time.Since(start)

	require.Equal(t, 3, ExitCode(err), "exit status of the command")
	require.Less(t, elapsed, orphanSleeperLifetime/2,
		"a finished script must not wait for an orphaned child holding its output pipe")
}

// orphanEnv returns an environment that makes the helper process spawn a
// pipe-holding child, plus the cleanup that releases that child once the test is
// over.
func orphanEnv(t *testing.T) []string {
	t.Helper()
	release := filepath.Join(t.TempDir(), "release")
	t.Cleanup(func() {
		// Registered after TempDir, so it runs before the directory is
		// removed: let the orphan go and wait for it to be gone.
		_ = os.WriteFile(release, nil, 0o600)
		waitForPath(release+".done", 3*time.Second)
		time.Sleep(2 * orphanSleeperPoll)
	})
	return append(os.Environ(),
		orphanHelperEnv+"=spawner",
		orphanReleaseEnv+"="+release,
	)
}

// testWorkingDir is not t.TempDir(): the orphaned helper inherits it, and
// Windows cannot remove a directory that is still a live process's working
// directory.
func testWorkingDir(t *testing.T) string {
	t.Helper()
	workingDir, err := os.Getwd()
	require.NoError(t, err)
	return workingDir
}

// orphanHelperCommandLine quotes the test binary itself so the interpreter can
// run it again, keeping any spaces and backslashes in the path intact.
func orphanHelperCommandLine() string {
	exe := strings.ReplaceAll(os.Args[0], "'", `'\''`)
	return fmt.Sprintf("'%s' -test.run=TestExecOrphanHelper", exe)
}

// waitForPath blocks until path exists or timeout elapses.
func waitForPath(path string, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(orphanSleeperPoll)
	}
}

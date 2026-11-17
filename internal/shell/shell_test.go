package shell

import (
	"context"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Benchmark to measure CPU efficiency
func BenchmarkShellQuickCommands(b *testing.B) {
	shell := NewShell(&Options{WorkingDir: b.TempDir()})

	b.ReportAllocs()

	for b.Loop() {
		_, _, err := shell.Exec(b.Context(), "echo test")
		exitCode := ExitCode(err)
		if err != nil || exitCode != 0 {
			b.Fatalf("Command failed: %v, exit code: %d", err, exitCode)
		}
	}
}

func TestTestTimeout(t *testing.T) {
	// XXX(@andreynering): This fails on Windows. Address once possible.
	if runtime.GOOS == "windows" {
		t.Skip("Skipping test on Windows")
	}

	ctx, cancel := context.WithTimeout(t.Context(), time.Millisecond)
	t.Cleanup(cancel)

	shell := NewShell(&Options{WorkingDir: t.TempDir()})
	_, _, err := shell.Exec(ctx, "sleep 10")
	if status := ExitCode(err); status == 0 {
		t.Fatalf("Expected non-zero exit status, got %d", status)
	}
	if !IsInterrupt(err) {
		t.Fatalf("Expected command to be interrupted, but it was not")
	}
	if err == nil {
		t.Fatalf("Expected an error due to timeout, but got none")
	}
}

func TestTestCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel() // immediately cancel the context

	shell := NewShell(&Options{WorkingDir: t.TempDir()})
	_, _, err := shell.Exec(ctx, "sleep 10")
	if status := ExitCode(err); status == 0 {
		t.Fatalf("Expected non-zero exit status, got %d", status)
	}
	if !IsInterrupt(err) {
		t.Fatalf("Expected command to be interrupted, but it was not")
	}
	if err == nil {
		t.Fatalf("Expected an error due to cancel, but got none")
	}
}

func TestRunCommandError(t *testing.T) {
	shell := NewShell(&Options{WorkingDir: t.TempDir()})
	_, _, err := shell.Exec(t.Context(), "nopenopenope")
	if status := ExitCode(err); status == 0 {
		t.Fatalf("Expected non-zero exit status, got %d", status)
	}
	if IsInterrupt(err) {
		t.Fatalf("Expected command to not be interrupted, but it was")
	}
	if err == nil {
		t.Fatalf("Expected an error, got nil")
	}
}

func TestRunContinuity(t *testing.T) {
	tempDir1 := t.TempDir()
	tempDir2 := t.TempDir()

	shell := NewShell(&Options{WorkingDir: tempDir1})
	if _, _, err := shell.Exec(t.Context(), "export FOO=bar"); err != nil {
		t.Fatalf("failed to set env: %v", err)
	}
	if _, _, err := shell.Exec(t.Context(), "cd "+filepath.ToSlash(tempDir2)); err != nil {
		t.Fatalf("failed to change directory: %v", err)
	}
	out, _, err := shell.Exec(t.Context(), "echo $FOO ; pwd")
	if err != nil {
		t.Fatalf("failed to echo: %v", err)
	}
	expect := "bar\n" + tempDir2 + "\n"
	if out != expect {
		t.Fatalf("expected output %q, got %q", expect, out)
	}
}

func TestCrossPlatformExecution(t *testing.T) {
	shell := NewShell(&Options{WorkingDir: "."})
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	// Test a simple command that should work on all platforms
	stdout, stderr, err := shell.Exec(ctx, "echo hello")
	if err != nil {
		t.Fatalf("Echo command failed: %v, stderr: %s", err, stderr)
	}

	if stdout == "" {
		t.Error("Echo command produced no output")
	}

	// The output should contain "hello" regardless of platform
	if !strings.Contains(strings.ToLower(stdout), "hello") {
		t.Errorf("Echo output should contain 'hello', got: %q", stdout)
	}
}

// A configured bash binary replaces the interpreter: the command runs with
// that shell, at the shell's working directory, and its exit status comes
// back as one callers can read.
func TestShellRunsCommandsThroughConfiguredBash(t *testing.T) {
	t.Parallel()

	bash := findBashForTest(t)
	sh := NewShell(&Options{WorkingDir: t.TempDir(), BashPath: bash})

	var out strings.Builder
	err := sh.ExecStream(t.Context(), `echo "bash=$BASH_VERSION"; pwd; exit 3`, &out, &out)
	require.Equal(t, 3, ExitCode(err), "a bash command's exit status must survive")

	text := out.String()
	require.Contains(t, text, "bash=", "the configured bash ran the command: %s", text)
	require.NotContains(t, text, "syntax error")
}

// findBashForTest returns a real bash found on PATH, skipping the test when
// there is none. A wrapper that only shadows the name (on Windows, the WSL
// launcher) is not good enough, so the version variable must be set.
func findBashForTest(t *testing.T) string {
	t.Helper()
	for _, candidate := range []string{"bash", "/bin/bash", `C:\msys64\usr\bin\bash.exe`} {
		path, err := exec.LookPath(candidate)
		if err != nil {
			continue
		}
		out, err := exec.Command(path, "-c", "echo $BASH_VERSION").Output()
		if err == nil && strings.TrimSpace(string(out)) != "" {
			return path
		}
	}
	t.Skip("no real bash on this machine")
	return ""
}

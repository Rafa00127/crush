//go:build windows

package shell

import (
	"context"
	"fmt"
	"os/exec"
	"time"

	"mvdan.cc/sh/v3/interp"
)

// defaultKillTimeout matches mvdan's DefaultExecHandler default.
const defaultKillTimeout = 2 * time.Second

// isolateProcess is a no-op on Windows. Session isolation via Setsid is a
// Unix-only concept; Windows uses CREATE_NEW_PROCESS_GROUP which mvdan's
// default handler already handles adequately.
func isolateProcess(_ *exec.Cmd) {}

// processGroupExecHandler returns Crush's exec handler for Windows: a mirror
// of interp.DefaultExecHandler that also sets cmd.WaitDelay.
//
// Upstream sets WaitDelay only off Windows (mvdan.cc/sh v3.14.1,
// interp/handler.go), because there the field shares an if-branch with
// cmd.Cancel = os.Interrupt, which Windows cannot deliver. Without a delay,
// os/exec's Wait blocks on the stdout/stderr copying goroutines until every
// writer closes the pipe, so a child that outlives the command while holding
// the inherited handle keeps the command waiting forever — long after it
// printed its error and exited. That is what made long bash tool calls report
// "still running" for a command that had already failed. With WaitDelay the
// pipes are closed killTimeout after the process exits and the real exit
// status comes back (see https://go.dev/issue/23019).
func processGroupExecHandler(killTimeout time.Duration) interp.ExecHandlerFunc {
	return func(ctx context.Context, args []string) error {
		hc := interp.HandlerCtx(ctx)
		path, err := interp.LookPathDir(hc.Dir, hc.Env, args[0])
		if err != nil {
			fmt.Fprintln(hc.Stderr, err)
			return interp.ExitStatus(127)
		}

		// CommandContext, not a struct literal: cancelling ctx must kill
		// the process, which is what Windows gets instead of the Unix
		// signal escalation.
		cmd := exec.CommandContext(ctx, path)
		cmd.Args = args
		cmd.Env = execEnvList(hc.Env)
		cmd.Dir = hc.Dir
		cmd.Stdin = hc.Stdin
		cmd.Stdout = hc.Stdout
		cmd.Stderr = hc.Stderr
		cmd.WaitDelay = killTimeout

		err = cmd.Start()
		if err == nil {
			err = cmd.Wait()
		}

		return exitStatusFromError(ctx, hc.Stderr, err)
	}
}

package shell

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"syscall"

	"mvdan.cc/sh/v3/interp"
)

// exitStatusFromError translates an exec error into an interp exit status,
// matching the conventions of interp.DefaultExecHandler. Shared by the
// platform-specific exec handlers, which differ only in how they isolate and
// bound the child process.
func exitStatusFromError(ctx context.Context, stderr io.Writer, err error) error {
	if err == nil {
		return nil
	}
	switch err := err.(type) {
	case *exec.ExitError:
		if status, ok := err.Sys().(syscall.WaitStatus); ok && status.Signaled() {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return interp.ExitStatus(128 + uint8(status.Signal()))
		}
		return interp.ExitStatus(uint8(err.ExitCode()))
	case *exec.Error:
		fmt.Fprintf(stderr, "%v\n", err)
		return interp.ExitStatus(127)
	default:
		// ErrWaitDelay means the command itself succeeded and only an
		// orphaned child was still holding the output pipe, which the
		// handler closed after its timeout. That is not a failure.
		if errors.Is(err, exec.ErrWaitDelay) {
			return nil
		}
		return err
	}
}

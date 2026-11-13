package tools

import (
	"cmp"
	"context"
	_ "embed"
	"fmt"
	"slices"
	"strings"
	"time"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/shell"
)

const (
	JobOutputToolName = "job_output"
)

//go:embed job_output.md
var jobOutputDescription string

type JobOutputParams struct {
	ShellID string `json:"shell_id,omitempty" description:"The ID of the background shell to retrieve output from. Omit it to list every job and its state"`
}

type JobOutputResponseMetadata struct {
	ShellID          string `json:"shell_id"`
	Command          string `json:"command"`
	Description      string `json:"description"`
	Done             bool   `json:"done"`
	WorkingDirectory string `json:"working_directory"`
	// ElapsedSeconds is how long the job has been running (or ran).
	ElapsedSeconds float64 `json:"elapsed_seconds"`
	// IdleSeconds is how long the running job has produced no output. It is
	// zero once the job is done, where it carries no meaning.
	IdleSeconds float64 `json:"idle_seconds"`
	// OutputBytes is how many bytes stdout and stderr have carried in total.
	OutputBytes int64 `json:"output_bytes"`
}

// jobStatusText describes a job in one line: whether it is still running, how
// long it has been going, how long since it last produced output, and how much
// output it produced. The idle figure is what separates a slow-but-working
// command from a stuck one, and it is the only progress signal a running job
// has.
func jobStatusText(elapsed, idle time.Duration, written int64, done bool) string {
	if done {
		return fmt.Sprintf("completed (elapsed %s, %s captured)",
			formatJobDuration(elapsed), formatJobBytes(written))
	}
	return fmt.Sprintf("running (elapsed %s, last output %s ago, %s captured)",
		formatJobDuration(elapsed), formatJobDuration(idle), formatJobBytes(written))
}

// formatJobDuration renders a job duration at second resolution: sub-second
// detail is noise for a command that has been running for minutes.
func formatJobDuration(d time.Duration) string {
	if d < time.Second {
		return "0s"
	}
	return d.Round(time.Second).String()
}

// formatJobBytes renders a captured-byte count in the largest unit that keeps
// it readable.
func formatJobBytes(n int64) string {
	const unit = 1024
	switch {
	case n < unit:
		return fmt.Sprintf("%d B", n)
	case n < unit*unit:
		return fmt.Sprintf("%.1f KB", float64(n)/unit)
	default:
		return fmt.Sprintf("%.1f MB", float64(n)/(unit*unit))
	}
}

// listJobs renders every tracked job, running ones first, so a caller can see
// what still holds resources instead of probing IDs one at a time.
func listJobs(bgManager *shell.BackgroundShellManager) string {
	ids := bgManager.List()

	type jobRow struct {
		id      string
		line    string
		command string
		running bool
		elapsed time.Duration
	}
	rows := make([]jobRow, 0, len(ids))
	running := 0
	for _, id := range ids {
		bgShell, ok := bgManager.Get(id)
		if !ok {
			continue
		}
		done := bgShell.IsDone()
		if !done {
			running++
		}
		rows = append(rows, jobRow{
			id:      id,
			line:    jobStatusText(bgShell.Elapsed(), bgShell.IdleFor(), bgShell.OutputWritten(), done),
			command: oneLine(bgShell.Command, 100),
			running: !done,
			elapsed: bgShell.Elapsed(),
		})
	}
	if len(rows) == 0 {
		return "No background jobs."
	}

	slices.SortFunc(rows, func(a, b jobRow) int {
		if a.running != b.running {
			if a.running {
				return -1
			}
			return 1
		}
		return cmp.Compare(b.elapsed, a.elapsed)
	})

	var sb strings.Builder
	fmt.Fprintf(&sb, "%d background job(s), %d still running:\n", len(rows), running)
	for _, r := range rows {
		fmt.Fprintf(&sb, "  %s  %s  %s\n", r.id, r.line, r.command)
	}
	return strings.TrimRight(sb.String(), "\n")
}

// oneLine flattens a command onto a single line and bounds its width, so a
// listing stays readable however the command was formatted.
func oneLine(command string, limit int) string {
	flat := strings.Join(strings.Fields(command), " ")
	if len(flat) <= limit {
		return flat
	}
	return flat[:limit] + "..."
}

func NewJobOutputTool(spillDir string) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		JobOutputToolName,
		jobOutputDescription,
		func(ctx context.Context, params JobOutputParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			bgManager := shell.GetBackgroundShellManager()
			if params.ShellID == "" {
				return fantasy.NewTextResponse(listJobs(bgManager)), nil
			}

			bgShell, ok := bgManager.Get(params.ShellID)
			if !ok {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("background shell not found: %s", params.ShellID)), nil
			}

			stdout, stderr, done, err := bgShell.GetOutput()
			stdout = shell.DecodeTerminalTextLines(stdout)
			stderr = shell.DecodeTerminalTextLines(stderr)

			var outputParts []string
			if stdout != "" {
				outputParts = append(outputParts, stdout)
			}
			if stderr != "" {
				outputParts = append(outputParts, stderr)
			}

			if done {
				if err != nil {
					exitCode := shell.ExitCode(err)
					if exitCode != 0 {
						outputParts = append(outputParts, fmt.Sprintf("Exit code %d", exitCode))
					}
				}
			}

			output := strings.Join(outputParts, "\n")
			output = TruncateOutput(output, spillDir)

			idle := bgShell.IdleFor()
			metadata := JobOutputResponseMetadata{
				ShellID:          params.ShellID,
				Command:          bgShell.Command,
				Description:      bgShell.Description,
				Done:             done,
				WorkingDirectory: bgShell.WorkingDir,
				ElapsedSeconds:   bgShell.Elapsed().Seconds(),
				OutputBytes:      bgShell.OutputWritten(),
			}
			if !done {
				metadata.IdleSeconds = idle.Seconds()
			}

			if output == "" {
				output = BashNoOutput
			}

			status := jobStatusText(bgShell.Elapsed(), idle, metadata.OutputBytes, done)
			result := fmt.Sprintf("Status: %s\n\n%s", status, output)
			if note := redirectNote(bgShell.Command, metadata.OutputBytes, done); note != "" {
				result += "\n\n" + note
			}
			return fantasy.WithResponseMetadata(fantasy.NewTextResponse(result), metadata), nil
		},
	)
}

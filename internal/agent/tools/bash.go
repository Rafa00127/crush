package tools

import (
	"bytes"
	"cmp"
	"context"
	_ "embed"
	"fmt"
	"html/template"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/permission"
	"github.com/charmbracelet/crush/internal/shell"
	"mvdan.cc/sh/v3/syntax"
)

type BashParams struct {
	Description         string `json:"description" description:"A brief description of what the command does, try to keep it under 30 characters or so"`
	Command             string `json:"command" description:"The command to execute"`
	WorkingDir          string `json:"working_dir,omitempty" description:"The working directory to execute the command in (defaults to current directory)"`
	RunInBackground     bool   `json:"run_in_background,omitempty" description:"Set to true (boolean) to run this command in the background. Its output will be delivered to you when it finishes; you do not need to fetch it."`
	AutoBackgroundAfter int    `json:"auto_background_after,omitempty" description:"Seconds to wait before moving a still-running command to the background and freeing the turn. Defaults to the configured auto_background_after"`
}

type BashPermissionsParams struct {
	Description         string `json:"description"`
	Command             string `json:"command"`
	WorkingDir          string `json:"working_dir"`
	RunInBackground     bool   `json:"run_in_background"`
	AutoBackgroundAfter int    `json:"auto_background_after"`
}

type BashResponseMetadata struct {
	StartTime        int64  `json:"start_time"`
	EndTime          int64  `json:"end_time"`
	Output           string `json:"output"`
	Description      string `json:"description"`
	WorkingDirectory string `json:"working_directory"`
	Background       bool   `json:"background,omitempty"`
	ShellID          string `json:"shell_id,omitempty"`
}

const (
	BashToolName = "bash"

	// DefaultAutoBackgroundAfter is the wait used when the caller passes no
	// configured value; config.Options.GetAutoBackgroundAfter normally
	// supplies the real number.
	DefaultAutoBackgroundAfter = config.DefaultAutoBackgroundAfter
	// RedirectedOutputGrace is how long a command that writes its output to
	// a file is watched before it is moved to background. Such a command
	// never produces anything on the captured streams, so waiting the full
	// threshold buys no information; the grace only exists to catch an
	// immediate failure, and the file is what the caller reads afterwards.
	RedirectedOutputGrace = 2 * time.Second
	MaxOutputLength       = 30000
	BashNoOutput          = "no output"
)

//go:embed bash.md.tpl
var bashDescriptionTmpl []byte

var bashDescriptionTpl = template.Must(
	template.New("bashDescription").
		Parse(string(bashDescriptionTmpl)),
)

type bashDescriptionData struct {
	BannedCommands  string
	MaxOutputLength int
	Attribution     config.Attribution
	ModelID         string
	RgAvailable     bool
	GhAvailable     bool
}

// defaultBlockedCommands is the built-in dangerous-command list. It is a
// default, not a hardcoded policy: the `permissions` config block can add
// to it and take away from it (see resolveBlockedCommands).
var defaultBlockedCommands = []string{
	// Network/Download tools
	"alias",
	"aria2c",
	"axel",
	"chrome",
	"curl",
	"curlie",
	"firefox",
	"http-prompt",
	"httpie",
	"links",
	"lynx",
	"nc",
	"safari",
	"scp",
	"ssh",
	"telnet",
	"w3m",
	"wget",
	"xh",

	// System administration
	"doas",
	"su",
	"sudo",

	// Indirect execution: these run a command name the static check can't
	// see, so whatever they wrap would never be flagged on its own.
	"env",
	"eval",
	"nohup",
	"xargs",

	// Package managers
	"apk",
	"apt",
	"apt-cache",
	"apt-get",
	"dnf",
	"dpkg",
	"emerge",
	"home-manager",
	"makepkg",
	"opkg",
	"pacman",
	"paru",
	"pkg",
	"pkg_add",
	"pkg_delete",
	"portage",
	"rpm",
	"yay",
	"yum",
	"zypper",

	// System modification
	"at",
	"batch",
	"chkconfig",
	"crontab",
	"fdisk",
	"mkfs",
	"mount",
	"parted",
	"service",
	"systemctl",
	"umount",

	// Network configuration
	"firewall-cmd",
	"ifconfig",
	"ip",
	"iptables",
	"netstat",
	"pfctl",
	"route",
	"ufw",
}

// resolveBlockedCommands returns the effective dangerous-command list:
// the built-in defaults plus perms.BlockedCommands, minus
// perms.AllowedCommands.
//
// The built-in order is preserved and user additions are appended. The list is
// embedded verbatim in the tool description, so it has to be deterministic, but
// sorting it would scramble the category grouping the defaults are written in
// and churn the prompt for every existing user.
func resolveBlockedCommands(perms *config.Permissions) []string {
	if perms == nil {
		return slices.Clone(defaultBlockedCommands)
	}
	// Both sides are normalized the way the block list itself matches, so a
	// config entry spelled "/usr/bin/CURL" or " curl " lines up with the
	// "curl" already on the list. Without this, allowing a command by a
	// slightly different spelling did nothing at all, which is the worst way
	// for a permission setting to be wrong: it looks applied.
	allowed := make(map[string]struct{}, len(perms.AllowedCommands))
	for _, cmd := range perms.AllowedCommands {
		allowed[shell.NormalizeCommandName(cmd)] = struct{}{}
	}

	blocked := make([]string, 0, len(defaultBlockedCommands)+len(perms.BlockedCommands))
	blocked = append(blocked, defaultBlockedCommands...)
	for _, cmd := range perms.BlockedCommands {
		if name := shell.NormalizeCommandName(cmd); name != "" {
			blocked = append(blocked, name)
		}
	}
	blocked = slices.DeleteFunc(blocked, func(c string) bool {
		_, ok := allowed[shell.NormalizeCommandName(c)]
		return ok
	})

	seen := make(map[string]struct{}, len(blocked))
	deduped := blocked[:0]
	for _, cmd := range blocked {
		if _, dup := seen[cmd]; dup {
			continue
		}
		seen[cmd] = struct{}{}
		deduped = append(deduped, cmd)
	}
	return deduped
}

func bashDescription(attribution *config.Attribution, modelID string, blocked []string) string {
	var out bytes.Buffer
	if err := bashDescriptionTpl.Execute(&out, bashDescriptionData{
		BannedCommands:  strings.Join(blocked, ", "),
		MaxOutputLength: MaxOutputLength,
		Attribution:     *attribution,
		ModelID:         modelID,
		RgAvailable:     getRg() != "",
		GhAvailable:     ghAvailable,
	}); err != nil {
		// this should never happen.
		panic("failed to execute bash description template: " + err.Error())
	}
	return out.String()
}

// blockFuncs builds the block list from the resolved dangerous commands.
// The flat command list is configurable; the argument blockers below are
// structural rules about specific flags rather than a list of binaries, so
// they stay built in.
func blockFuncs(blocked []string) []shell.BlockFunc {
	return []shell.BlockFunc{
		shell.CommandsBlocker(blocked),

		// System package managers
		shell.ArgumentsBlocker("apk", []string{"add"}, nil),
		shell.ArgumentsBlocker("apt", []string{"install"}, nil),
		shell.ArgumentsBlocker("apt-get", []string{"install"}, nil),
		shell.ArgumentsBlocker("dnf", []string{"install"}, nil),
		shell.ArgumentsBlocker("pacman", nil, []string{"-S"}),
		shell.ArgumentsBlocker("pkg", []string{"install"}, nil),
		shell.ArgumentsBlocker("yum", []string{"install"}, nil),
		shell.ArgumentsBlocker("zypper", []string{"install"}, nil),

		// Language-specific package managers
		shell.ArgumentsBlocker("brew", []string{"install"}, nil),
		shell.ArgumentsBlocker("cargo", []string{"install"}, nil),
		shell.ArgumentsBlocker("gem", []string{"install"}, nil),
		shell.ArgumentsBlocker("go", []string{"install"}, nil),
		shell.ArgumentsBlocker("npm", []string{"install"}, []string{"--global"}),
		shell.ArgumentsBlocker("npm", []string{"install"}, []string{"-g"}),
		shell.ArgumentsBlocker("pip", []string{"install"}, []string{"--user"}),
		shell.ArgumentsBlocker("pip3", []string{"install"}, []string{"--user"}),
		shell.ArgumentsBlocker("pnpm", []string{"add"}, []string{"--global"}),
		shell.ArgumentsBlocker("pnpm", []string{"add"}, []string{"-g"}),
		shell.ArgumentsBlocker("yarn", []string{"global", "add"}, nil),

		// `go test -exec` can run arbitrary commands
		shell.ArgumentsBlocker("go", []string{"test"}, []string{"-exec"}),
	}
}

func NewBashTool(permissions permission.Service, workingDir, spillDir string, attribution *config.Attribution, modelID string, maxBackgroundJobs int, autoBackgroundAfter int, perms *config.Permissions) fantasy.AgentTool {
	blocked := resolveBlockedCommands(perms)
	userSafe := userSafeCommands(perms)
	return fantasy.NewAgentTool(
		BashToolName,
		string(bashDescription(attribution, modelID, blocked)),
		func(ctx context.Context, params BashParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if params.Command == "" {
				return fantasy.NewTextErrorResponse("missing command"), nil
			}

			// Determine working directory
			execWorkingDir := cmp.Or(params.WorkingDir, workingDir)

			safeReadOnly := isSafeReadOnly(params.Command, userSafe)

			sessionID := GetSessionFromContext(ctx)
			if sessionID == "" {
				return fantasy.ToolResponse{}, fmt.Errorf("session ID is required for executing shell command")
			}
			// Check whether the command is dangerous so we can surface a
			// warning in the permission dialog.
			var check shell.CommandCheck
			mode := permissions.PermissionMode()
			if !safeReadOnly {
				check = shell.CheckCommand(params.Command, blockFuncs(blocked))

				if check.Matched && mode.RefusesDangerous() {
					return fantasy.NewTextErrorResponse(yoloBlockedMessage(check.Reason)), nil
				}

				p, err := permissions.Request(
					ctx,
					permission.CreatePermissionRequest{
						SessionID:   sessionID,
						Path:        execWorkingDir,
						ToolCallID:  call.ID,
						ToolName:    BashToolName,
						Action:      "execute",
						Description: fmt.Sprintf("Execute command: %s", params.Command),
						Params:      BashPermissionsParams(params),
						Danger:      check.Reason,
						// Path is only the working directory here, so a
						// session grant keyed on it alone would turn one
						// approval of `ls` into standing approval for every
						// later command run in that directory. Key it to the
						// command the user was actually shown.
						GrantKey: params.Command,
					},
				)
				if err != nil {
					return fantasy.ToolResponse{}, err
				}
				if !p {
					return NewPermissionDeniedResponse(), nil
				}
			}

			// A named match reaching here was approved at a prompt; keep the block list for everything else.
			var blocksToUse []shell.BlockFunc
			if !check.Matched && mode != permission.PermissionModeSysadmin {
				blocksToUse = blockFuncs(blocked)
			}
			return executeBashCommand(ctx, params, execWorkingDir, spillDir, blocksToUse, maxBackgroundJobs, autoBackgroundAfter)
		},
	)
}

// yoloBlockedMessage names the way out so the model doesn't retry variations.
func yoloBlockedMessage(command string) string {
	return fmt.Sprintf("blocked: %s is a dangerous command, and yolo mode blocks dangerous commands without asking. Tell the user: they can switch to normal mode to be asked about it, use sysadmin mode to allow it, or mark it safe with `permissions safe`.", command)
}

func executeBashCommand(ctx context.Context, params BashParams, execWorkingDir, spillDir string, blockFuncsToUse []shell.BlockFunc, maxBackgroundJobs, autoBackgroundAfter int) (fantasy.ToolResponse, error) {	// If explicitly requested as background, start immediately with detached context
	if params.RunInBackground {
		startTime := time.Now()
		bgManager := shell.GetBackgroundShellManager()
		// Use background context so it continues after tool returns
		bgShell, err := bgManager.Start(context.Background(), execWorkingDir, blockFuncsToUse, params.Command, params.Description, maxBackgroundJobs)
		if err != nil {
			return fantasy.NewTextErrorResponse("Failed to start background shell: " + err.Error()), nil
		}

		// Wait a short time to detect fast failures (blocked commands, syntax errors, etc.)
		time.Sleep(1 * time.Second)
		stdout, stderr, done, execErr := bgShell.GetOutput()

		if done {
			// Command failed or completed very quickly
			bgManager.Remove(bgShell.ID)

			interrupted := shell.IsInterrupt(execErr)
			exitCode := shell.ExitCode(execErr)
			if exitCode == 0 && !interrupted && execErr != nil {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("[Job %s] Error executing command: %s", bgShell.ID, execErr)), nil
			}

			stdout = formatOutput(stdout, stderr, execErr, spillDir)

			metadata := BashResponseMetadata{
				StartTime:        startTime.UnixMilli(),
				EndTime:          time.Now().UnixMilli(),
				Output:           stdout,
				Description:      params.Description,
				Background:       params.RunInBackground,
				WorkingDirectory: bgShell.WorkingDir,
			}
			if stdout == "" {
				stdout = BashNoOutput
			}
			return fantasy.WithResponseMetadata(fantasy.NewTextResponse(stdout), metadata), nil
		}

		// Still running after fast-failure check - return as background job
		metadata := BashResponseMetadata{
			StartTime:        startTime.UnixMilli(),
			EndTime:          time.Now().UnixMilli(),
			Description:      params.Description,
			WorkingDirectory: bgShell.WorkingDir,
			Background:       true,
			ShellID:          bgShell.ID,
		}
		bgShell.MarkBackgrounded()
		response := fmt.Sprintf(
			"Background shell started with ID: %s.\n\nIt runs on its own: when it finishes, its output will be delivered to you, so there is nothing to wait for. job_kill %s stops it early.%s",
			bgShell.ID, bgShell.ID, redirectSection(params.Command))
		return fantasy.WithResponseMetadata(fantasy.NewTextResponse(response), metadata), nil
	}

	// Start synchronous execution with auto-background support
	startTime := time.Now()

	// Start with detached context so it can survive if moved to background
	bgManager := shell.GetBackgroundShellManager()
	bgShell, err := bgManager.Start(context.Background(), execWorkingDir, blockFuncsToUse, params.Command, params.Description, maxBackgroundJobs)
	if err != nil {
		return fantasy.NewTextErrorResponse("Failed to start shell: " + err.Error()), nil
	}

	// Wait for either completion, auto-background threshold, or context cancellation
	waitSeconds := cmp.Or(params.AutoBackgroundAfter, autoBackgroundAfter, DefaultAutoBackgroundAfter)
	autoBackgroundThreshold := time.Duration(waitSeconds) * time.Second
	// A command that writes its output to a file will never produce
	// anything here, so the full threshold would be spent waiting for
	// output that cannot arrive. Only the fast-failure window is worth
	// keeping, and the log file is what the caller reads afterwards.
	redirectedTo := redirectTarget(params.Command)
	if redirectedTo != "" && autoBackgroundThreshold > RedirectedOutputGrace {
		autoBackgroundThreshold = RedirectedOutputGrace
	}
	timer := time.NewTimer(autoBackgroundThreshold)
	defer timer.Stop()

	select {
	case <-bgShell.Done():
	case <-timer.C:
	case <-ctx.Done():
		// Incoming context was cancelled before we moved to background
		// Kill the shell and return error
		bgManager.Kill(bgShell.ID)
		return fantasy.ToolResponse{}, ctx.Err()
	}
	stdout, stderr, done, execErr := bgShell.GetOutput()

	if done {
		// Command completed within threshold - return synchronously
		// Remove from background manager since we're returning directly
		// Don't call Kill() as it cancels the context and corrupts the exit code
		bgManager.Remove(bgShell.ID)

		interrupted := shell.IsInterrupt(execErr)
		exitCode := shell.ExitCode(execErr)
		if exitCode == 0 && !interrupted && execErr != nil {
			return fantasy.NewTextErrorResponse(fmt.Sprintf("[Job %s] Error executing command: %s", bgShell.ID, execErr)), nil
		}

		stdout = formatOutput(stdout, stderr, execErr, spillDir)

		if note := redirectNote(params.Command, bgShell.OutputWritten(), true); note != "" {
			stdout = strings.TrimSpace(stdout + "\n\n" + note)
		}

		metadata := BashResponseMetadata{
			StartTime:        startTime.UnixMilli(),
			EndTime:          time.Now().UnixMilli(),
			Output:           stdout,
			Description:      params.Description,
			Background:       params.RunInBackground,
			WorkingDirectory: bgShell.WorkingDir,
		}
		if stdout == "" {
			stdout = BashNoOutput
		}
		return fantasy.WithResponseMetadata(fantasy.NewTextResponse(stdout), metadata), nil
	}

	// Still running - keep as background job
	bgShell.MarkBackgrounded()
	metadata := BashResponseMetadata{
		StartTime:        startTime.UnixMilli(),
		EndTime:          time.Now().UnixMilli(),
		Description:      params.Description,
		WorkingDirectory: bgShell.WorkingDir,
		Background:       true,
		ShellID:          bgShell.ID,
	}
	response := fmt.Sprintf(
		"It was still running after %s, so it has been moved to the background (last output %s ago, %s captured).\n\nBackground shell ID: %s\n\nThe command keeps running and its output keeps accumulating; job_output %s returns it whenever you need the result, and job_kill %s stops it. You are not blocked on this: carry on with the conversation, or check back when its result is what you need.%s",
		autoBackgroundThreshold,
		formatJobDuration(bgShell.IdleFor()),
		formatJobBytes(bgShell.OutputWritten()),
		bgShell.ID,
		bgShell.ID,
		bgShell.ID,
		redirectSection(params.Command),
	)
	return fantasy.WithResponseMetadata(fantasy.NewTextResponse(response), metadata), nil
}

// redirectTarget reports the file a command sends its output to, if it does.
//
// A command that redirects its output writes nothing on the streams the job
// captures, so a perfectly busy job looks silent and a model waiting on it
// sees "no output" forever. Naming the file is what lets it read the log
// instead.
//
// Only the command's final top-level statement is considered: in a compound
// command the earlier statements are setup (`cat > script.py <<EOF ...`, `cd`),
// and their redirections say nothing about where the long-running command
// writes.
func redirectTarget(command string) string {
	// An unparseable command yields no target: scanning its raw text would
	// read heredoc bodies and quoted scripts as redirections.
	return scanRedirect(lastStatementSource(command))
}

// lastStatementSource returns the source text of the final top-level statement,
// or an empty string when the command does not parse.
func lastStatementSource(command string) string {
	file, err := syntax.NewParser().Parse(strings.NewReader(command), "")
	if err != nil || len(file.Stmts) == 0 {
		return ""
	}
	stmt := file.Stmts[len(file.Stmts)-1]
	start, end := int(stmt.Pos().Offset()), int(stmt.End().Offset())
	if start < 0 || end > len(command) || start >= end {
		return ""
	}
	return command[start:end]
}

// scanRedirect walks command once, skipping quoted spans, and reads the word
// after a `>`, `>>` or `&>` redirection. A descriptor duplicate such as `2>&1`
// yields no target, and a redirect whose target is a descriptor yields none
// either.
func scanRedirect(command string) string {
	for i := 0; i < len(command); i++ {
		switch c := command[i]; c {
		case '\'', '"':
			// Skip the quoted span: a `>` inside it is text, not a redirect.
			end := i + 1
			for end < len(command) && command[end] != c {
				end++
			}
			i = end
		case '>':
			// Skip the rest of the operator, then whitespace.
			if i+1 < len(command) && command[i+1] == '>' {
				i++
			}
			j := i + 1
			for j < len(command) && (command[j] == ' ' || command[j] == '\t') {
				j++
			}
			if j >= len(command) || command[j] == '&' {
				// `>&2`, `2>&1`: a descriptor, not a file.
				return ""
			}
			if q := command[j]; q == '\'' || q == '"' {
				end := j + 1
				for end < len(command) && command[end] != q {
					end++
				}
				return command[j+1 : end]
			}
			end := j
			for end < len(command) && !strings.ContainsRune(" \t;&|<>", rune(command[end])) {
				end++
			}
			if end > j {
				return command[j:end]
			}
			return ""
		}
	}
	return ""
}

// redirectSection tells the caller where a job's output is going when its
// command writes to a file, and that waiting on the job for output is
// pointless. Empty for commands without a redirect.
func redirectSection(command string) string {
	target := redirectTarget(command)
	if target == "" {
		return ""
	}
	return fmt.Sprintf("\n\nIts output goes to %q: read that file for progress and for the result, since the job itself carries only the exit status. Check back on the file when you need the outcome; do not wait on the job for output.", target)
}

// redirectNote explains where a command's output went when nothing reached the
// captured streams. It is empty when the command wrote nothing anywhere it
// knows about, or when output was captured anyway.
//
// A running job gets "read the file instead of waiting"; a finished one gets a
// pointer to the result the caller would otherwise think was lost.
func redirectNote(command string, written int64, done bool) string {
	if written > 0 {
		return ""
	}
	target := redirectTarget(command)
	if target == "" {
		return ""
	}
	if done {
		return fmt.Sprintf("This command wrote its output to %q rather than to its own streams; read that file for the result.", target)
	}
	return fmt.Sprintf("This command has produced no output because it redirects its output to %q; read that file instead of waiting on the job.", target)
}

// formatOutput formats the output of a completed command with error handling
func formatOutput(stdout, stderr string, execErr error, spillDir string) string {
	interrupted := shell.IsInterrupt(execErr)
	exitCode := shell.ExitCode(execErr)

	// Repair ANSI-code-page output before truncation, so the cap measures
	// UTF-8 text and the spilled full output stays readable.
	stdout = shell.DecodeTerminalText(stdout)
	stderr = shell.DecodeTerminalText(stderr)

	stdout = truncateOutput(stdout, spillDir)
	stderr = truncateOutput(stderr, spillDir)

	errorMessage := stderr
	if errorMessage == "" && execErr != nil {
		errorMessage = execErr.Error()
	}

	if interrupted {
		if errorMessage != "" {
			errorMessage += "\n"
		}
		errorMessage += "Command was aborted before completion"
	}

	hasBothOutputs := stdout != "" && stderr != ""

	if hasBothOutputs {
		stdout += "\n"
	}

	if errorMessage != "" {
		stdout += "\n" + errorMessage
	}

	exitMarker := fmt.Sprintf("[exit code: %d]", exitCode)
	if stdout == "" {
		return exitMarker
	}
	return stdout + "\n" + exitMarker
}

// TruncateOutput keeps the head and tail of oversized tool output. The
// dropped middle is written in full to a file under spillDir so the agent
// can read it back rather than losing it for good.
func TruncateOutput(content, spillDir string) string {
	if ansi.StringWidth(content) <= MaxOutputLength {
		return content
	}

	halfLength := MaxOutputLength / 2
	start := ansi.Truncate(content, halfLength, "")
	end := ansi.TruncateLeft(content, ansi.StringWidth(content)-halfLength, "")

	truncatedLinesCount := max(strings.Count(content, "\n")-strings.Count(start, "\n")-strings.Count(end, "\n"), 0)
	marker := fmt.Sprintf("... [%d lines truncated] ...", truncatedLinesCount)
	if path, err := shell.SpillOutput(ansi.Strip(content), spillDir); err == nil {
		marker = fmt.Sprintf("... [%d lines truncated, full output: %s] ...", truncatedLinesCount, path)
	} else {
		slog.Debug("Could not spill tool output", "dir", spillDir, "error", err)
	}
	return fmt.Sprintf("%s\n\n%s\n\n%s", start, marker, end)
}

func truncateOutput(content, spillDir string) string {
	return TruncateOutput(content, spillDir)
}

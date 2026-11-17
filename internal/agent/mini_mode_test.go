package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/stretchr/testify/require"
)

// Mini mode keeps the shell and the one tool a shell cannot provide for
// itself, and drops everything else — that trimming is what keeps the other
// schemas out of every request.
func TestEffectiveToolNamesMiniKeepsOnlyTheShell(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store, err := config.Init(t.Context(), dir, "", false)
	require.NoError(t, err)
	store.Overrides().Mode = config.ModeMini

	c := &coordinator{cfg: store, interactive: true}
	agentCfg := config.Agent{
		ID: config.AgentCoder,
		AllowedTools: []string{
			tools.BashToolName,
			tools.ViewToolName,
			tools.EditToolName,
			tools.GrepToolName,
			tools.JobOutputToolName,
			tools.JobKillToolName,
			AgentToolName,
			AgentDispatchToolName,
			tools.QuestionToolName,
		},
	}

	require.Equal(t, []string{
		tools.BashToolName,
		tools.ReadImageToolName,
		tools.FetchToolName,
		tools.WebSearchToolName,
		tools.JobKillToolName,
	}, c.effectiveToolNames(agentCfg, false))

	t.Run("subagents are not trimmed by mini", func(t *testing.T) {
		t.Parallel()
		// A subagent's tools are chosen by its own access level; mini
		// decides what the top-level conversation can reach.
		got := c.effectiveToolNames(agentCfg, true)
		require.Contains(t, got, tools.ViewToolName)
	})

	t.Run("mini-mcp adds the tools a shell cannot replace", func(t *testing.T) {
		t.Parallel()
		store.Overrides().Mode = config.ModeMiniMCP
		t.Cleanup(func() { store.Overrides().Mode = config.ModeMini })

		// Same five, plus the file tools whose diff preview, diagnostics,
		// line-numbered reads and ignore-aware, output-capped search a shell
		// does worse.
		got := c.effectiveToolNames(agentCfg, false)
		require.Equal(t, []string{
			tools.BashToolName,
			tools.ReadImageToolName,
			tools.FetchToolName,
			tools.WebSearchToolName,
			tools.JobKillToolName,
			tools.ViewToolName,
			tools.EditToolName,
			tools.MultiEditToolName,
			tools.WriteToolName,
			tools.GrepToolName,
			tools.GlobToolName,
			tools.LSToolName,
			tools.JobOutputToolName,
		}, got)
	})
}

// The full toolset is untouched when mini is off, including the tools that
// depend on the run mode.
func TestEffectiveToolNamesFullMode(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store, err := config.Init(t.Context(), dir, "", false)
	require.NoError(t, err)

	c := &coordinator{cfg: store, interactive: true}
	agentCfg := config.Agent{
		ID:           config.AgentCoder,
		AllowedTools: []string{tools.BashToolName, AgentToolName, AgentDispatchToolName},
	}

	// Interactive runs can only use one of the delegation tools.
	require.Equal(t, []string{tools.BashToolName, AgentDispatchToolName},
		c.effectiveToolNames(agentCfg, false))
}

// Mini's system prompt is the short fixed one, not the coder prompt.
func TestMiniPromptIsShort(t *testing.T) {
	t.Parallel()

	p, err := miniPrompt()
	require.NoError(t, err)

	dir := t.TempDir()
	store, err := config.Init(t.Context(), dir, "", false)
	require.NoError(t, err)

	built, reminder, err := p.BuildWithReminder(t.Context(), "provider", "model", store)
	require.NoError(t, err)
	require.Equal(t, "You are a helpful software engineer assistant.", strings.TrimSpace(built),
		"mini's system prompt is one sentence")
	require.NotContains(t, built, "<workflow>", "the coder prompt's sections must not leak into mini")

	// The session context is a single reminder line, the way the minimal
	// presets this mirrors carry the working directory.
	require.Contains(t, reminder, "<system-reminder>")
	require.Contains(t, reminder, "Current working directory:")
	require.Less(t, len(reminder), 300, "the reminder stays one line: %d bytes", len(reminder))
}

// The session context must say where the session is without a round of
// discovery: a git repo gets its branch (and whether the tree is dirty), a
// non-repo says so outright.
func TestMiniContextNamesTheRepository(t *testing.T) {
	t.Parallel()

	p, err := miniPrompt()
	require.NoError(t, err)

	dir := t.TempDir()
	store, err := config.Init(t.Context(), dir, "", false)
	require.NoError(t, err)

	_, reminder, err := p.BuildWithReminder(t.Context(), "provider", "model", store)
	require.NoError(t, err)
	require.Contains(t, reminder, "not a git repository")

	// A repository: fake just enough of one for the fs-based branch reader.
	gitDir := filepath.Join(dir, ".git")
	require.NoError(t, os.MkdirAll(filepath.Join(gitDir, "refs", "heads"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte("ref: refs/heads/topic\n"), 0o644))

	store2, err := config.Init(t.Context(), dir, "", false)
	require.NoError(t, err)
	_, reminder, err = p.BuildWithReminder(t.Context(), "provider", "model", store2)
	require.NoError(t, err)
	require.Contains(t, reminder, "git branch topic")
	require.NotContains(t, reminder, "not a git repository")
}

// mini-mcp lists the skills and context files instead of inlining them: the
// model gets a hint and reads what the task needs, which is what keeps the
// reminder small while making AGENTS.md findable at all.
func TestMiniMCPContextListsSkillsAndContextFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	agents := filepath.Join(dir, "AGENTS.md")
	require.NoError(t, os.WriteFile(agents, []byte("# rules\n- build with go build\n"), 0o644))

	store, err := config.Init(t.Context(), dir, "", false)
	require.NoError(t, err)
	store.Config().Options.ContextPaths = []string{agents}

	p, err := miniMCPPrompt()
	require.NoError(t, err)
	_, reminder, err := p.BuildWithReminder(t.Context(), "provider", "model", store)
	require.NoError(t, err)

	// The file is named (with forward slashes, like every other path a prompt
	// shows), and its contents are not inlined.
	require.Contains(t, reminder, filepath.ToSlash(agents))
	require.NotContains(t, reminder, "build with go build")
	require.Contains(t, reminder, "<context_files>")

	// Plain mini keeps the one-line reminder.
	pMini, err := miniPrompt()
	require.NoError(t, err)
	_, plain, err := pMini.BuildWithReminder(t.Context(), "provider", "model", store)
	require.NoError(t, err)
	require.NotContains(t, plain, "<context_files>")
	require.NotContains(t, plain, "AGENTS.md")
}

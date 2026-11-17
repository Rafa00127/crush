package agent

import (
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
	store.Overrides().Mini = true

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

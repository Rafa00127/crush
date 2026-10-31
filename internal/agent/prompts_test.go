package agent

import (
	"bytes"
	"testing"
	"text/template"

	"github.com/charmbracelet/crush/internal/agent/prompt"
	"github.com/stretchr/testify/require"
)

func renderAgentTemplate(t *testing.T, name string, raw []byte, data prompt.PromptDat) string {
	t.Helper()

	tmpl, err := template.New(name).Parse(string(raw))
	require.NoError(t, err)

	var buf bytes.Buffer
	require.NoError(t, tmpl.Execute(&buf, data))
	return buf.String()
}

func TestCoderPromptWebSearchRouting(t *testing.T) {
	t.Parallel()

	base := prompt.PromptDat{WorkingDir: "/workspace", Platform: "linux", Date: "1/2/2026"}

	without := renderAgentTemplate(t, "coder", coderPromptTmpl, base)
	require.NotContains(t, without, "`web_search`")
	require.Contains(t, without, "`agentic_fetch`: delegate multi-page research")

	with := base
	with.WebSearch = true
	out := renderAgentTemplate(t, "coder", coderPromptTmpl, with)
	require.Contains(t, out, "`web_search`: find URLs and summaries")
}

func TestAgentPromptTemplatesRender(t *testing.T) {
	t.Parallel()

	data := prompt.PromptDat{
		WorkingDir: "/workspace",
		Platform:   "linux",
		Date:       "1/2/2026",
		ContextFiles: []prompt.ContextFile{
			{Path: "AGENTS.md", Content: "project rules"},
		},
	}

	t.Run("coder", func(t *testing.T) {
		t.Parallel()

		out := renderAgentTemplate(t, "coder", coderPromptTmpl, data)
		require.NotEmpty(t, out)
		require.NotContains(t, out, "project rules")
		require.NotContains(t, out, "<env>")
	})

	t.Run("context", func(t *testing.T) {
		t.Parallel()

		out := renderAgentTemplate(t, "context", contextReminderTmpl, data)
		require.Contains(t, out, "Working directory: /workspace")
		require.Contains(t, out, "project rules")
		require.Contains(t, out, "<project_context>")
	})
}

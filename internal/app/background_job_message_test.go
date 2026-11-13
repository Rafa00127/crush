package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/crush/internal/shell"
	"github.com/stretchr/testify/require"
)

func TestBackgroundJobMessage(t *testing.T) {
	t.Parallel()

	t.Run("prefers the description", func(t *testing.T) {
		t.Parallel()
		job := &shell.BackgroundShell{
			ID:          "00A",
			Command:     "python -u scripts/restore.py --frames 33",
			Description: "restore frames",
		}
		require.Equal(t, "Job 00A finished (exit -1) — restore frames", backgroundJobMessage(job),
			"an unfinished job reports -1, which the caller never sees because only finished jobs notify")
	})

	t.Run("falls back to the command and flattens it", func(t *testing.T) {
		t.Parallel()
		job := &shell.BackgroundShell{
			ID:      "00B",
			Command: "sleep 30\n&& echo done",
		}
		require.Equal(t, "Job 00B finished (exit -1) — sleep 30 && echo done", backgroundJobMessage(job))
	})

	t.Run("truncates a long subject", func(t *testing.T) {
		t.Parallel()
		job := &shell.BackgroundShell{ID: "00C", Command: strings.Repeat("x", 200)}
		msg := backgroundJobMessage(job)
		require.Contains(t, msg, "...")
		require.LessOrEqual(t, len(msg), 140)
	})
}

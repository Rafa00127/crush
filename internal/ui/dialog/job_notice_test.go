package dialog

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/stretchr/testify/require"
)

func newJobNoticeForTest(t *testing.T) *JobNotice {
	t.Helper()
	sty := styles.CharmtonePantera()
	return NewJobNotice(&common.Common{Styles: &sty}, "00A", "Job 00A finished (exit 0) — restore frames")
}

// Enter confirms the selected answer, so the default (Tell Crush) hands the
// job to the agent without needing any other key.
func TestJobNotice_EnterTellsCrush(t *testing.T) {
	t.Parallel()

	n := newJobNoticeForTest(t)
	action := n.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.Equal(t, ActionTellAboutJob{JobID: "00A", Message: "Job 00A finished (exit 0) — restore frames"}, action)
}

// Moving to Dismiss and pressing Enter closes without telling the agent.
func TestJobNotice_EnterOnDismissCloses(t *testing.T) {
	t.Parallel()

	n := newJobNoticeForTest(t)
	require.Nil(t, n.HandleMsg(tea.KeyPressMsg{Code: tea.KeyRight}))
	require.Equal(t, ActionClose{}, n.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEnter}))
}

func TestJobNotice_EscapeAndShortcuts(t *testing.T) {
	t.Parallel()

	n := newJobNoticeForTest(t)
	require.Equal(t, ActionClose{}, n.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEsc}))

	n = newJobNoticeForTest(t)
	require.Equal(t, ActionClose{}, n.HandleMsg(tea.KeyPressMsg{Code: 'n', Text: "n"}))

	n = newJobNoticeForTest(t)
	require.Equal(t, ActionTellAboutJob{JobID: "00A", Message: "Job 00A finished (exit 0) — restore frames"},
		n.HandleMsg(tea.KeyPressMsg{Code: 'y', Text: "y"}))
}

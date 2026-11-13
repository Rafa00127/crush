package dialog

import (
	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/crush/internal/ui/common"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

// JobNoticeID is the identifier for the background-job notice dialog.
const JobNoticeID = "job_notice"

// JobNotice asks what to do with a finished background job. It borrows the
// permission dialog's chrome on purpose: a job finishing is the same kind of
// decision (a box with two answers), and it must not be something the user
// only notices if they happen to read the help line.
type JobNotice struct {
	com     *common.Common
	jobID   string
	message string

	// dismissing is true when the "Dismiss" button is selected.
	dismissing bool

	help   help.Model
	keyMap struct {
		LeftRight  key.Binding
		EnterSpace key.Binding
		Tell       key.Binding
		Dismiss    key.Binding
		Close      key.Binding
	}
}

var _ Dialog = (*JobNotice)(nil)

// NewJobNotice creates the dialog for one finished job.
func NewJobNotice(com *common.Common, jobID, message string) *JobNotice {
	n := &JobNotice{
		com:     com,
		jobID:   jobID,
		message: message,
		help:    help.New(),
	}
	n.keyMap.LeftRight = key.NewBinding(
		key.WithKeys("left", "right", "tab", "shift+tab"),
		key.WithHelp("←/→", "switch options"),
	)
	n.keyMap.EnterSpace = key.NewBinding(
		key.WithKeys("enter", " ", "space"),
		key.WithHelp("enter", "confirm"),
	)
	n.keyMap.Tell = key.NewBinding(
		key.WithKeys("y", "Y"),
		key.WithHelp("y", "tell Crush"),
	)
	n.keyMap.Dismiss = key.NewBinding(
		key.WithKeys("n", "N"),
		key.WithHelp("n", "dismiss"),
	)
	n.keyMap.Close = CloseKey
	return n
}

// ID implements [Model].
func (*JobNotice) ID() string { return JobNoticeID }

// HandleMsg implements [Model].
func (n *JobNotice) HandleMsg(msg tea.Msg) Action {
	keyMsg, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	switch {
	case key.Matches(keyMsg, n.keyMap.Close):
		return ActionClose{}
	case key.Matches(keyMsg, n.keyMap.LeftRight):
		n.dismissing = !n.dismissing
	case key.Matches(keyMsg, n.keyMap.EnterSpace):
		if n.dismissing {
			return ActionClose{}
		}
		return ActionTellAboutJob{JobID: n.jobID, Message: n.message}
	case key.Matches(keyMsg, n.keyMap.Tell):
		return ActionTellAboutJob{JobID: n.jobID, Message: n.message}
	case key.Matches(keyMsg, n.keyMap.Dismiss):
		return ActionClose{}
	}
	return nil
}

// Draw implements [Dialog].
func (n *JobNotice) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := n.com.Styles

	width := min(simpleMaxWidth, area.Dx()-4)
	if width < 40 {
		width = max(area.Dx()-4, 20)
	}
	dialogStyle := t.Dialog.View.Width(width).Padding(0, 1)
	contentWidth := width - dialogStyle.GetHorizontalFrameSize()

	title := common.DialogTitle(
		t, "Background Job Finished",
		contentWidth-t.Dialog.Title.GetHorizontalFrameSize(),
		t.Dialog.TitleGradFromColor, t.Dialog.TitleGradToColor,
	)
	title = t.Dialog.Title.Render(title)

	body := ansi.Wordwrap(n.message, contentWidth, " ")

	buttons := lipgloss.NewStyle().Width(contentWidth).Align(lipgloss.Right).Render(
		lipgloss.JoinHorizontal(
			lipgloss.Center,
			common.Button(t, common.ButtonOpts{Text: "Tell Crush", Selected: !n.dismissing}),
			"  ",
			common.Button(t, common.ButtonOpts{Text: "Dismiss", Selected: n.dismissing}),
		),
	)

	helpView := shortHelpLine(&n.help, n.ShortHelp(), contentWidth)

	inner := lipgloss.JoinVertical(lipgloss.Left, title, "", body, "", buttons, "", helpView)
	DrawCenterCursor(scr, area, dialogStyle.Render(inner), nil)
	return nil
}

// ShortHelp implements [help.KeyMap].
func (n *JobNotice) ShortHelp() []key.Binding {
	return []key.Binding{n.keyMap.LeftRight, n.keyMap.EnterSpace, n.keyMap.Close}
}

// FullHelp implements [help.KeyMap].
func (n *JobNotice) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{n.keyMap.LeftRight, n.keyMap.EnterSpace},
		{n.keyMap.Tell, n.keyMap.Dismiss, n.keyMap.Close},
	}
}

package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7D56F4"))
	dimStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	rolelineStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#7D56F4")).Bold(true)
)

type sessionItem struct{ s Session }

func (i sessionItem) FilterValue() string {
	return i.s.Project + " " + i.s.Preview + " " + i.s.Entrypoint + " " + i.s.GitBranch
}
func (i sessionItem) Title() string {
	p := i.s.Preview
	if p == "" {
		p = "(no preview)"
	}
	return p
}
func (i sessionItem) Description() string {
	when := humanTime(i.s.UpdatedAt)
	ep := i.s.Entrypoint
	if ep == "" {
		ep = "?"
	}
	br := i.s.GitBranch
	if br == "" {
		br = "-"
	}
	return fmt.Sprintf("%s · %s · %s · %s", when, ep, shortPath(i.s.Project), br)
}

type model struct {
	list      list.Model
	viewport  viewport.Model
	sessions  []Session
	width     int
	height    int
	ready     bool
	resumeCmd string
}

type loadedMsg struct{ sessions []Session }
type previewMsg struct {
	id   string
	body string
}

func loadCmd() tea.Cmd {
	return func() tea.Msg {
		ss, _ := scanAll()
		return loadedMsg{ss}
	}
}

func previewCmd(s Session) tea.Cmd {
	return func() tea.Msg {
		body, _ := renderTranscript(s.Path, 200_000)
		return previewMsg{id: s.ID, body: body}
	}
}

func (m model) Init() tea.Cmd { return loadCmd() }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		left := msg.Width * 2 / 5
		if left < 40 {
			left = 40
		}
		m.list.SetSize(left, msg.Height-2)
		if !m.ready {
			m.viewport = viewport.New(msg.Width-left-2, msg.Height-2)
			m.ready = true
		} else {
			m.viewport.Width = msg.Width - left - 2
			m.viewport.Height = msg.Height - 2
		}
	case loadedMsg:
		m.sessions = msg.sessions
		items := make([]list.Item, len(m.sessions))
		for i, s := range m.sessions {
			items[i] = sessionItem{s}
		}
		m.list.SetItems(items)
		if len(m.sessions) > 0 {
			cmds = append(cmds, previewCmd(m.sessions[0]))
		}
	case previewMsg:
		if it, ok := m.list.SelectedItem().(sessionItem); ok && it.s.ID == msg.id {
			m.viewport.SetContent(msg.body)
			m.viewport.GotoTop()
		}
	case tea.KeyMsg:
		filtering := m.list.FilterState() == list.Filtering
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "q":
			if !filtering {
				return m, tea.Quit
			}
		case "enter":
			if it, ok := m.list.SelectedItem().(sessionItem); ok {
				m.resumeCmd = buildResumeCmd(it.s, nil)
				return m, tea.Quit
			}
		case "d":
			if !filtering {
				if it, ok := m.list.SelectedItem().(sessionItem); ok {
					m.resumeCmd = buildResumeCmd(it.s, []string{"--dangerously-skip-permissions"})
					return m, tea.Quit
				}
			}
		case "c":
			if !filtering {
				if it, ok := m.list.SelectedItem().(sessionItem); ok {
					m.resumeCmd = buildResumeCmd(it.s, []string{"--chrome"})
					return m, tea.Quit
				}
			}
		case "D":
			if !filtering {
				if it, ok := m.list.SelectedItem().(sessionItem); ok {
					m.resumeCmd = buildResumeCmd(it.s, []string{"--dangerously-skip-permissions", "--chrome"})
					return m, tea.Quit
				}
			}
		case "r":
			if !filtering {
				return m, loadCmd()
			}
		case "j", "down":
			m.list, _ = m.list.Update(msg)
			if it, ok := m.list.SelectedItem().(sessionItem); ok {
				cmds = append(cmds, previewCmd(it.s))
			}
			return m, tea.Batch(cmds...)
		case "k", "up":
			m.list, _ = m.list.Update(msg)
			if it, ok := m.list.SelectedItem().(sessionItem); ok {
				cmds = append(cmds, previewCmd(it.s))
			}
			return m, tea.Batch(cmds...)
		}
	}
	var cmd tea.Cmd
	prev := m.list.Index()
	m.list, cmd = m.list.Update(msg)
	cmds = append(cmds, cmd)
	if m.list.Index() != prev {
		if it, ok := m.list.SelectedItem().(sessionItem); ok {
			cmds = append(cmds, previewCmd(it.s))
		}
	}
	return m, tea.Batch(cmds...)
}

func (m model) View() string {
	if !m.ready {
		return "loading…"
	}
	left := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("241")).
		Render(m.list.View())
	right := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("241")).
		Render(m.viewport.View())
	help := dimStyle.Render(" enter resume · d +dsp · c +chrome · D both · / search · r refresh · q quit ")
	return lipgloss.JoinVertical(lipgloss.Left,
		lipgloss.JoinHorizontal(lipgloss.Top, left, right),
		help,
	)
}

func main() {
	ttyIn, ttyOut, err := openTTY()
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot open tty:", err)
		os.Exit(1)
	}
	defer ttyIn.Close()
	defer ttyOut.Close()
	lipgloss.SetDefaultRenderer(lipgloss.NewRenderer(ttyOut))

	delegate := list.NewDefaultDelegate()
	delegate.Styles.SelectedTitle = delegate.Styles.SelectedTitle.Foreground(lipgloss.Color("#7D56F4")).BorderForeground(lipgloss.Color("#7D56F4"))
	delegate.Styles.SelectedDesc = delegate.Styles.SelectedDesc.Foreground(lipgloss.Color("#9B7DE0")).BorderForeground(lipgloss.Color("#7D56F4"))

	l := list.New(nil, delegate, 0, 0)
	l.Title = "Claude Sessions"
	l.Styles.Title = titleStyle
	l.SetShowStatusBar(true)
	l.SetFilteringEnabled(true)

	m := model{list: l}
	prog := tea.NewProgram(m, tea.WithAltScreen(), tea.WithInput(ttyIn), tea.WithOutput(ttyOut))
	final, err := prog.Run()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if fm, ok := final.(model); ok && fm.resumeCmd != "" {
		fmt.Println(fm.resumeCmd)
	}
}

func buildResumeCmd(s Session, extra []string) string {
	cwd := s.CWD
	if cwd == "" {
		cwd = s.Project
	}
	flags := ""
	for _, f := range extra {
		flags += " " + f
	}
	if runtime.GOOS == "windows" {
		return fmt.Sprintf(`cd /d "%s" && claude%s --resume %s`, cwd, flags, s.ID)
	}
	return fmt.Sprintf(`cd %q && claude%s --resume %s`, cwd, flags, s.ID)
}

func humanTime(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	default:
		return t.Format("2006-01-02")
	}
}

func shortPath(p string) string {
	home, _ := os.UserHomeDir()
	if home != "" && strings.HasPrefix(p, home) {
		return "~" + p[len(home):]
	}
	return p
}

// silence unused-import complaints if a field gets stripped
var _ = exec.Command
var _ = rolelineStyle

package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"curler/internal/config"
	"curler/internal/executor"
	"curler/internal/format"
)

type pane int

const (
	paneList pane = iota
	paneResponse
)

type runnerFunc func(context.Context, *executor.ResolvedRequest) (*executor.Response, error)

type model struct {
	path   string
	file   *config.File
	run    runnerFunc
	refs   []string
	width  int
	height int

	selected int
	focus    pane

	responseView viewport.Model
	status       string
	busy         bool
}

type runResultMsg struct {
	response *executor.Response
	rendered string
	err      error
}

func Run(path string, file *config.File) error {
	program := tea.NewProgram(newModel(path, file, executor.Execute), tea.WithAltScreen())
	_, err := program.Run()
	return err
}

func newModel(path string, file *config.File, runner runnerFunc) *model {
	file.Normalize()

	m := &model{
		path:   path,
		file:   file,
		run:    runner,
		focus:  paneList,
		status: "use up/down to choose a request, enter or ctrl+r to run, tab to focus the response pane",
	}
	m.responseView = viewport.New(0, 0)
	m.responseView.SetHorizontalStep(8)

	m.refreshRefs()
	if len(m.refs) == 0 {
		m.status = "no saved requests found; add them in .curler.yaml"
	}
	m.resetResponse(m.responsePlaceholder())
	return m
}

func (m *model) Init() tea.Cmd {
	return nil
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resize()
		return m, nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	case tea.MouseMsg:
		if m.focus == paneResponse {
			var cmd tea.Cmd
			m.responseView, cmd = m.responseView.Update(msg)
			return m, cmd
		}
	case runResultMsg:
		m.busy = false
		m.focus = paneResponse
		if msg.err != nil {
			m.resetResponse("Request failed.\n\n" + msg.err.Error())
			m.status = "request failed: " + msg.err.Error()
			return m, nil
		}
		m.resetResponse(msg.rendered)
		m.status = fmt.Sprintf("request complete: %s", msg.response.Status)
		return m, nil
	}

	return m, nil
}

func (m *model) View() string {
	if m.width == 0 || m.height == 0 {
		return "Loading curler..."
	}

	leftWidth := maxInt(28, m.width/4)
	rightWidth := maxInt(50, m.width-leftWidth-4)
	summaryHeight := maxInt(12, m.height/3)
	responseHeight := maxInt(8, m.height-summaryHeight-8)

	left := panelStyle(leftWidth, 0).Render(m.renderList())
	summary := panelStyle(rightWidth, summaryHeight).Render(m.renderSummary())
	response := panelStyle(rightWidth, responseHeight).Render(m.renderResponsePanel())

	help := lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Render(
		"q quit • up/down choose request • enter or ctrl+r run • tab focus response • esc focus list • response pane: arrows, pgup/pgdn, mouse wheel scroll",
	)
	status := lipgloss.NewStyle().Bold(true).Render(m.status)

	return lipgloss.JoinVertical(
		lipgloss.Left,
		lipgloss.JoinHorizontal(lipgloss.Top, left, lipgloss.JoinVertical(lipgloss.Left, summary, response)),
		"",
		status,
		help,
	)
}

func (m *model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "tab":
		if m.focus == paneList {
			m.focus = paneResponse
			m.status = "response pane focused"
		} else {
			m.focus = paneList
			m.status = "request list focused"
		}
		return m, nil
	case "esc":
		m.focus = paneList
		m.status = "request list focused"
		return m, nil
	case "enter", "ctrl+r":
		if m.busy {
			return m, nil
		}
		cmd, err := m.runSelected()
		if err != nil {
			m.resetResponse("Request failed.\n\n" + err.Error())
			m.status = "run failed: " + err.Error()
			m.focus = paneResponse
			return m, nil
		}
		m.busy = true
		m.status = "sending request..."
		return m, cmd
	}

	if m.focus == paneResponse {
		var cmd tea.Cmd
		m.responseView, cmd = m.responseView.Update(msg)
		return m, cmd
	}

	switch msg.String() {
	case "up", "k":
		if m.selected > 0 {
			m.selected--
			m.onSelectionChanged()
		}
	case "down", "j":
		if m.selected < len(m.refs)-1 {
			m.selected++
			m.onSelectionChanged()
		}
	case "g", "home":
		if len(m.refs) > 0 && m.selected != 0 {
			m.selected = 0
			m.onSelectionChanged()
		}
	case "G", "end":
		if len(m.refs) > 0 && m.selected != len(m.refs)-1 {
			m.selected = len(m.refs) - 1
			m.onSelectionChanged()
		}
	}

	return m, nil
}

func (m *model) renderList() string {
	lines := []string{
		m.panelHeader("Requests", m.focus == paneList),
		"",
		"Config: " + m.path,
		"Active env: " + m.file.ActiveEnv,
		"",
	}

	if len(m.refs) == 0 {
		lines = append(lines,
			"No saved requests yet.",
			"",
			"Add requests to .curler.yaml and reopen curler.",
		)
		return strings.Join(lines, "\n")
	}

	for index, ref := range m.refs {
		prefix := "  "
		if index == m.selected {
			prefix = "▸ "
		}
		lines = append(lines, prefix+ref)
	}

	return strings.Join(lines, "\n")
}

func (m *model) renderSummary() string {
	lines := []string{m.panelHeader("Summary (read-only)", false), ""}

	if len(m.refs) == 0 {
		lines = append(lines, "No request selected.", "", "Edit .curler.yaml to add requests.")
		return strings.Join(lines, "\n")
	}

	ref := m.currentRef()
	collectionName, requestName, request, err := m.file.GetRequest(ref)
	if err != nil {
		return strings.Join([]string{
			m.panelHeader("Summary (read-only)", false),
			"",
			"Failed to load request summary.",
			err.Error(),
		}, "\n")
	}
	request.Normalize()

	lines = append(lines,
		"Selected: "+collectionName+"/"+requestName,
		"Edit in YAML: "+m.path,
		"Environment: "+m.file.ActiveEnv,
		"Method: "+request.Method,
		"URL: "+request.URL,
		"Timeout: "+request.Timeout,
		"Auth: "+m.renderAuthSummary(request.Auth),
		"",
		"Headers:",
	)
	lines = append(lines, indentedLines(mapToLines(request.Headers, ": "))...)
	lines = append(lines, "", "Query:")
	lines = append(lines, indentedLines(mapToLines(request.Query, "="))...)
	lines = append(lines, "", "Body:")
	lines = append(lines, indentedLines(m.renderBodySummary(request.Body))...)
	return strings.Join(lines, "\n")
}

func (m *model) renderResponsePanel() string {
	title := m.panelHeader("Response", m.focus == paneResponse)
	meta := "Scroll with arrows, pgup/pgdn, or mouse wheel when this pane is focused."
	if m.busy {
		meta = "Request in flight..."
	} else if m.responseView.TotalLineCount() > 0 {
		meta = fmt.Sprintf("Line %d/%d", minInt(m.responseView.YOffset+1, maxInt(1, m.responseView.TotalLineCount())), maxInt(1, m.responseView.TotalLineCount()))
	}
	return strings.Join([]string{title, meta, "", m.responseView.View()}, "\n")
}

func (m *model) renderAuthSummary(auth config.AuthConfig) string {
	switch auth.Type {
	case config.AuthTypeBearer:
		if strings.TrimSpace(auth.Token) == "" {
			return "bearer"
		}
		return "bearer (" + auth.Token + ")"
	case config.AuthTypeBasic:
		if strings.TrimSpace(auth.Username) == "" {
			return "basic"
		}
		return "basic (" + auth.Username + ")"
	case "", config.AuthTypeNone:
		return "none"
	default:
		return auth.Type
	}
}

func (m *model) renderBodySummary(body config.BodyConfig) []string {
	if body.Mode == "" || body.Mode == config.BodyModeNone {
		return []string{"mode: none"}
	}

	lines := []string{"mode: " + body.Mode}
	content := strings.TrimSpace(body.Content)
	if content == "" {
		return lines
	}

	lines = append(lines, "preview:")
	previewLines := strings.Split(content, "\n")
	if len(previewLines) > 6 {
		previewLines = previewLines[:6]
		previewLines = append(previewLines, "... (edit .curler.yaml for the full body)")
	}
	for _, line := range previewLines {
		lines = append(lines, "  "+line)
	}
	return lines
}

func (m *model) resize() {
	leftWidth := maxInt(28, m.width/4)
	rightWidth := maxInt(50, m.width-leftWidth-8)
	summaryHeight := maxInt(12, m.height/3)
	responseHeight := maxInt(8, m.height-summaryHeight-10)

	m.responseView.Width = maxInt(20, rightWidth-4)
	m.responseView.Height = maxInt(4, responseHeight-4)
}

func (m *model) refreshRefs() {
	m.refs = m.file.ListRefs()
	if len(m.refs) == 0 {
		m.selected = 0
		return
	}
	if m.selected >= len(m.refs) {
		m.selected = len(m.refs) - 1
	}
}

func (m *model) runSelected() (tea.Cmd, error) {
	if len(m.refs) == 0 {
		return nil, fmt.Errorf("no saved requests found")
	}

	_, _, request, err := m.file.GetRequest(m.currentRef())
	if err != nil {
		return nil, err
	}
	env, err := m.file.Environment(m.file.ActiveEnv)
	if err != nil {
		return nil, err
	}
	resolved, err := executor.PrepareRequest(request, env, executor.OverrideOptions{})
	if err != nil {
		return nil, err
	}

	return func() tea.Msg {
		response, err := m.run(context.Background(), resolved)
		if err != nil {
			return runResultMsg{err: err}
		}
		return runResultMsg{
			response: response,
			rendered: format.RenderResponse(response),
		}
	}, nil
}

func (m *model) onSelectionChanged() {
	m.resetResponse(m.responsePlaceholder())
	m.status = "selected " + m.currentRef() + " • press enter or ctrl+r to run • edit .curler.yaml to modify it"
}

func (m *model) currentRef() string {
	if len(m.refs) == 0 || m.selected < 0 || m.selected >= len(m.refs) {
		return ""
	}
	return m.refs[m.selected]
}

func (m *model) responsePlaceholder() string {
	if len(m.refs) == 0 {
		return "No requests available.\n\nEdit .curler.yaml to add requests, then reopen curler."
	}
	return fmt.Sprintf(
		"Selected %s.\n\nThis screen is read-only.\nEdit %s to change requests.\n\nPress Enter or Ctrl+R to run the request.\nUse Tab to focus this pane, then arrows, PgUp/PgDn, or the mouse wheel to scroll the response.",
		m.currentRef(),
		m.path,
	)
}

func (m *model) resetResponse(content string) {
	m.responseView.SetContent(content)
	m.responseView.GotoTop()
}

func mapToLines(values map[string]string, separator string) []string {
	if len(values) == 0 {
		return []string{"(none)"}
	}

	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	lines := make([]string, 0, len(keys))
	for _, key := range keys {
		lines = append(lines, key+separator+values[key])
	}
	return lines
}

func indentedLines(lines []string) []string {
	if len(lines) == 0 {
		return []string{"  (none)"}
	}
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		result = append(result, "  "+line)
	}
	return result
}

func panelStyle(width, height int) lipgloss.Style {
	style := lipgloss.NewStyle().
		Width(width).
		Border(lipgloss.NormalBorder()).
		Padding(0, 1)
	if height > 0 {
		style = style.Height(height)
	}
	return style
}

func (m *model) panelHeader(title string, focused bool) string {
	if focused {
		return title + " [focused]"
	}
	return title
}

func maxInt(values ...int) int {
	best := values[0]
	for _, value := range values[1:] {
		if value > best {
			best = value
		}
	}
	return best
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

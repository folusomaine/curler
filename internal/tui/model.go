package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"curler/internal/config"
	"curler/internal/executor"
	"curler/internal/format"
)

const (
	fieldActiveEnv = iota
	fieldCollection
	fieldName
	fieldMethod
	fieldURL
	fieldTimeout
	fieldAuthType
	fieldAuthUser
	fieldAuthPass
	fieldAuthToken
	fieldBodyMode
	fieldHeaders
	fieldQuery
	fieldBody
	fieldCount
)

type runnerFunc func(context.Context, *executor.ResolvedRequest) (*executor.Response, error)

type model struct {
	path   string
	file   *config.File
	run    runnerFunc
	refs   []string
	width  int
	height int

	listFocused bool
	focusIndex  int
	selected    int

	activeEnvInput  textinput.Model
	collectionInput textinput.Model
	nameInput       textinput.Model
	methodInput     textinput.Model
	urlInput        textinput.Model
	timeoutInput    textinput.Model
	authTypeInput   textinput.Model
	authUserInput   textinput.Model
	authPassInput   textinput.Model
	authTokenInput  textinput.Model
	bodyModeInput   textinput.Model
	headersInput    textarea.Model
	queryInput      textarea.Model
	bodyInput       textarea.Model
	responseView    viewport.Model

	originalCollection string
	originalRequest    string
	status             string
	busy               bool
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
		path:        path,
		file:        file,
		run:         runner,
		listFocused: true,
		status:      "press n to create a request, enter to edit, ctrl+r to run, ctrl+s to save",
	}

	m.activeEnvInput = newTextInput("Active env", file.ActiveEnv)
	m.collectionInput = newTextInput("Collection", "")
	m.nameInput = newTextInput("Request name", "")
	m.methodInput = newTextInput("Method", "GET")
	m.urlInput = newTextInput("URL", "")
	m.timeoutInput = newTextInput("Timeout", config.DefaultTimeout)
	m.authTypeInput = newTextInput("Auth type", config.AuthTypeNone)
	m.authUserInput = newTextInput("Basic user", "")
	m.authPassInput = newTextInput("Basic pass", "")
	m.authTokenInput = newTextInput("Bearer token", "")
	m.bodyModeInput = newTextInput("Body mode", config.BodyModeNone)

	m.headersInput = newTextarea("Headers (Key: Value)")
	m.queryInput = newTextarea("Query (key=value)")
	m.bodyInput = newTextarea("Body")
	m.responseView = viewport.New(0, 0)
	m.responseView.SetContent("Run a request to view the response here.")

	m.refreshRefs()
	if len(m.refs) > 0 {
		m.loadSelectedRequest()
	} else {
		m.loadDraft("", "", &config.RequestDef{})
	}
	m.blurAllFields()
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
	case runResultMsg:
		m.busy = false
		if msg.err != nil {
			m.status = "request failed: " + msg.err.Error()
			return m, nil
		}
		m.responseView.SetContent(msg.rendered)
		m.status = fmt.Sprintf("request complete: %s", msg.response.Status)
		return m, nil
	}

	var cmd tea.Cmd
	if !m.listFocused {
		cmd = m.updateFocusedField(msg)
	}
	return m, cmd
}

func (m *model) View() string {
	if m.width == 0 {
		return "Loading curler..."
	}

	leftWidth := maxInt(24, m.width/4)
	rightWidth := m.width - leftWidth - 4
	if rightWidth < 40 {
		rightWidth = 40
		leftWidth = maxInt(20, m.width-rightWidth-4)
	}

	left := lipgloss.NewStyle().
		Width(leftWidth).
		Border(lipgloss.NormalBorder()).
		Padding(0, 1).
		Render(m.renderList())

	editor := lipgloss.NewStyle().
		Width(rightWidth).
		Border(lipgloss.NormalBorder()).
		Padding(0, 1).
		Render(m.renderEditor())

	response := lipgloss.NewStyle().
		Width(rightWidth).
		Height(maxInt(8, m.height/3)).
		Border(lipgloss.NormalBorder()).
		Padding(0, 1).
		Render("Response\n\n" + m.responseView.View())

	right := lipgloss.JoinVertical(lipgloss.Left, editor, response)
	help := lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Render(
		"q quit • enter edit • esc list • tab next field • shift+tab prev field • ctrl+s save • ctrl+r run • n new request",
	)
	status := lipgloss.NewStyle().Bold(true).Render(m.status)

	return lipgloss.JoinVertical(lipgloss.Left,
		lipgloss.JoinHorizontal(lipgloss.Top, left, right),
		"",
		status,
		help,
	)
}

func (m *model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "ctrl+s":
		if err := m.saveCurrent(); err != nil {
			m.status = "save failed: " + err.Error()
			return m, nil
		}
		m.status = "saved " + m.currentRef()
		return m, nil
	case "ctrl+r":
		if m.busy {
			return m, nil
		}
		cmd, err := m.runCurrent()
		if err != nil {
			m.status = "run failed: " + err.Error()
			return m, nil
		}
		m.busy = true
		m.status = "sending request..."
		return m, cmd
	case "n":
		m.listFocused = false
		m.focusIndex = fieldCollection
		m.loadDraft("", "", &config.RequestDef{})
		m.focusField()
		m.status = "new request draft"
		return m, nil
	case "esc":
		m.listFocused = true
		m.blurAllFields()
		m.status = "list focused"
		return m, nil
	}

	if m.listFocused {
		switch msg.String() {
		case "up", "k":
			if m.selected > 0 {
				m.selected--
				m.loadSelectedRequest()
			}
		case "down", "j":
			if m.selected < len(m.refs)-1 {
				m.selected++
				m.loadSelectedRequest()
			}
		case "enter", "right", "l":
			m.listFocused = false
			m.focusIndex = fieldMethod
			m.focusField()
			m.status = "editor focused"
		}
		return m, nil
	}

	switch msg.String() {
	case "tab":
		m.focusIndex = (m.focusIndex + 1) % fieldCount
		m.focusField()
		return m, nil
	case "shift+tab":
		m.focusIndex--
		if m.focusIndex < 0 {
			m.focusIndex = fieldCount - 1
		}
		m.focusField()
		return m, nil
	}

	return m, m.updateFocusedField(msg)
}

func (m *model) updateFocusedField(msg tea.Msg) tea.Cmd {
	switch m.focusIndex {
	case fieldActiveEnv:
		var cmd tea.Cmd
		m.activeEnvInput, cmd = m.activeEnvInput.Update(msg)
		return cmd
	case fieldCollection:
		var cmd tea.Cmd
		m.collectionInput, cmd = m.collectionInput.Update(msg)
		return cmd
	case fieldName:
		var cmd tea.Cmd
		m.nameInput, cmd = m.nameInput.Update(msg)
		return cmd
	case fieldMethod:
		var cmd tea.Cmd
		m.methodInput, cmd = m.methodInput.Update(msg)
		return cmd
	case fieldURL:
		var cmd tea.Cmd
		m.urlInput, cmd = m.urlInput.Update(msg)
		return cmd
	case fieldTimeout:
		var cmd tea.Cmd
		m.timeoutInput, cmd = m.timeoutInput.Update(msg)
		return cmd
	case fieldAuthType:
		var cmd tea.Cmd
		m.authTypeInput, cmd = m.authTypeInput.Update(msg)
		return cmd
	case fieldAuthUser:
		var cmd tea.Cmd
		m.authUserInput, cmd = m.authUserInput.Update(msg)
		return cmd
	case fieldAuthPass:
		var cmd tea.Cmd
		m.authPassInput, cmd = m.authPassInput.Update(msg)
		return cmd
	case fieldAuthToken:
		var cmd tea.Cmd
		m.authTokenInput, cmd = m.authTokenInput.Update(msg)
		return cmd
	case fieldBodyMode:
		var cmd tea.Cmd
		m.bodyModeInput, cmd = m.bodyModeInput.Update(msg)
		return cmd
	case fieldHeaders:
		var cmd tea.Cmd
		m.headersInput, cmd = m.headersInput.Update(msg)
		return cmd
	case fieldQuery:
		var cmd tea.Cmd
		m.queryInput, cmd = m.queryInput.Update(msg)
		return cmd
	case fieldBody:
		var cmd tea.Cmd
		m.bodyInput, cmd = m.bodyInput.Update(msg)
		return cmd
	default:
		return nil
	}
}

func (m *model) renderList() string {
	var lines []string
	lines = append(lines, "Requests")
	lines = append(lines, "")
	if len(m.refs) == 0 {
		lines = append(lines, "No saved requests yet.")
		lines = append(lines, "Press n to create one.")
		return strings.Join(lines, "\n")
	}

	for index, ref := range m.refs {
		prefix := "  "
		if index == m.selected {
			prefix = "▸ "
		}
		lines = append(lines, prefix+ref)
	}

	focus := "form"
	if m.listFocused {
		focus = "list"
	}
	lines = append(lines, "", "Focus: "+focus)
	lines = append(lines, "Active env: "+m.activeEnvInput.Value())
	return strings.Join(lines, "\n")
}

func (m *model) renderEditor() string {
	sections := []string{
		"Editor",
		"",
		m.activeEnvInput.View(),
		m.collectionInput.View(),
		m.nameInput.View(),
		m.methodInput.View(),
		m.urlInput.View(),
		m.timeoutInput.View(),
		m.authTypeInput.View(),
		m.authUserInput.View(),
		m.authPassInput.View(),
		m.authTokenInput.View(),
		m.bodyModeInput.View(),
		"",
		m.headersInput.View(),
		"",
		m.queryInput.View(),
		"",
		m.bodyInput.View(),
	}
	return strings.Join(sections, "\n")
}

func (m *model) resize() {
	rightWidth := maxInt(40, m.width-maxInt(24, m.width/4)-8)
	for _, input := range []*textinput.Model{
		&m.activeEnvInput,
		&m.collectionInput,
		&m.nameInput,
		&m.methodInput,
		&m.urlInput,
		&m.timeoutInput,
		&m.authTypeInput,
		&m.authUserInput,
		&m.authPassInput,
		&m.authTokenInput,
		&m.bodyModeInput,
	} {
		input.Width = maxInt(20, rightWidth-4)
	}
	for _, input := range []*textarea.Model{
		&m.headersInput,
		&m.queryInput,
		&m.bodyInput,
	} {
		input.SetWidth(maxInt(20, rightWidth-6))
	}
	m.bodyInput.SetHeight(maxInt(6, m.height/6))
	m.headersInput.SetHeight(4)
	m.queryInput.SetHeight(4)
	m.responseView.Width = maxInt(20, rightWidth-6)
	m.responseView.Height = maxInt(6, m.height/3-4)
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

func (m *model) loadSelectedRequest() {
	if len(m.refs) == 0 {
		m.loadDraft("", "", &config.RequestDef{})
		return
	}
	collectionName, requestName, request, err := m.file.GetRequest(m.refs[m.selected])
	if err != nil {
		m.status = "failed to load request: " + err.Error()
		return
	}
	m.loadDraft(collectionName, requestName, request)
}

func (m *model) loadDraft(collectionName, requestName string, request *config.RequestDef) {
	request.Normalize()
	m.originalCollection = collectionName
	m.originalRequest = requestName

	m.activeEnvInput.SetValue(m.file.ActiveEnv)
	m.collectionInput.SetValue(collectionName)
	m.nameInput.SetValue(requestName)
	m.methodInput.SetValue(request.Method)
	m.urlInput.SetValue(request.URL)
	m.timeoutInput.SetValue(request.Timeout)
	m.authTypeInput.SetValue(request.Auth.Type)
	m.authUserInput.SetValue(request.Auth.Username)
	m.authPassInput.SetValue(request.Auth.Password)
	m.authTokenInput.SetValue(request.Auth.Token)
	m.bodyModeInput.SetValue(request.Body.Mode)
	m.headersInput.SetValue(formatPairs(request.Headers, ": "))
	m.queryInput.SetValue(formatPairs(request.Query, "="))
	m.bodyInput.SetValue(request.Body.Content)
}

func (m *model) currentDraft() (string, string, *config.RequestDef, error) {
	collectionName := strings.TrimSpace(m.collectionInput.Value())
	requestName := strings.TrimSpace(m.nameInput.Value())
	if collectionName == "" {
		return "", "", nil, fmt.Errorf("collection is required")
	}
	if requestName == "" {
		return "", "", nil, fmt.Errorf("request name is required")
	}
	activeEnv := strings.TrimSpace(m.activeEnvInput.Value())
	if _, ok := m.file.Environments[activeEnv]; !ok {
		return "", "", nil, fmt.Errorf("environment %q does not exist", activeEnv)
	}

	headers, err := parsePairs(m.headersInput.Value(), []string{":", "="})
	if err != nil {
		return "", "", nil, fmt.Errorf("headers: %w", err)
	}
	query, err := parsePairs(m.queryInput.Value(), []string{"="})
	if err != nil {
		return "", "", nil, fmt.Errorf("query: %w", err)
	}

	request := &config.RequestDef{
		Method:  strings.TrimSpace(m.methodInput.Value()),
		URL:     strings.TrimSpace(m.urlInput.Value()),
		Headers: headers,
		Query:   query,
		Auth: config.AuthConfig{
			Type:     strings.TrimSpace(strings.ToLower(m.authTypeInput.Value())),
			Username: m.authUserInput.Value(),
			Password: m.authPassInput.Value(),
			Token:    m.authTokenInput.Value(),
		},
		Body: config.BodyConfig{
			Mode:    strings.TrimSpace(strings.ToLower(m.bodyModeInput.Value())),
			Content: m.bodyInput.Value(),
		},
		Timeout: strings.TrimSpace(m.timeoutInput.Value()),
	}
	request.Normalize()
	if err := config.ValidateAuthType(request.Auth.Type); err != nil {
		return "", "", nil, err
	}
	if err := config.ValidateBodyMode(request.Body.Mode); err != nil {
		return "", "", nil, err
	}
	return collectionName, requestName, request, nil
}

func (m *model) saveCurrent() error {
	collectionName, requestName, request, err := m.currentDraft()
	if err != nil {
		return err
	}

	if m.originalCollection != "" && m.originalRequest != "" &&
		(m.originalCollection != collectionName || m.originalRequest != requestName) {
		m.file.DeleteRequest(m.originalCollection, m.originalRequest)
	}
	m.file.ActiveEnv = strings.TrimSpace(m.activeEnvInput.Value())
	m.file.UpsertRequest(collectionName, requestName, request)
	if err := config.SavePath(m.path, m.file); err != nil {
		return err
	}

	m.refreshRefs()
	m.selectRef(collectionName + "/" + requestName)
	m.originalCollection = collectionName
	m.originalRequest = requestName
	return nil
}

func (m *model) runCurrent() (tea.Cmd, error) {
	_, _, request, err := m.currentDraft()
	if err != nil {
		return nil, err
	}
	envName := strings.TrimSpace(m.activeEnvInput.Value())
	env, err := m.file.Environment(envName)
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

func (m *model) focusField() {
	m.blurAllFields()
	switch m.focusIndex {
	case fieldActiveEnv:
		m.activeEnvInput.Focus()
	case fieldCollection:
		m.collectionInput.Focus()
	case fieldName:
		m.nameInput.Focus()
	case fieldMethod:
		m.methodInput.Focus()
	case fieldURL:
		m.urlInput.Focus()
	case fieldTimeout:
		m.timeoutInput.Focus()
	case fieldAuthType:
		m.authTypeInput.Focus()
	case fieldAuthUser:
		m.authUserInput.Focus()
	case fieldAuthPass:
		m.authPassInput.Focus()
	case fieldAuthToken:
		m.authTokenInput.Focus()
	case fieldBodyMode:
		m.bodyModeInput.Focus()
	case fieldHeaders:
		m.headersInput.Focus()
	case fieldQuery:
		m.queryInput.Focus()
	case fieldBody:
		m.bodyInput.Focus()
	}
}

func (m *model) blurAllFields() {
	m.activeEnvInput.Blur()
	m.collectionInput.Blur()
	m.nameInput.Blur()
	m.methodInput.Blur()
	m.urlInput.Blur()
	m.timeoutInput.Blur()
	m.authTypeInput.Blur()
	m.authUserInput.Blur()
	m.authPassInput.Blur()
	m.authTokenInput.Blur()
	m.bodyModeInput.Blur()
	m.headersInput.Blur()
	m.queryInput.Blur()
	m.bodyInput.Blur()
}

func (m *model) selectRef(ref string) {
	for index, item := range m.refs {
		if item == ref {
			m.selected = index
			m.loadSelectedRequest()
			return
		}
	}
}

func (m *model) currentRef() string {
	collectionName := strings.TrimSpace(m.collectionInput.Value())
	requestName := strings.TrimSpace(m.nameInput.Value())
	if collectionName == "" || requestName == "" {
		return "(unsaved request)"
	}
	return collectionName + "/" + requestName
}

func newTextInput(placeholder, value string) textinput.Model {
	input := textinput.New()
	input.Placeholder = placeholder
	input.SetValue(value)
	input.CharLimit = 2048
	return input
}

func newTextarea(placeholder string) textarea.Model {
	input := textarea.New()
	input.Placeholder = placeholder
	input.ShowLineNumbers = false
	input.SetHeight(4)
	return input
}

func formatPairs(values map[string]string, separator string) string {
	if len(values) == 0 {
		return ""
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	lines := make([]string, 0, len(keys))
	for _, key := range keys {
		lines = append(lines, fmt.Sprintf("%s%s%s", key, separator, values[key]))
	}
	return strings.Join(lines, "\n")
}

func parsePairs(value string, separators []string) (map[string]string, error) {
	result := map[string]string{}
	lines := strings.Split(value, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		found := false
		for _, separator := range separators {
			if index := strings.Index(line, separator); index >= 0 {
				key := strings.TrimSpace(line[:index])
				resolvedValue := strings.TrimSpace(line[index+len(separator):])
				if key == "" {
					return nil, fmt.Errorf("missing key in %q", line)
				}
				result[key] = resolvedValue
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("invalid pair %q", line)
		}
	}
	return result, nil
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

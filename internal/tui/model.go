package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/charlesolinsky/local-llm/internal/activity"
	"github.com/charlesolinsky/local-llm/internal/config"
)

type panel int

const (
	panelLogs panel = iota
	panelModels
	panelChat
)

type model struct {
	store  *config.Store
	ctrl   *controller
	client *apiClient
	width  int
	height int
	ready  bool

	panel      panel
	modelIdx   int
	aliasNames []string
	lib        []config.LibraryEntry
	rows       []libRow
	rowIdx     int
	installed  []string

	gatewayUp bool
	ollamaUp  bool
	owned     bool
	busy      bool
	showKey   bool
	status    string
	notices   []string

	logView  viewport.Model
	logText  string
	logSize  int64
	follow   bool
	listView viewport.Model

	chatView viewport.Model
	messages []chatMessage
	input    textinput.Model
	spinner  spinner.Model
	sending  bool
}

type snapshotMsg struct {
	gateway   bool
	ollama    bool
	owned     bool
	logs      string
	size      int64
	installed []string
}

type toggleDoneMsg struct {
	started bool
	err     error
}

type chatResultMsg struct {
	content string
	err     error
}

type tickMsg struct{}

type modelOpMsg struct {
	op  string
	err error
}

func newModel(store *config.Store, ctrl *controller) model {
	cfg := store.Snapshot()
	ti := textinput.New()
	ti.Placeholder = "optional test prompt…"
	ti.CharLimit = 2000
	ti.Prompt = "› "
	ti.PromptStyle = styleAccent
	ti.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	ti.Cursor.Style = styleAccent

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = styleAccent

	return model{
		store:      store,
		ctrl:       ctrl,
		client:     newAPIClient(&cfg),
		panel:      panelLogs,
		aliasNames: cfg.AliasNames(),
		lib:        config.LoadLibrary(store.Path()),
		follow:     true,
		input:      ti,
		spinner:    sp,
		status:     "idle",
		logView:    viewport.New(80, 10),
		listView:   viewport.New(80, 10),
		chatView:   viewport.New(80, 6),
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(takeSnapshot(m.ctrl), scheduleTick(), textinput.Blink)
}

func scheduleTick() tea.Cmd {
	return tea.Tick(750*time.Millisecond, func(time.Time) tea.Msg { return tickMsg{} })
}

func takeSnapshot(ctrl *controller) tea.Cmd {
	return func() tea.Msg {
		logs, _ := activity.TailFile(activity.DefaultPath(), 48<<10)
		tags, _ := listOllamaTags(ctrl.snap().OllamaBase)
		return snapshotMsg{
			gateway:   ctrl.gatewayUp(),
			ollama:    ctrl.ollamaUp(),
			owned:     ctrl.owned(),
			logs:      logs,
			size:      activity.FileSize(activity.DefaultPath()),
			installed: tags,
		}
	}
}

func runToggle(ctrl *controller, start bool) tea.Cmd {
	return func() tea.Msg {
		var err error
		if start {
			err = ctrl.start()
		} else {
			err = ctrl.stop()
		}
		return toggleDoneMsg{started: start, err: err}
	}
}

func sendChat(c *apiClient, modelName, prompt string) tea.Cmd {
	return func() tea.Msg {
		content, err := c.chat(modelName, prompt)
		return chatResultMsg{content: content, err: err}
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.ready = true
		m.layout()
		m.redrawLogs()
		m.redrawList()
		m.redrawChat()
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case tickMsg:
		return m, tea.Batch(takeSnapshot(m.ctrl), scheduleTick())

	case snapshotMsg:
		changed := msg.gateway != m.gatewayUp || msg.ollama != m.ollamaUp ||
			msg.owned != m.owned || msg.size != m.logSize || msg.logs != m.logText
		m.gatewayUp = msg.gateway
		m.ollamaUp = msg.ollama
		m.owned = msg.owned
		if msg.installed != nil {
			m.installed = msg.installed
			m.rows = buildLibraryRows(m.lib, m.installed, m.store.Snapshot())
			if m.rowIdx >= len(m.rows) && len(m.rows) > 0 {
				m.rowIdx = len(m.rows) - 1
			}
			m.redrawList()
		}
		if !m.busy {
			switch {
			case m.gatewayUp && m.ollamaUp:
				m.status = "running"
			case m.gatewayUp:
				m.status = "gateway up, ollama down"
			default:
				m.status = "stopped"
			}
		}
		if msg.logs != m.logText || msg.size != m.logSize {
			m.logText = msg.logs
			m.logSize = msg.size
			m.redrawLogs()
		}
		if !changed {
			return m, nil
		}
		return m, nil

	case modelOpMsg:
		m.busy = false
		if msg.err != nil {
			m.status = msg.err.Error()
			m.note(msg.err.Error())
		} else {
			m.note(msg.op)
			m.status = "ready"
		}
		m.aliasNames = m.store.Snapshot().AliasNames()
		return m, takeSnapshot(m.ctrl)

	case toggleDoneMsg:
		m.busy = false
		if msg.err != nil {
			m.status = msg.err.Error()
			m.note(msg.err.Error())
			return m, takeSnapshot(m.ctrl)
		}
		if msg.started {
			m.status = "running"
			m.note("gateway started")
		} else {
			m.status = "stopped"
			m.note("gateway stopped")
		}
		return m, takeSnapshot(m.ctrl)

	case chatResultMsg:
		m.sending = false
		if msg.err != nil {
			m.messages = append(m.messages, chatMessage{Role: "error", Content: msg.err.Error()})
		} else {
			m.messages = append(m.messages, chatMessage{Role: "assistant", Content: msg.content})
		}
		m.redrawChat()
		return m, nil

	case spinner.TickMsg:
		if !m.busy && !m.sending {
			return m, nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		if m.sending {
			m.redrawChat()
		}
		return m, cmd
	}

	if m.panel == panelChat {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	var cmd tea.Cmd
	m.logView, cmd = m.logView.Update(msg)
	return m, cmd
}

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	typing := m.panel == panelChat && m.input.Focused()

	switch key {
	case "ctrl+c":
		return m, tea.Quit
	case "q":
		if typing && m.input.Value() != "" {
			break
		}
		return m, tea.Quit
	case " ":
		if typing {
			break
		}
		if m.busy {
			return m, nil
		}
		m.busy = true
		if m.gatewayUp {
			m.status = "stopping"
			m.note("stopping gateway…")
			return m, tea.Batch(m.spinner.Tick, runToggle(m.ctrl, false))
		}
		m.status = "starting"
		m.note("starting gateway…")
		return m, tea.Batch(m.spinner.Tick, runToggle(m.ctrl, true))
	case "tab":
		switch m.panel {
		case panelLogs:
			m.panel = panelModels
			m.input.Blur()
		case panelModels:
			m.panel = panelChat
			m.input.Focus()
		default:
			m.panel = panelLogs
			m.input.Blur()
		}
		m.layout()
		m.redrawLogs()
		m.redrawList()
		m.redrawChat()
		return m, nil
	case "1":
		if typing {
			break
		}
		return m.setPanel(panelLogs)
	case "2":
		if typing {
			break
		}
		return m.setPanel(panelModels)
	case "3":
		if typing {
			break
		}
		return m.setPanel(panelChat)
	case "?":
		if typing {
			break
		}
		// help is always visible; stay put
		return m, nil
	case "k", "K":
		if !typing || m.input.Value() == "" {
			m.showKey = !m.showKey
			return m, nil
		}
	case "f", "F":
		if !typing {
			m.follow = !m.follow
			if m.follow {
				m.logView.GotoBottom()
			}
			return m, nil
		}
	case "up":
		if m.panel == panelModels && len(m.rows) > 0 {
			m.rowIdx--
			if m.rowIdx < 0 {
				m.rowIdx = len(m.rows) - 1
			}
			m.redrawList()
			return m, nil
		}
	case "down":
		if m.panel == panelModels && len(m.rows) > 0 {
			m.rowIdx = (m.rowIdx + 1) % len(m.rows)
			m.redrawList()
			return m, nil
		}
	case "i":
		if typing {
			break
		}
		if m.panel == panelModels {
			return m.startInstall()
		}
	case "x":
		if typing {
			break
		}
		if m.panel == panelModels {
			return m.startUninstall()
		}
	case "[":
		if typing {
			break
		}
		return m.bumpContext(-1)
	case "]":
		if typing {
			break
		}
		return m.bumpContext(1)
	case "enter":
		if m.panel == panelModels {
			return m.setActiveRow()
		}
		if m.panel != panelChat || m.sending {
			return m, nil
		}
		prompt := strings.TrimSpace(m.input.Value())
		if prompt == "" || len(m.aliasNames) == 0 {
			return m, nil
		}
		if !m.gatewayUp {
			m.messages = append(m.messages, chatMessage{Role: "error", Content: "gateway is stopped — press space to start"})
			m.redrawChat()
			return m, nil
		}
		alias := m.store.Snapshot().Active
		if alias == "" && len(m.aliasNames) > 0 {
			alias = m.aliasNames[0]
		}
		m.messages = append(m.messages, chatMessage{Role: "user", Content: prompt})
		m.input.SetValue("")
		m.sending = true
		m.redrawChat()
		return m, tea.Batch(m.spinner.Tick, sendChat(m.client, alias, prompt))
	}

	if m.panel == panelChat {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	if m.panel == panelModels {
		var cmd tea.Cmd
		m.listView, cmd = m.listView.Update(msg)
		return m, cmd
	}
	if key == "pgup" || key == "pgdown" || key == "up" || key == "down" || key == "home" || key == "end" {
		m.follow = false
	}
	var cmd tea.Cmd
	m.logView, cmd = m.logView.Update(msg)
	return m, cmd
}

func (m model) setPanel(p panel) (tea.Model, tea.Cmd) {
	m.panel = p
	if p == panelChat {
		m.input.Focus()
	} else {
		m.input.Blur()
	}
	m.layout()
	m.redrawLogs()
	m.redrawList()
	m.redrawChat()
	return m, nil
}

func (m *model) note(s string) {
	line := time.Now().Format("15:04:05") + "  tui    " + s
	m.notices = append(m.notices, line)
	if len(m.notices) > 20 {
		m.notices = m.notices[len(m.notices)-20:]
	}
	m.redrawLogs()
}

func (m *model) innerWidth() int {
	w := m.width - 2
	if w < 20 {
		return 20
	}
	return w
}

func (m *model) chromeHeight() int {
	// header + meta + tab row (2 lines with underline) + help
	h := 5
	if m.panel == panelChat {
		h += 1
	}
	return h
}

func (m *model) layout() {
	inner := m.innerWidth()
	body := m.height - m.chromeHeight()
	if body < 3 {
		body = 3
	}
	m.logView.Width = inner
	m.logView.Height = body
	m.listView.Width = inner
	m.listView.Height = body
	m.chatView.Width = inner
	m.chatView.Height = body
	iw := inner - 2
	if iw < 8 {
		iw = 8
	}
	m.input.Width = iw
}

func (m *model) redrawLogs() {
	var b strings.Builder
	if len(m.notices) > 0 {
		for _, n := range m.notices {
			b.WriteString(styleMuted.Render(n))
			b.WriteString("\n")
		}
	}
	text := strings.TrimRight(m.logText, "\n")
	if text == "" && len(m.notices) == 0 {
		b.WriteString(styleMuted.Render("No activity yet. Press space to start the gateway."))
	} else if text != "" {
		for i, line := range strings.Split(text, "\n") {
			if i > 0 {
				b.WriteString("\n")
			}
			b.WriteString(styleLogLine(line))
		}
	}
	atBottom := m.logView.AtBottom()
	m.logView.SetContent(b.String())
	if m.follow || atBottom {
		m.logView.GotoBottom()
	}
}

func (m *model) redrawChat() {
	inner := m.innerWidth()
	wrap := lipgloss.NewStyle().Width(inner)
	var b strings.Builder
	if len(m.messages) == 0 {
		b.WriteString(styleMuted.Render("Smoke-test only. Press enter to send."))
	}
	for i, msg := range m.messages {
		if i > 0 {
			b.WriteString("\n")
		}
		switch msg.Role {
		case "user":
			b.WriteString(styleYou.Render("you  "))
			b.WriteString(wrap.Render(msg.Content))
		case "assistant":
			name := m.store.Snapshot().Active
			if name == "" {
				name = "model"
			}
			b.WriteString(styleBot.Render(name + "  "))
			b.WriteString(wrap.Render(msg.Content))
		case "error":
			b.WriteString(styleErr.Render("error  " + msg.Content))
		}
	}
	if m.sending {
		b.WriteString("\n" + m.spinner.View() + " " + styleMuted.Render("generating"))
	}
	m.chatView.SetContent(b.String())
	m.chatView.GotoBottom()
}

func (m *model) redrawList() {
	inner := m.innerWidth()
	m.listView.SetContent(m.renderLibrary(inner))
	// Keep the selected row in view.
	if m.rowIdx >= 0 {
		m.listView.SetYOffset(max(0, m.rowIdx-1))
	}
}

func (m model) View() string {
	if !m.ready {
		return "starting…"
	}
	inner := m.innerWidth()
	var body string
	switch m.panel {
	case panelModels:
		body = m.listView.View()
	case panelChat:
		body = m.chatView.View()
	default:
		body = m.logView.View()
	}

	parts := []string{
		m.renderHeader(inner),
		m.renderMeta(inner),
		m.renderTabs(inner),
		body,
	}
	if m.panel == panelChat {
		parts = append(parts, m.input.View())
	}
	parts = append(parts, m.renderHelp(inner))

	frame := lipgloss.JoinVertical(lipgloss.Left, parts...)
	need := m.height
	if got := lipgloss.Height(frame); got < need {
		frame += strings.Repeat("\n", need-got)
	}
	return styleApp.MaxHeight(m.height).Render(frame)
}

func (m model) renderHeader(inner int) string {
	dot := func(up bool) string {
		if up {
			return styleOk.Render("●")
		}
		return styleBad.Render("●")
	}
	ol := dot(m.ollamaUp)
	if !m.ollamaUp {
		ol = styleWarn.Render("●")
	}
	src := ""
	if m.gatewayUp && m.owned {
		src = styleMuted.Render("  local")
	} else if m.gatewayUp {
		src = styleMuted.Render("  ext")
	}
	left := styleTitle.Render("local-llm") + "  " +
		dot(m.gatewayUp) + styleMuted.Render(" gw  ") +
		ol + styleMuted.Render(" ollama") + src
	right := m.statusText()
	gap := inner - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

func (m model) statusText() string {
	if m.busy {
		return m.spinner.View() + " " + styleMuted.Render(m.status)
	}
	switch {
	case m.status == "running":
		return styleOk.Render(m.status)
	case strings.Contains(m.status, "down") || strings.Contains(m.status, "error"):
		return styleBad.Render(m.status)
	case m.status == "stopped" || m.status == "idle":
		return styleMuted.Render(m.status)
	default:
		return styleMuted.Render(m.status)
	}
}

func (m model) renderMeta(inner int) string {
	cfg := m.store.Snapshot()
	port := listenPort(cfg.Listen)
	lanURL := "http://<lan-ip>:" + port + "/v1"
	if ip := lanIPv4(); ip != "" {
		lanURL = "http://" + ip + ":" + port + "/v1"
	}
	key := maskKey(cfg.APIKey)
	if m.showKey {
		key = cfg.APIKey
	}
	line := styleAccent.Render(lanURL) +
		styleMuted.Render("  ·  ") + styleMuted.Render(key) +
		styleMuted.Render("  ·  ") + styleAccent.Render(cfg.Active) +
		styleMuted.Render(fmt.Sprintf("  ctx %d", cfg.ContextLength))
	return lipgloss.NewStyle().MaxWidth(inner).Render(line)
}

func (m model) renderTabs(inner int) string {
	tabs := []struct {
		id    panel
		label string
	}{
		{panelLogs, "1 logs"},
		{panelModels, "2 models"},
		{panelChat, "3 test"},
	}
	var parts []string
	for _, t := range tabs {
		st := styleTab
		if t.id == m.panel {
			st = styleTabActive
		}
		parts = append(parts, st.Render(t.label))
	}
	row := lipgloss.JoinHorizontal(lipgloss.Bottom, parts...)
	gapW := inner - lipgloss.Width(row)
	if gapW < 0 {
		gapW = 0
	}
	gap := styleTabGap.Render(strings.Repeat(" ", gapW))
	return lipgloss.JoinHorizontal(lipgloss.Bottom, row, gap)
}

func (m model) renderLibrary(inner int) string {
	var b strings.Builder
	header := fmt.Sprintf("  %-8s %-22s %-8s %-10s  %s", "alias", "tag", "size", "status", "role")
	b.WriteString(styleMuted.Render(header))
	b.WriteString("\n")
	if len(m.rows) == 0 {
		b.WriteString(styleMuted.Render("  catalog empty"))
		return b.String()
	}
	for i, row := range m.rows {
		status := "available"
		stStatus := styleMuted
		if row.Installed {
			status = "installed"
			stStatus = styleOk
		}
		if row.Active {
			status = "active"
			stStatus = styleAccent
		}
		mark := " "
		if i == m.rowIdx {
			mark = "▸"
		}
		plain := fmt.Sprintf("%s %-8s %-22s %-8s ", mark, row.Alias, truncate(row.Tag, 22), row.Size)
		line := plain + stStatus.Render(fmt.Sprintf("%-10s", status)) + "  " + styleMuted.Render(row.Role)
		if i == m.rowIdx {
			line = styleRowCursor.Width(inner).Render(
				fmt.Sprintf("%s %-8s %-22s %-8s %-10s  %s", mark, row.Alias, truncate(row.Tag, 22), row.Size, status, row.Role),
			)
		}
		b.WriteString(lipgloss.NewStyle().MaxWidth(inner).Render(line))
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m model) renderHelp(inner int) string {
	help := "space start/stop   tab/1–3 pane   f follow   k key   q quit"
	switch m.panel {
	case panelModels:
		help = "↑↓ move   enter active   i install   x remove   [ ] ctx   tab pane"
	case panelChat:
		help = "enter send   tab pane   space start/stop   q quit"
	}
	return styleHelp.MaxWidth(inner).Render(help)
}

func styleLogLine(line string) string {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return styleMuted.Render(line)
	}
	ts := styleMuted.Render(fields[0])
	rest := strings.TrimPrefix(line, fields[0])
	switch {
	case fields[1] == "200" || fields[1] == "info":
		return ts + styleText.Render(rest)
	case fields[1] == "tui":
		return ts + styleMuted.Render(rest)
	case len(fields[1]) == 3 && fields[1][0] == '4':
		return ts + styleWarn.Render(rest)
	case len(fields[1]) == 3 && fields[1][0] == '5':
		return ts + styleErr.Render(rest)
	default:
		return ts + styleText.Render(rest)
	}
}

func (m model) startInstall() (tea.Model, tea.Cmd) {
	if m.busy || len(m.rows) == 0 {
		return m, nil
	}
	row := m.rows[m.rowIdx]
	if row.Installed {
		m.note(row.Alias + " already installed")
		return m, nil
	}
	if !m.ollamaUp {
		m.note("ollama is down")
		return m, nil
	}
	m.busy = true
	m.status = "installing " + row.Tag
	m.note("pulling " + row.Tag + " …")
	store := m.store
	return m, tea.Batch(m.spinner.Tick, func() tea.Msg {
		if err := ollamaPull(row.Tag); err != nil {
			return modelOpMsg{op: "install", err: err}
		}
		if err := store.UpsertModel(row.Alias, row.Tag); err != nil {
			return modelOpMsg{op: "install", err: err}
		}
		return modelOpMsg{op: "installed " + row.Alias, err: nil}
	})
}

func (m model) startUninstall() (tea.Model, tea.Cmd) {
	if m.busy || len(m.rows) == 0 {
		return m, nil
	}
	row := m.rows[m.rowIdx]
	if !row.Installed {
		m.note(row.Alias + " is not installed")
		return m, nil
	}
	cfg := m.store.Snapshot()
	if len(cfg.Models) <= 1 && cfg.Models[row.Alias].Upstream != "" {
		m.note("cannot remove the last alias")
		return m, nil
	}
	m.busy = true
	m.status = "removing " + row.Tag
	m.note("removing " + row.Tag + " …")
	store := m.store
	return m, tea.Batch(m.spinner.Tick, func() tea.Msg {
		_ = store.RemoveModel(row.Alias)
		if err := ollamaRemove(row.Tag); err != nil {
			return modelOpMsg{op: "remove", err: err}
		}
		return modelOpMsg{op: "removed " + row.Alias, err: nil}
	})
}

func (m model) setActiveRow() (tea.Model, tea.Cmd) {
	if len(m.rows) == 0 {
		return m, nil
	}
	row := m.rows[m.rowIdx]
	if !row.Installed {
		m.note("install " + row.Alias + " first (i)")
		return m, nil
	}
	if err := m.store.SetActive(row.Alias); err != nil {
		// Alias might be in library but not yet in models.yaml — add then activate.
		if err := m.store.UpsertModel(row.Alias, row.Tag); err != nil {
			m.note(err.Error())
			return m, nil
		}
		if err := m.store.SetActive(row.Alias); err != nil {
			m.note(err.Error())
			return m, nil
		}
	}
	m.aliasNames = m.store.Snapshot().AliasNames()
	m.note("active model → " + row.Alias)
	m.rows = buildLibraryRows(m.lib, m.installed, m.store.Snapshot())
	m.redrawList()
	return m, nil
}

func (m model) bumpContext(dir int) (tea.Model, tea.Cmd) {
	cfg := m.store.Snapshot()
	next := nextContext(cfg.ContextLength, dir)
	if err := m.store.SetContextLength(next); err != nil {
		m.note(err.Error())
		return m, nil
	}
	m.note(fmt.Sprintf("context length → %d", next))
	m.redrawList()
	return m, nil
}

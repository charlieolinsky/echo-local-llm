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
	panelChat
)

type model struct {
	cfg    *config.Config
	ctrl   *controller
	client *apiClient
	width  int
	height int
	ready  bool

	panel      panel
	modelIdx   int
	aliasNames []string

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

	chatView viewport.Model
	messages []chatMessage
	input    textinput.Model
	spinner  spinner.Model
	sending  bool
}

type snapshotMsg struct {
	gateway bool
	ollama  bool
	owned   bool
	logs    string
	size    int64
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

func newModel(cfg *config.Config, ctrl *controller) model {
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
		cfg:        cfg,
		ctrl:       ctrl,
		client:     newAPIClient(cfg),
		panel:      panelLogs,
		aliasNames: cfg.AliasNames(),
		follow:     true,
		input:      ti,
		spinner:    sp,
		status:     "idle",
		logView:    viewport.New(80, 10),
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
		return snapshotMsg{
			gateway: ctrl.gatewayUp(),
			ollama:  ctrl.ollamaUp(),
			owned:   ctrl.owned(),
			logs:    logs,
			size:    activity.FileSize(activity.DefaultPath()),
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
		if m.panel == panelLogs {
			m.panel = panelChat
			m.input.Focus()
		} else {
			m.panel = panelLogs
			m.input.Blur()
		}
		m.layout()
		m.redrawLogs()
		m.redrawChat()
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
		if m.panel == panelChat && !m.input.Focused() && len(m.aliasNames) > 0 {
			m.modelIdx--
			if m.modelIdx < 0 {
				m.modelIdx = len(m.aliasNames) - 1
			}
			return m, nil
		}
	case "down":
		if m.panel == panelChat && !m.input.Focused() && len(m.aliasNames) > 0 {
			m.modelIdx = (m.modelIdx + 1) % len(m.aliasNames)
			return m, nil
		}
	case "enter":
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
		alias := m.aliasNames[m.modelIdx]
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
	if key == "pgup" || key == "pgdown" || key == "up" || key == "down" || key == "home" || key == "end" {
		m.follow = false
	}
	var cmd tea.Cmd
	m.logView, cmd = m.logView.Update(msg)
	return m, cmd
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
	// title, status, url, models, rule, help  = 6
	h := 6
	if m.panel == panelChat {
		h += 2 // input + extra rule
	}
	return h
}

func (m *model) layout() {
	inner := m.innerWidth()
	body := m.height - m.chromeHeight()
	if body < 4 {
		body = 4
	}
	if m.panel == panelChat {
		chatH := body / 3
		if chatH < 4 {
			chatH = 4
		}
		if chatH > body-4 {
			chatH = body - 4
		}
		m.logView.Width = inner
		m.logView.Height = body - chatH
		m.chatView.Width = inner
		m.chatView.Height = chatH
		iw := inner - 2
		if iw < 8 {
			iw = 8
		}
		m.input.Width = iw
		return
	}
	m.logView.Width = inner
	m.logView.Height = body
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
		b.WriteString(text)
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
			name := "model"
			if len(m.aliasNames) > 0 {
				name = m.aliasNames[m.modelIdx]
			}
			b.WriteString(styleBot.Render(name+"  "))
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

func (m model) View() string {
	if !m.ready {
		return "starting…"
	}
	inner := m.innerWidth()
	rule := styleMuted.Render(strings.Repeat("─", inner))

	parts := []string{
		m.renderTitle(inner),
		m.renderStatus(inner),
		m.renderURL(inner),
		m.renderModels(inner),
		rule,
		m.logView.View(),
	}
	if m.panel == panelChat {
		alias := "—"
		if len(m.aliasNames) > 0 {
			alias = m.aliasNames[m.modelIdx]
		}
		parts = append(parts, rule, styleMuted.Render("test · "+alias), m.chatView.View(), m.input.View())
	}
	parts = append(parts, m.renderHelp(inner))

	return styleApp.MaxWidth(m.width).MaxHeight(m.height).Render(
		lipgloss.JoinVertical(lipgloss.Left, parts...),
	)
}

func (m model) renderTitle(inner int) string {
	left := styleTitle.Render("local-llm")
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
	case strings.Contains(m.status, "stopped") || m.status == "idle":
		return styleMuted.Render(m.status)
	case strings.Contains(m.status, "down") || strings.Contains(m.status, "error"):
		return styleBad.Render(m.status)
	case m.status == "running":
		return styleOk.Render(m.status)
	default:
		return styleMuted.Render(m.status)
	}
}

func (m model) renderStatus(inner int) string {
	gw := styleBad.Render("down")
	if m.gatewayUp {
		gw = styleOk.Render("up")
	}
	ol := styleBad.Render("down")
	if m.ollamaUp {
		ol = styleOk.Render("up")
	} else {
		ol = styleWarn.Render("down")
	}
	src := styleMuted.Render("external")
	if !m.gatewayUp {
		src = styleMuted.Render("—")
	} else if m.owned {
		src = styleMuted.Render("started here")
	}
	line := fmt.Sprintf("%s %s  %s %s  %s",
		styleMuted.Render("gateway"), gw,
		styleMuted.Render("ollama"), ol,
		src,
	)
	return lipgloss.NewStyle().MaxWidth(inner).Render(line)
}

func (m model) renderURL(inner int) string {
	port := listenPort(m.cfg.Listen)
	lanURL := "http://<lan-ip>:" + port + "/v1"
	if ip := lanIPv4(); ip != "" {
		lanURL = "http://" + ip + ":" + port + "/v1"
	}
	key := maskKey(m.cfg.APIKey)
	if m.showKey {
		key = m.cfg.APIKey
	}
	line := styleAccent.Render(lanURL) + "  " + styleMuted.Render("key") + " " + styleMuted.Render(key)
	return lipgloss.NewStyle().MaxWidth(inner).Render(line)
}

func (m model) renderModels(inner int) string {
	if len(m.aliasNames) == 0 {
		return styleMuted.Render("no model aliases")
	}
	var parts []string
	for i, name := range m.aliasNames {
		up := m.cfg.Models[name].Upstream
		s := name + "→" + up
		if i == m.modelIdx {
			s = styleAccent.Render(s)
		} else {
			s = styleMuted.Render(s)
		}
		parts = append(parts, s)
	}
	return lipgloss.NewStyle().MaxWidth(inner).Render(strings.Join(parts, "  "))
}

func (m model) renderHelp(inner int) string {
	follow := "follow"
	if m.follow {
		follow = "following"
	}
	help := "space start/stop  tab logs/test  f " + follow + "  k key  q quit"
	if m.panel == panelChat {
		help = "enter send  tab logs  space start/stop  q quit"
	}
	return styleHelp.MaxWidth(inner).Render(help)
}

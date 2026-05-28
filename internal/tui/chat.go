// Role:    Main TUI model for interactive chat (bubbletea Elm Architecture)
// Depends: fmt, strings, charm.land/bubbletea/v2, charm.land/bubbles/v2, charm.land/lipgloss/v2, internal/client
// Exports: Model, NewModel

package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/epiral/pinix/internal/client"
)

// chatState tracks what the TUI is doing.
type chatState int

const (
	stateIdle     chatState = iota // Waiting for user input
	stateStreaming                  // Receiving streaming response
)

// Model is the top-level bubbletea model for the chat TUI.
type Model struct {
	// Config
	client    *client.Client
	agentID   string
	agentName string
	topicID   string
	hubToken  string

	// UI components
	viewport viewport.Model
	input    textarea.Model
	spinner  spinner.Model
	renderer *Renderer

	// State
	state        chatState
	history      string // rendered message history
	streamBuf    string // current streaming text buffer
	thinkBuf     string // current thinking buffer
	lastToolName string // last tool call name (for result matching)
	session      *StreamSession
	ready        bool // viewport initialized
	width        int
	height       int
	quitting     bool
}

// NewModel creates a new chat TUI model.
func NewModel(cli *client.Client, agentID, agentName, hubToken string) Model {
	ti := textarea.New()
	ti.Placeholder = "Type a message... (Enter to send, Alt+Enter for newline)"
	ti.Focus()
	ti.SetHeight(3)
	ti.MaxHeight = 3
	ti.ShowLineNumbers = false
	// Rebind: InsertNewline on Alt+Enter, so bare Enter can be handled by us
	ti.KeyMap.InsertNewline.SetKeys("alt+enter")

	sp := spinner.New(
		spinner.WithSpinner(spinner.Dot),
		spinner.WithStyle(lipgloss.NewStyle().Foreground(lipgloss.Color("5"))),
	)

	return Model{
		client:    cli,
		agentID:   agentID,
		agentName: agentName,
		hubToken:  hubToken,
		input:     ti,
		spinner:   sp,
		state:     stateIdle,
	}
}

// TopicID returns the topic ID for display on exit.
func (m Model) TopicID() string {
	return m.topicID
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		textarea.Blink,
		m.spinner.Tick,
	)
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKey(msg)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m = m.layout()
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	// --- Stream messages ---
	case StreamTokenMsg:
		m.streamBuf += msg.Content
		m.updateViewportContent()
		m.scrollToBottom()
		if m.session != nil {
			cmds = append(cmds, m.session.NextMsg())
		}
		return m, tea.Batch(cmds...)

	case StreamThinkingMsg:
		m.thinkBuf += msg.Content
		m.updateViewportContent()
		m.scrollToBottom()
		if m.session != nil {
			cmds = append(cmds, m.session.NextMsg())
		}
		return m, tea.Batch(cmds...)

	case StreamToolCallMsg:
		if m.renderer != nil {
			m.history += m.renderer.RenderToolCall(msg.Name, msg.Args)
		}
		m.lastToolName = msg.Name
		m.updateViewportContent()
		m.scrollToBottom()
		if m.session != nil {
			cmds = append(cmds, m.session.NextMsg())
		}
		return m, tea.Batch(cmds...)

	case StreamToolResultMsg:
		name := msg.Name
		if name == "" {
			name = m.lastToolName
		}
		if m.renderer != nil {
			m.history += m.renderer.RenderToolResult(name, msg.Content)
		}
		m.updateViewportContent()
		m.scrollToBottom()
		if m.session != nil {
			cmds = append(cmds, m.session.NextMsg())
		}
		return m, tea.Batch(cmds...)

	case StreamDoneMsg:
		// Finalize the streaming buffer into history
		if m.streamBuf != "" && m.renderer != nil {
			m.history += m.renderer.RenderAssistantMessage(m.streamBuf)
		}
		m.streamBuf = ""
		m.thinkBuf = ""
		m.lastToolName = ""
		m.session = nil
		m.state = stateIdle
		m.input.Focus()
		m.updateViewportContent()
		m.scrollToBottom()
		return m, textarea.Blink

	case StreamErrorMsg:
		if m.renderer != nil {
			m.history += m.renderer.RenderError(msg.Err.Error())
		}
		m.streamBuf = ""
		m.thinkBuf = ""
		m.lastToolName = ""
		m.session = nil
		m.state = stateIdle
		m.input.Focus()
		m.updateViewportContent()
		m.scrollToBottom()
		return m, textarea.Blink
	}

	// Pass through to sub-components
	if m.state == stateIdle {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		cmds = append(cmds, cmd)
	}

	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

// handleKey processes key events.
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		if m.state == stateStreaming {
			// Cancel current stream
			if m.session != nil {
				m.session.Cancel()
			}
			if m.streamBuf != "" && m.renderer != nil {
				m.history += m.renderer.RenderAssistantMessage(m.streamBuf + "\n\n[cancelled]")
			}
			m.streamBuf = ""
			m.thinkBuf = ""
			m.lastToolName = ""
			m.session = nil
			m.state = stateIdle
			m.input.Focus()
			m.updateViewportContent()
			m.scrollToBottom()
			return m, textarea.Blink
		}
		// Quit
		m.quitting = true
		return m, tea.Quit

	case "enter":
		if m.state == stateStreaming {
			return m, nil
		}
		text := strings.TrimSpace(m.input.Value())
		if text == "" {
			return m, nil
		}
		return m.sendMessage(text)

	case "esc":
		if m.state == stateIdle {
			m.quitting = true
			return m, tea.Quit
		}
		return m, nil
	}

	// Default: pass to input
	if m.state == stateIdle {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	return m, nil
}

// sendMessage sends the user's message and starts streaming.
func (m Model) sendMessage(text string) (tea.Model, tea.Cmd) {
	// Add user message to history
	if m.renderer != nil {
		m.history += m.renderer.RenderUserMessage(text)
	}
	m.input.Reset()
	m.input.Blur()
	m.state = stateStreaming
	m.streamBuf = ""
	m.thinkBuf = ""

	// Start the stream session
	m.session = NewStreamSession(m.client, m.agentID, m.topicID, text, m.hubToken)

	m.updateViewportContent()
	m.scrollToBottom()

	return m, m.session.NextMsg()
}

// layout recalculates component sizes when the window changes.
func (m Model) layout() Model {
	if m.width == 0 || m.height == 0 {
		return m
	}

	m.renderer = NewRenderer(m.width)

	inputHeight := 5 // textarea + padding
	statusHeight := 1
	vpHeight := m.height - inputHeight - statusHeight
	if vpHeight < 3 {
		vpHeight = 3
	}

	if !m.ready {
		m.viewport = viewport.New(
			viewport.WithWidth(m.width),
			viewport.WithHeight(vpHeight),
		)
		m.ready = true
	} else {
		m.viewport.SetWidth(m.width)
		m.viewport.SetHeight(vpHeight)
	}

	m.input.SetWidth(m.width - 2)
	m.updateViewportContent()
	m.scrollToBottom()

	return m
}

// updateViewportContent rebuilds the viewport content from history + current stream.
func (m *Model) updateViewportContent() {
	if !m.ready {
		return
	}

	var content strings.Builder
	content.WriteString(m.history)

	// Show current streaming content
	if m.state == stateStreaming {
		if m.thinkBuf != "" && m.renderer != nil {
			content.WriteString(m.renderer.RenderThinking(m.thinkBuf))
		}
		if m.streamBuf != "" && m.renderer != nil {
			content.WriteString(m.renderer.RenderAssistantStreaming(m.streamBuf))
		}
		if m.streamBuf == "" && m.thinkBuf == "" {
			// Show spinner
			content.WriteString("  " + m.spinner.View() + " thinking...\n")
		}
	}

	m.viewport.SetContent(content.String())
}

// scrollToBottom scrolls the viewport to the bottom.
func (m *Model) scrollToBottom() {
	if !m.ready {
		return
	}
	m.viewport.GotoBottom()
}

// View implements tea.Model — returns tea.View for bubbletea v2.
func (m Model) View() tea.View {
	if m.quitting {
		v := tea.NewView("")
		return v
	}
	if !m.ready {
		v := tea.NewView("Initializing...\n")
		v.AltScreen = true
		return v
	}

	var b strings.Builder

	// Viewport (messages)
	b.WriteString(m.viewport.View())
	b.WriteString("\n")

	// Status bar
	if m.renderer != nil {
		status := m.renderer.RenderStatusBar(m.agentName, m.topicID)
		b.WriteString(status)
		b.WriteString("\n")
	}

	// Input area
	if m.state == stateStreaming {
		spinnerView := m.spinner.View()
		b.WriteString(fmt.Sprintf(" %s streaming... (Ctrl+C to cancel)", spinnerView))
	} else {
		b.WriteString(m.input.View())
	}

	v := tea.NewView(b.String())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

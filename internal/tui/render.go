// Role:    Message rendering for TUI chat — markdown, tool calls, thinking
// Depends: fmt, strings, charm.land/glamour/v2, charm.land/lipgloss/v2
// Exports: NewRenderer, RenderUserMessage, RenderAssistantMessage, RenderToolCall, RenderToolResult, RenderThinking, RenderError

package tui

import (
	"fmt"
	"strings"

	glamour "charm.land/glamour/v2"
	lipgloss "charm.land/lipgloss/v2"
)

// Renderer handles styled text rendering for the chat TUI.
type Renderer struct {
	width    int
	glamour  *glamour.TermRenderer
	userStyle     lipgloss.Style
	assistStyle   lipgloss.Style
	toolStyle     lipgloss.Style
	resultStyle   lipgloss.Style
	thinkStyle    lipgloss.Style
	errorStyle    lipgloss.Style
	dimStyle      lipgloss.Style
}

// NewRenderer creates a renderer for the given terminal width.
func NewRenderer(width int) *Renderer {
	contentWidth := width - 4
	if contentWidth < 40 {
		contentWidth = 40
	}

	gr, _ := glamour.NewTermRenderer(
		glamour.WithEnvironmentConfig(),
		glamour.WithWordWrap(contentWidth),
	)

	return &Renderer{
		width:   width,
		glamour: gr,
		userStyle: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("6")),
		assistStyle: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("5")),
		toolStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("3")),
		resultStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("8")),
		thinkStyle: lipgloss.NewStyle().
			Italic(true).
			Foreground(lipgloss.Color("8")),
		errorStyle: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("1")),
		dimStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("8")),
	}
}

// RenderUserMessage renders a user message with "You:" prefix.
func (r *Renderer) RenderUserMessage(msg string) string {
	header := r.userStyle.Render("You:")
	return header + "\n" + msg + "\n"
}

// RenderAssistantMessage renders an assistant message with markdown.
func (r *Renderer) RenderAssistantMessage(content string) string {
	header := r.assistStyle.Render("Assistant:")
	if r.glamour != nil && strings.TrimSpace(content) != "" {
		rendered, err := r.glamour.Render(content)
		if err == nil {
			return header + "\n" + strings.TrimRight(rendered, "\n") + "\n"
		}
	}
	return header + "\n" + content + "\n"
}

// RenderAssistantStreaming renders the assistant header + partial content during streaming.
func (r *Renderer) RenderAssistantStreaming(content string) string {
	header := r.assistStyle.Render("Assistant:")
	if strings.TrimSpace(content) == "" {
		return header + "\n"
	}
	// During streaming, render markdown for accumulated content
	if r.glamour != nil {
		rendered, err := r.glamour.Render(content)
		if err == nil {
			return header + "\n" + strings.TrimRight(rendered, "\n") + "\n"
		}
	}
	return header + "\n" + content + "\n"
}

// RenderToolCall renders a tool call notification.
func (r *Renderer) RenderToolCall(name, args string) string {
	header := r.toolStyle.Render(fmt.Sprintf("  [tool] %s", name))
	if args != "" {
		truncated := truncate(args, 200)
		return header + "\n" + r.dimStyle.Render("  "+truncated) + "\n"
	}
	return header + "\n"
}

// RenderToolResult renders a tool result (truncated).
func (r *Renderer) RenderToolResult(name, content string) string {
	truncated := truncate(content, 300)
	header := r.resultStyle.Render(fmt.Sprintf("  [result] %s", name))
	return header + "\n" + r.dimStyle.Render("  "+truncated) + "\n"
}

// RenderThinking renders thinking text in dim italic.
func (r *Renderer) RenderThinking(content string) string {
	truncated := truncate(content, 500)
	return r.thinkStyle.Render("  (thinking) "+truncated) + "\n"
}

// RenderError renders an error message.
func (r *Renderer) RenderError(msg string) string {
	return r.errorStyle.Render("Error: "+msg) + "\n"
}

// RenderStatusBar renders the bottom status bar.
func (r *Renderer) RenderStatusBar(agentName, topicID string) string {
	left := r.dimStyle.Render(fmt.Sprintf(" agent: %s", agentName))
	right := r.dimStyle.Render(fmt.Sprintf("topic: %s ", truncate(topicID, 12)))
	gap := r.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

func truncate(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

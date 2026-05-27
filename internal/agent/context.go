// Role:    Context construction — system prompt, clip usage tracking, user message wrapping, context compression
// Depends: encoding/json, fmt, strings, time
// Exports: ContextManager

package agent

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Compression constants (char-based, ~3.3 chars/token)
const (
	MaxContext        = 120_000 // trigger compression
	TargetContext     = 80_000  // compress down to
	TurnWindow        = 5      // clip usage recency window
	TruncateTool      = 1_000  // tool I/O cap in TRUNCATE tier
	TruncateAssistant = 500    // assistant cap in BRIEF tier
	MaxToolResult     = 16_000 // per-result truncation
)

// ContextManager tracks clip usage and builds context for the LLM.
type ContextManager struct {
	clipUsage   map[string]int // alias -> last used turn
	currentTurn int
}

// NewContextManager creates a new context manager.
func NewContextManager() *ContextManager {
	return &ContextManager{
		clipUsage: make(map[string]int),
	}
}

// AdvanceTurn increments the turn counter.
func (cm *ContextManager) AdvanceTurn() {
	cm.currentTurn++
}

// TrackClipUsage records that a clip was used in the current turn.
func (cm *ContextManager) TrackClipUsage(alias string) {
	cm.clipUsage[alias] = cm.currentTurn
}

// isRecentlyUsed checks if a clip was used within the turn window.
func (cm *ContextManager) isRecentlyUsed(alias string) bool {
	lastUsed, ok := cm.clipUsage[alias]
	if !ok {
		return false
	}
	return cm.currentTurn-lastUsed <= TurnWindow
}

// ActiveClips filters clips to only those visible to the agent.
func ActiveClips(agt *Agent, clips []ClipInfo) []ClipInfo {
	var result []ClipInfo
	for _, clip := range clips {
		if clip.Status != "" && clip.Status != "running" {
			continue
		}
		if len(agt.Scope) > 0 && !contains(agt.Scope, clip.Alias) && !contains(agt.Scope, clip.Name) {
			continue
		}
		result = append(result, clip)
	}
	return result
}

// BuildSystemMessage constructs the system prompt with clip documentation.
func (cm *ContextManager) BuildSystemMessage(agt *Agent, clips []ClipInfo) LLMMessage {
	cm.AdvanceTurn()

	visible := ActiveClips(agt, clips)

	var b strings.Builder

	// User's custom system prompt
	if agt.SystemPrompt != "" {
		b.WriteString(agt.SystemPrompt)
		b.WriteString("\n\n")
	}

	// Clip documentation
	if len(visible) > 0 {
		b.WriteString("# 可用工具\n\n")
		b.WriteString("通过 run 工具执行命令来调用以下工具。\n\n")

		for _, clip := range visible {
			isPinned := contains(agt.Pinned, clip.Alias) || contains(agt.Pinned, clip.Name)
			isRecent := cm.isRecentlyUsed(clip.Alias)

			if isPinned || isRecent {
				b.WriteString(formatClipFull(&clip))
			} else {
				b.WriteString(formatClipSummary(&clip))
			}
		}
	}

	// System suffix — operating instructions
	b.WriteString(systemSuffix)

	return LLMMessage{
		Role:    RoleSystem,
		Content: b.String(),
	}
}

// WrapUserMessage wraps user text with environment context.
func WrapUserMessage(agt *Agent, clips []ClipInfo, userText string) string {
	var b strings.Builder
	b.WriteString("<user>\n")
	b.WriteString(userText)
	b.WriteString("\n</user>\n")

	b.WriteString("<environment>\n")
	b.WriteString(fmt.Sprintf("<time>%s</time>\n", time.Now().Format("2006-01-02 15:04:05 MST")))

	if len(agt.Pinned) > 0 {
		b.WriteString(fmt.Sprintf("<pinned-clips>%s</pinned-clips>\n", strings.Join(agt.Pinned, ", ")))
	}

	// Hub status
	if len(clips) > 0 {
		names := make([]string, 0, len(clips))
		for _, c := range clips {
			names = append(names, c.Alias)
		}
		b.WriteString(fmt.Sprintf("<hub>%d clips available: %s</hub>\n", len(clips), strings.Join(names, ", ")))
	}
	b.WriteString("</environment>")

	return b.String()
}

// --- Clip formatting ---

func formatClipFull(clip *ClipInfo) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("## %s", clip.Alias))
	if clip.Description != "" {
		b.WriteString(fmt.Sprintf(" — %s", clip.Description))
	}
	b.WriteString("\n")

	for _, cmd := range clip.Commands {
		b.WriteString(fmt.Sprintf("  %s %s", clip.Alias, cmd.Name))
		if cmd.Description != "" {
			b.WriteString(fmt.Sprintf(" — %s", cmd.Description))
		}
		b.WriteString("\n")
		params := formatCommandParams(cmd.Input)
		if params != "" {
			b.WriteString(params)
		}
	}
	b.WriteString("\n")
	return b.String()
}

func formatClipSummary(clip *ClipInfo) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("- %s", clip.Alias))
	if clip.Description != "" {
		b.WriteString(fmt.Sprintf(": %s", clip.Description))
	}
	if len(clip.Commands) > 0 {
		names := make([]string, len(clip.Commands))
		for i, cmd := range clip.Commands {
			names[i] = cmd.Name
		}
		b.WriteString(fmt.Sprintf(" [%s]", strings.Join(names, ", ")))
	}
	b.WriteString("\n")
	return b.String()
}

// --- Context compression ---

// turnGroup represents a group of messages that form one "turn" in the conversation.
type turnGroup struct {
	messages []LLMMessage
	size     int // estimated char count
}

// CompressContext compresses the LLM message context if it exceeds MaxContext.
// The first message (system prompt) is always preserved.
// Returns the compressed message list.
func CompressContext(messages []LLMMessage) []LLMMessage {
	totalSize := 0
	for _, m := range messages {
		totalSize += estimateSize(&m)
	}

	if totalSize <= MaxContext {
		return messages
	}

	if len(messages) < 2 {
		return messages
	}

	// Preserve system prompt
	system := messages[0]
	rest := messages[1:]

	// Identify turns
	turns := identifyTurns(rest)

	// Apply tier-based compression from oldest to newest
	numTurns := len(turns)
	for i := range turns {
		age := numTurns - 1 - i // 0 = newest

		if age == 0 {
			// FULL: keep as-is
			continue
		} else if age <= 3 {
			// TRUNCATE: drop reasoning, cap tool I/O
			truncateTurn(&turns[i])
		} else {
			// BRIEF: only keep brief, drop tool results
			briefTurn(&turns[i])
		}
	}

	// Rebuild message list
	result := []LLMMessage{system}
	totalSize = estimateSize(&system)

	for _, turn := range turns {
		for _, m := range turn.messages {
			s := estimateSize(&m)
			if totalSize+s > TargetContext {
				// DROP: skip this message
				continue
			}
			result = append(result, m)
			totalSize += s
		}
	}

	return result
}

func identifyTurns(messages []LLMMessage) []turnGroup {
	var turns []turnGroup
	var current turnGroup

	for _, m := range messages {
		if m.Role == RoleUser {
			// User message is its own turn
			if len(current.messages) > 0 {
				turns = append(turns, current)
			}
			turns = append(turns, turnGroup{
				messages: []LLMMessage{m},
				size:     estimateSize(&m),
			})
			current = turnGroup{}
		} else {
			// Assistant + tool messages group together
			current.messages = append(current.messages, m)
			current.size += estimateSize(&m)
		}
	}

	if len(current.messages) > 0 {
		turns = append(turns, current)
	}

	return turns
}

func truncateTurn(turn *turnGroup) {
	for i := range turn.messages {
		m := &turn.messages[i]
		// Drop reasoning
		m.ReasoningContent = ""
		// Truncate tool call arguments
		for j := range m.ToolCalls {
			if len(m.ToolCalls[j].Function.Arguments) > TruncateTool {
				m.ToolCalls[j].Function.Arguments = m.ToolCalls[j].Function.Arguments[:TruncateTool] + "..."
			}
		}
		// Truncate tool result content
		if m.Role == RoleAssistant && strings.HasPrefix(m.Content, "Tool result") {
			if len(m.Content) > TruncateTool {
				m.Content = m.Content[:TruncateTool] + "\n[truncated]"
			}
		}
	}
}

func briefTurn(turn *turnGroup) {
	for i := range turn.messages {
		m := &turn.messages[i]
		m.ReasoningContent = ""

		if m.Role == RoleAssistant {
			// Keep only brief from tool call args
			for j := range m.ToolCalls {
				brief := extractBrief(m.ToolCalls[j].Function.Arguments)
				if brief != "" {
					m.ToolCalls[j].Function.Arguments = fmt.Sprintf(`{"brief":"%s"}`, brief)
				}
			}
			// Truncate assistant content
			if len(m.Content) > TruncateAssistant {
				m.Content = m.Content[:TruncateAssistant] + "..."
			}
		}

		// Tool results → omitted
		if m.Role == RoleAssistant && strings.HasPrefix(m.Content, "Tool result") {
			m.Content = "[omitted]"
		}
	}
}

func extractBrief(argsJSON string) string {
	var args struct {
		Brief string `json:"brief"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return ""
	}
	return args.Brief
}

func estimateSize(m *LLMMessage) int {
	size := len(m.Content) + len(m.ReasoningContent)
	for _, tc := range m.ToolCalls {
		size += len(tc.Function.Arguments)
	}
	return size
}

// TruncateToolResult performs middle-out truncation on a tool result string.
func TruncateToolResult(result string) string {
	if len(result) <= MaxToolResult {
		return result
	}
	half := MaxToolResult / 2
	omitted := len(result) - MaxToolResult
	return result[:half] + fmt.Sprintf("\n\n[%d chars omitted]\n\n", omitted) + result[len(result)-half:]
}

// --- Helpers ---

func contains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

// timeNow is a package-level function for testing.
var timeNow = time.Now

const systemSuffix = `
# 操作规范

## run 工具使用
- 每次调用 run 工具前，在 brief 中用一句话说明你要做什么
- brief 会展示给用户，也用于上下文压缩
- command 是具体要执行的命令

## 命令格式
- 基本: clip command --key value positional-arg
- 管道: cmd1 | cmd2（前一个输出作为后一个输入）
- 链接: cmd1 && cmd2（顺序执行）
- Heredoc: command << EOF\n内容\nEOF

## 内置命令
- echo <text> — 输出文本，支持 \n 换行
- time — 当前时间
- help [clip] — 查看帮助
- topic list — 列出对话
- topic current — 当前对话信息
- topic messages <id> [n] — 查看消息历史
- topic rename <id> <name> — 重命名对话
- topic search <query> — 搜索对话
- agent list — 列出 agent
- agent current — 当前 agent 配置
- agent rename <name> — 重命名
- agent set <key> <value> — 修改配置
- agent pin <clip> — 固定 clip
- agent unpin <clip> — 取消固定
- agent scope — 查看权限范围

## 输出格式
- 使用 Markdown 格式
- 数学公式用 KaTeX: 行内 $...$，块级 $$...$$
- 代码块标注语言
- 表格用 Markdown 表格
`

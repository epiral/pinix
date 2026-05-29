// Role:    Main agent loop — LLM → tool calls → execute → repeat → stream output
// Depends: context, encoding/json, fmt, log/slog, strings, time
// Exports: Runtime, NewRuntime, Chat

package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// Runtime is the Go Agent Runtime embedded in pinixd.
type Runtime struct {
	store   *Store
	llm     *LLMClient
	ctx     *ContextManager
	invoker ClipInvoker

	// getClips returns the current list of available clips.
	getClips func() []ClipInfo
}

// NewRuntime creates a new agent runtime.
func NewRuntime(store *Store, invoker ClipInvoker, getClips func() []ClipInfo) *Runtime {
	return &Runtime{
		store:    store,
		llm:      NewLLMClient(),
		ctx:      NewContextManager(),
		invoker:  invoker,
		getClips: getClips,
	}
}

// ChatInput is the input to a chat invocation.
type ChatInput struct {
	AgentID  string `json:"agent_id"`
	TopicID  string `json:"topic_id,omitempty"`
	Message  string `json:"message"`
}

// ChatOutput streams events via the onEvent callback and returns the final messages.
func (r *Runtime) Chat(ctx context.Context, input ChatInput, onEvent func(StreamEvent)) error {
	// Load agent
	agt, err := r.store.GetAgent(input.AgentID)
	if err != nil {
		return fmt.Errorf("get agent: %w", err)
	}
	if agt == nil {
		return fmt.Errorf("agent not found: %s", input.AgentID)
	}

	// Get or create topic
	var topic *Topic
	if input.TopicID != "" {
		topic, err = r.store.GetTopic(input.TopicID)
		if err != nil {
			return fmt.Errorf("get topic: %w", err)
		}
	}
	if topic == nil {
		topic = &Topic{
			AgentID: agt.ID,
			Title:   truncate(input.Message, 30),
		}
		if err := r.store.SaveTopic(topic); err != nil {
			return fmt.Errorf("create topic: %w", err)
		}
	}

	// Cancel any running runs on this topic
	_ = r.store.CancelRunningRuns(topic.ID)

	// Create run
	run, err := r.store.CreateRun(topic.ID)
	if err != nil {
		return fmt.Errorf("create run: %w", err)
	}

	// Run the loop
	loopErr := r.runLoop(ctx, agt, topic, run, input.Message, onEvent)

	// Finish run
	status := RunStatusDone
	if loopErr != nil {
		status = RunStatusError
		if ctx.Err() != nil {
			status = RunStatusCancelled
		}
	}
	_ = r.store.FinishRun(run.ID, status)

	// Update topic timestamp
	_ = r.store.SaveTopic(topic)

	// Generate title if this topic has no meaningful title yet.
	// This covers both: (1) topics created by Chat() itself (title = truncated message),
	// and (2) topics pre-created by the frontend with an empty title.
	if topic.Title == "" || topic.Title == truncate(input.Message, 30) {
		r.generateTopicTitle(agt, topic, input.Message)
	}

	if loopErr != nil && ctx.Err() == nil {
		onEvent(StreamEvent{Type: StreamEventError, Error: loopErr.Error()})
	}
	onEvent(StreamEvent{Type: StreamEventDone})

	return loopErr
}

func (r *Runtime) runLoop(ctx context.Context, agt *Agent, topic *Topic, run *Run, userMessage string, onEvent func(StreamEvent)) error {
	// Get current clips
	clips := r.getClips()
	visible := ActiveClips(agt, clips)

	// Build builtins
	builtins := r.buildBuiltins(agt, topic.ID)

	// Load existing messages
	history, err := r.store.ListMessages(topic.ID)
	if err != nil {
		return fmt.Errorf("load history: %w", err)
	}

	// Save user message
	userMsg := &Message{
		TopicID: topic.ID,
		RunID:   run.ID,
		Role:    RoleUser,
		Content: userMessage,
	}
	if err := r.store.AddMessage(userMsg); err != nil {
		return fmt.Errorf("save user message: %w", err)
	}

	// Build LLM context
	systemMsg := r.ctx.BuildSystemMessage(agt, clips)
	llmMessages := []LLMMessage{systemMsg}

	// Convert history to LLM messages
	for _, m := range history {
		llmMessages = append(llmMessages, toLLMMessage(m))
	}

	// Wrap and add user message
	wrappedUser := WrapUserMessage(agt, visible, userMessage)
	llmMessages = append(llmMessages, LLMMessage{
		Role:    RoleUser,
		Content: wrappedUser,
	})

	// Build tool definition
	tool := BuildRunTool(visible)

	// Agent loop
	for iteration := 0; ; iteration++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// Compress context if needed
		preCompChars := totalContextSize(llmMessages)
		llmMessages = CompressContext(llmMessages)
		postCompChars := totalContextSize(llmMessages)
		compressed := preCompChars != postCompChars

		slog.Info("agent.loop: calling LLM",
			"topic", topic.ID,
			"run", run.ID,
			"iteration", iteration,
			"messages", len(llmMessages),
			"context_chars", postCompChars,
			"compressed", compressed,
		)

		// Call LLM
		streamInput := ChatStreamInput{
			BaseURL:         agt.BaseURL,
			APIKey:          agt.APIKey,
			Model:           agt.LLMModel,
			Messages:        llmMessages,
			Tools:           []LLMTool{tool},
			Temperature:     agt.Temperature,
			MaxTokens:       agt.MaxTokens,
			EnableReasoning: agt.EnableReasoning,
			OnDelta:         onEvent,
		}

		if dbg := GetDebugDumper(); dbg != nil {
			sysChars := 0
			if len(llmMessages) > 0 {
				sysChars = len(llmMessages[0].Content)
			}
			entry := DebugEntry{
				TopicID:      topic.ID,
				RunID:        run.ID,
				Iteration:    iteration,
				Model:        agt.LLMModel,
				MsgCount:     len(llmMessages),
				TotalChars:   postCompChars,
				SystemChars:  sysChars,
				Compressed:   compressed,
				PreCompChars: preCompChars,
			}
			streamInput.OnRequest = func(body []byte) {
				dbg.DumpRequest(entry, body)
			}
		}

		result, err := r.llm.ChatStream(ctx, streamInput)
		if err != nil {
			return fmt.Errorf("LLM call failed: %w", err)
		}

		// Debug: dump response
		if dbg := GetDebugDumper(); dbg != nil {
			dbg.DumpResponse(run.ID, iteration, result)
		}

		// Save assistant message
		assistantMsg := &Message{
			TopicID:   topic.ID,
			RunID:     run.ID,
			Role:      RoleAssistant,
			Content:   result.Content,
			Reasoning: result.Reasoning,
		}
		if result.Usage != nil {
			usageJSON, _ := json.Marshal(result.Usage)
			assistantMsg.Usage = string(usageJSON)
		}
		if err := r.store.AddMessage(assistantMsg); err != nil {
			slog.Error("agent.loop: save assistant message", "error", err)
		}

		// Add assistant message to context
		assistantLLM := LLMMessage{
			Role:             RoleAssistant,
			Content:          result.Content,
			ReasoningContent: result.Reasoning,
		}

		// No tool calls → done
		if len(result.ToolCalls) == 0 {
			llmMessages = append(llmMessages, assistantLLM)
			return nil
		}

		// Convert tool calls to LLM format
		for _, tc := range result.ToolCalls {
			assistantLLM.ToolCalls = append(assistantLLM.ToolCalls, LLMToolCall{
				ID:   tc.ID,
				Type: "function",
				Function: LLMToolFunction{
					Name:      tc.Name,
					Arguments: tc.Arguments,
				},
			})
		}
		llmMessages = append(llmMessages, assistantLLM)

		// Execute tool calls
		for _, tc := range result.ToolCalls {
			if ctx.Err() != nil {
				return ctx.Err()
			}

			onEvent(StreamEvent{
				Type:      StreamEventToolCall,
				ID:        tc.ID,
				Name:      tc.Name,
				Arguments: tc.Arguments,
			})

			// Track clip usage
			tokens := Tokenize(extractCommand(tc.Arguments))
			if len(tokens) > 0 {
				r.ctx.TrackClipUsage(tokens[0])
			}

			slog.Info("agent.loop: executing tool",
				"run", run.ID,
				"tool_call_id", tc.ID,
				"brief", extractBrief(tc.Arguments),
			)

			start := time.Now()
			toolResult, toolErr := ExecuteToolCall(tc, visible, builtins, r.invoker)
			duration := time.Since(start)

			if toolErr != nil {
				toolResult = fmt.Sprintf("Error: %v", toolErr)
				slog.Error("agent.loop: tool error",
					"run", run.ID,
					"tool_call_id", tc.ID,
					"duration_ms", duration.Milliseconds(),
					"error", toolErr,
				)
			} else {
				slog.Info("agent.loop: tool done",
					"run", run.ID,
					"tool_call_id", tc.ID,
					"duration_ms", duration.Milliseconds(),
					"result_len", len(toolResult),
				)
			}

			// Truncate large results
			toolResult = TruncateToolResult(toolResult)

			onEvent(StreamEvent{
				Type:    StreamEventToolResult,
				ID:      tc.ID,
				Name:    tc.Name,
				Content: toolResult,
			})

			// Save tool message
			toolMsg := &Message{
				TopicID:    topic.ID,
				RunID:      run.ID,
				Role:       RoleTool,
				Content:    toolResult,
				ToolCallID: tc.ID,
				ToolName:   tc.Name,
				ToolArgs:   tc.Arguments,
			}
			if err := r.store.AddMessage(toolMsg); err != nil {
				slog.Error("agent.loop: save tool message", "error", err)
			}

			// Add tool result to context
			llmMessages = append(llmMessages, LLMMessage{
				Role:       RoleTool,
				Content:    toolResult,
				ToolCallID: tc.ID,
				Name:       tc.Name,
			})
		}
	}
}

func (r *Runtime) generateTopicTitle(agt *Agent, topic *Topic, userText string) {
	// Get the first assistant message for context
	msgs, err := r.store.ListMessages(topic.ID)
	if err != nil || len(msgs) == 0 {
		return
	}
	var assistantText string
	for _, m := range msgs {
		if m.Role == RoleAssistant {
			assistantText = m.Content
			break
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	title, err := r.llm.GenerateTitle(ctx, agt.BaseURL, agt.APIKey, agt.LLMModel, userText, assistantText)
	if err != nil {
		slog.Error("agent: generate title", "error", err)
		return
	}
	if title != "" {
		_ = r.store.UpdateTopicTitle(topic.ID, title)
	}
}

// --- Builtins ---

func (r *Runtime) buildBuiltins(agt *Agent, topicID string) Builtins {
	builtins := make(Builtins)

	builtins["topic"] = func(args []string, stdin string) (string, error) {
		if len(args) == 0 {
			return "Usage: topic <list|current|messages|rename|search>", nil
		}
		switch args[0] {
		case "list":
			return r.builtinTopicList(agt.ID)
		case "current":
			return r.builtinTopicCurrent(topicID)
		case "messages":
			if len(args) < 2 {
				return r.builtinTopicMessages(topicID, 20)
			}
			n := 20
			id := args[1]
			if len(args) > 2 {
				fmt.Sscanf(args[2], "%d", &n)
			}
			return r.builtinTopicMessages(id, n)
		case "rename":
			if len(args) < 3 {
				return "", fmt.Errorf("usage: topic rename <id> <name>")
			}
			return r.builtinTopicRename(args[1], strings.Join(args[2:], " "))
		case "search":
			if len(args) < 2 {
				return "", fmt.Errorf("usage: topic search <query>")
			}
			return r.builtinTopicSearch(strings.Join(args[1:], " "))
		default:
			return "", fmt.Errorf("unknown topic subcommand: %s", args[0])
		}
	}

	builtins["agent"] = func(args []string, stdin string) (string, error) {
		if len(args) == 0 {
			return "Usage: agent <list|current|rename|set|pin|unpin|scope>", nil
		}
		switch args[0] {
		case "list":
			return r.builtinAgentList()
		case "current":
			return r.builtinAgentCurrent(agt)
		case "rename":
			if len(args) < 2 {
				return "", fmt.Errorf("usage: agent rename <name>")
			}
			agt.Name = strings.Join(args[1:], " ")
			return fmt.Sprintf("Agent renamed to: %s", agt.Name), r.store.SaveAgent(agt)
		case "set":
			if len(args) < 3 {
				if stdin != "" && len(args) >= 2 {
					return r.builtinAgentSet(agt, args[1], stdin)
				}
				return "", fmt.Errorf("usage: agent set <key> <value>")
			}
			return r.builtinAgentSet(agt, args[1], strings.Join(args[2:], " "))
		case "pin":
			if len(args) < 2 {
				return "", fmt.Errorf("usage: agent pin <clip>")
			}
			if !contains(agt.Pinned, args[1]) {
				agt.Pinned = append(agt.Pinned, args[1])
				if err := r.store.SaveAgent(agt); err != nil {
					return "", err
				}
			}
			return fmt.Sprintf("Pinned: %s. Current pins: %s", args[1], strings.Join(agt.Pinned, ", ")), nil
		case "unpin":
			if len(args) < 2 {
				return "", fmt.Errorf("usage: agent unpin <clip>")
			}
			newPinned := make([]string, 0, len(agt.Pinned))
			for _, p := range agt.Pinned {
				if p != args[1] {
					newPinned = append(newPinned, p)
				}
			}
			agt.Pinned = newPinned
			if err := r.store.SaveAgent(agt); err != nil {
				return "", err
			}
			return fmt.Sprintf("Unpinned: %s. Current pins: %s", args[1], strings.Join(agt.Pinned, ", ")), nil
		case "scope":
			if len(agt.Scope) == 0 {
				return "Scope: all clips (no restrictions)", nil
			}
			return fmt.Sprintf("Scope: %s", strings.Join(agt.Scope, ", ")), nil
		default:
			return "", fmt.Errorf("unknown agent subcommand: %s", args[0])
		}
	}

	return builtins
}

func (r *Runtime) builtinTopicList(agentID string) (string, error) {
	topics, err := r.store.ListTopics(agentID)
	if err != nil {
		return "", err
	}
	if len(topics) == 0 {
		return "No topics.", nil
	}
	var b strings.Builder
	for _, t := range topics {
		count, _ := r.store.CountMessages(t.ID)
		b.WriteString(fmt.Sprintf("[%s] %s (%d messages, %s)\n", t.ID, t.Title, count, t.UpdatedAt.Format("2006-01-02 15:04")))
	}
	return b.String(), nil
}

func (r *Runtime) builtinTopicCurrent(topicID string) (string, error) {
	topic, err := r.store.GetTopic(topicID)
	if err != nil || topic == nil {
		return "No current topic.", nil
	}
	count, _ := r.store.CountMessages(topic.ID)
	return fmt.Sprintf("ID: %s\nTitle: %s\nMessages: %d\nCreated: %s\nUpdated: %s",
		topic.ID, topic.Title, count,
		topic.CreatedAt.Format("2006-01-02 15:04"),
		topic.UpdatedAt.Format("2006-01-02 15:04")), nil
}

func (r *Runtime) builtinTopicMessages(topicID string, n int) (string, error) {
	msgs, err := r.store.ListMessages(topicID)
	if err != nil {
		return "", err
	}
	if len(msgs) == 0 {
		return "No messages.", nil
	}
	// Take last n
	if n > 0 && len(msgs) > n {
		msgs = msgs[len(msgs)-n:]
	}
	var b strings.Builder
	for _, m := range msgs {
		content := m.Content
		if len(content) > 200 {
			content = content[:200] + "..."
		}
		b.WriteString(fmt.Sprintf("[%s] %s: %s\n", m.CreatedAt.Format("15:04"), m.Role, content))
	}
	return b.String(), nil
}

func (r *Runtime) builtinTopicRename(id, name string) (string, error) {
	if err := r.store.UpdateTopicTitle(id, name); err != nil {
		return "", err
	}
	return fmt.Sprintf("Topic %s renamed to: %s", id, name), nil
}

func (r *Runtime) builtinTopicSearch(query string) (string, error) {
	msgs, err := r.store.SearchMessages(query, 20)
	if err != nil {
		return "", err
	}
	if len(msgs) == 0 {
		return "No results.", nil
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Found %d results for \"%s\":\n\n", len(msgs), query))
	for _, m := range msgs {
		content := m.Content
		if len(content) > 100 {
			content = content[:100] + "..."
		}
		b.WriteString(fmt.Sprintf("[%s] topic:%s %s: %s\n", m.CreatedAt.Format("2006-01-02 15:04"), m.TopicID, m.Role, content))
	}
	return b.String(), nil
}

func (r *Runtime) builtinAgentList() (string, error) {
	agents, err := r.store.ListAgents()
	if err != nil {
		return "", err
	}
	if len(agents) == 0 {
		return "No agents.", nil
	}
	var b strings.Builder
	for _, a := range agents {
		b.WriteString(fmt.Sprintf("[%s] %s (model: %s)\n", a.ID, a.Name, a.LLMModel))
	}
	return b.String(), nil
}

func (r *Runtime) builtinAgentCurrent(agt *Agent) (string, error) {
	masked := agt.APIKey
	if len(masked) > 8 {
		masked = masked[:4] + "..." + masked[len(masked)-4:]
	}
	return fmt.Sprintf("ID: %s\nName: %s\nProvider: %s\nModel: %s\nBase URL: %s\nAPI Key: %s\nTemperature: %.1f\nMax Tokens: %d\nReasoning: %v\nPinned: %s\nScope: %s",
		agt.ID, agt.Name, agt.LLMProvider, agt.LLMModel, agt.BaseURL, masked,
		agt.Temperature, agt.MaxTokens, agt.EnableReasoning,
		strings.Join(agt.Pinned, ", "),
		func() string {
			if len(agt.Scope) == 0 {
				return "all"
			}
			return strings.Join(agt.Scope, ", ")
		}()), nil
}

func (r *Runtime) builtinAgentSet(agt *Agent, key, value string) (string, error) {
	switch key {
	case "name":
		agt.Name = value
	case "model":
		agt.LLMModel = value
	case "system_prompt":
		agt.SystemPrompt = value
	case "temperature":
		var t float64
		fmt.Sscanf(value, "%f", &t)
		agt.Temperature = t
	case "max_tokens":
		var n int
		fmt.Sscanf(value, "%d", &n)
		agt.MaxTokens = n
	case "base_url":
		agt.BaseURL = value
	case "api_key":
		agt.APIKey = value
	case "provider":
		agt.LLMProvider = value
	default:
		return "", fmt.Errorf("unknown key: %s. Available: name, model, system_prompt, temperature, max_tokens, base_url, api_key, provider", key)
	}
	if err := r.store.SaveAgent(agt); err != nil {
		return "", err
	}
	return fmt.Sprintf("Set %s = %s", key, value), nil
}

// --- Helpers ---

// toLLMMessage converts a stored Message to an LLM wire format message.
func toLLMMessage(m Message) LLMMessage {
	msg := LLMMessage{
		Role:    m.Role,
		Content: m.Content,
	}

	// Tool messages: set tool_call_id
	if m.Role == RoleTool {
		msg.ToolCallID = m.ToolCallID
		msg.Name = m.ToolName
	}

	// Assistant messages with tool calls: reconstruct tool_calls
	if m.Role == RoleAssistant && m.ToolArgs != "" {
		msg.ToolCalls = []LLMToolCall{{
			ID:   m.ToolCallID,
			Type: "function",
			Function: LLMToolFunction{
				Name:      m.ToolName,
				Arguments: m.ToolArgs,
			},
		}}
	}

	// Reasoning
	if m.Reasoning != "" {
		msg.ReasoningContent = m.Reasoning
	}

	return msg
}

func totalContextSize(messages []LLMMessage) int {
	total := 0
	for _, m := range messages {
		total += estimateSize(&m)
	}
	return total
}

func extractCommand(argsJSON string) string {
	var args struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return ""
	}
	return args.Command
}

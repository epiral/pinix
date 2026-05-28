// Role:    Virtual clip command handler — routes Hub invoke requests to agent runtime methods
// Depends: context, encoding/json, fmt
// Exports: Handler, NewHandler

package agent

import (
	"context"
	"encoding/json"
	"fmt"
)

// Handler processes invoke requests for the "agent" virtual clip.
type Handler struct {
	runtime *Runtime
}

// NewHandler creates a new agent handler.
func NewHandler(rt *Runtime) *Handler {
	return &Handler{runtime: rt}
}

// HandleInvoke processes a command for the agent virtual clip.
// It returns streaming chunks via onChunk and a final result.
func (h *Handler) HandleInvoke(ctx context.Context, command string, input json.RawMessage, onChunk func(json.RawMessage)) (json.RawMessage, error) {
	switch command {
	case "chat":
		return h.handleChat(ctx, input, onChunk)
	case "topic list":
		return h.handleTopicList(ctx, input)
	case "topic get":
		return h.handleTopicGet(ctx, input)
	case "topic create":
		return h.handleTopicCreate(ctx, input)
	case "topic delete":
		return h.handleTopicDelete(ctx, input)
	case "topic rename":
		return h.handleTopicRename(ctx, input)
	case "topic search":
		return h.handleTopicSearch(ctx, input)
	case "run get":
		return h.handleRunGet(ctx, input)
	case "run cancel":
		return h.handleRunCancel(ctx, input)
	case "agent list":
		return h.handleAgentList(ctx)
	case "agent get":
		return h.handleAgentGet(ctx, input)
	case "agent create":
		return h.handleAgentCreate(ctx, input)
	case "agent update":
		return h.handleAgentUpdate(ctx, input)
	case "agent delete":
		return h.handleAgentDelete(ctx, input)
	case "event create":
		return h.handleEventCreate(ctx, input)
	case "event list":
		return h.handleEventList(ctx, input)
	case "event update":
		return h.handleEventUpdate(ctx, input)
	case "event cancel":
		return h.handleEventCancel(ctx, input)
	case "config get":
		return h.handleConfigGet(ctx, input)
	default:
		return nil, fmt.Errorf("unknown agent command: %s", command)
	}
}

// Manifest returns the virtual clip manifest for the agent.
func (h *Handler) Manifest() json.RawMessage {
	m := map[string]any{
		"name":        "agent",
		"package":     "@pinix/agent",
		"version":     "1.0.0",
		"domain":      "ai",
		"description": "Built-in AI Agent Runtime",
		"commands": []map[string]any{
			{"name": "chat", "description": "Send a message and get a streaming response", "input": `{"type":"object","properties":{"agent_id":{"type":"string"},"topic_id":{"type":"string"},"message":{"type":"string"}},"required":["agent_id","message"]}`},
			{"name": "topic list", "description": "List all topics for an agent"},
			{"name": "topic get", "description": "Get topic details with messages"},
			{"name": "topic create", "description": "Create a new topic"},
			{"name": "topic delete", "description": "Delete a topic"},
			{"name": "topic rename", "description": "Rename a topic"},
			{"name": "topic search", "description": "Search across topics"},
			{"name": "run get", "description": "Get run details"},
			{"name": "run cancel", "description": "Cancel a running run"},
			{"name": "agent list", "description": "List all agent configurations"},
			{"name": "agent get", "description": "Get agent details"},
			{"name": "agent create", "description": "Create a new agent"},
			{"name": "agent update", "description": "Update agent configuration"},
			{"name": "agent delete", "description": "Delete an agent"},
			{"name": "event create", "description": "Create a scheduled event"},
			{"name": "event list", "description": "List scheduled events"},
			{"name": "event update", "description": "Update a scheduled event"},
			{"name": "event cancel", "description": "Cancel a scheduled event"},
			{"name": "config get", "description": "Get global agent configuration"},
		},
	}
	b, _ := json.Marshal(m)
	return b
}

// --- Chat handler ---

func (h *Handler) handleChat(ctx context.Context, input json.RawMessage, onChunk func(json.RawMessage)) (json.RawMessage, error) {
	var chatInput ChatInput
	if err := json.Unmarshal(input, &chatInput); err != nil {
		return nil, fmt.Errorf("parse chat input: %w", err)
	}

	if chatInput.Message == "" {
		return nil, fmt.Errorf("message is required")
	}
	if chatInput.AgentID == "" {
		// Use first agent as default
		agents, err := h.runtime.store.ListAgents()
		if err != nil || len(agents) == 0 {
			return nil, fmt.Errorf("no agents configured; create one first")
		}
		chatInput.AgentID = agents[0].ID
	}

	onEvent := func(event StreamEvent) {
		chunk, _ := json.Marshal(event)
		if onChunk != nil {
			onChunk(chunk)
		}
	}

	err := h.runtime.Chat(ctx, chatInput, onEvent)
	if err != nil {
		return jsonResult("error", err.Error()), nil
	}
	return jsonResult("status", "done"), nil
}

// --- CRUD handlers ---

func (h *Handler) handleTopicList(_ context.Context, input json.RawMessage) (json.RawMessage, error) {
	var req struct {
		AgentID string `json:"agent_id"`
	}
	_ = json.Unmarshal(input, &req)

	if req.AgentID == "" {
		agents, _ := h.runtime.store.ListAgents()
		if len(agents) > 0 {
			req.AgentID = agents[0].ID
		}
	}

	topics, err := h.runtime.store.ListTopics(req.AgentID)
	if err != nil {
		return nil, err
	}
	if topics == nil {
		topics = []Topic{}
	}
	return json.Marshal(map[string]any{"topics": topics})
}

func (h *Handler) handleTopicGet(_ context.Context, input json.RawMessage) (json.RawMessage, error) {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(input, &req); err != nil || req.ID == "" {
		return nil, fmt.Errorf("id is required")
	}

	topic, err := h.runtime.store.GetTopic(req.ID)
	if err != nil {
		return nil, err
	}
	if topic == nil {
		return nil, fmt.Errorf("topic not found: %s", req.ID)
	}

	msgs, err := h.runtime.store.ListMessages(topic.ID)
	if err != nil {
		return nil, err
	}
	if msgs == nil {
		msgs = []Message{}
	}

	result := map[string]any{
		"topic":    topic,
		"messages": msgs,
	}
	return json.Marshal(result)
}

func (h *Handler) handleTopicCreate(_ context.Context, input json.RawMessage) (json.RawMessage, error) {
	var req struct {
		AgentID string `json:"agent_id"`
		Title   string `json:"title"`
	}
	_ = json.Unmarshal(input, &req)

	if req.AgentID == "" {
		agents, _ := h.runtime.store.ListAgents()
		if len(agents) > 0 {
			req.AgentID = agents[0].ID
		}
	}

	topic := &Topic{
		AgentID: req.AgentID,
		Title:   req.Title,
	}
	if err := h.runtime.store.SaveTopic(topic); err != nil {
		return nil, err
	}
	return json.Marshal(topic)
}

func (h *Handler) handleTopicDelete(_ context.Context, input json.RawMessage) (json.RawMessage, error) {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(input, &req); err != nil || req.ID == "" {
		return nil, fmt.Errorf("id is required")
	}
	if err := h.runtime.store.DeleteTopic(req.ID); err != nil {
		return nil, err
	}
	return jsonResult("status", "deleted"), nil
}

func (h *Handler) handleTopicRename(_ context.Context, input json.RawMessage) (json.RawMessage, error) {
	var req struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	}
	if err := json.Unmarshal(input, &req); err != nil || req.ID == "" {
		return nil, fmt.Errorf("id and title are required")
	}
	if err := h.runtime.store.UpdateTopicTitle(req.ID, req.Title); err != nil {
		return nil, err
	}
	return jsonResult("status", "renamed"), nil
}

func (h *Handler) handleTopicSearch(_ context.Context, input json.RawMessage) (json.RawMessage, error) {
	var req struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal(input, &req); err != nil || req.Query == "" {
		return nil, fmt.Errorf("query is required")
	}
	msgs, err := h.runtime.store.SearchMessages(req.Query, 20)
	if err != nil {
		return nil, err
	}
	if msgs == nil {
		msgs = []Message{}
	}
	return json.Marshal(msgs)
}

func (h *Handler) handleRunGet(_ context.Context, input json.RawMessage) (json.RawMessage, error) {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(input, &req); err != nil || req.ID == "" {
		return nil, fmt.Errorf("id is required")
	}
	run, err := h.runtime.store.GetRun(req.ID)
	if err != nil {
		return nil, err
	}
	if run == nil {
		return nil, fmt.Errorf("run not found: %s", req.ID)
	}

	msgs, err := h.runtime.store.ListRunMessages(req.ID)
	if err != nil {
		return nil, err
	}

	result := map[string]any{
		"run":      run,
		"messages": msgs,
	}
	return json.Marshal(result)
}

func (h *Handler) handleRunCancel(_ context.Context, input json.RawMessage) (json.RawMessage, error) {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(input, &req); err != nil || req.ID == "" {
		return nil, fmt.Errorf("id is required")
	}
	if err := h.runtime.store.FinishRun(req.ID, RunStatusCancelled); err != nil {
		return nil, err
	}
	return jsonResult("status", "cancelled"), nil
}

func (h *Handler) handleAgentList(_ context.Context) (json.RawMessage, error) {
	agents, err := h.runtime.store.ListAgents()
	if err != nil {
		return nil, err
	}
	if agents == nil {
		agents = []Agent{}
	}
	// Mask API keys
	for i := range agents {
		agents[i].APIKey = maskKey(agents[i].APIKey)
	}
	return json.Marshal(map[string]any{"agents": agents})
}

func (h *Handler) handleAgentGet(_ context.Context, input json.RawMessage) (json.RawMessage, error) {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(input, &req); err != nil || req.ID == "" {
		return nil, fmt.Errorf("id is required")
	}
	agt, err := h.runtime.store.GetAgent(req.ID)
	if err != nil {
		return nil, err
	}
	if agt == nil {
		return nil, fmt.Errorf("agent not found: %s", req.ID)
	}
	agt.APIKey = maskKey(agt.APIKey)
	return json.Marshal(agt)
}

func (h *Handler) handleAgentCreate(_ context.Context, input json.RawMessage) (json.RawMessage, error) {
	var agt Agent
	if err := json.Unmarshal(input, &agt); err != nil {
		return nil, fmt.Errorf("parse agent: %w", err)
	}
	if agt.Name == "" {
		agt.Name = "New Agent"
	}
	if agt.Temperature == 0 {
		agt.Temperature = 0.3
	}
	if agt.MaxTokens == 0 {
		agt.MaxTokens = 8192
	}
	if err := h.runtime.store.SaveAgent(&agt); err != nil {
		return nil, err
	}
	return json.Marshal(agt)
}

func (h *Handler) handleAgentUpdate(_ context.Context, input json.RawMessage) (json.RawMessage, error) {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(input, &req); err != nil || req.ID == "" {
		return nil, fmt.Errorf("id is required")
	}

	agt, err := h.runtime.store.GetAgent(req.ID)
	if err != nil || agt == nil {
		return nil, fmt.Errorf("agent not found: %s", req.ID)
	}

	// Merge updates
	var updates map[string]json.RawMessage
	if err := json.Unmarshal(input, &updates); err != nil {
		return nil, err
	}

	if v, ok := updates["name"]; ok {
		json.Unmarshal(v, &agt.Name)
	}
	if v, ok := updates["llm_provider"]; ok {
		json.Unmarshal(v, &agt.LLMProvider)
	}
	if v, ok := updates["llm_model"]; ok {
		json.Unmarshal(v, &agt.LLMModel)
	}
	if v, ok := updates["base_url"]; ok {
		json.Unmarshal(v, &agt.BaseURL)
	}
	if v, ok := updates["api_key"]; ok {
		json.Unmarshal(v, &agt.APIKey)
	}
	if v, ok := updates["system_prompt"]; ok {
		json.Unmarshal(v, &agt.SystemPrompt)
	}
	if v, ok := updates["temperature"]; ok {
		json.Unmarshal(v, &agt.Temperature)
	}
	if v, ok := updates["max_tokens"]; ok {
		json.Unmarshal(v, &agt.MaxTokens)
	}
	if v, ok := updates["enable_reasoning"]; ok {
		json.Unmarshal(v, &agt.EnableReasoning)
	}
	if v, ok := updates["scope"]; ok {
		json.Unmarshal(v, &agt.Scope)
	}
	if v, ok := updates["pinned"]; ok {
		json.Unmarshal(v, &agt.Pinned)
	}

	if err := h.runtime.store.SaveAgent(agt); err != nil {
		return nil, err
	}
	return json.Marshal(agt)
}

func (h *Handler) handleAgentDelete(_ context.Context, input json.RawMessage) (json.RawMessage, error) {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(input, &req); err != nil || req.ID == "" {
		return nil, fmt.Errorf("id is required")
	}
	if err := h.runtime.store.DeleteAgent(req.ID); err != nil {
		return nil, err
	}
	return jsonResult("status", "deleted"), nil
}

func (h *Handler) handleEventCreate(_ context.Context, input json.RawMessage) (json.RawMessage, error) {
	var evt Event
	if err := json.Unmarshal(input, &evt); err != nil {
		return nil, fmt.Errorf("parse event: %w", err)
	}
	if err := h.runtime.store.SaveEvent(&evt); err != nil {
		return nil, err
	}
	return json.Marshal(evt)
}

func (h *Handler) handleEventList(_ context.Context, input json.RawMessage) (json.RawMessage, error) {
	var req struct {
		AgentID string `json:"agent_id"`
	}
	_ = json.Unmarshal(input, &req)

	if req.AgentID == "" {
		agents, _ := h.runtime.store.ListAgents()
		if len(agents) > 0 {
			req.AgentID = agents[0].ID
		}
	}

	events, err := h.runtime.store.ListEvents(req.AgentID)
	if err != nil {
		return nil, err
	}
	if events == nil {
		events = []Event{}
	}
	return json.Marshal(map[string]any{"events": events})
}

func (h *Handler) handleEventUpdate(_ context.Context, input json.RawMessage) (json.RawMessage, error) {
	var evt Event
	if err := json.Unmarshal(input, &evt); err != nil {
		return nil, fmt.Errorf("parse event: %w", err)
	}
	if evt.ID == "" {
		return nil, fmt.Errorf("id is required")
	}
	if err := h.runtime.store.SaveEvent(&evt); err != nil {
		return nil, err
	}
	return json.Marshal(evt)
}

func (h *Handler) handleEventCancel(_ context.Context, input json.RawMessage) (json.RawMessage, error) {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(input, &req); err != nil || req.ID == "" {
		return nil, fmt.Errorf("id is required")
	}
	// Load and update status
	evt := &Event{
		ID:     req.ID,
		Status: EventStatusCancelled,
	}
	if err := h.runtime.store.SaveEvent(evt); err != nil {
		return nil, err
	}
	return jsonResult("status", "cancelled"), nil
}

func (h *Handler) handleConfigGet(_ context.Context, input json.RawMessage) (json.RawMessage, error) {
	agents, err := h.runtime.store.ListAgents()
	if err != nil {
		return nil, err
	}
	if len(agents) == 0 {
		return json.Marshal(map[string]any{"agents": []any{}})
	}
	// Mask API keys
	for i := range agents {
		agents[i].APIKey = maskKey(agents[i].APIKey)
	}
	return json.Marshal(map[string]any{"agents": agents})
}

// --- Helpers ---

func jsonResult(key, value string) json.RawMessage {
	b, _ := json.Marshal(map[string]string{key: value})
	return b
}

func maskKey(key string) string {
	if len(key) <= 8 {
		return "***"
	}
	return key[:4] + "..." + key[len(key)-4:]
}

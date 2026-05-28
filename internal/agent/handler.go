// Role:    Virtual clip command handler — routes Hub invoke requests to agent runtime methods
// Depends: context, encoding/json, fmt, strings
// Exports: Handler, NewHandler

package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Handler processes invoke requests for the "agent" virtual clip.
type Handler struct {
	runtime   *Runtime
	resources map[string]*resourceDef
}

// NewHandler creates a new agent handler.
func NewHandler(rt *Runtime) *Handler {
	h := &Handler{runtime: rt}
	h.resources = h.buildResources()
	return h
}

// --- Resource routing ---

// resourceDef describes a resource and its supported verbs.
type resourceDef struct {
	name        string
	description string
	verbs       map[string]verbHandler
}

// verbHandler handles a resource verb. id is empty for collection verbs (list, create).
type verbHandler struct {
	description string
	needsID     bool
	handler     func(ctx context.Context, id string, input json.RawMessage) (json.RawMessage, error)
}

// parsedCommand is the result of parsing a command string.
type parsedCommand struct {
	resource string // e.g. "agents"
	id       string // e.g. "agt_123" (empty for collection verbs)
	verb     string // e.g. "list", "get", "update"
}

// parseResourceCommand parses "/agents list", "/agents/agt_123 get", etc.
func parseResourceCommand(command string) (parsedCommand, bool) {
	if !strings.HasPrefix(command, "/") {
		return parsedCommand{}, false
	}

	// Split into path and verb: "/agents/agt_123 get" → path="/agents/agt_123", verb="get"
	parts := strings.SplitN(command, " ", 2)
	if len(parts) != 2 {
		return parsedCommand{}, false
	}
	path := strings.TrimPrefix(parts[0], "/")
	verb := strings.TrimSpace(parts[1])
	if path == "" || verb == "" {
		return parsedCommand{}, false
	}

	// Split path: "agents/agt_123" → resource="agents", id="agt_123"
	// Or just "agents" → resource="agents", id=""
	segments := strings.SplitN(path, "/", 2)
	pc := parsedCommand{
		resource: segments[0],
		verb:     verb,
	}
	if len(segments) == 2 {
		pc.id = segments[1]
	}
	return pc, true
}

// HandleInvoke processes a command for the agent virtual clip.
// Commands are either:
//   - Resource commands: "/agents list", "/agents/:id get", "/topics/:id update"
//   - Action commands: "chat", "get-run", "cancel-run", "cancel-event"
//   - Help: "--help", "/agents --help"
func (h *Handler) HandleInvoke(ctx context.Context, command string, input json.RawMessage, onChunk func(json.RawMessage)) (json.RawMessage, error) {
	command = strings.TrimSpace(command)

	// Global help
	if command == "--help" || command == "help" {
		return h.globalHelp()
	}

	// Resource help: "/agents --help"
	if strings.HasPrefix(command, "/") && strings.HasSuffix(command, "--help") {
		resName := strings.TrimPrefix(command, "/")
		resName = strings.TrimSuffix(resName, " --help")
		resName = strings.TrimSpace(resName)
		// Strip any ID segment for help
		if idx := strings.Index(resName, "/"); idx >= 0 {
			resName = resName[:idx]
		}
		return h.resourceHelp(resName)
	}

	// Resource commands: /agents list, /agents/agt_123 get, etc.
	if pc, ok := parseResourceCommand(command); ok {
		return h.dispatchResource(ctx, pc, input)
	}

	// Action commands
	switch command {
	case "chat":
		return h.handleChat(ctx, input, onChunk)
	case "get-run":
		return h.handleGetRun(ctx, input)
	case "cancel-run":
		return h.handleCancelRun(ctx, input)
	case "cancel-event":
		return h.handleCancelEvent(ctx, input)
	}

	// Legacy command compatibility (topic list → /topics list, etc.)
	if newCmd, ok := legacyCommandMap[command]; ok {
		return h.HandleInvoke(ctx, newCmd, input, onChunk)
	}

	return nil, fmt.Errorf("unknown command: %s (use --help to see available commands)", command)
}

// legacyCommandMap maps old "noun verb" commands to new "/resource verb" format.
var legacyCommandMap = map[string]string{
	"topic list":   "/topics list",
	"topic get":    "/topics/:id get",   // id from input
	"topic create": "/topics create",
	"topic delete": "/topics/:id delete", // id from input
	"topic rename": "/topics/:id update", // id from input
	"topic search": "/topics list",       // query from input
	"run get":      "get-run",
	"run cancel":   "cancel-run",
	"agent list":   "/agents list",
	"agent get":    "/agents/:id get",
	"agent create": "/agents create",
	"agent update": "/agents/:id update",
	"agent delete": "/agents/:id delete",
	"event create": "/events create",
	"event list":   "/events list",
	"event update": "/events/:id update",
	"event cancel": "cancel-event",
	"config get":   "/agents list",
}

// dispatchResource routes a parsed resource command to the right handler.
func (h *Handler) dispatchResource(ctx context.Context, pc parsedCommand, input json.RawMessage) (json.RawMessage, error) {
	res, ok := h.resources[pc.resource]
	if !ok {
		return nil, fmt.Errorf("unknown resource: %s (use --help to see available resources)", pc.resource)
	}

	vh, ok := res.verbs[pc.verb]
	if !ok {
		verbs := make([]string, 0, len(res.verbs))
		for v := range res.verbs {
			verbs = append(verbs, v)
		}
		return nil, fmt.Errorf("resource %q does not support verb %q (available: %s)", pc.resource, pc.verb, strings.Join(verbs, ", "))
	}

	id := pc.id
	if id == "" && vh.needsID {
		// Try to extract ID from input JSON (legacy compat)
		id = extractID(input)
		if id == "" {
			return nil, fmt.Errorf("/%s/:id %s requires an ID in the path or input", pc.resource, pc.verb)
		}
	}

	return vh.handler(ctx, id, input)
}

// extractID pulls "id" from a JSON input object.
func extractID(input json.RawMessage) string {
	var obj struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(input, &obj)
	return obj.ID
}

// --- Help system ---

func (h *Handler) globalHelp() (json.RawMessage, error) {
	var b strings.Builder
	b.WriteString("Agent Runtime — Built-in AI Agent\n\n")

	b.WriteString("Action Commands:\n")
	b.WriteString("  chat             Send a message and get a streaming response\n")
	b.WriteString("  get-run          Get run details with messages\n")
	b.WriteString("  cancel-run       Cancel a running run\n")
	b.WriteString("  cancel-event     Cancel a scheduled event\n\n")

	b.WriteString("Resources:\n")
	for _, name := range []string{"agents", "topics", "events"} {
		res := h.resources[name]
		verbs := make([]string, 0, len(res.verbs))
		for v := range res.verbs {
			verbs = append(verbs, v)
		}
		b.WriteString(fmt.Sprintf("  /%s — %s [%s]\n", res.name, res.description, strings.Join(verbs, ", ")))
	}
	b.WriteString("\nUse /<resource> --help for details on a specific resource.\n")

	return json.Marshal(map[string]string{"help": b.String()})
}

func (h *Handler) resourceHelp(name string) (json.RawMessage, error) {
	res, ok := h.resources[name]
	if !ok {
		return nil, fmt.Errorf("unknown resource: %s", name)
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("/%s — %s\n\n", res.name, res.description))
	b.WriteString("Verbs:\n")
	for verb, vh := range res.verbs {
		if vh.needsID {
			b.WriteString(fmt.Sprintf("  /%s/:id %s — %s\n", res.name, verb, vh.description))
		} else {
			b.WriteString(fmt.Sprintf("  /%s %s — %s\n", res.name, verb, vh.description))
		}
	}
	return json.Marshal(map[string]string{"help": b.String()})
}

// --- Resource definitions ---

func (h *Handler) buildResources() map[string]*resourceDef {
	return map[string]*resourceDef{
		"agents": {
			name:        "agents",
			description: "Agent configurations (LLM provider, model, API key, system prompt)",
			verbs: map[string]verbHandler{
				"list":   {description: "List all agents", handler: h.agentList},
				"get":    {description: "Get agent details", needsID: true, handler: h.agentGet},
				"create": {description: "Create a new agent", handler: h.agentCreate},
				"update": {description: "Update agent configuration", needsID: true, handler: h.agentUpdate},
				"delete": {description: "Delete an agent", needsID: true, handler: h.agentDelete},
			},
		},
		"topics": {
			name:        "topics",
			description: "Conversation threads",
			verbs: map[string]verbHandler{
				"list":   {description: "List topics (supports query param for search)", handler: h.topicList},
				"get":    {description: "Get topic with messages", needsID: true, handler: h.topicGet},
				"create": {description: "Create a new topic", handler: h.topicCreate},
				"update": {description: "Update topic (e.g. title)", needsID: true, handler: h.topicUpdate},
				"delete": {description: "Delete a topic", needsID: true, handler: h.topicDelete},
			},
		},
		"events": {
			name:        "events",
			description: "Scheduled triggers for automated runs",
			verbs: map[string]verbHandler{
				"list":   {description: "List scheduled events", handler: h.eventList},
				"get":    {description: "Get event details", needsID: true, handler: h.eventGet},
				"create": {description: "Create a scheduled event", handler: h.eventCreate},
				"update": {description: "Update a scheduled event", needsID: true, handler: h.eventUpdate},
			},
		},
	}
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

// --- Action commands ---

func (h *Handler) handleGetRun(_ context.Context, input json.RawMessage) (json.RawMessage, error) {
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

	return json.Marshal(map[string]any{"run": run, "messages": msgs})
}

func (h *Handler) handleCancelRun(_ context.Context, input json.RawMessage) (json.RawMessage, error) {
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

func (h *Handler) handleCancelEvent(_ context.Context, input json.RawMessage) (json.RawMessage, error) {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(input, &req); err != nil || req.ID == "" {
		return nil, fmt.Errorf("id is required")
	}
	evt := &Event{ID: req.ID, Status: EventStatusCancelled}
	if err := h.runtime.store.SaveEvent(evt); err != nil {
		return nil, err
	}
	return jsonResult("status", "cancelled"), nil
}

// --- Resource handlers: agents ---

func (h *Handler) agentList(_ context.Context, _ string, _ json.RawMessage) (json.RawMessage, error) {
	agents, err := h.runtime.store.ListAgents()
	if err != nil {
		return nil, err
	}
	if agents == nil {
		agents = []Agent{}
	}
	for i := range agents {
		agents[i].APIKey = maskKey(agents[i].APIKey)
	}
	return json.Marshal(map[string]any{"agents": agents})
}

func (h *Handler) agentGet(_ context.Context, id string, _ json.RawMessage) (json.RawMessage, error) {
	agt, err := h.runtime.store.GetAgent(id)
	if err != nil {
		return nil, err
	}
	if agt == nil {
		return nil, fmt.Errorf("agent not found: %s", id)
	}
	agt.APIKey = maskKey(agt.APIKey)
	return json.Marshal(agt)
}

func (h *Handler) agentCreate(_ context.Context, _ string, input json.RawMessage) (json.RawMessage, error) {
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

func (h *Handler) agentUpdate(_ context.Context, id string, input json.RawMessage) (json.RawMessage, error) {
	agt, err := h.runtime.store.GetAgent(id)
	if err != nil || agt == nil {
		return nil, fmt.Errorf("agent not found: %s", id)
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

func (h *Handler) agentDelete(_ context.Context, id string, _ json.RawMessage) (json.RawMessage, error) {
	if err := h.runtime.store.DeleteAgent(id); err != nil {
		return nil, err
	}
	return jsonResult("status", "deleted"), nil
}

// --- Resource handlers: topics ---

func (h *Handler) topicList(_ context.Context, _ string, input json.RawMessage) (json.RawMessage, error) {
	var req struct {
		AgentID string `json:"agent_id"`
		Query   string `json:"query"`
	}
	_ = json.Unmarshal(input, &req)

	// Search mode: query across all topics
	if req.Query != "" {
		msgs, err := h.runtime.store.SearchMessages(req.Query, 20)
		if err != nil {
			return nil, err
		}
		if msgs == nil {
			msgs = []Message{}
		}
		return json.Marshal(map[string]any{"messages": msgs})
	}

	// List mode: topics for an agent
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

func (h *Handler) topicGet(_ context.Context, id string, _ json.RawMessage) (json.RawMessage, error) {
	topic, err := h.runtime.store.GetTopic(id)
	if err != nil {
		return nil, err
	}
	if topic == nil {
		return nil, fmt.Errorf("topic not found: %s", id)
	}

	msgs, err := h.runtime.store.ListMessages(topic.ID)
	if err != nil {
		return nil, err
	}
	if msgs == nil {
		msgs = []Message{}
	}

	return json.Marshal(map[string]any{"topic": topic, "messages": msgs})
}

func (h *Handler) topicCreate(_ context.Context, _ string, input json.RawMessage) (json.RawMessage, error) {
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

	topic := &Topic{AgentID: req.AgentID, Title: req.Title}
	if err := h.runtime.store.SaveTopic(topic); err != nil {
		return nil, err
	}
	return json.Marshal(topic)
}

func (h *Handler) topicUpdate(_ context.Context, id string, input json.RawMessage) (json.RawMessage, error) {
	var req struct {
		Title string `json:"title"`
	}
	_ = json.Unmarshal(input, &req)

	if err := h.runtime.store.UpdateTopicTitle(id, req.Title); err != nil {
		return nil, err
	}
	return jsonResult("status", "updated"), nil
}

func (h *Handler) topicDelete(_ context.Context, id string, _ json.RawMessage) (json.RawMessage, error) {
	if err := h.runtime.store.DeleteTopic(id); err != nil {
		return nil, err
	}
	return jsonResult("status", "deleted"), nil
}

// --- Resource handlers: events ---

func (h *Handler) eventList(_ context.Context, _ string, input json.RawMessage) (json.RawMessage, error) {
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

func (h *Handler) eventGet(_ context.Context, id string, _ json.RawMessage) (json.RawMessage, error) {
	events, err := h.runtime.store.ListEvents("")
	if err != nil {
		return nil, err
	}
	for _, evt := range events {
		if evt.ID == id {
			return json.Marshal(evt)
		}
	}
	return nil, fmt.Errorf("event not found: %s", id)
}

func (h *Handler) eventCreate(_ context.Context, _ string, input json.RawMessage) (json.RawMessage, error) {
	var evt Event
	if err := json.Unmarshal(input, &evt); err != nil {
		return nil, fmt.Errorf("parse event: %w", err)
	}
	if err := h.runtime.store.SaveEvent(&evt); err != nil {
		return nil, err
	}
	return json.Marshal(evt)
}

func (h *Handler) eventUpdate(_ context.Context, id string, input json.RawMessage) (json.RawMessage, error) {
	var evt Event
	if err := json.Unmarshal(input, &evt); err != nil {
		return nil, fmt.Errorf("parse event: %w", err)
	}
	evt.ID = id
	if err := h.runtime.store.SaveEvent(&evt); err != nil {
		return nil, err
	}
	return json.Marshal(evt)
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

// Role:    LLM streaming client for OpenAI-compatible and Anthropic APIs
// Depends: bufio, bytes, context, encoding/json, fmt, io, net/http, strings
// Exports: LLMClient, NewLLMClient, ChatStream, GenerateTitle

package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// LLMClient calls an LLM API for chat completions.
type LLMClient struct {
	httpClient *http.Client
}

// NewLLMClient creates a new LLM client.
func NewLLMClient() *LLMClient {
	return &LLMClient{
		httpClient: &http.Client{},
	}
}

// ChatStreamInput is the input to a streaming chat completion call.
type ChatStreamInput struct {
	BaseURL         string
	APIKey          string
	Model           string
	Messages        []LLMMessage
	Tools           []LLMTool
	Temperature     float64
	MaxTokens       int
	EnableReasoning bool
	OnDelta         func(StreamEvent) // called for each streaming chunk
	OnRequest       func(body []byte) // debug: called with serialized request body before sending
}

// ChatStream performs a streaming chat completion and returns the accumulated result.
func (c *LLMClient) ChatStream(ctx context.Context, input ChatStreamInput) (*LLMResult, error) {
	body := buildChatRequest(input)

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	if input.OnRequest != nil {
		input.OnRequest(jsonBody)
	}

	url := strings.TrimRight(input.BaseURL, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+input.APIKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("LLM API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	return readSSEStream(ctx, resp.Body, input.OnDelta)
}

// GenerateTitle generates a short topic title from user and assistant text.
func (c *LLMClient) GenerateTitle(ctx context.Context, baseURL, apiKey, model, userText, assistantText string) (string, error) {
	messages := []LLMMessage{
		{Role: RoleSystem, Content: "根据对话内容生成一个简短的标题，不超过10个字，只输出标题文字，不要加引号或标点。"},
		{Role: RoleUser, Content: fmt.Sprintf("用户：%s\n助手：%s", userText, truncate(assistantText, 200))},
	}

	body := map[string]any{
		"model":      model,
		"messages":   messages,
		"max_tokens": 30,
		"stream":     false,
	}
	// Disable thinking for title generation
	body["thinking"] = map[string]any{"type": "disabled"}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return "", err
	}

	url := strings.TrimRight(baseURL, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("title generation failed: status %d", resp.StatusCode)
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	if len(result.Choices) > 0 {
		return strings.TrimSpace(result.Choices[0].Message.Content), nil
	}
	return "", fmt.Errorf("no choices returned")
}

// --- Internal ---

func buildChatRequest(input ChatStreamInput) map[string]any {
	body := map[string]any{
		"model":    input.Model,
		"messages": input.Messages,
		"stream":   true,
	}
	if input.Temperature > 0 {
		body["temperature"] = input.Temperature
	}
	if input.MaxTokens > 0 {
		body["max_tokens"] = input.MaxTokens
	}
	if len(input.Tools) > 0 {
		body["tools"] = input.Tools
	}
	if input.EnableReasoning {
		// OpenRouter / DeepSeek reasoning toggle
		body["reasoning"] = map[string]any{"enabled": true}
	}
	return body
}

// readSSEStream reads an SSE stream and accumulates the result.
func readSSEStream(ctx context.Context, reader io.Reader, onDelta func(StreamEvent)) (*LLMResult, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024) // 1MB max line

	result := &LLMResult{}
	toolChunks := make(map[int]*toolChunk)

	var eventLines []string
	for scanner.Scan() {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}

		line := scanner.Text()

		// SSE: blank line = end of event
		if line == "" {
			if len(eventLines) > 0 {
				processSSEEvent(eventLines, result, toolChunks, onDelta)
				eventLines = eventLines[:0]
			}
			continue
		}
		eventLines = append(eventLines, line)
	}
	// Process any remaining event
	if len(eventLines) > 0 {
		processSSEEvent(eventLines, result, toolChunks, onDelta)
	}

	// Convert accumulated tool chunks to ToolCalls
	for i := 0; i < len(toolChunks); i++ {
		if tc, ok := toolChunks[i]; ok {
			result.ToolCalls = append(result.ToolCalls, ToolCall{
				ID:        tc.id,
				Name:      tc.name,
				Arguments: tc.arguments.String(),
			})
		}
	}

	return result, scanner.Err()
}

type toolChunk struct {
	id        string
	name      string
	arguments strings.Builder
}

func processSSEEvent(lines []string, result *LLMResult, toolChunks map[int]*toolChunk, onDelta func(StreamEvent)) {
	for _, line := range lines {
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			return
		}

		var chunk sseChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		if len(chunk.Choices) == 0 {
			// Check for usage-only message
			if chunk.Usage != nil {
				result.Usage = &TokenUsage{
					PromptTokens:     chunk.Usage.PromptTokens,
					CompletionTokens: chunk.Usage.CompletionTokens,
					TotalTokens:      chunk.Usage.TotalTokens,
				}
				if onDelta != nil {
					onDelta(StreamEvent{
						Type:             StreamEventUsage,
						PromptTokens:     chunk.Usage.PromptTokens,
						CompletionTokens: chunk.Usage.CompletionTokens,
					})
				}
			}
			continue
		}

		delta := chunk.Choices[0].Delta

		// Content
		if delta.Content != "" {
			result.Content += delta.Content
			if onDelta != nil {
				onDelta(StreamEvent{Type: StreamEventText, Content: delta.Content})
			}
		}

		// Reasoning — three formats:
		// 1. reasoning_content (DeepSeek R1/V4)
		if delta.ReasoningContent != "" {
			result.Reasoning += delta.ReasoningContent
			if onDelta != nil {
				onDelta(StreamEvent{Type: StreamEventThinking, Content: delta.ReasoningContent})
			}
		}
		// 2. reasoning (OpenRouter shorthand)
		if delta.Reasoning != "" {
			result.Reasoning += delta.Reasoning
			if onDelta != nil {
				onDelta(StreamEvent{Type: StreamEventThinking, Content: delta.Reasoning})
			}
		}
		// 3. reasoning_details (OpenRouter unified)
		for _, detail := range delta.ReasoningDetails {
			if detail.Type == "text" && detail.Text != "" {
				result.Reasoning += detail.Text
				if onDelta != nil {
					onDelta(StreamEvent{Type: StreamEventThinking, Content: detail.Text})
				}
			}
		}

		// Tool calls
		for _, tc := range delta.ToolCalls {
			chunk, ok := toolChunks[tc.Index]
			if !ok {
				chunk = &toolChunk{}
				toolChunks[tc.Index] = chunk
			}
			if tc.ID != "" {
				chunk.id = tc.ID
			}
			if tc.Function.Name != "" {
				chunk.name = tc.Function.Name
			}
			if tc.Function.Arguments != "" {
				chunk.arguments.WriteString(tc.Function.Arguments)
			}
		}

		// Usage in choice-level
		if chunk.Usage != nil && result.Usage == nil {
			result.Usage = &TokenUsage{
				PromptTokens:     chunk.Usage.PromptTokens,
				CompletionTokens: chunk.Usage.CompletionTokens,
				TotalTokens:      chunk.Usage.TotalTokens,
			}
		}
	}
}

// --- SSE chunk types ---

type sseChunk struct {
	Choices []sseChoice    `json:"choices"`
	Usage   *sseUsage      `json:"usage,omitempty"`
}

type sseChoice struct {
	Delta sseDelta `json:"delta"`
}

type sseDelta struct {
	Content          string             `json:"content,omitempty"`
	ReasoningContent string             `json:"reasoning_content,omitempty"`
	Reasoning        string             `json:"reasoning,omitempty"`
	ReasoningDetails []sseReasonDetail  `json:"reasoning_details,omitempty"`
	ToolCalls        []sseToolCallDelta `json:"tool_calls,omitempty"`
}

type sseReasonDetail struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type sseToolCallDelta struct {
	Index    int    `json:"index"`
	ID       string `json:"id,omitempty"`
	Function struct {
		Name      string `json:"name,omitempty"`
		Arguments string `json:"arguments,omitempty"`
	} `json:"function"`
}

type sseUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

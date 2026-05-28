// Role:    Bridge between Hub streaming and bubbletea messages
// Depends: context, encoding/json, fmt, internal/client, internal/agent
// Exports: StreamTokenMsg, StreamThinkingMsg, StreamToolCallMsg, StreamToolResultMsg, StreamDoneMsg, StreamErrorMsg, StartStream

package tui

import (
	"context"
	"encoding/json"
	"fmt"

	tea "charm.land/bubbletea/v2"
	"github.com/epiral/pinix/internal/agent"
	"github.com/epiral/pinix/internal/client"
)

// Stream event messages sent to bubbletea.

// StreamTokenMsg carries a text token from the assistant.
type StreamTokenMsg struct {
	Content string
}

// StreamThinkingMsg carries thinking/reasoning content.
type StreamThinkingMsg struct {
	Content string
}

// StreamToolCallMsg indicates a tool call started.
type StreamToolCallMsg struct {
	ID   string
	Name string
	Args string
}

// StreamToolResultMsg carries a tool execution result.
type StreamToolResultMsg struct {
	ID      string
	Name    string
	Content string
}

// StreamDoneMsg indicates the stream has completed.
type StreamDoneMsg struct{}

// StreamErrorMsg indicates a stream error.
type StreamErrorMsg struct {
	Err error
}

// StartStream opens a streaming invoke to the Hub and sends bubbletea messages.
// It returns a tea.Cmd that should be started via p.Send indirectly (called in Update).
func StartStream(cli *client.Client, agentID, topicID, message, hubToken string) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()

		input, _ := json.Marshal(map[string]string{
			"agent_id": agentID,
			"topic_id": topicID,
			"message":  message,
		})

		stream, err := cli.OpenInvoke(ctx, "agent", "chat", input, "", hubToken)
		if err != nil {
			return StreamErrorMsg{Err: fmt.Errorf("open invoke: %w", err)}
		}
		defer stream.Close()

		for stream.Receive() {
			output := stream.Msg().GetOutput()
			if len(output) == 0 {
				continue
			}

			// Check for hub-level error
			if hubErr := stream.Msg().GetError(); hubErr != nil {
				return StreamErrorMsg{Err: fmt.Errorf("%s (%s)", hubErr.GetMessage(), hubErr.GetCode())}
			}

			var evt agent.StreamEvent
			if err := json.Unmarshal(output, &evt); err != nil {
				continue
			}

			switch evt.Type {
			case agent.StreamEventText:
				return StreamTokenMsg{Content: evt.Content}
			case agent.StreamEventThinking:
				return StreamThinkingMsg{Content: evt.Content}
			case agent.StreamEventToolCall:
				return StreamToolCallMsg{
					ID:   evt.ID,
					Name: evt.Name,
					Args: evt.Arguments,
				}
			case agent.StreamEventToolResult:
				return StreamToolResultMsg{
					ID:      evt.ID,
					Name:    evt.Name,
					Content: evt.Content,
				}
			case agent.StreamEventDone:
				return StreamDoneMsg{}
			case agent.StreamEventError:
				return StreamErrorMsg{Err: fmt.Errorf("agent error: %s", evt.Error)}
			}
		}

		if err := stream.Err(); err != nil {
			return StreamErrorMsg{Err: err}
		}

		return StreamDoneMsg{}
	}
}

// ContinueStream continues reading from the same Hub stream after yielding one message.
// The Hub uses server-streaming: each Receive() yields a chunk.
// We need a different approach — read all chunks in a goroutine and send them via a channel.

// StreamSession manages reading from a Hub stream and sending bubbletea messages.
type StreamSession struct {
	msgCh  chan tea.Msg
	cancel context.CancelFunc
}

// NewStreamSession opens a streaming invoke and reads all events into a channel.
func NewStreamSession(cli *client.Client, agentID, topicID, message, hubToken string) *StreamSession {
	ch := make(chan tea.Msg, 64)
	ctx, cancel := context.WithCancel(context.Background())

	s := &StreamSession{
		msgCh:  ch,
		cancel: cancel,
	}

	go func() {
		defer close(ch)

		input, _ := json.Marshal(map[string]string{
			"agent_id": agentID,
			"topic_id": topicID,
			"message":  message,
		})

		stream, err := cli.OpenInvoke(ctx, "agent", "chat", input, "", hubToken)
		if err != nil {
			ch <- StreamErrorMsg{Err: fmt.Errorf("open invoke: %w", err)}
			return
		}
		defer stream.Close()

		for stream.Receive() {
			// Check for hub-level error
			if hubErr := stream.Msg().GetError(); hubErr != nil {
				ch <- StreamErrorMsg{Err: fmt.Errorf("%s (%s)", hubErr.GetMessage(), hubErr.GetCode())}
				return
			}

			output := stream.Msg().GetOutput()
			if len(output) == 0 {
				continue
			}

			var evt agent.StreamEvent
			if err := json.Unmarshal(output, &evt); err != nil {
				continue
			}

			var msg tea.Msg
			switch evt.Type {
			case agent.StreamEventText:
				msg = StreamTokenMsg{Content: evt.Content}
			case agent.StreamEventThinking:
				msg = StreamThinkingMsg{Content: evt.Content}
			case agent.StreamEventToolCall:
				msg = StreamToolCallMsg{
					ID:   evt.ID,
					Name: evt.Name,
					Args: evt.Arguments,
				}
			case agent.StreamEventToolResult:
				msg = StreamToolResultMsg{
					ID:      evt.ID,
					Name:    evt.Name,
					Content: evt.Content,
				}
			case agent.StreamEventDone:
				msg = StreamDoneMsg{}
			case agent.StreamEventError:
				msg = StreamErrorMsg{Err: fmt.Errorf("agent error: %s", evt.Error)}
			case agent.StreamEventUsage:
				continue
			default:
				continue
			}

			select {
			case ch <- msg:
			case <-ctx.Done():
				return
			}
		}

		if err := stream.Err(); err != nil {
			select {
			case ch <- StreamErrorMsg{Err: err}:
			case <-ctx.Done():
			}
			return
		}

		// If we didn't get an explicit done event, send one
		select {
		case ch <- StreamDoneMsg{}:
		case <-ctx.Done():
		}
	}()

	return s
}

// NextMsg returns a tea.Cmd that waits for the next message from the stream.
func (s *StreamSession) NextMsg() tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-s.msgCh
		if !ok {
			return StreamDoneMsg{}
		}
		return msg
	}
}

// Cancel stops the streaming session.
func (s *StreamSession) Cancel() {
	if s.cancel != nil {
		s.cancel()
	}
}

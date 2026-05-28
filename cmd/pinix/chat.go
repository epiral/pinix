// Role:    Cobra command for `pinix chat` — interactive TUI chat with the Agent Runtime
// Depends: context, encoding/json, fmt, os, internal/client, internal/tui, cobra
// Exports: newChatCommand

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"
	"github.com/epiral/pinix/internal/agent"
	"github.com/epiral/pinix/internal/client"
	"github.com/epiral/pinix/internal/tui"
	"github.com/spf13/cobra"
)

func newChatCommand() *cobra.Command {
	var serverURL string
	var hubToken string
	var agentID string

	cmd := &cobra.Command{
		Use:   "chat",
		Short: "Interactive TUI chat with an AI agent",
		Long: `Opens a full-screen interactive chat with the Go Agent Runtime.

The agent streams responses with thinking, tool calls, and text.
Enter sends a message, Alt+Enter adds a newline, Ctrl+C cancels or quits.

Examples:
  pinix chat
  pinix chat --agent-id my-agent
  pinix chat --server http://127.0.0.1:9000`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runChat(serverURL, hubToken, agentID)
		},
	}

	cmd.Flags().StringVar(&serverURL, "server", defaultHubURL(), "Hub URL")
	cmd.Flags().StringVar(&hubToken, "auth-token", defaultHubToken(), "Hub auth token")
	cmd.Flags().StringVar(&agentID, "agent-id", "", "Agent ID (default: first available)")

	return cmd
}

func runChat(serverURL, hubToken, agentID string) error {
	cli, err := client.New(serverURL)
	if err != nil {
		return fmt.Errorf("connect to hub: %w", err)
	}

	// Resolve agent
	resolvedID, agentName, err := resolveAgent(cli, agentID, hubToken)
	if err != nil {
		return err
	}

	model := tui.NewModel(cli, resolvedID, agentName, hubToken)

	p, err := tea.NewProgram(model).Run()
	if err != nil {
		return fmt.Errorf("TUI error: %w", err)
	}

	// On exit, show topic ID if available
	if m, ok := p.(tui.Model); ok {
		if tid := m.TopicID(); tid != "" {
			fmt.Fprintf(os.Stderr, "Topic: %s\n", tid)
		}
	}

	return nil
}

// resolveAgent determines which agent to use. If agentID is empty, it lists
// agents via the Hub and picks the first one.
func resolveAgent(cli *client.Client, agentID, hubToken string) (id, name string, err error) {
	if agentID != "" {
		// Verify the agent exists
		input, _ := json.Marshal(map[string]string{"id": agentID})
		result, err := cli.Invoke(context.Background(), "agent", "agent get", input, "", hubToken)
		if err != nil {
			return "", "", fmt.Errorf("agent %q not found: %w", agentID, err)
		}
		var agt agent.Agent
		if err := json.Unmarshal(result, &agt); err != nil {
			return agentID, agentID, nil
		}
		return agt.ID, agt.Name, nil
	}

	// List agents and pick first
	result, err := cli.Invoke(context.Background(), "agent", "agent list", nil, "", hubToken)
	if err != nil {
		return "", "", fmt.Errorf("list agents: %w", err)
	}

	var agents []agent.Agent
	if err := json.Unmarshal(result, &agents); err != nil {
		return "", "", fmt.Errorf("parse agent list: %w", err)
	}
	if len(agents) == 0 {
		return "", "", fmt.Errorf("no agents configured; create one first via the Console")
	}

	return agents[0].ID, agents[0].Name, nil
}

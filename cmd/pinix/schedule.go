// Role:    Schedule subcommand group — manage scheduled Clip invocations via builtin scheduler Clip
// Depends: encoding/json, fmt, strings, internal/client, cobra
// Exports: newScheduleCommand

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/epiral/pinix/internal/client"
	"github.com/spf13/cobra"
)

const schedulerClipName = "scheduler"

func newScheduleCommand() *cobra.Command {
	var serverURL string
	var hubToken string

	cmd := &cobra.Command{
		Use:   "schedule",
		Short: "Manage scheduled Clip invocations",
		Long: `Create, list, and manage scheduled (cron) invocations of Clips.

Examples:
  pinix schedule list
  pinix schedule add morning-digest lark-im search --cron "0 9 * * *" --input '{"query":"unread"}'
  pinix schedule pause morning-digest
  pinix schedule resume morning-digest
  pinix schedule run morning-digest
  pinix schedule history morning-digest
  pinix schedule remove morning-digest`,
	}
	cmd.PersistentFlags().StringVar(&serverURL, "server", defaultHubURL(), "Hub URL (default: auto-discover)")
	cmd.PersistentFlags().StringVar(&hubToken, "auth-token", defaultHubToken(), "Hub auth token")

	cmd.AddCommand(newScheduleListCommand(&serverURL, &hubToken))
	cmd.AddCommand(newScheduleAddCommand(&serverURL, &hubToken))
	cmd.AddCommand(newScheduleRemoveCommand(&serverURL, &hubToken))
	cmd.AddCommand(newSchedulePauseCommand(&serverURL, &hubToken))
	cmd.AddCommand(newScheduleResumeCommand(&serverURL, &hubToken))
	cmd.AddCommand(newScheduleRunCommand(&serverURL, &hubToken))
	cmd.AddCommand(newScheduleHistoryCommand(&serverURL, &hubToken))

	return cmd
}

// invokeScheduler invokes the builtin scheduler Clip.
func invokeScheduler(ctx context.Context, serverURL, hubToken, command string, input any) (json.RawMessage, error) {
	cli, err := client.New(serverURL)
	if err != nil {
		return nil, err
	}
	inputJSON, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("marshal input: %w", err)
	}
	return cli.Invoke(ctx, schedulerClipName, command, inputJSON, "", hubToken)
}

func newScheduleListCommand(serverURL, hubToken *string) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all scheduled tasks",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			output, err := invokeScheduler(cmd.Context(), *serverURL, *hubToken, "list", map[string]any{})
			if err != nil {
				return err
			}
			var result struct {
				Schedules []struct {
					ID          string  `json:"id"`
					Clip        string  `json:"clip"`
					Command     string  `json:"command"`
					Cron        string  `json:"cron"`
					Enabled     bool    `json:"enabled"`
					NextRun     *string `json:"next_run"`
					LastSuccess *bool   `json:"last_success"`
				} `json:"schedules"`
			}
			if err := json.Unmarshal(output, &result); err != nil {
				return fmt.Errorf("parse response: %w", err)
			}
			if len(result.Schedules) == 0 {
				fmt.Println("(no scheduled tasks)")
				return nil
			}
			for _, s := range result.Schedules {
				status := "enabled"
				if !s.Enabled {
					status = "paused"
				}
				nextRun := "-"
				if s.NextRun != nil {
					nextRun = *s.NextRun
				}
				lastResult := "-"
				if s.LastSuccess != nil {
					if *s.LastSuccess {
						lastResult = "ok"
					} else {
						lastResult = "fail"
					}
				}
				fmt.Printf("%s\t%s\t%s\t%s\t%s\tnext:%s\tlast:%s\n",
					s.ID, s.Clip, s.Command, s.Cron, status, nextRun, lastResult)
			}
			return nil
		},
	}
}

func newScheduleAddCommand(serverURL, hubToken *string) *cobra.Command {
	var cronExpr string
	var input string
	var description string

	cmd := &cobra.Command{
		Use:   "add <id> <clip> <command>",
		Short: "Create a scheduled task",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			if cronExpr == "" {
				return fmt.Errorf("--cron is required")
			}
			_, err := invokeScheduler(cmd.Context(), *serverURL, *hubToken, "add", map[string]any{
				"id":          strings.TrimSpace(args[0]),
				"clip":        strings.TrimSpace(args[1]),
				"command":     strings.TrimSpace(args[2]),
				"cron":        cronExpr,
				"input":       input,
				"description": description,
			})
			if err != nil {
				return err
			}
			fmt.Printf("created schedule %q: %s %s [%s]\n", args[0], args[1], args[2], cronExpr)
			return nil
		},
	}
	cmd.Flags().StringVar(&cronExpr, "cron", "", "cron expression (required)")
	cmd.Flags().StringVar(&input, "input", "", "JSON input for the command")
	cmd.Flags().StringVar(&description, "desc", "", "human-readable description")
	return cmd
}

func newScheduleRemoveCommand(serverURL, hubToken *string) *cobra.Command {
	return &cobra.Command{
		Use:   "remove <id>",
		Short: "Remove a scheduled task",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := invokeScheduler(cmd.Context(), *serverURL, *hubToken, "remove", map[string]string{"id": strings.TrimSpace(args[0])})
			if err != nil {
				return err
			}
			fmt.Printf("removed schedule %q\n", args[0])
			return nil
		},
	}
}

func newSchedulePauseCommand(serverURL, hubToken *string) *cobra.Command {
	return &cobra.Command{
		Use:   "pause <id>",
		Short: "Pause a scheduled task",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := invokeScheduler(cmd.Context(), *serverURL, *hubToken, "pause", map[string]string{"id": strings.TrimSpace(args[0])})
			if err != nil {
				return err
			}
			fmt.Printf("paused schedule %q\n", args[0])
			return nil
		},
	}
}

func newScheduleResumeCommand(serverURL, hubToken *string) *cobra.Command {
	return &cobra.Command{
		Use:   "resume <id>",
		Short: "Resume a paused scheduled task",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := invokeScheduler(cmd.Context(), *serverURL, *hubToken, "resume", map[string]string{"id": strings.TrimSpace(args[0])})
			if err != nil {
				return err
			}
			fmt.Printf("resumed schedule %q\n", args[0])
			return nil
		},
	}
}

func newScheduleRunCommand(serverURL, hubToken *string) *cobra.Command {
	return &cobra.Command{
		Use:   "run <id>",
		Short: "Trigger a scheduled task immediately",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := invokeScheduler(cmd.Context(), *serverURL, *hubToken, "run", map[string]string{"id": strings.TrimSpace(args[0])})
			if err != nil {
				return err
			}
			fmt.Printf("triggered schedule %q\n", args[0])
			return nil
		},
	}
}

func newScheduleHistoryCommand(serverURL, hubToken *string) *cobra.Command {
	return &cobra.Command{
		Use:   "history <id>",
		Short: "Show execution history for a scheduled task",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			output, err := invokeScheduler(cmd.Context(), *serverURL, *hubToken, "history", map[string]string{"id": strings.TrimSpace(args[0])})
			if err != nil {
				return err
			}
			var result struct {
				Executions []struct {
					StartedAt  string `json:"started_at"`
					DurationMs int64  `json:"duration_ms"`
					Success    bool   `json:"success"`
					Error      string `json:"error"`
				} `json:"executions"`
			}
			if err := json.Unmarshal(output, &result); err != nil {
				return fmt.Errorf("parse response: %w", err)
			}
			if len(result.Executions) == 0 {
				fmt.Println("(no executions)")
				return nil
			}
			for _, e := range result.Executions {
				status := "ok"
				if !e.Success {
					status = "FAIL"
				}
				errMsg := ""
				if e.Error != "" {
					errMsg = " error=" + e.Error
				}
				fmt.Printf("%s  %dms  %s%s\n", e.StartedAt, e.DurationMs, status, errMsg)
			}
			return nil
		},
	}
}

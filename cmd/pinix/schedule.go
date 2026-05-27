// Role:    Schedule subcommand group — manage scheduled Clip invocations
// Depends: fmt, strings, internal/client, cobra
// Exports: newScheduleCommand

package main

import (
	"fmt"
	"strings"

	"github.com/epiral/pinix/internal/client"
	"github.com/spf13/cobra"
)

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

func newScheduleListCommand(serverURL, hubToken *string) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all scheduled tasks",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cli, err := client.New(*serverURL)
			if err != nil {
				return err
			}
			schedules, err := cli.ListSchedules(cmd.Context(), *hubToken)
			if err != nil {
				return err
			}
			if len(schedules) == 0 {
				fmt.Println("(no scheduled tasks)")
				return nil
			}
			for _, s := range schedules {
				rule := s.GetRule()
				status := "enabled"
				if !rule.GetEnabled() {
					status = "paused"
				}
				nextRun := "-"
				if s.GetNextRun() != "" {
					nextRun = s.GetNextRun()
				}
				lastResult := "-"
				if s.GetLastRun() != nil {
					if s.GetLastRun().GetSuccess() {
						lastResult = "ok"
					} else {
						lastResult = "fail"
					}
				}
				fmt.Printf("%s\t%s\t%s\t%s\t%s\tnext:%s\tlast:%s\n",
					rule.GetId(),
					rule.GetClip(),
					rule.GetCommand(),
					rule.GetCron(),
					status,
					nextRun,
					lastResult,
				)
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
		Long: `Create a new scheduled Clip invocation.

The id is a user-chosen name for this schedule (e.g., "morning-digest").
The cron expression follows standard cron format: minute hour day-of-month month day-of-week.

Examples:
  pinix schedule add morning-digest lark-im search --cron "0 9 * * *"
  pinix schedule add sync-check store push --cron "*/5 * * * *" --input '{"force":true}'`,
		Args: cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := strings.TrimSpace(args[0])
			clip := strings.TrimSpace(args[1])
			command := strings.TrimSpace(args[2])

			if cronExpr == "" {
				return fmt.Errorf("--cron is required")
			}

			cli, err := client.New(*serverURL)
			if err != nil {
				return err
			}
			rule, err := cli.AddSchedule(cmd.Context(), id, clip, command, cronExpr, input, description, *hubToken)
			if err != nil {
				return err
			}
			fmt.Printf("created schedule %q: %s %s [%s]\n", rule.GetId(), rule.GetClip(), rule.GetCommand(), rule.GetCron())
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
			id := strings.TrimSpace(args[0])
			cli, err := client.New(*serverURL)
			if err != nil {
				return err
			}
			if err := cli.RemoveSchedule(cmd.Context(), id, *hubToken); err != nil {
				return err
			}
			fmt.Printf("removed schedule %q\n", id)
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
			id := strings.TrimSpace(args[0])
			cli, err := client.New(*serverURL)
			if err != nil {
				return err
			}
			if err := cli.PauseSchedule(cmd.Context(), id, *hubToken); err != nil {
				return err
			}
			fmt.Printf("paused schedule %q\n", id)
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
			id := strings.TrimSpace(args[0])
			cli, err := client.New(*serverURL)
			if err != nil {
				return err
			}
			if err := cli.ResumeSchedule(cmd.Context(), id, *hubToken); err != nil {
				return err
			}
			fmt.Printf("resumed schedule %q\n", id)
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
			id := strings.TrimSpace(args[0])
			cli, err := client.New(*serverURL)
			if err != nil {
				return err
			}
			if err := cli.RunSchedule(cmd.Context(), id, *hubToken); err != nil {
				return err
			}
			fmt.Printf("triggered schedule %q\n", id)
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
			id := strings.TrimSpace(args[0])
			cli, err := client.New(*serverURL)
			if err != nil {
				return err
			}
			executions, err := cli.GetScheduleHistory(cmd.Context(), id, *hubToken)
			if err != nil {
				return err
			}
			if len(executions) == 0 {
				fmt.Println("(no executions)")
				return nil
			}
			for _, e := range executions {
				status := "ok"
				if !e.GetSuccess() {
					status = "FAIL"
				}
				errMsg := ""
				if e.GetError() != "" {
					errMsg = " error=" + e.GetError()
				}
				fmt.Printf("%s  %s  %dms  %s%s\n",
					e.GetStartedAt(),
					e.GetScheduleId(),
					e.GetDurationMs(),
					status,
					errMsg,
				)
			}
			return nil
		},
	}
}

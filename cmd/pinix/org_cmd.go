// Role:    Organization management subcommand group for the pinix CLI
// Depends: cobra, internal/client
// Exports: newOrgGroupCommand

package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

func newOrgGroupCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "org",
		Short: "Manage organizations",
	}
	cmd.AddCommand(newOrgCreateCommand())
	cmd.AddCommand(newOrgListCommand())
	cmd.AddCommand(newOrgMembersCommand())
	cmd.AddCommand(newOrgAddMemberCommand())
	cmd.AddCommand(newOrgRemoveMemberCommand())
	return cmd
}

func newOrgCreateCommand() *cobra.Command {
	var registryURL string

	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a new organization",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			reg, err := requireRegistryClient(registryURL)
			if err != nil {
				return err
			}
			token, err := loadRegistryToken(reg.BaseURL())
			if err != nil {
				return err
			}
			org, err := reg.CreateOrg(cmd.Context(), token, strings.TrimSpace(args[0]))
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "created organization %s (id: %s)\n", org.Name, org.ID)
			return nil
		},
	}
	cmd.Flags().StringVar(&registryURL, "registry", "", "Pinix Registry base URL")
	return cmd
}

func newOrgListCommand() *cobra.Command {
	var registryURL string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List organizations you belong to",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			reg, err := requireRegistryClient(registryURL)
			if err != nil {
				return err
			}
			token, err := loadRegistryToken(reg.BaseURL())
			if err != nil {
				return err
			}
			resp, err := reg.ListUserOrgs(cmd.Context(), token)
			if err != nil {
				return err
			}
			if len(resp.Orgs) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "(no organizations)")
				return nil
			}
			for _, org := range resp.Orgs {
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\n", org.ID, org.Name)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&registryURL, "registry", "", "Pinix Registry base URL")
	return cmd
}

func newOrgMembersCommand() *cobra.Command {
	var registryURL string

	cmd := &cobra.Command{
		Use:   "members <org-id>",
		Short: "List members of an organization",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			reg, err := requireRegistryClient(registryURL)
			if err != nil {
				return err
			}
			resp, err := reg.ListOrgMembers(cmd.Context(), strings.TrimSpace(args[0]))
			if err != nil {
				return err
			}
			if len(resp.Members) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "(no members)")
				return nil
			}
			for _, m := range resp.Members {
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\n", m.UserID, m.Role)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&registryURL, "registry", "", "Pinix Registry base URL")
	return cmd
}

func newOrgAddMemberCommand() *cobra.Command {
	var registryURL string
	var role string

	cmd := &cobra.Command{
		Use:   "add-member <org-id> <username>",
		Short: "Add a member to an organization",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			reg, err := requireRegistryClient(registryURL)
			if err != nil {
				return err
			}
			token, err := loadRegistryToken(reg.BaseURL())
			if err != nil {
				return err
			}
			orgID := strings.TrimSpace(args[0])
			username := strings.TrimSpace(args[1])
			if err := reg.AddOrgMember(cmd.Context(), token, orgID, username, role); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "added %s to organization as %s\n", username, role)
			return nil
		},
	}
	cmd.Flags().StringVar(&registryURL, "registry", "", "Pinix Registry base URL")
	cmd.Flags().StringVar(&role, "role", "member", "Role: owner or member")
	return cmd
}

func newOrgRemoveMemberCommand() *cobra.Command {
	var registryURL string

	cmd := &cobra.Command{
		Use:   "remove-member <org-id> <user-id>",
		Short: "Remove a member from an organization",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			reg, err := requireRegistryClient(registryURL)
			if err != nil {
				return err
			}
			token, err := loadRegistryToken(reg.BaseURL())
			if err != nil {
				return err
			}
			orgID := strings.TrimSpace(args[0])
			userID := strings.TrimSpace(args[1])
			if err := reg.RemoveOrgMember(cmd.Context(), token, orgID, userID); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "removed %s from organization\n", userID)
			return nil
		},
	}
	cmd.Flags().StringVar(&registryURL, "registry", "", "Pinix Registry base URL")
	return cmd
}

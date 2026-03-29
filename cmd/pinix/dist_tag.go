package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

func newDistTagCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dist-tag",
		Short: "Manage distribution tags for registry packages",
	}
	cmd.AddCommand(newDistTagListCommand())
	cmd.AddCommand(newDistTagAddCommand())
	return cmd
}

func newDistTagListCommand() *cobra.Command {
	var registryURL string

	cmd := &cobra.Command{
		Use:   "list <package>",
		Short: "List dist-tags for a package",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			reg, err := requireRegistryClient(registryURL)
			if err != nil {
				return err
			}
			doc, err := reg.GetPackage(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if len(doc.DistTags) == 0 {
				fmt.Println("(no dist-tags)")
				return nil
			}
			tags := make([]string, 0, len(doc.DistTags))
			for tag := range doc.DistTags {
				tags = append(tags, tag)
			}
			sort.Strings(tags)
			for _, tag := range tags {
				fmt.Printf("%s\t%s\n", tag, doc.DistTags[tag])
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&registryURL, "registry", "", "Pinix Registry base URL")
	return cmd
}

func newDistTagAddCommand() *cobra.Command {
	var registryURL string

	cmd := &cobra.Command{
		Use:   "add <package>@<version> <tag>",
		Short: "Set a dist-tag for a package version",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			pkgVersion := strings.TrimSpace(args[0])
			tag := strings.TrimSpace(args[1])

			atIdx := strings.LastIndex(pkgVersion, "@")
			// For scoped packages like @scope/name@1.0.0, find the last @
			if atIdx <= 0 || (strings.HasPrefix(pkgVersion, "@") && strings.Count(pkgVersion, "@") == 1) {
				return fmt.Errorf("usage: pinix dist-tag add <package>@<version> <tag>")
			}
			pkg := pkgVersion[:atIdx]
			version := pkgVersion[atIdx+1:]

			if pkg == "" || version == "" || tag == "" {
				return fmt.Errorf("package, version, and tag are all required")
			}

			reg, err := requireRegistryClient(registryURL)
			if err != nil {
				return err
			}
			token, err := loadRegistryToken(reg.BaseURL())
			if err != nil {
				return err
			}
			if err := reg.SetDistTag(cmd.Context(), pkg, tag, version, token); err != nil {
				return err
			}
			fmt.Printf("%s: %s = %s\n", pkg, tag, version)
			return nil
		},
	}
	cmd.Flags().StringVar(&registryURL, "registry", "", "Pinix Registry base URL")
	return cmd
}

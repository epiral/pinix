// Role:    CLI update subcommand for upgrading installed Clips to newer versions
// Depends: context, fmt, os, strings, internal/client, internal/daemon, pinix v2, cobra
// Exports: newUpdateCommand

package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	pinixv2 "github.com/epiral/pinix/gen/go/pinix/v2"
	"github.com/epiral/pinix/internal/client"
	daemonpkg "github.com/epiral/pinix/internal/daemon"
	"github.com/spf13/cobra"
)

func newUpdateCommand(serverURL, hubToken *string) *cobra.Command {
	var registryURL string
	var version string
	var all bool

	cmd := &cobra.Command{
		Use:   "update [alias]",
		Short: "Update installed Clips to the latest version from Registry",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if all && len(args) > 0 {
				return fmt.Errorf("cannot specify both --all and a clip alias")
			}
			if !all && len(args) == 0 {
				return fmt.Errorf("specify a clip alias or use --all")
			}

			cli, err := client.New(*serverURL)
			if err != nil {
				return err
			}

			clips, err := cli.ListClips(cmd.Context(), *hubToken)
			if err != nil {
				return fmt.Errorf("list clips: %w", err)
			}

			if all {
				return updateAllClips(cmd.Context(), cli, clips, registryURL, *hubToken)
			}
			return updateSingleClip(cmd.Context(), cli, clips, args[0], registryURL, version, *hubToken)
		},
	}
	cmd.Flags().StringVar(&registryURL, "registry", "", "Pinix Registry base URL (default: from config or https://api.pinix.ai)")
	cmd.Flags().StringVar(&version, "version", "", "update to a specific version instead of latest")
	cmd.Flags().BoolVar(&all, "all", false, "update all registry-installed clips")
	return cmd
}

func updateSingleClip(ctx context.Context, cli *client.Client, clips []*pinixv2.ClipInfo, alias, registryURL, targetVersion, hubToken string) error {
	alias = strings.TrimSpace(alias)

	// Find the clip by alias
	var found *pinixv2.ClipInfo
	for _, clip := range clips {
		if clip.GetName() == alias {
			found = clip
			break
		}
	}
	if found == nil {
		return fmt.Errorf("clip %q not found", alias)
	}

	pkg := strings.TrimSpace(found.GetPackage())
	if pkg == "" || !strings.HasPrefix(pkg, "@") {
		return fmt.Errorf("clip %q is not a registry clip (package=%q)", alias, pkg)
	}

	currentVersion := strings.TrimSpace(found.GetVersion())

	// Resolve target version from Registry
	reg, err := client.NewRegistry(getRegistryURL(registryURL))
	if err != nil {
		return err
	}

	targetVersion = strings.TrimSpace(targetVersion)
	if targetVersion == "" {
		doc, err := reg.GetPackage(ctx, pkg)
		if err != nil {
			return fmt.Errorf("fetch package %q from registry: %w", pkg, err)
		}
		resolved, _, err := doc.ResolveVersion("")
		if err != nil {
			return fmt.Errorf("resolve latest version for %q: %w", pkg, err)
		}
		targetVersion = resolved
	}

	if currentVersion == targetVersion {
		fmt.Printf("%s\t%s\talready up to date\n", alias, currentVersion)
		return nil
	}

	// Build the canonical registry source and call AddClip
	source, err := daemonpkg.NormalizeAddSourceWithVersion(pkg, reg.BaseURL(), targetVersion)
	if err != nil {
		return fmt.Errorf("normalize source: %w", err)
	}

	clip, err := cli.Add(ctx, source, alias, "", "", hubToken)
	if err != nil {
		return err
	}
	fmt.Printf("%s\t%s\t%s -> %s\n", clip.GetName(), firstNonEmpty(clip.GetPackage(), "-"), currentVersion, firstNonEmpty(clip.GetVersion(), "-"))
	return nil
}

func updateAllClips(ctx context.Context, cli *client.Client, clips []*pinixv2.ClipInfo, registryURL, hubToken string) error {
	var registryClips []*pinixv2.ClipInfo
	for _, clip := range clips {
		pkg := strings.TrimSpace(clip.GetPackage())
		if pkg != "" && strings.HasPrefix(pkg, "@") {
			registryClips = append(registryClips, clip)
		}
	}

	if len(registryClips) == 0 {
		fmt.Println("no registry clips to update")
		return nil
	}

	var errs []error
	for _, clip := range registryClips {
		err := updateSingleClip(ctx, cli, clips, clip.GetName(), registryURL, "", hubToken)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error updating %s: %v\n", clip.GetName(), err)
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("%d clip(s) failed to update", len(errs))
	}
	return nil
}

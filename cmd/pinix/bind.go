// Role:    CLI commands for reading and writing local Clip bindings metadata
// Depends: context, encoding/json, fmt, os, path/filepath, sort, strconv, strings, internal/client, internal/daemon, pinix v2, cobra
// Exports: newBindCommand, newUnbindCommand, newBindingsCommand

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	pinixv2 "github.com/epiral/pinix/gen/go/pinix/v2"
	"github.com/epiral/pinix/internal/client"
	daemonpkg "github.com/epiral/pinix/internal/daemon"
	"github.com/spf13/cobra"
)

type clipBinding struct {
	Alias     string `json:"alias"`
	Hub       string `json:"hub,omitempty"`
	HubToken  string `json:"hub_token,omitempty"`
	ClipToken string `json:"clip_token,omitempty"`
}

func newBindCommand(serverURL, authToken *string) *cobra.Command {
	var configPath string
	var remoteHub string
	var remoteHubToken string
	var remoteClipToken string
	var listOnly bool

	cmd := &cobra.Command{
		Use:   "bind <clip-alias> <slot> [target-alias]",
		Short: "Bind a Clip dependency slot to another Clip alias",
		Args: func(cmd *cobra.Command, args []string) error {
			if listOnly {
				if len(args) != 2 {
					return fmt.Errorf("expected <clip-alias> <slot> with --list")
				}
				return nil
			}
			if len(args) != 3 {
				return fmt.Errorf("expected <clip-alias> <slot> <target-alias>")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			clipAlias := strings.TrimSpace(args[0])
			slot := strings.TrimSpace(args[1])
			if clipAlias == "" || slot == "" {
				return fmt.Errorf("clip alias and slot are required")
			}

			registry, err := daemonpkg.NewRegistry(configPath)
			if err != nil {
				return err
			}
			clip, err := requireLocalClip(registry, clipAlias)
			if err != nil {
				return err
			}
			dep, err := requireDependencySlot(clip, slot)
			if err != nil {
				return err
			}

			if listOnly {
				if strings.TrimSpace(remoteClipToken) != "" {
					return fmt.Errorf("--clip-token cannot be used with --list")
				}
				return listBindingCandidates(cmd.Context(), cmd.OutOrStdout(), firstNonEmpty(strings.TrimSpace(remoteHub), strings.TrimSpace(*serverURL)), bindingHubToken(*authToken, remoteHub, remoteHubToken), dep)
			}

			if strings.TrimSpace(remoteHub) == "" {
				if strings.TrimSpace(remoteHubToken) != "" || strings.TrimSpace(remoteClipToken) != "" {
					return fmt.Errorf("--hub-token and --clip-token require --hub")
				}
			}

			targetAlias := strings.TrimSpace(args[2])
			if targetAlias == "" {
				return fmt.Errorf("target alias is required")
			}

			bindingsPath := clipBindingsPath(clip)
			bindings, err := readClipBindings(bindingsPath)
			if err != nil {
				return err
			}
			bindings[slot] = clipBinding{
				Alias:     targetAlias,
				Hub:       strings.TrimSpace(remoteHub),
				HubToken:  strings.TrimSpace(remoteHubToken),
				ClipToken: strings.TrimSpace(remoteClipToken),
			}
			if err := writeClipBindings(bindingsPath, bindings); err != nil {
				return err
			}

			_, _ = dep, clip
			fmt.Fprintf(cmd.OutOrStdout(), "bound %s %s -> %s\n", clipAlias, slot, targetAlias)
			return nil
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "Pinix config path (default: ~/.pinix/config.json)")
	cmd.Flags().StringVar(&remoteHub, "hub", "", "remote hub URL for cross-hub bindings or listing")
	cmd.Flags().StringVar(&remoteHubToken, "hub-token", "", "hub token for the remote hub")
	cmd.Flags().StringVar(&remoteClipToken, "clip-token", "", "clip token stored alongside the binding")
	cmd.Flags().BoolVar(&listOnly, "list", false, "list Clip aliases that satisfy the slot dependency")
	return cmd
}

func newUnbindCommand() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "unbind <clip-alias> <slot>",
		Short: "Remove a Clip dependency binding",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			registry, err := daemonpkg.NewRegistry(configPath)
			if err != nil {
				return err
			}
			clip, err := requireLocalClip(registry, args[0])
			if err != nil {
				return err
			}

			bindingsPath := clipBindingsPath(clip)
			bindings, err := readClipBindings(bindingsPath)
			if err != nil {
				return err
			}
			delete(bindings, strings.TrimSpace(args[1]))
			if err := writeClipBindings(bindingsPath, bindings); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "unbound %s %s\n", strings.TrimSpace(args[0]), strings.TrimSpace(args[1]))
			return nil
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "Pinix config path (default: ~/.pinix/config.json)")
	return cmd
}

func newBindingsCommand() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "bindings <clip-alias>",
		Short: "Show bindings.json for a local Clip",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			registry, err := daemonpkg.NewRegistry(configPath)
			if err != nil {
				return err
			}
			clip, err := requireLocalClip(registry, args[0])
			if err != nil {
				return err
			}

			bindings, err := readClipBindings(clipBindingsPath(clip))
			if err != nil {
				return err
			}
			payload, err := json.MarshalIndent(bindings, "", "  ")
			if err != nil {
				return fmt.Errorf("marshal bindings: %w", err)
			}
			payload = append(payload, '\n')
			_, err = cmd.OutOrStdout().Write(payload)
			return err
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "Pinix config path (default: ~/.pinix/config.json)")
	return cmd
}

func requireLocalClip(registry *daemonpkg.Registry, alias string) (daemonpkg.ClipConfig, error) {
	alias = strings.TrimSpace(alias)
	if alias == "" {
		return daemonpkg.ClipConfig{}, fmt.Errorf("clip alias is required")
	}
	clip, ok, err := registry.GetClip(alias)
	if err != nil {
		return daemonpkg.ClipConfig{}, fmt.Errorf("load clip %q: %w", alias, err)
	}
	if !ok {
		return daemonpkg.ClipConfig{}, fmt.Errorf("clip %q not found in local registry", alias)
	}
	return clip, nil
}

func requireDependencySlot(clip daemonpkg.ClipConfig, slot string) (daemonpkg.DependencySpec, error) {
	slot = strings.TrimSpace(slot)
	if slot == "" {
		return daemonpkg.DependencySpec{}, fmt.Errorf("slot is required")
	}
	if clip.Manifest == nil {
		return daemonpkg.DependencySpec{}, fmt.Errorf("clip %q manifest unavailable", clip.Name)
	}
	spec, ok := clip.Manifest.Dependencies[slot]
	if !ok {
		return daemonpkg.DependencySpec{}, fmt.Errorf("clip %q does not declare dependency slot %q", clip.Name, slot)
	}
	if strings.TrimSpace(spec.Package) == "" {
		spec.Package = slot
	}
	return spec, nil
}

func clipBindingsPath(clip daemonpkg.ClipConfig) string {
	return filepath.Join(strings.TrimSpace(clip.Path), "bindings.json")
}

func readClipBindings(path string) (map[string]clipBinding, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]clipBinding{}, nil
		}
		return nil, fmt.Errorf("read bindings: %w", err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return map[string]clipBinding{}, nil
	}
	bindings := make(map[string]clipBinding)
	if err := json.Unmarshal(data, &bindings); err != nil {
		return nil, fmt.Errorf("parse bindings: %w", err)
	}
	return bindings, nil
}

func writeClipBindings(path string, bindings map[string]clipBinding) error {
	if len(bindings) == 0 {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove bindings: %w", err)
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create bindings dir: %w", err)
	}
	data, err := json.MarshalIndent(bindings, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal bindings: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write bindings: %w", err)
	}
	return nil
}

func listBindingCandidates(ctx context.Context, out interface{ Write([]byte) (int, error) }, hubURL, hubToken string, dep daemonpkg.DependencySpec) error {
	if strings.TrimSpace(hubURL) == "" {
		return fmt.Errorf("hub URL is required")
	}
	cli, err := client.New(hubURL)
	if err != nil {
		return err
	}
	clips, err := cli.ListClips(ctx, hubToken)
	if err != nil {
		return err
	}

	matching := make([]*pinixv2.ClipInfo, 0, len(clips))
	for _, clip := range clips {
		if clip == nil {
			continue
		}
		if strings.TrimSpace(clip.GetPackage()) != strings.TrimSpace(dep.Package) {
			continue
		}
		if !versionMatches(strings.TrimSpace(clip.GetVersion()), strings.TrimSpace(dep.Version)) {
			continue
		}
		matching = append(matching, clip)
	}
	sort.Slice(matching, func(i, j int) bool {
		return matching[i].GetName() < matching[j].GetName()
	})

	if len(matching) == 0 {
		_, err := fmt.Fprintln(out, "(no matching clips)")
		return err
	}
	for _, clip := range matching {
		if _, err := fmt.Fprintf(out, "%s\t%s\t%s\t%s\n", clip.GetName(), firstNonEmpty(clip.GetPackage(), "-"), firstNonEmpty(clip.GetVersion(), "-"), firstNonEmpty(clip.GetProvider(), "-")); err != nil {
			return err
		}
	}
	return nil
}

func bindingHubToken(authToken, remoteHub, remoteHubToken string) string {
	if strings.TrimSpace(remoteHub) != "" {
		return strings.TrimSpace(remoteHubToken)
	}
	return strings.TrimSpace(authToken)
}

type semVersion struct {
	major      int
	minor      int
	patch      int
	prerelease []string
}

func versionMatches(version, constraint string) bool {
	constraint = strings.TrimSpace(constraint)
	if constraint == "" || constraint == "*" {
		return true
	}
	version = strings.TrimSpace(version)
	if version == "" {
		return false
	}
	if strings.Contains(constraint, "||") {
		for _, part := range strings.Split(constraint, "||") {
			if versionMatches(version, part) {
				return true
			}
		}
		return false
	}

	current, ok := parseSemVersion(version)
	if !ok {
		return strings.EqualFold(version, constraint)
	}

	constraint = normalizeHyphenRange(constraint)
	clauses := splitConstraintClauses(constraint)
	if len(clauses) == 0 {
		return false
	}
	for _, clause := range clauses {
		if !matchVersionClause(current, clause) {
			return false
		}
	}
	return true
}

func normalizeHyphenRange(constraint string) string {
	if !strings.Contains(constraint, " - ") {
		return constraint
	}
	left, right, ok := strings.Cut(constraint, " - ")
	if !ok {
		return constraint
	}
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	if left == "" || right == "" {
		return constraint
	}
	return ">=" + left + " <=" + right
}

func splitConstraintClauses(constraint string) []string {
	return strings.FieldsFunc(constraint, func(r rune) bool {
		switch r {
		case ',', ' ', '\t', '\n':
			return true
		default:
			return false
		}
	})
}

func matchVersionClause(version semVersion, clause string) bool {
	clause = strings.TrimSpace(clause)
	if clause == "" || clause == "*" || strings.EqualFold(clause, "x") {
		return true
	}
	if strings.HasPrefix(clause, "^") {
		base, _, wildcard, ok := parseConstraintVersion(strings.TrimPrefix(clause, "^"))
		if !ok {
			return false
		}
		if wildcard {
			return compareSemVersion(version, base) >= 0
		}
		upper := caretUpperBound(base)
		return compareSemVersion(version, base) >= 0 && compareSemVersion(version, upper) < 0
	}
	if strings.HasPrefix(clause, "~") {
		base, parts, wildcard, ok := parseConstraintVersion(strings.TrimPrefix(clause, "~"))
		if !ok {
			return false
		}
		if wildcard {
			return compareSemVersion(version, base) >= 0
		}
		upper := tildeUpperBound(base, parts)
		return compareSemVersion(version, base) >= 0 && compareSemVersion(version, upper) < 0
	}

	for _, op := range []string{">=", "<=", ">", "<", "="} {
		if strings.HasPrefix(clause, op) {
			base, _, _, ok := parseConstraintVersion(strings.TrimPrefix(clause, op))
			if !ok {
				return false
			}
			cmp := compareSemVersion(version, base)
			switch op {
			case ">=":
				return cmp >= 0
			case "<=":
				return cmp <= 0
			case ">":
				return cmp > 0
			case "<":
				return cmp < 0
			case "=":
				return cmp == 0
			}
		}
	}

	base, parts, wildcard, ok := parseConstraintVersion(clause)
	if !ok {
		return false
	}
	if wildcard || parts < 3 {
		upper, hasUpper := wildcardUpperBound(base, parts)
		if compareSemVersion(version, base) < 0 {
			return false
		}
		if hasUpper && compareSemVersion(version, upper) >= 0 {
			return false
		}
		return true
	}
	return compareSemVersion(version, base) == 0
}

func parseConstraintVersion(raw string) (semVersion, int, bool, bool) {
	raw = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(raw), "v"))
	if raw == "" {
		return semVersion{}, 0, false, false
	}
	if idx := strings.IndexByte(raw, '+'); idx >= 0 {
		raw = raw[:idx]
	}
	parts := strings.Split(raw, ".")
	if len(parts) == 0 || len(parts) > 3 {
		return semVersion{}, 0, false, false
	}

	version := semVersion{}
	fixed := 0
	wildcard := false
	for i, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || part == "*" || strings.EqualFold(part, "x") {
			wildcard = true
			break
		}
		if i == len(parts)-1 && strings.Contains(part, "-") {
			numeric, prerelease, ok := strings.Cut(part, "-")
			if !ok {
				return semVersion{}, 0, false, false
			}
			part = numeric
			version.prerelease = splitPrerelease(prerelease)
		}
		value, err := strconv.Atoi(part)
		if err != nil || value < 0 {
			return semVersion{}, 0, false, false
		}
		switch i {
		case 0:
			version.major = value
		case 1:
			version.minor = value
		case 2:
			version.patch = value
		}
		fixed++
	}
	return version, fixed, wildcard, true
}

func parseSemVersion(raw string) (semVersion, bool) {
	version, parts, wildcard, ok := parseConstraintVersion(raw)
	if !ok || wildcard || parts == 0 {
		return semVersion{}, false
	}
	return version, true
}

func wildcardUpperBound(base semVersion, parts int) (semVersion, bool) {
	switch parts {
	case 0:
		return semVersion{}, false
	case 1:
		return semVersion{major: base.major + 1}, true
	case 2:
		return semVersion{major: base.major, minor: base.minor + 1}, true
	default:
		return semVersion{major: base.major, minor: base.minor, patch: base.patch + 1}, true
	}
}

func caretUpperBound(base semVersion) semVersion {
	if base.major > 0 {
		return semVersion{major: base.major + 1}
	}
	if base.minor > 0 {
		return semVersion{major: 0, minor: base.minor + 1}
	}
	return semVersion{major: 0, minor: 0, patch: base.patch + 1}
}

func tildeUpperBound(base semVersion, parts int) semVersion {
	if parts <= 1 {
		return semVersion{major: base.major + 1}
	}
	return semVersion{major: base.major, minor: base.minor + 1}
}

func compareSemVersion(left, right semVersion) int {
	for _, cmp := range []int{compareInt(left.major, right.major), compareInt(left.minor, right.minor), compareInt(left.patch, right.patch)} {
		if cmp != 0 {
			return cmp
		}
	}
	return comparePrerelease(left.prerelease, right.prerelease)
}

func compareInt(left, right int) int {
	switch {
	case left < right:
		return -1
	case left > right:
		return 1
	default:
		return 0
	}
}

func comparePrerelease(left, right []string) int {
	if len(left) == 0 && len(right) == 0 {
		return 0
	}
	if len(left) == 0 {
		return 1
	}
	if len(right) == 0 {
		return -1
	}
	max := len(left)
	if len(right) > max {
		max = len(right)
	}
	for i := 0; i < max; i++ {
		if i >= len(left) {
			return -1
		}
		if i >= len(right) {
			return 1
		}
		cmp := comparePrereleaseIdentifier(left[i], right[i])
		if cmp != 0 {
			return cmp
		}
	}
	return 0
}

func comparePrereleaseIdentifier(left, right string) int {
	leftInt, leftErr := strconv.Atoi(left)
	rightInt, rightErr := strconv.Atoi(right)
	switch {
	case leftErr == nil && rightErr == nil:
		return compareInt(leftInt, rightInt)
	case leftErr == nil:
		return -1
	case rightErr == nil:
		return 1
	default:
		return strings.Compare(left, right)
	}
}

func splitPrerelease(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ".")
	cleaned := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		cleaned = append(cleaned, part)
	}
	return cleaned
}

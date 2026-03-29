// Role:    CLI commands for listing and mutating Clip bindings through Hub RPCs
// Depends: context, encoding/json, fmt, sort, strconv, strings, internal/client, pinix v2, cobra
// Exports: newBindCommand, newUnbindCommand, newBindingsCommand

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	pinixv2 "github.com/epiral/pinix/gen/go/pinix/v2"
	"github.com/epiral/pinix/internal/client"
	"github.com/spf13/cobra"
)

type clipBinding struct {
	Alias     string `json:"alias"`
	Hub       string `json:"hub,omitempty"`
	HubToken  string `json:"hub_token,omitempty"`
	ClipToken string `json:"clip_token,omitempty"`
}

func newBindCommand(serverURL, authToken *string) *cobra.Command {
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

			state, err := loadBindingState(cmd.Context(), *serverURL, *authToken, clipAlias)
			if err != nil {
				return err
			}
			dep, err := requireDependencySlot(state, clipAlias, slot)
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

			cli, err := client.New(*serverURL)
			if err != nil {
				return err
			}
			if err := cli.SetBinding(cmd.Context(), clipAlias, slot, &pinixv2.ClipBinding{
				Alias:     targetAlias,
				Hub:       strings.TrimSpace(remoteHub),
				HubToken:  strings.TrimSpace(remoteHubToken),
				ClipToken: strings.TrimSpace(remoteClipToken),
			}, *authToken); err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "bound %s %s -> %s\n", clipAlias, slot, targetAlias)
			return nil
		},
	}
	cmd.Flags().StringVar(&remoteHub, "hub", "", "remote hub URL for cross-hub bindings or listing")
	cmd.Flags().StringVar(&remoteHubToken, "hub-token", "", "hub token for the remote hub")
	cmd.Flags().StringVar(&remoteClipToken, "clip-token", "", "clip token stored alongside the binding")
	cmd.Flags().BoolVar(&listOnly, "list", false, "list Clip aliases that satisfy the slot dependency")
	return cmd
}

func newUnbindCommand(serverURL, authToken *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "unbind <clip-alias> <slot>",
		Short: "Remove a Clip dependency binding",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cli, err := client.New(*serverURL)
			if err != nil {
				return err
			}
			if err := cli.RemoveBinding(cmd.Context(), strings.TrimSpace(args[0]), strings.TrimSpace(args[1]), *authToken); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "unbound %s %s\n", strings.TrimSpace(args[0]), strings.TrimSpace(args[1]))
			return nil
		},
	}
	return cmd
}

func newBindingsCommand(serverURL, authToken *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bindings <clip-alias>",
		Short: "Show bindings for a local Clip",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			state, err := loadBindingState(cmd.Context(), *serverURL, *authToken, strings.TrimSpace(args[0]))
			if err != nil {
				return err
			}
			payload, err := json.MarshalIndent(protoBindingsToLocal(state.GetBindings()), "", "  ")
			if err != nil {
				return fmt.Errorf("marshal bindings: %w", err)
			}
			payload = append(payload, '\n')
			_, err = cmd.OutOrStdout().Write(payload)
			return err
		},
	}
	return cmd
}

func loadBindingState(ctx context.Context, serverURL, hubToken, clipAlias string) (*pinixv2.GetBindingsResponse, error) {
	clipAlias = strings.TrimSpace(clipAlias)
	if clipAlias == "" {
		return nil, fmt.Errorf("clip alias is required")
	}
	cli, err := client.New(serverURL)
	if err != nil {
		return nil, err
	}
	return cli.GetBindings(ctx, clipAlias, hubToken)
}

func requireDependencySlot(state *pinixv2.GetBindingsResponse, clipAlias, slot string) (*pinixv2.DependencySlot, error) {
	slot = strings.TrimSpace(slot)
	if slot == "" {
		return nil, fmt.Errorf("slot is required")
	}
	if state == nil {
		return nil, fmt.Errorf("clip %q bindings unavailable", clipAlias)
	}
	spec, ok := state.GetDependencySlots()[slot]
	if !ok {
		return nil, fmt.Errorf("clip %q does not declare dependency slot %q", clipAlias, slot)
	}
	dep := &pinixv2.DependencySlot{
		Package: strings.TrimSpace(spec.GetPackage()),
		Version: strings.TrimSpace(spec.GetVersion()),
	}
	if dep.Package == "" {
		dep.Package = slot
	}
	return dep, nil
}

func protoBindingsToLocal(bindings map[string]*pinixv2.ClipBinding) map[string]clipBinding {
	if len(bindings) == 0 {
		return map[string]clipBinding{}
	}
	result := make(map[string]clipBinding, len(bindings))
	for slot, binding := range bindings {
		if binding == nil {
			continue
		}
		result[slot] = clipBinding{
			Alias:     strings.TrimSpace(binding.GetAlias()),
			Hub:       strings.TrimSpace(binding.GetHub()),
			HubToken:  strings.TrimSpace(binding.GetHubToken()),
			ClipToken: strings.TrimSpace(binding.GetClipToken()),
		}
	}
	return result
}

func listBindingCandidates(ctx context.Context, out interface{ Write([]byte) (int, error) }, hubURL, hubToken string, dep *pinixv2.DependencySlot) error {
	if strings.TrimSpace(hubURL) == "" {
		return fmt.Errorf("hub URL is required")
	}
	if dep == nil {
		return fmt.Errorf("dependency slot is required")
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
		if strings.TrimSpace(clip.GetPackage()) != strings.TrimSpace(dep.GetPackage()) {
			continue
		}
		if !versionMatches(strings.TrimSpace(clip.GetVersion()), strings.TrimSpace(dep.GetVersion())) {
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

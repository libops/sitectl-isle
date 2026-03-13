package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/libops/sitectl-isle/pkg/components"
	createpkg "github.com/libops/sitectl-isle/pkg/create"
	corecomponent "github.com/libops/sitectl/pkg/component"
	"github.com/libops/sitectl/pkg/config"
	"github.com/spf13/cobra"
)

var (
	componentSetYolo      bool
	componentSetInput     = config.GetInput
	componentApplyOptions = createpkg.Apply
)

var componentSetCmd = &cobra.Command{
	Use:   "set <name> <on|off>",
	Short: "Set an ISLE component on or off for the current project",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runComponentSet(cmd, args[0], args[1])
	},
}

func init() {
	componentSetCmd.Flags().StringVar(&statusPath, "path", "", "Path to the checked out isle-site-template project. Defaults to the active sitectl context project directory")
	corecomponent.AddDrupalRootfsFlag(componentSetCmd, &statusDrupalRootfs, createpkg.DefaultDrupalRootfs)
	componentSetCmd.Flags().BoolVar(&componentSetYolo, "yolo", false, "Apply the component change without confirmation")
	componentCmd.AddCommand(componentSetCmd)
}

func runComponentSet(cmd *cobra.Command, name, stateValue string) error {
	ctx, err := resolveStatusContext()
	if err != nil {
		return err
	}
	if ctx.DockerHostType != config.ContextLocal {
		return fmt.Errorf("component changes are local-only; context %q is %q", ctx.Name, ctx.DockerHostType)
	}

	state, err := corecomponent.ParseState(stateValue)
	if err != nil {
		return err
	}

	defs := componentDefinitions()
	def, ok := defs[name]
	if !ok {
		return fmt.Errorf("unknown component %q", name)
	}

	statuses, err := corecomponent.DetectComponentStatuses(ctx, ctx.ProjectDir, corecomponent.DetectOptions{
		ComposeRoot:  ctx.ProjectDir,
		DrupalRootfs: statusDrupalRootfs,
	}, components.Fcrepo(components.TemplateSource{}), components.Blazegraph(components.TemplateSource{}))
	if err != nil {
		return err
	}

	currentStates := map[string]corecomponent.DetectedState{}
	for _, status := range statuses {
		currentStates[status.Name] = status.State
	}
	for componentName, current := range currentStates {
		if componentName == name {
			continue
		}
		if current == corecomponent.StateDrifted {
			return fmt.Errorf("component %q is drifted; resolve it first or set it explicitly before changing %q", componentName, name)
		}
	}

	if !componentSetYolo {
		prompt, err := componentSetPrompt(def, state)
		if err != nil {
			return err
		}
		confirmed, err := confirmComponentSet(prompt)
		if err != nil {
			return err
		}
		if !confirmed {
			return fmt.Errorf("component change cancelled")
		}
	}

	opts := createpkg.Options{
		Path:         ctx.ProjectDir,
		DrupalRootfs: statusDrupalRootfs,
		Fcrepo:       string(resolveComponentCreateState("fcrepo", name, state, currentStates)),
		Blazegraph:   string(resolveComponentCreateState("blazegraph", name, state, currentStates)),
	}
	if opts.Fcrepo == "" {
		opts.Fcrepo = createpkg.FcrepoStateOn
	}
	if opts.Blazegraph == "" {
		opts.Blazegraph = createpkg.FcrepoStateOn
	}

	if opts.Fcrepo == createpkg.FcrepoStateOff {
		scheme, err := resolveCurrentFileSystemURI(ctx.ProjectDir, statusDrupalRootfs)
		if err != nil {
			return err
		}
		opts.ISLEFileSystemURI = scheme
	}

	if err := componentApplyOptions(opts); err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "%s: %s\n", name, state)
	return nil
}

func componentDefinitions() map[string]corecomponent.Definition {
	defs := orderedComponentDefinitions()
	out := make(map[string]corecomponent.Definition, len(defs))
	for _, def := range defs {
		out[def.Name] = def
	}
	return out
}

func orderedComponentDefinitions() []corecomponent.Definition {
	return []corecomponent.Definition{
		components.Fcrepo(components.TemplateSource{}),
		components.Blazegraph(components.TemplateSource{}),
	}
}

func resolveComponentCreateState(componentName, targetName string, targetState corecomponent.State, current map[string]corecomponent.DetectedState) corecomponent.State {
	if componentName == targetName {
		return targetState
	}
	switch current[componentName] {
	case corecomponent.DetectedState(corecomponent.StateOff):
		return corecomponent.StateOff
	case "", corecomponent.DetectedState(corecomponent.StateOn):
		return corecomponent.StateOn
	default:
		return corecomponent.StateOn
	}
}

func componentSetPrompt(def corecomponent.Definition, state corecomponent.State) (string, error) {
	var summary string
	switch state {
	case corecomponent.StateOn:
		summary = def.Behavior.Enable.Summary
	case corecomponent.StateOff:
		summary = def.Behavior.Disable.Summary
	default:
		return "", fmt.Errorf("unsupported component state %q", state)
	}

	migration := def.Behavior.Enable.DataMigration
	if state == corecomponent.StateOff {
		migration = def.Behavior.Disable.DataMigration
	}
	lines := []string{
		fmt.Sprintf("Set %s=%s?", def.Name, state),
	}
	if strings.TrimSpace(summary) != "" {
		lines = append(lines, summary)
	}
	if migration != "" && migration != corecomponent.DataMigrationNone {
		lines = append(lines, fmt.Sprintf("Migration impact: %s.", migration))
	}
	lines = append(lines, "This updates docker compose and Drupal config. Continue? [y/N]: ")
	return strings.Join(lines, "\n"), nil
}

func confirmComponentSet(prompt string) (bool, error) {
	response, err := componentSetInput(prompt)
	if err != nil {
		return false, err
	}
	value := strings.TrimSpace(strings.ToLower(response))
	return value == "y" || value == "yes", nil
}

func resolveCurrentFileSystemURI(projectDir, drupalRootfs string) (string, error) {
	layout := corecomponent.ResolveDrupalLayout(projectDir, drupalRootfs)
	fieldPath := filepath.Join(layout.ConfigSyncDir(), "field.storage.media.field_media_file.yml")
	data, err := os.ReadFile(fieldPath)
	if err != nil {
		return createpkg.DefaultISLEFileSystemURI, nil
	}
	rendered := string(data)
	for _, scheme := range []string{"public", "private", "archive", "gs-production"} {
		if strings.Contains(rendered, `uri_scheme: "`+scheme+`"`) || strings.Contains(rendered, "uri_scheme: "+scheme) {
			return scheme, nil
		}
	}
	return createpkg.DefaultISLEFileSystemURI, nil
}

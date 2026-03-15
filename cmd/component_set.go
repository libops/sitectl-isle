package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/libops/sitectl-isle/pkg/components"
	createpkg "github.com/libops/sitectl-isle/pkg/create"
	"github.com/libops/sitectl-isle/pkg/traefikconfig"
	corecomponent "github.com/libops/sitectl/pkg/component"
	"github.com/libops/sitectl/pkg/config"
	"github.com/spf13/cobra"
)

var (
	componentSetYolo      bool
	componentSetState     string
	componentSetTLSMode   string
	componentSetInput     = config.GetInput
	componentApplyOptions = createpkg.Apply
	componentPromptChoice = corecomponent.PromptChoice
	componentPromptState  = corecomponent.PromptState
)

var componentSetCmd = &cobra.Command{
	Use:   "set <name> [on|off]",
	Short: "Set an ISLE component on or off for the current project",
	Args:  cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		stateValue, err := resolveComponentSetStateValue(cmd, args)
		if err != nil {
			return err
		}
		return runComponentSet(cmd, args[0], stateValue)
	},
}

func init() {
	componentSetCmd.Flags().StringVar(&statusPath, "path", "", "Path to the checked out isle-site-template project. Defaults to the active sitectl context project directory")
	corecomponent.AddDrupalRootfsFlag(componentSetCmd, &statusDrupalRootfs, createpkg.DefaultDrupalRootfs)
	componentSetCmd.Flags().StringVar(&componentSetState, "state", "", "Component state to apply. Valid values are on or off. If omitted, the command prompts interactively.")
	componentSetCmd.Flags().BoolVar(&componentSetYolo, "yolo", false, "Apply the component change without confirmation")
	componentSetCmd.Flags().StringVar(&componentSetTLSMode, "tls-mode", "", "TLS mode for the selected component. Valid values are http, self-managed, mkcert, or letsencrypt.")
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

	var state corecomponent.State
	if strings.TrimSpace(stateValue) != "" {
		state, err = corecomponent.ParseState(stateValue)
		if err != nil {
			return err
		}
	}

	defs := componentDefinitions()
	def, ok := defs[name]
	if !ok {
		return fmt.Errorf("unknown component %q", name)
	}

	statuses, err := detectComponentViews(ctx.ProjectDir, statusDrupalRootfs)
	if err != nil {
		return err
	}
	statusByName := map[string]componentView{}
	for _, status := range statuses {
		statusByName[status.Name] = status
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

	if strings.TrimSpace(stateValue) == "" {
		status, ok := statusByName[name]
		if !ok {
			return fmt.Errorf("missing detected status for component %q", name)
		}
		state, err = promptComponentSetState(status)
		if err != nil {
			return err
		}
	}

	if !componentSetYolo {
		if requiresTLSModeSelection(name, state) {
			mode, err := resolveTLSComponentMode(name)
			if err != nil {
				return err
			}
			componentSetTLSMode = mode
		}
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

	if name == "isle-tls" || name == "isle-tls-override" {
		return runTLSComponentSet(cmd, ctx.ProjectDir, name, state)
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
	defs := managedComponentDefinitions()
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

func managedComponentDefinitions() []corecomponent.Definition {
	defs := append([]corecomponent.Definition{}, orderedComponentDefinitions()...)
	defs = append(defs,
		components.ISLEEntrypoint(),
		components.ISLEEntrypointOverride(),
	)
	return defs
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

	body := []string{
		fmt.Sprintf("Set `%s` to `%s`.", def.Name, state),
	}
	if strings.TrimSpace(summary) != "" {
		body = append(body, "", summary)
	}
	if requestedMode := componentRequestedMode(def.Name, state); strings.TrimSpace(requestedMode) != "" {
		body = append(body, "", fmt.Sprintf("Requested mode: `%s`.", requestedMode))
	}
	if migration != "" && migration != corecomponent.DataMigrationNone {
		body = append(body, "", fmt.Sprintf("Migration impact: `%s`.", migration))
	}
	body = append(body, "", "This updates docker compose and Drupal config.")

	section := corecomponent.RenderSection("Confirm component change", strings.Join(body, "\n"))
	prompt := corecomponent.RenderPromptLine("Continue? [y/N]: ")
	return section + "\n\n" + prompt, nil
}

func runTLSComponentSet(cmd *cobra.Command, projectDir, name string, state corecomponent.State) error {
	mode, err := resolvedTLSMode(name, state)
	if err != nil {
		return err
	}

	switch name {
	case "isle-tls":
		if state == corecomponent.StateOn && mode == traefikconfig.ModeHTTP {
			return fmt.Errorf("isle-tls=on requires --tls-mode self-managed, mkcert, or letsencrypt")
		}
		if err := traefikconfig.ApplyProd(projectDir, mode); err != nil {
			return err
		}
	case "isle-tls-override":
		if err := traefikconfig.ApplyDev(projectDir, state == corecomponent.StateOn, mode); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported entrypoint component %q", name)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "%s: %s\n", name, state)
	return nil
}

func componentRequestedMode(name string, state corecomponent.State) string {
	mode, err := resolvedTLSMode(name, state)
	if err != nil {
		return ""
	}
	return mode
}

func confirmComponentSet(prompt string) (bool, error) {
	response, err := componentSetInput(prompt)
	if err != nil {
		return false, err
	}
	value := strings.TrimSpace(strings.ToLower(response))
	return value == "y" || value == "yes", nil
}

func requiresTLSModeSelection(name string, state corecomponent.State) bool {
	return state == corecomponent.StateOn && strings.TrimSpace(componentSetTLSMode) == "" &&
		(name == "isle-tls" || name == "isle-tls-override")
}

func resolveTLSComponentMode(name string) (string, error) {
	defaultValue, err := defaultTLSPromptMode(name)
	if err != nil {
		return "", err
	}
	return promptTLSComponentMode(name, defaultValue, componentSetInput, componentPromptChoice)
}

func resolvedTLSMode(name string, state corecomponent.State) (string, error) {
	switch name {
	case "isle-tls":
		if state == corecomponent.StateOff {
			return traefikconfig.ModeHTTP, nil
		}
		if strings.TrimSpace(componentSetTLSMode) == "" {
			return traefikconfig.ModeSelfManaged, nil
		}
		return componentSetTLSMode, nil
	case "isle-tls-override":
		if state == corecomponent.StateOff {
			return traefikconfig.ModeInherited, nil
		}
		if strings.TrimSpace(componentSetTLSMode) == "" {
			return traefikconfig.ModeMkcert, nil
		}
		return componentSetTLSMode, nil
	default:
		return "", fmt.Errorf("unsupported TLS component %q", name)
	}
}

func defaultTLSPromptMode(name string) (string, error) {
	switch name {
	case "isle-tls":
		return traefikconfig.ModeSelfManaged, nil
	case "isle-tls-override":
		return traefikconfig.ModeMkcert, nil
	default:
		return "", fmt.Errorf("unsupported TLS component %q", name)
	}
}

func promptTLSComponentMode(name, defaultValue string, input corecomponent.InputFunc, promptChoice func(string, []corecomponent.Choice, string, corecomponent.InputFunc, ...string) (string, error)) (string, error) {
	var question string
	choices := []corecomponent.Choice{
		{
			Value:   traefikconfig.ModeSelfManaged,
			Label:   traefikconfig.ModeSelfManaged,
			Help:    "Use HTTPS with certificates you manage yourself.",
			Aliases: []string{"1"},
		},
		{
			Value:   traefikconfig.ModeMkcert,
			Label:   traefikconfig.ModeMkcert,
			Help:    "Use HTTPS with mkcert for local development.",
			Aliases: []string{"2"},
		},
		{
			Value:   traefikconfig.ModeLetsEncrypt,
			Label:   traefikconfig.ModeLetsEncrypt,
			Help:    "Use HTTPS with Let's Encrypt automation.",
			Aliases: []string{"3"},
		},
	}

	switch name {
	case "isle-tls":
		question = "Choose how the production stack frontend should be served."
	case "isle-tls-override":
		question = "Choose how the development stack frontend should be served."
		choices = []corecomponent.Choice{
			{
				Value:   traefikconfig.ModeHTTP,
				Label:   traefikconfig.ModeHTTP,
				Help:    "Use HTTP only for the dev override.",
				Aliases: []string{"1"},
			},
			{
				Value:   traefikconfig.ModeMkcert,
				Label:   traefikconfig.ModeMkcert,
				Help:    "Use HTTPS with mkcert for local development.",
				Aliases: []string{"2"},
			},
			{
				Value:   traefikconfig.ModeSelfManaged,
				Label:   traefikconfig.ModeSelfManaged,
				Help:    "Use HTTPS with certificates you manage yourself.",
				Aliases: []string{"3"},
			},
			{
				Value:   traefikconfig.ModeLetsEncrypt,
				Label:   traefikconfig.ModeLetsEncrypt,
				Help:    "Use HTTPS with Let's Encrypt automation.",
				Aliases: []string{"4"},
			},
		}
	default:
		return "", fmt.Errorf("unsupported TLS component %q", name)
	}

	section := corecomponent.RenderSection("Frontend mode", question)
	return promptChoice(name+"-tls-mode", choices, defaultValue, input, strings.Split(section, "\n")...)
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

func resolveComponentSetStateValue(cmd *cobra.Command, args []string) (string, error) {
	positionalState := ""
	if len(args) > 1 {
		positionalState = args[1]
	}
	flagChanged := cmd.Flags().Changed("state")
	if positionalState != "" && flagChanged {
		return "", fmt.Errorf("state specified twice: use either positional on|off or --state")
	}
	if positionalState != "" {
		return positionalState, nil
	}
	if flagChanged {
		return componentSetState, nil
	}
	return "", nil
}

func promptComponentSetState(status componentView) (corecomponent.State, error) {
	guidance := status.Definition.Guidance
	guidance.DefaultState = corecomponent.ReviewDefaultState(status)
	guidance.Question = corecomponent.BuildReviewQuestion(status)
	return componentPromptState(status.Name, guidance, componentSetInput)
}

package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/libops/sitectl-isle/pkg/components"
	createpkg "github.com/libops/sitectl-isle/pkg/create"
	"github.com/libops/sitectl-isle/pkg/externalcantaloupe"
	"github.com/libops/sitectl-isle/pkg/traefikconfig"
	corecomponent "github.com/libops/sitectl/pkg/component"
	"github.com/libops/sitectl/pkg/config"
	"github.com/libops/sitectl/pkg/plugin"
	"github.com/spf13/cobra"
)

var (
	componentSetYolo           bool
	componentSetState          string
	componentSetDisposition    string
	componentSetTLSMode        string
	componentSetInput          = config.GetInput
	componentApplyOptions      = createpkg.Apply
	componentPromptChoice      = corecomponent.PromptChoice
	componentPromptState       = corecomponent.PromptState
	componentPromptDisposition corecomponent.PromptDispositionFunc
)

var componentSetCmd = &cobra.Command{
	Use:   "set <name> [disposition]",
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
	componentSetCmd.Flags().StringVar(&componentSetDisposition, "disposition", "", "Component disposition to apply. Valid values depend on the component, commonly disabled, superceded, enabled, or distributed.")
	componentSetCmd.Flags().BoolVar(&componentSetYolo, "yolo", false, "Apply the component change without confirmation")
	componentSetCmd.Flags().StringVar(&componentSetTLSMode, "tls-mode", "", "TLS mode for the selected component. Valid values are http, self-managed, mkcert, or letsencrypt.")
	addComponentSetFollowUpFlags(componentSetCmd, managedComponentDefinitions())
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
	var disposition corecomponent.Disposition
	if strings.TrimSpace(stateValue) != "" {
		disposition, err = corecomponent.ParseDisposition(stateValue)
		if err != nil {
			return err
		}
		state = corecomponent.DispositionToState(disposition)
	}

	defs := componentDefinitions()
	def, ok := defs[name]
	if !ok {
		return fmt.Errorf("unknown component %q", name)
	}
	if disposition != "" {
		disposition, err = corecomponent.ResolveAllowedDisposition(def.AllowedDispositions, disposition)
		if err != nil {
			return err
		}
		state = corecomponent.DispositionToState(disposition)
	}

	statuses, err := detectComponentViewsForContext(ctx, statusDrupalRootfs)
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
		if !blocksComponentSetOnDrift(componentName) {
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
		disposition, state, err = promptComponentSetState(status)
		if err != nil {
			return err
		}
	}
	if disposition == "" {
		disposition = corecomponent.StateToDisposition(state)
	}

	followUps, err := resolveComponentSetFollowUps(cmd, def, statusByName[name], disposition, state)
	if err != nil {
		return err
	}

	if !componentSetYolo {
		if requiresTLSModeSelection(name, disposition) {
			mode, err := resolveTLSComponentMode(name)
			if err != nil {
				return err
			}
			componentSetTLSMode = mode
			followUps["tls-mode"] = mode
		}
		prompt, err := componentSetPrompt(def, disposition, state, followUps)
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
		return runTLSComponentSet(cmd, ctx, name, disposition, state)
	}
	if name == "external-cantaloupe" {
		if err := externalcantaloupe.Apply(ctx.ProjectDir, resolveEnvironmentOverridePath(ctx), strings.TrimSpace(followUps["upstream-url"]), disposition == corecomponent.DispositionDistributed); err != nil {
			return err
		}
		if err := ctx.EnsureTrackedComposeOverrideSymlink(); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%s: %s", name, disposition)
		if value := strings.TrimSpace(followUps["upstream-url"]); value != "" {
			fmt.Fprintf(cmd.OutOrStdout(), " (%s)", value)
		}
		fmt.Fprintln(cmd.OutOrStdout())
		return nil
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

	fmt.Fprintf(cmd.OutOrStdout(), "%s: %s\n", name, disposition)
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
		components.ExternalCantaloupe(),
		components.ISLEEntrypoint(),
		components.ISLEEntrypointOverride(),
	)
	return defs
}

func blocksComponentSetOnDrift(name string) bool {
	switch name {
	case "external-cantaloupe", "isle-tls", "isle-tls-override":
		return false
	default:
		return true
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

func componentSetPrompt(def corecomponent.Definition, disposition corecomponent.Disposition, state corecomponent.State, followUps map[string]string) (string, error) {
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
		fmt.Sprintf("Set `%s` to `%s`.", def.Name, disposition),
	}
	if strings.TrimSpace(summary) != "" {
		body = append(body, "", summary)
	}
	if requestedMode := componentRequestedMode(def.Name, disposition, state); strings.TrimSpace(requestedMode) != "" {
		body = append(body, "", fmt.Sprintf("Requested mode: `%s`.", requestedMode))
	}
	if rendered := corecomponent.RenderDecisionFollowUps(def, corecomponent.ReviewDecision{
		Disposition: disposition,
		State:       state,
		Options:     followUps,
	}); rendered != "" {
		body = append(body, "", rendered)
	}
	if migration != "" && migration != corecomponent.DataMigrationNone {
		body = append(body, "", fmt.Sprintf("Migration impact: `%s`.", migration))
	}
	body = append(body, "", "This updates docker compose and Drupal config.")

	section := corecomponent.RenderSection("Confirm component change", strings.Join(body, "\n"))
	prompt := corecomponent.RenderPromptLine("Continue? [y/N]: ")
	return section + "\n\n" + prompt, nil
}

func runTLSComponentSet(cmd *cobra.Command, ctx *config.Context, name string, disposition corecomponent.Disposition, state corecomponent.State) error {
	mode, err := resolvedTLSMode(name, disposition, state)
	if err != nil {
		return err
	}

	switch name {
	case "isle-tls":
		if state == corecomponent.StateOn && mode == traefikconfig.ModeHTTP {
			return fmt.Errorf("isle-tls=on requires --tls-mode self-managed, mkcert, or letsencrypt")
		}
		if err := traefikconfig.ApplyProd(ctx.ProjectDir, mode); err != nil {
			return err
		}
	case "isle-tls-override":
		if err := traefikconfig.ApplyOverride(ctx.ProjectDir, resolveEnvironmentOverridePath(ctx), state == corecomponent.StateOn, mode); err != nil {
			return err
		}
		if err := ctx.EnsureTrackedComposeOverrideSymlink(); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported entrypoint component %q", name)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "%s: %s\n", name, disposition)
	return nil
}

func componentRequestedMode(name string, disposition corecomponent.Disposition, state corecomponent.State) string {
	mode, err := resolvedTLSMode(name, disposition, state)
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

func requiresTLSModeSelection(name string, disposition corecomponent.Disposition) bool {
	return disposition == corecomponent.DispositionEnabled && strings.TrimSpace(componentSetTLSMode) == "" &&
		(name == "isle-tls" || name == "isle-tls-override")
}

func resolveTLSComponentMode(name string) (string, error) {
	defaultValue, err := defaultTLSPromptMode(name)
	if err != nil {
		return "", err
	}
	return promptTLSComponentMode(name, defaultValue, componentSetInput, componentPromptChoice)
}

func resolvedTLSMode(name string, disposition corecomponent.Disposition, state corecomponent.State) (string, error) {
	switch name {
	case "isle-tls":
		if disposition != corecomponent.DispositionEnabled {
			return traefikconfig.ModeHTTP, nil
		}
		if strings.TrimSpace(componentSetTLSMode) == "" {
			return traefikconfig.ModeSelfManaged, nil
		}
		return componentSetTLSMode, nil
	case "isle-tls-override":
		if disposition != corecomponent.DispositionEnabled {
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

// isleSetRunner implements plugin.SetRunner for the isle plugin.
type isleSetRunner struct {
	drupalRootfs string
}

func (r *isleSetRunner) BindFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&statusPath, "path", "", "Project path override")
	corecomponent.AddDrupalRootfsFlag(cmd, &r.drupalRootfs, createpkg.DefaultDrupalRootfs)
	cmd.Flags().StringVar(&componentSetState, "state", "", "Component state to apply (on, off)")
	cmd.Flags().StringVar(&componentSetDisposition, "disposition", "", "Component disposition to apply")
	cmd.Flags().BoolVar(&componentSetYolo, "yolo", false, "Apply without confirmation")
	cmd.Flags().StringVar(&componentSetTLSMode, "tls-mode", "", "TLS mode for the selected component")
	addComponentSetFollowUpFlags(cmd, managedComponentDefinitions())
}

func (r *isleSetRunner) Run(cmd *cobra.Command, args []string, ctx *config.Context) error {
	statusDrupalRootfs = r.drupalRootfs
	stateValue, err := resolveComponentSetStateValue(cmd, args)
	if err != nil {
		return err
	}
	return runComponentSet(cmd, args[0], stateValue)
}

var _ plugin.SetRunner = (*isleSetRunner)(nil)

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

func addComponentSetFollowUpFlags(cmd *cobra.Command, defs []corecomponent.Definition) {
	for _, def := range defs {
		for _, followUp := range def.FollowUps {
			flagName := componentSetFollowUpFlagName(def.Name, followUp.Name)
			if flagName == "" || cmd.Flags().Lookup(flagName) != nil {
				continue
			}
			usage := strings.TrimSpace(followUp.FlagUsage)
			if usage == "" {
				label := strings.TrimSpace(followUp.Label)
				if label == "" {
					label = followUp.Name
				}
				usage = fmt.Sprintf("%s for %s", label, def.Name)
			}
			cmd.Flags().String(flagName, strings.TrimSpace(followUp.DefaultValue), usage)
		}
	}
}

func componentSetFollowUpFlagName(componentName, followUpName string) string {
	if strings.TrimSpace(componentName) == "" || strings.TrimSpace(followUpName) == "" {
		return ""
	}
	return componentName + "-" + followUpName
}

func resolveComponentSetFollowUps(cmd *cobra.Command, def corecomponent.Definition, view componentView, disposition corecomponent.Disposition, state corecomponent.State) (map[string]string, error) {
	options := map[string]string{}
	for _, spec := range def.FollowUpsForDisposition(disposition) {
		flagName := componentSetFollowUpFlagName(def.Name, spec.Name)
		switch {
		case spec.Name == "tls-mode" && (def.Name == "isle-tls" || def.Name == "isle-tls-override"):
			defaultValue := strings.TrimSpace(view.FollowUpValues[spec.Name])
			if defaultValue == "" {
				defaultValue = strings.TrimSpace(spec.DefaultValue)
			}
			if strings.TrimSpace(componentSetTLSMode) != "" {
				defaultValue = strings.TrimSpace(componentSetTLSMode)
			}
			options[spec.Name] = defaultValue
		case spec.Name == "tls-mode" && strings.TrimSpace(componentSetTLSMode) != "":
			options[spec.Name] = strings.TrimSpace(componentSetTLSMode)
		case cmd != nil && cmd.Flags().Lookup(flagName) != nil && cmd.Flags().Changed(flagName):
			value, err := cmd.Flags().GetString(flagName)
			if err != nil {
				return nil, err
			}
			options[spec.Name] = strings.TrimSpace(value)
		case componentSetYolo:
			defaultValue := strings.TrimSpace(view.FollowUpValues[spec.Name])
			if defaultValue == "" {
				defaultValue = strings.TrimSpace(spec.DefaultValue)
			}
			options[spec.Name] = defaultValue
		default:
			defaultValue := strings.TrimSpace(view.FollowUpValues[spec.Name])
			if defaultValue == "" {
				defaultValue = strings.TrimSpace(spec.DefaultValue)
			}
			value, err := corecomponent.PromptFollowUp(def.Name, spec, defaultValue, componentSetInput, componentPromptChoice)
			if err != nil {
				return nil, err
			}
			options[spec.Name] = strings.TrimSpace(value)
		}
	}
	return options, nil
}

func resolveComponentSetStateValue(cmd *cobra.Command, args []string) (string, error) {
	positionalState := ""
	if len(args) > 1 {
		positionalState = args[1]
	}
	flagChanged := cmd.Flags().Changed("state")
	dispositionChanged := cmd.Flags().Changed("disposition")
	if (positionalState != "" && flagChanged) || (positionalState != "" && dispositionChanged) || (flagChanged && dispositionChanged) {
		return "", fmt.Errorf("component setting specified twice: use either positional disposition, --state, or --disposition")
	}
	if positionalState != "" {
		return positionalState, nil
	}
	if dispositionChanged {
		return componentSetDisposition, nil
	}
	if flagChanged {
		return componentSetState, nil
	}
	return "", nil
}

func promptComponentSetState(status componentView) (corecomponent.Disposition, corecomponent.State, error) {
	guidance := status.Definition.Guidance
	guidance.DefaultState = corecomponent.ReviewDefaultState(status)
	guidance.Question = corecomponent.BuildReviewQuestion(status)
	if len(status.Definition.AllowedDispositions) > 0 {
		if componentPromptDisposition != nil {
			disposition, err := componentPromptDisposition(status.Name, guidance, status.Definition.AllowedDispositions, corecomponent.ReviewDefaultDisposition(status), componentSetInput)
			if err != nil {
				return "", "", err
			}
			return disposition, corecomponent.DispositionToState(disposition), nil
		}
		if componentPromptState != nil {
			state, err := componentPromptState(status.Name, guidance, componentSetInput)
			if err != nil {
				return "", "", err
			}
			disposition := corecomponent.LegacyDispositionForState(status.Definition.AllowedDispositions, state)
			return disposition, corecomponent.DispositionToState(disposition), nil
		}
		disposition, err := corecomponent.PromptDisposition(status.Name, guidance, status.Definition.AllowedDispositions, corecomponent.ReviewDefaultDisposition(status), componentSetInput)
		if err != nil {
			return "", "", err
		}
		return disposition, corecomponent.DispositionToState(disposition), nil
	}
	state, err := componentPromptState(status.Name, guidance, componentSetInput)
	if err != nil {
		return "", "", err
	}
	return corecomponent.StateToDisposition(state), state, nil
}

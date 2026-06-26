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
	"github.com/libops/sitectl/pkg/plugin"
	coretraefik "github.com/libops/sitectl/pkg/services/traefik"
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

const botMitigationTurnstileWarning = "Bot mitigation is using Cloudflare Turnstile test keys by default. Configure real TURNSTILE_SITE_KEY and TURNSTILE_SECRET_KEY values from Cloudflare; the test keys always allow JavaScript-capable bots to pass."

type componentSetOptions struct {
	Path           string
	CodebaseRootfs string
	DrupalRootfs   string
	State          string
	Disposition    string
	Yolo           bool
	TLSMode        string
}

func componentSetOptionsFromGlobals() componentSetOptions {
	return componentSetOptions{
		Path:           statusPath,
		CodebaseRootfs: statusCodebaseRootfs,
		DrupalRootfs:   statusDrupalRootfs,
		State:          componentSetState,
		Disposition:    componentSetDisposition,
		Yolo:           componentSetYolo,
		TLSMode:        componentSetTLSMode,
	}
}

func runComponentSet(cmd *cobra.Command, name, stateValue string) error {
	return runComponentSetWithOptions(cmd, name, stateValue, componentSetOptionsFromGlobals())
}

func runComponentSetWithOptions(cmd *cobra.Command, name, stateValue string, opts componentSetOptions) error {
	ctx, err := resolveStatusContextForPath(opts.Path)
	if err != nil {
		return err
	}
	if ctx.DockerHostType != config.ContextLocal {
		return fmt.Errorf("component changes are local-only; context %q is %q", ctx.Name, ctx.DockerHostType)
	}
	rootfs, err := resolveCodebaseRootfsForContext(cmd, ctx, opts.CodebaseRootfs, opts.DrupalRootfs)
	if err != nil {
		return err
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

	statuses, err := detectComponentViewsForContext(ctx, rootfs)
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
		if !blocksComponentSetOnDrift(name, componentName) {
			continue
		}
		if current == corecomponent.StateDrifted {
			return fmt.Errorf("component %q is drifted (%s); resolve it first or set it explicitly before changing %q", componentName, componentDriftSummary(statusByName[componentName], 6), name)
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
	disposition, state = normalizeComponentSetDispositionState(name, disposition, state)

	tlsMode := strings.TrimSpace(opts.TLSMode)
	followUps, err := resolveComponentSetFollowUps(cmd, def, statusByName[name], disposition, opts)
	if err != nil {
		return err
	}

	if !opts.Yolo {
		if requiresTLSModeSelection(name, disposition, tlsMode) {
			mode, err := resolveTLSComponentMode(name)
			if err != nil {
				return err
			}
			tlsMode = mode
			followUps["tls-mode"] = mode
		}
		prompt, err := componentSetPrompt(def, disposition, state, followUps, tlsMode)
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
		return runTLSComponentSet(cmd, ctx, name, disposition, state, tlsMode)
	}
	if name == "iiif" || name == "iiif-topology" {
		return runIIIFComponentSet(cmd, ctx, rootfs, name, disposition, statusByName, followUps, currentStates)
	}
	if createpkg.IsDerivativeService(name) {
		return runDerivativeServiceComponentSet(cmd, ctx, name, disposition)
	}
	if name == coretraefik.BotMitigationName {
		return runBotMitigationComponentSet(cmd, ctx, disposition, state)
	}
	applyOpts := createpkg.Options{
		Path:            ctx.ProjectDir,
		DrupalRootfs:    rootfs,
		Fcrepo:          string(resolveComponentCreateState("fcrepo", name, state, currentStates)),
		Blazegraph:      string(resolveComponentCreateState("blazegraph", name, state, currentStates)),
		IIIF:            resolveIIIFCreateValue(name, disposition, currentStates),
		IIIFTopology:    resolveIIIFTopologyCreateValue(name, disposition, currentStates),
		IIIFUpstreamURL: resolveIIIFTopologyUpstream(name, followUps, statusByName),
		ComposeOverride: resolveEnvironmentOverridePath(ctx),
		Codebase:        resolveCodebaseCreateValue(name, disposition, currentStates),
	}
	if applyOpts.Fcrepo == "" {
		applyOpts.Fcrepo = createpkg.FcrepoStateOn
	}
	if applyOpts.Blazegraph == "" {
		applyOpts.Blazegraph = createpkg.FcrepoStateOn
	}
	if applyOpts.IIIF == "" {
		applyOpts.IIIF = createpkg.IIIFCantaloupe
	}
	if applyOpts.IIIFTopology == "" {
		applyOpts.IIIFTopology = createpkg.IIIFTopologyLocal
	}
	if applyOpts.Codebase == "" {
		applyOpts.Codebase = createpkg.CodebaseNested
	}

	if applyOpts.Fcrepo == createpkg.FcrepoStateOff {
		if scheme := selectedFcrepoFileSystemURI(cmd, followUps, opts); scheme != "" {
			applyOpts.ISLEFileSystemURI = scheme
		} else {
			scheme, err := resolveCurrentFileSystemURI(ctx.ProjectDir, rootfs)
			if err != nil {
				return err
			}
			applyOpts.ISLEFileSystemURI = scheme
		}
	}

	if err := componentApplyOptions(applyOpts); err != nil {
		return err
	}
	if err := ctx.EnsureTrackedComposeOverrideSymlink(); err != nil {
		return err
	}
	if err := updateContextRootfsForCodebase(ctx, opts.Path, name, applyOpts.Codebase); err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "%s: %s", name, disposition)
	if value := strings.TrimSpace(followUps["upstream-url"]); value != "" {
		fmt.Fprintf(cmd.OutOrStdout(), " (%s)", value)
	}
	fmt.Fprintln(cmd.OutOrStdout())
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
	defs := []corecomponent.Definition{
		components.Fcrepo(components.TemplateSource{}),
		components.Blazegraph(components.TemplateSource{}),
		components.IIIF(components.TemplateSource{}),
		components.IIIFTopology(),
		components.Codebase(),
		coretraefik.BotMitigation(isleBotMitigationOptions()),
	}
	defs = append(defs, components.DerivativeServices()...)
	return defs
}

func managedComponentDefinitions() []corecomponent.Definition {
	defs := append([]corecomponent.Definition{}, orderedComponentDefinitions()...)
	defs = append(defs,
		components.ISLEEntrypoint(),
		components.ISLEEntrypointOverride(),
	)
	return defs
}

func componentCatalogDefinitions() []corecomponent.Definition {
	defs := managedComponentDefinitions()
	for i := range defs {
		for j := range defs[i].FollowUps {
			if defs[i].FollowUps[j].Name == "tls-mode" && (defs[i].Name == "isle-tls" || defs[i].Name == "isle-tls-override") {
				defs[i].FollowUps[j].FlagName = "tls-mode"
			}
		}
	}
	return defs
}

func blocksComponentSetOnDrift(targetName, driftedName string) bool {
	if createpkg.IsDerivativeService(targetName) {
		return false
	}
	switch targetName {
	case "iiif", "iiif-topology", "codebase", "isle-tls", "isle-tls-override", coretraefik.BotMitigationName:
		return false
	}
	switch driftedName {
	case "iiif", "iiif-topology", "codebase", "isle-tls", "isle-tls-override", coretraefik.BotMitigationName:
		return false
	default:
		return !createpkg.IsDerivativeService(driftedName)
	}
}

func normalizeComponentSetDispositionState(name string, disposition corecomponent.Disposition, state corecomponent.State) (corecomponent.Disposition, corecomponent.State) {
	if !createpkg.IsDerivativeService(name) {
		return disposition, state
	}
	switch disposition {
	case corecomponent.DispositionDistributed:
		return disposition, corecomponent.StateOn
	case corecomponent.DispositionEnabled:
		return disposition, corecomponent.StateOff
	default:
		return disposition, state
	}
}

func runIIIFComponentSet(cmd *cobra.Command, ctx *config.Context, drupalRootfs, name string, disposition corecomponent.Disposition, statusByName map[string]componentView, followUps map[string]string, currentStates map[string]corecomponent.DetectedState) error {
	opts := createpkg.Options{
		Path:            ctx.ProjectDir,
		DrupalRootfs:    drupalRootfs,
		IIIF:            resolveIIIFCreateValue(name, disposition, currentStates),
		IIIFTopology:    resolveIIIFTopologyCreateValue(name, disposition, currentStates),
		IIIFUpstreamURL: resolveIIIFTopologyUpstream(name, followUps, statusByName),
		ComposeOverride: resolveEnvironmentOverridePath(ctx),
	}
	if opts.IIIF == "" {
		opts.IIIF = createpkg.IIIFCantaloupe
	}
	if opts.IIIFTopology == "" {
		opts.IIIFTopology = createpkg.IIIFTopologyLocal
	}
	if err := createpkg.ApplyIIIF(opts); err != nil {
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

func runBotMitigationComponentSet(cmd *cobra.Command, ctx *config.Context, disposition corecomponent.Disposition, state corecomponent.State) error {
	target := coretraefik.BotMitigationStateOff
	if state == corecomponent.StateOn {
		target = coretraefik.BotMitigationStateOn
	}
	if err := coretraefik.ApplyBotMitigation(ctx.ProjectDir, target, isleBotMitigationOptions()); err != nil {
		return err
	}
	if err := ctx.EnsureTrackedComposeOverrideSymlink(); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s: %s\n", coretraefik.BotMitigationName, disposition)
	if state == corecomponent.StateOn {
		fmt.Fprintln(cmd.OutOrStdout(), botMitigationTurnstileWarning)
	}
	return nil
}

func runDerivativeServiceComponentSet(cmd *cobra.Command, ctx *config.Context, name string, disposition corecomponent.Disposition) error {
	topology := createpkg.DerivativeTopologyLocal
	if disposition == corecomponent.DispositionDistributed {
		topology = createpkg.DerivativeTopologyDistributed
	}
	opts := createpkg.Options{
		Path: ctx.ProjectDir,
		DerivativeServices: map[string]string{
			name: topology,
		},
	}
	if err := createpkg.ApplyDerivativeServices(opts); err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "%s: %s\n", name, disposition)
	return nil
}

func isleBotMitigationOptions() coretraefik.BotMitigationOptions {
	return coretraefik.BotMitigationOptions{
		RouterName:       "drupal",
		RouterConfigPath: "conf/traefik/drupal.yml",
	}
}

func resolveIIIFCreateValue(targetName string, targetDisposition corecomponent.Disposition, current map[string]corecomponent.DetectedState) string {
	if targetName == "iiif" {
		if targetDisposition == corecomponent.DispositionTriplet {
			return createpkg.IIIFTriplet
		}
		return createpkg.IIIFCantaloupe
	}
	if current["iiif"] == corecomponent.DetectedState(corecomponent.StateOn) {
		return createpkg.IIIFTriplet
	}
	return createpkg.IIIFCantaloupe
}

func resolveIIIFTopologyCreateValue(targetName string, targetDisposition corecomponent.Disposition, current map[string]corecomponent.DetectedState) string {
	if targetName == "iiif-topology" {
		if targetDisposition == corecomponent.DispositionDistributed {
			return createpkg.IIIFTopologyExternal
		}
		return createpkg.IIIFTopologyLocal
	}
	if current["iiif-topology"] == corecomponent.DetectedState(corecomponent.StateOn) {
		return createpkg.IIIFTopologyExternal
	}
	return createpkg.IIIFTopologyLocal
}

func resolveIIIFTopologyUpstream(targetName string, followUps map[string]string, statusByName map[string]componentView) string {
	if targetName == "iiif-topology" {
		return strings.TrimSpace(followUps["upstream-url"])
	}
	return strings.TrimSpace(statusByName["iiif-topology"].FollowUpValues["upstream-url"])
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

func resolveCodebaseCreateValue(targetName string, targetDisposition corecomponent.Disposition, current map[string]corecomponent.DetectedState) string {
	if targetName == "codebase" {
		if targetDisposition == corecomponent.DispositionGitRoot {
			return createpkg.CodebaseGitRoot
		}
		return createpkg.CodebaseNested
	}
	if current["codebase"] == corecomponent.DetectedState(corecomponent.StateOn) {
		return createpkg.CodebaseGitRoot
	}
	return createpkg.CodebaseNested
}

func updateContextRootfsForCodebase(ctx *config.Context, pathOverride, targetName, codebase string) error {
	if ctx == nil || strings.TrimSpace(pathOverride) != "" || targetName != "codebase" || codebase != createpkg.CodebaseGitRoot {
		return nil
	}
	ctx.DrupalRootfs = corecomponent.DefaultDrupalRootfs
	return config.SaveContext(ctx, false)
}

func componentSetPrompt(def corecomponent.Definition, disposition corecomponent.Disposition, state corecomponent.State, followUps map[string]string, tlsMode string) (string, error) {
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
	if requestedMode := componentRequestedMode(def.Name, disposition, tlsMode); strings.TrimSpace(requestedMode) != "" {
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

func runTLSComponentSet(cmd *cobra.Command, ctx *config.Context, name string, disposition corecomponent.Disposition, state corecomponent.State, tlsMode string) error {
	mode, err := resolvedTLSMode(name, disposition, tlsMode)
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

func componentRequestedMode(name string, disposition corecomponent.Disposition, tlsMode string) string {
	mode, err := resolvedTLSMode(name, disposition, tlsMode)
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

func requiresTLSModeSelection(name string, disposition corecomponent.Disposition, tlsMode string) bool {
	return disposition == corecomponent.DispositionEnabled && strings.TrimSpace(tlsMode) == "" &&
		(name == "isle-tls" || name == "isle-tls-override")
}

func resolveTLSComponentMode(name string) (string, error) {
	defaultValue, err := defaultTLSPromptMode(name)
	if err != nil {
		return "", err
	}
	return promptTLSComponentMode(name, defaultValue, componentSetInput, componentPromptChoice)
}

func resolvedTLSMode(name string, disposition corecomponent.Disposition, tlsMode string) (string, error) {
	tlsMode = strings.TrimSpace(tlsMode)
	switch name {
	case "isle-tls":
		if disposition != corecomponent.DispositionEnabled {
			return traefikconfig.ModeHTTP, nil
		}
		if tlsMode == "" {
			return traefikconfig.ModeSelfManaged, nil
		}
		return tlsMode, nil
	case "isle-tls-override":
		if disposition != corecomponent.DispositionEnabled {
			return traefikconfig.ModeInherited, nil
		}
		if tlsMode == "" {
			return traefikconfig.ModeMkcert, nil
		}
		return tlsMode, nil
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
	codebaseRootfs string
	drupalRootfs   string
	path           string
	state          string
	disposition    string
	yolo           bool
	tlsMode        string
}

func (r *isleSetRunner) BindFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&r.path, "path", "", "Project path override")
	addCodebaseRootfsFlags(cmd, &r.codebaseRootfs, &r.drupalRootfs, createpkg.DefaultDrupalRootfs)
	cmd.Flags().StringVar(&r.state, "state", "", "Component state to apply (on, off)")
	cmd.Flags().StringVar(&r.disposition, "disposition", "", "Component disposition to apply")
	cmd.Flags().BoolVar(&r.yolo, "yolo", false, "Apply without confirmation")
	cmd.Flags().StringVar(&r.tlsMode, "tls-mode", "", "TLS mode for the selected component")
	addComponentSetFollowUpFlags(cmd, managedComponentDefinitions())
}

func (r *isleSetRunner) Run(cmd *cobra.Command, args []string, ctx *config.Context) error {
	stateValue, err := resolveComponentSetStateValueFrom(cmd, args, r.state, r.disposition)
	if err != nil {
		return err
	}
	return runComponentSetWithOptions(cmd, args[0], stateValue, componentSetOptions{
		Path:           r.path,
		CodebaseRootfs: r.codebaseRootfs,
		DrupalRootfs:   r.drupalRootfs,
		State:          r.state,
		Disposition:    r.disposition,
		Yolo:           r.yolo,
		TLSMode:        r.tlsMode,
	})
}

var _ plugin.SetRunner = (*isleSetRunner)(nil)

func resolveCurrentFileSystemURI(projectDir, drupalRootfs string) (string, error) {
	layout := corecomponent.ResolveDrupalLayout(projectDir, drupalRootfs)
	fieldPath := filepath.Join(layout.ConfigSyncDir(), "field.storage.media.field_media_file.yml")
	data, err := os.ReadFile(fieldPath) // #nosec G304 -- config sync path is resolved inside the selected ISLE project.
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

func selectedFcrepoFileSystemURI(cmd *cobra.Command, followUps map[string]string, opts componentSetOptions) string {
	scheme := strings.TrimSpace(followUps["isle-file-system-uri"])
	if scheme == "" {
		return ""
	}
	if !opts.Yolo {
		return scheme
	}
	if cmd == nil || cmd.Flags().Lookup("isle-file-system-uri") == nil || !cmd.Flags().Changed("isle-file-system-uri") {
		return ""
	}
	return scheme
}

func addComponentSetFollowUpFlags(cmd *cobra.Command, defs []corecomponent.Definition) {
	for _, def := range defs {
		for _, followUp := range def.FollowUps {
			if followUp.Name == "tls-mode" && (def.Name == "isle-tls" || def.Name == "isle-tls-override") {
				continue
			}
			flagName := componentSetFollowUpSpecFlagName(def.Name, followUp)
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

func resolveComponentSetFollowUps(cmd *cobra.Command, def corecomponent.Definition, view componentView, disposition corecomponent.Disposition, opts componentSetOptions) (map[string]string, error) {
	options := map[string]string{}
	for _, spec := range def.FollowUpsForDisposition(disposition) {
		flagName := componentSetFollowUpSpecFlagName(def.Name, spec)
		switch {
		case spec.Name == "tls-mode" && (def.Name == "isle-tls" || def.Name == "isle-tls-override"):
			defaultValue := strings.TrimSpace(view.FollowUpValues[spec.Name])
			if defaultValue == "" {
				defaultValue = strings.TrimSpace(spec.DefaultValue)
			}
			if strings.TrimSpace(opts.TLSMode) != "" {
				defaultValue = strings.TrimSpace(opts.TLSMode)
			}
			options[spec.Name] = defaultValue
		case spec.Name == "tls-mode" && strings.TrimSpace(opts.TLSMode) != "":
			options[spec.Name] = strings.TrimSpace(opts.TLSMode)
		case cmd != nil && cmd.Flags().Lookup(flagName) != nil && cmd.Flags().Changed(flagName):
			value, err := cmd.Flags().GetString(flagName)
			if err != nil {
				return nil, err
			}
			options[spec.Name] = strings.TrimSpace(value)
		case opts.Yolo:
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

func componentSetFollowUpSpecFlagName(componentName string, followUp corecomponent.FollowUpSpec) string {
	if strings.TrimSpace(followUp.FlagName) != "" {
		return strings.TrimSpace(followUp.FlagName)
	}
	return componentSetFollowUpFlagName(componentName, followUp.Name)
}

func resolveComponentSetStateValue(cmd *cobra.Command, args []string) (string, error) {
	return resolveComponentSetStateValueFrom(cmd, args, componentSetState, componentSetDisposition)
}

func resolveComponentSetStateValueFrom(cmd *cobra.Command, args []string, stateFlag, dispositionFlag string) (string, error) {
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
		return dispositionFlag, nil
	}
	if flagChanged {
		return stateFlag, nil
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

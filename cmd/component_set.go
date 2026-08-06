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
	"github.com/libops/sitectl/pkg/plugin"
	coredevmode "github.com/libops/sitectl/pkg/services/devmode"
	coretraefik "github.com/libops/sitectl/pkg/services/traefik"
	"github.com/spf13/cobra"
)

var (
	componentSetYolo           bool
	componentSetState          string
	componentSetDisposition    string
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
}

func componentSetOptionsFromGlobals() componentSetOptions {
	return componentSetOptions{
		Path:           statusPath,
		CodebaseRootfs: statusCodebaseRootfs,
		DrupalRootfs:   statusDrupalRootfs,
		State:          componentSetState,
		Disposition:    componentSetDisposition,
		Yolo:           componentSetYolo,
	}
}

func runComponentSet(cmd *cobra.Command, name, stateValue string) error {
	return runComponentSetWithOptions(cmd, name, stateValue, componentSetOptionsFromGlobals())
}

func runComponentSetWithOptions(cmd *cobra.Command, name, stateValue string, opts componentSetOptions) (retErr error) {
	ctx, err := resolveStatusContextForPath(opts.Path)
	if err != nil {
		return err
	}
	legacyCompose, err := usesLegacyComposeFilename(ctx)
	if err != nil {
		return err
	}
	if err := normalizeComposeProjectFilename(ctx); err != nil {
		return err
	}
	if legacyCompose {
		defer func() {
			if err := restoreLegacyComposeProjectFilename(ctx); err != nil && retErr == nil {
				retErr = err
			}
		}()
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
	remoteContext := ctx.DockerHostType != config.ContextLocal
	if remoteContext {
		if !remoteComponentSetAllowed(name) {
			return fmt.Errorf("component %q changes are local-only; context %q is %q", name, ctx.Name, ctx.DockerHostType)
		}
		fmt.Fprintln(cmd.ErrOrStderr(), "Warning: modifying remote project files directly; commit and review these changes through version control before promoting them.")
	}
	if disposition != "" {
		disposition, err = corecomponent.ResolveAllowedDisposition(def.AllowedDispositions, disposition)
		if err != nil {
			return err
		}
		state = corecomponent.DispositionToState(disposition)
	}

	assistantDevMode := name == coredevmode.Name && strings.TrimSpace(stateValue) == "" && componentSetBoolFlagValue(cmd, "assistant")
	if assistantDevMode {
		disposition = corecomponent.DispositionEnabled
		state = corecomponent.StateOn
	}

	var statuses []componentView
	if remoteContext {
		statuses, err = detectComponentViewsForDefinitions(ctx, rootfs, def)
	} else {
		statuses, err = detectComponentViewsForContext(ctx, rootfs)
	}
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
			if inferred, ok := inferEnabledRepositoryComponent(ctx, componentName); ok {
				currentStates[componentName] = inferred
				continue
			}
			return fmt.Errorf("component %q is drifted (%s); resolve it first or set it explicitly before changing %q", componentName, componentDriftSummary(statusByName[componentName], 6), name)
		}
	}

	if strings.TrimSpace(stateValue) == "" && !assistantDevMode {
		status, ok := statusByName[name]
		if !ok {
			return fmt.Errorf("missing detected status for component %q", name)
		}
		if opts.Yolo {
			disposition, state, err = defaultComponentSetState(status)
		} else {
			disposition, state, err = promptComponentSetState(status)
		}
		if err != nil {
			return err
		}
	}
	if disposition == "" {
		disposition = corecomponent.StateToDisposition(state)
	}
	disposition, state = normalizeComponentSetDispositionState(name, disposition, state)

	followUps, err := resolveComponentSetFollowUps(cmd, def, statusByName[name], disposition, opts)
	if err != nil {
		return err
	}
	defer func() {
		if retErr != nil {
			return
		}
		desired, err := corecomponent.LoadOrInitializeDesiredState(ctx, orderedComponentDefinitions())
		if err != nil {
			retErr = fmt.Errorf("load component desired state: %w", err)
			return
		}
		if err := desired.Set(def, disposition, followUps); err != nil {
			retErr = err
			return
		}
		if err := corecomponent.SaveDesiredState(ctx, desired); err != nil {
			retErr = fmt.Errorf("save component desired state: %w", err)
		}
	}()

	if !opts.Yolo {
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

	if name == "iiif" || name == "iiif-topology" {
		return runIIIFComponentSet(cmd, ctx, rootfs, name, disposition, statusByName, followUps, currentStates)
	}
	if createpkg.IsDerivativeService(name) {
		return runDerivativeServiceComponentSet(cmd, ctx, name, disposition)
	}
	if createpkg.IsFeatureBundle(name) {
		return runFeatureBundleComponentSet(cmd, ctx, rootfs, name, state, followUps)
	}
	if name == coretraefik.IngressName {
		return runIngressComponentSet(cmd, ctx, disposition, state, followUps)
	}
	if name == coredevmode.Name {
		return runDevModeComponentSet(cmd, ctx, disposition, state, followUps)
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
		applyOpts.Fcrepo = createpkg.FcrepoStateOff
	}
	if applyOpts.Blazegraph == "" {
		applyOpts.Blazegraph = createpkg.FcrepoStateOff
	}
	if applyOpts.IIIF == "" {
		applyOpts.IIIF = createpkg.IIIFTriplet
	}
	if applyOpts.IIIFTopology == "" {
		applyOpts.IIIFTopology = createpkg.IIIFTopologyLocal
	}
	if applyOpts.Codebase == "" {
		applyOpts.Codebase = createpkg.CodebaseGitRoot
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

func usesLegacyComposeFilename(ctx *config.Context) (bool, error) {
	if ctx == nil || ctx.DockerHostType != config.ContextLocal {
		return false, nil
	}
	canonical := ctx.ResolveProjectPath("compose.yaml")
	if _, err := os.Stat(canonical); err == nil {
		return false, nil
	} else if !os.IsNotExist(err) {
		return false, fmt.Errorf("inspect canonical Compose file: %w", err)
	}
	legacy := ctx.ResolveProjectPath("docker-compose.yml")
	if _, err := os.Stat(legacy); err == nil {
		return true, nil
	} else if !os.IsNotExist(err) {
		return false, fmt.Errorf("inspect legacy Compose file: %w", err)
	}
	return false, nil
}

func restoreLegacyComposeProjectFilename(ctx *config.Context) error {
	canonical := ctx.ResolveProjectPath("compose.yaml")
	legacy := ctx.ResolveProjectPath("docker-compose.yml")
	data, err := os.ReadFile(canonical)
	if err != nil {
		return fmt.Errorf("read canonical Compose file for legacy cleanup: %w", err)
	}
	data = []byte(strings.ReplaceAll(string(data), "./compose.yaml:/docker-compose.yml", "./docker-compose.yml:/docker-compose.yml"))
	if err := os.WriteFile(canonical, data, 0o644); err != nil {
		return fmt.Errorf("restore legacy Compose self-mount: %w", err)
	}
	if err := os.Rename(canonical, legacy); err != nil {
		return fmt.Errorf("restore legacy Compose project filename: %w", err)
	}
	return nil
}

func remoteComponentSetAllowed(name string) bool {
	switch name {
	case coretraefik.IngressName, coredevmode.Name:
		return true
	default:
		return false
	}
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
	ingress, err := isleIngressComponent()
	if err != nil {
		panic(err)
	}
	defs := []corecomponent.Definition{
		components.Fcrepo(components.TemplateSource{}),
		components.Blazegraph(components.TemplateSource{}),
		components.IIIF(components.TemplateSource{}),
		components.IIIFTopology(),
		components.Codebase(),
		ingress.Definition(),
		isleDevModeDefinition(),
		coretraefik.BotMitigation(createpkg.BotMitigationOptions()),
	}
	defs = append(defs, components.FeatureBundles(components.TemplateSource{})...)
	defs = append(defs, components.DerivativeServices()...)
	return defs
}

func managedComponentDefinitions() []corecomponent.Definition {
	return append([]corecomponent.Definition{}, orderedComponentDefinitions()...)
}

func componentCatalogDefinitions() []corecomponent.Definition {
	return managedComponentDefinitions()
}

func blocksComponentSetOnDrift(targetName, driftedName string) bool {
	if createpkg.IsDerivativeService(targetName) || createpkg.IsFeatureBundle(targetName) {
		return false
	}
	switch targetName {
	case "iiif", "iiif-topology", "codebase", coredevmode.Name, coretraefik.IngressName, coretraefik.BotMitigationName:
		return false
	}
	switch driftedName {
	case "iiif", "iiif-topology", "codebase", coredevmode.Name, coretraefik.IngressName, coretraefik.BotMitigationName:
		return false
	default:
		return !createpkg.IsDerivativeService(driftedName) && !createpkg.IsFeatureBundle(driftedName)
	}
}

// inferEnabledRepositoryComponent preserves an older template's repository
// service when newer rules classify its surrounding configuration as drifted.
// Absence is not enough to infer disabled because a partially removed service
// is exactly the ambiguity the drift guard is intended to catch.
func inferEnabledRepositoryComponent(ctx *config.Context, name string) (corecomponent.DetectedState, bool) {
	if name != "fcrepo" && name != "blazegraph" {
		return "", false
	}
	compose, err := corecomponent.LoadComposeFileForContext(ctx, ctx.ResolveProjectPath("compose.yaml"))
	if err != nil || !compose.HasService(name) {
		return "", false
	}
	return corecomponent.DetectedState(corecomponent.StateOn), true
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
		opts.IIIF = createpkg.IIIFTriplet
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
	if err := createpkg.ApplyBotMitigation(ctx.ProjectDir, target); err != nil {
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

func runIngressComponentSet(cmd *cobra.Command, ctx *config.Context, disposition corecomponent.Disposition, state corecomponent.State, followUps map[string]string) error {
	component, err := isleIngressComponent()
	if err != nil {
		return err
	}
	manager := corecomponent.NewManager(ctx)
	spec := component.SpecForWithOptions(state, followUps)
	switch state {
	case corecomponent.StateOn:
		if err := manager.EnableComponentWithOptions(cmd.Context(), spec, corecomponent.ApplyOptions{Yolo: true}); err != nil {
			return err
		}
		if err := applyISLEIngressFiles(ctx, followUps); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported ingress state %q", state)
	}
	if err := ctx.EnsureTrackedComposeOverrideSymlink(); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s: %s", coretraefik.IngressName, disposition)
	if mode := strings.TrimSpace(followUps["mode"]); mode != "" {
		fmt.Fprintf(cmd.OutOrStdout(), " (%s)", mode)
	}
	fmt.Fprintln(cmd.OutOrStdout())
	return nil
}

func isleIngressComponent() (corecomponent.ComposeServiceComponent, error) {
	return coretraefik.Ingress(coretraefik.IngressOptions{
		AppService:      "drupal",
		HTTPEntrypoint:  "http",
		HTTPSEntrypoint: "https",
		RouterHosts: map[string]string{
			"activemq":   "activemq.{domain}",
			"blazegraph": "blazegraph.{domain}",
			"drupal":     "{domain}",
			"fcrepo":     "fcrepo.{domain}",
			"solr":       "solr.{domain}",
			"traefik":    "traefik.{domain}",
			"triplet":    "{domain}",
			"cantaloupe": "{domain}",
		},
		AppEnvDeletes: []string{
			"DRUPAL_DEFAULT_SITE_URL",
			"DRUPAL_ENABLE_HTTPS",
			"DRUPAL_TRUSTED_HOST_PATTERNS",
			"DRUSH_OPTIONS_URI",
		},
		ServiceEnvTemplates: map[string]map[string]string{
			"drupal": {
				"DRUPAL_DEFAULT_CANTALOUPE_URL": "{base_url}/iiif/3",
			},
			"fcrepo": {
				"FCREPO_ALLOW_EXTERNAL_DRUPAL": "{base_url}/",
			},
			"triplet": {
				"TRIPLET_PUBLIC_BASE_URL": "{base_url}",
			},
		},
	})
}

func uploadLimitValue(values map[string]string, key string) string {
	value := strings.TrimSpace(values[key])
	if value != "" {
		return value
	}
	switch key {
	case "max-upload-size":
		return coretraefik.DefaultMaxUploadSize
	case "upload-timeout":
		return coretraefik.DefaultUploadTimeout
	default:
		return ""
	}
}

func runDevModeComponentSet(cmd *cobra.Command, ctx *config.Context, disposition corecomponent.Disposition, state corecomponent.State, followUps map[string]string) error {
	component, err := isleDevModeComponent()
	if err != nil {
		return err
	}
	manager := corecomponent.NewManager(ctx)
	spec := component.SpecForWithOptions(state, followUps)
	switch state {
	case corecomponent.StateOn:
		if err := manager.EnableComponentWithOptions(cmd.Context(), spec, corecomponent.ApplyOptions{Yolo: true}); err != nil {
			return err
		}
		if err := applyISLEDevMode(ctx, true); err != nil {
			return err
		}
	case corecomponent.StateOff:
		if err := manager.DisableComponentWithOptions(cmd.Context(), spec, corecomponent.ApplyOptions{Yolo: true}); err != nil {
			return err
		}
		if err := applyISLEDevMode(ctx, false); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported dev mode state %q", state)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s: %s\n", coredevmode.Name, disposition)
	return nil
}

func isleDevModeDefinition() corecomponent.Definition {
	component, err := isleDevModeComponent()
	if err != nil {
		panic(err)
	}
	return component.Definition()
}

func isleDevModeComponent() (corecomponent.ComposeServiceComponent, error) {
	return coredevmode.Component(coredevmode.Options{
		AppService: "drupal",
		Environment: map[string]string{
			"DEVELOPMENT_ENVIRONMENT": "true",
			"UID":                     "${UID:-1000}",
		},
		Volumes: []string{
			"./assets:/var/www/drupal/assets:z,rw",
			"./composer.json:/var/www/drupal/composer.json:z,rw",
			"./composer.lock:/var/www/drupal/composer.lock:z,rw",
			"./config:/var/www/drupal/config:z,rw",
			"./web/modules/custom:/var/www/drupal/web/modules/custom:z,rw",
			"./web/themes/custom:/var/www/drupal/web/themes/custom:z,rw",
		},
	})
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

func runFeatureBundleComponentSet(cmd *cobra.Command, ctx *config.Context, drupalRootfs, name string, state corecomponent.State, followUps map[string]string) error {
	opts := createpkg.Options{
		Path:         ctx.ProjectDir,
		DrupalRootfs: drupalRootfs,
		EnvFiles:     append([]string{}, ctx.EnvFile...),
		FeatureBundles: map[string]string{
			name: string(state),
		},
		FeatureBundleOptions: map[string]map[string]string{
			name: followUps,
		},
	}
	if err := createpkg.ApplyFeatureBundles(opts); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s: %s\n", name, corecomponent.StateToDisposition(state))
	if state == corecomponent.StateOn {
		switch name {
		case createpkg.FeatureBundleMergePDF:
			fmt.Fprintln(cmd.OutOrStdout(), "Next: rebuild and deploy Drupal, import configuration, then backfill aggregated PDFs for existing paged content if needed.")
		case createpkg.FeatureBundleHOCRSearch:
			fmt.Fprintln(cmd.OutOrStdout(), "Next: update the Composer lock file for the hOCR packages, rebuild and deploy Drupal, import configuration, generate hOCR for existing images, then reindex Solr.")
		}
	} else {
		switch name {
		case createpkg.FeatureBundleMergePDF:
			fmt.Fprintln(cmd.OutOrStdout(), "Next: rebuild and redeploy the Compose project, then import Drupal configuration. Existing aggregated PDFs are retained.")
		case createpkg.FeatureBundleHOCRSearch:
			fmt.Fprintln(cmd.OutOrStdout(), "Next: update the Composer lock file, rebuild and deploy Drupal, import configuration, then clear or reindex stale hOCR search data as required.")
		}
	}
	return nil
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

func confirmComponentSet(prompt string) (bool, error) {
	response, err := componentSetInput(prompt)
	if err != nil {
		return false, err
	}
	value := strings.TrimSpace(strings.ToLower(response))
	return value == "y" || value == "yes", nil
}

// isleSetRunner implements plugin.SetRunner for the isle plugin.
type isleSetRunner struct {
	codebaseRootfs string
	drupalRootfs   string
	path           string
	state          string
	disposition    string
	yolo           bool
}

func (r *isleSetRunner) BindFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&r.path, "path", "", "Project path override")
	addCodebaseRootfsFlags(cmd, &r.codebaseRootfs, &r.drupalRootfs, createpkg.DefaultDrupalRootfs)
	cmd.Flags().StringVar(&r.state, "state", "", "Component state to apply (on, off)")
	cmd.Flags().StringVar(&r.disposition, "disposition", "", "Component disposition to apply")
	cmd.Flags().BoolVar(&r.yolo, "yolo", false, "Apply without confirmation")
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
			if followUp.MultiValue {
				cmd.Flags().StringArray(flagName, corecomponent.SplitFollowUpValues(followUp.DefaultValue), usage)
			} else if followUp.BoolValue {
				cmd.Flags().Bool(flagName, corecomponent.FollowUpBoolDefault(followUp), usage)
			} else {
				cmd.Flags().String(flagName, strings.TrimSpace(followUp.DefaultValue), usage)
			}
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
		case spec.Name == "acme-email" && def.Name == coretraefik.IngressName && !coretraefik.IngressModeRequiresACMEEmail(resolvedIngressMode(options, view, def)):
			options[spec.Name] = strings.TrimSpace(view.FollowUpValues[spec.Name])
		case cmd != nil && cmd.Flags().Lookup(flagName) != nil && cmd.Flags().Changed(flagName):
			if spec.BoolValue {
				value, err := cmd.Flags().GetBool(flagName)
				if err != nil {
					return nil, err
				}
				options[spec.Name] = corecomponent.FormatFollowUpBool(value)
			} else if spec.MultiValue {
				values, err := cmd.Flags().GetStringArray(flagName)
				if err != nil {
					return nil, err
				}
				options[spec.Name] = corecomponent.JoinFollowUpValues(values)
			} else {
				value, err := cmd.Flags().GetString(flagName)
				if err != nil {
					return nil, err
				}
				options[spec.Name] = strings.TrimSpace(value)
			}
		case opts.Yolo:
			defaultValue := strings.TrimSpace(view.FollowUpValues[spec.Name])
			if defaultValue == "" {
				defaultValue = strings.TrimSpace(spec.DefaultValue)
			}
			if spec.BoolValue {
				options[spec.Name] = corecomponent.FormatFollowUpBool(corecomponent.FollowUpBoolDefault(spec))
			} else if spec.MultiValue {
				options[spec.Name] = corecomponent.NormalizeFollowUpValue(defaultValue)
			} else {
				options[spec.Name] = defaultValue
			}
		default:
			defaultValue := strings.TrimSpace(view.FollowUpValues[spec.Name])
			if defaultValue == "" {
				defaultValue = strings.TrimSpace(spec.DefaultValue)
			}
			value, err := corecomponent.PromptFollowUp(def.Name, spec, defaultValue, componentSetInput, componentPromptChoice)
			if err != nil {
				return nil, err
			}
			if spec.MultiValue {
				options[spec.Name] = corecomponent.NormalizeFollowUpValue(value)
			} else {
				options[spec.Name] = strings.TrimSpace(value)
			}
		}
		if spec.Required && !corecomponent.FollowUpValuePresent(options[spec.Name]) {
			flagName := componentSetFollowUpSpecFlagName(def.Name, spec)
			if flagName != "" {
				return nil, fmt.Errorf("--%s is required when enabling component %q", flagName, def.Name)
			}
			return nil, fmt.Errorf("%s is required when enabling component %q", spec.Name, def.Name)
		}
	}
	return options, nil
}

func resolvedIngressMode(options map[string]string, view componentView, def corecomponent.Definition) string {
	if mode := strings.TrimSpace(options["mode"]); mode != "" {
		return mode
	}
	if mode := strings.TrimSpace(view.FollowUpValues["mode"]); mode != "" {
		return mode
	}
	for _, spec := range def.FollowUps {
		if spec.Name == "mode" && strings.TrimSpace(spec.DefaultValue) != "" {
			return strings.TrimSpace(spec.DefaultValue)
		}
	}
	return coretraefik.IngressModeHTTP
}

func componentSetFollowUpSpecFlagName(componentName string, followUp corecomponent.FollowUpSpec) string {
	if strings.TrimSpace(followUp.FlagName) != "" {
		return strings.TrimSpace(followUp.FlagName)
	}
	return componentSetFollowUpFlagName(componentName, followUp.Name)
}

func componentSetBoolFlagValue(cmd *cobra.Command, flagName string) bool {
	if cmd == nil || strings.TrimSpace(flagName) == "" || cmd.Flags().Lookup(flagName) == nil || !cmd.Flags().Changed(flagName) {
		return false
	}
	value, err := cmd.Flags().GetBool(flagName)
	return err == nil && value
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

func defaultComponentSetState(status componentView) (corecomponent.Disposition, corecomponent.State, error) {
	disposition := corecomponent.ReviewDefaultDisposition(status)
	if disposition == "" {
		disposition = corecomponent.StateToDisposition(corecomponent.ReviewDefaultState(status))
	}
	resolved, err := corecomponent.ResolveAllowedDisposition(status.Definition.AllowedDispositions, disposition)
	if err != nil {
		return "", "", err
	}
	return resolved, corecomponent.DispositionToState(resolved), nil
}

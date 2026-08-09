package cmd

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	createpkg "github.com/libops/sitectl-isle/pkg/create"
	corecomponent "github.com/libops/sitectl/pkg/component"
	"github.com/libops/sitectl/pkg/config"
	"github.com/libops/sitectl/pkg/helpers"
	"github.com/libops/sitectl/pkg/plugin"
	coredevmode "github.com/libops/sitectl/pkg/services/devmode"
	coretraefik "github.com/libops/sitectl/pkg/services/traefik"
	"github.com/spf13/cobra"
	yaml "gopkg.in/yaml.v3"
)

var (
	createPath               string
	createDrupalRootfs       string
	createTemplateRepo       string
	createTemplateBranch     string
	createSetDefaultContext  bool
	createSetupOnly          bool
	createInput              = config.GetInput
	createEnsureLocalContext = ensureCreateContext
	createPrepareTarget      = func(runCtx context.Context, req plugin.ComposeCreateRequest, ctx *config.Context) (plugin.ComposeCreateTargetObservation, error) {
		return commandSDK.PrepareComposeCreateTargetContext(runCtx, req, ctx)
	}
	createRevalidateTarget = func(runCtx context.Context, req plugin.ComposeCreateRequest, ctx *config.Context, observation plugin.ComposeCreateTargetObservation) error {
		return commandSDK.RevalidateComposeCreateTargetContext(runCtx, req, ctx, observation)
	}
	createEnsureObservedCheckout = func(runCtx context.Context, out io.Writer, req plugin.ComposeCreateRequest, ctx *config.Context, observation plugin.ComposeCreateTargetObservation) (bool, error) {
		return commandSDK.EnsureObservedComposeTemplateCheckoutContext(runCtx, out, req, ctx, observation)
	}
	createEnsureJWTKeyPair = func(runCtx context.Context, ctx *config.Context) error {
		if err := runCtx.Err(); err != nil {
			return err
		}
		if err := createpkg.EnsureJWTKeyPair(ctx); err != nil {
			return err
		}
		return runCtx.Err()
	}
	createApply = func(runCtx context.Context, opts createpkg.Options) error {
		if err := runCtx.Err(); err != nil {
			return err
		}
		if err := createpkg.Apply(opts); err != nil {
			return err
		}
		return runCtx.Err()
	}
	createRunProjectCommand = defaultRunProjectCommand
	createRunComposeCommand = func(runCtx context.Context, ctx *config.Context, projectDir string, stdout, stderr io.Writer, command string) error {
		return commandSDK.RunComposeProjectCommandContext(runCtx, ctx, projectDir, stdout, stderr, command)
	}
	createRunComposeArgv = func(runCtx context.Context, ctx *config.Context, projectDir string, stdout, stderr io.Writer, argv []string) error {
		return commandSDK.RunComposeProjectArgvContext(runCtx, ctx, projectDir, nil, stdout, stderr, argv)
	}
	createAcquireProjectLock = func(runCtx context.Context, ctx *config.Context) (*config.ProjectMutationLock, error) {
		return ctx.AcquireProjectMutationLock(runCtx)
	}
	createNormalizeCheckout   = normalizeComposeProjectFilename
	createBootstrapCheckout   = bootstrapCheckoutContext
	createBootstrapForContext = bootstrapCheckoutForContext
	createRunStartup          = runStartup
	createRefreshContext      = func(runCtx context.Context, ctx *config.Context) error {
		if err := runCtx.Err(); err != nil {
			return err
		}
		if err := refreshCreateContextComposeMetadata(ctx); err != nil {
			return err
		}
		return runCtx.Err()
	}
	createCheckPrereqs     = checkPrereqs
	createLookPath         = exec.LookPath
	createRunCheckCommand  = runCheckCommand
	createSleep            = time.Sleep
	createComponentBindErr error
	createResolveRequest   = resolveCreateRequest
)

const (
	defaultTemplateRepo                   = "https://github.com/libops/isle"
	defaultTemplateBranch                 = "v1.3.1"
	maxExistingISLEComposeConfigJSONBytes = 16 << 20
	maxExistingISLETemplateContractBytes  = 1 << 20
	existingISLETemplateContractPath      = ".libops/template-contract.yaml"
	existingISLEComponentRevision         = "v1.0.0"
)

var existingISLELifecycleFiles = []string{
	"conf/triplet/config.yaml",
	"scripts/drupal-media-storage-state.php",
	"scripts/drupal-wait-installed.sh",
	"scripts/ensure-islandora-jwt-keypair.sh",
	"scripts/initialize-compose.sh",
	"scripts/sitectl-prepare-build.sh",
	"scripts/sitectl-prepare-init.sh",
	"scripts/sitectl-rollout-preflight.sh",
}

var existingISLERequiredReadOnlyBinds = []existingISLERequiredReadOnlyBind{
	{
		Service: "drupal",
		Source:  "scripts/drupal-media-storage-state.php",
		Target:  "/var/www/drupal/drupal-media-storage-state.php",
	},
	{
		Service: "drupal",
		Source:  "scripts/drupal-wait-installed.sh",
		Target:  "/usr/local/lib/sitectl/drupal-wait-installed.sh",
	},
	{
		Service: "init",
		Source:  "compose.yaml",
		Target:  "/work/compose.yaml",
	},
	{
		Service: "init",
		Source:  "scripts/ensure-islandora-jwt-keypair.sh",
		Target:  "/usr/local/lib/sitectl/ensure-islandora-jwt-keypair.sh",
	},
	{
		Service: "init",
		Source:  "scripts/initialize-compose.sh",
		Target:  "/usr/local/lib/sitectl/initialize-compose.sh",
	},
}

type createRequest struct {
	plugin.ComposeCreateRequest
	Apply             createpkg.Options
	IngressState      corecomponent.State
	IngressMode       string
	IngressDomain     string
	IngressACMEEmail  string
	IngressTrustedIPs string
	MaxUploadSize     string
	UploadTimeout     string
	DevModeState      corecomponent.State
}

type createRunner struct{}

func (createRunner) BindFlags(cmd *cobra.Command) {
	bindCreateFlags(cmd)
}

func (createRunner) Run(cmd *cobra.Command) error {
	progress := createProgressOutput(cmd)
	printIslandoraIntro(cmd, progress)
	req, err := createResolveRequest(cmd)
	if err != nil {
		return err
	}
	if err := validateISLECreateTarget(req); err != nil {
		return err
	}
	if err := createCheckPrereqs(progress); err != nil {
		return err
	}
	return runCreateCommand(cmd, req)
}

func validateISLECreateTarget(req createRequest) error {
	if req.TargetType != config.ContextRemote {
		return nil
	}
	return fmt.Errorf("remote ISLE create is not supported: manage the remote site lifecycle through LibOps provisioning and Cloud Compose until sitectl core provides fenced remote customization hooks; use a local create target for developer-managed checkouts")
}

func createDefinition() plugin.CreateSpec {
	return plugin.CreateSpec{
		Name:                "default",
		Description:         "Create a new ISLE site from the LibOps ISLE template",
		Default:             true,
		MinCPUCores:         4,
		MinMemory:           "8 GiB",
		MinDiskSpace:        "30 GiB",
		DockerComposeRepo:   defaultTemplateRepo,
		DockerComposeBranch: defaultTemplateBranch,
		DockerComposeBuild: []string{
			"bash scripts/sitectl-prepare-build.sh",
			"docker compose pull --ignore-buildable --ignore-pull-failures",
			"docker compose build",
		},
		Images: []plugin.ComposeImageSpec{
			{Service: "drupal", Image: createpkg.DefaultDrupalBaseImageRef, BuildPolicy: plugin.BuildPolicyAlways},
		},
		DockerComposeInit: []string{
			"bash scripts/sitectl-prepare-init.sh",
			`docker compose run --rm -e HOST_UID="$(id -u)" -e HOST_GID="$(id -g)" init`,
			"bash scripts/sitectl-rollout-preflight.sh",
		},
		InitArtifacts: []plugin.InitArtifact{
			{Path: "certs/cert.pem"},
			{Path: "certs/privkey.pem"},
			{Path: "certs/rootCA.pem"},
			{Path: "certs/rootCA-key.pem"},
			{Path: "certs/UID", ValueFrom: plugin.InitArtifactValueFromHostUID},
			{Path: "secrets/ACTIVEMQ_PASSWORD"},
			{Path: "secrets/ACTIVEMQ_WEB_ADMIN_PASSWORD"},
			{Path: "secrets/DB_ROOT_PASSWORD"},
			{Path: "secrets/DRUPAL_DEFAULT_ACCOUNT_PASSWORD"},
			{Path: "secrets/DRUPAL_DEFAULT_DB_PASSWORD"},
			{Path: "secrets/DRUPAL_DEFAULT_SALT"},
			{Path: "secrets/FCREPO_DB_PASSWORD"},
			{Path: "secrets/JWT_ADMIN_TOKEN"},
			{Path: "secrets/JWT_PUBLIC_KEY"},
			{Path: "secrets/JWT_PRIVATE_KEY"},
			{Path: "secrets/TOMCAT_ADMIN_PASSWORD"},
		},
		DockerComposeUp:   []string{"docker compose up --remove-orphans --wait --wait-timeout 600 -d"},
		DockerComposeDown: []string{"docker compose down"},
		DockerComposeRollout: []string{
			"docker compose pull --ignore-buildable --quiet || docker compose pull --ignore-buildable",
			"docker compose build --pull",
			"bash scripts/sitectl-rollout-preflight.sh",
			rolloutComposeConfigCommand,
			rolloutMountedWaitProbeCommand,
			"docker compose up --remove-orphans --pull missing --quiet-pull --force-recreate -d drupal",
			"docker compose exec -T drupal sh /usr/local/lib/sitectl/drupal-wait-installed.sh",
			"docker compose exec -T --workdir /var/www/drupal drupal drush updb -y",
			"docker compose exec -T --workdir /var/www/drupal drupal drush cr",
			"docker compose up --remove-orphans --wait --wait-timeout 600 --pull missing --quiet-pull -d",
		},
	}
}

func bindCreateFlags(cmd *cobra.Command) {
	if err := commandSDK.BindComposeCreateFlags(cmd, createDefinition(), &createDrupalRootfs, createpkg.DefaultDrupalRootfs); err != nil {
		createComponentBindErr = err
		bindLocalCreateComponentFlags(cmd)
		return
	}
	createComponentBindErr = nil
}

func bindLocalCreateComponentFlags(cmd *cobra.Command) {
	if cmd == nil || commandSDK == nil {
		return
	}

	localDefs := commandSDK.LocalComponentDefinitions()
	options := make([]corecomponent.CreateOption, 0, len(localDefs))
	for _, def := range localDefs {
		if def.Name == "" || cmd.Flags().Lookup(def.Name) != nil {
			continue
		}
		options = append(options, def.CreateOption())
	}
	corecomponent.AddCreateFlags(cmd, options...)
	if cmd.Flags().Lookup("drupal-rootfs") == nil {
		corecomponent.AddDrupalRootfsFlag(cmd, &createDrupalRootfs, createpkg.DefaultDrupalRootfs)
	}
}

func resolveCreateRequest(cmd *cobra.Command) (createRequest, error) {
	if createComponentBindErr != nil {
		if _, err := commandSDK.CreateComponentDefinitions(); err != nil {
			return createRequest{}, fmt.Errorf("load create component definitions: %w", err)
		}
		createComponentBindErr = nil
	}
	resolved, err := commandSDK.ResolveComposeCreateRequest(cmd, createInput, "isle", createDrupalRootfs, "", defaultTemplateRepo, defaultTemplateBranch)
	if err != nil {
		return createRequest{}, err
	}
	opts := createpkg.Options{
		Path:           resolved.Path,
		DrupalRootfs:   helpers.FirstNonEmpty(resolved.DrupalRootfs, createpkg.DefaultDrupalRootfs),
		ImageOverrides: maps.Clone(resolved.ImageOverrides.Images),
	}
	if decision, ok := resolved.Decisions["fcrepo"]; ok {
		opts.Fcrepo = string(decision.State)
		opts.ISLEFileSystemURI = strings.TrimSpace(decision.Options["isle-file-system-uri"])
	}
	if decision, ok := resolved.Decisions["blazegraph"]; ok {
		opts.Blazegraph = string(decision.State)
	}
	if decision, ok := resolved.Decisions["iiif"]; ok {
		opts.IIIF = createIIIFValue(decision.Disposition)
	}
	if decision, ok := resolved.Decisions["iiif-topology"]; ok {
		opts.IIIFTopology = createIIIFTopologyValue(decision.Disposition)
		opts.IIIFUpstreamURL = strings.TrimSpace(decision.Options["upstream-url"])
	}
	if decision, ok := resolved.Decisions["bot-mitigation"]; ok {
		opts.BotMitigation = string(decision.State)
	}
	var ingressState corecomponent.State
	var ingressMode string
	var ingressDomain string
	var ingressACMEEmail string
	var ingressTrustedIPs string
	var maxUploadSize string
	var uploadTimeout string
	if decision, ok := resolved.Decisions[coretraefik.IngressName]; ok {
		ingressState = decision.State
		ingressMode = strings.TrimSpace(decision.Options["mode"])
		ingressDomain = strings.TrimSpace(decision.Options["domain"])
		ingressACMEEmail = strings.TrimSpace(decision.Options["acme-email"])
		ingressTrustedIPs = strings.TrimSpace(decision.Options["trusted-ip"])
		maxUploadSize = strings.TrimSpace(decision.Options["max-upload-size"])
		uploadTimeout = strings.TrimSpace(decision.Options["upload-timeout"])
	}
	var devModeState corecomponent.State
	if decision, ok := resolved.Decisions[coredevmode.Name]; ok {
		devModeState = decision.State
	}
	if decision, ok := resolved.Decisions["codebase"]; ok {
		opts.Codebase = createCodebaseValue(decision.Disposition)
	}
	for _, name := range createpkg.DerivativeServiceNames() {
		decision, ok := resolved.Decisions[name]
		if !ok || !cmd.Flags().Changed(name) {
			continue
		}
		if opts.DerivativeServices == nil {
			opts.DerivativeServices = map[string]string{}
		}
		opts.DerivativeServices[name] = createDerivativeTopologyValue(decision.Disposition)
	}
	for _, name := range createpkg.FeatureBundleNames() {
		decision, ok := resolved.Decisions[name]
		if !ok {
			continue
		}
		if opts.FeatureBundles == nil {
			opts.FeatureBundles = map[string]string{}
			opts.FeatureBundleOptions = map[string]map[string]string{}
		}
		opts.FeatureBundles[name] = string(decision.State)
		opts.FeatureBundleOptions[name] = maps.Clone(decision.Options)
	}
	if opts.ISLEFileSystemURI == "" {
		opts.ISLEFileSystemURI = createpkg.DefaultISLEFileSystemURI
	}
	if opts.Codebase == createpkg.CodebaseGitRoot {
		resolved.DrupalRootfs = corecomponent.DefaultDrupalRootfs
		opts.DrupalRootfs = corecomponent.DefaultDrupalRootfs
	}
	return createRequest{
		ComposeCreateRequest: resolved,
		Apply:                opts,
		IngressState:         ingressState,
		IngressMode:          ingressMode,
		IngressDomain:        ingressDomain,
		IngressACMEEmail:     ingressACMEEmail,
		IngressTrustedIPs:    ingressTrustedIPs,
		MaxUploadSize:        maxUploadSize,
		UploadTimeout:        uploadTimeout,
		DevModeState:         devModeState,
	}, nil
}

func createIIIFValue(disposition corecomponent.Disposition) string {
	if disposition == corecomponent.DispositionTriplet {
		return createpkg.IIIFTriplet
	}
	return createpkg.IIIFCantaloupe
}

func createIIIFTopologyValue(disposition corecomponent.Disposition) string {
	if disposition == corecomponent.DispositionDistributed {
		return createpkg.IIIFTopologyExternal
	}
	return createpkg.IIIFTopologyLocal
}

func createDerivativeTopologyValue(disposition corecomponent.Disposition) string {
	if disposition == corecomponent.DispositionDistributed {
		return createpkg.DerivativeTopologyDistributed
	}
	return createpkg.DerivativeTopologyLocal
}

func createCodebaseValue(disposition corecomponent.Disposition) string {
	if disposition == corecomponent.DispositionGitRoot {
		return createpkg.CodebaseGitRoot
	}
	return createpkg.CodebaseNested
}

func runCreateCommand(cmd *cobra.Command, req createRequest) (returnErr error) {
	if err := validateISLECreateTarget(req); err != nil {
		return err
	}
	if commandSDK == nil {
		return fmt.Errorf("plugin sdk is not initialized")
	}

	progress := createProgressOutput(cmd)
	summary := cmd.OutOrStdout()
	fmt.Fprintln(progress, corecomponent.RenderSection("Context", "Preparing the sitectl context for this ISLE checkout."))
	ctx, err := createEnsureLocalContext(commandSDK, req)
	if err != nil {
		return err
	}
	fmt.Fprintln(progress)
	req.ContextName = ctx.Name
	req.Path = ctx.ProjectDir
	req.Apply.Path = ctx.ProjectDir
	req.Apply.EnvFiles = append([]string{}, ctx.EnvFile...)
	existingCheckout := plugin.CheckoutSource(strings.TrimSpace(string(req.CheckoutSource))) == plugin.CheckoutSourceExisting
	observation, err := createPrepareTarget(cmd.Context(), req.ComposeCreateRequest, ctx)
	if err != nil {
		return err
	}
	if existingCheckout {
		if err := validateExistingISLECheckout(cmd.Context(), ctx); err != nil {
			return err
		}
	}
	lock, err := createAcquireProjectLock(cmd.Context(), ctx)
	if err != nil {
		return fmt.Errorf("acquire project mutation lock: %w", err)
	}
	originalContext := cmd.Context()
	cmd.SetContext(lock.Context())
	defer func() {
		cmd.SetContext(originalContext)
		if releaseErr := lock.Release(); releaseErr != nil {
			returnErr = errors.Join(returnErr, fmt.Errorf("release project mutation lock: %w", releaseErr))
		}
	}()
	lockedContext := lock.Context()
	if err := createRevalidateTarget(lockedContext, req.ComposeCreateRequest, ctx, observation); err != nil {
		return err
	}
	if existingCheckout {
		if err := validateExistingISLECheckout(lockedContext, ctx); err != nil {
			return err
		}
	}
	fmt.Fprintln(progress, corecomponent.RenderSection("Template checkout", "Validating and preparing the requested checkout."))
	fmt.Fprintln(progress)
	if _, err := createEnsureObservedCheckout(lockedContext, progress, req.ComposeCreateRequest, ctx, observation); err != nil {
		return err
	}
	// A prior template-owned create may have stopped after cloning but before
	// filename normalization or the pristine commit. Existing checkouts are
	// accepted only when they already implement the canonical template contract,
	// so sitectl must not rewrite or rename them during admission.
	if !existingCheckout {
		if err := createNormalizeCheckout(lockedContext, ctx); err != nil {
			return err
		}
		if err := createBootstrapForContext(lockedContext, progress, ctx); err != nil {
			return err
		}
	}
	fmt.Fprintln(progress)
	fmt.Fprintln(progress, corecomponent.RenderSection("Template configuration", "Applying requested ISLE component and topology choices."))
	fmt.Fprintln(progress)
	if ctx.DockerHostType == config.ContextRemote {
		fmt.Fprintln(progress, "Warning: remote ISLE create leaves template-level Drupal/codebase rewrites to version-controlled local changes.")
	} else {
		if err := runWithSpinner(progress, "Applying ISLE options", func() error {
			return createApply(lockedContext, req.Apply)
		}); err != nil {
			printCreateFailureSummary(summary, req)
			return err
		}
	}
	if err := createEnsureJWTKeyPair(lockedContext, ctx); err != nil {
		printCreateFailureSummary(summary, req)
		return fmt.Errorf("prepare Islandora JWT keys: %w", err)
	}
	if err := applyCreateIngress(lockedContext, ctx, req); err != nil {
		printCreateFailureSummary(summary, req)
		return err
	}
	if err := applyCreateDevMode(lockedContext, ctx, req); err != nil {
		printCreateFailureSummary(summary, req)
		return err
	}
	pluginName := strings.TrimSpace(ctx.Plugin)
	if pluginName == "" {
		pluginName = "isle"
	}
	definitions := orderedComponentDefinitions()
	componentDecisions := make(map[string]corecomponent.ReviewDecision, len(definitions))
	for _, definition := range definitions {
		if decision, ok := req.Decisions[definition.Name]; ok {
			componentDecisions[definition.Name] = decision
		}
	}
	desired, err := corecomponent.DesiredStateFromDecisions(pluginName, definitions, componentDecisions)
	if err != nil {
		printCreateFailureSummary(summary, req)
		return fmt.Errorf("build component desired state: %w", err)
	}
	if err := lockedContext.Err(); err != nil {
		return err
	}
	if err := corecomponent.SaveDesiredState(ctx, desired); err != nil {
		printCreateFailureSummary(summary, req)
		return fmt.Errorf("save component desired state: %w", err)
	}
	if err := lockedContext.Err(); err != nil {
		return err
	}
	if err := createRefreshContext(lockedContext, ctx); err != nil {
		printCreateFailureSummary(summary, req)
		return err
	}
	if !req.ImageOverrides.Empty() {
		if ctx.DockerHostType == config.ContextRemote {
			fmt.Fprintln(progress, "Warning: modifying remote project files directly; commit and review these changes through version control before promoting them.")
		}
		if err := lockedContext.Err(); err != nil {
			return err
		}
		if err := plugin.ApplyComposeImageOverridesContext(ctx, req.ImageOverrides); err != nil {
			printCreateFailureSummary(summary, req)
			return err
		}
		if err := lockedContext.Err(); err != nil {
			return err
		}
		fmt.Fprintf(progress, "Wrote %s\n", plugin.ComposeImageOverrideFile)
	}
	if req.Apply.BotMitigation == coretraefik.BotMitigationStateOn {
		fmt.Fprintln(progress, botMitigationTurnstileWarning)
	}
	if !req.SetupOnly {
		if err := createRunStartup(lockedContext, progress, ctx); err != nil {
			printCreateFailureSummary(summary, req)
			return err
		}
	}

	printCreateSummary(summary, req)
	return nil
}

func normalizeComposeProjectFilename(runCtx context.Context, ctx *config.Context) error {
	if ctx == nil {
		return fmt.Errorf("context is nil")
	}
	if runCtx == nil {
		runCtx = context.Background()
	}
	if err := runCtx.Err(); err != nil {
		return err
	}
	canonical := ctx.ResolveProjectPath("compose.yaml")
	canonicalExists, err := ctx.FileExists(canonical)
	if err != nil {
		return fmt.Errorf("inspect canonical Compose file: %w", err)
	}
	if err := runCtx.Err(); err != nil {
		return err
	}
	if !canonicalExists {
		legacy := ctx.ResolveProjectPath("docker-compose.yml")
		legacyExists, err := ctx.FileExists(legacy)
		if err != nil {
			return fmt.Errorf("inspect legacy Compose file: %w", err)
		}
		if err := runCtx.Err(); err != nil {
			return err
		}
		if !legacyExists {
			return nil
		}
		if err := ctx.ValidateProjectRegularFile(ctx.ProjectDir, legacy); err != nil {
			return fmt.Errorf("validate legacy Compose file: %w", err)
		}
		if err := runCtx.Err(); err != nil {
			return err
		}
		if ctx.DockerHostType == config.ContextRemote {
			if err := createRunComposeArgv(runCtx, ctx, ctx.ProjectDir, io.Discard, io.Discard, []string{"mv", "--", "docker-compose.yml", "compose.yaml"}); err != nil {
				return fmt.Errorf("normalize Compose project filename: %w", err)
			}
		} else if err := os.Rename(legacy, canonical); err != nil {
			return fmt.Errorf("normalize Compose project filename: %w", err)
		}
	}
	return repairCanonicalComposeSelfMount(runCtx, ctx, canonical)
}

func repairCanonicalComposeSelfMount(runCtx context.Context, ctx *config.Context, canonical string) error {
	if err := runCtx.Err(); err != nil {
		return err
	}
	if err := ctx.ValidateProjectRegularFile(ctx.ProjectDir, canonical); err != nil {
		return fmt.Errorf("validate canonical Compose file: %w", err)
	}
	if err := runCtx.Err(); err != nil {
		return err
	}
	data, err := ctx.ReadFile(canonical)
	if err != nil {
		return fmt.Errorf("read canonical Compose file: %w", err)
	}
	if err := runCtx.Err(); err != nil {
		return err
	}
	repaired := bytes.ReplaceAll(data, []byte("./docker-compose.yml:/docker-compose.yml"), []byte("./compose.yaml:/docker-compose.yml"))
	if bytes.Equal(data, repaired) {
		return runCtx.Err()
	}
	if err := runCtx.Err(); err != nil {
		return err
	}
	if err := ctx.WriteProjectFile(ctx.ProjectDir, canonical, repaired); err != nil {
		return fmt.Errorf("update canonical Compose self-mount: %w", err)
	}
	return runCtx.Err()
}

func applyCreateIngress(runCtx context.Context, ctx *config.Context, req createRequest) error {
	if err := runCtx.Err(); err != nil {
		return err
	}
	if req.IngressState == "" {
		return nil
	}
	component, err := isleIngressComponent()
	if err != nil {
		return err
	}
	manager := corecomponent.NewManager(ctx)
	values := map[string]string{
		"mode":            strings.TrimSpace(req.IngressMode),
		"domain":          strings.TrimSpace(req.IngressDomain),
		"acme-email":      strings.TrimSpace(req.IngressACMEEmail),
		"trusted-ip":      strings.TrimSpace(req.IngressTrustedIPs),
		"max-upload-size": strings.TrimSpace(req.MaxUploadSize),
		"upload-timeout":  strings.TrimSpace(req.UploadTimeout),
	}
	spec := component.SpecForWithOptions(req.IngressState, values)
	switch req.IngressState {
	case corecomponent.StateOn:
		if err := manager.EnableComponentWithOptions(runCtx, spec, corecomponent.ApplyOptions{Yolo: true}); err != nil {
			return err
		}
		if err := runCtx.Err(); err != nil {
			return err
		}
		if err := applyISLEIngressFiles(ctx, values); err != nil {
			return err
		}
		return runCtx.Err()
	default:
		return fmt.Errorf("unsupported ingress state %q", req.IngressState)
	}
}

func applyCreateDevMode(runCtx context.Context, ctx *config.Context, req createRequest) error {
	if err := runCtx.Err(); err != nil {
		return err
	}
	if req.DevModeState == "" {
		return nil
	}
	component, err := isleDevModeComponent()
	if err != nil {
		return err
	}
	manager := corecomponent.NewManager(ctx)
	spec := component.SpecForWithOptions(req.DevModeState, nil)
	switch req.DevModeState {
	case corecomponent.StateOn:
		if err := manager.EnableComponentWithOptions(runCtx, spec, corecomponent.ApplyOptions{Yolo: true}); err != nil {
			return err
		}
		if err := runCtx.Err(); err != nil {
			return err
		}
		if err := applyISLEDevMode(ctx, true); err != nil {
			return err
		}
		return runCtx.Err()
	case corecomponent.StateOff:
		if err := manager.DisableComponentWithOptions(runCtx, spec, corecomponent.ApplyOptions{Yolo: true}); err != nil {
			return err
		}
		if err := runCtx.Err(); err != nil {
			return err
		}
		if err := applyISLEDevMode(ctx, false); err != nil {
			return err
		}
		return runCtx.Err()
	default:
		return fmt.Errorf("unsupported dev mode state %q", req.DevModeState)
	}
}

func createProgressOutput(cmd *cobra.Command) io.Writer {
	if cmd == nil {
		return os.Stderr
	}
	if os.Getenv("SITECTL_RPC") == "1" {
		return cmd.ErrOrStderr()
	}
	return cmd.OutOrStdout()
}

func printIslandoraIntro(cmd *cobra.Command, out io.Writer) {
	fmt.Fprintln(out, corecomponent.RenderIntroSection(
		cmd.Short,
		cmd.Long,
	))
	fmt.Fprintln(out)
}

func ensureCreateContext(sdk *plugin.SDK, req createRequest) (*config.Context, error) {
	if sdk == nil {
		return nil, fmt.Errorf("plugin sdk is not initialized")
	}
	return sdk.EnsureComposeCreateContext(req.ComposeCreateRequest, plugin.ComposeCreateContextOptions{
		DefaultName:         "isle-local",
		DefaultSite:         filepath.Base(helpers.FirstNonEmpty(req.Path, "isle")),
		DefaultPlugin:       "isle",
		DefaultProjectDir:   req.Path,
		DefaultProjectName:  filepath.Base(helpers.FirstNonEmpty(req.Path, "isle")),
		DefaultEnvironment:  "local",
		DefaultDrupalRootfs: req.DrupalRootfs,
		DrupalContainerRoot: "/var/www/drupal",
		Input:               createInput,
	})
}

func refreshCreateContextComposeMetadata(ctx *config.Context) error {
	if ctx == nil {
		return nil
	}
	projectDir := strings.TrimSpace(ctx.ProjectDir)
	if projectDir == "" {
		return nil
	}
	composeProjectName := config.DetectContextComposeProjectName(ctx)
	if strings.TrimSpace(composeProjectName) == "" || composeProjectName == ctx.ComposeProjectName {
		return nil
	}
	ctx.ComposeProjectName = composeProjectName
	ctx.ComposeNetwork = config.DetectContextComposeNetwork(ctx)
	return config.SaveContext(ctx, false)
}

func runStartup(runCtx context.Context, out io.Writer, ctx *config.Context) error {
	if ctx == nil {
		return fmt.Errorf("context is nil")
	}
	if runCtx == nil {
		runCtx = context.Background()
	}
	if err := runCtx.Err(); err != nil {
		return err
	}
	cleanupLabel, _, _ := cleanupCommand()
	logPath, err := startupLogPath(ctx.Name)
	if err != nil {
		return fmt.Errorf("resolve startup log path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(logPath), 0o750); err != nil {
		return fmt.Errorf("create startup log directory: %w", err)
	}

	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600) // #nosec G304 -- startup log path is generated under sitectl config state.
	if err != nil {
		return fmt.Errorf("create startup log %q: %w", logPath, err)
	}
	defer logFile.Close()

	fmt.Fprintln(out, corecomponent.RenderSection(
		"Islandora install is now running",
		fmt.Sprintf(`ISLE will now run the plugin-owned init and up commands from the checked out template.
While this runs, Docker will pull images over the network and build the Drupal container locally before Docker Compose starts the stack.

Once docker compose brings the containers up, the Islandora Drupal site will install automatically and be configured using the Islandora starter site.

Output is being written to a log file while the terminal shows progress. Expect your web browser to open when the site is ready.

If you need to cancel, press Ctrl+C, then run:

  cd %s
  %s

This will completely stop and destroy the setup.`, shellPath(ctx.ProjectDir), cleanupLabel),
	))
	fmt.Fprintln(out)

	if err := runWithSpinner(out, "Starting the Islandora stack", func() error {
		for _, commandText := range startupCommands() {
			if err := runCtx.Err(); err != nil {
				return err
			}
			commandText = strings.TrimSpace(commandText)
			if commandText == "" {
				continue
			}
			if _, err := fmt.Fprintf(logFile, "Running %s\n", commandText); err != nil {
				return err
			}
			if err := runCreateProjectShellCommand(runCtx, ctx, logFile, logFile, commandText); err != nil {
				return fmt.Errorf("run %s: %w", commandText, err)
			}
		}
		return runCtx.Err()
	}); err != nil {
		const failureLogLines = 200
		tail, tailErr := tailLines(logPath, failureLogLines)
		fmt.Fprintln(out)
		fmt.Fprintln(out, corecomponent.RenderSection(
			"Install failed",
			fmt.Sprintf("Startup logs were saved to %s. Showing the last %d lines below.", logPath, failureLogLines),
		))
		if tailErr == nil && strings.TrimSpace(tail) != "" {
			fmt.Fprintln(out, tail)
		}
		return fmt.Errorf("run ISLE startup commands: %w", err)
	}

	fmt.Fprintln(out)
	fmt.Fprintln(out, corecomponent.RenderSection(
		"Install log",
		fmt.Sprintf("Startup logs were saved to %s.", logPath),
	))
	fmt.Fprintln(out)
	return nil
}

type prereqCheck struct {
	label string
	run   func() error
}

func checkPrereqs(out io.Writer) error {
	fmt.Fprintln(out, corecomponent.RenderSection(
		"Prerequisites",
		"Checking the local tools and services needed to clone, build, and start ISLE.",
	))
	checks := []prereqCheck{
		{
			label: "git is installed",
			run: func() error {
				_, err := createLookPath("git")
				if err != nil {
					return fmt.Errorf("git is not installed or not on PATH")
				}
				return nil
			},
		},
		{
			label: "bash is installed",
			run: func() error {
				_, err := createLookPath("bash")
				if err != nil {
					return fmt.Errorf("bash is not installed or not on PATH")
				}
				return nil
			},
		},
		{
			label: "docker is installed",
			run: func() error {
				_, err := createLookPath("docker")
				if err != nil {
					return fmt.Errorf("docker is not installed or not on PATH")
				}
				return nil
			},
		},
		{
			label: "docker daemon is running",
			run: func() error {
				return createRunCheckCommand("docker", "info")
			},
		},
		{
			label: "docker compose is available",
			run: func() error {
				return createRunCheckCommand("docker", "compose", "version")
			},
		},
		{
			label: "docker buildx is available",
			run: func() error {
				return createRunCheckCommand("docker", "buildx", "version")
			},
		},
	}

	for _, check := range checks {
		if err := check.run(); err != nil {
			fmt.Fprintln(out, corecomponent.RenderChecklistItem(check.label, "failed", "fix this before continuing"))
			return fmt.Errorf("prerequisite check failed for %s: %w", check.label, err)
		}
		fmt.Fprintln(out, corecomponent.RenderChecklistItem(check.label, "ok", ""))
	}
	fmt.Fprintln(out)
	return nil
}

func validateExistingISLECheckout(runCtx context.Context, ctx *config.Context) error {
	return validateExistingISLECheckoutLimit(runCtx, ctx, maxExistingISLEComposeConfigJSONBytes)
}

type cappedComposeConfigBuffer struct {
	buffer   bytes.Buffer
	maximum  int
	exceeded bool
}

type existingISLEComposeModel struct {
	Services map[string]existingISLEComposeService `json:"services"`
}

type existingISLEComposeService struct {
	Volumes []existingISLEComposeVolume `json:"volumes"`
}

type existingISLEComposeVolume struct {
	Type     string `json:"type"`
	Source   string `json:"source"`
	Target   string `json:"target"`
	ReadOnly bool   `json:"read_only"`
}

type existingISLERequiredReadOnlyBind struct {
	Service string
	Source  string
	Target  string
}

func (b *cappedComposeConfigBuffer) Write(data []byte) (int, error) {
	originalLength := len(data)
	remaining := b.maximum - b.buffer.Len()
	if remaining > 0 {
		if len(data) > remaining {
			data = data[:remaining]
		}
		_, _ = b.buffer.Write(data)
	}
	if originalLength > remaining {
		b.exceeded = true
	}
	// Report the full write so a remote Compose process can finish and close its
	// pipes; excess bytes are deliberately discarded after the cap is reached.
	return originalLength, nil
}

func validateExistingISLECheckoutLimit(runCtx context.Context, ctx *config.Context, maximum int) error {
	if ctx == nil {
		return fmt.Errorf("context is nil")
	}
	if maximum < 1 {
		return fmt.Errorf("effective Compose model size limit must be positive")
	}
	if runCtx == nil {
		runCtx = context.Background()
	}
	if err := runCtx.Err(); err != nil {
		return err
	}
	if err := validateExistingISLETemplateContract(runCtx, ctx); err != nil {
		return err
	}
	output := &cappedComposeConfigBuffer{maximum: maximum}
	if err := createRunComposeArgv(runCtx, ctx, ctx.ProjectDir, output, io.Discard, []string{"docker", "compose", "config", "--format", "json"}); err != nil {
		return fmt.Errorf("resolve effective Compose model for existing ISLE checkout: %w", err)
	}
	if output.exceeded {
		return fmt.Errorf("effective Compose model for existing ISLE checkout exceeds %d bytes", maximum)
	}
	var model existingISLEComposeModel
	if err := json.Unmarshal(output.buffer.Bytes(), &model); err != nil {
		return fmt.Errorf("parse effective Compose model for existing ISLE checkout: %w", err)
	}
	missing := make([]string, 0, 3)
	for _, service := range []string{"drupal", "alpaca", "init"} {
		if _, ok := model.Services[service]; !ok {
			missing = append(missing, service)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("project directory %q is not an existing ISLE checkout: effective Compose services %s are required", ctx.ProjectDir, strings.Join(missing, ", "))
	}
	if err := validateExistingISLELifecycleBinds(ctx, model); err != nil {
		return err
	}
	return runCtx.Err()
}

func validateExistingISLELifecycleBinds(ctx *config.Context, model existingISLEComposeModel) error {
	for _, required := range existingISLERequiredReadOnlyBinds {
		expectedSource := filepath.Clean(ctx.ResolveProjectPath(required.Source))
		valid := false
		for _, volume := range model.Services[required.Service].Volumes {
			if volume.Target != required.Target || volume.Type != "bind" || !volume.ReadOnly {
				continue
			}
			actualSource := volume.Source
			if !filepath.IsAbs(actualSource) {
				actualSource = filepath.Join(ctx.ProjectDir, actualSource)
			}
			if filepath.Clean(actualSource) == expectedSource {
				valid = true
				break
			}
		}
		if !valid {
			return fmt.Errorf("existing ISLE checkout service %q must bind lifecycle file %q to %q read-only; migrate the checkout to the current ISLE template v1.3.1 lifecycle contract before retrying", required.Service, required.Source, required.Target)
		}
	}
	return nil
}

type existingISLETemplateContract struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Schema     int    `yaml:"schema"`
	Spec       struct {
		ComponentDefaults struct {
			Revision string `yaml:"revision"`
		} `yaml:"componentDefaults"`
	} `yaml:"spec"`
}

func validateExistingISLETemplateContract(runCtx context.Context, ctx *config.Context) error {
	if err := runCtx.Err(); err != nil {
		return err
	}
	canonical := ctx.ResolveProjectPath("compose.yaml")
	canonicalExists, err := ctx.FileExists(canonical)
	if err != nil {
		return fmt.Errorf("inspect canonical Compose file: %w", err)
	}
	if !canonicalExists {
		for _, legacyName := range []string{"docker-compose.yml", "docker-compose.yaml", "compose.yml"} {
			legacyExists, legacyErr := ctx.FileExists(ctx.ResolveProjectPath(legacyName))
			if legacyErr != nil {
				return fmt.Errorf("inspect legacy Compose file %q: %w", legacyName, legacyErr)
			}
			if legacyExists {
				return fmt.Errorf("existing ISLE checkout uses legacy Compose file %q; migrate the checkout to the canonical ISLE template v1.3.1 contract with compose.yaml before retrying (sitectl will not rename an existing checkout)", legacyName)
			}
		}
		return fmt.Errorf("existing ISLE checkout is missing canonical compose.yaml; migrate the checkout to the ISLE template v1.3.1 contract before retrying")
	}
	if err := ctx.ValidateProjectRegularFile(ctx.ProjectDir, canonical); err != nil {
		return fmt.Errorf("validate canonical compose.yaml: %w", err)
	}
	for _, legacyName := range []string{"docker-compose.yml", "docker-compose.yaml", "compose.yml"} {
		legacyExists, legacyErr := ctx.FileExists(ctx.ResolveProjectPath(legacyName))
		if legacyErr != nil {
			return fmt.Errorf("inspect legacy Compose file %q: %w", legacyName, legacyErr)
		}
		if legacyExists {
			return fmt.Errorf("existing ISLE checkout contains legacy Compose file %q alongside compose.yaml; migrate the checkout fully to the ISLE template v1.3.1 contract before retrying", legacyName)
		}
	}
	requiredFiles := append([]string{existingISLETemplateContractPath}, existingISLELifecycleFiles...)
	for _, relativePath := range requiredFiles {
		if err := runCtx.Err(); err != nil {
			return err
		}
		filename := ctx.ResolveProjectPath(relativePath)
		exists, existsErr := ctx.FileExists(filename)
		if existsErr != nil {
			return fmt.Errorf("inspect required ISLE template file %q: %w", relativePath, existsErr)
		}
		if !exists {
			return fmt.Errorf("existing ISLE checkout is missing required template v1.3.1 file %q; migrate the checkout to the current ISLE lifecycle contract before retrying", relativePath)
		}
		if err := ctx.ValidateProjectRegularFile(ctx.ProjectDir, filename); err != nil {
			return fmt.Errorf("validate required ISLE template file %q: %w", relativePath, err)
		}
	}
	contractData, err := ctx.ReadFile(ctx.ResolveProjectPath(existingISLETemplateContractPath))
	if err != nil {
		return fmt.Errorf("read %s: %w", existingISLETemplateContractPath, err)
	}
	if len(contractData) > maxExistingISLETemplateContractBytes {
		return fmt.Errorf("existing ISLE template contract exceeds %d bytes", maxExistingISLETemplateContractBytes)
	}
	decoder := yaml.NewDecoder(bytes.NewReader(contractData))
	decoder.KnownFields(true)
	var contract existingISLETemplateContract
	if err := decoder.Decode(&contract); err != nil {
		return fmt.Errorf("parse %s: %w", existingISLETemplateContractPath, err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("parse %s: multiple YAML documents are not allowed", existingISLETemplateContractPath)
		}
		return fmt.Errorf("parse %s: %w", existingISLETemplateContractPath, err)
	}
	if contract.APIVersion != "sitectl.libops.io/v1alpha1" || contract.Kind != "TemplateContract" || contract.Schema != 1 || strings.TrimSpace(contract.Spec.ComponentDefaults.Revision) != existingISLEComponentRevision {
		return fmt.Errorf("existing ISLE checkout has an unsupported %s; migrate it to the ISLE template v1.3.1 contract before retrying", existingISLETemplateContractPath)
	}
	return runCtx.Err()
}

func bootstrapCheckoutForContext(runCtx context.Context, out io.Writer, ctx *config.Context) error {
	if ctx == nil {
		return fmt.Errorf("context is nil")
	}
	if runCtx == nil {
		runCtx = context.Background()
	}
	if err := runCtx.Err(); err != nil {
		return err
	}
	if ctx.DockerHostType != config.ContextRemote {
		return createBootstrapCheckout(runCtx, out, ctx.ProjectDir)
	}
	projectDir := strings.TrimSpace(ctx.ProjectDir)
	hasCommit, err := checkoutHasCommit(projectDir, func(stdout, stderr io.Writer, name string, args ...string) error {
		return createRunComposeArgv(runCtx, ctx, projectDir, stdout, stderr, append([]string{name}, args...))
	})
	if err != nil {
		return err
	}
	if err := runCtx.Err(); err != nil {
		return err
	}
	if hasCommit {
		return nil
	}
	fmt.Fprintln(out)
	fmt.Fprintln(out, corecomponent.RenderSection(
		"Git bootstrap",
		fmt.Sprintf("Recording the pristine template checkout in %s before applying any sitectl-isle changes.", projectDir),
	))
	fmt.Fprintln(out)
	return runWithSpinner(out, "Creating initial git commit", func() error {
		safeDirectory := "safe.directory=" + projectDir
		if err := createRunComposeArgv(runCtx, ctx, projectDir, io.Discard, io.Discard, []string{"git", "-c", safeDirectory, "add", "."}); err != nil {
			return fmt.Errorf("stage initial checkout: %w", err)
		}
		if err := runCtx.Err(); err != nil {
			return err
		}
		if err := createRunComposeArgv(
			runCtx,
			ctx,
			projectDir,
			io.Discard,
			io.Discard,
			[]string{
				"git",
				"-c", safeDirectory,
				"-c", "user.name=sitectl-isle",
				"-c", "user.email=sitectl-isle@localhost",
				"commit",
				"-m", "initial commit.",
			},
		); err != nil {
			return fmt.Errorf("create initial commit: %w", err)
		}
		return runCtx.Err()
	})
}

func runCreateProjectShellCommand(runCtx context.Context, ctx *config.Context, stdout, stderr io.Writer, commandText string) error {
	if ctx == nil {
		return fmt.Errorf("context is nil")
	}
	if err := runCtx.Err(); err != nil {
		return err
	}
	// The sitectl SDK owns local and remote shell transport. Its only
	// context-aware project runner currently accepts shell text, invokes
	// `bash -lc` locally, and has no argv variant. It also applies the local
	// Compose port environment only to `docker compose up` operations.
	return createRunComposeCommand(runCtx, ctx, ctx.ProjectDir, stdout, stderr, commandText)
}

func bootstrapCheckout(out io.Writer, projectDir string) error {
	return bootstrapCheckoutContext(context.Background(), out, projectDir)
}

func bootstrapCheckoutContext(runCtx context.Context, out io.Writer, projectDir string) error {
	if runCtx == nil {
		runCtx = context.Background()
	}
	if err := runCtx.Err(); err != nil {
		return err
	}
	hasCommit, err := checkoutHasCommit(projectDir, func(stdout, stderr io.Writer, name string, args ...string) error {
		return createRunProjectCommand(runCtx, projectDir, stdout, stderr, name, args...)
	})
	if err != nil {
		return err
	}
	if err := runCtx.Err(); err != nil {
		return err
	}
	if hasCommit {
		return nil
	}
	fmt.Fprintln(out)
	fmt.Fprintln(out, corecomponent.RenderSection(
		"Git bootstrap",
		fmt.Sprintf("Recording the pristine template checkout in %s before applying any sitectl-isle changes.", projectDir),
	))
	fmt.Fprintln(out)

	return runWithSpinner(out, "Creating initial git commit", func() error {
		safeDirectory := "safe.directory=" + projectDir
		if err := createRunProjectCommand(runCtx, projectDir, io.Discard, io.Discard, "git", "-c", safeDirectory, "add", "."); err != nil {
			return fmt.Errorf("stage initial checkout: %w", err)
		}
		if err := runCtx.Err(); err != nil {
			return err
		}
		if err := createRunProjectCommand(
			runCtx,
			projectDir,
			io.Discard,
			io.Discard,
			"git",
			"-c", safeDirectory,
			"-c", "user.name=sitectl-isle",
			"-c", "user.email=sitectl-isle@localhost",
			"commit",
			"-m", "initial commit.",
		); err != nil {
			return fmt.Errorf("create initial commit: %w", err)
		}
		return runCtx.Err()
	})
}

type checkoutCommandRunner func(stdout, stderr io.Writer, name string, args ...string) error

func checkoutHasCommit(projectDir string, run checkoutCommandRunner) (bool, error) {
	projectDir = strings.TrimSpace(projectDir)
	if projectDir == "" {
		return false, fmt.Errorf("project directory cannot be empty")
	}
	if run == nil {
		return false, fmt.Errorf("checkout command runner is nil")
	}
	safeDirectory := "safe.directory=" + projectDir
	var commit bytes.Buffer
	verifyErr := run(&commit, io.Discard, "git", "-c", safeDirectory, "rev-parse", "--verify", "HEAD")
	if verifyErr == nil {
		if strings.TrimSpace(commit.String()) == "" {
			return false, fmt.Errorf("inspect checkout commit: git returned an empty HEAD")
		}
		return true, nil
	}
	var repository bytes.Buffer
	if err := run(&repository, io.Discard, "git", "-c", safeDirectory, "rev-parse", "--is-inside-work-tree"); err != nil {
		return false, fmt.Errorf("inspect checkout commit: %w", errors.Join(verifyErr, err))
	}
	if strings.TrimSpace(repository.String()) != "true" {
		return false, fmt.Errorf("project directory %q is not a Git work tree", projectDir)
	}
	return false, nil
}

func defaultRunProjectCommand(runCtx context.Context, projectDir string, stdout, stderr io.Writer, name string, args ...string) error {
	command := exec.CommandContext(runCtx, name, args...) // #nosec G204 -- command helper is used for fixed git/template commands assembled by sitectl-isle.
	command.Dir = projectDir
	command.Stdout = stdout
	command.Stderr = stderr
	command.Env = os.Environ()
	if os.Getenv("TERM") == "" {
		command.Env = append(command.Env, "TERM=dumb")
	}
	return command.Run()
}

func runCheckCommand(name string, args ...string) error {
	command := exec.Command(name, args...) // #nosec G204 -- command helper checks fixed local prerequisites without a shell.
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	command.Env = os.Environ()
	return command.Run()
}

func startupCommands() []string {
	spec := createDefinition()
	commands := append([]string{}, spec.DockerComposeInit...)
	commands = append(commands, spec.DockerComposeBuild...)
	commands = append(commands, spec.DockerComposeUp...)
	return commands
}

func cleanupCommand() (string, string, []string) {
	if _, err := createLookPath("make"); err == nil {
		return "make clean", "make", []string{"clean"}
	}
	return "bash ./scripts/clean.sh", "bash", []string{"./scripts/clean.sh"}
}

func startupLogPath(contextName string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".sitectl", "isle", contextName+"-install.log"), nil
}

func runWithSpinner(out io.Writer, label string, fn func() error) error {
	done := make(chan error, 1)
	started := time.Now()
	go func() {
		done <- fn()
	}()

	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	ticker := time.NewTicker(180 * time.Millisecond)
	defer ticker.Stop()
	minVisible := 1500 * time.Millisecond

	index := 0
	for {
		select {
		case err := <-done:
			if remaining := minVisible - time.Since(started); remaining > 0 {
				createSleep(remaining)
			}
			if err != nil {
				fmt.Fprintf(out, "\r%s... failed\n", label)
				return err
			}
			fmt.Fprintf(out, "\r%s... done\n", label)
			return nil
		case <-ticker.C:
			fmt.Fprintf(out, "\r%s... %s", label, frames[index%len(frames)])
			index++
		}
	}
}

func tailLines(path string, limit int) (string, error) {
	file, err := os.Open(path) // #nosec G304 -- path is a sitectl-generated startup log path.
	if err != nil {
		return "", err
	}
	defer file.Close()

	lines := []string{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
		if len(lines) > limit {
			lines = lines[1:]
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return strings.Join(lines, "\n"), nil
}

func printCreateSummary(out io.Writer, req createRequest) {
	fmt.Fprintln(out)
	fmt.Fprintln(out, corecomponent.RenderSection("Create complete", "ISLE is ready locally. Review the generated changes, set your Git remote, and commit the checkout when you are ready."))
	fmt.Fprintln(out)
	if req.SetupOnly {
		fmt.Fprintf(out, "Checkout: %s\n", req.Path)
		fmt.Fprintf(out, "Context:  %s\n", req.ContextName)
		fmt.Fprintln(out, "The site was prepared and left stopped because --setup-only was used.")
	} else {
		fmt.Fprintf(out, "Checkout: %s\n", req.Path)
		fmt.Fprintf(out, "Context:  %s\n", req.ContextName)
		fmt.Fprintln(out, "The site was prepared and started locally.")
	}
	fmt.Fprintln(out)
	fmt.Fprintln(out, corecomponent.RenderSection("Next steps", "Set your Git remote, review the generated changes, and commit the checkout."))
	fmt.Fprintln(out)
	fmt.Fprintln(out, corecomponent.RenderCommandBlock(buildCommitSuggestion(req)))
	fmt.Fprintln(out)
}

func printCreateFailureSummary(out io.Writer, req createRequest) {
	fmt.Fprintln(out)
	fmt.Fprintln(out, corecomponent.RenderSection(
		"Retry",
		"Fix the issue above, then rerun the same create command below to continue with this checkout.",
	))
	fmt.Fprintln(out)
	fmt.Fprintf(out, "Checkout: %s\n", req.Path)
	fmt.Fprintf(out, "Context:  %s\n", req.ContextName)
	fmt.Fprintln(out)
	fmt.Fprintln(out, buildRecreateCommand(req))
	fmt.Fprintln(out)
}

func buildCommitSuggestion(req createRequest) string {
	return fmt.Sprintf(`cd %s
git remote add origin git@github.com:your-org/your-repo.git
git add .
%s`,
		shellPath(req.Path),
		buildCommitCommand(req),
	)
}

func buildCommitCommand(req createRequest) string {
	return fmt.Sprintf(
		`git commit -m %s -m %s`,
		strconv.Quote("Local setup of ISLE site template"),
		shellSingleQuote(buildRecreateCommand(req)),
	)
}

func buildRecreateCommand(req createRequest) string {
	args := []string{
		`--type=` + shellDoubleQuote(recreateTargetType(req)),
		`--checkout-source=` + shellDoubleQuote(recreateCheckoutSource(req)),
		`--context=` + shellDoubleQuote(req.ContextName),
		`--path=` + shellDoubleQuote(req.Path),
		`--template-repo=` + shellDoubleQuote(req.TemplateRepo),
		`--template-branch=` + shellDoubleQuote(req.TemplateBranch),
		`--drupal-rootfs=` + shellDoubleQuote(req.DrupalRootfs),
		`--fcrepo=` + req.Apply.Fcrepo,
		`--blazegraph=` + req.Apply.Blazegraph,
		`--iiif=` + iiifDispositionFlagValue(req.Apply.IIIF),
		`--iiif-topology=` + iiifTopologyDispositionFlagValue(req.Apply.IIIFTopology),
		`--codebase=` + codebaseDispositionFlagValue(req.Apply.Codebase),
		`--ingress=` + string(corecomponent.StateToDisposition(req.IngressState)),
		`--dev-mode=` + string(corecomponent.StateToDisposition(req.DevModeState)),
		`--bot-mitigation=` + req.Apply.BotMitigation,
		`--isle-file-system-uri=` + shellDoubleQuote(req.Apply.ISLEFileSystemURI),
	}
	if req.Apply.IIIFTopology == createpkg.IIIFTopologyExternal {
		args = append(args, `--iiif-upstream-url=`+shellDoubleQuote(req.Apply.IIIFUpstreamURL))
	}
	if req.IngressState == corecomponent.StateOn {
		args = append(args,
			`--mode=`+shellDoubleQuote(helpers.FirstNonEmpty(req.IngressMode, coretraefik.IngressModeHTTP)),
			`--domain=`+shellDoubleQuote(helpers.FirstNonEmpty(req.IngressDomain, coretraefik.DefaultIngressDomain)),
		)
		if strings.TrimSpace(req.IngressACMEEmail) != "" {
			args = append(args, `--acme-email=`+shellDoubleQuote(req.IngressACMEEmail))
		}
		for _, trustedIP := range corecomponent.SplitFollowUpValues(req.IngressTrustedIPs) {
			args = append(args, `--trusted-ip=`+shellDoubleQuote(trustedIP))
		}
		args = append(args,
			`--max-upload-size=`+shellDoubleQuote(uploadLimitValue(map[string]string{"max-upload-size": req.MaxUploadSize}, "max-upload-size")),
			`--upload-timeout=`+shellDoubleQuote(uploadLimitValue(map[string]string{"upload-timeout": req.UploadTimeout}, "upload-timeout")),
		)
	}
	for _, name := range createpkg.DerivativeServiceNames() {
		topology, ok := req.Apply.DerivativeServices[name]
		if !ok {
			continue
		}
		args = append(args, `--`+name+`=`+derivativeTopologyDispositionFlagValue(topology))
	}
	for _, name := range createpkg.FeatureBundleNames() {
		state, ok := req.Apply.FeatureBundles[name]
		if !ok {
			continue
		}
		args = append(args, `--`+name+`=`+string(corecomponent.StateToDisposition(corecomponent.State(state))))
		if name == createpkg.FeatureBundleHOCRSearch && corecomponent.State(state) == corecomponent.StateOn {
			termID := strings.TrimSpace(req.Apply.FeatureBundleOptions[name][createpkg.HOCRStructuredTextTermOption])
			if termID != "" {
				args = append(args, `--hocr-term-id=`+shellDoubleQuote(termID))
			}
		}
	}
	if req.SetDefaultContext {
		args = append(args, "--default-context")
	}
	if req.SetupOnly {
		args = append(args, "--setup-only")
	}
	lines := []string{"sitectl create isle \\"}
	for i, arg := range args {
		line := "  " + arg
		if i < len(args)-1 {
			line += " \\"
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func recreateTargetType(req createRequest) string {
	if strings.TrimSpace(string(req.TargetType)) != "" {
		return string(req.TargetType)
	}
	return string(config.ContextLocal)
}

func recreateCheckoutSource(req createRequest) string {
	if strings.TrimSpace(string(req.CheckoutSource)) != "" {
		return string(req.CheckoutSource)
	}
	return string(plugin.CheckoutSourceTemplate)
}

func iiifDispositionFlagValue(value string) string {
	if value == createpkg.IIIFTriplet {
		return string(corecomponent.DispositionTriplet)
	}
	return string(corecomponent.DispositionCantaloupe)
}

func iiifTopologyDispositionFlagValue(value string) string {
	if value == createpkg.IIIFTopologyExternal {
		return string(corecomponent.DispositionDistributed)
	}
	return string(corecomponent.DispositionDisabled)
}

func derivativeTopologyDispositionFlagValue(value string) string {
	if value == createpkg.DerivativeTopologyDistributed {
		return string(corecomponent.DispositionDistributed)
	}
	return string(corecomponent.DispositionEnabled)
}

func codebaseDispositionFlagValue(value string) string {
	if value == createpkg.CodebaseGitRoot {
		return string(corecomponent.DispositionGitRoot)
	}
	return string(corecomponent.DispositionNested)
}

func shellDoubleQuote(value string) string {
	return strconv.Quote(value)
}

func shellSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

func shellPath(value string) string {
	return shellSingleQuote(value)
}

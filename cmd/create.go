package cmd

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
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
)

var (
	createPath              string
	createDrupalRootfs      string
	createTemplateRepo      string
	createTemplateBranch    string
	createSetDefaultContext bool
	createSetupOnly         bool
	createInput             = config.GetInput
	createCloneTemplateRepo = func(opts plugin.GitTemplateOptions) error {
		return commandSDK.CloneTemplateRepo(opts)
	}
	createEnsureLocalContext = ensureCreateContext
	createApply              = createpkg.Apply
	createRunProjectCommand  = defaultRunProjectCommand
	createBootstrapCheckout  = bootstrapCheckout
	createRunStartup         = runStartup
	createRewriteCommand     = rewriteCreateProjectShellCommand
	createRefreshContext     = refreshCreateContextComposeMetadata
	createPrepareStartup     = prepareCreateStartup
	createCheckPrereqs       = checkPrereqs
	createLookPath           = exec.LookPath
	createRunCheckCommand    = runCheckCommand
	createSleep              = time.Sleep
	createComponentBindErr   error
)

const (
	defaultTemplateRepo   = "https://github.com/islandora-devops/isle-site-template"
	defaultTemplateBranch = "main"
)

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
	if err := createCheckPrereqs(progress); err != nil {
		return err
	}
	req, err := resolveCreateRequest(cmd)
	if err != nil {
		return err
	}
	return runCreateCommand(cmd, req)
}

func createDefinition() plugin.CreateSpec {
	return plugin.CreateSpec{
		Name:                "default",
		Description:         "Create a new ISLE site from the Islandora site template",
		Default:             true,
		MinCPUCores:         4,
		MinMemory:           "8 GiB",
		MinDiskSpace:        "30 GiB",
		DockerComposeRepo:   defaultTemplateRepo,
		DockerComposeBranch: defaultTemplateBranch,
		DockerComposeBuild: []string{
			"if [ -d drupal/rootfs ]; then find drupal/rootfs -type d -exec chmod 755 {} \\; ; fi",
			"docker compose pull --ignore-buildable --ignore-pull-failures",
			"docker compose build",
		},
		Images: []plugin.ComposeImageSpec{
			{Service: "drupal", Image: "libops/islandora:nginx-1.30.3-php84", BuildPolicy: plugin.BuildPolicyAlways},
		},
		DockerComposeInit: []string{
			"find certs secrets -type f -empty -delete && ./scripts/init.sh",
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
			"docker compose up --remove-orphans --pull missing --quiet-pull -d drupal",
			"docker compose exec -T drupal sh -c 'attempt=0; until test -f /installed; do attempt=$((attempt + 1)); if [ \"$attempt\" -ge 150 ]; then echo \"Drupal did not become ready for database migration within 5 minutes\" >&2; exit 1; fi; sleep 2; done'",
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

func runCreateCommand(cmd *cobra.Command, req createRequest) error {
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
	cloned, err := ensureClonedCheckoutForContext(progress, ctx, req)
	if err != nil {
		return err
	}
	if cloned {
		if err := bootstrapCheckoutForContext(progress, ctx); err != nil {
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
			return createApply(req.Apply)
		}); err != nil {
			printCreateFailureSummary(summary, req)
			return err
		}
	}
	if err := applyCreateIngress(ctx, req); err != nil {
		printCreateFailureSummary(summary, req)
		return err
	}
	if err := applyCreateDevMode(ctx, req); err != nil {
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
	if err := corecomponent.SaveDesiredState(ctx, desired); err != nil {
		printCreateFailureSummary(summary, req)
		return fmt.Errorf("save component desired state: %w", err)
	}
	if err := createRefreshContext(ctx); err != nil {
		printCreateFailureSummary(summary, req)
		return err
	}
	if !req.ImageOverrides.Empty() {
		if ctx.DockerHostType == config.ContextRemote {
			fmt.Fprintln(progress, "Warning: modifying remote project files directly; commit and review these changes through version control before promoting them.")
		}
		if err := plugin.ApplyComposeImageOverridesContext(ctx, req.ImageOverrides); err != nil {
			printCreateFailureSummary(summary, req)
			return err
		}
		fmt.Fprintf(progress, "Wrote %s\n", plugin.ComposeImageOverrideFile)
	}
	if req.Apply.BotMitigation == coretraefik.BotMitigationStateOn {
		fmt.Fprintln(progress, botMitigationTurnstileWarning)
	}
	if !req.SetupOnly {
		if err := createRunStartup(progress, ctx); err != nil {
			printCreateFailureSummary(summary, req)
			return err
		}
	}

	printCreateSummary(summary, req)
	return nil
}

func applyCreateIngress(ctx *config.Context, req createRequest) error {
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
		if err := manager.EnableComponentWithOptions(context.Background(), spec, corecomponent.ApplyOptions{Yolo: true}); err != nil {
			return err
		}
		return applyISLEIngressFiles(ctx, values)
	default:
		return fmt.Errorf("unsupported ingress state %q", req.IngressState)
	}
}

func applyCreateDevMode(ctx *config.Context, req createRequest) error {
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
		if err := manager.EnableComponentWithOptions(context.Background(), spec, corecomponent.ApplyOptions{Yolo: true}); err != nil {
			return err
		}
		return applyISLEDevMode(ctx, true)
	case corecomponent.StateOff:
		if err := manager.DisableComponentWithOptions(context.Background(), spec, corecomponent.ApplyOptions{Yolo: true}); err != nil {
			return err
		}
		return applyISLEDevMode(ctx, false)
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

func runStartup(out io.Writer, ctx *config.Context) error {
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

	startupEnv, err := createPrepareStartup(out, ctx)
	if err != nil {
		return err
	}

	if err := runWithSpinner(out, "Starting the Islandora stack", func() error {
		for _, commandText := range startupCommands() {
			commandText = strings.TrimSpace(commandText)
			if commandText == "" {
				continue
			}
			commandText = createRewriteCommand(ctx, commandText)
			commandText = shellEnvPrefix(startupEnv) + commandText
			if _, err := fmt.Fprintf(logFile, "Running %s\n", commandText); err != nil {
				return err
			}
			if err := runCreateProjectShellCommand(ctx, logFile, logFile, commandText); err != nil {
				return fmt.Errorf("run %s: %w", commandText, err)
			}
		}
		return nil
	}); err != nil {
		tail, tailErr := tailLines(logPath, 20)
		fmt.Fprintln(out)
		fmt.Fprintln(out, corecomponent.RenderSection(
			"Install failed",
			fmt.Sprintf("Startup logs were saved to %s. Showing the last 20 lines below.", logPath),
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

func prepareCreateStartup(out io.Writer, ctx *config.Context) (map[string]string, error) {
	if ctx == nil {
		return nil, nil
	}
	envValues, messages, err := ctx.PrepareComposeUpPortOverride()
	if err != nil {
		return nil, err
	}
	for _, message := range messages {
		fmt.Fprintln(out, message)
	}
	return envValues, nil
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

func ensureClonedCheckout(out io.Writer, req createRequest) (bool, error) {
	repoURL := strings.TrimSpace(req.TemplateRepo)
	branch := strings.TrimSpace(req.TemplateBranch)
	projectDir := strings.TrimSpace(req.Path)
	if repoURL == "" {
		return false, fmt.Errorf("template repo cannot be empty")
	}
	if projectDir == "" {
		return false, fmt.Errorf("project directory cannot be empty")
	}

	entries, err := os.ReadDir(projectDir)
	if err == nil && len(entries) > 0 {
		return false, nil
	}
	if err != nil && !os.IsNotExist(err) {
		return false, fmt.Errorf("read project directory %q: %w", projectDir, err)
	}

	if err := os.MkdirAll(filepath.Dir(projectDir), 0o750); err != nil {
		return false, fmt.Errorf("create parent directory for %q: %w", projectDir, err)
	}

	parent := corecomponent.RenderSection(
		"Template checkout",
		fmt.Sprintf("Cloning %s at %s into %s.", repoURL, helpers.FirstNonEmpty(branch, "default branch"), projectDir),
	)
	fmt.Fprintln(out, parent)
	fmt.Fprintln(out)
	if err := runWithSpinner(out, "Cloning template repository", func() error {
		return createCloneTemplateRepo(plugin.GitTemplateOptions{
			TemplateRepo:   repoURL,
			TemplateBranch: branch,
			ProjectDir:     projectDir,
			Quiet:          true,
		})
	}); err != nil {
		return false, err
	}
	return true, nil
}

func ensureClonedCheckoutForContext(out io.Writer, ctx *config.Context, req createRequest) (bool, error) {
	if ctx == nil || ctx.DockerHostType != config.ContextRemote {
		return ensureClonedCheckout(out, req)
	}
	repoURL := strings.TrimSpace(req.TemplateRepo)
	branch := strings.TrimSpace(req.TemplateBranch)
	projectDir := strings.TrimSpace(ctx.ProjectDir)
	if repoURL == "" {
		return false, fmt.Errorf("template repo cannot be empty")
	}
	if projectDir == "" {
		return false, fmt.Errorf("project directory cannot be empty")
	}

	var present bytes.Buffer
	checkCommand := fmt.Sprintf("if [ -d %s ] && [ -n \"$(ls -A %s 2>/dev/null)\" ]; then echo present; fi", plugin.ShellQuote(projectDir), plugin.ShellQuote(projectDir))
	if err := commandSDK.RunComposeProjectCommandContext(context.Background(), ctx, "", &present, io.Discard, checkCommand); err == nil && strings.TrimSpace(present.String()) == "present" {
		return false, nil
	}
	if err := commandSDK.RunComposeProjectCommandContext(context.Background(), ctx, "", io.Discard, io.Discard, fmt.Sprintf("mkdir -p %s", plugin.ShellQuote(filepath.Dir(projectDir)))); err != nil {
		return false, fmt.Errorf("create parent directory for %q: %w", projectDir, err)
	}

	fmt.Fprintln(out, corecomponent.RenderSection(
		"Template checkout",
		fmt.Sprintf("Cloning %s at %s into %s on %s.", repoURL, helpers.FirstNonEmpty(branch, "default branch"), projectDir, ctx.SSHHostname),
	))
	fmt.Fprintln(out)
	cloneArgs := []string{"git", "clone"}
	if branch != "" {
		cloneArgs = append(cloneArgs, "--branch", branch)
	}
	cloneArgs = append(cloneArgs, repoURL, projectDir)
	initBranch := helpers.FirstNonEmpty(branch, "main")
	cloneCommand := strings.Join([]string{
		plugin.ShellJoin(cloneArgs),
		"rm -rf " + plugin.ShellQuote(filepath.Join(projectDir, ".git")),
		plugin.ShellJoin([]string{"git", "-C", projectDir, "init", "-b", initBranch}),
	}, " && ")
	if err := runWithSpinner(out, "Cloning template repository", func() error {
		return commandSDK.RunComposeProjectCommandContext(context.Background(), ctx, "", io.Discard, io.Discard, cloneCommand)
	}); err != nil {
		return false, err
	}
	return true, nil
}

func bootstrapCheckoutForContext(out io.Writer, ctx *config.Context) error {
	if ctx == nil || ctx.DockerHostType != config.ContextRemote {
		return createBootstrapCheckout(out, ctx.ProjectDir)
	}
	projectDir := strings.TrimSpace(ctx.ProjectDir)
	fmt.Fprintln(out)
	fmt.Fprintln(out, corecomponent.RenderSection(
		"Git bootstrap",
		fmt.Sprintf("Recording the pristine template checkout in %s before applying any sitectl-isle changes.", projectDir),
	))
	fmt.Fprintln(out)
	return runWithSpinner(out, "Creating initial git commit", func() error {
		safeDirectory := "safe.directory=" + projectDir
		if err := commandSDK.RunComposeProjectCommandContext(context.Background(), ctx, projectDir, io.Discard, io.Discard, plugin.ShellJoin([]string{"git", "-c", safeDirectory, "add", "."})); err != nil {
			return fmt.Errorf("stage initial checkout: %w", err)
		}
		if err := commandSDK.RunComposeProjectCommandContext(
			context.Background(),
			ctx,
			projectDir,
			io.Discard,
			io.Discard,
			plugin.ShellJoin([]string{
				"git",
				"-c", safeDirectory,
				"-c", "user.name=sitectl-isle",
				"-c", "user.email=sitectl-isle@localhost",
				"commit",
				"-m", "initial commit.",
			}),
		); err != nil {
			return fmt.Errorf("create initial commit: %w", err)
		}
		return nil
	})
}

func runCreateProjectShellCommand(ctx *config.Context, stdout, stderr io.Writer, commandText string) error {
	if ctx != nil && ctx.DockerHostType == config.ContextRemote {
		return commandSDK.RunComposeProjectCommandContext(context.Background(), ctx, ctx.ProjectDir, stdout, stderr, commandText)
	}
	return createRunProjectCommand(ctx.ProjectDir, stdout, stderr, "bash", "-lc", commandText)
}

func rewriteCreateProjectShellCommand(ctx *config.Context, commandText string) string {
	if ctx == nil {
		return commandText
	}
	return ctx.DockerComposeShellCommand(commandText)
}

func bootstrapCheckout(out io.Writer, projectDir string) error {
	fmt.Fprintln(out)
	fmt.Fprintln(out, corecomponent.RenderSection(
		"Git bootstrap",
		fmt.Sprintf("Recording the pristine template checkout in %s before applying any sitectl-isle changes.", projectDir),
	))
	fmt.Fprintln(out)

	return runWithSpinner(out, "Creating initial git commit", func() error {
		safeDirectory := "safe.directory=" + projectDir
		if err := createRunProjectCommand(projectDir, io.Discard, io.Discard, "git", "-c", safeDirectory, "add", "."); err != nil {
			return fmt.Errorf("stage initial checkout: %w", err)
		}
		if err := createRunProjectCommand(
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
		return nil
	})
}

func defaultRunProjectCommand(projectDir string, stdout, stderr io.Writer, name string, args ...string) error {
	command := exec.Command(name, args...) // #nosec G204 -- command helper is used for fixed git/template commands assembled by sitectl-isle.
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

func startupCommand() (string, string, []string) {
	return "ISLE startup commands", "bash", []string{"-lc", strings.Join(startupCommands(), " && ")}
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

func shellEnvPrefix(values map[string]string) string {
	if len(values) == 0 {
		return ""
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		if strings.TrimSpace(key) != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+shellSingleQuote(values[key]))
	}
	if len(parts) == 0 {
		return ""
	}
	return "export " + strings.Join(parts, " ") + "; "
}

package cmd

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	createpkg "github.com/libops/sitectl-isle/pkg/create"
	corecomponent "github.com/libops/sitectl/pkg/component"
	"github.com/libops/sitectl/pkg/config"
	"github.com/libops/sitectl/pkg/plugin"
	"github.com/spf13/cobra"
)

var (
	createPath               string
	createDrupalRootfs       string
	createISLEFileSystemURI  string
	createTemplateRepo       string
	createTemplateBranch     string
	createGitRemoteURL       string
	createGitRemoteName      string
	createTemplateRemoteName string
	createSetDefaultContext  bool
	createSetupOnly          bool
	createInput              = config.GetInput
	createCloneTemplateRepo  = func(opts plugin.GitTemplateOptions) error {
		return commandSDK.CloneTemplateRepo(opts)
	}
	createConfigureTemplateRemotes = func(opts plugin.GitTemplateOptions) error {
		return commandSDK.ConfigureTemplateRemotes(opts)
	}
	createEnsureLocalContext = ensureLocalContext
	createApply              = createpkg.Apply
	createRunProjectCommand  = defaultRunProjectCommand
)

const (
	defaultTemplateRepo   = "https://github.com/islandora-devops/isle-site-template"
	defaultTemplateBranch = "main"
)

type createRequest struct {
	ContextName        string
	Path               string
	DrupalRootfs       string
	TemplateRepo       string
	TemplateBranch     string
	GitRemoteURL       string
	GitRemoteName      string
	TemplateRemoteName string
	SetDefaultContext  bool
	SetupOnly          bool
	Apply              createpkg.Options
}

var createComponentOptions = []corecomponent.CreateOption{
	{
		Name:    "fcrepo",
		Default: corecomponent.StateOn,
		Guidance: corecomponent.StateGuidance{
			Question:     "FCRepo controls whether binary content is stored in Fedora.",
			OnHelp:       "Keep the default Islandora repository stack with Fedora-backed storage.",
			OffHelp:      "Store files directly in Drupal's filesystem and remove Fedora-specific wiring.",
			DefaultState: corecomponent.StateOn,
		},
	},
	{
		Name:    "blazegraph",
		Default: corecomponent.StateOn,
		Guidance: corecomponent.StateGuidance{
			Question:     "Blazegraph controls triplestore indexing support.",
			OnHelp:       "Keep triplestore indexing enabled for the standard Islandora stack.",
			OffHelp:      "Remove triplestore indexing services and Drupal actions if you do not need them.",
			DefaultState: corecomponent.StateOn,
		},
	},
}

var createCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new ISLE checkout, register a local sitectl context, and apply component-state mutations",
	RunE: func(cmd *cobra.Command, args []string) error {
		req, err := resolveCreateRequest(cmd)
		if err != nil {
			return err
		}
		return runCreateCommand(cmd, req)
	},
}

func init() {
	createCmd.Flags().StringVar(&createPath, "path", ".", "Path to the checked out isle-site-template project")
	createCmd.Flags().StringVar(&createTemplateRepo, "template-repo", defaultTemplateRepo, "Git repository to clone for the site template")
	createCmd.Flags().StringVar(&createTemplateBranch, "template-branch", defaultTemplateBranch, "Git branch or ref to clone from the template repository")
	createCmd.Flags().StringVar(&createGitRemoteURL, "git-remote-url", "", "Git repository URL to set as the working checkout remote after cloning")
	createCmd.Flags().StringVar(&createGitRemoteName, "git-remote-name", "origin", "Name of the user-facing git remote to configure when --git-remote-url is set")
	createCmd.Flags().StringVar(&createTemplateRemoteName, "template-remote-name", "upstream", "Name to keep for the template repository remote when --git-remote-url is set")
	createCmd.Flags().BoolVar(&createSetDefaultContext, "default-context", false, "Set the created sitectl context as the default context")
	createCmd.Flags().BoolVar(&createSetupOnly, "setup-only", false, "Create and customize the checkout, but do not run make up")
	corecomponent.AddCreateFlags(createCmd, createComponentOptions...)
	corecomponent.AddDrupalRootfsFlag(createCmd, &createDrupalRootfs, createpkg.DefaultDrupalRootfs)
	createCmd.Flags().StringVar(&createISLEFileSystemURI, "isle-file-system-uri", createpkg.DefaultISLEFileSystemURI, "Filesystem scheme to use when FCRepo is off. Common values are public or private")

	createCmd.Long = fmt.Sprintf("Create a local ISLE working copy from a Git template, register a local sitectl context for that working directory, then apply component-state mutations. --fcrepo accepts %q or %q. --isle-file-system-uri accepts any non-empty filesystem URI; common values are %q and %q.",
		createpkg.FcrepoStateOn, createpkg.FcrepoStateOff, createpkg.PublicISLEFileSystemURI, createpkg.PrivateISLEFileSystemURI)
}

func resolveCreateRequest(cmd *cobra.Command) (createRequest, error) {
	contextName, err := cmd.Flags().GetString("context")
	if err != nil {
		return createRequest{}, fmt.Errorf("get context flag: %w", err)
	}
	if !cmd.Flags().Changed("context") {
		contextName = ""
	}

	requestPath := createPath
	if !cmd.Flags().Changed("path") {
		requestPath = ""
	}
	setupOnly, err := cmd.Flags().GetBool("setup-only")
	if err != nil {
		return createRequest{}, fmt.Errorf("get setup-only flag: %w", err)
	}

	opts := createpkg.Options{
		Path:              createPath,
		DrupalRootfs:      createDrupalRootfs,
		ISLEFileSystemURI: createISLEFileSystemURI,
	}

	states, err := corecomponent.ResolveCreateStates(cmd, createInput, createComponentOptions...)
	if err != nil {
		return createRequest{}, err
	}
	opts.Fcrepo = string(states["fcrepo"])
	opts.Blazegraph = string(states["blazegraph"])

	if opts.Fcrepo == createpkg.FcrepoStateOff && !cmd.Flags().Changed("isle-file-system-uri") {
		var err error
		opts.ISLEFileSystemURI, err = promptISLEFileSystemURI(createpkg.DefaultISLEFileSystemURI)
		if err != nil {
			return createRequest{}, err
		}
	}

	gitRemoteURL := createGitRemoteURL
	if !cmd.Flags().Changed("git-remote-url") {
		gitRemoteURL, err = promptGitRemoteURL()
		if err != nil {
			return createRequest{}, err
		}
	}

	return createRequest{
		ContextName:        contextName,
		Path:               requestPath,
		DrupalRootfs:       createDrupalRootfs,
		TemplateRepo:       createTemplateRepo,
		TemplateBranch:     createTemplateBranch,
		GitRemoteURL:       gitRemoteURL,
		GitRemoteName:      createGitRemoteName,
		TemplateRemoteName: createTemplateRemoteName,
		SetDefaultContext:  createSetDefaultContext,
		SetupOnly:          setupOnly,
		Apply:              opts,
	}, nil
}

func promptISLEFileSystemURI(defaultValue string) (string, error) {
	prompt := fmt.Sprintf("When fcrepo is off, choose a filesystem URI. Common values are %q or %q [%s]: ",
		createpkg.PublicISLEFileSystemURI,
		createpkg.PrivateISLEFileSystemURI,
		defaultValue,
	)
	input, err := createInput(prompt)
	if err != nil {
		return "", err
	}
	value := strings.TrimSpace(input)
	if value == "" {
		return defaultValue, nil
	}
	return value, nil
}

func promptGitRemoteURL() (string, error) {
	input, err := createInput("Git remote URL for your site repository (leave blank to keep the template repo as origin): ")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(input), nil
}

func runCreateCommand(cmd *cobra.Command, req createRequest) error {
	if commandSDK == nil {
		return fmt.Errorf("plugin sdk is not initialized")
	}

	ctx, err := createEnsureLocalContext(commandSDK, req)
	if err != nil {
		return err
	}
	if err := ensureClonedCheckout(req.TemplateRepo, req.TemplateBranch, ctx.ProjectDir); err != nil {
		return err
	}
	if err := configureGitRemotes(req, ctx.ProjectDir); err != nil {
		return err
	}

	req.ContextName = ctx.Name
	req.Path = ctx.ProjectDir
	req.Apply.Path = ctx.ProjectDir
	if err := createApply(req.Apply); err != nil {
		return err
	}
	if !req.SetupOnly {
		if err := createRunProjectCommand(ctx.ProjectDir, "make", "up"); err != nil {
			return fmt.Errorf("run make up: %w", err)
		}
	}

	printCreateSummary(cmd.OutOrStdout(), req)
	return nil
}

func ensureLocalContext(sdk *plugin.SDK, req createRequest) (*config.Context, error) {
	return sdk.PromptAndSaveLocalContext(config.LocalContextCreateOptions{
		Name:              req.ContextName,
		DefaultName:       "isle-local",
		ProjectDir:        req.Path,
		DefaultProjectDir: req.Path,
		ProjectName:       filepath.Base(firstNonEmpty(req.Path, "isle-site-template")),
		ConfirmOverwrite:  false,
		SetDefault:        req.SetDefaultContext,
		Input:             createInput,
	})
}

func ensureClonedCheckout(repoURL, branch, projectDir string) error {
	if repoURL == "" {
		return fmt.Errorf("template repo cannot be empty")
	}
	if projectDir == "" {
		return fmt.Errorf("project directory cannot be empty")
	}

	entries, err := os.ReadDir(projectDir)
	if err == nil && len(entries) > 0 {
		return nil
	}
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read project directory %q: %w", projectDir, err)
	}

	if err := os.MkdirAll(filepath.Dir(projectDir), 0o755); err != nil {
		return fmt.Errorf("create parent directory for %q: %w", projectDir, err)
	}

	return createCloneTemplateRepo(plugin.GitTemplateOptions{
		TemplateRepo:   repoURL,
		TemplateBranch: branch,
		ProjectDir:     projectDir,
	})
}

func configureGitRemotes(req createRequest, projectDir string) error {
	if req.GitRemoteURL == "" {
		return nil
	}

	return createConfigureTemplateRemotes(plugin.GitTemplateOptions{
		ProjectDir:         projectDir,
		GitRemoteURL:       req.GitRemoteURL,
		GitRemoteName:      req.GitRemoteName,
		TemplateRemoteName: req.TemplateRemoteName,
	})
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func defaultRunProjectCommand(projectDir, name string, args ...string) error {
	command := exec.Command(name, args...)
	command.Dir = projectDir
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	command.Env = os.Environ()
	if os.Getenv("TERM") == "" {
		command.Env = append(command.Env, "TERM=dumb")
	}
	return command.Run()
}

func printCreateSummary(out io.Writer, req createRequest) {
	fmt.Fprintln(out)
	fmt.Fprintln(out, "ISLE site created locally.")
	if req.SetupOnly {
		fmt.Fprintf(out, "Checkout: %s\n", req.Path)
		fmt.Fprintf(out, "Context:  %s\n", req.ContextName)
		fmt.Fprintln(out, "The site was prepared and left stopped because --setup-only was used.")
	} else {
		fmt.Fprintf(out, "Checkout: %s\n", req.Path)
		fmt.Fprintf(out, "Context:  %s\n", req.ContextName)
		fmt.Fprintln(out, "The site was prepared and brought up with make up.")
	}
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Review the generated changes and commit them.")
	fmt.Fprintf(out, "Suggested commit:\n%s\n", buildCommitSuggestion(req))
}

func buildCommitSuggestion(req createRequest) string {
	return fmt.Sprintf(
		"cd %s\ngit add .\n%s",
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
		`--context=` + shellDoubleQuote(req.ContextName),
		`--path=` + shellDoubleQuote(req.Path),
		`--template-repo=` + shellDoubleQuote(req.TemplateRepo),
		`--template-branch=` + shellDoubleQuote(req.TemplateBranch),
		`--git-remote-name=` + shellDoubleQuote(req.GitRemoteName),
		`--template-remote-name=` + shellDoubleQuote(req.TemplateRemoteName),
		`--drupal-rootfs=` + shellDoubleQuote(req.DrupalRootfs),
		`--fcrepo=` + req.Apply.Fcrepo,
		`--blazegraph=` + req.Apply.Blazegraph,
		`--isle-file-system-uri=` + shellDoubleQuote(req.Apply.ISLEFileSystemURI),
	}
	if req.GitRemoteURL != "" {
		args = append(args, `--git-remote-url=`+shellDoubleQuote(req.GitRemoteURL))
	}
	if req.SetDefaultContext {
		args = append(args, "--default-context")
	}
	if req.SetupOnly {
		args = append(args, "--setup-only")
	}
	lines := []string{"sitectl isle create \\"}
	for i, arg := range args {
		line := "  " + arg
		if i < len(args)-1 {
			line += " \\"
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
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

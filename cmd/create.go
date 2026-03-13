package cmd

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

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
	createRunStartup         = runStartup
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

var createCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a a new ISLE install",
	Long: `Use Islandora' ISLE Site Template to install your own running version of Islandora.

This command will walk you through setting up Islandora.

After you answer a few questions, an Islandora site will be running on your machine, so be sure docker is installed and running.
`,
	RunE: func(cmd *cobra.Command, args []string) error {
		printIslandoraIntro(cmd, cmd.OutOrStdout())
		req, err := resolveCreateRequest(cmd)
		if err != nil {
			return err
		}
		return runCreateCommand(cmd, req)
	},
}

func init() {
	createCmd.Flags().StringVar(&createPath, "path", ".", "Path to the checked out isle-site-template project")
	createCmd.Flags().StringVar(&createTemplateRepo, "template-repo", defaultTemplateRepo, "Source git repository to clone for the site template")
	createCmd.Flags().StringVar(&createTemplateBranch, "template-branch", defaultTemplateBranch, "Git branch or ref to clone from the template repository")
	createCmd.Flags().StringVar(&createGitRemoteURL, "git-remote-url", "", "Where you will host git. Git repository URL to set as the working checkout remote after cloning")
	createCmd.Flags().StringVar(&createGitRemoteName, "git-remote-name", "origin", "Name of the user-facing git remote to configure when --git-remote-url is set")
	createCmd.Flags().StringVar(&createTemplateRemoteName, "template-remote-name", "upstream", "Name to keep for the template repository remote when --git-remote-url is set")
	createCmd.Flags().BoolVar(&createSetDefaultContext, "default-context", false, "Set the created sitectl context as the default context")
	createCmd.Flags().BoolVar(&createSetupOnly, "setup-only", false, "Create and customize the checkout, but do not run make up")
	corecomponent.AddCreateFlags(createCmd, createComponentOptions()...)
	corecomponent.AddDrupalRootfsFlag(createCmd, &createDrupalRootfs, createpkg.DefaultDrupalRootfs)
	createCmd.Flags().StringVar(&createISLEFileSystemURI, "isle-file-system-uri", createpkg.DefaultISLEFileSystemURI, "Filesystem scheme to use when FCRepo is off. Common values are public or private")
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

	states, err := corecomponent.ResolveCreateStates(cmd, createInput, createComponentOptions()...)
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

func createComponentOptions() []corecomponent.CreateOption {
	defs := orderedComponentDefinitions()
	options := make([]corecomponent.CreateOption, 0, len(defs))
	for _, def := range defs {
		options = append(options, def.CreateOption())
	}
	return options
}

func promptISLEFileSystemURI(defaultValue string) (string, error) {
	question := corecomponent.RenderSection(
		"File system URI",
		fmt.Sprintf("When fcrepo is off, choose the Drupal filesystem URI to use for stored files. Common values are %q and %q.", createpkg.PublicISLEFileSystemURI, createpkg.PrivateISLEFileSystemURI),
	)
	prompt := corecomponent.RenderPromptLine(fmt.Sprintf("Choose isle-file-system-uri [%s]: ", defaultValue))
	input, err := createInput(append(strings.Split(question, "\n"), "", prompt)...)
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
	question := corecomponent.RenderSection(
		"Git remote URL",
		"Set the Git remote URL for your site repository. Leave this blank to keep the template repository as origin.",
	)
	prompt := corecomponent.RenderPromptLine("Git remote URL: ")
	input, err := createInput(append(strings.Split(question, "\n"), "", prompt)...)
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
	if err := ensureClonedCheckout(cmd.OutOrStdout(), req.TemplateRepo, req.TemplateBranch, ctx.ProjectDir); err != nil {
		return err
	}
	if err := configureGitRemotes(req, ctx.ProjectDir); err != nil {
		return err
	}

	req.ContextName = ctx.Name
	req.Path = ctx.ProjectDir
	req.Apply.Path = ctx.ProjectDir
	if err := createApply(req.Apply); err != nil {
		printCreateFailureSummary(cmd.OutOrStdout(), req)
		return err
	}
	if !req.SetupOnly {
		if err := createRunStartup(cmd.OutOrStdout(), ctx); err != nil {
			printCreateFailureSummary(cmd.OutOrStdout(), req)
			return err
		}
	}

	printCreateSummary(cmd.OutOrStdout(), req)
	return nil
}

func printIslandoraIntro(cmd *cobra.Command, out io.Writer) {
	fmt.Fprintln(out, corecomponent.RenderIntroSection(
		cmd.Short,
		cmd.Long,
	))
	fmt.Fprintln(out)
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
		ContextNamePrompt: append(
			strings.Split(corecomponent.RenderSection("sitectl context name", `Choose the sitectl context name to save for this local checkout.
This is only important if you'll be running multiple ISLE installs on this machine. This is just a short label so you can easily identify multiple ISLE`), "\n"),
			"",
			corecomponent.RenderPromptLine("Context name [%s]: "),
		),
		ProjectDirPrompt: append(
			strings.Split(corecomponent.RenderSection("Project directory", `Choose the full directory path where this ISLE install will live on your machine.
ISLE Site Template will be cloned into this directory, so make sure it's empty of doesn't exist yet.`), "\n"),
			"",
			corecomponent.RenderPromptLine("Project directory [%s]: "),
		),
	})
}

func runStartup(out io.Writer, ctx *config.Context) error {
	logPath, err := startupLogPath(ctx.Name)
	if err != nil {
		return fmt.Errorf("resolve startup log path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return fmt.Errorf("create startup log directory: %w", err)
	}

	logFile, err := os.Create(logPath)
	if err != nil {
		return fmt.Errorf("create startup log %q: %w", logPath, err)
	}
	defer logFile.Close()

	fmt.Fprintln(out, corecomponent.RenderSection(
		"Install",
		"The site install is now starting. Docker must be running, network access is required so Docker can pull images, and Docker Buildx is required because ISLE will build the Drupal container locally. Once docker compose brings the containers up, the site will install automatically and be configured using the Islandora starter site. Output is being written to a log file while the terminal shows progress. Expect your web browser to open when the site is ready.",
	))
	fmt.Fprintln(out)

	if err := runWithSpinner(out, "Running make init up", func() error {
		return createRunProjectCommand(ctx.ProjectDir, logFile, logFile, "make", "init", "up")
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
		return fmt.Errorf("run make up: %w", err)
	}

	fmt.Fprintln(out)
	fmt.Fprintln(out, corecomponent.RenderSection(
		"Install log",
		fmt.Sprintf("Startup logs were saved to %s.", logPath),
	))
	fmt.Fprintln(out)
	return nil
}

func ensureClonedCheckout(out io.Writer, repoURL, branch, projectDir string) error {
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

	parent := corecomponent.RenderSection(
		"Template checkout",
		fmt.Sprintf("Cloning %s at %s into %s.", repoURL, firstNonEmpty(branch, "default branch"), projectDir),
	)
	fmt.Fprintln(out, parent)
	fmt.Fprintln(out)
	return runWithSpinner(out, "Cloning template repository", func() error {
		return createCloneTemplateRepo(plugin.GitTemplateOptions{
			TemplateRepo:   repoURL,
			TemplateBranch: branch,
			ProjectDir:     projectDir,
			Quiet:          true,
		})
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

func defaultRunProjectCommand(projectDir string, stdout, stderr io.Writer, name string, args ...string) error {
	command := exec.Command(name, args...)
	command.Dir = projectDir
	command.Stdout = stdout
	command.Stderr = stderr
	command.Env = os.Environ()
	if os.Getenv("TERM") == "" {
		command.Env = append(command.Env, "TERM=dumb")
	}
	return command.Run()
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
	go func() {
		done <- fn()
	}()

	frames := []string{"|", "/", "-", `\`}
	ticker := time.NewTicker(120 * time.Millisecond)
	defer ticker.Stop()

	index := 0
	for {
		select {
		case err := <-done:
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
	file, err := os.Open(path)
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
	fmt.Fprintln(out, "Retry command:")
	fmt.Fprintln(out, buildRecreateCommand(req))
	fmt.Fprintln(out)
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

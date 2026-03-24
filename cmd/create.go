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
	"github.com/libops/sitectl/pkg/helpers"
	"github.com/libops/sitectl/pkg/plugin"
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
	createCheckPrereqs       = checkPrereqs
	createLookPath           = exec.LookPath
	createRunCheckCommand    = runCheckCommand
	createSleep              = time.Sleep
)

const (
	defaultTemplateRepo   = "https://github.com/islandora-devops/isle-site-template"
	defaultTemplateBranch = "main"
)

type createRequest struct {
	plugin.ComposeCreateRequest
	Apply createpkg.Options
}

type createRunner struct{}

func (createRunner) BindFlags(cmd *cobra.Command) {
	bindCreateFlags(cmd)
}

func (createRunner) Run(cmd *cobra.Command) error {
	printIslandoraIntro(cmd, cmd.OutOrStdout())
	if err := createCheckPrereqs(cmd.OutOrStdout()); err != nil {
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
		Description:         "Create a new ISLE site from the upstream template",
		Default:             true,
		MinCPUCores:         4,
		MinMemory:           "8 GiB",
		MinDiskSpace:        "30 GiB",
		DockerComposeRepo:   defaultTemplateRepo,
		DockerComposeBranch: defaultTemplateBranch,
		DockerComposeUp:     []string{"make up"},
		DockerComposeDown:   []string{"make clean"},
	}
}

func bindCreateFlags(cmd *cobra.Command) {
	if err := commandSDK.BindComposeCreateFlags(cmd, createDefinition(), &createDrupalRootfs, createpkg.DefaultDrupalRootfs); err != nil {
		panic(err)
	}
}

func resolveCreateRequest(cmd *cobra.Command) (createRequest, error) {
	resolved, err := commandSDK.ResolveComposeCreateRequest(cmd, createInput, createDrupalRootfs, "", defaultTemplateRepo, defaultTemplateBranch)
	if err != nil {
		return createRequest{}, err
	}
	opts := createpkg.Options{
		Path:         resolved.Path,
		DrupalRootfs: helpers.FirstNonEmpty(resolved.DrupalRootfs, createpkg.DefaultDrupalRootfs),
	}
	if decision, ok := resolved.Decisions["fcrepo"]; ok {
		opts.Fcrepo = string(decision.State)
		opts.ISLEFileSystemURI = strings.TrimSpace(decision.Options["isle-file-system-uri"])
	}
	if decision, ok := resolved.Decisions["blazegraph"]; ok {
		opts.Blazegraph = string(decision.State)
	}
	if opts.ISLEFileSystemURI == "" {
		opts.ISLEFileSystemURI = createpkg.DefaultISLEFileSystemURI
	}
	return createRequest{
		ComposeCreateRequest: resolved,
		Apply:                opts,
	}, nil
}

func runCreateCommand(cmd *cobra.Command, req createRequest) error {
	if commandSDK == nil {
		return fmt.Errorf("plugin sdk is not initialized")
	}

	ctx, err := createEnsureLocalContext(commandSDK, req)
	if err != nil {
		return err
	}
	cloned, err := ensureClonedCheckout(cmd.OutOrStdout(), req.TemplateRepo, req.TemplateBranch, ctx.ProjectDir)
	if err != nil {
		return err
	}
	req.ContextName = ctx.Name
	req.Path = ctx.ProjectDir
	req.Apply.Path = ctx.ProjectDir
	if cloned {
		if err := createBootstrapCheckout(cmd.OutOrStdout(), ctx.ProjectDir); err != nil {
			return err
		}
	}
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

func ensureCreateContext(sdk *plugin.SDK, req createRequest) (*config.Context, error) {
	if sdk == nil {
		return nil, fmt.Errorf("plugin sdk is not initialized")
	}
	return sdk.EnsureComposeCreateContext(req.ComposeCreateRequest, plugin.ComposeCreateContextOptions{
		DefaultName:         "isle-local",
		DefaultSite:         filepath.Base(helpers.FirstNonEmpty(req.Path, "isle-site-template")),
		DefaultPlugin:       "isle",
		DefaultProjectDir:   req.Path,
		DefaultProjectName:  filepath.Base(helpers.FirstNonEmpty(req.Path, "isle-site-template")),
		DefaultEnvironment:  "local",
		DefaultDrupalRootfs: req.DrupalRootfs,
		DrupalContainerRoot: "/var/www/drupal",
		Input:               createInput,
	})
}

func runStartup(out io.Writer, ctx *config.Context) error {
	commandLabel, commandName, commandArgs := startupCommand()
	cleanupLabel, _, _ := cleanupCommand()
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
		"Islandora install is now running",
		fmt.Sprintf(`ISLE will now run %s from the checked out template.
While this runs, Docker will pull images over the network and build the Drupal container locally before Docker Compose starts the stack.

Once docker compose brings the containers up, the Islandora Drupal site will install automatically and be configured using the Islandora starter site.

Output is being written to a log file while the terminal shows progress. Expect your web browser to open when the site is ready.

If you need to cancel, press Ctrl+C, then run:

  cd %s
  %s

This will completely stop and destroy the setup.`, commandLabel, shellPath(ctx.ProjectDir), cleanupLabel),
	))
	fmt.Fprintln(out)

	if err := runWithSpinner(out, "Starting the Islandora stack", func() error {
		return createRunProjectCommand(ctx.ProjectDir, logFile, logFile, commandName, commandArgs...)
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
		return fmt.Errorf("run %s: %w", commandLabel, err)
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
	if _, err := createLookPath("make"); err != nil {
		fmt.Fprintln(out, corecomponent.RenderChecklistItem(
			"make is installed",
			"fallback",
			"missing, so create will run bash ./scripts/up.sh instead",
		))
	} else {
		fmt.Fprintln(out, corecomponent.RenderChecklistItem("make is installed", "ok", ""))
	}
	fmt.Fprintln(out)
	return nil
}

func ensureClonedCheckout(out io.Writer, repoURL, branch, projectDir string) (bool, error) {
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

	if err := os.MkdirAll(filepath.Dir(projectDir), 0o755); err != nil {
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

func bootstrapCheckout(out io.Writer, projectDir string) error {
	fmt.Fprintln(out)
	fmt.Fprintln(out, corecomponent.RenderSection(
		"Git bootstrap",
		fmt.Sprintf("Recording the pristine template checkout in %s before applying any sitectl-isle changes.", projectDir),
	))
	fmt.Fprintln(out)

	return runWithSpinner(out, "Creating initial git commit", func() error {
		if err := createRunProjectCommand(projectDir, io.Discard, io.Discard, "git", "add", "."); err != nil {
			return fmt.Errorf("stage initial checkout: %w", err)
		}
		if err := createRunProjectCommand(
			projectDir,
			io.Discard,
			io.Discard,
			"git",
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

func runCheckCommand(name string, args ...string) error {
	command := exec.Command(name, args...)
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	command.Env = os.Environ()
	return command.Run()
}

func startupCommand() (string, string, []string) {
	if _, err := createLookPath("make"); err == nil {
		return "make up", "make", []string{"up"}
	}
	return "bash ./scripts/up.sh", "bash", []string{"./scripts/up.sh"}
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
	fmt.Fprintln(out, corecomponent.RenderCommandBlock(buildRecreateCommand(req)))
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
		`--context=` + shellDoubleQuote(req.ContextName),
		`--path=` + shellDoubleQuote(req.Path),
		`--template-repo=` + shellDoubleQuote(req.TemplateRepo),
		`--template-branch=` + shellDoubleQuote(req.TemplateBranch),
		`--drupal-rootfs=` + shellDoubleQuote(req.DrupalRootfs),
		`--fcrepo=` + req.Apply.Fcrepo,
		`--blazegraph=` + req.Apply.Blazegraph,
		`--isle-file-system-uri=` + shellDoubleQuote(req.Apply.ISLEFileSystemURI),
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

func shellDoubleQuote(value string) string {
	return strconv.Quote(value)
}

func shellSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

func shellPath(value string) string {
	return shellSingleQuote(value)
}

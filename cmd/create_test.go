package cmd

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	createpkg "github.com/libops/sitectl-isle/pkg/create"
	corecomponent "github.com/libops/sitectl/pkg/component"
	"github.com/libops/sitectl/pkg/config"
	"github.com/libops/sitectl/pkg/plugin"
	"github.com/spf13/cobra"
)

func TestResolveCreateRequestPromptsForMissingComponentFlags(t *testing.T) {
	oldInput := createInput
	t.Cleanup(func() {
		createInput = oldInput
	})

	var promptCount int
	inputs := []string{"off", "on", "public", ""}
	createInput = func(question ...string) (string, error) {
		promptCount++
		value := inputs[0]
		inputs = inputs[1:]
		return value, nil
	}

	oldPath := createPath
	oldDrupalRootfs := createDrupalRootfs
	oldISLEFileSystemURI := createISLEFileSystemURI
	oldTemplateRepo := createTemplateRepo
	oldTemplateBranch := createTemplateBranch
	oldGitRemoteURL := createGitRemoteURL
	oldGitRemoteName := createGitRemoteName
	oldTemplateRemoteName := createTemplateRemoteName
	oldSetDefaultContext := createSetDefaultContext
	oldSetupOnly := createSetupOnly
	t.Cleanup(func() {
		createPath = oldPath
		createDrupalRootfs = oldDrupalRootfs
		createISLEFileSystemURI = oldISLEFileSystemURI
		createTemplateRepo = oldTemplateRepo
		createTemplateBranch = oldTemplateBranch
		createGitRemoteURL = oldGitRemoteURL
		createGitRemoteName = oldGitRemoteName
		createTemplateRemoteName = oldTemplateRemoteName
		createSetDefaultContext = oldSetDefaultContext
		createSetupOnly = oldSetupOnly
	})

	createPath = "/tmp/site"
	createDrupalRootfs = createpkg.DefaultDrupalRootfs
	createISLEFileSystemURI = "private"
	createTemplateRepo = defaultTemplateRepo
	createTemplateBranch = defaultTemplateBranch
	createGitRemoteURL = ""
	createGitRemoteName = "origin"
	createTemplateRemoteName = "upstream"

	cmd := newCreateCommandForTest()

	req, err := resolveCreateRequest(cmd)
	if err != nil {
		t.Fatalf("resolveCreateRequest() error = %v", err)
	}

	if promptCount != 4 {
		t.Fatalf("expected 4 prompts, got %d", promptCount)
	}
	if req.ContextName != "" {
		t.Fatalf("expected context name prompt path, got %q", req.ContextName)
	}
	if req.Apply.Fcrepo != "off" {
		t.Fatalf("expected prompted fcrepo state off, got %q", req.Apply.Fcrepo)
	}
	if req.Apply.Blazegraph != "on" {
		t.Fatalf("expected prompted blazegraph state on, got %q", req.Apply.Blazegraph)
	}
	if req.Apply.ISLEFileSystemURI != "public" {
		t.Fatalf("expected prompted isle-file-system-uri public, got %q", req.Apply.ISLEFileSystemURI)
	}
	if req.TemplateRepo != defaultTemplateRepo {
		t.Fatalf("expected template repo %q, got %q", defaultTemplateRepo, req.TemplateRepo)
	}
	if req.TemplateBranch != defaultTemplateBranch {
		t.Fatalf("expected template branch %q, got %q", defaultTemplateBranch, req.TemplateBranch)
	}
	if req.GitRemoteURL != "" {
		t.Fatalf("expected empty git remote url, got %q", req.GitRemoteURL)
	}
}

func TestResolveCreateRequestSkipsPromptForExplicitFlags(t *testing.T) {
	oldInput := createInput
	t.Cleanup(func() {
		createInput = oldInput
	})

	createInput = func(question ...string) (string, error) {
		t.Fatal("did not expect prompt")
		return "", nil
	}

	oldPath := createPath
	oldDrupalRootfs := createDrupalRootfs
	oldISLEFileSystemURI := createISLEFileSystemURI
	oldTemplateRepo := createTemplateRepo
	oldTemplateBranch := createTemplateBranch
	oldGitRemoteURL := createGitRemoteURL
	oldGitRemoteName := createGitRemoteName
	oldTemplateRemoteName := createTemplateRemoteName
	oldSetDefaultContext := createSetDefaultContext
	oldSetupOnly := createSetupOnly
	t.Cleanup(func() {
		createPath = oldPath
		createDrupalRootfs = oldDrupalRootfs
		createISLEFileSystemURI = oldISLEFileSystemURI
		createTemplateRepo = oldTemplateRepo
		createTemplateBranch = oldTemplateBranch
		createGitRemoteURL = oldGitRemoteURL
		createGitRemoteName = oldGitRemoteName
		createTemplateRemoteName = oldTemplateRemoteName
		createSetDefaultContext = oldSetDefaultContext
		createSetupOnly = oldSetupOnly
	})

	createPath = "/tmp/site"
	createDrupalRootfs = createpkg.DefaultDrupalRootfs
	createISLEFileSystemURI = "public"
	createTemplateRepo = defaultTemplateRepo
	createTemplateBranch = defaultTemplateBranch
	createGitRemoteURL = "git@github.com:example/site.git"
	createGitRemoteName = "origin"
	createTemplateRemoteName = "upstream"

	cmd := newCreateCommandForTest()
	_ = cmd.Flags().Set("context", "isle-local")
	_ = cmd.Flags().Set("fcrepo", "off")
	_ = cmd.Flags().Set("blazegraph", "on")
	_ = cmd.Flags().Set("isle-file-system-uri", "public")
	_ = cmd.Flags().Set("git-remote-url", "git@github.com:example/site.git")

	req, err := resolveCreateRequest(cmd)
	if err != nil {
		t.Fatalf("resolveCreateRequest() error = %v", err)
	}

	if req.Apply.Fcrepo != "off" || req.Apply.Blazegraph != "on" || req.Apply.ISLEFileSystemURI != "public" {
		t.Fatalf("unexpected options %+v", req.Apply)
	}
	if req.GitRemoteURL != "git@github.com:example/site.git" {
		t.Fatalf("expected git remote url preserved, got %q", req.GitRemoteURL)
	}
}

func TestResolveCreateRequestAcceptsCustomISLEFileSystemURI(t *testing.T) {
	oldInput := createInput
	t.Cleanup(func() {
		createInput = oldInput
	})

	createInput = func(question ...string) (string, error) {
		t.Fatal("did not expect prompt")
		return "", nil
	}

	oldPath := createPath
	oldDrupalRootfs := createDrupalRootfs
	oldISLEFileSystemURI := createISLEFileSystemURI
	oldTemplateRepo := createTemplateRepo
	oldTemplateBranch := createTemplateBranch
	oldGitRemoteURL := createGitRemoteURL
	oldGitRemoteName := createGitRemoteName
	oldTemplateRemoteName := createTemplateRemoteName
	oldSetDefaultContext := createSetDefaultContext
	oldSetupOnly := createSetupOnly
	t.Cleanup(func() {
		createPath = oldPath
		createDrupalRootfs = oldDrupalRootfs
		createISLEFileSystemURI = oldISLEFileSystemURI
		createTemplateRepo = oldTemplateRepo
		createTemplateBranch = oldTemplateBranch
		createGitRemoteURL = oldGitRemoteURL
		createGitRemoteName = oldGitRemoteName
		createTemplateRemoteName = oldTemplateRemoteName
		createSetDefaultContext = oldSetDefaultContext
		createSetupOnly = oldSetupOnly
	})

	createPath = "/tmp/site"
	createDrupalRootfs = createpkg.DefaultDrupalRootfs
	createISLEFileSystemURI = "archive"
	createTemplateRepo = defaultTemplateRepo
	createTemplateBranch = defaultTemplateBranch
	createGitRemoteURL = ""
	createGitRemoteName = "origin"
	createTemplateRemoteName = "upstream"

	cmd := newCreateCommandForTest()
	_ = cmd.Flags().Set("context", "isle-local")
	_ = cmd.Flags().Set("fcrepo", "off")
	_ = cmd.Flags().Set("blazegraph", "on")
	_ = cmd.Flags().Set("isle-file-system-uri", "archive")
	_ = cmd.Flags().Set("git-remote-url", "")

	req, err := resolveCreateRequest(cmd)
	if err != nil {
		t.Fatalf("resolveCreateRequest() error = %v", err)
	}

	if req.Apply.ISLEFileSystemURI != "archive" {
		t.Fatalf("expected custom isle-file-system-uri preserved, got %q", req.Apply.ISLEFileSystemURI)
	}
	if req.Apply.DrupalRootfs != createpkg.DefaultDrupalRootfs {
		t.Fatalf("expected drupal rootfs preserved, got %q", req.Apply.DrupalRootfs)
	}
}

func TestEnsureClonedCheckoutClonesEmptyDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	projectDir := filepath.Join(tmpDir, "site")

	oldClone := createCloneTemplateRepo
	t.Cleanup(func() {
		createCloneTemplateRepo = oldClone
	})

	var cloned bool
	createCloneTemplateRepo = func(opts plugin.GitTemplateOptions) error {
		cloned = true
		if opts.TemplateRepo != defaultTemplateRepo {
			t.Fatalf("expected repo %q, got %q", defaultTemplateRepo, opts.TemplateRepo)
		}
		if opts.TemplateBranch != defaultTemplateBranch {
			t.Fatalf("expected branch %q, got %q", defaultTemplateBranch, opts.TemplateBranch)
		}
		if opts.ProjectDir != projectDir {
			t.Fatalf("expected dir %q, got %q", projectDir, opts.ProjectDir)
		}
		if !opts.Quiet {
			t.Fatal("expected clone to run in quiet mode")
		}
		return os.MkdirAll(opts.ProjectDir, 0o755)
	}

	if err := ensureClonedCheckout(io.Discard, defaultTemplateRepo, defaultTemplateBranch, projectDir); err != nil {
		t.Fatalf("ensureClonedCheckout() error = %v", err)
	}
	if !cloned {
		t.Fatal("expected clone to run")
	}
}

func TestEnsureClonedCheckoutSkipsNonEmptyDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	projectDir := filepath.Join(tmpDir, "site")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(projectDir) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "docker-compose.yml"), []byte("services: {}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(docker-compose.yml) error = %v", err)
	}

	oldClone := createCloneTemplateRepo
	t.Cleanup(func() {
		createCloneTemplateRepo = oldClone
	})

	createCloneTemplateRepo = func(opts plugin.GitTemplateOptions) error {
		t.Fatal("did not expect clone to run")
		return nil
	}

	if err := ensureClonedCheckout(io.Discard, defaultTemplateRepo, defaultTemplateBranch, projectDir); err != nil {
		t.Fatalf("ensureClonedCheckout() error = %v", err)
	}
}

func TestConfigureGitRemotesUsesConfiguredRemote(t *testing.T) {
	oldConfigure := createConfigureTemplateRemotes
	t.Cleanup(func() {
		createConfigureTemplateRemotes = oldConfigure
	})

	var got plugin.GitTemplateOptions
	createConfigureTemplateRemotes = func(opts plugin.GitTemplateOptions) error {
		got = opts
		return nil
	}

	err := configureGitRemotes(createRequest{
		GitRemoteURL:       "git@github.com:example/site.git",
		GitRemoteName:      "origin",
		TemplateRemoteName: "upstream",
	}, "/tmp/site")
	if err != nil {
		t.Fatalf("configureGitRemotes() error = %v", err)
	}
	if got.ProjectDir != "/tmp/site" {
		t.Fatalf("expected project dir /tmp/site, got %q", got.ProjectDir)
	}
	if got.GitRemoteURL != "git@github.com:example/site.git" {
		t.Fatalf("expected git remote url preserved, got %q", got.GitRemoteURL)
	}
}

func TestRunCreateCommandRunsMakeUpAndPrintsCommitSuggestion(t *testing.T) {
	oldSDK := commandSDK
	oldEnsure := createEnsureLocalContext
	oldClone := createCloneTemplateRepo
	oldConfigure := createConfigureTemplateRemotes
	oldApply := createApply
	oldRunStartup := createRunStartup
	t.Cleanup(func() {
		commandSDK = oldSDK
		createEnsureLocalContext = oldEnsure
		createCloneTemplateRepo = oldClone
		createConfigureTemplateRemotes = oldConfigure
		createApply = oldApply
		createRunStartup = oldRunStartup
	})

	commandSDK = &plugin.SDK{}
	projectDir := filepath.Join(t.TempDir(), "site")
	createEnsureLocalContext = func(_ *plugin.SDK, req createRequest) (*config.Context, error) {
		return &config.Context{Name: "isle-local-2", ProjectDir: projectDir}, nil
	}
	createCloneTemplateRepo = func(opts plugin.GitTemplateOptions) error { return nil }
	createConfigureTemplateRemotes = func(opts plugin.GitTemplateOptions) error { return nil }
	createApply = func(opts createpkg.Options) error { return nil }

	var ranStartup bool
	createRunStartup = func(_ io.Writer, ctx *config.Context) error {
		ranStartup = true
		if ctx.ProjectDir != projectDir {
			t.Fatalf("expected startup in %q, got %q", projectDir, ctx.ProjectDir)
		}
		if ctx.Name != "isle-local-2" {
			t.Fatalf("expected context isle-local-2, got %q", ctx.Name)
		}
		return nil
	}

	var out bytes.Buffer
	cmd := &cobra.Command{Use: "create"}
	cmd.SetOut(&out)

	err := runCreateCommand(cmd, createRequest{
		Path:               projectDir,
		DrupalRootfs:       createpkg.DefaultDrupalRootfs,
		TemplateRepo:       defaultTemplateRepo,
		TemplateBranch:     defaultTemplateBranch,
		GitRemoteURL:       "git@github.com:example/site.git",
		GitRemoteName:      "origin",
		TemplateRemoteName: "upstream",
		Apply: createpkg.Options{
			DrupalRootfs:      createpkg.DefaultDrupalRootfs,
			Fcrepo:            createpkg.FcrepoStateOn,
			Blazegraph:        createpkg.FcrepoStateOff,
			ISLEFileSystemURI: createpkg.PublicISLEFileSystemURI,
		},
	})
	if err != nil {
		t.Fatalf("runCreateCommand() error = %v", err)
	}
	if !ranStartup {
		t.Fatal("expected startup to run")
	}

	rendered := out.String()
	if !strings.Contains(rendered, "cd '") {
		t.Fatalf("expected cd line in commit suggestion, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "\ngit add .\ngit commit -m") {
		t.Fatalf("expected commit suggestion, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "-m 'sitectl isle create \\\n") {
		t.Fatalf("expected multiline recreate command body, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "--fcrepo=on \\\n  --blazegraph=off") {
		t.Fatalf("expected recreate command with component flags, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "--git-remote-url=") {
		t.Fatalf("expected recreate command with git remote url, got:\n%s", rendered)
	}
}

func TestRunCreateCommandSkipsMakeUpWhenSetupOnly(t *testing.T) {
	oldSDK := commandSDK
	oldEnsure := createEnsureLocalContext
	oldClone := createCloneTemplateRepo
	oldConfigure := createConfigureTemplateRemotes
	oldApply := createApply
	oldRunStartup := createRunStartup
	t.Cleanup(func() {
		commandSDK = oldSDK
		createEnsureLocalContext = oldEnsure
		createCloneTemplateRepo = oldClone
		createConfigureTemplateRemotes = oldConfigure
		createApply = oldApply
		createRunStartup = oldRunStartup
	})

	commandSDK = &plugin.SDK{}
	projectDir := filepath.Join(t.TempDir(), "site")
	createEnsureLocalContext = func(_ *plugin.SDK, req createRequest) (*config.Context, error) {
		return &config.Context{Name: "isle-local", ProjectDir: projectDir}, nil
	}
	createCloneTemplateRepo = func(opts plugin.GitTemplateOptions) error { return nil }
	createConfigureTemplateRemotes = func(opts plugin.GitTemplateOptions) error { return nil }
	createApply = func(opts createpkg.Options) error { return nil }
	createRunStartup = func(_ io.Writer, ctx *config.Context) error {
		t.Fatal("did not expect startup to run")
		return nil
	}

	var out bytes.Buffer
	cmd := &cobra.Command{Use: "create"}
	cmd.SetOut(&out)

	err := runCreateCommand(cmd, createRequest{
		Path:               projectDir,
		DrupalRootfs:       createpkg.DefaultDrupalRootfs,
		TemplateRepo:       defaultTemplateRepo,
		TemplateBranch:     defaultTemplateBranch,
		GitRemoteName:      "origin",
		TemplateRemoteName: "upstream",
		SetupOnly:          true,
		Apply: createpkg.Options{
			DrupalRootfs:      createpkg.DefaultDrupalRootfs,
			Fcrepo:            createpkg.FcrepoStateOff,
			Blazegraph:        createpkg.FcrepoStateOn,
			ISLEFileSystemURI: createpkg.PrivateISLEFileSystemURI,
		},
	})
	if err != nil {
		t.Fatalf("runCreateCommand() error = %v", err)
	}

	rendered := out.String()
	if !strings.Contains(rendered, "--setup-only") {
		t.Fatalf("expected recreate command to include --setup-only, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "cd '") {
		t.Fatalf("expected cd line in commit suggestion, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "prepared and left stopped because --setup-only was used") {
		t.Fatalf("expected setup-only summary, got:\n%s", rendered)
	}
}

func newCreateCommandForTest() *cobra.Command {
	cmd := &cobra.Command{Use: "create"}
	cmd.Flags().String("context", "local", "")
	cmd.Flags().String("path", ".", "")
	cmd.Flags().String("template-repo", defaultTemplateRepo, "")
	cmd.Flags().String("template-branch", defaultTemplateBranch, "")
	cmd.Flags().String("git-remote-url", "", "")
	cmd.Flags().String("git-remote-name", "origin", "")
	cmd.Flags().String("template-remote-name", "upstream", "")
	cmd.Flags().Bool("default-context", false, "")
	cmd.Flags().Bool("setup-only", false, "")
	corecomponent.AddCreateFlags(cmd, createComponentOptions()...)
	corecomponent.AddDrupalRootfsFlag(cmd, &createDrupalRootfs, createpkg.DefaultDrupalRootfs)
	cmd.Flags().String("isle-file-system-uri", "private", "")
	return cmd
}

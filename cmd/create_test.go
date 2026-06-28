package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	createpkg "github.com/libops/sitectl-isle/pkg/create"
	corecomponent "github.com/libops/sitectl/pkg/component"
	"github.com/libops/sitectl/pkg/config"
	"github.com/libops/sitectl/pkg/plugin"
	coretraefik "github.com/libops/sitectl/pkg/services/traefik"
	"github.com/spf13/cobra"
)

func TestResolveCreateRequestPromptsForMissingComponentFlags(t *testing.T) {
	oldInput := createInput
	t.Cleanup(func() {
		createInput = oldInput
	})

	var promptCount int
	inputs := []string{"2", "1", "1", "1", "1"}
	createInput = func(question ...string) (string, error) {
		promptCount++
		value := inputs[0]
		inputs = inputs[1:]
		return value, nil
	}

	oldPath := createPath
	oldDrupalRootfs := createDrupalRootfs
	oldTemplateRepo := createTemplateRepo
	oldTemplateBranch := createTemplateBranch
	oldSetDefaultContext := createSetDefaultContext
	oldSetupOnly := createSetupOnly
	t.Cleanup(func() {
		createPath = oldPath
		createDrupalRootfs = oldDrupalRootfs
		createTemplateRepo = oldTemplateRepo
		createTemplateBranch = oldTemplateBranch
		createSetDefaultContext = oldSetDefaultContext
		createSetupOnly = oldSetupOnly
	})

	createPath = "/tmp/site"
	createDrupalRootfs = createpkg.DefaultDrupalRootfs
	createTemplateRepo = defaultTemplateRepo
	createTemplateBranch = defaultTemplateBranch

	cmd := newCreateCommandForTest()

	req, err := resolveCreateRequest(cmd)
	if err != nil {
		t.Fatalf("resolveCreateRequest() error = %v", err)
	}

	if promptCount != 5 {
		t.Fatalf("expected 5 prompts, got %d", promptCount)
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
	if req.Apply.IIIF != createpkg.IIIFCantaloupe {
		t.Fatalf("expected prompted iiif cantaloupe, got %q", req.Apply.IIIF)
	}
	if req.Apply.IIIFTopology != createpkg.IIIFTopologyLocal {
		t.Fatalf("expected default local iiif topology, got %q", req.Apply.IIIFTopology)
	}
	if req.Apply.BotMitigation != coretraefik.BotMitigationStateOff {
		t.Fatalf("expected prompted bot mitigation off, got %q", req.Apply.BotMitigation)
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
	oldTemplateRepo := createTemplateRepo
	oldTemplateBranch := createTemplateBranch
	oldSetDefaultContext := createSetDefaultContext
	oldSetupOnly := createSetupOnly
	t.Cleanup(func() {
		createPath = oldPath
		createDrupalRootfs = oldDrupalRootfs
		createTemplateRepo = oldTemplateRepo
		createTemplateBranch = oldTemplateBranch
		createSetDefaultContext = oldSetDefaultContext
		createSetupOnly = oldSetupOnly
	})

	createPath = "/tmp/site"
	createDrupalRootfs = createpkg.DefaultDrupalRootfs
	createTemplateRepo = defaultTemplateRepo
	createTemplateBranch = defaultTemplateBranch

	cmd := newCreateCommandForTest()
	_ = cmd.Flags().Set("context", "isle-local")
	_ = cmd.Flags().Set("fcrepo", "off")
	_ = cmd.Flags().Set("blazegraph", "on")
	_ = cmd.Flags().Set("iiif", "triplet")
	_ = cmd.Flags().Set("iiif-topology", "distributed")
	_ = cmd.Flags().Set("iiif-upstream-url", "https://iiif.example.org")
	_ = cmd.Flags().Set("codebase", "git-root")
	_ = cmd.Flags().Set("reverse-proxy", "enabled")
	_ = cmd.Flags().Set("trusted-ip", "10.0.0.0/8")
	_ = cmd.Flags().Set("trusted-ip", "203.0.113.4")
	_ = cmd.Flags().Set("upload-limits", "enabled")
	_ = cmd.Flags().Set("max-upload-size", "2G")
	_ = cmd.Flags().Set("upload-timeout", "10m")
	_ = cmd.Flags().Set("bot-mitigation", "on")
	_ = cmd.Flags().Set("homarus", "distributed")
	_ = cmd.Flags().Set("isle-file-system-uri", "public")

	req, err := resolveCreateRequest(cmd)
	if err != nil {
		t.Fatalf("resolveCreateRequest() error = %v", err)
	}

	if req.Apply.Fcrepo != "off" || req.Apply.Blazegraph != "on" || req.Apply.IIIF != createpkg.IIIFTriplet || req.Apply.IIIFTopology != createpkg.IIIFTopologyExternal || req.Apply.IIIFUpstreamURL != "https://iiif.example.org" || req.Apply.Codebase != createpkg.CodebaseGitRoot || req.Apply.BotMitigation != coretraefik.BotMitigationStateOn || req.Apply.ISLEFileSystemURI != "public" {
		t.Fatalf("unexpected options %+v", req.Apply)
	}
	if req.DrupalRootfs != "." || req.Apply.DrupalRootfs != "." {
		t.Fatalf("expected git-root codebase to use root drupal rootfs, got request=%q apply=%q", req.DrupalRootfs, req.Apply.DrupalRootfs)
	}
	if req.Apply.DerivativeServices["homarus"] != createpkg.DerivativeTopologyDistributed {
		t.Fatalf("expected homarus distributed option, got %+v", req.Apply.DerivativeServices)
	}
	if req.ReverseProxyState != corecomponent.StateOn || req.ReverseProxyTrustedIPs != "10.0.0.0/8,203.0.113.4" {
		t.Fatalf("expected reverse proxy enabled with trusted IPs, got state=%q trusted=%q", req.ReverseProxyState, req.ReverseProxyTrustedIPs)
	}
	if req.UploadLimitsState != corecomponent.StateOn || req.MaxUploadSize != "2G" || req.UploadTimeout != "10m" {
		t.Fatalf("expected upload limits enabled with overrides, got state=%q size=%q timeout=%q", req.UploadLimitsState, req.MaxUploadSize, req.UploadTimeout)
	}
}

func TestResolveCreateRequestAcceptsCustomISLEFileSystemURI(t *testing.T) {
	oldInput := createInput
	t.Cleanup(func() {
		createInput = oldInput
	})

	createInput = func(question ...string) (string, error) {
		return "", nil
	}

	oldPath := createPath
	oldDrupalRootfs := createDrupalRootfs
	oldTemplateRepo := createTemplateRepo
	oldTemplateBranch := createTemplateBranch
	oldSetDefaultContext := createSetDefaultContext
	oldSetupOnly := createSetupOnly
	t.Cleanup(func() {
		createPath = oldPath
		createDrupalRootfs = oldDrupalRootfs
		createTemplateRepo = oldTemplateRepo
		createTemplateBranch = oldTemplateBranch
		createSetDefaultContext = oldSetDefaultContext
		createSetupOnly = oldSetupOnly
	})

	createPath = "/tmp/site"
	createDrupalRootfs = createpkg.DefaultDrupalRootfs
	createTemplateRepo = defaultTemplateRepo
	createTemplateBranch = defaultTemplateBranch

	cmd := newCreateCommandForTest()
	_ = cmd.Flags().Set("context", "isle-local")
	_ = cmd.Flags().Set("fcrepo", "off")
	_ = cmd.Flags().Set("blazegraph", "on")
	_ = cmd.Flags().Set("iiif", "cantaloupe")
	_ = cmd.Flags().Set("bot-mitigation", "off")
	_ = cmd.Flags().Set("isle-file-system-uri", "archive")

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

func TestBindCreateFlagsFallsBackToLocalComponentsWhenIncludedPluginMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	plugin.InvalidateInstalledDiscoveryCache()
	t.Cleanup(plugin.InvalidateInstalledDiscoveryCache)

	oldSDK := commandSDK
	oldDrupalRootfs := createDrupalRootfs
	oldInput := createInput
	oldBindErr := createComponentBindErr
	t.Cleanup(func() {
		commandSDK = oldSDK
		createDrupalRootfs = oldDrupalRootfs
		createInput = oldInput
		createComponentBindErr = oldBindErr
	})

	createInput = func(question ...string) (string, error) {
		t.Fatal("did not expect prompt")
		return "", nil
	}
	commandSDK = plugin.NewSDK(plugin.Metadata{Name: "isle", Includes: []string{"drupal"}})
	commandSDK.RegisterComponentDefinitions(orderedComponentDefinitions()...)
	cmd := &cobra.Command{Use: "create"}
	cmd.Flags().String("context", "", "")

	var recovered any
	func() {
		defer func() {
			recovered = recover()
		}()
		bindCreateFlags(cmd)
	}()
	if recovered != nil {
		t.Fatalf("bindCreateFlags() panicked: %v", recovered)
	}
	if createComponentBindErr == nil {
		t.Fatal("expected bind-time included plugin error")
	}

	for _, name := range []string{"path", "fcrepo", "homarus", "drupal-rootfs"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Fatalf("expected fallback flag %q to be registered", name)
		}
	}

	_, err := resolveCreateRequest(cmd)
	if err == nil {
		t.Fatal("expected lazy included plugin error")
	}
	if !strings.Contains(err.Error(), "load create component definitions") || !strings.Contains(err.Error(), `plugin "drupal" is not installed`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveCreateRequestRequiresTrustedIPForReverseProxy(t *testing.T) {
	oldInput := createInput
	t.Cleanup(func() {
		createInput = oldInput
	})

	createInput = func(question ...string) (string, error) {
		return "", nil
	}

	oldPath := createPath
	oldDrupalRootfs := createDrupalRootfs
	oldTemplateRepo := createTemplateRepo
	oldTemplateBranch := createTemplateBranch
	t.Cleanup(func() {
		createPath = oldPath
		createDrupalRootfs = oldDrupalRootfs
		createTemplateRepo = oldTemplateRepo
		createTemplateBranch = oldTemplateBranch
	})

	createPath = "/tmp/site"
	createDrupalRootfs = createpkg.DefaultDrupalRootfs
	createTemplateRepo = defaultTemplateRepo
	createTemplateBranch = defaultTemplateBranch

	cmd := newCreateCommandForTest()
	_ = cmd.Flags().Set("reverse-proxy", "enabled")

	_, err := resolveCreateRequest(cmd)
	if err == nil || !strings.Contains(err.Error(), "trusted-ip is required") {
		t.Fatalf("expected trusted-ip required error, got %v", err)
	}
}

func TestResolveCreateRequestPromptsForCustomISLEFileSystemURI(t *testing.T) {
	oldInput := createInput
	t.Cleanup(func() {
		createInput = oldInput
	})

	var promptCount int
	inputs := []string{"2", "3", "archive", "1", "1", "1"}
	createInput = func(question ...string) (string, error) {
		promptCount++
		value := inputs[0]
		inputs = inputs[1:]
		return value, nil
	}

	oldPath := createPath
	oldDrupalRootfs := createDrupalRootfs
	oldTemplateRepo := createTemplateRepo
	oldTemplateBranch := createTemplateBranch
	oldSetDefaultContext := createSetDefaultContext
	oldSetupOnly := createSetupOnly
	t.Cleanup(func() {
		createPath = oldPath
		createDrupalRootfs = oldDrupalRootfs
		createTemplateRepo = oldTemplateRepo
		createTemplateBranch = oldTemplateBranch
		createSetDefaultContext = oldSetDefaultContext
		createSetupOnly = oldSetupOnly
	})

	createPath = "/tmp/site"
	createDrupalRootfs = createpkg.DefaultDrupalRootfs
	createTemplateRepo = defaultTemplateRepo
	createTemplateBranch = defaultTemplateBranch

	cmd := newCreateCommandForTest()

	req, err := resolveCreateRequest(cmd)
	if err != nil {
		t.Fatalf("resolveCreateRequest() error = %v", err)
	}

	if promptCount != 6 {
		t.Fatalf("expected 6 prompts, got %d", promptCount)
	}
	if req.Apply.ISLEFileSystemURI != "archive" {
		t.Fatalf("expected prompted custom isle-file-system-uri archive, got %q", req.Apply.ISLEFileSystemURI)
	}
}

func TestCheckPrereqsSuccess(t *testing.T) {
	oldLookPath := createLookPath
	oldRunCheck := createRunCheckCommand
	t.Cleanup(func() {
		createLookPath = oldLookPath
		createRunCheckCommand = oldRunCheck
	})

	createLookPath = func(file string) (string, error) { return "/usr/bin/" + file, nil }
	createRunCheckCommand = func(name string, args ...string) error { return nil }

	var out bytes.Buffer
	if err := checkPrereqs(&out); err != nil {
		t.Fatalf("checkPrereqs() error = %v", err)
	}

	rendered := out.String()
	rendered = stripANSI(rendered)
	if !strings.Contains(rendered, "PREREQUISITES") {
		t.Fatalf("expected prerequisites heading, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "• git is installed: ok") {
		t.Fatalf("expected git checklist item, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "bash is installed") {
		t.Fatalf("expected bash checklist item, got:\n%s", rendered)
	}
	if strings.Contains(rendered, "make is installed") {
		t.Fatalf("did not expect make prerequisite, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "docker buildx is available: ok") {
		t.Fatalf("expected buildx checklist item, got:\n%s", rendered)
	}
}

func TestCheckPrereqsDoesNotRequireMake(t *testing.T) {
	oldLookPath := createLookPath
	oldRunCheck := createRunCheckCommand
	t.Cleanup(func() {
		createLookPath = oldLookPath
		createRunCheckCommand = oldRunCheck
	})

	createLookPath = func(file string) (string, error) {
		if file == "make" {
			return "", errors.New("missing")
		}
		return "/usr/bin/" + file, nil
	}
	createRunCheckCommand = func(name string, args ...string) error { return nil }

	var out bytes.Buffer
	if err := checkPrereqs(&out); err != nil {
		t.Fatalf("checkPrereqs() error = %v", err)
	}
	rendered := stripANSI(out.String())
	if strings.Contains(rendered, "make is installed") || strings.Contains(rendered, "scripts/up.sh") {
		t.Fatalf("did not expect make fallback guidance, got:\n%s", rendered)
	}
}

func TestCheckPrereqsFailsEarly(t *testing.T) {
	oldLookPath := createLookPath
	oldRunCheck := createRunCheckCommand
	t.Cleanup(func() {
		createLookPath = oldLookPath
		createRunCheckCommand = oldRunCheck
	})

	createLookPath = func(file string) (string, error) {
		if file == "git" {
			return "", errors.New("missing")
		}
		return "/usr/bin/" + file, nil
	}
	createRunCheckCommand = func(name string, args ...string) error { return nil }

	var out bytes.Buffer
	err := checkPrereqs(&out)
	if err == nil {
		t.Fatal("expected prerequisite failure")
	}
	if !strings.Contains(err.Error(), "git is installed") {
		t.Fatalf("expected git failure in error, got %v", err)
	}
	rendered := stripANSI(out.String())
	if !strings.Contains(rendered, "• git is installed: failed") {
		t.Fatalf("expected failed checklist line, got:\n%s", rendered)
	}
}

func TestStartupCommandUsesCreateDefinitionLifecycle(t *testing.T) {
	label, name, args := startupCommand()
	if label != "ISLE startup commands" {
		t.Fatalf("expected ISLE startup label, got %q", label)
	}
	if name != "bash" {
		t.Fatalf("expected bash command, got %q", name)
	}
	if len(args) != 2 || args[0] != "-lc" {
		t.Fatalf("expected bash lifecycle args, got %#v", args)
	}
	for _, want := range []string{
		"docker compose pull --ignore-buildable",
		"docker compose build",
		"docker compose up --remove-orphans -d",
	} {
		if !strings.Contains(args[1], want) {
			t.Fatalf("expected startup command to contain %q, got %q", want, args[1])
		}
	}
}

func TestEnsureClonedCheckoutClonesEmptyDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	projectDir := filepath.Join(tmpDir, "site")

	oldClone := createCloneTemplateRepo
	t.Cleanup(func() {
		createCloneTemplateRepo = oldClone
	})

	var cloneInvoked bool
	createCloneTemplateRepo = func(opts plugin.GitTemplateOptions) error {
		cloneInvoked = true
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

	cloned, err := ensureClonedCheckout(io.Discard, createRequest{ComposeCreateRequest: plugin.ComposeCreateRequest{
		TemplateRepo:   defaultTemplateRepo,
		TemplateBranch: defaultTemplateBranch,
		Path:           projectDir,
	}})
	if err != nil {
		t.Fatalf("ensureClonedCheckout() error = %v", err)
	}
	if !cloned {
		t.Fatal("expected checkout to be reported as cloned")
	}
	if !cloneInvoked {
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

	cloned, err := ensureClonedCheckout(io.Discard, createRequest{ComposeCreateRequest: plugin.ComposeCreateRequest{
		TemplateRepo:   defaultTemplateRepo,
		TemplateBranch: defaultTemplateBranch,
		Path:           projectDir,
	}})
	if err != nil {
		t.Fatalf("ensureClonedCheckout() error = %v", err)
	}
	if cloned {
		t.Fatal("did not expect checkout to be reported as cloned")
	}
}

func TestRunCreateCommandRunsMakeUpAndPrintsCommitSuggestion(t *testing.T) {
	oldSDK := commandSDK
	oldEnsure := createEnsureLocalContext
	oldClone := createCloneTemplateRepo
	oldApply := createApply
	oldBootstrap := createBootstrapCheckout
	oldRunStartup := createRunStartup
	oldSleep := createSleep
	t.Cleanup(func() {
		commandSDK = oldSDK
		createEnsureLocalContext = oldEnsure
		createCloneTemplateRepo = oldClone
		createApply = oldApply
		createBootstrapCheckout = oldBootstrap
		createRunStartup = oldRunStartup
		createSleep = oldSleep
	})

	createSleep = func(time.Duration) {}
	commandSDK = &plugin.SDK{}
	projectDir := filepath.Join(t.TempDir(), "site")
	createEnsureLocalContext = func(_ *plugin.SDK, req createRequest) (*config.Context, error) {
		return &config.Context{Name: "isle-local-2", ProjectDir: projectDir}, nil
	}
	createCloneTemplateRepo = func(opts plugin.GitTemplateOptions) error {
		return os.MkdirAll(opts.ProjectDir, 0o755)
	}
	createApply = func(opts createpkg.Options) error { return nil }
	var bootstrapped bool
	createBootstrapCheckout = func(_ io.Writer, gotProjectDir string) error {
		bootstrapped = true
		if gotProjectDir != projectDir {
			t.Fatalf("expected bootstrap in %q, got %q", projectDir, gotProjectDir)
		}
		return nil
	}

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
		ComposeCreateRequest: plugin.ComposeCreateRequest{
			Path:           projectDir,
			DrupalRootfs:   createpkg.DefaultDrupalRootfs,
			TemplateRepo:   defaultTemplateRepo,
			TemplateBranch: defaultTemplateBranch,
		},
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
	if !bootstrapped {
		t.Fatal("expected bootstrap to run for a fresh clone")
	}

	rendered := out.String()
	if !strings.Contains(rendered, "cd '") {
		t.Fatalf("expected cd line in commit suggestion, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "git add .") || !strings.Contains(rendered, "git commit -m") {
		t.Fatalf("expected commit suggestion, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "sitectl create isle") {
		t.Fatalf("expected recreate command body, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, `--type="local"`) || !strings.Contains(rendered, `--checkout-source="template"`) {
		t.Fatalf("expected recreate command with non-interactive create flags, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "--fcrepo=on") || !strings.Contains(rendered, "--blazegraph=off") {
		t.Fatalf("expected recreate command with component flags, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "git remote add origin git@github.com:your-org/your-repo.git") {
		t.Fatalf("expected git remote setup guidance, got:\n%s", rendered)
	}
}

func TestRunCreateCommandWritesProgressToStderrDuringRPC(t *testing.T) {
	t.Setenv("SITECTL_RPC", "1")

	oldSDK := commandSDK
	oldEnsure := createEnsureLocalContext
	oldClone := createCloneTemplateRepo
	oldApply := createApply
	oldBootstrap := createBootstrapCheckout
	oldRunStartup := createRunStartup
	oldSleep := createSleep
	t.Cleanup(func() {
		commandSDK = oldSDK
		createEnsureLocalContext = oldEnsure
		createCloneTemplateRepo = oldClone
		createApply = oldApply
		createBootstrapCheckout = oldBootstrap
		createRunStartup = oldRunStartup
		createSleep = oldSleep
	})

	createSleep = func(time.Duration) {}
	commandSDK = &plugin.SDK{}
	projectDir := filepath.Join(t.TempDir(), "site")
	createEnsureLocalContext = func(_ *plugin.SDK, req createRequest) (*config.Context, error) {
		return &config.Context{Name: "isle-local", ProjectDir: projectDir}, nil
	}
	createCloneTemplateRepo = func(opts plugin.GitTemplateOptions) error {
		return os.MkdirAll(opts.ProjectDir, 0o755)
	}
	createApply = func(opts createpkg.Options) error { return nil }
	createBootstrapCheckout = func(out io.Writer, gotProjectDir string) error {
		if gotProjectDir != projectDir {
			t.Fatalf("expected bootstrap in %q, got %q", projectDir, gotProjectDir)
		}
		_, err := fmt.Fprintln(out, "bootstrap progress")
		return err
	}
	createRunStartup = func(_ io.Writer, ctx *config.Context) error {
		t.Fatal("did not expect startup to run")
		return nil
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd := &cobra.Command{Use: "create"}
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	err := runCreateCommand(cmd, createRequest{
		ComposeCreateRequest: plugin.ComposeCreateRequest{
			Path:           projectDir,
			DrupalRootfs:   createpkg.DefaultDrupalRootfs,
			TemplateRepo:   defaultTemplateRepo,
			TemplateBranch: defaultTemplateBranch,
			SetupOnly:      true,
		},
		Apply: createpkg.Options{
			DrupalRootfs:      createpkg.DefaultDrupalRootfs,
			Fcrepo:            createpkg.FcrepoStateOff,
			Blazegraph:        createpkg.FcrepoStateOff,
			ISLEFileSystemURI: createpkg.PrivateISLEFileSystemURI,
		},
	})
	if err != nil {
		t.Fatalf("runCreateCommand() error = %v", err)
	}

	progress := stripANSI(stderr.String())
	for _, want := range []string{
		"Preparing the sitectl context",
		"TEMPLATE CHECKOUT",
		"bootstrap progress",
		"TEMPLATE CONFIGURATION",
		"Applying ISLE options... done",
	} {
		if !strings.Contains(progress, want) {
			t.Fatalf("expected stderr progress to contain %q, got:\n%s", want, progress)
		}
	}

	summary := stripANSI(stdout.String())
	if !strings.Contains(summary, "CREATE COMPLETE") {
		t.Fatalf("expected stdout summary, got:\n%s", summary)
	}
	if strings.Contains(summary, "Template configuration") {
		t.Fatalf("expected progress to stay out of stdout summary, got:\n%s", summary)
	}
}

func TestPrintCreateFailureSummaryUsesPlainReplayCommand(t *testing.T) {
	var out bytes.Buffer
	req := createRequest{
		ComposeCreateRequest: plugin.ComposeCreateRequest{
			ContextName:    "foobar",
			TargetType:     config.ContextLocal,
			CheckoutSource: plugin.CheckoutSourceTemplate,
			Path:           "/Users/jjc223/foobar",
			DrupalRootfs:   createpkg.DefaultDrupalRootfs,
			TemplateRepo:   defaultTemplateRepo,
			TemplateBranch: defaultTemplateBranch,
		},
		Apply: createpkg.Options{
			DrupalRootfs:      createpkg.DefaultDrupalRootfs,
			Fcrepo:            createpkg.FcrepoStateOff,
			Blazegraph:        createpkg.FcrepoStateOff,
			IIIF:              createpkg.IIIFTriplet,
			IIIFTopology:      createpkg.IIIFTopologyLocal,
			BotMitigation:     coretraefik.BotMitigationStateOn,
			ISLEFileSystemURI: createpkg.PrivateISLEFileSystemURI,
		},
	}

	printCreateFailureSummary(&out, req)

	rendered := out.String()
	for _, want := range []string{
		"sitectl create isle \\",
		`--type="local"`,
		`--checkout-source="template"`,
		`--context="foobar"`,
		`--bot-mitigation=on`,
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("expected failure summary to contain %q, got:\n%s", want, rendered)
		}
	}
	if strings.Contains(rendered, "│") {
		t.Fatalf("expected plain replay command without box borders, got:\n%s", rendered)
	}
}

func TestRunCreateCommandSkipsMakeUpWhenSetupOnly(t *testing.T) {
	oldSDK := commandSDK
	oldEnsure := createEnsureLocalContext
	oldClone := createCloneTemplateRepo
	oldApply := createApply
	oldBootstrap := createBootstrapCheckout
	oldRunStartup := createRunStartup
	oldSleep := createSleep
	t.Cleanup(func() {
		commandSDK = oldSDK
		createEnsureLocalContext = oldEnsure
		createCloneTemplateRepo = oldClone
		createApply = oldApply
		createBootstrapCheckout = oldBootstrap
		createRunStartup = oldRunStartup
		createSleep = oldSleep
	})

	createSleep = func(time.Duration) {}
	commandSDK = &plugin.SDK{}
	projectDir := filepath.Join(t.TempDir(), "site")
	createEnsureLocalContext = func(_ *plugin.SDK, req createRequest) (*config.Context, error) {
		return &config.Context{Name: "isle-local", ProjectDir: projectDir}, nil
	}
	createCloneTemplateRepo = func(opts plugin.GitTemplateOptions) error {
		return os.MkdirAll(opts.ProjectDir, 0o755)
	}
	createApply = func(opts createpkg.Options) error { return nil }
	var bootstrapped bool
	createBootstrapCheckout = func(_ io.Writer, gotProjectDir string) error {
		bootstrapped = true
		if gotProjectDir != projectDir {
			t.Fatalf("expected bootstrap in %q, got %q", projectDir, gotProjectDir)
		}
		return nil
	}
	createRunStartup = func(_ io.Writer, ctx *config.Context) error {
		t.Fatal("did not expect startup to run")
		return nil
	}

	var out bytes.Buffer
	cmd := &cobra.Command{Use: "create"}
	cmd.SetOut(&out)

	err := runCreateCommand(cmd, createRequest{
		ComposeCreateRequest: plugin.ComposeCreateRequest{
			Path:           projectDir,
			DrupalRootfs:   createpkg.DefaultDrupalRootfs,
			TemplateRepo:   defaultTemplateRepo,
			TemplateBranch: defaultTemplateBranch,
			SetupOnly:      true,
		},
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
	if !bootstrapped {
		t.Fatal("expected bootstrap to run for a fresh clone")
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

func TestBootstrapCheckoutRunsGitAddAndInitialCommit(t *testing.T) {
	projectDir := t.TempDir()

	oldRunProject := createRunProjectCommand
	oldSleep := createSleep
	t.Cleanup(func() {
		createRunProjectCommand = oldRunProject
		createSleep = oldSleep
	})

	createSleep = func(time.Duration) {}

	var commands [][]string
	createRunProjectCommand = func(gotProjectDir string, stdout, stderr io.Writer, name string, args ...string) error {
		if gotProjectDir != projectDir {
			t.Fatalf("expected project dir %q, got %q", projectDir, gotProjectDir)
		}
		if name != "git" {
			t.Fatalf("expected git command, got %q", name)
		}
		commands = append(commands, append([]string{name}, args...))
		return nil
	}

	if err := bootstrapCheckout(io.Discard, projectDir); err != nil {
		t.Fatalf("bootstrapCheckout() error = %v", err)
	}

	if len(commands) != 2 {
		t.Fatalf("expected 2 git commands, got %#v", commands)
	}
	if got, want := strings.Join(commands[0], " "), fmt.Sprintf("git -c safe.directory=%s add .", projectDir); got != want {
		t.Fatalf("expected first command `git add .`, got %q", got)
	}
	if got, want := strings.Join(commands[1], " "), fmt.Sprintf("git -c safe.directory=%s -c user.name=sitectl-isle -c user.email=sitectl-isle@localhost commit -m initial commit.", projectDir); got != want {
		t.Fatalf("unexpected commit command %q", got)
	}
}

func newCreateCommandForTest() *cobra.Command {
	commandSDK = plugin.NewSDK(plugin.Metadata{Name: "isle"})
	commandSDK.RegisterComponentDefinitions(orderedComponentDefinitions()...)
	cmd := &cobra.Command{Use: "create"}
	cmd.Flags().String("context", "", "")
	if err := commandSDK.BindComposeCreateFlags(cmd, createDefinition(), &createDrupalRootfs, createpkg.DefaultDrupalRootfs); err != nil {
		panic(err)
	}
	_ = cmd.Flags().Set("type", "local")
	_ = cmd.Flags().Set("checkout-source", "template")
	return cmd
}

var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSI(value string) string {
	return ansiPattern.ReplaceAllString(value, "")
}

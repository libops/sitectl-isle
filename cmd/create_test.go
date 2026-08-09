package cmd

import (
	"bytes"
	"context"
	"encoding/json"
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
	t.Setenv("HOME", t.TempDir())
	oldInput := createInput
	t.Cleanup(func() {
		createInput = oldInput
	})

	var componentPromptCount int
	createInput = func(question ...string) (string, error) {
		prompt := strings.ToLower(stripANSI(strings.Join(question, "\n")))
		switch {
		case strings.Contains(prompt, "drupal filesystem uri"):
			componentPromptCount++
			return "public", nil
		case strings.Contains(prompt, "fcrepo"):
			componentPromptCount++
			return "superseded", nil
		case strings.Contains(prompt, "blazegraph"):
			componentPromptCount++
			return "enabled", nil
		case strings.Contains(prompt, "iiif topology"), strings.Contains(prompt, "iiif-topology"):
			return "", nil
		case strings.Contains(prompt, "iiif"):
			componentPromptCount++
			return "cantaloupe", nil
		case strings.Contains(prompt, "bot mitigation"), strings.Contains(prompt, "bot-mitigation"):
			componentPromptCount++
			return "disabled", nil
		default:
			return "", nil
		}
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

	if componentPromptCount != 5 {
		t.Fatalf("expected 5 component prompts, got %d", componentPromptCount)
	}
	if req.ContextName != "site" {
		t.Fatalf("expected context name derived from the reviewed path, got %q", req.ContextName)
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
	if req.Apply.FeatureBundles[createpkg.FeatureBundleMergePDF] != string(corecomponent.StateOn) || req.Apply.FeatureBundles[createpkg.FeatureBundleHOCRSearch] != string(corecomponent.StateOn) {
		t.Fatalf("expected current-template feature defaults enabled, got %+v", req.Apply.FeatureBundles)
	}
	if got := req.Apply.FeatureBundleOptions[createpkg.FeatureBundleHOCRSearch][createpkg.HOCRStructuredTextTermOption]; got != "56" {
		t.Fatalf("expected default hOCR term ID 56, got %q", got)
	}
	if req.TemplateRepo != defaultTemplateRepo {
		t.Fatalf("expected template repo %q, got %q", defaultTemplateRepo, req.TemplateRepo)
	}
	if req.TemplateBranch != defaultTemplateBranch {
		t.Fatalf("expected template branch %q, got %q", defaultTemplateBranch, req.TemplateBranch)
	}
}

func TestResolveCreateRequestReviewsExplicitFlagsUsingThemAsDefaults(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	oldInput := createInput
	t.Cleanup(func() {
		createInput = oldInput
	})

	var promptCount int
	createInput = func(question ...string) (string, error) {
		promptCount++
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
	_ = cmd.Flags().Set("ingress", "enabled")
	_ = cmd.Flags().Set("mode", coretraefik.IngressModeHTTPSCustom)
	_ = cmd.Flags().Set("domain", "repo.example.org")
	_ = cmd.Flags().Set("trusted-ip", "10.0.0.0/8")
	_ = cmd.Flags().Set("trusted-ip", "203.0.113.4")
	_ = cmd.Flags().Set("max-upload-size", "2G")
	_ = cmd.Flags().Set("upload-timeout", "10m")
	_ = cmd.Flags().Set("bot-mitigation", "on")
	_ = cmd.Flags().Set("homarus", "distributed")
	_ = cmd.Flags().Set("mergepdf", "disabled")
	_ = cmd.Flags().Set("hocr-search", "enabled")
	_ = cmd.Flags().Set("hocr-term-id", "77")
	_ = cmd.Flags().Set("isle-file-system-uri", "public")

	req, err := resolveCreateRequest(cmd)
	if err != nil {
		t.Fatalf("resolveCreateRequest() error = %v", err)
	}
	if promptCount == 0 {
		t.Fatal("expected explicit flags to be reviewed")
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
	if req.Apply.FeatureBundles[createpkg.FeatureBundleMergePDF] != string(corecomponent.StateOff) || req.Apply.FeatureBundles[createpkg.FeatureBundleHOCRSearch] != string(corecomponent.StateOn) {
		t.Fatalf("unexpected feature-bundle options: %+v", req.Apply.FeatureBundles)
	}
	if got := req.Apply.FeatureBundleOptions[createpkg.FeatureBundleHOCRSearch][createpkg.HOCRStructuredTextTermOption]; got != "77" {
		t.Fatalf("expected explicit hOCR term ID 77, got %q", got)
	}
	if got := req.Apply.FeatureBundleOptions[createpkg.FeatureBundleMergePDF]; len(got) != 0 {
		t.Fatalf("disabled mergepdf should not retain an enabled-only tag option: %+v", req.Apply.FeatureBundleOptions)
	}
	if req.IngressState != "on" || req.IngressMode != coretraefik.IngressModeHTTPSCustom || req.IngressDomain != "repo.example.org" || req.IngressTrustedIPs != "10.0.0.0/8,203.0.113.4" {
		t.Fatalf("expected ingress enabled with overrides, got state=%q mode=%q domain=%q trusted=%q", req.IngressState, req.IngressMode, req.IngressDomain, req.IngressTrustedIPs)
	}
	if req.MaxUploadSize != "2G" || req.UploadTimeout != "10m" {
		t.Fatalf("expected upload limit overrides, got size=%q timeout=%q", req.MaxUploadSize, req.UploadTimeout)
	}
}

func TestResolveCreateRequestAcceptsCustomISLEFileSystemURI(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
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
	if req.Apply.DrupalRootfs != corecomponent.DefaultDrupalRootfs {
		t.Fatalf("expected LibOps git-root Drupal path, got %q", req.Apply.DrupalRootfs)
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

func TestResolveCreateRequestPromptsForCustomISLEFileSystemURI(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	oldInput := createInput
	t.Cleanup(func() {
		createInput = oldInput
	})

	var customPrompted bool
	createInput = func(question ...string) (string, error) {
		prompt := strings.ToLower(stripANSI(strings.Join(question, "\n")))
		switch {
		case strings.Contains(prompt, "custom uri scheme"):
			customPrompted = true
			return "archive", nil
		case strings.Contains(prompt, "drupal filesystem uri"):
			return "custom", nil
		case strings.Contains(prompt, "fcrepo"):
			return "superseded", nil
		default:
			return "", nil
		}
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

	if !customPrompted {
		t.Fatal("expected the custom filesystem URI prompt")
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

func TestCreateRunnerRejectsRemoteBeforeLocalPrerequisites(t *testing.T) {
	oldResolve := createResolveRequest
	oldPrereqs := createCheckPrereqs
	t.Cleanup(func() {
		createResolveRequest = oldResolve
		createCheckPrereqs = oldPrereqs
	})

	createResolveRequest = func(*cobra.Command) (createRequest, error) {
		return createRequest{ComposeCreateRequest: plugin.ComposeCreateRequest{TargetType: config.ContextRemote}}, nil
	}
	createCheckPrereqs = func(io.Writer) error {
		t.Fatal("remote create ran local Docker/buildx prerequisites")
		return nil
	}

	cmd := &cobra.Command{Use: "create", Short: "Create ISLE"}
	cmd.SetOut(io.Discard)
	err := (createRunner{}).Run(cmd)
	if err == nil || !strings.Contains(err.Error(), "Cloud Compose") || !strings.Contains(err.Error(), "fenced remote customization hooks") {
		t.Fatalf("createRunner.Run() error = %v, want remote provisioning guidance", err)
	}
}

func TestRunCreateCommandRejectsRemoteBeforeContextMutation(t *testing.T) {
	oldEnsure := createEnsureLocalContext
	t.Cleanup(func() { createEnsureLocalContext = oldEnsure })
	createEnsureLocalContext = func(*plugin.SDK, createRequest) (*config.Context, error) {
		t.Fatal("remote create attempted to create or update a sitectl context")
		return nil, nil
	}

	cmd := &cobra.Command{Use: "create"}
	cmd.SetOut(io.Discard)
	err := runCreateCommand(cmd, createRequest{ComposeCreateRequest: plugin.ComposeCreateRequest{TargetType: config.ContextRemote}})
	if err == nil || !strings.Contains(err.Error(), "remote ISLE create is not supported") {
		t.Fatalf("runCreateCommand() error = %v, want remote create rejection", err)
	}
}

func writeCurrentExistingISLECheckout(t *testing.T, projectDir string) {
	t.Helper()
	files := map[string]string{
		"compose.yaml":                            "services:\n  alpaca: {}\n  drupal:\n    volumes:\n      - ./scripts/drupal-media-storage-state.php:/var/www/drupal/drupal-media-storage-state.php:ro\n      - ./scripts/drupal-wait-installed.sh:/usr/local/lib/sitectl/drupal-wait-installed.sh:ro\n  init:\n    volumes:\n      - ./compose.yaml:/work/compose.yaml:ro\n      - ./scripts/ensure-islandora-jwt-keypair.sh:/usr/local/lib/sitectl/ensure-islandora-jwt-keypair.sh:ro\n      - ./scripts/initialize-compose.sh:/usr/local/lib/sitectl/initialize-compose.sh:ro\n",
		".libops/template-contract.yaml":          "apiVersion: sitectl.libops.io/v1alpha1\nkind: TemplateContract\nschema: 1\nspec:\n  componentDefaults:\n    revision: v1.0.0\n",
		"conf/triplet/config.yaml":                "services: {}\n",
		"scripts/drupal-media-storage-state.php":  "<?php\n",
		"scripts/drupal-wait-installed.sh":        "#!/usr/bin/env bash\n",
		"scripts/ensure-islandora-jwt-keypair.sh": "#!/usr/bin/env bash\n",
		"scripts/initialize-compose.sh":           "#!/usr/bin/env bash\n",
		"scripts/sitectl-prepare-build.sh":        "#!/usr/bin/env bash\n",
		"scripts/sitectl-prepare-init.sh":         "#!/usr/bin/env bash\n",
		"scripts/sitectl-rollout-preflight.sh":    "#!/usr/bin/env bash\n",
	}
	for name, contents := range files {
		filename := filepath.Join(projectDir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(filename), 0o750); err != nil {
			t.Fatalf("MkdirAll(%s) error = %v", filepath.Dir(filename), err)
		}
		if err := os.WriteFile(filename, []byte(contents), 0o640); err != nil {
			t.Fatalf("WriteFile(%s) error = %v", filename, err)
		}
	}
}

func currentExistingISLEComposeModel(projectDir string) existingISLEComposeModel {
	readOnlyBind := func(source, target string) existingISLEComposeVolume {
		return existingISLEComposeVolume{
			Type:     "bind",
			Source:   filepath.Join(projectDir, filepath.FromSlash(source)),
			Target:   target,
			ReadOnly: true,
		}
	}
	return existingISLEComposeModel{Services: map[string]existingISLEComposeService{
		"alpaca": {},
		"drupal": {Volumes: []existingISLEComposeVolume{
			readOnlyBind("scripts/drupal-media-storage-state.php", "/var/www/drupal/drupal-media-storage-state.php"),
			readOnlyBind("scripts/drupal-wait-installed.sh", "/usr/local/lib/sitectl/drupal-wait-installed.sh"),
		}},
		"init": {Volumes: []existingISLEComposeVolume{
			readOnlyBind("compose.yaml", "/work/compose.yaml"),
			readOnlyBind("scripts/ensure-islandora-jwt-keypair.sh", "/usr/local/lib/sitectl/ensure-islandora-jwt-keypair.sh"),
			readOnlyBind("scripts/initialize-compose.sh", "/usr/local/lib/sitectl/initialize-compose.sh"),
		}},
	}}
}

func existingISLEComposeModelJSON(t *testing.T, model existingISLEComposeModel) string {
	t.Helper()
	data, err := json.Marshal(model)
	if err != nil {
		t.Fatalf("Marshal(existing ISLE Compose model) error = %v", err)
	}
	return string(data)
}

func TestValidateExistingISLECheckoutRejectsUnrelatedComposeProject(t *testing.T) {
	projectDir := t.TempDir()
	writeCurrentExistingISLECheckout(t, projectDir)
	oldRunArgv := createRunComposeArgv
	t.Cleanup(func() { createRunComposeArgv = oldRunArgv })
	createRunComposeArgv = composeConfigRunnerForTest(t, config.ContextLocal, `{"services":{"wordpress":{}}}`)
	ctx := &config.Context{ProjectDir: projectDir, DockerHostType: config.ContextLocal}
	err := validateExistingISLECheckout(context.Background(), ctx)
	if err == nil || !strings.Contains(err.Error(), "not an existing ISLE checkout") {
		t.Fatalf("validateExistingISLECheckout() error = %v, want existing-checkout rejection", err)
	}
}

func TestValidateExistingISLECheckoutAcceptsISLEComposeProject(t *testing.T) {
	projectDir := t.TempDir()
	writeCurrentExistingISLECheckout(t, projectDir)
	oldRunArgv := createRunComposeArgv
	t.Cleanup(func() { createRunComposeArgv = oldRunArgv })
	createRunComposeArgv = composeConfigRunnerForTest(t, config.ContextLocal, existingISLEComposeModelJSON(t, currentExistingISLEComposeModel(projectDir)))
	ctx := &config.Context{ProjectDir: projectDir, DockerHostType: config.ContextLocal}
	err := validateExistingISLECheckout(context.Background(), ctx)
	if err != nil {
		t.Fatalf("validateExistingISLECheckout() error = %v", err)
	}
}

func TestValidateExistingISLECheckoutRejectsMissingInitService(t *testing.T) {
	projectDir := t.TempDir()
	writeCurrentExistingISLECheckout(t, projectDir)
	model := currentExistingISLEComposeModel(projectDir)
	delete(model.Services, "init")
	oldRunArgv := createRunComposeArgv
	t.Cleanup(func() { createRunComposeArgv = oldRunArgv })
	createRunComposeArgv = composeConfigRunnerForTest(t, config.ContextLocal, existingISLEComposeModelJSON(t, model))
	err := validateExistingISLECheckout(context.Background(), &config.Context{ProjectDir: projectDir, DockerHostType: config.ContextLocal})
	if err == nil || !strings.Contains(err.Error(), "init") || !strings.Contains(err.Error(), "required") {
		t.Fatalf("validateExistingISLECheckout() error = %v, want init-service rejection", err)
	}
}

func TestValidateExistingISLECheckoutRejectsWritableLifecycleBind(t *testing.T) {
	projectDir := t.TempDir()
	writeCurrentExistingISLECheckout(t, projectDir)
	model := currentExistingISLEComposeModel(projectDir)
	drupal := model.Services["drupal"]
	drupal.Volumes[1].ReadOnly = false
	model.Services["drupal"] = drupal
	oldRunArgv := createRunComposeArgv
	t.Cleanup(func() { createRunComposeArgv = oldRunArgv })
	createRunComposeArgv = composeConfigRunnerForTest(t, config.ContextLocal, existingISLEComposeModelJSON(t, model))
	err := validateExistingISLECheckout(context.Background(), &config.Context{ProjectDir: projectDir, DockerHostType: config.ContextLocal})
	if err == nil || !strings.Contains(err.Error(), "drupal-wait-installed.sh") || !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("validateExistingISLECheckout() error = %v, want read-only lifecycle-bind rejection", err)
	}
}

func TestValidateExistingISLECheckoutRejectsLegacyComposeBeforeInspection(t *testing.T) {
	projectDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectDir, "docker-compose.yml"), []byte("services:\n  drupal: {}\n  alpaca: {}\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	oldRunArgv := createRunComposeArgv
	t.Cleanup(func() { createRunComposeArgv = oldRunArgv })
	createRunComposeArgv = func(context.Context, *config.Context, string, io.Writer, io.Writer, []string) error {
		t.Fatal("legacy checkout reached Docker Compose inspection")
		return nil
	}
	err := validateExistingISLECheckout(context.Background(), &config.Context{ProjectDir: projectDir, DockerHostType: config.ContextLocal})
	if err == nil || !strings.Contains(err.Error(), "legacy Compose file") || !strings.Contains(err.Error(), "will not rename") {
		t.Fatalf("validateExistingISLECheckout() error = %v, want explicit legacy migration guidance", err)
	}
	if _, statErr := os.Stat(filepath.Join(projectDir, "docker-compose.yml")); statErr != nil {
		t.Fatalf("legacy checkout was mutated during rejection: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(projectDir, "compose.yaml")); !os.IsNotExist(statErr) {
		t.Fatalf("legacy checkout was silently normalized: %v", statErr)
	}
}

func TestValidateExistingISLECheckoutRejectsMissingLifecycleContractBeforeInspection(t *testing.T) {
	projectDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectDir, "compose.yaml"), []byte("services:\n  drupal: {}\n  alpaca: {}\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	oldRunArgv := createRunComposeArgv
	t.Cleanup(func() { createRunComposeArgv = oldRunArgv })
	createRunComposeArgv = func(context.Context, *config.Context, string, io.Writer, io.Writer, []string) error {
		t.Fatal("incomplete checkout reached Docker Compose inspection")
		return nil
	}
	err := validateExistingISLECheckout(context.Background(), &config.Context{ProjectDir: projectDir, DockerHostType: config.ContextLocal})
	if err == nil || !strings.Contains(err.Error(), existingISLETemplateContractPath) || !strings.Contains(err.Error(), "v1.3.1") {
		t.Fatalf("validateExistingISLECheckout() error = %v, want lifecycle-contract migration guidance", err)
	}
}

func TestValidateExistingISLECheckoutRejectsUnsupportedTemplateContractBeforeInspection(t *testing.T) {
	projectDir := t.TempDir()
	writeCurrentExistingISLECheckout(t, projectDir)
	contractPath := filepath.Join(projectDir, filepath.FromSlash(existingISLETemplateContractPath))
	if err := os.WriteFile(contractPath, []byte("apiVersion: sitectl.libops.io/v1alpha1\nkind: TemplateContract\nschema: 1\nspec:\n  componentDefaults:\n    revision: legacy\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	oldRunArgv := createRunComposeArgv
	t.Cleanup(func() { createRunComposeArgv = oldRunArgv })
	createRunComposeArgv = func(context.Context, *config.Context, string, io.Writer, io.Writer, []string) error {
		t.Fatal("unsupported contract reached Docker Compose inspection")
		return nil
	}
	err := validateExistingISLECheckout(context.Background(), &config.Context{ProjectDir: projectDir, DockerHostType: config.ContextLocal})
	if err == nil || !strings.Contains(err.Error(), "unsupported") || !strings.Contains(err.Error(), "v1.3.1") {
		t.Fatalf("validateExistingISLECheckout() error = %v, want contract migration guidance", err)
	}
}

func TestValidateExistingISLECheckoutRejectsOversizedEffectiveComposeModel(t *testing.T) {
	projectDir := t.TempDir()
	writeCurrentExistingISLECheckout(t, projectDir)
	oldRunArgv := createRunComposeArgv
	t.Cleanup(func() { createRunComposeArgv = oldRunArgv })
	createRunComposeArgv = composeConfigRunnerForTest(t, config.ContextLocal, `{"services":{"drupal":{},"alpaca":{}},"padding":"0123456789"}`)
	ctx := &config.Context{ProjectDir: projectDir, DockerHostType: config.ContextLocal}
	err := validateExistingISLECheckoutLimit(context.Background(), ctx, 32)
	if err == nil || !strings.Contains(err.Error(), "exceeds 32 bytes") {
		t.Fatalf("validateExistingISLECheckoutLimit() error = %v, want bounded-output rejection", err)
	}
}

func TestValidateExistingISLECheckoutUsesEffectiveComposeServices(t *testing.T) {
	projectDir := t.TempDir()
	writeCurrentExistingISLECheckout(t, projectDir)
	oldRunArgv := createRunComposeArgv
	t.Cleanup(func() { createRunComposeArgv = oldRunArgv })
	model := currentExistingISLEComposeModel(projectDir)
	model.Services["mariadb"] = existingISLEComposeService{}
	createRunComposeArgv = composeConfigRunnerForTest(t, config.ContextLocal, existingISLEComposeModelJSON(t, model))
	ctx := &config.Context{DockerHostType: config.ContextLocal, ProjectDir: projectDir}
	if err := validateExistingISLECheckout(context.Background(), ctx); err != nil {
		t.Fatalf("validateExistingISLECheckout() error = %v", err)
	}
}

func TestRunCreateCommandAcceptsCanonicalNonGitExistingCheckoutWithoutNormalization(t *testing.T) {
	oldSDK := commandSDK
	oldEnsure := createEnsureLocalContext
	oldNormalize := createNormalizeCheckout
	oldBootstrapForContext := createBootstrapForContext
	oldApply := createApply
	oldEnsureJWTKeyPair := createEnsureJWTKeyPair
	oldAcquireProjectLock := createAcquireProjectLock
	oldRunArgv := createRunComposeArgv
	oldSleep := createSleep
	t.Cleanup(func() {
		commandSDK = oldSDK
		createEnsureLocalContext = oldEnsure
		createNormalizeCheckout = oldNormalize
		createBootstrapForContext = oldBootstrapForContext
		createApply = oldApply
		createEnsureJWTKeyPair = oldEnsureJWTKeyPair
		createAcquireProjectLock = oldAcquireProjectLock
		createRunComposeArgv = oldRunArgv
		createSleep = oldSleep
	})

	commandSDK = &plugin.SDK{}
	createSleep = func(time.Duration) {}
	projectDir := t.TempDir()
	writeCurrentExistingISLECheckout(t, projectDir)
	canonicalCompose := filepath.Join(projectDir, "compose.yaml")
	before, err := os.ReadFile(canonicalCompose)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(projectDir, ".git")); !os.IsNotExist(err) {
		t.Fatalf("test checkout unexpectedly has Git metadata: %v", err)
	}
	ctx := &config.Context{
		Name:           "isle-existing",
		Plugin:         "isle",
		DockerHostType: config.ContextLocal,
		ProjectDir:     projectDir,
		ComposeFile:    []string{"compose.yaml"},
	}
	createEnsureLocalContext = func(_ *plugin.SDK, _ createRequest) (*config.Context, error) {
		return ctx, nil
	}
	stubComposeCreateTargetLifecycle(t, projectDir, false, nil)
	createRunComposeArgv = composeConfigRunnerForTest(t, config.ContextLocal, existingISLEComposeModelJSON(t, currentExistingISLEComposeModel(projectDir)))
	createAcquireProjectLock = func(runCtx context.Context, got *config.Context) (*config.ProjectMutationLock, error) {
		return got.AcquireProjectMutationLock(runCtx)
	}

	createNormalizeCheckout = func(context.Context, *config.Context) error {
		t.Fatal("explicit existing checkout was sent through filename normalization")
		return nil
	}
	createBootstrapForContext = func(context.Context, io.Writer, *config.Context) error {
		t.Fatal("explicit existing checkout was sent through Git bootstrap")
		return nil
	}
	createApply = func(context.Context, createpkg.Options) error { return nil }
	reachedConfiguration := errors.New("reached existing-checkout configuration")
	createEnsureJWTKeyPair = func(context.Context, *config.Context) error {
		return reachedConfiguration
	}

	cmd := &cobra.Command{Use: "create"}
	cmd.SetOut(io.Discard)
	err = runCreateCommand(cmd, createRequest{
		ComposeCreateRequest: plugin.ComposeCreateRequest{
			TargetType:     config.ContextLocal,
			CheckoutSource: plugin.CheckoutSourceExisting,
			Path:           projectDir,
			SetupOnly:      true,
		},
		Apply: createpkg.Options{DrupalRootfs: createpkg.DefaultDrupalRootfs},
	})
	if !errors.Is(err, reachedConfiguration) {
		t.Fatalf("runCreateCommand() error = %v, want existing checkout to reach configuration", err)
	}
	after, readErr := os.ReadFile(canonicalCompose)
	if readErr != nil {
		t.Fatalf("read admitted Compose file: %v", readErr)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("existing Compose file changed during admission:\nbefore: %s\nafter: %s", before, after)
	}
	if _, err := os.Lstat(filepath.Join(projectDir, ".git")); !os.IsNotExist(err) {
		t.Fatalf("explicit existing checkout gained Git metadata: %v", err)
	}
}

func composeConfigRunnerForTest(t *testing.T, targetType config.ContextType, model string) func(context.Context, *config.Context, string, io.Writer, io.Writer, []string) error {
	t.Helper()
	return func(runCtx context.Context, ctx *config.Context, projectDir string, stdout, stderr io.Writer, argv []string) error {
		if err := runCtx.Err(); err != nil {
			return err
		}
		if ctx == nil || ctx.DockerHostType != targetType || projectDir != ctx.ProjectDir {
			t.Fatalf("unexpected Compose target: context=%#v project=%q", ctx, projectDir)
		}
		want := []string{"docker", "compose", "config", "--format", "json"}
		if strings.Join(argv, "\x00") != strings.Join(want, "\x00") {
			t.Fatalf("Compose config argv = %#v, want %#v", argv, want)
		}
		_, err := io.WriteString(stdout, model)
		return err
	}
}

func TestRefreshCreateContextComposeMetadataKeepsContextDerivedNameWithoutTemplateName(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	projectDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectDir, "compose.yaml"), []byte("services: {}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(compose.yaml) error = %v", err)
	}
	projectName := filepath.Base(projectDir)
	ctx := &config.Context{
		Name:               "isle-local",
		Site:               "isle",
		Plugin:             "isle",
		DockerHostType:     config.ContextLocal,
		ProjectDir:         projectDir,
		ComposeProjectName: projectName,
		ComposeNetwork:     projectName + "_default",
	}
	if err := config.SaveContext(ctx, true); err != nil {
		t.Fatalf("SaveContext() error = %v", err)
	}

	if err := refreshCreateContextComposeMetadata(ctx); err != nil {
		t.Fatalf("refreshCreateContextComposeMetadata() error = %v", err)
	}

	if ctx.ComposeProjectName != projectName {
		t.Fatalf("expected in-memory compose project name preserved, got %q", ctx.ComposeProjectName)
	}
	if ctx.ComposeNetwork != projectName+"_default" {
		t.Fatalf("expected in-memory compose network preserved, got %q", ctx.ComposeNetwork)
	}
	saved, err := config.GetContext("isle-local")
	if err != nil {
		t.Fatalf("GetContext() error = %v", err)
	}
	if saved.ComposeProjectName != projectName || saved.ComposeNetwork != projectName+"_default" {
		t.Fatalf("expected saved context metadata preserved, got project=%q network=%q", saved.ComposeProjectName, saved.ComposeNetwork)
	}
}

func stubComposeCreateTargetLifecycle(t *testing.T, projectDir string, cloned bool, checkout func(context.Context, *config.Context)) {
	t.Helper()
	oldPrepare := createPrepareTarget
	oldRevalidate := createRevalidateTarget
	oldEnsureObserved := createEnsureObservedCheckout
	t.Cleanup(func() {
		createPrepareTarget = oldPrepare
		createRevalidateTarget = oldRevalidate
		createEnsureObservedCheckout = oldEnsureObserved
	})
	createPrepareTarget = func(runCtx context.Context, req plugin.ComposeCreateRequest, ctx *config.Context) (plugin.ComposeCreateTargetObservation, error) {
		if err := runCtx.Err(); err != nil {
			return plugin.ComposeCreateTargetObservation{}, err
		}
		if ctx == nil || ctx.ProjectDir != projectDir || req.Path != projectDir {
			t.Fatalf("unexpected prepared create target: context=%#v request path=%q", ctx, req.Path)
		}
		if err := os.MkdirAll(projectDir, 0o750); err != nil {
			return plugin.ComposeCreateTargetObservation{}, err
		}
		return plugin.ComposeCreateTargetObservation{}, nil
	}
	createRevalidateTarget = func(runCtx context.Context, _ plugin.ComposeCreateRequest, _ *config.Context, _ plugin.ComposeCreateTargetObservation) error {
		return runCtx.Err()
	}
	createEnsureObservedCheckout = func(runCtx context.Context, _ io.Writer, _ plugin.ComposeCreateRequest, ctx *config.Context, _ plugin.ComposeCreateTargetObservation) (bool, error) {
		if checkout != nil {
			checkout(runCtx, ctx)
		}
		return cloned, runCtx.Err()
	}
}

func TestRunCreateCommandRunsMakeUpAndPrintsCommitSuggestion(t *testing.T) {
	oldSDK := commandSDK
	oldEnsure := createEnsureLocalContext
	oldApply := createApply
	oldEnsureJWTKeyPair := createEnsureJWTKeyPair
	oldAcquireProjectLock := createAcquireProjectLock
	oldBootstrap := createBootstrapCheckout
	oldRunStartup := createRunStartup
	oldSleep := createSleep
	t.Cleanup(func() {
		commandSDK = oldSDK
		createEnsureLocalContext = oldEnsure
		createApply = oldApply
		createEnsureJWTKeyPair = oldEnsureJWTKeyPair
		createAcquireProjectLock = oldAcquireProjectLock
		createBootstrapCheckout = oldBootstrap
		createRunStartup = oldRunStartup
		createSleep = oldSleep
	})

	createSleep = func(time.Duration) {}
	commandSDK = &plugin.SDK{}
	projectDir := filepath.Join(t.TempDir(), "site")
	createEnsureLocalContext = func(_ *plugin.SDK, req createRequest) (*config.Context, error) {
		return &config.Context{Name: "isle-local-2", Plugin: "isle", DockerHostType: config.ContextLocal, ProjectDir: projectDir}, nil
	}
	var lockHeldDuringCheckout bool
	stubComposeCreateTargetLifecycle(t, projectDir, true, func(lockedContext context.Context, ctx *config.Context) {
		reentrantContext, cancelReentrant := context.WithTimeout(lockedContext, 20*time.Millisecond)
		defer cancelReentrant()
		reentrantLock, err := ctx.AcquireProjectMutationLock(reentrantContext)
		if err != nil {
			t.Fatalf("checkout did not receive the held project mutation lock context: %v", err)
		}
		if err := reentrantLock.Release(); err != nil {
			t.Fatalf("release reentrant project mutation lock: %v", err)
		}
		probeContext, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()
		competingLock, err := ctx.AcquireProjectMutationLock(probeContext)
		if err == nil {
			_ = competingLock.Release()
			t.Fatal("custom create did not hold its project mutation lock during checkout")
		}
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("competing project lock during checkout error = %v, want deadline exceeded", err)
		}
		lockHeldDuringCheckout = true
	})
	createApply = func(_ context.Context, opts createpkg.Options) error { return nil }
	var projectLockAcquisitions int
	createAcquireProjectLock = func(runCtx context.Context, ctx *config.Context) (*config.ProjectMutationLock, error) {
		projectLockAcquisitions++
		return ctx.AcquireProjectMutationLock(runCtx)
	}
	var jwtKeyPairPrepared bool
	createEnsureJWTKeyPair = func(_ context.Context, ctx *config.Context) error {
		jwtKeyPairPrepared = true
		if ctx.ProjectDir != projectDir {
			t.Fatalf("expected JWT keys prepared in %q, got %q", projectDir, ctx.ProjectDir)
		}
		return nil
	}
	var bootstrapped bool
	createBootstrapCheckout = func(_ context.Context, _ io.Writer, gotProjectDir string) error {
		bootstrapped = true
		if gotProjectDir != projectDir {
			t.Fatalf("expected bootstrap in %q, got %q", projectDir, gotProjectDir)
		}
		return nil
	}

	var ranStartup bool
	createRunStartup = func(_ context.Context, _ io.Writer, ctx *config.Context) error {
		ranStartup = true
		if !jwtKeyPairPrepared {
			t.Fatal("expected JWT keypair to be prepared before startup")
		}
		if ctx.ProjectDir != projectDir {
			t.Fatalf("expected startup in %q, got %q", projectDir, ctx.ProjectDir)
		}
		if ctx.Name != "isle-local-2" {
			t.Fatalf("expected context isle-local-2, got %q", ctx.Name)
		}
		probeContext, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()
		competingLock, err := ctx.AcquireProjectMutationLock(probeContext)
		if err == nil {
			_ = competingLock.Release()
			t.Fatal("custom create released its project mutation lock before startup completed")
		}
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("competing project lock error = %v, want deadline exceeded while create lock is held", err)
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
			FeatureBundles: map[string]string{
				createpkg.FeatureBundleMergePDF:   string(corecomponent.StateOn),
				createpkg.FeatureBundleHOCRSearch: string(corecomponent.StateOn),
			},
			FeatureBundleOptions: map[string]map[string]string{
				createpkg.FeatureBundleHOCRSearch: {createpkg.HOCRStructuredTextTermOption: "77"},
			},
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
	if projectLockAcquisitions != 1 {
		t.Fatalf("project mutation lock acquisitions = %d, want 1", projectLockAcquisitions)
	}
	if !lockHeldDuringCheckout {
		t.Fatal("expected project mutation lock to be held before checkout")
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
	if !strings.Contains(rendered, "--mergepdf=enabled") || !strings.Contains(rendered, "--hocr-search=enabled") || !strings.Contains(rendered, `--hocr-term-id="77"`) {
		t.Fatalf("expected recreate command with feature-bundle flags, got:\n%s", rendered)
	}
	if strings.Contains(rendered, "--islandora-tag") {
		t.Fatalf("recreate command contains retired Islandora tag override:\n%s", rendered)
	}
	if !strings.Contains(rendered, "git remote add origin git@github.com:your-org/your-repo.git") {
		t.Fatalf("expected git remote setup guidance, got:\n%s", rendered)
	}
}

func TestRunCreateCommandStopsWhenTargetChangesBeforeLock(t *testing.T) {
	oldSDK := commandSDK
	oldEnsure := createEnsureLocalContext
	oldPrepare := createPrepareTarget
	oldRevalidate := createRevalidateTarget
	oldEnsureObserved := createEnsureObservedCheckout
	oldAcquireProjectLock := createAcquireProjectLock
	t.Cleanup(func() {
		commandSDK = oldSDK
		createEnsureLocalContext = oldEnsure
		createPrepareTarget = oldPrepare
		createRevalidateTarget = oldRevalidate
		createEnsureObservedCheckout = oldEnsureObserved
		createAcquireProjectLock = oldAcquireProjectLock
	})

	commandSDK = &plugin.SDK{}
	projectDir := filepath.Join(t.TempDir(), "site")
	createEnsureLocalContext = func(_ *plugin.SDK, _ createRequest) (*config.Context, error) {
		return &config.Context{Name: "isle-race", Plugin: "isle", DockerHostType: config.ContextLocal, ProjectDir: projectDir}, nil
	}
	createPrepareTarget = func(runCtx context.Context, _ plugin.ComposeCreateRequest, _ *config.Context) (plugin.ComposeCreateTargetObservation, error) {
		if err := runCtx.Err(); err != nil {
			return plugin.ComposeCreateTargetObservation{}, err
		}
		return plugin.ComposeCreateTargetObservation{}, os.MkdirAll(projectDir, 0o750)
	}
	targetChanged := errors.New("target changed while waiting for lock")
	createRevalidateTarget = func(runCtx context.Context, _ plugin.ComposeCreateRequest, _ *config.Context, _ plugin.ComposeCreateTargetObservation) error {
		if err := runCtx.Err(); err != nil {
			return err
		}
		return targetChanged
	}
	var checkoutCalled bool
	createEnsureObservedCheckout = func(context.Context, io.Writer, plugin.ComposeCreateRequest, *config.Context, plugin.ComposeCreateTargetObservation) (bool, error) {
		checkoutCalled = true
		return false, nil
	}
	createAcquireProjectLock = func(runCtx context.Context, ctx *config.Context) (*config.ProjectMutationLock, error) {
		return ctx.AcquireProjectMutationLock(runCtx)
	}

	type commandContextKey struct{}
	originalContext := context.WithValue(context.Background(), commandContextKey{}, "original")
	cmd := &cobra.Command{Use: "create"}
	cmd.SetContext(originalContext)
	cmd.SetOut(io.Discard)
	err := runCreateCommand(cmd, createRequest{ComposeCreateRequest: plugin.ComposeCreateRequest{
		CheckoutSource: plugin.CheckoutSourceTemplate,
		TemplateRepo:   defaultTemplateRepo,
		TemplateBranch: defaultTemplateBranch,
		Path:           projectDir,
		SetupOnly:      true,
	}})
	if !errors.Is(err, targetChanged) {
		t.Fatalf("runCreateCommand() error = %v, want target transition rejection", err)
	}
	if checkoutCalled {
		t.Fatal("target transition rejection did not stop before checkout mutation")
	}
	if cmd.Context() != originalContext {
		t.Fatal("runCreateCommand() did not restore the command context after releasing the mutation lock")
	}
	lock, err := (&config.Context{ProjectDir: projectDir}).AcquireProjectMutationLock(context.Background())
	if err != nil {
		t.Fatalf("reacquire project mutation lock after transition rejection: %v", err)
	}
	if err := lock.Release(); err != nil {
		t.Fatalf("release reacquired project mutation lock: %v", err)
	}
}

func TestRunCreateCommandNormalizesAndBootstrapsVerifiedRetry(t *testing.T) {
	oldSDK := commandSDK
	oldEnsure := createEnsureLocalContext
	oldApply := createApply
	oldEnsureJWTKeyPair := createEnsureJWTKeyPair
	oldBootstrap := createBootstrapCheckout
	oldRefresh := createRefreshContext
	oldSleep := createSleep
	t.Cleanup(func() {
		commandSDK = oldSDK
		createEnsureLocalContext = oldEnsure
		createApply = oldApply
		createEnsureJWTKeyPair = oldEnsureJWTKeyPair
		createBootstrapCheckout = oldBootstrap
		createRefreshContext = oldRefresh
		createSleep = oldSleep
	})

	createSleep = func(time.Duration) {}
	commandSDK = &plugin.SDK{}
	projectDir := t.TempDir()
	canonical := filepath.Join(projectDir, "compose.yaml")
	if err := os.WriteFile(canonical, []byte("services:\n  init:\n    volumes:\n      - ./docker-compose.yml:/docker-compose.yml:ro\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	createEnsureLocalContext = func(_ *plugin.SDK, _ createRequest) (*config.Context, error) {
		return &config.Context{Name: "isle-retry", Plugin: "isle", DockerHostType: config.ContextLocal, ProjectDir: projectDir}, nil
	}
	stubComposeCreateTargetLifecycle(t, projectDir, false, nil)
	createApply = func(context.Context, createpkg.Options) error { return nil }
	createEnsureJWTKeyPair = func(context.Context, *config.Context) error { return nil }
	var bootstrapped bool
	createBootstrapCheckout = func(_ context.Context, _ io.Writer, gotProjectDir string) error {
		bootstrapped = true
		if gotProjectDir != projectDir {
			t.Fatalf("bootstrap project directory = %q, want %q", gotProjectDir, projectDir)
		}
		return nil
	}
	createRefreshContext = func(context.Context, *config.Context) error { return nil }

	cmd := &cobra.Command{Use: "create"}
	cmd.SetOut(io.Discard)
	err := runCreateCommand(cmd, createRequest{
		ComposeCreateRequest: plugin.ComposeCreateRequest{
			CheckoutSource: plugin.CheckoutSourceTemplate,
			TemplateRepo:   defaultTemplateRepo,
			TemplateBranch: defaultTemplateBranch,
			Path:           projectDir,
			SetupOnly:      true,
		},
		Apply: createpkg.Options{DrupalRootfs: createpkg.DefaultDrupalRootfs},
	})
	if err != nil {
		t.Fatalf("runCreateCommand() retry error = %v", err)
	}
	if !bootstrapped {
		t.Fatal("verified retry skipped checkout bootstrap")
	}
	data, err := os.ReadFile(canonical)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "./docker-compose.yml:/docker-compose.yml") || !strings.Contains(string(data), "./compose.yaml:/docker-compose.yml:ro") {
		t.Fatalf("verified retry skipped Compose normalization:\n%s", data)
	}
	info, err := os.Stat(canonical)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("normalized retry mode = %o, want 640", info.Mode().Perm())
	}
}

func TestRunCreateCommandWritesProgressToStderrDuringRPC(t *testing.T) {
	t.Setenv("SITECTL_RPC", "1")

	oldSDK := commandSDK
	oldEnsure := createEnsureLocalContext
	oldApply := createApply
	oldEnsureJWTKeyPair := createEnsureJWTKeyPair
	oldBootstrap := createBootstrapCheckout
	oldRunStartup := createRunStartup
	oldSleep := createSleep
	t.Cleanup(func() {
		commandSDK = oldSDK
		createEnsureLocalContext = oldEnsure
		createApply = oldApply
		createEnsureJWTKeyPair = oldEnsureJWTKeyPair
		createBootstrapCheckout = oldBootstrap
		createRunStartup = oldRunStartup
		createSleep = oldSleep
	})

	createSleep = func(time.Duration) {}
	commandSDK = &plugin.SDK{}
	projectDir := filepath.Join(t.TempDir(), "site")
	createEnsureLocalContext = func(_ *plugin.SDK, req createRequest) (*config.Context, error) {
		return &config.Context{Name: "isle-local", Plugin: "isle", DockerHostType: config.ContextLocal, ProjectDir: projectDir}, nil
	}
	stubComposeCreateTargetLifecycle(t, projectDir, true, nil)
	createApply = func(context.Context, createpkg.Options) error { return nil }
	createEnsureJWTKeyPair = func(context.Context, *config.Context) error { return nil }
	createBootstrapCheckout = func(_ context.Context, out io.Writer, gotProjectDir string) error {
		if gotProjectDir != projectDir {
			t.Fatalf("expected bootstrap in %q, got %q", projectDir, gotProjectDir)
		}
		_, err := fmt.Fprintln(out, "bootstrap progress")
		return err
	}
	createRunStartup = func(_ context.Context, _ io.Writer, ctx *config.Context) error {
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
	oldApply := createApply
	oldEnsureJWTKeyPair := createEnsureJWTKeyPair
	oldBootstrap := createBootstrapCheckout
	oldRunStartup := createRunStartup
	oldSleep := createSleep
	t.Cleanup(func() {
		commandSDK = oldSDK
		createEnsureLocalContext = oldEnsure
		createApply = oldApply
		createEnsureJWTKeyPair = oldEnsureJWTKeyPair
		createBootstrapCheckout = oldBootstrap
		createRunStartup = oldRunStartup
		createSleep = oldSleep
	})

	createSleep = func(time.Duration) {}
	commandSDK = &plugin.SDK{}
	projectDir := filepath.Join(t.TempDir(), "site")
	createEnsureLocalContext = func(_ *plugin.SDK, req createRequest) (*config.Context, error) {
		return &config.Context{Name: "isle-local", Plugin: "isle", DockerHostType: config.ContextLocal, ProjectDir: projectDir}, nil
	}
	stubComposeCreateTargetLifecycle(t, projectDir, true, nil)
	createApply = func(context.Context, createpkg.Options) error { return nil }
	var jwtKeyPairPrepared bool
	createEnsureJWTKeyPair = func(_ context.Context, _ *config.Context) error {
		jwtKeyPairPrepared = true
		return nil
	}
	var bootstrapped bool
	createBootstrapCheckout = func(_ context.Context, _ io.Writer, gotProjectDir string) error {
		bootstrapped = true
		if gotProjectDir != projectDir {
			t.Fatalf("expected bootstrap in %q, got %q", projectDir, gotProjectDir)
		}
		return nil
	}
	createRunStartup = func(_ context.Context, _ io.Writer, ctx *config.Context) error {
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
	if !jwtKeyPairPrepared {
		t.Fatal("expected setup-only create to prepare the JWT keypair")
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
	createRunProjectCommand = func(_ context.Context, gotProjectDir string, stdout, stderr io.Writer, name string, args ...string) error {
		if gotProjectDir != projectDir {
			t.Fatalf("expected project dir %q, got %q", projectDir, gotProjectDir)
		}
		if name != "git" {
			t.Fatalf("expected git command, got %q", name)
		}
		commands = append(commands, append([]string{name}, args...))
		command := strings.Join(args, " ")
		if strings.Contains(command, "rev-parse --verify HEAD") {
			return errors.New("HEAD is unborn")
		}
		if strings.Contains(command, "rev-parse --is-inside-work-tree") {
			_, err := io.WriteString(stdout, "true\n")
			return err
		}
		return nil
	}

	if err := bootstrapCheckout(io.Discard, projectDir); err != nil {
		t.Fatalf("bootstrapCheckout() error = %v", err)
	}

	if len(commands) != 4 {
		t.Fatalf("expected 4 git commands, got %#v", commands)
	}
	if got, want := strings.Join(commands[0], " "), fmt.Sprintf("git -c safe.directory=%s rev-parse --verify HEAD", projectDir); got != want {
		t.Fatalf("unexpected HEAD inspection command %q", got)
	}
	if got, want := strings.Join(commands[1], " "), fmt.Sprintf("git -c safe.directory=%s rev-parse --is-inside-work-tree", projectDir); got != want {
		t.Fatalf("unexpected work-tree inspection command %q", got)
	}
	if got, want := strings.Join(commands[2], " "), fmt.Sprintf("git -c safe.directory=%s add .", projectDir); got != want {
		t.Fatalf("expected first command `git add .`, got %q", got)
	}
	if got, want := strings.Join(commands[3], " "), fmt.Sprintf("git -c safe.directory=%s -c user.name=sitectl-isle -c user.email=sitectl-isle@localhost commit -m initial commit.", projectDir); got != want {
		t.Fatalf("unexpected commit command %q", got)
	}
}

func TestBootstrapCheckoutSkipsCommitWhenRetryAlreadyHasHEAD(t *testing.T) {
	projectDir := t.TempDir()
	oldRunProject := createRunProjectCommand
	t.Cleanup(func() { createRunProjectCommand = oldRunProject })

	var commands [][]string
	createRunProjectCommand = func(_ context.Context, gotProjectDir string, stdout, stderr io.Writer, name string, args ...string) error {
		if gotProjectDir != projectDir || name != "git" {
			t.Fatalf("unexpected command target: project=%q name=%q", gotProjectDir, name)
		}
		commands = append(commands, append([]string{name}, args...))
		_, err := io.WriteString(stdout, strings.Repeat("b", 40)+"\n")
		return err
	}

	if err := bootstrapCheckout(io.Discard, projectDir); err != nil {
		t.Fatalf("bootstrapCheckout() retry error = %v", err)
	}
	if len(commands) != 1 || !strings.Contains(strings.Join(commands[0], " "), "rev-parse --verify HEAD") {
		t.Fatalf("retry bootstrap commands = %#v, want only checked HEAD inspection", commands)
	}
}

func TestBootstrapCheckoutContextStopsBeforeGitWhenCancelled(t *testing.T) {
	oldRunProject := createRunProjectCommand
	t.Cleanup(func() { createRunProjectCommand = oldRunProject })
	createRunProjectCommand = func(context.Context, string, io.Writer, io.Writer, string, ...string) error {
		t.Fatal("cancelled bootstrap executed Git")
		return nil
	}
	runCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := bootstrapCheckoutContext(runCtx, io.Discard, t.TempDir()); !errors.Is(err, context.Canceled) {
		t.Fatalf("bootstrapCheckoutContext() error = %v, want context cancellation", err)
	}
}

func TestBootstrapRemoteCheckoutUsesCheckedArgv(t *testing.T) {
	projectDir := "/srv/isle site"
	oldRunArgv := createRunComposeArgv
	oldSleep := createSleep
	t.Cleanup(func() {
		createRunComposeArgv = oldRunArgv
		createSleep = oldSleep
	})
	createSleep = func(time.Duration) {}

	var commands [][]string
	createRunComposeArgv = func(_ context.Context, ctx *config.Context, gotProjectDir string, stdout, stderr io.Writer, argv []string) error {
		if ctx == nil || ctx.DockerHostType != config.ContextRemote || gotProjectDir != projectDir {
			t.Fatalf("unexpected remote command target: context=%#v project=%q", ctx, gotProjectDir)
		}
		commands = append(commands, append([]string(nil), argv...))
		command := strings.Join(argv, " ")
		switch {
		case strings.Contains(command, "rev-parse --verify HEAD"):
			return errors.New("HEAD is unborn")
		case strings.Contains(command, "rev-parse --is-inside-work-tree"):
			_, err := io.WriteString(stdout, "true\n")
			return err
		default:
			return nil
		}
	}

	ctx := &config.Context{DockerHostType: config.ContextRemote, ProjectDir: projectDir}
	if err := bootstrapCheckoutForContext(context.Background(), io.Discard, ctx); err != nil {
		t.Fatalf("bootstrapCheckoutForContext() error = %v", err)
	}
	if len(commands) != 4 {
		t.Fatalf("remote bootstrap commands = %#v, want two inspections plus add and commit", commands)
	}
	wantAdd := []string{"git", "-c", "safe.directory=" + projectDir, "add", "."}
	if strings.Join(commands[2], "\x00") != strings.Join(wantAdd, "\x00") {
		t.Fatalf("remote git add argv = %#v, want %#v", commands[2], wantAdd)
	}
	wantCommit := []string{"git", "-c", "safe.directory=" + projectDir, "-c", "user.name=sitectl-isle", "-c", "user.email=sitectl-isle@localhost", "commit", "-m", "initial commit."}
	if strings.Join(commands[3], "\x00") != strings.Join(wantCommit, "\x00") {
		t.Fatalf("remote git commit argv = %#v, want %#v", commands[3], wantCommit)
	}
}

func TestRunStartupDelegatesLifecycleCommandsToComposeSDK(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	projectDir := t.TempDir()
	runCtx := context.WithValue(context.Background(), createTestContextKey{}, "held-lock")

	oldRunCompose := createRunComposeCommand
	t.Cleanup(func() {
		createRunComposeCommand = oldRunCompose
	})

	var commands []string
	createRunComposeCommand = func(runCtx context.Context, ctx *config.Context, gotProjectDir string, stdout, stderr io.Writer, command string) error {
		if runCtx.Value(createTestContextKey{}) != "held-lock" {
			t.Fatal("startup command did not receive the held lock context")
		}
		if gotProjectDir != projectDir {
			t.Fatalf("expected project dir %q, got %q", projectDir, gotProjectDir)
		}
		if ctx == nil || ctx.Name != "isle-local" {
			t.Fatalf("unexpected context: %#v", ctx)
		}
		commands = append(commands, command)
		return nil
	}

	if err := runStartup(runCtx, io.Discard, &config.Context{Name: "isle-local", ProjectDir: projectDir}); err != nil {
		t.Fatalf("runStartup() error = %v", err)
	}
	want := startupCommands()
	if len(commands) != len(want) {
		t.Fatalf("startup commands = %#v, want %#v", commands, want)
	}
	for index := range want {
		if commands[index] != want[index] {
			t.Fatalf("startup command %d = %q, want %q", index, commands[index], want[index])
		}
		if strings.HasPrefix(commands[index], "export ") {
			t.Fatalf("startup command %d unexpectedly materialized an environment prefix: %q", index, commands[index])
		}
	}
}

type createTestContextKey struct{}

func TestNormalizeComposeProjectFilename(t *testing.T) {
	t.Parallel()
	projectDir := t.TempDir()
	legacy := filepath.Join(projectDir, "docker-compose.yml")
	canonical := filepath.Join(projectDir, "compose.yaml")
	if err := os.WriteFile(legacy, []byte("services:\n  init:\n    volumes:\n      - ./docker-compose.yml:/docker-compose.yml:ro\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := normalizeComposeProjectFilename(context.Background(), &config.Context{ProjectDir: projectDir, DockerHostType: config.ContextLocal}); err != nil {
		t.Fatalf("normalizeComposeProjectFilename() error = %v", err)
	}
	if _, err := os.Stat(canonical); err != nil {
		t.Fatalf("canonical Compose file missing: %v", err)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatalf("legacy Compose file still exists: %v", err)
	}
	data, err := os.ReadFile(canonical)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "./compose.yaml:/docker-compose.yml:ro") {
		t.Fatalf("canonical Compose self-mount was not updated:\n%s", data)
	}
}

func TestNormalizeComposeProjectFilenameRepairsCanonicalRetry(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	canonical := filepath.Join(projectDir, "compose.yaml")
	if err := os.WriteFile(canonical, []byte("services:\n  init:\n    volumes:\n      - ./docker-compose.yml:/docker-compose.yml:ro\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	ctx := &config.Context{ProjectDir: projectDir, DockerHostType: config.ContextLocal}
	if err := normalizeComposeProjectFilename(context.Background(), ctx); err != nil {
		t.Fatalf("normalizeComposeProjectFilename() retry error = %v", err)
	}
	data, err := os.ReadFile(canonical)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "./docker-compose.yml:/docker-compose.yml") || !strings.Contains(string(data), "./compose.yaml:/docker-compose.yml:ro") {
		t.Fatalf("canonical Compose self-mount was not repaired on retry:\n%s", data)
	}
	info, err := os.Stat(canonical)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("canonical Compose mode = %o, want 640", info.Mode().Perm())
	}
	if err := normalizeComposeProjectFilename(context.Background(), ctx); err != nil {
		t.Fatalf("idempotent normalizeComposeProjectFilename() error = %v", err)
	}
}

func TestNormalizeComposeProjectFilenameStopsBeforeMutationWhenCancelled(t *testing.T) {
	projectDir := t.TempDir()
	legacy := filepath.Join(projectDir, "docker-compose.yml")
	if err := os.WriteFile(legacy, []byte("services: {}\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	oldRunArgv := createRunComposeArgv
	t.Cleanup(func() { createRunComposeArgv = oldRunArgv })
	createRunComposeArgv = func(context.Context, *config.Context, string, io.Writer, io.Writer, []string) error {
		t.Fatal("cancelled normalization executed a move")
		return nil
	}
	runCtx, cancel := context.WithCancel(context.Background())
	cancel()
	err := normalizeComposeProjectFilename(runCtx, &config.Context{ProjectDir: projectDir, DockerHostType: config.ContextLocal})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("normalizeComposeProjectFilename() error = %v, want context cancellation", err)
	}
	if _, err := os.Stat(legacy); err != nil {
		t.Fatalf("cancelled normalization changed legacy Compose file: %v", err)
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
	_ = cmd.Flags().Set("path", "/tmp/site")
	return cmd
}

var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSI(value string) string {
	return ansiPattern.ReplaceAllString(value, "")
}

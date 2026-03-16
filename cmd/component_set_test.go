package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	createpkg "github.com/libops/sitectl-isle/pkg/create"
	"github.com/libops/sitectl-isle/pkg/externalcantaloupe"
	"github.com/libops/sitectl-isle/pkg/traefikconfig"
	corecomponent "github.com/libops/sitectl/pkg/component"
	"github.com/spf13/cobra"
)

func TestRunComponentSetPreservesOtherDetectedState(t *testing.T) {
	projectDir := t.TempDir()
	writeISLEOnFixture(t, projectDir)

	oldStatusPath := statusPath
	oldDrupalRootfs := statusDrupalRootfs
	oldYolo := componentSetYolo
	oldApply := componentApplyOptions
	oldInput := componentSetInput
	oldPromptChoice := componentPromptChoice
	t.Cleanup(func() {
		statusPath = oldStatusPath
		statusDrupalRootfs = oldDrupalRootfs
		componentSetYolo = oldYolo
		componentApplyOptions = oldApply
		componentSetInput = oldInput
		componentPromptChoice = oldPromptChoice
	})

	statusPath = projectDir
	statusDrupalRootfs = createpkg.DefaultDrupalRootfs
	componentSetYolo = true

	var got createpkg.Options
	componentApplyOptions = func(opts createpkg.Options) error {
		got = opts
		return nil
	}

	var out bytes.Buffer
	cmd := componentSetCmd
	cmd.SetOut(&out)

	if err := runComponentSet(cmd, "fcrepo", "off"); err != nil {
		t.Fatalf("runComponentSet() error = %v", err)
	}

	if got.Path != projectDir {
		t.Fatalf("expected project path %q, got %q", projectDir, got.Path)
	}
	if got.Fcrepo != createpkg.FcrepoStateOff {
		t.Fatalf("expected fcrepo off, got %q", got.Fcrepo)
	}
	if got.Blazegraph != createpkg.FcrepoStateOn {
		t.Fatalf("expected blazegraph preserved as on, got %q", got.Blazegraph)
	}
	if got.ISLEFileSystemURI != createpkg.DefaultISLEFileSystemURI {
		t.Fatalf("expected default filesystem uri %q, got %q", createpkg.DefaultISLEFileSystemURI, got.ISLEFileSystemURI)
	}
	if !strings.Contains(out.String(), "fcrepo: superceded") {
		t.Fatalf("expected command output, got:\n%s", out.String())
	}
}

func TestRunComponentSetUsesCurrentFilesystemURIWhenTurningFcrepoOff(t *testing.T) {
	projectDir := t.TempDir()
	writeISLEOnFixture(t, projectDir)

	fieldPath := filepath.Join(projectDir, "drupal", "rootfs", "var", "www", "drupal", "config", "sync", "field.storage.media.field_media_file.yml")
	writeFileForTest(t, fieldPath, "settings:\n  uri_scheme: \"archive\"\n")

	oldStatusPath := statusPath
	oldDrupalRootfs := statusDrupalRootfs
	oldYolo := componentSetYolo
	oldApply := componentApplyOptions
	oldPromptChoice := componentPromptChoice
	t.Cleanup(func() {
		statusPath = oldStatusPath
		statusDrupalRootfs = oldDrupalRootfs
		componentSetYolo = oldYolo
		componentApplyOptions = oldApply
		componentPromptChoice = oldPromptChoice
	})

	statusPath = projectDir
	statusDrupalRootfs = createpkg.DefaultDrupalRootfs
	componentSetYolo = true

	var got createpkg.Options
	componentApplyOptions = func(opts createpkg.Options) error {
		got = opts
		return nil
	}

	if err := runComponentSet(componentSetCmd, "fcrepo", "off"); err != nil {
		t.Fatalf("runComponentSet() error = %v", err)
	}
	if got.ISLEFileSystemURI != "archive" {
		t.Fatalf("expected archive filesystem uri, got %q", got.ISLEFileSystemURI)
	}
}

func TestRunComponentSetRefusesWhenOtherComponentIsDrifted(t *testing.T) {
	projectDir := t.TempDir()
	writeISLEOnFixture(t, projectDir)

	composePath := filepath.Join(projectDir, "docker-compose.yml")
	writeFileForTest(t, composePath, `
services:
  alpaca:
    environment:
      ALPACA_FCREPO_INDEXER_ENABLED: "true"
      ALPACA_TRIPLESTORE_INDEXER_ENABLED: "true"
  drupal:
    environment:
      DRUPAL_DEFAULT_FCREPO_URL: http://fcrepo.example/fcrepo/rest/
      DRUPAL_DEFAULT_TRIPLESTORE_NAMESPACE: ""
      DRUPAL_ENABLE_HTTPS: "false"
  fcrepo:
    image: islandora/fcrepo6
volumes:
  fcrepo-data: {}
`)

	oldStatusPath := statusPath
	oldDrupalRootfs := statusDrupalRootfs
	oldYolo := componentSetYolo
	oldPromptChoice := componentPromptChoice
	t.Cleanup(func() {
		statusPath = oldStatusPath
		statusDrupalRootfs = oldDrupalRootfs
		componentSetYolo = oldYolo
		componentPromptChoice = oldPromptChoice
	})

	statusPath = projectDir
	statusDrupalRootfs = createpkg.DefaultDrupalRootfs
	componentSetYolo = true

	err := runComponentSet(componentSetCmd, "fcrepo", "off")
	if err == nil {
		t.Fatal("expected drift error")
	}
	if !strings.Contains(err.Error(), `component "blazegraph" is drifted`) {
		t.Fatalf("expected blazegraph drift error, got %v", err)
	}
}

func TestRunComponentSetAppliesISLETLSOverrideHTTPOverride(t *testing.T) {
	projectDir := t.TempDir()
	writeISLEOnFixture(t, projectDir)

	oldStatusPath := statusPath
	oldDrupalRootfs := statusDrupalRootfs
	oldYolo := componentSetYolo
	oldTLSMode := componentSetTLSMode
	oldPromptChoice := componentPromptChoice
	t.Cleanup(func() {
		statusPath = oldStatusPath
		statusDrupalRootfs = oldDrupalRootfs
		componentSetYolo = oldYolo
		componentSetTLSMode = oldTLSMode
		componentPromptChoice = oldPromptChoice
	})

	statusPath = projectDir
	statusDrupalRootfs = createpkg.DefaultDrupalRootfs
	componentSetYolo = true
	componentSetTLSMode = "http"

	var out bytes.Buffer
	cmd := componentSetCmd
	cmd.SetOut(&out)

	if err := runComponentSet(cmd, "isle-tls-override", "on"); err != nil {
		t.Fatalf("runComponentSet() error = %v", err)
	}

	rendered := out.String()
	if !strings.Contains(rendered, "isle-tls-override: enabled") {
		t.Fatalf("expected component output, got:\n%s", rendered)
	}

	devOverride, err := os.ReadFile(filepath.Join(projectDir, "docker-compose.local.yml"))
	if err != nil {
		t.Fatalf("ReadFile(docker-compose.local.yml) error = %v", err)
	}
	if !strings.Contains(string(devOverride), "DRUPAL_ENABLE_HTTPS: \"false\"") {
		t.Fatalf("expected dev http override, got:\n%s", string(devOverride))
	}
}

func TestRunComponentSetEnablesExternalCantaloupe(t *testing.T) {
	projectDir := t.TempDir()
	writeISLEOnFixture(t, projectDir)

	oldStatusPath := statusPath
	oldDrupalRootfs := statusDrupalRootfs
	oldYolo := componentSetYolo
	oldInput := componentSetInput
	oldPromptChoice := componentPromptChoice
	t.Cleanup(func() {
		statusPath = oldStatusPath
		statusDrupalRootfs = oldDrupalRootfs
		componentSetYolo = oldYolo
		componentSetInput = oldInput
		componentPromptChoice = oldPromptChoice
		if flag := componentSetCmd.Flags().Lookup("external-cantaloupe-upstream-url"); flag != nil {
			flag.Changed = false
		}
	})

	statusPath = projectDir
	statusDrupalRootfs = createpkg.DefaultDrupalRootfs
	componentSetYolo = true
	_ = componentSetCmd.Flags().Set("external-cantaloupe-upstream-url", "http://cantaloupe.example:8182")

	var out bytes.Buffer
	cmd := componentSetCmd
	cmd.SetOut(&out)

	if err := runComponentSet(cmd, "external-cantaloupe", "on"); err != nil {
		t.Fatalf("runComponentSet() error = %v", err)
	}

	composeText, err := os.ReadFile(filepath.Join(projectDir, "docker-compose.yml"))
	if err != nil {
		t.Fatalf("ReadFile(docker-compose.yml) error = %v", err)
	}
	if strings.Contains(string(composeText), "\n  cantaloupe:\n") {
		t.Fatalf("expected base cantaloupe removed, got:\n%s", string(composeText))
	}

	overrideText, err := os.ReadFile(filepath.Join(projectDir, "docker-compose.local.yml"))
	if err != nil {
		t.Fatalf("ReadFile(docker-compose.local.yml) error = %v", err)
	}
	if !strings.Contains(string(overrideText), "cantaloupe:") || !strings.Contains(string(overrideText), "8182:8182") {
		t.Fatalf("expected cantaloupe local override, got:\n%s", string(overrideText))
	}

	traefikText, err := os.ReadFile(filepath.Join(projectDir, externalcantaloupe.DefaultTraefikConfigPath))
	if err != nil {
		t.Fatalf("ReadFile(cantaloupe.yml) error = %v", err)
	}
	if !strings.Contains(string(traefikText), "http://cantaloupe.example:8182") {
		t.Fatalf("expected external upstream in traefik config, got:\n%s", string(traefikText))
	}
	if !strings.Contains(out.String(), "external-cantaloupe: distributed (http://cantaloupe.example:8182)") {
		t.Fatalf("expected command output, got:\n%s", out.String())
	}
}

func TestRunComponentSetPromptsForProdTLSModeWhenMissing(t *testing.T) {
	projectDir := t.TempDir()
	writeISLEOnFixture(t, projectDir)

	oldStatusPath := statusPath
	oldDrupalRootfs := statusDrupalRootfs
	oldYolo := componentSetYolo
	oldTLSMode := componentSetTLSMode
	oldInput := componentSetInput
	oldPromptChoice := componentPromptChoice
	t.Cleanup(func() {
		statusPath = oldStatusPath
		statusDrupalRootfs = oldDrupalRootfs
		componentSetYolo = oldYolo
		componentSetTLSMode = oldTLSMode
		componentSetInput = oldInput
		componentPromptChoice = oldPromptChoice
	})

	statusPath = projectDir
	statusDrupalRootfs = createpkg.DefaultDrupalRootfs
	componentSetYolo = false
	componentSetTLSMode = ""

	var promptedName string
	var promptedDefault string
	componentPromptChoice = func(name string, choices []corecomponent.Choice, defaultValue string, input corecomponent.InputFunc, sections ...string) (string, error) {
		promptedName = name
		promptedDefault = defaultValue
		return traefikconfig.ModeLetsEncrypt, nil
	}
	componentSetInput = func(question ...string) (string, error) {
		return "y", nil
	}

	if err := runComponentSet(componentSetCmd, "isle-tls", "on"); err != nil {
		t.Fatalf("runComponentSet() error = %v", err)
	}

	if promptedName != "isle-tls-tls-mode" {
		t.Fatalf("expected prod tls prompt, got %q", promptedName)
	}
	if promptedDefault != traefikconfig.ModeSelfManaged {
		t.Fatalf("expected self-managed default, got %q", promptedDefault)
	}

	envText, err := os.ReadFile(filepath.Join(projectDir, ".env"))
	if err != nil {
		t.Fatalf("ReadFile(.env) error = %v", err)
	}
	if !strings.Contains(string(envText), "URI_SCHEME=\"https\"") || !strings.Contains(string(envText), "TLS_PROVIDER=\"letsencrypt\"") {
		t.Fatalf("expected letsencrypt env settings, got:\n%s", string(envText))
	}

	composeText, err := os.ReadFile(filepath.Join(projectDir, "docker-compose.yml"))
	if err != nil {
		t.Fatalf("ReadFile(docker-compose.yml) error = %v", err)
	}
	if !strings.Contains(string(composeText), "DRUPAL_ENABLE_HTTPS: \"true\"") {
		t.Fatalf("expected https enabled in docker-compose.yml, got:\n%s", string(composeText))
	}
}

func TestRunComponentSetPromptsForDevTLSModeWhenMissing(t *testing.T) {
	projectDir := t.TempDir()
	writeISLEOnFixture(t, projectDir)

	oldStatusPath := statusPath
	oldDrupalRootfs := statusDrupalRootfs
	oldYolo := componentSetYolo
	oldTLSMode := componentSetTLSMode
	oldInput := componentSetInput
	oldPromptChoice := componentPromptChoice
	t.Cleanup(func() {
		statusPath = oldStatusPath
		statusDrupalRootfs = oldDrupalRootfs
		componentSetYolo = oldYolo
		componentSetTLSMode = oldTLSMode
		componentSetInput = oldInput
		componentPromptChoice = oldPromptChoice
	})

	statusPath = projectDir
	statusDrupalRootfs = createpkg.DefaultDrupalRootfs
	componentSetYolo = false
	componentSetTLSMode = ""

	var promptedName string
	var promptedDefault string
	componentPromptChoice = func(name string, choices []corecomponent.Choice, defaultValue string, input corecomponent.InputFunc, sections ...string) (string, error) {
		promptedName = name
		promptedDefault = defaultValue
		return traefikconfig.ModeHTTP, nil
	}
	componentSetInput = func(question ...string) (string, error) {
		return "y", nil
	}

	if err := runComponentSet(componentSetCmd, "isle-tls-override", "on"); err != nil {
		t.Fatalf("runComponentSet() error = %v", err)
	}

	if promptedName != "isle-tls-override-tls-mode" {
		t.Fatalf("expected dev tls prompt, got %q", promptedName)
	}
	if promptedDefault != traefikconfig.ModeMkcert {
		t.Fatalf("expected mkcert default, got %q", promptedDefault)
	}

	devOverride, err := os.ReadFile(filepath.Join(projectDir, "docker-compose.local.yml"))
	if err != nil {
		t.Fatalf("ReadFile(docker-compose.local.yml) error = %v", err)
	}
	if !strings.Contains(string(devOverride), "DRUPAL_ENABLE_HTTPS: \"false\"") {
		t.Fatalf("expected dev http override, got:\n%s", string(devOverride))
	}
}

func TestRunComponentSetForcesHTTPWhenTurningProdTLSOff(t *testing.T) {
	projectDir := t.TempDir()
	writeISLEOnFixture(t, projectDir)
	if err := traefikconfig.ApplyProd(projectDir, traefikconfig.ModeMkcert); err != nil {
		t.Fatalf("ApplyProd() error = %v", err)
	}

	oldStatusPath := statusPath
	oldDrupalRootfs := statusDrupalRootfs
	oldYolo := componentSetYolo
	oldTLSMode := componentSetTLSMode
	oldPromptChoice := componentPromptChoice
	t.Cleanup(func() {
		statusPath = oldStatusPath
		statusDrupalRootfs = oldDrupalRootfs
		componentSetYolo = oldYolo
		componentSetTLSMode = oldTLSMode
		componentPromptChoice = oldPromptChoice
	})

	statusPath = projectDir
	statusDrupalRootfs = createpkg.DefaultDrupalRootfs
	componentSetYolo = true
	componentSetTLSMode = traefikconfig.ModeMkcert

	if err := runComponentSet(componentSetCmd, "isle-tls", "off"); err != nil {
		t.Fatalf("runComponentSet() error = %v", err)
	}

	envText, err := os.ReadFile(filepath.Join(projectDir, ".env"))
	if err != nil {
		t.Fatalf("ReadFile(.env) error = %v", err)
	}
	if !strings.Contains(string(envText), "URI_SCHEME=\"http\"") {
		t.Fatalf("expected URI_SCHEME to be http, got:\n%s", string(envText))
	}
}

func TestResolveComponentSetStateValueUsesFlag(t *testing.T) {
	oldState := componentSetState
	t.Cleanup(func() {
		componentSetState = oldState
	})

	cmd := &cobra.Command{Use: "set"}
	cmd.Flags().String("state", "", "")
	componentSetState = "off"
	if err := cmd.Flags().Set("state", "off"); err != nil {
		t.Fatalf("Flags().Set(state) error = %v", err)
	}

	value, err := resolveComponentSetStateValue(cmd, []string{"fcrepo"})
	if err != nil {
		t.Fatalf("resolveComponentSetStateValue() error = %v", err)
	}
	if value != "off" {
		t.Fatalf("expected off, got %q", value)
	}
}

func TestResolveComponentSetStateValueRejectsDuplicateSources(t *testing.T) {
	oldState := componentSetState
	t.Cleanup(func() {
		componentSetState = oldState
	})

	cmd := &cobra.Command{Use: "set"}
	cmd.Flags().String("state", "", "")
	componentSetState = "off"
	if err := cmd.Flags().Set("state", "off"); err != nil {
		t.Fatalf("Flags().Set(state) error = %v", err)
	}

	_, err := resolveComponentSetStateValue(cmd, []string{"fcrepo", "on"})
	if err == nil {
		t.Fatal("expected duplicate state source error")
	}
}

func TestRunComponentSetPromptsForStateWhenMissing(t *testing.T) {
	projectDir := t.TempDir()
	writeISLEOnFixture(t, projectDir)

	oldStatusPath := statusPath
	oldDrupalRootfs := statusDrupalRootfs
	oldYolo := componentSetYolo
	oldApply := componentApplyOptions
	oldPromptState := componentPromptState
	oldPromptChoice := componentPromptChoice
	oldInput := componentSetInput
	oldState := componentSetState
	t.Cleanup(func() {
		statusPath = oldStatusPath
		statusDrupalRootfs = oldDrupalRootfs
		componentSetYolo = oldYolo
		componentApplyOptions = oldApply
		componentPromptState = oldPromptState
		componentPromptChoice = oldPromptChoice
		componentSetInput = oldInput
		componentSetState = oldState
	})

	statusPath = projectDir
	statusDrupalRootfs = createpkg.DefaultDrupalRootfs
	componentSetYolo = false
	componentSetState = ""

	var promptedName string
	componentPromptState = func(name string, guidance corecomponent.StateGuidance, input corecomponent.InputFunc) (corecomponent.State, error) {
		promptedName = name
		if !strings.Contains(guidance.Question, "Current disposition: `enabled`") {
			t.Fatalf("expected current disposition in prompt, got:\n%s", guidance.Question)
		}
		return corecomponent.StateOff, nil
	}
	componentSetInput = func(question ...string) (string, error) {
		return "y", nil
	}

	var got createpkg.Options
	componentApplyOptions = func(opts createpkg.Options) error {
		got = opts
		return nil
	}

	if err := runComponentSet(componentSetCmd, "fcrepo", ""); err != nil {
		t.Fatalf("runComponentSet() error = %v", err)
	}

	if promptedName != "fcrepo" {
		t.Fatalf("expected fcrepo prompt, got %q", promptedName)
	}
	if got.Fcrepo != createpkg.FcrepoStateOff {
		t.Fatalf("expected prompted off state to be applied, got %q", got.Fcrepo)
	}
}

func TestRunComponentSetPromptsForStateAndTLSModeWhenMissing(t *testing.T) {
	projectDir := t.TempDir()
	writeISLEOnFixture(t, projectDir)

	oldStatusPath := statusPath
	oldDrupalRootfs := statusDrupalRootfs
	oldYolo := componentSetYolo
	oldTLSMode := componentSetTLSMode
	oldPromptState := componentPromptState
	oldPromptChoice := componentPromptChoice
	oldInput := componentSetInput
	oldState := componentSetState
	t.Cleanup(func() {
		statusPath = oldStatusPath
		statusDrupalRootfs = oldDrupalRootfs
		componentSetYolo = oldYolo
		componentSetTLSMode = oldTLSMode
		componentPromptState = oldPromptState
		componentPromptChoice = oldPromptChoice
		componentSetInput = oldInput
		componentSetState = oldState
	})

	statusPath = projectDir
	statusDrupalRootfs = createpkg.DefaultDrupalRootfs
	componentSetYolo = false
	componentSetTLSMode = ""
	componentSetState = ""

	var promptedStateName string
	var promptedModeName string
	componentPromptState = func(name string, guidance corecomponent.StateGuidance, input corecomponent.InputFunc) (corecomponent.State, error) {
		promptedStateName = name
		return corecomponent.StateOn, nil
	}
	componentPromptChoice = func(name string, choices []corecomponent.Choice, defaultValue string, input corecomponent.InputFunc, sections ...string) (string, error) {
		promptedModeName = name
		return traefikconfig.ModeLetsEncrypt, nil
	}
	componentSetInput = func(question ...string) (string, error) {
		return "y", nil
	}

	if err := runComponentSet(componentSetCmd, "isle-tls", ""); err != nil {
		t.Fatalf("runComponentSet() error = %v", err)
	}

	if promptedStateName != "isle-tls" {
		t.Fatalf("expected state prompt for isle-tls, got %q", promptedStateName)
	}
	if promptedModeName != "isle-tls-tls-mode" {
		t.Fatalf("expected tls mode prompt for isle-tls, got %q", promptedModeName)
	}

	envText, err := os.ReadFile(filepath.Join(projectDir, ".env"))
	if err != nil {
		t.Fatalf("ReadFile(.env) error = %v", err)
	}
	if !strings.Contains(string(envText), "TLS_PROVIDER=\"letsencrypt\"") {
		t.Fatalf("expected letsencrypt env after prompts, got:\n%s", string(envText))
	}
}

func writeFileForTest(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", path, err)
	}
}

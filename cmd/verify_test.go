package cmd

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/libops/sitectl/pkg/config"
	sitevalidate "github.com/libops/sitectl/pkg/validate"
)

func TestVerifyBotMitigationSkipsForwardedHeaderProbeWhenIngressTrustedIPsUnset(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	writeVerifyCompose(t, projectDir, `services:
  traefik:
    command:
      - --experimental.localPlugins.captcha-protect.modulename=github.com/libops/captcha-protect
    volumes:
      - ./conf/traefik/plugins/captcha-protect:/plugins-local/src/github.com/libops/captcha-protect:r
`)

	results := verifyBotMitigation(context.Background(), verifyTestContext(projectDir), projectDir, "on")
	if len(results) != 1 {
		t.Fatalf("expected one result, got %#v", results)
	}
	result := results[0]
	if result.Status != sitevalidate.StatusOK {
		t.Fatalf("expected ok result, got %#v", result)
	}
	if !strings.Contains(result.Detail, "skipped X-Forwarded-For challenge probe") {
		t.Fatalf("expected skipped probe detail, got %#v", result)
	}
}

func TestBotMitigationForwardedHeaderProbeEnabledDetectsIngressTrustedIPs(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	writeVerifyCompose(t, projectDir, `services:
  traefik:
    command:
      - --entryPoints.http.forwardedHeaders.trustedIPs=127.0.0.1/32
      - --experimental.localPlugins.captcha-protect.modulename=github.com/libops/captcha-protect
`)

	if !botMitigationForwardedHeaderProbeEnabled(verifyTestContext(projectDir)) {
		t.Fatal("expected forwarded-header probe to be enabled when ingress trusted IPs are configured")
	}
}

func TestValidateDemoObjectsScriptContractAcceptsManagedInternalRoute(t *testing.T) {
	t.Parallel()

	input := `#!/usr/bin/env bash
set -eou pipefail
URL="${SITECTL_DEMO_OBJECTS_URL:-$(site_url)}"
WORKBENCH_URL="$(container_url_for_url "${URL}")"
NETWORK="$(container_network_for_url "${WORKBENCH_URL}")"
`
	if err := validateDemoObjectsScriptContract([]byte(input)); err != nil {
		t.Fatalf("validateDemoObjectsScriptContract() error = %v", err)
	}
}

func TestValidateDemoObjectsScriptContractRejectsLegacyContractActionably(t *testing.T) {
	t.Parallel()

	input := `#!/usr/bin/env bash
set -eou pipefail
URL="${URI_SCHEME}://${DOMAIN}"
`
	err := validateDemoObjectsScriptContract([]byte(input))
	if err == nil {
		t.Fatal("expected legacy contract failure")
	}
	for _, want := range []string{"legacy runtime contract", "https://github.com/libops/isle", "v1.3.1"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("expected error to contain %q, got %v", want, err)
		}
	}
}

func TestRunDemoObjectsScriptRejectsLegacyContractBeforeRuntimeWork(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	scriptsDir := filepath.Join(projectDir, "scripts")
	if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(scripts) error = %v", err)
	}
	legacy := []byte("#!/usr/bin/env bash\nURL=\"${URI_SCHEME}://${DOMAIN}\"\n")
	if err := os.WriteFile(filepath.Join(scriptsDir, "demo-objects.sh"), legacy, 0o644); err != nil {
		t.Fatalf("WriteFile(demo-objects.sh) error = %v", err)
	}
	if _, err := runDemoObjectsScript(t.Context(), projectDir); err == nil || !strings.Contains(err.Error(), "legacy runtime contract") {
		t.Fatalf("runDemoObjectsScript() error = %v, want an early legacy contract failure", err)
	}
}

func TestVerifyNoFedoraManagedFilesUsesDirectDrushArgv(t *testing.T) {
	oldRun := verifyRunLocalProjectOutput
	t.Cleanup(func() {
		verifyRunLocalProjectOutput = oldRun
	})

	var gotName string
	var gotArgs []string
	verifyRunLocalProjectOutput = func(ctx context.Context, projectDir, name string, args ...string) (string, error) {
		gotName = name
		gotArgs = append([]string{}, args...)
		return "0\n", nil
	}

	result := verifyNoFedoraManagedFiles(context.Background(), t.TempDir())
	if result.Status != sitevalidate.StatusOK {
		t.Fatalf("verifyNoFedoraManagedFiles() = %#v", result)
	}
	wantArgs := []string{
		"compose", "exec", "-T", "--workdir", "/var/www/drupal", "drupal",
		"drush", "--root=/var/www/drupal", "sql:query", "--extra=--skip-column-names",
		"SELECT COUNT(*) FROM file_managed WHERE uri LIKE 'fedora%';",
	}
	if gotName != "docker" || !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Fatalf("command = %q %#v, want docker %#v", gotName, gotArgs, wantArgs)
	}
}

func TestCountContainerFilesUsesDirectFindArgv(t *testing.T) {
	oldRun := verifyRunLocalProjectOutput
	t.Cleanup(func() {
		verifyRunLocalProjectOutput = oldRun
	})

	var gotName string
	var gotArgs []string
	verifyRunLocalProjectOutput = func(ctx context.Context, projectDir, name string, args ...string) (string, error) {
		gotName = name
		gotArgs = append([]string{}, args...)
		return "first\x00second\x00", nil
	}

	count, err := countContainerFiles(context.Background(), t.TempDir(), "drupal", "/var/www/drupal/private")
	if err != nil {
		t.Fatalf("countContainerFiles() error = %v", err)
	}
	if count != 2 {
		t.Fatalf("countContainerFiles() = %d, want 2", count)
	}
	wantArgs := []string{"compose", "exec", "-T", "drupal", "find", "/var/www/drupal/private", "-type", "f", "-print0"}
	if gotName != "docker" || !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Fatalf("command = %q %#v, want docker %#v", gotName, gotArgs, wantArgs)
	}
}

func TestVerifyRuntimeDoesNotMaterializeCompatibilityPrograms(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile("verify.go")
	if err != nil {
		t.Fatalf("ReadFile(verify.go) error = %v", err)
	}
	source := string(data)
	for _, forbidden := range []string{
		"prepareDemoObjectsScript(",
		"materializeDemoObjectsScript(",
		"os.CreateTemp(",
		`"bash", "-lc"`,
		"| wc -l",
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("verify runtime must invoke checked-in programs and direct argv instead of containing %q", forbidden)
		}
	}
}

func verifyTestContext(projectDir string) *config.Context {
	return &config.Context{
		DockerHostType: config.ContextLocal,
		ProjectDir:     projectDir,
	}
}

func writeVerifyCompose(t *testing.T, projectDir, contents string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(projectDir, "compose.yaml"), []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile(compose.yaml) error = %v", err)
	}
}

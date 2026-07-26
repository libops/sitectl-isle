package cmd

import (
	"context"
	"os"
	"path/filepath"
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

func TestPrepareDemoObjectsScriptUsesInternalRoute(t *testing.T) {
	t.Parallel()

	input := `#!/usr/bin/env bash
set -eou pipefail
source "$(dirname "${BASH_SOURCE[0]}")/profile.sh"
URL="${URI_SCHEME}://${DOMAIN}"
if [ "${URI_PORT}" != "80" ] && [ "${URI_PORT}" != "443" ]; then
  URL="${URL}:${URI_PORT}"
fi
`
	prepared, err := prepareDemoObjectsScript([]byte(input))
	if err != nil {
		t.Fatalf("prepareDemoObjectsScript() error = %v", err)
	}
	for _, want := range []string{
		`source "./scripts/profile.sh"`,
		`URL="${SITECTL_DEMO_OBJECTS_URL:-${URI_SCHEME}://${DOMAIN}}"`,
		`if [ -z "${SITECTL_DEMO_OBJECTS_URL:-}" ]`,
	} {
		if !strings.Contains(prepared, want) {
			t.Fatalf("expected prepared script to contain %q, got:\n%s", want, prepared)
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
	if err := os.WriteFile(filepath.Join(projectDir, "docker-compose.yml"), []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile(docker-compose.yml) error = %v", err)
	}
}

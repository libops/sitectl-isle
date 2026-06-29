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

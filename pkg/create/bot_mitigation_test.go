package create

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpdateComposeForBotMitigationPreservesMergeAndFoldedCommand(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	composePath := filepath.Join(projectDir, "docker-compose.yml")
	compose := `---
x-common: &common
  restart: unless-stopped
services:
  traefik:
    <<: *common
    command: >-
      --ping=true
      --log.level=INFO
      --entryPoints.http.address=:80
      --api.debug=${DEVELOPMENT_ENVIRONMENT:-false}
    volumes:
      - ./conf/traefik:/etc/traefik:ro
    environment:
      URI_SCHEME: ${URI_SCHEME:-http}
`
	if err := os.WriteFile(composePath, []byte(compose), 0o644); err != nil {
		t.Fatalf("WriteFile(compose) error = %v", err)
	}

	if err := updateComposeForBotMitigation(projectDir, true); err != nil {
		t.Fatalf("updateComposeForBotMitigation(on) error = %v", err)
	}

	updated, err := os.ReadFile(composePath)
	if err != nil {
		t.Fatalf("ReadFile(compose) error = %v", err)
	}
	rendered := string(updated)
	for _, want := range []string{
		"<<: *common",
		"command: >-",
		"      --ping=true\n      --log.level=INFO\n      --entryPoints.http.address=:80",
		"      --experimental.localPlugins.captcha-protect.modulename=github.com/libops/captcha-protect",
		"      - ./conf/traefik:/etc/traefik:ro",
		"      - ./conf/traefik/plugins/captcha-protect:/plugins-local/src/github.com/libops/captcha-protect:r",
		"      TURNSTILE_SITE_KEY: ${TURNSTILE_SITE_KEY:-1x00000000000000000000AA}",
		"      TURNSTILE_SECRET_KEY: ${TURNSTILE_SECRET_KEY:-1x0000000000000000000000000000000AA}",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("expected compose to contain %q, got:\n%s", want, rendered)
		}
	}
	if strings.Contains(rendered, "!!merge") {
		t.Fatalf("expected compose not to contain yaml merge tags, got:\n%s", rendered)
	}

	if err := updateComposeForBotMitigation(projectDir, false); err != nil {
		t.Fatalf("updateComposeForBotMitigation(off) error = %v", err)
	}
	disabled, err := os.ReadFile(composePath)
	if err != nil {
		t.Fatalf("ReadFile(disabled compose) error = %v", err)
	}
	disabledText := string(disabled)
	if strings.Contains(disabledText, "captcha-protect") || strings.Contains(disabledText, "TURNSTILE_SITE_KEY") || strings.Contains(disabledText, "TURNSTILE_SECRET_KEY") {
		t.Fatalf("expected bot mitigation settings removed, got:\n%s", disabledText)
	}
	if !strings.Contains(disabledText, "<<: *common") || !strings.Contains(disabledText, "command: >-") {
		t.Fatalf("expected merge key and folded command preserved after disable, got:\n%s", disabledText)
	}
}

func TestUpdateDrupalTraefikForBotMitigationMovesMiddlewareDefinitionToBottom(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	traefikPath := filepath.Join(projectDir, "drupal.yml")
	traefik := `http:
  services:
    drupal:
      loadBalancer:
        servers:
          - url: http://drupal:80
  middlewares:
    existing:
      headers: {}
  routers:
    drupal:
      rule: Host("example.test")
      service: drupal
{{ if eq (env "TLS_PROVIDER") "letsencrypt" }}
  tls:
    options:
      default: {}
{{ end }}
`
	if err := os.WriteFile(traefikPath, []byte(traefik), 0o644); err != nil {
		t.Fatalf("WriteFile(drupal.yml) error = %v", err)
	}

	if err := updateDrupalTraefikForBotMitigation(traefikPath, true); err != nil {
		t.Fatalf("updateDrupalTraefikForBotMitigation(on) error = %v", err)
	}
	if err := updateDrupalTraefikForBotMitigation(traefikPath, true); err != nil {
		t.Fatalf("second updateDrupalTraefikForBotMitigation(on) error = %v", err)
	}

	updated, err := os.ReadFile(traefikPath)
	if err != nil {
		t.Fatalf("ReadFile(drupal.yml) error = %v", err)
	}
	rendered := string(updated)
	definitionIdx := strings.LastIndex(rendered, "  middlewares:\n    existing:")
	templateEndIdx := strings.Index(rendered, "{{ end }}")
	if definitionIdx == -1 {
		t.Fatalf("expected middleware definition at bottom, got:\n%s", rendered)
	}
	if templateEndIdx == -1 {
		t.Fatalf("expected template end marker preserved, got:\n%s", rendered)
	}
	if definitionIdx < templateEndIdx {
		t.Fatalf("expected middleware definition after template block, got:\n%s", rendered)
	}
	for _, want := range []string{
		"      middlewares:\n        - captcha-protect",
		"  middlewares:\n    existing:\n      headers: {}",
		"    captcha-protect:\n      plugin:\n        captcha-protect:",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("expected drupal traefik config to contain %q, got:\n%s", want, rendered)
		}
	}
	if got := strings.Count(rendered, "\n        - captcha-protect\n"); got != 1 {
		t.Fatalf("expected one router captcha-protect reference, got %d:\n%s", got, rendered)
	}
	if got := strings.Count(rendered, "\n    captcha-protect:\n"); got != 1 {
		t.Fatalf("expected one captcha-protect middleware definition, got %d:\n%s", got, rendered)
	}
}

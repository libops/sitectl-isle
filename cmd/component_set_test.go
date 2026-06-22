package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	createpkg "github.com/libops/sitectl-isle/pkg/create"
	"github.com/libops/sitectl-isle/pkg/traefikconfig"
	corecomponent "github.com/libops/sitectl/pkg/component"
	"github.com/spf13/cobra"
)

func newComponentSetTestCommand() *cobra.Command {
	var path string
	var codebaseRootfs string
	var drupalRootfs string
	var state string
	var disposition string
	var yolo bool
	var tlsMode string

	cmd := &cobra.Command{Use: "set <name> [disposition]"}
	cmd.Flags().StringVar(&path, "path", "", "Path to the checked out isle-site-template project. Defaults to the active sitectl context project directory")
	addCodebaseRootfsFlags(cmd, &codebaseRootfs, &drupalRootfs, createpkg.DefaultDrupalRootfs)
	cmd.Flags().StringVar(&state, "state", "", "Component state to apply. Valid values are on or off. If omitted, the command prompts interactively.")
	cmd.Flags().StringVar(&disposition, "disposition", "", "Component disposition to apply. Valid values depend on the component, commonly disabled, superceded, enabled, or distributed.")
	cmd.Flags().BoolVar(&yolo, "yolo", false, "Apply the component change without confirmation")
	cmd.Flags().StringVar(&tlsMode, "tls-mode", "", "TLS mode for the selected component. Valid values are http, self-managed, mkcert, or letsencrypt.")
	addComponentSetFollowUpFlags(cmd, managedComponentDefinitions())
	return cmd
}

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
	cmd := newComponentSetTestCommand()
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

	fieldPath := filepath.Join(projectDir, createpkg.DefaultDrupalRootfs, "config", "sync", "field.storage.media.field_media_file.yml")
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

	if err := runComponentSet(newComponentSetTestCommand(), "fcrepo", "off"); err != nil {
		t.Fatalf("runComponentSet() error = %v", err)
	}
	if got.ISLEFileSystemURI != "archive" {
		t.Fatalf("expected archive filesystem uri, got %q", got.ISLEFileSystemURI)
	}
}

func TestIsleSetRunnerPropagatesCodebaseRootfsAliases(t *testing.T) {
	tests := []struct {
		name string
		flag string
	}{
		{name: "codebase rootfs", flag: "codebase-rootfs"},
		{name: "drupal rootfs alias", flag: "drupal-rootfs"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			projectDir := t.TempDir()
			writeISLEOnFixture(t, projectDir)
			customRootfs := "custom/rootfs"
			if err := os.MkdirAll(filepath.Join(projectDir, "custom"), 0o755); err != nil {
				t.Fatalf("MkdirAll(custom) error = %v", err)
			}
			if err := os.Rename(filepath.Join(projectDir, createpkg.DefaultDrupalRootfs), filepath.Join(projectDir, customRootfs)); err != nil {
				t.Fatalf("Rename(default rootfs) error = %v", err)
			}

			oldStatusPath := statusPath
			oldCodebaseRootfs := statusCodebaseRootfs
			oldDrupalRootfs := statusDrupalRootfs
			oldYolo := componentSetYolo
			oldApply := componentApplyOptions
			oldPromptChoice := componentPromptChoice
			t.Cleanup(func() {
				statusPath = oldStatusPath
				statusCodebaseRootfs = oldCodebaseRootfs
				statusDrupalRootfs = oldDrupalRootfs
				componentSetYolo = oldYolo
				componentApplyOptions = oldApply
				componentPromptChoice = oldPromptChoice
			})

			var got createpkg.Options
			componentApplyOptions = func(opts createpkg.Options) error {
				got = opts
				return nil
			}
			componentSetYolo = true
			statusCodebaseRootfs = ""
			statusDrupalRootfs = ""

			runner := &isleSetRunner{}
			cmd := &cobra.Command{Use: "set"}
			runner.BindFlags(cmd)
			if err := cmd.Flags().Set("path", projectDir); err != nil {
				t.Fatalf("Flags().Set(path) error = %v", err)
			}
			if err := cmd.Flags().Set(tt.flag, customRootfs); err != nil {
				t.Fatalf("Flags().Set(%s) error = %v", tt.flag, err)
			}
			if err := cmd.Flags().Set("yolo", "true"); err != nil {
				t.Fatalf("Flags().Set(yolo) error = %v", err)
			}

			if err := runner.Run(cmd, []string{"fcrepo", "off"}, nil); err != nil {
				t.Fatalf("Run() error = %v", err)
			}

			if statusCodebaseRootfs != "" || statusDrupalRootfs != "" {
				t.Fatalf("expected runner path not to mutate rootfs globals, got codebase=%q drupal=%q", statusCodebaseRootfs, statusDrupalRootfs)
			}
			if got.DrupalRootfs != customRootfs {
				t.Fatalf("expected apply DrupalRootfs %q, got %q", customRootfs, got.DrupalRootfs)
			}
		})
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

	err := runComponentSet(newComponentSetTestCommand(), "fcrepo", "off")
	if err == nil {
		t.Fatal("expected drift error")
	}
	if !strings.Contains(err.Error(), `component "blazegraph" is drifted`) {
		t.Fatalf("expected blazegraph drift error, got %v", err)
	}
	if !strings.Contains(err.Error(), "docker-compose.yml") || !strings.Contains(err.Error(), "DRUPAL_DEFAULT_TRIPLESTORE_NAMESPACE") {
		t.Fatalf("expected drift error to include failed file/check detail, got %v", err)
	}
}

func TestRunComponentSetIIIFIgnoresOtherComponentDrift(t *testing.T) {
	projectDir := t.TempDir()
	writeISLEOnFixture(t, projectDir)

	configDir := filepath.Join(projectDir, createpkg.DefaultDrupalRootfs, "config", "sync")
	if err := os.Remove(filepath.Join(configDir, "system.action.index_media_in_triplestore.yml")); err != nil {
		t.Fatalf("Remove(triplestore action) error = %v", err)
	}

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

	var out bytes.Buffer
	cmd := newComponentSetTestCommand()
	cmd.SetOut(&out)

	if err := runComponentSet(cmd, "iiif", "triplet"); err != nil {
		t.Fatalf("runComponentSet() error = %v", err)
	}
	if !strings.Contains(out.String(), "iiif: triplet") {
		t.Fatalf("expected command output, got:\n%s", out.String())
	}

	composeText, err := os.ReadFile(filepath.Join(projectDir, "docker-compose.yml"))
	if err != nil {
		t.Fatalf("ReadFile(docker-compose.yml) error = %v", err)
	}
	compose := string(composeText)
	if !strings.Contains(compose, "\n  triplet:\n") || strings.Contains(compose, "\n  cantaloupe:\n") {
		t.Fatalf("expected triplet to replace cantaloupe, got:\n%s", compose)
	}
	if !strings.Contains(compose, "\n  blazegraph:\n") || !strings.Contains(compose, "DRUPAL_DEFAULT_TRIPLESTORE_NAMESPACE: islandora") {
		t.Fatalf("expected blazegraph compose defaults preserved, got:\n%s", compose)
	}
	if _, err := os.Stat(filepath.Join(configDir, "system.action.index_media_in_triplestore.yml")); !os.IsNotExist(err) {
		t.Fatalf("expected unrelated blazegraph drift to remain untouched, stat err=%v", err)
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
	cmd := newComponentSetTestCommand()
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

func TestRunComponentSetEnablesBotMitigation(t *testing.T) {
	projectDir := t.TempDir()
	writeISLEOnFixture(t, projectDir)
	writeFileForTest(t, filepath.Join(projectDir, "conf", "traefik", "drupal.yml"), `http:
  services:
    drupal:
      loadBalancer:
        servers:
          - url: {{ env "DRUPAL_UPSTREAM_URL" }}
  routers:
    drupal:
      rule: Host(`+"`"+`{{ env "DOMAIN" }}`+"`"+`)
      service: drupal
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

	var out bytes.Buffer
	cmd := newComponentSetTestCommand()
	cmd.SetOut(&out)

	if err := runComponentSet(cmd, "bot-mitigation", "on"); err != nil {
		t.Fatalf("runComponentSet() error = %v", err)
	}
	if !strings.Contains(out.String(), "bot-mitigation: enabled") {
		t.Fatalf("expected component output, got:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "Configure real TURNSTILE_SITE_KEY and TURNSTILE_SECRET_KEY values from Cloudflare") {
		t.Fatalf("expected Turnstile key warning, got:\n%s", out.String())
	}

	composeText, err := os.ReadFile(filepath.Join(projectDir, "docker-compose.yml"))
	if err != nil {
		t.Fatalf("ReadFile(docker-compose.yml) error = %v", err)
	}
	compose := string(composeText)
	for _, want := range []string{
		"--experimental.localPlugins.captcha-protect.modulename=github.com/libops/captcha-protect",
		"./conf/traefik/plugins/captcha-protect:/plugins-local/src/github.com/libops/captcha-protect:r",
		"./conf/traefik/challenge.tmpl.html:/challenge.tmpl.html:ro",
		`TURNSTILE_SITE_KEY: "${TURNSTILE_SITE_KEY:-1x00000000000000000000AA}"`,
		`TURNSTILE_SECRET_KEY: "${TURNSTILE_SECRET_KEY:-1x0000000000000000000000000000000AA}"`,
	} {
		if !strings.Contains(compose, want) {
			t.Fatalf("expected compose to contain %q, got:\n%s", want, compose)
		}
	}

	traefikText, err := os.ReadFile(filepath.Join(projectDir, "conf", "traefik", "drupal.yml"))
	if err != nil {
		t.Fatalf("ReadFile(conf/traefik/drupal.yml) error = %v", err)
	}
	traefik := string(traefikText)
	for _, want := range []string{
		"      middlewares:\n        - captcha-protect",
		"    captcha-protect:\n      plugin:\n        captcha-protect:",
		`{{ env "DRUPAL_UPSTREAM_URL" }}`,
		"          siteKey: '{{ env \"TURNSTILE_SITE_KEY\" }}'",
		"          secretKey: '{{ env \"TURNSTILE_SECRET_KEY\" }}'",
		"          protectFileExtensions: php,html,jp2,tif,tiff",
	} {
		if !strings.Contains(traefik, want) {
			t.Fatalf("expected drupal traefik config to contain %q, got:\n%s", want, traefik)
		}
	}
	templateText, err := os.ReadFile(filepath.Join(projectDir, "conf", "traefik", "challenge.tmpl.html"))
	if err != nil {
		t.Fatalf("ReadFile(conf/traefik/challenge.tmpl.html) error = %v", err)
	}
	if !strings.Contains(string(templateText), `{{ .FrontendJS }}`) {
		t.Fatalf("expected plugin challenge template, got:\n%s", string(templateText))
	}
}

func TestRunComponentSetDisablesBotMitigation(t *testing.T) {
	projectDir := t.TempDir()
	writeISLEOnFixture(t, projectDir)

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

	cmd := newComponentSetTestCommand()
	if err := runComponentSet(cmd, "bot-mitigation", "on"); err != nil {
		t.Fatalf("runComponentSet(on) error = %v", err)
	}
	if err := runComponentSet(cmd, "bot-mitigation", "off"); err != nil {
		t.Fatalf("runComponentSet(off) error = %v", err)
	}

	composeText, err := os.ReadFile(filepath.Join(projectDir, "docker-compose.yml"))
	if err != nil {
		t.Fatalf("ReadFile(docker-compose.yml) error = %v", err)
	}
	compose := string(composeText)
	for _, removed := range []string{
		"captcha-protect",
		"TURNSTILE_SITE_KEY",
		"TURNSTILE_SECRET_KEY",
	} {
		if strings.Contains(compose, removed) {
			t.Fatalf("expected compose to remove %q, got:\n%s", removed, compose)
		}
	}

	traefikText, err := os.ReadFile(filepath.Join(projectDir, "conf", "traefik", "drupal.yml"))
	if err != nil {
		t.Fatalf("ReadFile(conf/traefik/drupal.yml) error = %v", err)
	}
	if strings.Contains(string(traefikText), "captcha-protect") {
		t.Fatalf("expected drupal traefik config to remove captcha-protect, got:\n%s", string(traefikText))
	}
}

func TestRunComponentSetDistributesCantaloupeIIIF(t *testing.T) {
	projectDir := t.TempDir()
	writeISLEOnFixture(t, projectDir)

	oldStatusPath := statusPath
	oldDrupalRootfs := statusDrupalRootfs
	oldYolo := componentSetYolo
	oldInput := componentSetInput
	oldPromptChoice := componentPromptChoice
	cmd := newComponentSetTestCommand()
	t.Cleanup(func() {
		statusPath = oldStatusPath
		statusDrupalRootfs = oldDrupalRootfs
		componentSetYolo = oldYolo
		componentSetInput = oldInput
		componentPromptChoice = oldPromptChoice
		if flag := cmd.Flags().Lookup("iiif-upstream-url"); flag != nil {
			flag.Changed = false
		}
	})

	statusPath = projectDir
	statusDrupalRootfs = createpkg.DefaultDrupalRootfs
	componentSetYolo = true
	_ = cmd.Flags().Set("iiif-upstream-url", "http://cantaloupe.example:8182")

	var out bytes.Buffer
	cmd.SetOut(&out)

	if err := runComponentSet(cmd, "iiif-topology", "distributed"); err != nil {
		t.Fatalf("runComponentSet() error = %v", err)
	}

	composeText, err := os.ReadFile(filepath.Join(projectDir, "docker-compose.yml"))
	if err != nil {
		t.Fatalf("ReadFile(docker-compose.yml) error = %v", err)
	}
	if strings.Contains(string(composeText), "\n  cantaloupe:\n") {
		t.Fatalf("expected base cantaloupe removed, got:\n%s", string(composeText))
	}
	if strings.Contains(string(composeText), "\n  cantaloupe-data:") {
		t.Fatalf("expected base cantaloupe volume removed, got:\n%s", string(composeText))
	}

	overrideText, err := os.ReadFile(filepath.Join(projectDir, "docker-compose.local.yml"))
	if err != nil {
		t.Fatalf("ReadFile(docker-compose.local.yml) error = %v", err)
	}
	if !strings.Contains(string(overrideText), "cantaloupe:") || !strings.Contains(string(overrideText), "8182:8182") || !strings.Contains(string(overrideText), "cantaloupe-data:") {
		t.Fatalf("expected cantaloupe local override, got:\n%s", string(overrideText))
	}
	if !strings.Contains(string(overrideText), "IIIF_UPSTREAM_URL: \"http://cantaloupe:8182\"") {
		t.Fatalf("expected local IIIF upstream override, got:\n%s", string(overrideText))
	}

	traefikText, err := os.ReadFile(filepath.Join(projectDir, "conf", "traefik", "cantaloupe.yml"))
	if err != nil {
		t.Fatalf("ReadFile(cantaloupe.yml) error = %v", err)
	}
	if !strings.Contains(string(traefikText), `{{ env "IIIF_UPSTREAM_URL" }}`) {
		t.Fatalf("expected templated upstream in traefik config, got:\n%s", string(traefikText))
	}
	if strings.Contains(string(traefikText), "http://cantaloupe.example:8182") {
		t.Fatalf("expected traefik config to avoid hard-coded upstream, got:\n%s", string(traefikText))
	}
	if !strings.Contains(string(composeText), "IIIF_UPSTREAM_URL: \"http://cantaloupe.example:8182\"") {
		t.Fatalf("expected base traefik upstream env, got:\n%s", string(composeText))
	}
	if !strings.Contains(string(composeText), "DRUPAL_DEFAULT_CANTALOUPE_URL: \"http://cantaloupe.example:8182\"") {
		t.Fatalf("expected drupal iiif URL to point at upstream, got:\n%s", string(composeText))
	}

	devComposeText, err := os.ReadFile(filepath.Join(projectDir, "docker-compose.dev.yml"))
	if err != nil {
		t.Fatalf("ReadFile(docker-compose.dev.yml) error = %v", err)
	}
	if !strings.Contains(string(devComposeText), "\n  cantaloupe:\n") {
		t.Fatalf("expected dev compose to include local cantaloupe, got:\n%s", string(devComposeText))
	}
	if !strings.Contains(string(devComposeText), "IIIF_UPSTREAM_URL: \"http://cantaloupe:8182\"") {
		t.Fatalf("expected dev compose to reset IIIF upstream, got:\n%s", string(devComposeText))
	}
	if !strings.Contains(string(devComposeText), "DRUPAL_DEFAULT_CANTALOUPE_URL: \"${URI_SCHEME}://${DOMAIN}/cantaloupe/iiif/2\"") {
		t.Fatalf("expected dev compose to reset Drupal IIIF URL, got:\n%s", string(devComposeText))
	}
	if !strings.Contains(out.String(), "iiif-topology: distributed (http://cantaloupe.example:8182)") {
		t.Fatalf("expected command output, got:\n%s", out.String())
	}
}

func TestRunComponentSetRejectsInvalidDistributedIIIFURL(t *testing.T) {
	projectDir := t.TempDir()
	writeISLEOnFixture(t, projectDir)

	oldStatusPath := statusPath
	oldDrupalRootfs := statusDrupalRootfs
	oldYolo := componentSetYolo
	cmd := newComponentSetTestCommand()
	t.Cleanup(func() {
		statusPath = oldStatusPath
		statusDrupalRootfs = oldDrupalRootfs
		componentSetYolo = oldYolo
		if flag := cmd.Flags().Lookup("iiif-upstream-url"); flag != nil {
			flag.Changed = false
		}
	})

	statusPath = projectDir
	statusDrupalRootfs = createpkg.DefaultDrupalRootfs
	componentSetYolo = true
	_ = cmd.Flags().Set("iiif-upstream-url", "ok")

	err := runComponentSet(cmd, "iiif-topology", "distributed")
	if err == nil {
		t.Fatal("expected invalid upstream url error")
	}
	if !strings.Contains(err.Error(), "invalid external IIIF upstream URL") {
		t.Fatalf("expected invalid upstream url error, got %v", err)
	}
}

func TestRunComponentSetDistributesDerivativeService(t *testing.T) {
	projectDir := t.TempDir()
	writeISLEOnFixture(t, projectDir)
	addDerivativeServiceFixture(t, projectDir, "homarus")

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

	var out bytes.Buffer
	cmd := newComponentSetTestCommand()
	cmd.SetOut(&out)

	if err := runComponentSet(cmd, "homarus", "distributed"); err != nil {
		t.Fatalf("runComponentSet() error = %v", err)
	}
	if !strings.Contains(out.String(), "homarus: distributed") {
		t.Fatalf("expected command output, got:\n%s", out.String())
	}

	composeText, err := os.ReadFile(filepath.Join(projectDir, "docker-compose.yml"))
	if err != nil {
		t.Fatalf("ReadFile(docker-compose.yml) error = %v", err)
	}
	compose := string(composeText)
	if strings.Contains(compose, "\n  homarus:\n") {
		t.Fatalf("expected homarus service removed, got:\n%s", compose)
	}
	if !strings.Contains(compose, `ALPACA_DERIVATIVE_HOMARUS_URL: "https://microservices.libops.site/homarus"`) {
		t.Fatalf("expected alpaca homarus URL override, got:\n%s", compose)
	}

	devComposeText, err := os.ReadFile(filepath.Join(projectDir, "docker-compose.dev.yml"))
	if err != nil {
		t.Fatalf("ReadFile(docker-compose.dev.yml) error = %v", err)
	}
	devCompose := string(devComposeText)
	if !strings.Contains(devCompose, "\n  homarus:\n") {
		t.Fatalf("expected dev compose to include local homarus, got:\n%s", devCompose)
	}
	if !strings.Contains(devCompose, `ALPACA_DERIVATIVE_HOMARUS_URL: "http://homarus:8080/"`) {
		t.Fatalf("expected dev compose to reset homarus URL, got:\n%s", devCompose)
	}
}

func TestRunComponentSetDistributesFITSAndKeepsLocalCrayfitsPointedAtManagedFITS(t *testing.T) {
	projectDir := t.TempDir()
	writeISLEOnFixture(t, projectDir)
	addDerivativeServiceFixture(t, projectDir, "crayfits")
	addDerivativeServiceFixture(t, projectDir, "fits")

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

	if err := runComponentSet(newComponentSetTestCommand(), "fits", "distributed"); err != nil {
		t.Fatalf("runComponentSet() error = %v", err)
	}

	composeText, err := os.ReadFile(filepath.Join(projectDir, "docker-compose.yml"))
	if err != nil {
		t.Fatalf("ReadFile(docker-compose.yml) error = %v", err)
	}
	compose := string(composeText)
	if strings.Contains(compose, "\n  fits:\n") {
		t.Fatalf("expected fits service removed, got:\n%s", compose)
	}
	if !strings.Contains(compose, "\n  crayfits:\n") {
		t.Fatalf("expected crayfits service preserved, got:\n%s", compose)
	}
	if !strings.Contains(compose, `CRAYFITS_WEBSERVICE_URI: "https://microservices.libops.site/fits/examine"`) {
		t.Fatalf("expected crayfits to point at managed fits, got:\n%s", compose)
	}
	if strings.Contains(compose, "ALPACA_DERIVATIVE_FITS_URL") {
		t.Fatalf("expected alpaca to keep using local crayfits, got:\n%s", compose)
	}

	devComposeText, err := os.ReadFile(filepath.Join(projectDir, "docker-compose.dev.yml"))
	if err != nil {
		t.Fatalf("ReadFile(docker-compose.dev.yml) error = %v", err)
	}
	devCompose := string(devComposeText)
	if !strings.Contains(devCompose, "\n  fits:\n") {
		t.Fatalf("expected dev compose to include local fits, got:\n%s", devCompose)
	}
	if !strings.Contains(devCompose, `CRAYFITS_WEBSERVICE_URI: "http://fits:8080/fits/examine"`) {
		t.Fatalf("expected dev compose to reset crayfits fits URL, got:\n%s", devCompose)
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

	if err := runComponentSet(newComponentSetTestCommand(), "isle-tls", "on"); err != nil {
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

	if err := runComponentSet(newComponentSetTestCommand(), "isle-tls-override", "on"); err != nil {
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

	if err := runComponentSet(newComponentSetTestCommand(), "isle-tls", "off"); err != nil {
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

	if err := runComponentSet(newComponentSetTestCommand(), "fcrepo", ""); err != nil {
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

	if err := runComponentSet(newComponentSetTestCommand(), "isle-tls", ""); err != nil {
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

func TestComponentExtensionSetRegistersFollowUpFlags(t *testing.T) {
	for _, name := range []string{"codebase-rootfs", "drupal-rootfs", "isle-file-system-uri", "iiif-upstream-url", "tls-mode"} {
		if componentExtensionSetCmd.Flags().Lookup(name) == nil {
			t.Fatalf("expected component set handler to register --%s", name)
		}
	}
	for _, name := range []string{"isle-tls-tls-mode", "isle-tls-override-tls-mode"} {
		if componentExtensionSetCmd.Flags().Lookup(name) != nil {
			t.Fatalf("did not expect unused TLS follow-up flag --%s", name)
		}
	}
}

func TestResolveCodebaseRootfsFlagRejectsConflictingAliases(t *testing.T) {
	var codebaseRootfs string
	var drupalRootfs string
	cmd := &cobra.Command{Use: "test"}
	addCodebaseRootfsFlags(cmd, &codebaseRootfs, &drupalRootfs, createpkg.DefaultDrupalRootfs)
	if err := cmd.ParseFlags([]string{"--codebase-rootfs", "app/rootfs", "--drupal-rootfs", "drupal/rootfs"}); err != nil {
		t.Fatalf("ParseFlags() error = %v", err)
	}

	_, err := resolveCodebaseRootfsFlag(cmd, codebaseRootfs, drupalRootfs)
	if err == nil {
		t.Fatal("expected conflicting rootfs alias error")
	}
	if !strings.Contains(err.Error(), "--codebase-rootfs and --drupal-rootfs cannot be combined") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func writeFileForTest(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", path, err)
	}
}

func addDerivativeServiceFixture(t *testing.T, projectDir, service string) {
	t.Helper()

	composePath := filepath.Join(projectDir, "docker-compose.yml")
	data, err := os.ReadFile(composePath)
	if err != nil {
		t.Fatalf("ReadFile(docker-compose.yml) error = %v", err)
	}
	block := "\n  " + service + ":\n    image: islandora/" + service + ":${ISLANDORA_TAG}\n"
	updated := strings.Replace(string(data), "\n  traefik:\n", block+"\n  traefik:\n", 1)
	if updated == string(data) {
		t.Fatalf("failed to insert %s service fixture", service)
	}
	if err := os.WriteFile(composePath, []byte(updated), 0o644); err != nil {
		t.Fatalf("WriteFile(docker-compose.yml) error = %v", err)
	}
}

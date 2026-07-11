package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	createpkg "github.com/libops/sitectl-isle/pkg/create"
	corecomponent "github.com/libops/sitectl/pkg/component"
	"github.com/libops/sitectl/pkg/config"
	"github.com/libops/sitectl/pkg/plugin"
	coretraefik "github.com/libops/sitectl/pkg/services/traefik"
	"github.com/spf13/cobra"
)

func newComponentSetTestCommand() *cobra.Command {
	var path string
	var codebaseRootfs string
	var drupalRootfs string
	var state string
	var disposition string
	var yolo bool

	cmd := &cobra.Command{Use: "set <name> [disposition]"}
	cmd.Flags().StringVar(&path, "path", "", "Path to the checked out ISLE project. Defaults to the active sitectl context project directory")
	addCodebaseRootfsFlags(cmd, &codebaseRootfs, &drupalRootfs, createpkg.DefaultDrupalRootfs)
	cmd.Flags().StringVar(&state, "state", "", "Component state to apply. Valid values are on or off. If omitted, the command prompts interactively.")
	cmd.Flags().StringVar(&disposition, "disposition", "", "Component disposition to apply. Valid values depend on the component, commonly disabled, superceded, enabled, or distributed.")
	cmd.Flags().BoolVar(&yolo, "yolo", false, "Apply the component change without confirmation")
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

func TestRunComponentSetUsesExplicitFilesystemURIWhenTurningFcrepoOff(t *testing.T) {
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

	cmd := newComponentSetTestCommand()
	if err := cmd.Flags().Set("isle-file-system-uri", "private"); err != nil {
		t.Fatalf("Flags().Set(isle-file-system-uri) error = %v", err)
	}

	if err := runComponentSet(cmd, "fcrepo", "off"); err != nil {
		t.Fatalf("runComponentSet() error = %v", err)
	}
	if got.ISLEFileSystemURI != "private" {
		t.Fatalf("expected explicit private filesystem uri, got %q", got.ISLEFileSystemURI)
	}
}

func TestRunComponentSetSwitchesDefaultCodebaseToGitRoot(t *testing.T) {
	projectDir := t.TempDir()
	writeISLEOnFixture(t, projectDir)
	writeISLEDefaultCodebaseFixture(t, projectDir)

	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	if err := config.SaveContext(&config.Context{
		Name:           "isle-local",
		Site:           "isle-local",
		Plugin:         "isle",
		DockerHostType: config.ContextLocal,
		DockerSocket:   "/var/run/docker.sock",
		ProjectDir:     projectDir,
		DrupalRootfs:   createpkg.DefaultDrupalRootfs,
	}, true); err != nil {
		t.Fatalf("SaveContext() error = %v", err)
	}

	oldSDK := commandSDK
	oldStatusPath := statusPath
	oldStatusCodebaseRootfs := statusCodebaseRootfs
	oldStatusDrupalRootfs := statusDrupalRootfs
	oldYolo := componentSetYolo
	oldApply := componentApplyOptions
	oldPromptChoice := componentPromptChoice
	t.Cleanup(func() {
		commandSDK = oldSDK
		statusPath = oldStatusPath
		statusCodebaseRootfs = oldStatusCodebaseRootfs
		statusDrupalRootfs = oldStatusDrupalRootfs
		componentSetYolo = oldYolo
		componentApplyOptions = oldApply
		componentPromptChoice = oldPromptChoice
	})

	commandSDK = plugin.NewSDK(plugin.Metadata{
		Name:        "isle",
		Version:     "test",
		Description: "test",
	})
	commandSDK.Config.Context = "isle-local"
	statusPath = ""
	statusCodebaseRootfs = createpkg.DefaultDrupalRootfs
	statusDrupalRootfs = createpkg.DefaultDrupalRootfs
	componentSetYolo = true
	componentApplyOptions = createpkg.Apply

	var out bytes.Buffer
	cmd := newComponentSetTestCommand()
	cmd.SetOut(&out)

	if err := runComponentSet(cmd, "codebase", "git-root"); err != nil {
		t.Fatalf("runComponentSet() error = %v", err)
	}

	if !strings.Contains(out.String(), "codebase: git-root") {
		t.Fatalf("expected command output, got:\n%s", out.String())
	}
	for _, rel := range []string{
		"Dockerfile",
		".dockerignore",
		"composer.json",
		"composer.lock",
		"config/sync/field.storage.media.field_media_file.yml",
		"web/modules/custom/.gitkeep",
		"web/themes/custom/.gitkeep",
	} {
		if _, err := os.Stat(filepath.Join(projectDir, rel)); err != nil {
			t.Fatalf("expected git-root codebase path %s: %v", rel, err)
		}
	}
	if _, err := os.Stat(filepath.Join(projectDir, "drupal", "Dockerfile")); !os.IsNotExist(err) {
		t.Fatalf("expected drupal/Dockerfile to move to git root, stat err = %v", err)
	}

	compose := readFileForTest(t, filepath.Join(projectDir, "docker-compose.yml"))
	if !strings.Contains(compose, "context: .") || strings.Contains(compose, "context: ./drupal") {
		t.Fatalf("expected drupal build context to switch to git root, got:\n%s", compose)
	}
	if !strings.Contains(compose, "- .:/drupal:rw") || strings.Contains(compose, "- ./drupal:/drupal:rw") {
		t.Fatalf("expected init bind mount to switch to git root, got:\n%s", compose)
	}

	devCompose := readFileForTest(t, filepath.Join(projectDir, "docker-compose.dev.yml"))
	if strings.Contains(devCompose, "drupal/rootfs/var/www/drupal") {
		t.Fatalf("expected dev bind mounts to point at git root, got:\n%s", devCompose)
	}
	for _, want := range []string{
		"./assets:/var/www/drupal/assets",
		"./composer.json:/var/www/drupal/composer.json",
		"./config:/var/www/drupal/config",
	} {
		if !strings.Contains(devCompose, want) {
			t.Fatalf("expected dev compose to contain %q, got:\n%s", want, devCompose)
		}
	}

	stored, err := config.GetContext("isle-local")
	if err != nil {
		t.Fatalf("GetContext() error = %v", err)
	}
	if stored.DrupalRootfs != corecomponent.DefaultDrupalRootfs {
		t.Fatalf("expected active context DrupalRootfs %q, got %q", corecomponent.DefaultDrupalRootfs, stored.DrupalRootfs)
	}
	views, err := detectComponentViewsForContext(&stored, stored.EffectiveDrupalRootfs())
	if err != nil {
		t.Fatalf("detectComponentViewsForContext() error = %v", err)
	}
	for _, view := range views {
		if view.Name == "codebase" {
			if view.Disposition != corecomponent.DispositionGitRoot {
				t.Fatalf("expected codebase disposition git-root after switch, got %q", view.Disposition)
			}
			return
		}
	}
	t.Fatal("expected codebase component status after switch")
}

func TestRunComponentSetUsesContextRootfsAfterCodebaseGitRoot(t *testing.T) {
	projectDir := t.TempDir()
	writeISLEOnFixture(t, projectDir)
	writeISLEDefaultCodebaseFixture(t, projectDir)

	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	if err := config.SaveContext(&config.Context{
		Name:           "isle-local",
		Site:           "isle-local",
		Plugin:         "isle",
		DockerHostType: config.ContextLocal,
		DockerSocket:   "/var/run/docker.sock",
		ProjectDir:     projectDir,
		DrupalRootfs:   createpkg.DefaultDrupalRootfs,
	}, true); err != nil {
		t.Fatalf("SaveContext() error = %v", err)
	}

	oldSDK := commandSDK
	oldStatusPath := statusPath
	oldStatusCodebaseRootfs := statusCodebaseRootfs
	oldStatusDrupalRootfs := statusDrupalRootfs
	oldYolo := componentSetYolo
	oldApply := componentApplyOptions
	oldPromptChoice := componentPromptChoice
	t.Cleanup(func() {
		commandSDK = oldSDK
		statusPath = oldStatusPath
		statusCodebaseRootfs = oldStatusCodebaseRootfs
		statusDrupalRootfs = oldStatusDrupalRootfs
		componentSetYolo = oldYolo
		componentApplyOptions = oldApply
		componentPromptChoice = oldPromptChoice
	})

	commandSDK = plugin.NewSDK(plugin.Metadata{
		Name:        "isle",
		Version:     "test",
		Description: "test",
	})
	commandSDK.Config.Context = "isle-local"
	statusPath = ""
	statusCodebaseRootfs = createpkg.DefaultDrupalRootfs
	statusDrupalRootfs = createpkg.DefaultDrupalRootfs
	componentSetYolo = true
	componentApplyOptions = createpkg.Apply

	if err := runComponentSet(newComponentSetTestCommand(), "codebase", "git-root"); err != nil {
		t.Fatalf("runComponentSet(codebase) error = %v", err)
	}
	stored, err := config.GetContext("isle-local")
	if err != nil {
		t.Fatalf("GetContext() error = %v", err)
	}
	if stored.DrupalRootfs != corecomponent.DefaultDrupalRootfs {
		t.Fatalf("expected stored rootfs %q, got %q", corecomponent.DefaultDrupalRootfs, stored.DrupalRootfs)
	}

	fcrepoCmd := newComponentSetTestCommand()
	if err := fcrepoCmd.Flags().Set("isle-file-system-uri", "private"); err != nil {
		t.Fatalf("Flags().Set(isle-file-system-uri) error = %v", err)
	}
	if err := runComponentSet(fcrepoCmd, "fcrepo", "superceded"); err != nil {
		t.Fatalf("runComponentSet(fcrepo) error = %v", err)
	}
	if err := runComponentSet(newComponentSetTestCommand(), "blazegraph", "disabled"); err != nil {
		t.Fatalf("runComponentSet(blazegraph) error = %v", err)
	}

	compose := readFileForTest(t, filepath.Join(projectDir, "docker-compose.yml"))
	for _, absent := range []string{"\n  fcrepo:\n", "\n  milliner:\n", "\n  blazegraph:\n", "fcrepo-data", "blazegraph-data"} {
		if strings.Contains(compose, absent) {
			t.Fatalf("expected %q removed after component sequence, got:\n%s", absent, compose)
		}
	}
	if err := runComponentSet(newComponentSetTestCommand(), "blazegraph", "enabled"); err != nil {
		t.Fatalf("runComponentSet(blazegraph enable) error = %v", err)
	}
	compose = readFileForTest(t, filepath.Join(projectDir, "docker-compose.yml"))
	for _, want := range []string{
		"\n  blazegraph:\n",
		"image: libops/blazegraph:2.1.5@sha256:3127324525a28f4905b56d24fa7e866c4bf4588f85f6f21df44ffc93b24666fc",
		"blazegraph-data",
		`ALPACA_TRIPLESTORE_INDEXER_ENABLED: "true"`,
		`DRUPAL_DEFAULT_TRIPLESTORE_NAMESPACE: "islandora"`,
	} {
		if !strings.Contains(compose, want) {
			t.Fatalf("expected blazegraph re-enabled compose to contain %q, got:\n%s", want, compose)
		}
	}
	field := readFileForTest(t, filepath.Join(projectDir, "config", "sync", "field.storage.media.field_media_file.yml"))
	if !strings.Contains(field, `uri_scheme: "private"`) && !strings.Contains(field, "uri_scheme: private") {
		t.Fatalf("expected fcrepo replacement to use private files, got:\n%s", field)
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
      rule: Host(`+"`"+`localhost`+"`"+`)
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
		"islandora-workbench-client:",
		"(Host(`localhost`)) && HeaderRegexp(`User-Agent`, `(?i)^Islandora Workbench$`)",
		"priority: 100000",
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
	traefik := string(traefikText)
	if strings.Contains(traefik, "captcha-protect") {
		t.Fatalf("expected drupal traefik config to remove captcha-protect, got:\n%s", string(traefikText))
	}
	if strings.Contains(traefik, "islandora-workbench-client") {
		t.Fatalf("expected drupal traefik config to remove Workbench bypass router, got:\n%s", traefik)
	}
}

func TestRunComponentSetConfiguresIngressLetsEncrypt(t *testing.T) {
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

	var out bytes.Buffer
	cmd := newComponentSetTestCommand()
	cmd.SetOut(&out)
	if err := cmd.Flags().Set("mode", coretraefik.IngressModeHTTPSLetsEncrypt); err != nil {
		t.Fatalf("Flags().Set(mode) error = %v", err)
	}
	if err := cmd.Flags().Set("domain", "repo.example.org"); err != nil {
		t.Fatalf("Flags().Set(domain) error = %v", err)
	}
	if err := cmd.Flags().Set("acme-email", "admin@example.org"); err != nil {
		t.Fatalf("Flags().Set(acme-email) error = %v", err)
	}
	if err := cmd.Flags().Set("trusted-ip", "10.0.0.0/8"); err != nil {
		t.Fatalf("Flags().Set(trusted-ip) error = %v", err)
	}
	if err := cmd.Flags().Set("trusted-ip", "203.0.113.4"); err != nil {
		t.Fatalf("Flags().Set(trusted-ip second) error = %v", err)
	}
	if err := cmd.Flags().Set("max-upload-size", "2G"); err != nil {
		t.Fatalf("Flags().Set(max-upload-size) error = %v", err)
	}
	if err := cmd.Flags().Set("upload-timeout", "10m"); err != nil {
		t.Fatalf("Flags().Set(upload-timeout) error = %v", err)
	}
	if err := runComponentSet(cmd, coretraefik.IngressName, "enabled"); err != nil {
		t.Fatalf("runComponentSet() error = %v", err)
	}
	if !strings.Contains(out.String(), "ingress: enabled (https-letsencrypt)") {
		t.Fatalf("expected component output, got:\n%s", out.String())
	}

	composePath := filepath.Join(projectDir, "docker-compose.yml")
	compose := readFileForTest(t, composePath)
	for _, want := range []string{
		`INGRESS_HOSTNAMES: "repo.example.org,localhost,127.0.0.1,::1"`,
		`INGRESS_SCHEME: "https"`,
		`DRUPAL_DEFAULT_FCREPO_URL: "http://fcrepo:8080/fcrepo/rest/"`,
		`FCREPO_ALLOW_EXTERNAL_DRUPAL: "https://repo.example.org/"`,
		"--entryPoints.https.address=:443",
		"--certificatesResolvers.letsencrypt.acme.email=admin@example.org",
		`- "443:443"`,
		"acme-data:/acme:rw",
		"--entryPoints.http.forwardedHeaders.trustedIPs=10.0.0.0/8,203.0.113.4",
		`PHP_UPLOAD_MAX_FILESIZE: "2G"`,
		`PHP_POST_MAX_SIZE: "2G"`,
		`NGINX_CLIENT_MAX_BODY_SIZE: "2G"`,
		`NGINX_CLIENT_BODY_TIMEOUT: "10m"`,
		`NGINX_FASTCGI_READ_TIMEOUT: "10m"`,
		`NGINX_FASTCGI_SEND_TIMEOUT: "10m"`,
		"--entryPoints.http.transport.respondingTimeouts.readTimeout=10m",
		`NGINX_SET_REAL_IP_FROM: "10.0.0.0/8"`,
		`NGINX_SET_REAL_IP_FROM2: "203.0.113.4"`,
		`NGINX_REAL_IP_RECURSIVE: "on"`,
	} {
		if !strings.Contains(compose, want) {
			t.Fatalf("expected compose to contain %q, got:\n%s", want, compose)
		}
	}

	router := readFileForTest(t, filepath.Join(projectDir, "conf", "traefik", "drupal.yml"))
	for _, want := range []string{
		"Host(`repo.example.org`)",
		"certResolver: letsencrypt",
	} {
		if !strings.Contains(router, want) {
			t.Fatalf("expected router to contain %q, got:\n%s", want, router)
		}
	}
}

func TestRunComponentSetIngressYoloUsesDefaultDispositionWhenMissing(t *testing.T) {
	projectDir := t.TempDir()
	writeISLEOnFixture(t, projectDir)

	oldPromptState := componentPromptState
	oldPromptDisposition := componentPromptDisposition
	t.Cleanup(func() {
		componentPromptState = oldPromptState
		componentPromptDisposition = oldPromptDisposition
	})

	componentPromptState = func(name string, guidance corecomponent.StateGuidance, input corecomponent.InputFunc) (corecomponent.State, error) {
		t.Fatalf("did not expect state prompt for %s", name)
		return "", nil
	}
	componentPromptDisposition = func(name string, guidance corecomponent.StateGuidance, allowed []corecomponent.Disposition, defaultDisposition corecomponent.Disposition, input corecomponent.InputFunc) (corecomponent.Disposition, error) {
		t.Fatalf("did not expect disposition prompt for %s", name)
		return "", nil
	}

	var out bytes.Buffer
	cmd := newComponentSetTestCommand()
	cmd.SetOut(&out)
	if err := cmd.Flags().Set("mode", coretraefik.IngressModeHTTP); err != nil {
		t.Fatalf("Flags().Set(mode) error = %v", err)
	}
	if err := cmd.Flags().Set("domain", "qa-origin.libops.io"); err != nil {
		t.Fatalf("Flags().Set(domain) error = %v", err)
	}

	opts := componentSetOptions{
		Path:         projectDir,
		DrupalRootfs: createpkg.DefaultDrupalRootfs,
		Yolo:         true,
	}
	if err := runComponentSetWithOptions(cmd, coretraefik.IngressName, "", opts); err != nil {
		t.Fatalf("runComponentSetWithOptions() error = %v", err)
	}
	if !strings.Contains(out.String(), "ingress: enabled (http)") {
		t.Fatalf("expected component output, got:\n%s", out.String())
	}

	compose := readFileForTest(t, filepath.Join(projectDir, "docker-compose.yml"))
	for _, want := range []string{
		`INGRESS_HOSTNAMES: "qa-origin.libops.io,localhost,127.0.0.1,::1"`,
		`INGRESS_SCHEME: "http"`,
		`DRUPAL_DEFAULT_CANTALOUPE_URL: "http://qa-origin.libops.io/iiif/3"`,
	} {
		if !strings.Contains(compose, want) {
			t.Fatalf("expected compose to contain %q, got:\n%s", want, compose)
		}
	}
}

func TestApplyISLEIngressFilesRemovesFcrepoEnvWhenServiceAbsent(t *testing.T) {
	projectDir := t.TempDir()
	writeFileForTest(t, filepath.Join(projectDir, "docker-compose.yml"), `
services:
  drupal:
    environment:
      DRUPAL_DEFAULT_FCREPO_HOST: fcrepo
      DRUPAL_DEFAULT_FCREPO_PORT: "8080"
      DRUPAL_DEFAULT_FCREPO_URL: http://fcrepo.localhost/fcrepo/rest/
  traefik:
    command: []
`)

	ctx := &config.Context{ProjectDir: projectDir}
	if err := applyISLEIngressFiles(ctx, map[string]string{
		"mode":   coretraefik.IngressModeHTTP,
		"domain": "localhost",
	}); err != nil {
		t.Fatalf("applyISLEIngressFiles() error = %v", err)
	}

	compose := readFileForTest(t, filepath.Join(projectDir, "docker-compose.yml"))
	for _, absent := range []string{
		"DRUPAL_DEFAULT_FCREPO_HOST",
		"DRUPAL_DEFAULT_FCREPO_PORT",
		"DRUPAL_DEFAULT_FCREPO_URL",
	} {
		if strings.Contains(compose, absent) {
			t.Fatalf("expected fcrepo env %q removed, got:\n%s", absent, compose)
		}
	}
}

func TestApplyISLEIngressFilesSetsFcrepoEnvWhenServicePresent(t *testing.T) {
	projectDir := t.TempDir()
	writeFileForTest(t, filepath.Join(projectDir, "docker-compose.yml"), `
services:
  drupal:
    environment: {}
  fcrepo:
    image: libops/fcrepo:7
  traefik:
    command: []
`)

	ctx := &config.Context{ProjectDir: projectDir}
	if err := applyISLEIngressFiles(ctx, map[string]string{
		"mode":   coretraefik.IngressModeHTTPSCustom,
		"domain": "repo.example.org",
	}); err != nil {
		t.Fatalf("applyISLEIngressFiles() error = %v", err)
	}

	compose := readFileForTest(t, filepath.Join(projectDir, "docker-compose.yml"))
	want := `DRUPAL_DEFAULT_FCREPO_URL: "http://fcrepo:8080/fcrepo/rest/"`
	if !strings.Contains(compose, want) {
		t.Fatalf("expected fcrepo URL %q, got:\n%s", want, compose)
	}
	if strings.Contains(compose, "drupal.internal") {
		t.Fatalf("expected non-local ingress to omit drupal.internal, got:\n%s", compose)
	}
}

func TestApplyISLEIngressFilesUsesInternalDrupalURLForLocalFcrepo(t *testing.T) {
	projectDir := t.TempDir()
	writeFileForTest(t, filepath.Join(projectDir, "docker-compose.yml"), `
services:
  drupal:
    environment:
      DRUPAL_DEFAULT_SITE_URL: http://localhost
      DRUSH_OPTIONS_URI: http://localhost
  fcrepo:
    environment:
      FCREPO_ALLOW_EXTERNAL_DRUPAL: http://localhost/
    image: libops/fcrepo:7
  traefik:
    command: []
`)

	ctx := &config.Context{ProjectDir: projectDir}
	if err := applyISLEIngressFiles(ctx, map[string]string{
		"mode":   coretraefik.IngressModeHTTP,
		"domain": "localhost",
	}); err != nil {
		t.Fatalf("applyISLEIngressFiles() error = %v", err)
	}

	compose := readFileForTest(t, filepath.Join(projectDir, "docker-compose.yml"))
	for _, want := range []string{
		`INGRESS_HOSTNAMES: "localhost,127.0.0.1,::1,drupal.internal"`,
		`FCREPO_ALLOW_EXTERNAL_DRUPAL: "http://drupal.internal/"`,
	} {
		if !strings.Contains(compose, want) {
			t.Fatalf("expected compose to contain %q, got:\n%s", want, compose)
		}
	}
	for _, notWant := range []string{
		`DRUPAL_DEFAULT_SITE_URL`,
		`DRUSH_OPTIONS_URI`,
		`DRUPAL_TRUSTED_HOST_PATTERNS`,
	} {
		if strings.Contains(compose, notWant) {
			t.Fatalf("expected compose not to contain %q, got:\n%s", notWant, compose)
		}
	}
}

func TestRunComponentSetIngressLetsEncryptRequiresACMEEmail(t *testing.T) {
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
	if err := cmd.Flags().Set("mode", coretraefik.IngressModeHTTPSLetsEncrypt); err != nil {
		t.Fatalf("Flags().Set(mode) error = %v", err)
	}
	err := runComponentSet(cmd, coretraefik.IngressName, "enabled")
	if err == nil || !strings.Contains(err.Error(), "--acme-email is required") {
		t.Fatalf("expected required acme-email error, got %v", err)
	}
}

func TestRunComponentSetTogglesDevMode(t *testing.T) {
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

	if err := runComponentSet(newComponentSetTestCommand(), "dev-mode", "enabled"); err != nil {
		t.Fatalf("runComponentSet(enabled) error = %v", err)
	}
	overridePath := filepath.Join(projectDir, "docker-compose.override.yml")
	override := readFileForTest(t, overridePath)
	for _, want := range []string{
		"UID: ${UID:-1000}",
		"./assets:/var/www/drupal/assets:z,rw",
		"./web/modules/custom:/var/www/drupal/web/modules/custom:z,rw",
		"--providers.file.watch=true",
	} {
		if !strings.Contains(override, want) {
			t.Fatalf("expected override to contain %q, got:\n%s", want, override)
		}
	}
	if strings.Contains(override, "--providers.docker") {
		t.Fatalf("dev-mode override must not enable the Docker provider, got:\n%s", override)
	}

	if err := runComponentSet(newComponentSetTestCommand(), "dev-mode", "disabled"); err != nil {
		t.Fatalf("runComponentSet(disabled) error = %v; override:\n%s", err, readFileForTest(t, overridePath))
	}
	if _, err := os.Stat(overridePath); !os.IsNotExist(err) {
		t.Fatalf("expected dev override removed, stat error = %v", err)
	}
}

func TestRunComponentSetDevModeAssistantImpliesEnabled(t *testing.T) {
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
	if err := cmd.Flags().Set("assistant", "true"); err != nil {
		t.Fatalf("Flags().Set(assistant) error = %v", err)
	}
	if err := cmd.Flags().Set("harness", "claude"); err != nil {
		t.Fatalf("Flags().Set(harness) error = %v", err)
	}
	if err := runComponentSet(cmd, "dev-mode", ""); err != nil {
		t.Fatalf("runComponentSet(dev-mode --assistant) error = %v", err)
	}

	override := readFileForTest(t, filepath.Join(projectDir, "docker-compose.override.yml"))
	for _, want := range []string{
		"cli-sandbox:",
		"image: ${SITECTL_ASSISTANT_IMAGE:-ghcr.io/libops/cli-sandbox:claude}",
		"- claude",
		"- --dangerously-skip-permissions",
		"./web/modules/custom:/var/www/drupal/web/modules/custom:z,rw",
	} {
		if !strings.Contains(override, want) {
			t.Fatalf("expected override to contain %q, got:\n%s", want, override)
		}
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
	if !strings.Contains(string(devComposeText), "DRUPAL_DEFAULT_CANTALOUPE_URL: \"http://localhost/cantaloupe/iiif/2\"") {
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

func TestComponentExtensionSetRegistersFollowUpFlags(t *testing.T) {
	for _, name := range []string{"codebase-rootfs", "drupal-rootfs", "isle-file-system-uri", "iiif-upstream-url", "mode", "domain", "acme-email", "trusted-ip", "max-upload-size", "upload-timeout"} {
		if componentExtensionSetCmd.Flags().Lookup(name) == nil {
			t.Fatalf("expected component set handler to register --%s", name)
		}
	}
	for _, name := range []string{"tls-mode"} {
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

func TestResolveCodebaseRootfsForContextUsesSavedContextRootfs(t *testing.T) {
	var codebaseRootfs string
	var drupalRootfs string
	cmd := &cobra.Command{Use: "test"}
	addCodebaseRootfsFlags(cmd, &codebaseRootfs, &drupalRootfs, createpkg.DefaultDrupalRootfs)

	got, err := resolveCodebaseRootfsForContext(cmd, &config.Context{
		ProjectDir:   t.TempDir(),
		DrupalRootfs: corecomponent.DefaultDrupalRootfs,
	}, codebaseRootfs, drupalRootfs)
	if err != nil {
		t.Fatalf("resolveCodebaseRootfsForContext() error = %v", err)
	}
	if got != corecomponent.DefaultDrupalRootfs {
		t.Fatalf("expected saved context rootfs %q, got %q", corecomponent.DefaultDrupalRootfs, got)
	}
}

func TestResolveCodebaseRootfsForContextDetectsCurrentDrupalLayout(t *testing.T) {
	projectDir := t.TempDir()
	for _, dir := range []string{
		filepath.Join(projectDir, "drupal", "config", "sync"),
		filepath.Join(projectDir, "drupal", "web"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll(%s) error = %v", dir, err)
		}
	}
	writeFileForTest(t, filepath.Join(projectDir, "drupal", "composer.json"), "{}\n")

	var codebaseRootfs string
	var drupalRootfs string
	cmd := &cobra.Command{Use: "test"}
	addCodebaseRootfsFlags(cmd, &codebaseRootfs, &drupalRootfs, createpkg.DefaultDrupalRootfs)

	got, err := resolveCodebaseRootfsForContext(cmd, &config.Context{ProjectDir: projectDir}, codebaseRootfs, drupalRootfs)
	if err != nil {
		t.Fatalf("resolveCodebaseRootfsForContext() error = %v", err)
	}
	if got != "drupal" {
		t.Fatalf("expected detected current Drupal layout %q, got %q", "drupal", got)
	}
}

func writeFileForTest(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", path, err)
	}
}

func readFileForTest(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", path, err)
	}
	return string(data)
}

func writeISLEDefaultCodebaseFixture(t *testing.T, projectDir string) {
	t.Helper()

	for _, dir := range []string{
		filepath.Join(projectDir, "drupal"),
		filepath.Join(projectDir, "drupal", "rootfs", "etc", "s6-overlay", "scripts"),
		filepath.Join(projectDir, "drupal", "rootfs", "opt", "solr"),
		filepath.Join(projectDir, createpkg.DefaultDrupalRootfs, "assets"),
		filepath.Join(projectDir, createpkg.DefaultDrupalRootfs, "recipes"),
		filepath.Join(projectDir, createpkg.DefaultDrupalRootfs, "web", "modules", "custom"),
		filepath.Join(projectDir, createpkg.DefaultDrupalRootfs, "web", "themes", "custom"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll(%s) error = %v", dir, err)
		}
	}

	writeFileForTest(t, filepath.Join(projectDir, "drupal", "Dockerfile"), `ARG REPOSITORY
ARG TAG
FROM ${REPOSITORY}/drupal:${TAG}

ARG TARGETARCH

COPY --link rootfs /

RUN --mount=type=cache,id=custom-drupal-composer-${TARGETARCH},sharing=locked,target=/root/.composer/cache \
    composer install -d /var/www/drupal --no-interaction --no-progress --prefer-dist --no-dev --optimize-autoloader && \
    chown -R nginx:nginx /var/www/drupal && \
    cleanup.sh
`)
	writeFileForTest(t, filepath.Join(projectDir, "drupal", ".dockerignore"), "README.md\n")
	writeFileForTest(t, filepath.Join(projectDir, "docker-compose.dev.yml"), `services:
  drupal:
    volumes:
      - ./drupal/rootfs/var/www/drupal/assets:/var/www/drupal/assets:z,rw,${CONSISTENCY}
      - ./drupal/rootfs/var/www/drupal/composer.json:/var/www/drupal/composer.json:z,rw,${CONSISTENCY}
      - ./drupal/rootfs/var/www/drupal/config:/var/www/drupal/config:z,rw,${CONSISTENCY}
      - ./drupal/rootfs/var/www/drupal/web/modules/custom:/var/www/drupal/web/modules/custom:z,rw,${CONSISTENCY}
      - ./drupal/rootfs/var/www/drupal/web/themes/custom:/var/www/drupal/web/themes/custom:z,rw,${CONSISTENCY}
`)
	for _, rel := range []string{
		"assets/default_settings.txt",
		"composer.json",
		"composer.lock",
		"recipes/README.txt",
		"web/modules/custom/.gitkeep",
		"web/themes/custom/.gitkeep",
	} {
		writeFileForTest(t, filepath.Join(projectDir, createpkg.DefaultDrupalRootfs, rel), rel+"\n")
	}

	composePath := filepath.Join(projectDir, "docker-compose.yml")
	compose := readFileForTest(t, composePath)
	compose = strings.Replace(compose, "  drupal:\n    environment:\n", "  drupal:\n    build:\n      context: ./drupal\n    environment:\n", 1)
	compose = strings.Replace(compose, "  traefik:\n", "  init:\n    volumes:\n      - ./drupal:/drupal:rw\n  traefik:\n", 1)
	writeFileForTest(t, composePath, compose)
}

func addDerivativeServiceFixture(t *testing.T, projectDir, service string) {
	t.Helper()

	composePath := filepath.Join(projectDir, "docker-compose.yml")
	data, err := os.ReadFile(composePath)
	if err != nil {
		t.Fatalf("ReadFile(docker-compose.yml) error = %v", err)
	}
	block := "\n  " + service + ":\n    image: libops/" + service + ":test\n"
	updated := strings.Replace(string(data), "\n  traefik:\n", block+"\n  traefik:\n", 1)
	if updated == string(data) {
		t.Fatalf("failed to insert %s service fixture", service)
	}
	if err := os.WriteFile(composePath, []byte(updated), 0o644); err != nil {
		t.Fatalf("WriteFile(docker-compose.yml) error = %v", err)
	}
}

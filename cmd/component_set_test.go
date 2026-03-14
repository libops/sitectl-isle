package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	createpkg "github.com/libops/sitectl-isle/pkg/create"
)

func TestRunComponentSetPreservesOtherDetectedState(t *testing.T) {
	projectDir := t.TempDir()
	writeISLEOnFixture(t, projectDir)

	oldStatusPath := statusPath
	oldDrupalRootfs := statusDrupalRootfs
	oldYolo := componentSetYolo
	oldApply := componentApplyOptions
	oldInput := componentSetInput
	t.Cleanup(func() {
		statusPath = oldStatusPath
		statusDrupalRootfs = oldDrupalRootfs
		componentSetYolo = oldYolo
		componentApplyOptions = oldApply
		componentSetInput = oldInput
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
	if !strings.Contains(out.String(), "fcrepo: off") {
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
	t.Cleanup(func() {
		statusPath = oldStatusPath
		statusDrupalRootfs = oldDrupalRootfs
		componentSetYolo = oldYolo
		componentApplyOptions = oldApply
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
	t.Cleanup(func() {
		statusPath = oldStatusPath
		statusDrupalRootfs = oldDrupalRootfs
		componentSetYolo = oldYolo
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
	t.Cleanup(func() {
		statusPath = oldStatusPath
		statusDrupalRootfs = oldDrupalRootfs
		componentSetYolo = oldYolo
		componentSetTLSMode = oldTLSMode
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
	if !strings.Contains(rendered, "isle-tls-override: on") {
		t.Fatalf("expected component output, got:\n%s", rendered)
	}

	devOverride, err := os.ReadFile(filepath.Join(projectDir, "docker-compose.dev.yml"))
	if err != nil {
		t.Fatalf("ReadFile(docker-compose.dev.yml) error = %v", err)
	}
	if !strings.Contains(string(devOverride), "DRUPAL_ENABLE_HTTPS: \"false\"") {
		t.Fatalf("expected dev http override, got:\n%s", string(devOverride))
	}
}

func writeFileForTest(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", path, err)
	}
}

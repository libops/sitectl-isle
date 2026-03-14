package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	createpkg "github.com/libops/sitectl-isle/pkg/create"
	"github.com/libops/sitectl/pkg/config"
	"github.com/libops/sitectl/pkg/plugin"
)

func TestStatusCommandReportsOn(t *testing.T) {
	projectDir := t.TempDir()
	writeISLEOnFixture(t, projectDir)

	oldStatusPath := statusPath
	oldStatusDrupalRootfs := statusDrupalRootfs
	oldStatusVerbose := statusVerbose
	t.Cleanup(func() {
		statusPath = oldStatusPath
		statusDrupalRootfs = oldStatusDrupalRootfs
		statusVerbose = oldStatusVerbose
	})
	statusPath = projectDir
	statusDrupalRootfs = createpkg.DefaultDrupalRootfs
	statusVerbose = false

	var out bytes.Buffer
	cmd := statusCmd
	cmd.SetOut(&out)

	if err := runStatus(cmd); err != nil {
		t.Fatalf("runStatus() error = %v", err)
	}

	rendered := out.String()
	if !strings.Contains(rendered, "BLAZEGRAPH") || !strings.Contains(rendered, "Current state: `on`") {
		t.Fatalf("expected blazegraph on, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "FCREPO") {
		t.Fatalf("expected fcrepo on, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "ISLE-TLS") || !strings.Contains(rendered, "Detected mode: mode=http") {
		t.Fatalf("expected isle-tls off, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "If enabled:") || !strings.Contains(rendered, "If disabled:") {
		t.Fatalf("expected transition guidance, got:\n%s", rendered)
	}
}

func TestStatusCommandReportsOff(t *testing.T) {
	projectDir := t.TempDir()
	writeISLEOnFixture(t, projectDir)
	if err := createpkg.Apply(createpkg.Options{
		Path:              projectDir,
		Fcrepo:            createpkg.FcrepoStateOff,
		Blazegraph:        createpkg.FcrepoStateOff,
		ISLEFileSystemURI: createpkg.PublicISLEFileSystemURI,
	}); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	oldStatusPath := statusPath
	oldStatusDrupalRootfs := statusDrupalRootfs
	oldStatusVerbose := statusVerbose
	t.Cleanup(func() {
		statusPath = oldStatusPath
		statusDrupalRootfs = oldStatusDrupalRootfs
		statusVerbose = oldStatusVerbose
	})
	statusPath = projectDir
	statusDrupalRootfs = createpkg.DefaultDrupalRootfs
	statusVerbose = false

	var out bytes.Buffer
	cmd := statusCmd
	cmd.SetOut(&out)

	if err := runStatus(cmd); err != nil {
		t.Fatalf("runStatus() error = %v", err)
	}

	rendered := out.String()
	if !strings.Contains(rendered, "BLAZEGRAPH") || !strings.Contains(rendered, "Current state: `off`") {
		t.Fatalf("expected blazegraph off, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "FCREPO") {
		t.Fatalf("expected fcrepo off, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "ISLE-TLS-OVERRIDE") || !strings.Contains(rendered, "docker-compose.dev.yml not present") {
		t.Fatalf("expected isle-tls-override off, got:\n%s", rendered)
	}
}

func TestStatusCommandVerboseReportsDrift(t *testing.T) {
	projectDir := t.TempDir()
	writeISLEOnFixture(t, projectDir)
	composePath := filepath.Join(projectDir, "docker-compose.yml")
	if err := os.WriteFile(composePath, []byte(`
services:
  alpaca:
    environment:
      ALPACA_FCREPO_INDEXER_ENABLED: "true"
      ALPACA_TRIPLESTORE_INDEXER_ENABLED: "true"
  drupal:
    environment:
      DRUPAL_DEFAULT_FCREPO_URL: http://fcrepo.example/fcrepo/rest/
      DRUPAL_DEFAULT_TRIPLESTORE_NAMESPACE: ""
  fcrepo:
    image: islandora/fcrepo6
volumes:
  fcrepo-data: {}
`), 0o644); err != nil {
		t.Fatalf("WriteFile(compose) error = %v", err)
	}

	oldStatusPath := statusPath
	oldStatusDrupalRootfs := statusDrupalRootfs
	oldStatusVerbose := statusVerbose
	t.Cleanup(func() {
		statusPath = oldStatusPath
		statusDrupalRootfs = oldStatusDrupalRootfs
		statusVerbose = oldStatusVerbose
	})
	statusPath = projectDir
	statusDrupalRootfs = createpkg.DefaultDrupalRootfs
	statusVerbose = true

	var out bytes.Buffer
	cmd := statusCmd
	cmd.SetOut(&out)

	if err := runStatus(cmd); err != nil {
		t.Fatalf("runStatus() error = %v", err)
	}

	rendered := out.String()
	if !strings.Contains(rendered, "Current state: `drifted`") {
		t.Fatalf("expected blazegraph drifted, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "drift:") {
		t.Fatalf("expected verbose drift details, got:\n%s", rendered)
	}
}

func TestStatusCommandUsesActiveContextProjectDir(t *testing.T) {
	projectDir := t.TempDir()
	writeISLEOnFixture(t, projectDir)

	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	if err := config.SaveContext(&config.Context{
		Name:           "isle-local",
		DockerHostType: config.ContextLocal,
		DockerSocket:   "/var/run/docker.sock",
		ProjectDir:     projectDir,
	}, true); err != nil {
		t.Fatalf("SaveContext() error = %v", err)
	}

	oldSDK := commandSDK
	oldStatusPath := statusPath
	oldStatusDrupalRootfs := statusDrupalRootfs
	oldStatusVerbose := statusVerbose
	t.Cleanup(func() {
		commandSDK = oldSDK
		statusPath = oldStatusPath
		statusDrupalRootfs = oldStatusDrupalRootfs
		statusVerbose = oldStatusVerbose
	})

	commandSDK = plugin.NewSDK(plugin.Metadata{
		Name:        "isle",
		Version:     "test",
		Description: "test",
	})
	commandSDK.Config.Context = "isle-local"
	statusPath = ""
	statusDrupalRootfs = createpkg.DefaultDrupalRootfs
	statusVerbose = false

	var out bytes.Buffer
	cmd := statusCmd
	cmd.SetOut(&out)

	if err := runStatus(cmd); err != nil {
		t.Fatalf("runStatus() error = %v", err)
	}

	rendered := out.String()
	if !strings.Contains(rendered, "BLAZEGRAPH") || !strings.Contains(rendered, "Current state: `on`") {
		t.Fatalf("expected blazegraph on from active context project dir, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "FCREPO") {
		t.Fatalf("expected fcrepo on from active context project dir, got:\n%s", rendered)
	}
}

func TestStatusCommandReportsProdAndDevTLSModes(t *testing.T) {
	projectDir := t.TempDir()
	writeISLEOnFixture(t, projectDir)

	if err := os.WriteFile(filepath.Join(projectDir, ".env"), []byte("URI_SCHEME=\"https\"\nTLS_PROVIDER=\"letsencrypt\"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(.env) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "docker-compose.yml"), []byte(`
services:
  alpaca:
    environment:
      ALPACA_FCREPO_INDEXER_ENABLED: "true"
      ALPACA_TRIPLESTORE_INDEXER_ENABLED: "true"
  blazegraph:
    image: islandora/blazegraph
  drupal:
    environment:
      DRUPAL_DEFAULT_FCREPO_URL: https://fcrepo.example/fcrepo/rest/
      DRUPAL_DEFAULT_TRIPLESTORE_NAMESPACE: islandora
      DRUPAL_ENABLE_HTTPS: "true"
  fcrepo:
    image: islandora/fcrepo6
  traefik:
    command: >-
      --ping=true
      --entrypoints.https.http.tls.certResolver=letsencrypt
      --certificatesresolvers.letsencrypt.acme.httpchallenge=true
volumes:
  blazegraph-data: {}
  fcrepo-data: {}
`), 0o644); err != nil {
		t.Fatalf("WriteFile(compose) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "docker-compose.dev.yml"), []byte(`
services:
  drupal:
    environment:
      DRUPAL_ENABLE_HTTPS: "false"
      DRUPAL_DEFAULT_CANTALOUPE_URL: http://${DOMAIN}/cantaloupe/iiif/2
      DRUPAL_DEFAULT_FCREPO_URL: http://fcrepo.${DOMAIN}/fcrepo/rest/
      DRUSH_OPTIONS_URI: http://${DOMAIN}
  fcrepo:
    environment:
      FCREPO_ALLOW_EXTERNAL_DRUPAL: http://${DOMAIN}/
  traefik:
    environment:
      DEVELOPMENT_ENVIRONMENT: "true"
      TLS_PROVIDER: self-managed
      URI_SCHEME: http
`), 0o644); err != nil {
		t.Fatalf("WriteFile(dev compose) error = %v", err)
	}

	oldStatusPath := statusPath
	oldStatusDrupalRootfs := statusDrupalRootfs
	oldStatusVerbose := statusVerbose
	t.Cleanup(func() {
		statusPath = oldStatusPath
		statusDrupalRootfs = oldStatusDrupalRootfs
		statusVerbose = oldStatusVerbose
	})
	statusPath = projectDir
	statusDrupalRootfs = createpkg.DefaultDrupalRootfs
	statusVerbose = false

	var out bytes.Buffer
	cmd := statusCmd
	cmd.SetOut(&out)

	if err := runStatus(cmd); err != nil {
		t.Fatalf("runStatus() error = %v", err)
	}

	rendered := out.String()
	if !strings.Contains(rendered, "ISLE-TLS") || !strings.Contains(rendered, "Detected mode: mode=letsencrypt") {
		t.Fatalf("expected prod letsencrypt mode, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "ISLE-TLS-OVERRIDE") || !strings.Contains(rendered, "Detected mode: mode=http") {
		t.Fatalf("expected dev http override mode, got:\n%s", rendered)
	}
}

func writeISLEOnFixture(t *testing.T, projectDir string) {
	t.Helper()

	configDir := filepath.Join(projectDir, "drupal", "rootfs", "var", "www", "drupal", "config", "sync")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(configDir) error = %v", err)
	}

	if err := os.WriteFile(filepath.Join(projectDir, "docker-compose.yml"), []byte(`
services:
  alpaca:
    environment:
      ALPACA_FCREPO_INDEXER_ENABLED: "true"
      ALPACA_TRIPLESTORE_INDEXER_ENABLED: "true"
  blazegraph:
    image: islandora/blazegraph
  drupal:
    environment:
      DRUPAL_DEFAULT_FCREPO_URL: http://fcrepo.example/fcrepo/rest/
      DRUPAL_DEFAULT_TRIPLESTORE_NAMESPACE: islandora
      DRUPAL_ENABLE_HTTPS: "false"
  fcrepo:
    image: islandora/fcrepo6
volumes:
  blazegraph-data: {}
  fcrepo-data: {}
`), 0o644); err != nil {
		t.Fatalf("WriteFile(compose) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, ".env"), []byte("URI_SCHEME=\"http\"\nTLS_PROVIDER=\"self-managed\"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(.env) error = %v", err)
	}

	files := []string{
		"context.context.all_media.yml",
		"context.context.external_files.yml",
		"context.context.repository_content.yml",
		"context.context.taxonomy_terms.yml",
		"system.action.delete_file_as_fedora_external_content.yml",
		"system.action.delete_node_from_fedora.yml",
		"system.action.delete_taxonomy_term_in_fedora.yml",
		"system.action.index_file_as_fedora_external_content.yml",
		"system.action.index_media_in_fedora.yml",
		"system.action.index_node_in_fedora.yml",
		"system.action.index_taxonomy_term_in_fedora.yml",
		"system.action.user_add_role_action.fedoraadmin.yml",
		"system.action.user_remove_role_action.fedoraadmin.yml",
		"user.role.fedoraadmin.yml",
		"views.view.non_fedora_files.yml",
		"system.action.delete_media_from_triplestore.yml",
		"system.action.delete_node_from_triplestore.yml",
		"system.action.delete_taxonomy_term_in_triplestore.yml",
		"system.action.index_media_in_triplestore.yml",
		"system.action.index_node_in_triplestore.yml",
		"system.action.index_taxonomy_term_in_the_triplestore.yml",
	}
	for _, name := range files {
		if err := os.WriteFile(filepath.Join(configDir, name), []byte("id: "+name+"\n"), 0o644); err != nil {
			t.Fatalf("WriteFile(%s) error = %v", name, err)
		}
	}

	if err := os.WriteFile(filepath.Join(configDir, "context.context.all_media.yml"), []byte("reactions:\n  index:\n    actions:\n      index_media_in_triplestore: index_media_in_triplestore\n  delete:\n    actions:\n      delete_media_from_triplestore: delete_media_from_triplestore\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(all_media) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "context.context.repository_content.yml"), []byte("reactions:\n  index:\n    actions:\n      index_node_in_triplestore: index_node_in_triplestore\n  delete:\n    actions:\n      delete_node_from_triplestore: delete_node_from_triplestore\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(repository_content) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "context.context.taxonomy_terms.yml"), []byte("reactions:\n  index:\n    actions:\n      index_taxonomy_term_in_the_triplestore: index_taxonomy_term_in_the_triplestore\n  delete:\n    actions:\n      delete_taxonomy_term_in_triplestore: delete_taxonomy_term_in_triplestore\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(taxonomy_terms) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "views.view.files.yml"), []byte("value: 'fedora://'\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(views.view.files.yml) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "jsonld.settings.yml"), []byte("namespace: 'http://fedora.info/definitions/v4/repository#'\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(jsonld.settings.yml) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "field.storage.media.field_media_file.yml"), []byte("settings:\n  uri_scheme: fedora\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(field.storage.media.field_media_file.yml) error = %v", err)
	}
}

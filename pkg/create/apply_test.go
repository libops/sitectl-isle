package create

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyFcrepoOffPublic(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	configDir := filepath.Join(projectDir, "drupal", "rootfs", "var", "www", "drupal", "config", "sync")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
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
      DRUPAL_DEFAULT_FCREPO_HOST: fcrepo
      DRUPAL_DEFAULT_FCREPO_PORT: 8080
      DRUPAL_DEFAULT_FCREPO_URL: http://fcrepo.example/fcrepo/rest/
      DRUPAL_DEFAULT_BROKER_URL: tcp://activemq:61613
      DRUPAL_DEFAULT_TRIPLESTORE_NAMESPACE: temporary
  fcrepo:
    image: islandora/fcrepo6
volumes:
  blazegraph-data: {}
  fcrepo-data: {}
`), 0o644); err != nil {
		t.Fatalf("WriteFile(compose) error = %v", err)
	}

	if err := os.WriteFile(filepath.Join(configDir, "field.storage.media.field_media_file.yml"), []byte("settings:\n  uri_scheme: fedora\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(media field) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "views.view.content.yml"), []byte("roles:\n  fedoraadmin: '0'\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(view) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "views.view.files.yml"), []byte("value: 'fedora://'\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(files view) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "jsonld.settings.yml"), []byte("namespace: 'http://fedora.info/definitions/v4/repository#'\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(jsonld) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "user.role.fedoraadmin.yml"), []byte("id: fedoraadmin\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(role) error = %v", err)
	}
	if err := Apply(Options{
		Path:              projectDir,
		Fcrepo:            FcrepoStateOff,
		Blazegraph:        FcrepoStateOn,
		ISLEFileSystemURI: PublicISLEFileSystemURI,
	}); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	composeData, err := os.ReadFile(filepath.Join(projectDir, "docker-compose.yml"))
	if err != nil {
		t.Fatalf("ReadFile(compose) error = %v", err)
	}
	compose := string(composeData)
	if strings.Contains(compose, "fcrepo:") {
		t.Fatalf("expected fcrepo service removed, got:\n%s", compose)
	}
	if strings.Contains(compose, "DRUPAL_DEFAULT_FCREPO_URL") {
		t.Fatalf("expected fcrepo env removed, got:\n%s", compose)
	}
	if !strings.Contains(compose, `ALPACA_FCREPO_INDEXER_ENABLED: "false"`) && !strings.Contains(compose, "ALPACA_FCREPO_INDEXER_ENABLED: \"false\"") {
		t.Fatalf("expected fcrepo indexer flag disabled, got:\n%s", compose)
	}
	if !strings.Contains(compose, "blazegraph:") {
		t.Fatalf("expected blazegraph service preserved, got:\n%s", compose)
	}
	if !strings.Contains(compose, `ALPACA_TRIPLESTORE_INDEXER_ENABLED: "true"`) && !strings.Contains(compose, "ALPACA_TRIPLESTORE_INDEXER_ENABLED: \"true\"") {
		t.Fatalf("expected triplestore indexer flag preserved, got:\n%s", compose)
	}
	if !strings.Contains(compose, "DRUPAL_DEFAULT_BROKER_URL") {
		t.Fatalf("expected broker url preserved, got:\n%s", compose)
	}
	if !strings.Contains(compose, `DRUPAL_DEFAULT_TRIPLESTORE_NAMESPACE: islandora`) && !strings.Contains(compose, "DRUPAL_DEFAULT_TRIPLESTORE_NAMESPACE: \"islandora\"") {
		t.Fatalf("expected triplestore namespace set when blazegraph is on, got:\n%s", compose)
	}
	if strings.Contains(compose, "fcrepo-data") {
		t.Fatalf("expected fcrepo-data volume removed, got:\n%s", compose)
	}

	mediaField, err := os.ReadFile(filepath.Join(configDir, "field.storage.media.field_media_file.yml"))
	if err != nil {
		t.Fatalf("ReadFile(media field) error = %v", err)
	}
	if !strings.Contains(string(mediaField), `uri_scheme: "public"`) {
		t.Fatalf("expected public uri_scheme, got:\n%s", string(mediaField))
	}

	filesView, err := os.ReadFile(filepath.Join(configDir, "views.view.files.yml"))
	if err != nil {
		t.Fatalf("ReadFile(files view) error = %v", err)
	}
	if !strings.Contains(string(filesView), "public://") {
		t.Fatalf("expected public scheme replacement, got:\n%s", string(filesView))
	}

	viewData, err := os.ReadFile(filepath.Join(configDir, "views.view.content.yml"))
	if err != nil {
		t.Fatalf("ReadFile(content view) error = %v", err)
	}
	if strings.Contains(string(viewData), "fedoraadmin") {
		t.Fatalf("expected fedoraadmin line removed, got:\n%s", string(viewData))
	}

	jsonldData, err := os.ReadFile(filepath.Join(configDir, "jsonld.settings.yml"))
	if err != nil {
		t.Fatalf("ReadFile(jsonld) error = %v", err)
	}
	if !strings.Contains(string(jsonldData), "fedora.info") {
		t.Fatalf("expected jsonld.settings.yml left untouched, got:\n%s", string(jsonldData))
	}

	if _, err := os.Stat(filepath.Join(configDir, "user.role.fedoraadmin.yml")); !os.IsNotExist(err) {
		t.Fatalf("expected user.role.fedoraadmin.yml deleted, stat err = %v", err)
	}
}

func TestApplyBlazegraphOff(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	configDir := filepath.Join(projectDir, "drupal", "rootfs", "var", "www", "drupal", "config", "sync")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
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
      DRUPAL_DEFAULT_FCREPO_HOST: fcrepo
      DRUPAL_DEFAULT_BROKER_URL: tcp://activemq:61613
      DRUPAL_DEFAULT_TRIPLESTORE_NAMESPACE: islandora
  fcrepo:
    image: islandora/fcrepo6
volumes:
  blazegraph-data: {}
  fcrepo-data: {}
`), 0o644); err != nil {
		t.Fatalf("WriteFile(compose) error = %v", err)
	}

	actionFiles := map[string]string{
		"system.action.delete_media_from_triplestore.yml":          "id: delete_media_from_triplestore\n  queue: islandora-indexing-triplestore-delete\n",
		"system.action.delete_node_from_triplestore.yml":           "id: delete_node_from_triplestore\n  queue: islandora-indexing-triplestore-delete\n",
		"system.action.delete_taxonomy_term_in_triplestore.yml":    "id: delete_taxonomy_term_in_triplestore\n  queue: islandora-indexing-triplestore-delete\n",
		"system.action.index_media_in_triplestore.yml":             "id: index_media_in_triplestore\n  queue: islandora-indexing-triplestore-index\n",
		"system.action.index_node_in_triplestore.yml":              "id: index_node_in_triplestore\n  queue: islandora-indexing-triplestore-index\n",
		"system.action.index_taxonomy_term_in_the_triplestore.yml": "id: index_taxonomy_term_in_the_triplestore\n  queue: islandora-indexing-triplestore-index\n",
	}
	for name, contents := range actionFiles {
		if err := os.WriteFile(filepath.Join(configDir, name), []byte(contents), 0o644); err != nil {
			t.Fatalf("WriteFile(%s) error = %v", name, err)
		}
	}

	contextFiles := map[string]string{
		"context.context.all_media.yml":          "reactions:\n  index:\n    actions:\n      index_media_in_triplestore: index_media_in_triplestore\n  delete:\n    actions:\n      delete_media_from_triplestore: delete_media_from_triplestore\n",
		"context.context.repository_content.yml": "reactions:\n  index:\n    actions:\n      index_node_in_triplestore: index_node_in_triplestore\n  delete:\n    actions:\n      delete_node_from_triplestore: delete_node_from_triplestore\n",
		"context.context.taxonomy_terms.yml":     "reactions:\n  index:\n    actions:\n      index_taxonomy_term_in_the_triplestore: index_taxonomy_term_in_the_triplestore\n  delete:\n    actions:\n      delete_taxonomy_term_in_triplestore: delete_taxonomy_term_in_triplestore\n",
	}
	for name, contents := range contextFiles {
		if err := os.WriteFile(filepath.Join(configDir, name), []byte(contents), 0o644); err != nil {
			t.Fatalf("WriteFile(%s) error = %v", name, err)
		}
	}

	if err := Apply(Options{
		Path:              projectDir,
		Fcrepo:            FcrepoStateOn,
		Blazegraph:        FcrepoStateOff,
		ISLEFileSystemURI: PrivateISLEFileSystemURI,
	}); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	composeData, err := os.ReadFile(filepath.Join(projectDir, "docker-compose.yml"))
	if err != nil {
		t.Fatalf("ReadFile(compose) error = %v", err)
	}
	compose := string(composeData)
	if strings.Contains(compose, "blazegraph:") {
		t.Fatalf("expected blazegraph service removed, got:\n%s", compose)
	}
	if !strings.Contains(compose, "fcrepo:") {
		t.Fatalf("expected fcrepo service preserved, got:\n%s", compose)
	}
	if !strings.Contains(compose, `ALPACA_TRIPLESTORE_INDEXER_ENABLED: "false"`) && !strings.Contains(compose, "ALPACA_TRIPLESTORE_INDEXER_ENABLED: \"false\"") {
		t.Fatalf("expected triplestore indexer flag disabled, got:\n%s", compose)
	}
	if !strings.Contains(compose, `ALPACA_FCREPO_INDEXER_ENABLED: "true"`) && !strings.Contains(compose, "ALPACA_FCREPO_INDEXER_ENABLED: \"true\"") {
		t.Fatalf("expected fcrepo indexer flag preserved, got:\n%s", compose)
	}
	if strings.Contains(compose, "DRUPAL_DEFAULT_BROKER_URL") {
		t.Fatalf("expected broker url removed, got:\n%s", compose)
	}
	if !strings.Contains(compose, `DRUPAL_DEFAULT_TRIPLESTORE_NAMESPACE: ""`) && !strings.Contains(compose, "DRUPAL_DEFAULT_TRIPLESTORE_NAMESPACE: \"\"") {
		t.Fatalf("expected triplestore namespace blanked, got:\n%s", compose)
	}
	if !strings.Contains(compose, "DRUPAL_DEFAULT_FCREPO_HOST") {
		t.Fatalf("expected fcrepo drupal settings preserved, got:\n%s", compose)
	}
	if strings.Contains(compose, "blazegraph-data") {
		t.Fatalf("expected blazegraph-data volume removed, got:\n%s", compose)
	}
	if !strings.Contains(compose, "fcrepo-data") {
		t.Fatalf("expected fcrepo-data volume preserved, got:\n%s", compose)
	}

	for name := range actionFiles {
		if _, err := os.Stat(filepath.Join(configDir, name)); !os.IsNotExist(err) {
			t.Fatalf("expected %s deleted, stat err = %v", name, err)
		}
	}

	for name, expectedAbsent := range map[string][]string{
		"context.context.all_media.yml":          {"index_media_in_triplestore", "delete_media_from_triplestore"},
		"context.context.repository_content.yml": {"index_node_in_triplestore", "delete_node_from_triplestore"},
		"context.context.taxonomy_terms.yml":     {"index_taxonomy_term_in_the_triplestore", "delete_taxonomy_term_in_triplestore"},
	} {
		data, err := os.ReadFile(filepath.Join(configDir, name))
		if err != nil {
			t.Fatalf("ReadFile(%s) error = %v", name, err)
		}
		for _, needle := range expectedAbsent {
			if strings.Contains(string(data), needle) {
				t.Fatalf("expected %s removed from %s, got:\n%s", needle, name, string(data))
			}
		}
	}

}

func TestApplyFcrepoOnNoOp(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	composePath := filepath.Join(projectDir, "docker-compose.yml")
	original := []byte("services:\n  alpaca:\n    environment: {}\n  drupal:\n    environment: {}\n  blazegraph:\n    image: islandora/blazegraph\n  fcrepo:\n    image: islandora/fcrepo6\n")
	if err := os.WriteFile(composePath, original, 0o644); err != nil {
		t.Fatalf("WriteFile(compose) error = %v", err)
	}

	if err := Apply(Options{
		Path:              projectDir,
		Fcrepo:            FcrepoStateOn,
		Blazegraph:        FcrepoStateOn,
		ISLEFileSystemURI: PublicISLEFileSystemURI,
	}); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	got, err := os.ReadFile(composePath)
	if err != nil {
		t.Fatalf("ReadFile(compose) error = %v", err)
	}
	rendered := string(got)
	if !strings.Contains(rendered, `ALPACA_FCREPO_INDEXER_ENABLED: "true"`) && !strings.Contains(rendered, "ALPACA_FCREPO_INDEXER_ENABLED: \"true\"") {
		t.Fatalf("expected fcrepo indexer enabled, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, `ALPACA_TRIPLESTORE_INDEXER_ENABLED: "true"`) && !strings.Contains(rendered, "ALPACA_TRIPLESTORE_INDEXER_ENABLED: \"true\"") {
		t.Fatalf("expected triplestore indexer enabled, got:\n%s", rendered)
	}
}

func TestApplyPreservesComposeAnchors(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	configDir := filepath.Join(projectDir, "drupal", "rootfs", "var", "www", "drupal", "config", "sync")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(configDir) error = %v", err)
	}

	compose := `---
# Common to all services
x-common: &common
  restart: unless-stopped
  tty: true # preserve me
  security_opt:
    - label=type:container_runtime_t
  networks:
    default:
networks:
  default:
volumes:
  blazegraph-data: {}
  fcrepo-data: {}
services:
  alpaca:
    <<: *common
    environment:
      ALPACA_FCREPO_INDEXER_ENABLED: "true"
      ALPACA_TRIPLESTORE_INDEXER_ENABLED: "true"
  blazegraph:
    <<: *common
    image: islandora/blazegraph:${ISLANDORA_TAG}
    volumes:
      - blazegraph-data:/data:rw
  drupal:
    <<: *common
    environment:
      DRUPAL_DEFAULT_BROKER_URL: tcp://activemq:61613
      DRUPAL_DEFAULT_FCREPO_HOST: fcrepo
      DRUPAL_DEFAULT_FCREPO_PORT: 8080
      DRUPAL_DEFAULT_FCREPO_URL: ${URI_SCHEME}://fcrepo.${DOMAIN}/fcrepo/rest/
  fcrepo:
    <<: *common
    image: islandora/fcrepo6:${ISLANDORA_TAG}
    volumes:
      - fcrepo-data:/data:rw
`
	if err := os.WriteFile(filepath.Join(projectDir, "docker-compose.yml"), []byte(compose), 0o644); err != nil {
		t.Fatalf("WriteFile(compose) error = %v", err)
	}

	for _, name := range []string{
		"context.context.all_media.yml",
		"context.context.repository_content.yml",
		"context.context.taxonomy_terms.yml",
	} {
		if err := os.WriteFile(filepath.Join(configDir, name), []byte("reactions: {}\n"), 0o644); err != nil {
			t.Fatalf("WriteFile(%s) error = %v", name, err)
		}
	}
	if err := Apply(Options{
		Path:              projectDir,
		Fcrepo:            FcrepoStateOff,
		Blazegraph:        FcrepoStateOff,
		ISLEFileSystemURI: PrivateISLEFileSystemURI,
	}); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	updated, err := os.ReadFile(filepath.Join(projectDir, "docker-compose.yml"))
	if err != nil {
		t.Fatalf("ReadFile(compose) error = %v", err)
	}
	rendered := string(updated)
	if !strings.Contains(rendered, "x-common: &common") {
		t.Fatalf("expected anchor preserved, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "<<: *common") {
		t.Fatalf("expected merge key preserved, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "# preserve me") {
		t.Fatalf("expected comment preserved, got:\n%s", rendered)
	}
	if strings.Contains(rendered, "restart: unless-stopped\n    image: islandora/alpaca") {
		t.Fatalf("expected common settings to stay behind the anchor, got:\n%s", rendered)
	}
	if strings.Contains(rendered, "\n  blazegraph:\n") {
		t.Fatalf("expected blazegraph removed, got:\n%s", rendered)
	}
	if strings.Contains(rendered, "\n  fcrepo:\n") {
		t.Fatalf("expected fcrepo removed, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, `ALPACA_FCREPO_INDEXER_ENABLED: "false"`) {
		t.Fatalf("expected fcrepo indexer disabled, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, `ALPACA_TRIPLESTORE_INDEXER_ENABLED: "false"`) {
		t.Fatalf("expected triplestore indexer disabled, got:\n%s", rendered)
	}
}

func TestApplyAcceptsCustomISLEFileSystemURI(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	configDir := filepath.Join(projectDir, "drupal", "rootfs", "var", "www", "drupal", "config", "sync")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	if err := os.WriteFile(filepath.Join(projectDir, "docker-compose.yml"), []byte(`
services:
  alpaca:
    environment: {}
  drupal:
    environment:
      DRUPAL_DEFAULT_FCREPO_URL: http://fcrepo.example/fcrepo/rest/
  fcrepo:
    image: islandora/fcrepo6
volumes:
  fcrepo-data: {}
`), 0o644); err != nil {
		t.Fatalf("WriteFile(compose) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "field.storage.media.field_media_file.yml"), []byte("settings:\n  uri_scheme: fedora\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(media field) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "views.view.files.yml"), []byte("value: 'fedora://'\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(files view) error = %v", err)
	}
	if err := Apply(Options{
		Path:              projectDir,
		Fcrepo:            FcrepoStateOff,
		Blazegraph:        FcrepoStateOn,
		ISLEFileSystemURI: "archive",
	}); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	filesView, err := os.ReadFile(filepath.Join(configDir, "views.view.files.yml"))
	if err != nil {
		t.Fatalf("ReadFile(files view) error = %v", err)
	}
	if !strings.Contains(string(filesView), "archive://") {
		t.Fatalf("expected archive scheme replacement, got:\n%s", string(filesView))
	}

	mediaField, err := os.ReadFile(filepath.Join(configDir, "field.storage.media.field_media_file.yml"))
	if err != nil {
		t.Fatalf("ReadFile(media field) error = %v", err)
	}
	if !strings.Contains(string(mediaField), `uri_scheme: "archive"`) {
		t.Fatalf("expected archive uri_scheme, got:\n%s", string(mediaField))
	}
}

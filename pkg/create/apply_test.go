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
	configDir := filepath.Join(projectDir, DefaultDrupalRootfs, "config", "sync")
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

func TestApplyFcrepoOffTripletDetectsCurrentDrupalLayout(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	configDir := filepath.Join(projectDir, "drupal", "config", "sync")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(config) error = %v", err)
	}
	if err := os.MkdirAll(filepath.Join(projectDir, "drupal", "web"), 0o755); err != nil {
		t.Fatalf("MkdirAll(web) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "docker-compose.yml"), []byte(`
services:
  alpaca:
    environment:
      ALPACA_FCREPO_INDEXER_ENABLED: "true"
      ALPACA_TRIPLESTORE_INDEXER_ENABLED: "true"
  blazegraph:
    image: islandora/blazegraph
  cantaloupe:
    image: islandora/cantaloupe
  drupal:
    environment:
      DRUPAL_DEFAULT_CANTALOUPE_URL: ${URI_SCHEME}://${DOMAIN}/cantaloupe/iiif/2
      DRUPAL_DEFAULT_FCREPO_HOST: fcrepo
      DRUPAL_DEFAULT_FCREPO_PORT: 8080
      DRUPAL_DEFAULT_FCREPO_URL: ${URI_SCHEME}://fcrepo.${DOMAIN}/fcrepo/rest/
      DRUPAL_DEFAULT_TRIPLESTORE_NAMESPACE: islandora
  fcrepo:
    image: islandora/fcrepo6
  traefik:
    environment: {}
  triplet:
    image: ghcr.io/libops/triplet:v1.1.0
    depends_on:
      fcrepo:
        condition: service_healthy
    volumes:
      - type: volume
        source: fcrepo-data
        target: /fcrepo
volumes:
  blazegraph-data: {}
  cantaloupe-data: {}
  fcrepo-data: {}
`), 0o644); err != nil {
		t.Fatalf("WriteFile(compose) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "field.storage.media.field_media_file.yml"), []byte("settings:\n  uri_scheme: fedora\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(media field) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "drupal", "web", "robots.txt"), []byte("User-agent: *\nDisallow: /cantaloupe/*\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(robots) error = %v", err)
	}

	if err := Apply(Options{
		Path:              projectDir,
		Fcrepo:            FcrepoStateOff,
		Blazegraph:        FcrepoStateOff,
		IIIF:              IIIFTriplet,
		ISLEFileSystemURI: PrivateISLEFileSystemURI,
	}); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	composeData, err := os.ReadFile(filepath.Join(projectDir, "docker-compose.yml"))
	if err != nil {
		t.Fatalf("ReadFile(compose) error = %v", err)
	}
	compose := string(composeData)
	if !strings.Contains(compose, "\n  triplet:\n") {
		t.Fatalf("expected triplet service, got:\n%s", compose)
	}
	for _, absent := range []string{"\n  fcrepo:\n", "\n  blazegraph:\n", "fcrepo-data", "blazegraph-data", "condition: service_healthy", "source: fcrepo-data"} {
		if strings.Contains(compose, absent) {
			t.Fatalf("expected %q removed, got:\n%s", absent, compose)
		}
	}

	mediaField, err := os.ReadFile(filepath.Join(configDir, "field.storage.media.field_media_file.yml"))
	if err != nil {
		t.Fatalf("ReadFile(media field) error = %v", err)
	}
	if !strings.Contains(string(mediaField), "uri_scheme: private") && !strings.Contains(string(mediaField), `uri_scheme: "private"`) {
		t.Fatalf("expected private uri_scheme, got:\n%s", string(mediaField))
	}

	robots, err := os.ReadFile(filepath.Join(projectDir, "drupal", "web", "robots.txt"))
	if err != nil {
		t.Fatalf("ReadFile(robots) error = %v", err)
	}
	if !strings.Contains(string(robots), "Disallow: /iiif/*") {
		t.Fatalf("expected IIIF robots rule, got:\n%s", string(robots))
	}
}

func TestApplyBlazegraphOff(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	configDir := filepath.Join(projectDir, DefaultDrupalRootfs, "config", "sync")
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

func TestApplyCodebaseGitRoot(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	drupalRoot := filepath.Join(projectDir, "drupal", "rootfs", "var", "www", "drupal")
	for _, dir := range []string{
		filepath.Join(projectDir, "drupal"),
		filepath.Join(drupalRoot, "assets"),
		filepath.Join(drupalRoot, "config", "sync"),
		filepath.Join(drupalRoot, "recipes"),
		filepath.Join(drupalRoot, "web", "modules", "custom"),
		filepath.Join(drupalRoot, "web", "themes", "custom"),
		filepath.Join(projectDir, "drupal", "rootfs", "etc", "s6-overlay", "scripts"),
		filepath.Join(projectDir, "drupal", "rootfs", "opt", "solr"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll(%s) error = %v", dir, err)
		}
	}

	writeTestFile(t, filepath.Join(projectDir, "drupal", "Dockerfile"), `# syntax=docker/dockerfile:1.23.0
ARG REPOSITORY
ARG TAG
FROM ${REPOSITORY}/drupal:${TAG}

ARG TARGETARCH

COPY --link rootfs /

RUN --mount=type=cache,id=custom-drupal-composer-${TARGETARCH},sharing=locked,target=/root/.composer/cache \
    composer install -d /var/www/drupal && \
    chown -R nginx:nginx /var/www/drupal && \
    cleanup.sh
`)
	writeTestFile(t, filepath.Join(projectDir, "drupal", ".dockerignore"), "README.md\n")
	writeTestFile(t, filepath.Join(projectDir, "docker-compose.yml"), `services:
  init:
    volumes:
      - ./drupal:/drupal:rw
  drupal:
    build:
      context: ./drupal
`)
	writeTestFile(t, filepath.Join(projectDir, "docker-compose.dev.yml"), `services:
  drupal:
    volumes:
      - ./drupal/rootfs/var/www/drupal/assets:/var/www/drupal/assets:z,rw,${CONSISTENCY}
      - ./drupal/rootfs/var/www/drupal/composer.json:/var/www/drupal/composer.json:z,rw,${CONSISTENCY}
      - ./drupal/rootfs/var/www/drupal/config:/var/www/drupal/config:z,rw,${CONSISTENCY}
      - ./drupal/rootfs/var/www/drupal/web/modules/custom:/var/www/drupal/web/modules/custom:z,rw,${CONSISTENCY}
`)
	for _, rel := range []string{
		"assets/default_settings.txt",
		"composer.json",
		"composer.lock",
		"config/sync/system.site.yml",
		"recipes/README.txt",
		"web/modules/custom/.gitkeep",
		"web/themes/custom/.gitkeep",
	} {
		writeTestFile(t, filepath.Join(drupalRoot, rel), rel+"\n")
	}

	if err := applyCodebaseGitRoot(projectDir); err != nil {
		t.Fatalf("applyCodebaseGitRoot() error = %v", err)
	}

	for _, rel := range []string{"Dockerfile", ".dockerignore", "composer.json", "composer.lock", "assets/default_settings.txt", "config/sync/system.site.yml", "recipes/README.txt", "web/modules/custom/.gitkeep"} {
		if _, err := os.Stat(filepath.Join(projectDir, rel)); err != nil {
			t.Fatalf("expected %s at git root: %v", rel, err)
		}
	}
	if _, err := os.Stat(filepath.Join(projectDir, "drupal", "Dockerfile")); !os.IsNotExist(err) {
		t.Fatalf("expected drupal/Dockerfile moved, stat err = %v", err)
	}

	dockerfile := readTestFile(t, filepath.Join(projectDir, "Dockerfile"))
	for _, want := range []string{
		"COPY --link composer.json composer.lock /var/www/drupal/",
		"COPY --link drupal/rootfs/etc/ /etc/",
		"COPY --link drupal/rootfs/opt/ /opt/",
	} {
		if !strings.Contains(dockerfile, want) {
			t.Fatalf("expected Dockerfile to contain %q, got:\n%s", want, dockerfile)
		}
	}
	if strings.Contains(dockerfile, "COPY --link rootfs /") {
		t.Fatalf("expected nested rootfs copy removed, got:\n%s", dockerfile)
	}

	compose := readTestFile(t, filepath.Join(projectDir, "docker-compose.yml"))
	if !strings.Contains(compose, "context: .") || strings.Contains(compose, "context: ./drupal") {
		t.Fatalf("expected drupal build context to be git root, got:\n%s", compose)
	}
	if !strings.Contains(compose, "- .:/drupal:rw") || strings.Contains(compose, "- ./drupal:/drupal:rw") {
		t.Fatalf("expected init volume to mount git root, got:\n%s", compose)
	}

	devCompose := readTestFile(t, filepath.Join(projectDir, "docker-compose.dev.yml"))
	if strings.Contains(devCompose, "drupal/rootfs/var/www/drupal") {
		t.Fatalf("expected dev compose bind mounts to point at git root, got:\n%s", devCompose)
	}
	for _, want := range []string{"./assets:/var/www/drupal/assets", "./composer.json:/var/www/drupal/composer.json", "./config:/var/www/drupal/config"} {
		if !strings.Contains(devCompose, want) {
			t.Fatalf("expected dev compose to contain %q, got:\n%s", want, devCompose)
		}
	}

	dockerignore := readTestFile(t, filepath.Join(projectDir, ".dockerignore"))
	if !strings.Contains(dockerignore, "web/core") || !strings.Contains(dockerignore, "drupal/rootfs/var/www/drupal") {
		t.Fatalf("expected git-root .dockerignore, got:\n%s", dockerignore)
	}
}

func TestApplyCreateMatrix(t *testing.T) {
	cases := []struct {
		name     string
		options  Options
		validate func(t *testing.T, projectDir string)
	}{
		{
			name: "vanilla isle-site-template",
			options: Options{
				Fcrepo:            FcrepoStateOn,
				Blazegraph:        FcrepoStateOn,
				IIIF:              IIIFCantaloupe,
				ISLEFileSystemURI: PrivateISLEFileSystemURI,
			},
			validate: func(t *testing.T, projectDir string) {
				compose := readTestFile(t, filepath.Join(projectDir, "docker-compose.yml"))
				for _, want := range []string{"\n  fcrepo:\n", "\n  blazegraph:\n", "\n  cantaloupe:\n"} {
					if !strings.Contains(compose, want) {
						t.Fatalf("expected vanilla compose to contain %q, got:\n%s", want, compose)
					}
				}
				if _, err := os.Stat(filepath.Join(projectDir, "drupal", "rootfs", "var", "www", "drupal", "composer.json")); err != nil {
					t.Fatalf("expected nested composer.json: %v", err)
				}
			},
		},
		{
			name: "isle-site-template fcrepo=off blazegraph=off",
			options: Options{
				Fcrepo:            FcrepoStateOff,
				Blazegraph:        FcrepoStateOff,
				IIIF:              IIIFCantaloupe,
				ISLEFileSystemURI: PrivateISLEFileSystemURI,
			},
			validate: func(t *testing.T, projectDir string) {
				compose := readTestFile(t, filepath.Join(projectDir, "docker-compose.yml"))
				for _, absent := range []string{"\n  fcrepo:\n", "\n  blazegraph:\n", "fcrepo-data", "blazegraph-data"} {
					if strings.Contains(compose, absent) {
						t.Fatalf("expected %q removed, got:\n%s", absent, compose)
					}
				}
			},
		},
		{
			name: "isle-site-template fcrepo=off blazegraph=off iiif=triplet",
			options: Options{
				Fcrepo:            FcrepoStateOff,
				Blazegraph:        FcrepoStateOff,
				IIIF:              IIIFTriplet,
				ISLEFileSystemURI: PrivateISLEFileSystemURI,
			},
			validate: func(t *testing.T, projectDir string) {
				compose := readTestFile(t, filepath.Join(projectDir, "docker-compose.yml"))
				if !strings.Contains(compose, "\n  triplet:\n") || strings.Contains(compose, "\n  cantaloupe:\n") {
					t.Fatalf("expected triplet to replace cantaloupe, got:\n%s", compose)
				}
				if !strings.Contains(compose, `DRUPAL_DEFAULT_CANTALOUPE_URL: "${URI_SCHEME}://${DOMAIN}/iiif/3"`) {
					t.Fatalf("expected Drupal IIIF URL to use /iiif/3, got:\n%s", compose)
				}
			},
		},
		{
			name: "isle-site-template fcrepo=off blazegraph=off iiif=triplet codebase=git-root",
			options: Options{
				Fcrepo:            FcrepoStateOff,
				Blazegraph:        FcrepoStateOff,
				IIIF:              IIIFTriplet,
				ISLEFileSystemURI: PrivateISLEFileSystemURI,
				Codebase:          CodebaseGitRoot,
			},
			validate: func(t *testing.T, projectDir string) {
				compose := readTestFile(t, filepath.Join(projectDir, "docker-compose.yml"))
				if !strings.Contains(compose, "context: .") || strings.Contains(compose, "context: ./drupal") {
					t.Fatalf("expected git-root build context, got:\n%s", compose)
				}
				for _, rel := range []string{"Dockerfile", "composer.json", "config/sync/field.storage.media.field_media_file.yml", "web/robots.txt"} {
					if _, err := os.Stat(filepath.Join(projectDir, rel)); err != nil {
						t.Fatalf("expected git-root path %s: %v", rel, err)
					}
				}
				if strings.Contains(compose, "\n  fcrepo:\n") || strings.Contains(compose, "\n  blazegraph:\n") || !strings.Contains(compose, "\n  triplet:\n") {
					t.Fatalf("expected fcrepo/blazegraph off and triplet on, got:\n%s", compose)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			projectDir := t.TempDir()
			writeApplyMatrixProject(t, projectDir)
			tc.options.Path = projectDir
			if err := Apply(tc.options); err != nil {
				t.Fatalf("Apply() error = %v", err)
			}
			tc.validate(t, projectDir)
		})
	}
}

func TestApplyPreservesComposeAnchors(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	configDir := filepath.Join(projectDir, DefaultDrupalRootfs, "config", "sync")
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
    image: libops/blazegraph@sha256:7f4c0b54d9ef0f0822913559d57498e8de2774cadb664161cfe32f40650d8fc0
    volumes:
      - blazegraph-data:/data:rw
  drupal:
    <<: *common
    environment:
      DRUPAL_DEFAULT_FCREPO_HOST: fcrepo
      DRUPAL_DEFAULT_FCREPO_PORT: 8080
      DRUPAL_DEFAULT_FCREPO_URL: ${URI_SCHEME}://fcrepo.${DOMAIN}/fcrepo/rest/
  fcrepo:
    <<: *common
    image: libops/fcrepo@sha256:611b9b15bf205c369aa664d119126429785da28d255635d8aeeb29ddf4ce03f0
    volumes:
      - fcrepo-data:/data:rw
  traefik:
    <<: *common
    command: >-
      --ping=true
      --log.level=INFO
      --entryPoints.http.address=:80
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
	if !strings.Contains(rendered, "command: >-") {
		t.Fatalf("expected traefik folded scalar style preserved, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "      --ping=true\n      --log.level=INFO\n      --entryPoints.http.address=:80") {
		t.Fatalf("expected traefik folded scalar content preserved, got:\n%s", rendered)
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
	configDir := filepath.Join(projectDir, DefaultDrupalRootfs, "config", "sync")
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

func TestApplyDerivativeServicesDistributed(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectDir, "docker-compose.yml"), []byte(`
services:
  alpaca:
    environment: {}
  homarus:
    image: libops/homarus:test
  mergepdf:
    image: islandora/mergepdf:test
`), 0o644); err != nil {
		t.Fatalf("WriteFile(compose) error = %v", err)
	}

	if err := ApplyDerivativeServices(Options{
		Path: projectDir,
		DerivativeServices: map[string]string{
			"homarus":  DerivativeTopologyDistributed,
			"mergepdf": DerivativeTopologyDistributed,
		},
	}); err != nil {
		t.Fatalf("ApplyDerivativeServices() error = %v", err)
	}

	composeData, err := os.ReadFile(filepath.Join(projectDir, "docker-compose.yml"))
	if err != nil {
		t.Fatalf("ReadFile(compose) error = %v", err)
	}
	compose := string(composeData)
	for _, service := range []string{"homarus", "mergepdf"} {
		if strings.Contains(compose, "\n  "+service+":\n") {
			t.Fatalf("expected %s service removed, got:\n%s", service, compose)
		}
	}
	for _, want := range []string{
		`ALPACA_DERIVATIVE_HOMARUS_URL: "https://microservices.libops.site/homarus"`,
		`ALPACA_DERIVATIVE_MERGEPDF_URL: "https://microservices.libops.site/mergepdf"`,
	} {
		if !strings.Contains(compose, want) {
			t.Fatalf("expected compose to contain %q, got:\n%s", want, compose)
		}
	}

	devComposeData, err := os.ReadFile(filepath.Join(projectDir, "docker-compose.dev.yml"))
	if err != nil {
		t.Fatalf("ReadFile(dev compose) error = %v", err)
	}
	devCompose := string(devComposeData)
	for _, service := range []string{"homarus", "mergepdf"} {
		if !strings.Contains(devCompose, "\n  "+service+":\n") {
			t.Fatalf("expected dev compose to contain %s service, got:\n%s", service, devCompose)
		}
	}
	for _, want := range []string{
		`ALPACA_DERIVATIVE_HOMARUS_URL: "http://homarus:8080/"`,
		`ALPACA_DERIVATIVE_MERGEPDF_URL: "http://mergepdf:8080/"`,
	} {
		if !strings.Contains(devCompose, want) {
			t.Fatalf("expected dev compose to contain %q, got:\n%s", want, devCompose)
		}
	}
}

func TestApplyDerivativeServicesDistributedFITSPreservesLocalCrayfits(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectDir, "docker-compose.yml"), []byte(`
services:
  alpaca:
    environment: {}
  crayfits:
    image: libops/crayfits:test
  fits:
    image: libops/fits:test
`), 0o644); err != nil {
		t.Fatalf("WriteFile(compose) error = %v", err)
	}

	if err := ApplyDerivativeServices(Options{
		Path: projectDir,
		DerivativeServices: map[string]string{
			"fits": DerivativeTopologyDistributed,
		},
	}); err != nil {
		t.Fatalf("ApplyDerivativeServices() error = %v", err)
	}

	composeData, err := os.ReadFile(filepath.Join(projectDir, "docker-compose.yml"))
	if err != nil {
		t.Fatalf("ReadFile(compose) error = %v", err)
	}
	compose := string(composeData)
	if strings.Contains(compose, "\n  fits:\n") {
		t.Fatalf("expected fits service removed, got:\n%s", compose)
	}
	if !strings.Contains(compose, "\n  crayfits:\n") {
		t.Fatalf("expected crayfits service preserved, got:\n%s", compose)
	}
	if !strings.Contains(compose, `CRAYFITS_WEBSERVICE_URI: "https://microservices.libops.site/fits/examine"`) {
		t.Fatalf("expected crayfits to point at distributed fits, got:\n%s", compose)
	}
	if strings.Contains(compose, "ALPACA_DERIVATIVE_FITS_URL") {
		t.Fatalf("expected alpaca to keep using local crayfits when only fits is distributed, got:\n%s", compose)
	}

	devComposeData, err := os.ReadFile(filepath.Join(projectDir, "docker-compose.dev.yml"))
	if err != nil {
		t.Fatalf("ReadFile(dev compose) error = %v", err)
	}
	devCompose := string(devComposeData)
	if !strings.Contains(devCompose, "\n  fits:\n") {
		t.Fatalf("expected dev compose to contain fits service, got:\n%s", devCompose)
	}
	if !strings.Contains(devCompose, `CRAYFITS_WEBSERVICE_URI: "http://fits:8080/fits/examine"`) {
		t.Fatalf("expected dev compose to reset crayfits fits URL, got:\n%s", devCompose)
	}
}

func TestApplyDerivativeServicesDistributedCrayfitsUsesManagedCrayfits(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectDir, "docker-compose.yml"), []byte(`
services:
  alpaca:
    environment: {}
  crayfits:
    image: libops/crayfits:test
  fits:
    image: libops/fits:test
`), 0o644); err != nil {
		t.Fatalf("WriteFile(compose) error = %v", err)
	}

	if err := ApplyDerivativeServices(Options{
		Path: projectDir,
		DerivativeServices: map[string]string{
			"crayfits": DerivativeTopologyDistributed,
			"fits":     DerivativeTopologyDistributed,
		},
	}); err != nil {
		t.Fatalf("ApplyDerivativeServices() error = %v", err)
	}

	composeData, err := os.ReadFile(filepath.Join(projectDir, "docker-compose.yml"))
	if err != nil {
		t.Fatalf("ReadFile(compose) error = %v", err)
	}
	compose := string(composeData)
	if strings.Contains(compose, "\n  crayfits:\n") || strings.Contains(compose, "\n  fits:\n") {
		t.Fatalf("expected crayfits and fits services removed, got:\n%s", compose)
	}
	if !strings.Contains(compose, `ALPACA_DERIVATIVE_FITS_URL: "https://microservices.libops.site/crayfits"`) {
		t.Fatalf("expected alpaca to point at managed crayfits, got:\n%s", compose)
	}

	devComposeData, err := os.ReadFile(filepath.Join(projectDir, "docker-compose.dev.yml"))
	if err != nil {
		t.Fatalf("ReadFile(dev compose) error = %v", err)
	}
	devCompose := string(devComposeData)
	if !strings.Contains(devCompose, "\n  crayfits:\n") || !strings.Contains(devCompose, "\n  fits:\n") {
		t.Fatalf("expected dev compose to contain crayfits and fits services, got:\n%s", devCompose)
	}
	if !strings.Contains(devCompose, `ALPACA_DERIVATIVE_FITS_URL: "http://crayfits:8080/"`) {
		t.Fatalf("expected dev compose to reset alpaca fits URL, got:\n%s", devCompose)
	}
	if !strings.Contains(devCompose, `CRAYFITS_WEBSERVICE_URI: "http://fits:8080/fits/examine"`) {
		t.Fatalf("expected dev compose to reset crayfits fits URL, got:\n%s", devCompose)
	}
}

func writeTestFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s) error = %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", path, err)
	}
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", path, err)
	}
	return string(data)
}

func writeApplyMatrixProject(t *testing.T, projectDir string) {
	t.Helper()
	drupalRoot := filepath.Join(projectDir, "drupal", "rootfs", "var", "www", "drupal")
	configDir := filepath.Join(drupalRoot, "config", "sync")
	for _, dir := range []string{
		filepath.Join(projectDir, "conf", "traefik"),
		filepath.Join(projectDir, "drupal"),
		filepath.Join(projectDir, "drupal", "rootfs", "etc", "s6-overlay", "scripts"),
		filepath.Join(projectDir, "drupal", "rootfs", "opt", "solr"),
		filepath.Join(drupalRoot, "assets"),
		configDir,
		filepath.Join(drupalRoot, "recipes"),
		filepath.Join(drupalRoot, "web", "modules", "custom"),
		filepath.Join(drupalRoot, "web", "themes", "custom"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll(%s) error = %v", dir, err)
		}
	}
	writeTestFile(t, filepath.Join(projectDir, "docker-compose.yml"), `services:
  alpaca:
    environment:
      ALPACA_FCREPO_INDEXER_ENABLED: "true"
      ALPACA_TRIPLESTORE_INDEXER_ENABLED: "true"
  blazegraph:
    image: islandora/blazegraph
  cantaloupe:
    image: islandora/cantaloupe
  drupal:
    build:
      context: ./drupal
    environment:
      DRUPAL_DEFAULT_CANTALOUPE_URL: ${URI_SCHEME}://${DOMAIN}/cantaloupe/iiif/2
      DRUPAL_DEFAULT_FCREPO_HOST: fcrepo
      DRUPAL_DEFAULT_FCREPO_PORT: 8080
      DRUPAL_DEFAULT_FCREPO_URL: ${URI_SCHEME}://fcrepo.${DOMAIN}/fcrepo/rest/
      DRUPAL_DEFAULT_TRIPLESTORE_NAMESPACE: islandora
  fcrepo:
    image: islandora/fcrepo6
  init:
    volumes:
      - ./drupal:/drupal:rw
  traefik:
    environment: {}
volumes:
  blazegraph-data: {}
  cantaloupe-data: {}
  fcrepo-data: {}
`)
	writeTestFile(t, filepath.Join(projectDir, "docker-compose.dev.yml"), `services:
  drupal:
    volumes:
      - ./drupal/rootfs/var/www/drupal/assets:/var/www/drupal/assets:z,rw,${CONSISTENCY}
      - ./drupal/rootfs/var/www/drupal/composer.json:/var/www/drupal/composer.json:z,rw,${CONSISTENCY}
      - ./drupal/rootfs/var/www/drupal/config:/var/www/drupal/config:z,rw,${CONSISTENCY}
      - ./drupal/rootfs/var/www/drupal/web/modules/custom:/var/www/drupal/web/modules/custom:z,rw,${CONSISTENCY}
      - ./drupal/rootfs/var/www/drupal/web/themes/custom:/var/www/drupal/web/themes/custom:z,rw,${CONSISTENCY}
`)
	writeTestFile(t, filepath.Join(projectDir, "conf", "traefik", "cantaloupe.yml"), "http: {}\n")
	writeTestFile(t, filepath.Join(projectDir, "drupal", "Dockerfile"), `# syntax=docker/dockerfile:1.23.0
ARG REPOSITORY
ARG TAG
FROM ${REPOSITORY}/drupal:${TAG}

ARG TARGETARCH

COPY --link rootfs /

RUN --mount=type=cache,id=custom-drupal-composer-${TARGETARCH},sharing=locked,target=/root/.composer/cache \
    composer install -d /var/www/drupal && \
    chown -R nginx:nginx /var/www/drupal && \
    cleanup.sh
`)
	writeTestFile(t, filepath.Join(projectDir, "drupal", ".dockerignore"), "README.md\n")
	for _, rel := range []string{
		"assets/default_settings.txt",
		"composer.json",
		"composer.lock",
		"recipes/README.txt",
		"web/modules/custom/.gitkeep",
		"web/themes/custom/.gitkeep",
	} {
		writeTestFile(t, filepath.Join(drupalRoot, rel), rel+"\n")
	}
	writeTestFile(t, filepath.Join(drupalRoot, "web", "robots.txt"), "User-agent: *\nDisallow: /cantaloupe/*\n")
	writeTestFile(t, filepath.Join(configDir, "field.storage.media.field_media_file.yml"), "settings:\n  uri_scheme: fedora\n")
	writeTestFile(t, filepath.Join(configDir, "views.view.files.yml"), "value: 'fedora://'\n")
	writeTestFile(t, filepath.Join(configDir, "context.context.all_media.yml"), "reactions:\n  index:\n    actions:\n      index_media_in_triplestore: index_media_in_triplestore\n  delete:\n    actions:\n      delete_media_from_triplestore: delete_media_from_triplestore\n")
	writeTestFile(t, filepath.Join(configDir, "context.context.repository_content.yml"), "reactions:\n  index:\n    actions:\n      index_node_in_triplestore: index_node_in_triplestore\n  delete:\n    actions:\n      delete_node_from_triplestore: delete_node_from_triplestore\n")
	writeTestFile(t, filepath.Join(configDir, "context.context.taxonomy_terms.yml"), "reactions:\n  index:\n    actions:\n      index_taxonomy_term_in_the_triplestore: index_taxonomy_term_in_the_triplestore\n  delete:\n    actions:\n      delete_taxonomy_term_in_triplestore: delete_taxonomy_term_in_triplestore\n")
	for _, name := range append([]string{}, fedoraCleanupFiles...) {
		writeTestFile(t, filepath.Join(configDir, name), "id: "+name+"\n")
	}
	for _, name := range append([]string{}, blazegraphCleanupFiles...) {
		writeTestFile(t, filepath.Join(configDir, name), "id: "+name+"\n")
	}
}

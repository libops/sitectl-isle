package create

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	corecomponent "github.com/libops/sitectl/pkg/component"
	"gopkg.in/yaml.v3"
)

func TestApplyFcrepoOffPublic(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	configDir := filepath.Join(projectDir, DefaultDrupalRootfs, "config", "sync")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.MkdirAll(filepath.Join(projectDir, "conf", "traefik"), 0o755); err != nil {
		t.Fatalf("MkdirAll(conf/traefik) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "conf", "traefik", "fcrepo.yml"), []byte("http: {}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(fcrepo route) error = %v", err)
	}

	if err := os.WriteFile(filepath.Join(projectDir, "docker-compose.yml"), []byte(`
services:
  alpaca:
    environment:
      ALPACA_FCREPO_INDEXER_ENABLED: "true"
      ALPACA_TRIPLESTORE_INDEXER_ENABLED: "true"
  blazegraph:
    image: islandora/blazegraph:main
  drupal:
    depends_on:
      database-init:
        condition: service_completed_successfully
    environment:
      DRUPAL_DEFAULT_FCREPO_HOST: fcrepo
      DRUPAL_DEFAULT_FCREPO_PORT: 8080
      DRUPAL_DEFAULT_FCREPO_URL: http://fcrepo.example/fcrepo/rest/
      DRUPAL_DEFAULT_SITE_URL: http://drupal.internal
      DRUPAL_DEFAULT_TRIPLESTORE_NAMESPACE: temporary
      DRUSH_OPTIONS_URI: http://drupal.internal
  database-init:
    image: libops/base:3
  fcrepo:
    image: islandora/fcrepo6
  fcrepo-database-init:
    image: libops/base:3
  milliner:
    image: islandora/milliner
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
	assertApplicationDatabaseBootstrap(t, projectDir)
	if strings.Contains(compose, "fcrepo:") {
		t.Fatalf("expected fcrepo service removed, got:\n%s", compose)
	}
	if strings.Contains(compose, "milliner:") {
		t.Fatalf("expected milliner service removed, got:\n%s", compose)
	}
	if strings.Contains(compose, "DRUPAL_DEFAULT_FCREPO_URL") {
		t.Fatalf("expected fcrepo env removed, got:\n%s", compose)
	}
	for _, want := range []string{
		`DRUPAL_DEFAULT_SITE_URL: "http://localhost"`,
		`DRUSH_OPTIONS_URI: "http://localhost"`,
	} {
		if !strings.Contains(compose, want) {
			t.Fatalf("expected compose to contain %q, got:\n%s", want, compose)
		}
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
	if _, err := os.Stat(filepath.Join(projectDir, "conf", "traefik", "fcrepo.yml")); !os.IsNotExist(err) {
		t.Fatalf("expected fcrepo Traefik route deleted, stat err = %v", err)
	}
}

func TestApplyFcrepoOnRestoresFcrepoAndMilliner(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	configDir := filepath.Join(projectDir, DefaultDrupalRootfs, "config", "sync")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(config) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "docker-compose.yml"), []byte(`
x-common: &common
  restart: unless-stopped
services:
  activemq:
    <<: *common
    image: libops/activemq:${ISLANDORA_TAG}
  alpaca:
    <<: *common
    environment:
      ALPACA_FCREPO_INDEXER_ENABLED: "false"
  drupal:
    <<: *common
    depends_on:
      database-init:
        condition: service_completed_successfully
      mariadb:
        condition: service_healthy
    environment: {}
    secrets:
      - source: DRUPAL_DEFAULT_DB_PASSWORD
        target: DB_PASSWORD
  database-init:
    image: libops/base:3@sha256:test
  fcrepo-database-init:
    image: libops/base:3@sha256:test
volumes: {}
`), 0o644); err != nil {
		t.Fatalf("WriteFile(compose) error = %v", err)
	}

	if err := Apply(Options{
		Path:       projectDir,
		Fcrepo:     FcrepoStateOn,
		Blazegraph: FcrepoStateOff,
	}); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	composeData, err := os.ReadFile(filepath.Join(projectDir, "docker-compose.yml"))
	if err != nil {
		t.Fatalf("ReadFile(compose) error = %v", err)
	}
	compose := string(composeData)
	assertApplicationDatabaseBootstrap(t, projectDir)
	for _, want := range []string{
		"\n  fcrepo:\n",
		"\n  milliner:\n",
		"  fcrepo-data: {}",
		"image: libops/fcrepo:7@sha256:d4d0fd92424e751199ee87117f85a99b147ef92d0b65544794184e0a52cb4db3",
		"image: islandora/milliner:main@sha256:b8032d819de5412d0a4db6a8ac8d5dd3a61b2e097af0a707d0ae4fcd03f22ca2",
		`ALPACA_FCREPO_INDEXER_ENABLED: "true"`,
		`DRUPAL_DEFAULT_FCREPO_URL: "http://fcrepo:8080/fcrepo/rest/"`,
		`DRUPAL_DEFAULT_SITE_URL: "http://localhost"`,
		`DRUSH_OPTIONS_URI: "http://drupal.internal"`,
		`DRUPAL_TRUSTED_HOST_PATTERNS: "^localhost$,^drupal\\.internal$"`,
		`FCREPO_ALLOW_EXTERNAL_DRUPAL: "http://drupal.internal/"`,
		`target: DB_PASSWORD`,
	} {
		if !strings.Contains(compose, want) {
			t.Fatalf("expected restored compose to contain %q, got:\n%s", want, compose)
		}
	}
	for _, name := range []string{
		"user.role.fedoraadmin.yml",
		"system.action.user_add_role_action.fedoraadmin.yml",
		"views.view.non_fedora_files.yml",
	} {
		if _, err := os.Stat(filepath.Join(configDir, name)); err != nil {
			t.Fatalf("expected fcrepo config %s restored: %v", name, err)
		}
	}
	parsed, err := corecomponent.LoadComposeFile(filepath.Join(projectDir, "docker-compose.yml"))
	if err != nil {
		t.Fatalf("LoadComposeFile() error = %v", err)
	}
	fcrepoBlock, ok := parsed.ServiceBlock("fcrepo")
	if !ok {
		t.Fatal("expected fcrepo service block")
	}
	for _, want := range []string{`DB_BOOTSTRAP_ENABLED: "true"`, "source: DB_ROOT_PASSWORD", "source: FCREPO_DB_PASSWORD", "target: DB_PASSWORD", "source: TOMCAT_ADMIN_PASSWORD", "source: JWT_ADMIN_TOKEN", "source: JWT_PUBLIC_KEY"} {
		if !strings.Contains(fcrepoBlock, want) {
			t.Fatalf("fcrepo block missing %q:\n%s", want, fcrepoBlock)
		}
	}
	if _, ok := parsed.SectionEntryBlock("secrets", "TOMCAT_ADMIN_PASSWORD"); !ok {
		t.Fatal("expected TOMCAT_ADMIN_PASSWORD top-level secret")
	}
	if parsed.HasService(legacyFcrepoDatabaseInit) {
		t.Fatal("expected legacy fcrepo database initializer removed")
	}
	drupalBlock, ok := parsed.ServiceBlock("drupal")
	if !ok {
		t.Fatal("expected drupal service block")
	}
	for _, want := range []string{"mariadb:", "source: DRUPAL_DEFAULT_DB_PASSWORD", "target: DB_PASSWORD"} {
		if !strings.Contains(drupalBlock, want) {
			t.Fatalf("drupal block lost %q while replacing its database initializer:\n%s", want, drupalBlock)
		}
	}
	route, err := os.ReadFile(filepath.Join(projectDir, "conf", "traefik", "fcrepo.yml"))
	if err != nil {
		t.Fatalf("ReadFile(fcrepo route) error = %v", err)
	}
	for _, want := range []string{"PathPrefix(`/fcrepo`)", "http://fcrepo:8080", "fcrepo-strip-suffix"} {
		if !strings.Contains(string(route), want) {
			t.Fatalf("restored fcrepo route missing %q:\n%s", want, route)
		}
	}
}

func TestSyncLocalDrupalInternalIngress(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectDir, "conf", "traefik"), 0o755); err != nil {
		t.Fatalf("MkdirAll(conf/traefik) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "docker-compose.yml"), []byte(`
services:
  traefik:
    networks:
      default:
        aliases:
          - fcrepo.localhost
networks:
  default: {}
`), 0o644); err != nil {
		t.Fatalf("WriteFile(compose) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "conf", "traefik", "drupal.yml"), []byte(`
http:
  services:
    drupal:
      loadBalancer:
        servers:
          - url: http://drupal:80
  routers:
    drupal:
      rule: Host(`+"`"+`localhost`+"`"+`)
      entryPoints:
        - http
      service: drupal
{{- if (eq (env "TLS_PROVIDER") "letsencrypt") }}
      tls:
        certResolver: letsencrypt
{{- else if (eq (env "URI_SCHEME") "https") }}
      tls: {}
{{- end }}
`), 0o644); err != nil {
		t.Fatalf("WriteFile(drupal router) error = %v", err)
	}

	if err := SyncLocalDrupalInternalIngress(projectDir, true); err != nil {
		t.Fatalf("SyncLocalDrupalInternalIngress(true) error = %v", err)
	}

	compose := readTestFile(t, filepath.Join(projectDir, "docker-compose.yml"))
	for _, want := range []string{"fcrepo.localhost", "drupal.internal"} {
		if !strings.Contains(compose, want) {
			t.Fatalf("expected compose to contain %q, got:\n%s", want, compose)
		}
	}
	router := readTestFile(t, filepath.Join(projectDir, "conf", "traefik", "drupal.yml"))
	for _, want := range []string{
		"drupal-internal:",
		"Host(`drupal.internal`)",
		"priority: 9000",
		"entryPoints:\n        - http",
		`{{- if (eq (env "TLS_PROVIDER") "letsencrypt") }}`,
		`{{- end }}`,
	} {
		if !strings.Contains(router, want) {
			t.Fatalf("expected router to contain %q, got:\n%s", want, router)
		}
	}
	for _, absent := range []string{"drupal-internal-host", "Host: localhost", "middlewares:"} {
		if strings.Contains(router, absent) {
			t.Fatalf("expected router not to contain %q, got:\n%s", absent, router)
		}
	}

	if err := SyncLocalDrupalInternalIngress(projectDir, false); err != nil {
		t.Fatalf("SyncLocalDrupalInternalIngress(false) error = %v", err)
	}

	compose = readTestFile(t, filepath.Join(projectDir, "docker-compose.yml"))
	if strings.Contains(compose, "drupal.internal") || !strings.Contains(compose, "fcrepo.localhost") {
		t.Fatalf("expected only local Drupal alias removed, got:\n%s", compose)
	}
	router = readTestFile(t, filepath.Join(projectDir, "conf", "traefik", "drupal.yml"))
	if strings.Contains(router, "drupal-internal") || strings.Contains(router, "drupal-internal-host") {
		t.Fatalf("expected local Drupal router removed, got:\n%s", router)
	}
	if strings.Contains(router, "middlewares: {}") {
		t.Fatalf("expected empty middleware map pruned, got:\n%s", router)
	}
}

func TestTrustedHostPatterns(t *testing.T) {
	t.Parallel()

	if got := TrustedHostPatterns("repo.example.org", false); got != `^repo\.example\.org$` {
		t.Fatalf("TrustedHostPatterns(public) = %q", got)
	}
	if got := TrustedHostPatterns("localhost", true); got != `^localhost$,^drupal\.internal$` {
		t.Fatalf("TrustedHostPatterns(local) = %q", got)
	}
}

func TestApplyRepositoryComponentsRestoresBlazegraphRuntimeAndDrupalConfig(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectDir, "docker-compose.yml"), []byte(`
x-common: &common
  restart: unless-stopped
services:
  alpaca:
    <<: *common
    environment:
      ALPACA_TRIPLESTORE_INDEXER_ENABLED: "false"
  drupal:
    <<: *common
    environment: {}
volumes: {}
`), 0o644); err != nil {
		t.Fatalf("WriteFile(compose) error = %v", err)
	}

	if err := applyRepositoryComponents(Options{
		Path:              projectDir,
		DrupalRootfs:      DefaultDrupalRootfs,
		Fcrepo:            FcrepoStateOff,
		Blazegraph:        FcrepoStateOn,
		ISLEFileSystemURI: PrivateISLEFileSystemURI,
	}); err != nil {
		t.Fatalf("applyRepositoryComponents() error = %v", err)
	}

	composeData, err := os.ReadFile(filepath.Join(projectDir, "docker-compose.yml"))
	if err != nil {
		t.Fatalf("ReadFile(compose) error = %v", err)
	}
	compose := string(composeData)
	for _, want := range []string{
		"\n  blazegraph:\n",
		"    <<: *common\n    image: " + blazegraphImageRef,
		"      - blazegraph-data:/data:rw",
		"  blazegraph-data: {}",
		`ALPACA_TRIPLESTORE_INDEXER_ENABLED: "true"`,
		`DRUPAL_DEFAULT_TRIPLESTORE_NAMESPACE: "islandora"`,
	} {
		if !strings.Contains(compose, want) {
			t.Fatalf("expected restored blazegraph compose to contain %q, got:\n%s", want, compose)
		}
	}

	assertRepositoryComponentState(t, projectDir, false, true)
}

func TestApplyBlazegraphOnRewritesExistingImage(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectDir, "docker-compose.yml"), []byte(`
services:
  alpaca:
    environment: {}
  blazegraph:
    image: registry.invalid/blazegraph@sha256:old
    labels:
      com.example.preserve: "true"
  drupal:
    environment: {}
volumes:
  blazegraph-data: {}
`), 0o644); err != nil {
		t.Fatalf("WriteFile(compose) error = %v", err)
	}

	if err := applyBlazegraphOn(projectDir, DefaultDrupalRootfs); err != nil {
		t.Fatalf("applyBlazegraphOn() error = %v", err)
	}

	composeData, err := os.ReadFile(filepath.Join(projectDir, "docker-compose.yml"))
	if err != nil {
		t.Fatalf("ReadFile(compose) error = %v", err)
	}
	compose := string(composeData)
	if strings.Contains(compose, "registry.invalid/blazegraph") {
		t.Fatalf("expected stale blazegraph image rewritten, got:\n%s", compose)
	}
	if !strings.Contains(compose, "image: "+blazegraphImageRef) {
		t.Fatalf("expected pinned LibOps blazegraph image, got:\n%s", compose)
	}
	if !strings.Contains(compose, `com.example.preserve: "true"`) {
		t.Fatalf("expected existing service config preserved, got:\n%s", compose)
	}
}

func TestApplyRepositoryComponentsReconcilesEveryStateTransition(t *testing.T) {
	t.Parallel()

	states := []struct {
		name       string
		fcrepo     string
		blazegraph string
	}{
		{name: "both disabled", fcrepo: FcrepoStateOff, blazegraph: FcrepoStateOff},
		{name: "standalone blazegraph", fcrepo: FcrepoStateOff, blazegraph: FcrepoStateOn},
		{name: "standalone fcrepo", fcrepo: FcrepoStateOn, blazegraph: FcrepoStateOff},
		{name: "both enabled", fcrepo: FcrepoStateOn, blazegraph: FcrepoStateOn},
	}

	for _, source := range states {
		for _, target := range states {
			t.Run(source.name+" to "+target.name, func(t *testing.T) {
				t.Parallel()

				projectDir := t.TempDir()
				writeRepositoryComponentsOffFixture(t, projectDir)
				applyRepositoryState(t, projectDir, source.fcrepo, source.blazegraph)
				seedRepositoryContextSentinels(t, projectDir)
				sourceActionSentinel := source.blazegraph == FcrepoStateOn
				if sourceActionSentinel {
					appendActionSentinel(t, projectDir)
				}

				applyRepositoryState(t, projectDir, target.fcrepo, target.blazegraph)
				fcrepoEnabled := target.fcrepo == FcrepoStateOn
				blazegraphEnabled := target.blazegraph == FcrepoStateOn
				assertRepositoryComponentState(t, projectDir, fcrepoEnabled, blazegraphEnabled)
				assertRepositoryContextSentinels(t, projectDir)
				assertActionSentinel(t, projectDir, blazegraphEnabled && sourceActionSentinel)

				wantSnapshot := repositoryComponentSnapshot(t, projectDir)
				applyRepositoryState(t, projectDir, target.fcrepo, target.blazegraph)
				if got := repositoryComponentSnapshot(t, projectDir); got != wantSnapshot {
					t.Fatalf("repeated repository reconciliation was not idempotent\n--- first ---\n%s\n--- second ---\n%s", wantSnapshot, got)
				}
			})
		}
	}
}

func TestApplyRepositoryComponentsHandlesMissingConfigDirectory(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	writeRepositoryComponentsOffFixture(t, projectDir)
	drupalRoot := filepath.Join(projectDir, DefaultDrupalRootfs)
	if err := os.RemoveAll(drupalRoot); err != nil {
		t.Fatalf("RemoveAll(Drupal root) error = %v", err)
	}

	applyRepositoryState(t, projectDir, FcrepoStateOff, FcrepoStateOff)
	if _, err := os.Stat(drupalRoot); !os.IsNotExist(err) {
		t.Fatalf("disabled reconciliation created absent Drupal root, stat error = %v", err)
	}

	applyRepositoryState(t, projectDir, FcrepoStateOff, FcrepoStateOn)
	assertRepositoryComponentState(t, projectDir, false, true)
}

func TestApplyRepositoryComponentsRejectsConfigDirectorySymlinkEscape(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	writeRepositoryComponentsOffFixture(t, projectDir)
	configDir := filepath.Join(projectDir, DefaultDrupalRootfs, "config", "sync")
	outsideDir := t.TempDir()
	outsidePath := filepath.Join(outsideDir, "sentinel.yml")
	writeTestFile(t, outsidePath, "outside: unchanged\n")
	if err := os.RemoveAll(configDir); err != nil {
		t.Fatalf("RemoveAll(config dir) error = %v", err)
	}
	if err := os.Symlink(outsideDir, configDir); err != nil {
		t.Skipf("Symlink(config dir) is unavailable: %v", err)
	}

	err := applyRepositoryComponents(Options{
		Path:              projectDir,
		DrupalRootfs:      DefaultDrupalRootfs,
		Fcrepo:            FcrepoStateOff,
		Blazegraph:        FcrepoStateOn,
		ISLEFileSystemURI: PrivateISLEFileSystemURI,
	})
	if err == nil {
		t.Fatal("applyRepositoryComponents() succeeded through escaping config directory symlink")
	}
	if got := readTestFile(t, outsidePath); got != "outside: unchanged\n" {
		t.Fatalf("outside sentinel changed through config directory symlink: %q", got)
	}
}

func TestApplyRepositoryComponentsRejectsManagedLeafSymlinkEscape(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	writeRepositoryComponentsOffFixture(t, projectDir)
	configDir := filepath.Join(projectDir, DefaultDrupalRootfs, "config", "sync")
	outsidePath := filepath.Join(t.TempDir(), "outside-context.yml")
	writeTestFile(t, outsidePath, "outside: unchanged\n")
	contextPath := filepath.Join(configDir, repositoryContextSpecs[0].Name)
	if err := os.Symlink(outsidePath, contextPath); err != nil {
		t.Skipf("Symlink(shared context) is unavailable: %v", err)
	}

	err := applyRepositoryComponents(Options{
		Path:              projectDir,
		DrupalRootfs:      DefaultDrupalRootfs,
		Fcrepo:            FcrepoStateOn,
		Blazegraph:        FcrepoStateOn,
		ISLEFileSystemURI: PrivateISLEFileSystemURI,
	})
	if err == nil {
		t.Fatal("applyRepositoryComponents() succeeded through escaping managed leaf symlink")
	}
	if got := readTestFile(t, outsidePath); got != "outside: unchanged\n" {
		t.Fatalf("outside context changed through managed leaf symlink: %q", got)
	}
}

func TestApplyBlazegraphOnRejectsInRootActionAlias(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	writeRepositoryComponentsOffFixture(t, projectDir)
	configDir := filepath.Join(projectDir, DefaultDrupalRootfs, "config", "sync")
	jsonLDPath := filepath.Join(configDir, "jsonld.settings.yml")
	writeTestFile(t, jsonLDPath, "outside-managed-action: unchanged\n")
	actionPath := filepath.Join(configDir, blazegraphCleanupFiles[0])
	if err := os.Symlink("jsonld.settings.yml", actionPath); err != nil {
		t.Skipf("Symlink(action alias) is unavailable: %v", err)
	}

	err := applyBlazegraphOn(projectDir, DefaultDrupalRootfs)
	if err == nil {
		t.Fatal("applyBlazegraphOn() succeeded through in-root managed action alias")
	}
	if got := readTestFile(t, jsonLDPath); got != "outside-managed-action: unchanged\n" {
		t.Fatalf("excluded JSON-LD config changed through action alias: %q", got)
	}
}

func TestApplyFcrepoOffOnRestoresStorageWiring(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	writeRepositoryComponentsOffFixture(t, projectDir)
	configDir := filepath.Join(projectDir, DefaultDrupalRootfs, "config", "sync")
	for _, name := range mediaSchemeFiles {
		writeTestFile(t, filepath.Join(configDir, name), "settings:\n  uri_scheme: fedora\n")
	}
	referencePath := filepath.Join(configDir, "views.view.files.yml")
	writeTestFile(t, referencePath, "uri: 'fedora://media/example'\n")

	applyRepositoryState(t, projectDir, FcrepoStateOff, FcrepoStateOff)
	for _, name := range mediaSchemeFiles {
		if got := readTestFile(t, filepath.Join(configDir, name)); !strings.Contains(got, "uri_scheme: private") && !strings.Contains(got, `uri_scheme: "private"`) {
			t.Fatalf("%s did not transition to private storage:\n%s", name, got)
		}
	}
	if got := readTestFile(t, referencePath); !strings.Contains(got, "private://media/example") {
		t.Fatalf("Fedora URI reference did not transition to private storage: %s", got)
	}

	applyRepositoryState(t, projectDir, FcrepoStateOn, FcrepoStateOff)
	for _, name := range mediaSchemeFiles {
		if got := readTestFile(t, filepath.Join(configDir, name)); !strings.Contains(got, "uri_scheme: fedora") && !strings.Contains(got, `uri_scheme: "fedora"`) {
			t.Fatalf("%s did not transition back to Fedora storage:\n%s", name, got)
		}
	}
	if got := readTestFile(t, referencePath); !strings.Contains(got, "fedora://media/example") {
		t.Fatalf("filesystem URI reference did not transition back to Fedora: %s", got)
	}
}

func TestApplyFcrepoOffRemovesEveryFedoraAdminMappingKey(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	writeRepositoryComponentsOffFixture(t, projectDir)
	path := filepath.Join(projectDir, DefaultDrupalRootfs, "config", "sync", "views.view.roles.yml")
	writeTestFile(t, path, `roles:
  fedoraadmin: 0
nested:
  entries:
    - fedoraadmin: "1"
      keep: true
label: fedoraadmin
`)

	applyRepositoryState(t, projectDir, FcrepoStateOff, FcrepoStateOff)
	data := []byte(readTestFile(t, path))
	for _, yamlPath := range []string{".roles.fedoraadmin", ".nested.entries"} {
		if yamlPath == ".nested.entries" {
			continue
		}
		if _, found, err := testYAMLPathValue(data, yamlPath); err != nil || found {
			t.Fatalf("fedoraadmin mapping path %s found=%t err=%v:\n%s", yamlPath, found, err, data)
		}
	}
	var decoded any
	if err := yaml.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal(cleaned roles) error = %v", err)
	}
	if yamlTreeHasMappingKey(decoded, "fedoraadmin") {
		t.Fatalf("recursive fedoraadmin mapping key remained:\n%s", data)
	}
	if !strings.Contains(string(data), "label: fedoraadmin") {
		t.Fatalf("fedoraadmin scalar value should be preserved:\n%s", data)
	}
}

func TestApplyBlazegraphOnPreservesExistingActionContents(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	writeRepositoryComponentsOffFixture(t, projectDir)
	applyRepositoryState(t, projectDir, FcrepoStateOff, FcrepoStateOn)
	actionPath := filepath.Join(projectDir, DefaultDrupalRootfs, "config", "sync", blazegraphCleanupFiles[0])
	data := []byte(readTestFile(t, actionPath))
	doc, err := corecomponent.LoadYAMLDocument(data)
	if err != nil {
		t.Fatalf("LoadYAMLDocument(action) error = %v", err)
	}
	if err := doc.SetString(".configuration.queue", "downstream-owned-queue"); err != nil {
		t.Fatalf("SetString(action queue) error = %v", err)
	}
	updated, err := doc.Bytes()
	if err != nil {
		t.Fatalf("Bytes(action) error = %v", err)
	}
	writeTestFile(t, actionPath, string(updated))

	applyRepositoryState(t, projectDir, FcrepoStateOff, FcrepoStateOn)
	if got := readTestFile(t, actionPath); !strings.Contains(got, "downstream-owned-queue") {
		t.Fatalf("Blazegraph apply overwrote downstream-owned action contents:\n%s", got)
	}
}

func TestApplyFcrepoOnPrefersLibopsServicesOverExistingMilliner(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	configDir := filepath.Join(projectDir, DefaultDrupalRootfs, "config", "sync")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(configDir) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "docker-compose.yml"), []byte(`
services:
  activemq:
    image: libops/activemq:nginx-1.30.3-php84
  alpaca:
    environment:
      ALPACA_FCREPO_INDEXER_ENABLED: "false"
  drupal:
    environment: {}
  milliner:
    image: islandora/milliner:6
volumes: {}
`), 0o644); err != nil {
		t.Fatalf("WriteFile(compose) error = %v", err)
	}

	if err := Apply(Options{
		Path:       projectDir,
		Fcrepo:     FcrepoStateOn,
		Blazegraph: FcrepoStateOff,
	}); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	composeData, err := os.ReadFile(filepath.Join(projectDir, "docker-compose.yml"))
	if err != nil {
		t.Fatalf("ReadFile(compose) error = %v", err)
	}
	compose := string(composeData)
	if !strings.Contains(compose, "image: libops/fcrepo:7@sha256:d4d0fd92424e751199ee87117f85a99b147ef92d0b65544794184e0a52cb4db3") {
		t.Fatalf("expected fcrepo to use the current libops fcrepo 7 image, got:\n%s", compose)
	}
	if !strings.Contains(compose, "image: islandora/milliner:6") {
		t.Fatalf("expected existing milliner image left in place, got:\n%s", compose)
	}
}

func TestApplyFcrepoOnDoesNotInferLibopsFcrepoFive(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	configDir := filepath.Join(projectDir, DefaultDrupalRootfs, "config", "sync")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(configDir) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "docker-compose.yml"), []byte(`
services:
  activemq:
    image: libops/activemq:5
  alpaca:
    environment:
      ALPACA_FCREPO_INDEXER_ENABLED: "false"
  drupal:
    environment: {}
volumes: {}
`), 0o644); err != nil {
		t.Fatalf("WriteFile(compose) error = %v", err)
	}

	if err := Apply(Options{
		Path:       projectDir,
		Fcrepo:     FcrepoStateOn,
		Blazegraph: FcrepoStateOff,
	}); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	composeData, err := os.ReadFile(filepath.Join(projectDir, "docker-compose.yml"))
	if err != nil {
		t.Fatalf("ReadFile(compose) error = %v", err)
	}
	compose := string(composeData)
	if strings.Contains(compose, "image: libops/fcrepo:5") {
		t.Fatalf("expected fcrepo restore to avoid nonexistent libops/fcrepo:5, got:\n%s", compose)
	}
	if !strings.Contains(compose, "image: libops/fcrepo:7@sha256:d4d0fd92424e751199ee87117f85a99b147ef92d0b65544794184e0a52cb4db3") {
		t.Fatalf("expected fcrepo restore to use libops/fcrepo:7, got:\n%s", compose)
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
    image: islandora/blazegraph:main
  cantaloupe:
    image: islandora/cantaloupe
  drupal:
    environment:
      DRUPAL_DEFAULT_CANTALOUPE_URL: http://localhost/cantaloupe/iiif/2
      DRUPAL_DEFAULT_FCREPO_HOST: fcrepo
      DRUPAL_DEFAULT_FCREPO_PORT: 8080
      DRUPAL_DEFAULT_FCREPO_URL: http://fcrepo.localhost/fcrepo/rest/
      DRUPAL_DEFAULT_TRIPLESTORE_NAMESPACE: islandora
  fcrepo:
    image: islandora/fcrepo6
  milliner:
    image: islandora/milliner
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
	for _, absent := range []string{"\n  fcrepo:\n", "\n  milliner:\n", "\n  blazegraph:\n", "fcrepo-data", "blazegraph-data", "condition: service_healthy", "source: fcrepo-data"} {
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
    image: islandora/blazegraph:main
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
	original := []byte(`services:
  activemq:
    image: libops/activemq:5
  alpaca:
    environment: {}
  drupal:
    environment: {}
  blazegraph:
    image: islandora/blazegraph:main
  fcrepo:
    depends_on:
      activemq:
        condition: service_healthy
      fcrepo-database-init:
        condition: service_completed_successfully
    image: islandora/fcrepo6
  fcrepo-database-init:
    image: libops/base:3
`)
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
	assertApplicationDatabaseBootstrap(t, projectDir)
	parsed, err := corecomponent.LoadComposeFile(composePath)
	if err != nil {
		t.Fatalf("LoadComposeFile() error = %v", err)
	}
	fcrepoBlock, ok := parsed.ServiceBlock("fcrepo")
	if !ok {
		t.Fatal("expected existing fcrepo service preserved")
	}
	for _, want := range []string{`DB_BOOTSTRAP_ENABLED: "true"`, "source: DB_ROOT_PASSWORD"} {
		if !strings.Contains(fcrepoBlock, want) {
			t.Fatalf("existing fcrepo block missing %q:\n%s", want, fcrepoBlock)
		}
	}
	if parsed.HasService(legacyFcrepoDatabaseInit) || strings.Contains(fcrepoBlock, legacyFcrepoDatabaseInit) {
		t.Fatalf("expected legacy fcrepo initializer and dependency removed:\n%s", rendered)
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

	writeTestFile(t, filepath.Join(projectDir, "drupal", "Dockerfile"), `ARG REPOSITORY
ARG TAG
FROM ${REPOSITORY}/drupal:${TAG}

ARG TARGETARCH

COPY --link rootfs /

RUN --mount=type=cache,id=custom-drupal-composer-${TARGETARCH},sharing=locked,target=/root/.composer/cache \
    composer install -d /var/www/drupal --no-interaction --no-progress --prefer-dist --no-dev --optimize-autoloader && \
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
		"ARG BASE_IMAGE=libops/islandora:nginx-1.30.3-php84",
		"FROM ${BASE_IMAGE}",
		"COPY --link composer.json composer.lock /var/www/drupal/",
		"COPY --link drupal/rootfs/opt/ /opt/",
	} {
		if !strings.Contains(dockerfile, want) {
			t.Fatalf("expected Dockerfile to contain %q, got:\n%s", want, dockerfile)
		}
	}
	if strings.Contains(dockerfile, "COPY --link rootfs /") {
		t.Fatalf("expected nested rootfs copy removed, got:\n%s", dockerfile)
	}
	if strings.Contains(dockerfile, "COPY --link drupal/rootfs/etc/ /etc/") {
		t.Fatalf("expected Drupal /etc overlay copy removed, got:\n%s", dockerfile)
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

func TestApplyCodebaseGitRootFromCurrentDrupalLayout(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	drupalRoot := filepath.Join(projectDir, "drupal")
	for _, dir := range []string{
		filepath.Join(drupalRoot, "assets"),
		filepath.Join(drupalRoot, "config", "sync"),
		filepath.Join(drupalRoot, "web", "modules", "custom"),
		filepath.Join(drupalRoot, "web", "themes", "custom"),
		filepath.Join(drupalRoot, "rootfs", "etc", "s6-overlay", "scripts"),
		filepath.Join(drupalRoot, "rootfs", "opt", "solr"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll(%s) error = %v", dir, err)
		}
	}

	writeTestFile(t, filepath.Join(projectDir, "README.md"), "project readme\n")
	writeTestFile(t, filepath.Join(drupalRoot, "README.md"), "drupal readme\n")
	writeTestFile(t, filepath.Join(drupalRoot, "Dockerfile"), `ARG BASE_IMAGE=libops/islandora:php84
FROM ${BASE_IMAGE}

ARG TARGETARCH

COPY --link composer.json composer.lock /var/www/drupal/
COPY --link assets/ /var/www/drupal/assets/
COPY --link rootfs/opt/ /opt/
`)
	writeTestFile(t, filepath.Join(drupalRoot, ".dockerignore"), "README.md\n")
	writeTestFile(t, filepath.Join(projectDir, "docker-compose.yml"), `services:
  init:
    volumes:
      - ./drupal:/drupal:rw
  drupal:
    build:
      context: ./drupal
`)
	for _, rel := range []string{
		"assets/default_settings.txt",
		"composer.json",
		"composer.lock",
		"config/sync/system.site.yml",
		"web/modules/custom/.gitkeep",
		"web/themes/custom/.gitkeep",
	} {
		writeTestFile(t, filepath.Join(drupalRoot, rel), rel+"\n")
	}

	if err := applyCodebaseGitRoot(projectDir); err != nil {
		t.Fatalf("applyCodebaseGitRoot() error = %v", err)
	}

	for _, rel := range []string{"Dockerfile", ".dockerignore", "composer.json", "composer.lock", "assets/default_settings.txt", "config/sync/system.site.yml", "web/modules/custom/.gitkeep"} {
		if _, err := os.Stat(filepath.Join(projectDir, rel)); err != nil {
			t.Fatalf("expected %s at git root: %v", rel, err)
		}
		if _, err := os.Stat(filepath.Join(drupalRoot, rel)); !os.IsNotExist(err) {
			t.Fatalf("expected drupal/%s moved to git root, stat err = %v", rel, err)
		}
	}
	if got := readTestFile(t, filepath.Join(projectDir, "README.md")); got != "project readme\n" {
		t.Fatalf("expected project README preserved, got %q", got)
	}
	if got := readTestFile(t, filepath.Join(drupalRoot, "README.md")); got != "drupal readme\n" {
		t.Fatalf("expected drupal README left in place, got %q", got)
	}

	dockerfile := readTestFile(t, filepath.Join(projectDir, "Dockerfile"))
	for _, want := range []string{
		"COPY --link composer.json composer.lock /var/www/drupal/",
		"COPY --link drupal/rootfs/opt/ /opt/",
	} {
		if !strings.Contains(dockerfile, want) {
			t.Fatalf("expected Dockerfile to contain %q, got:\n%s", want, dockerfile)
		}
	}
	if strings.Contains(dockerfile, "COPY --link drupal/rootfs/etc/ /etc/") {
		t.Fatalf("expected Drupal /etc overlay copy removed, got:\n%s", dockerfile)
	}

	compose := readTestFile(t, filepath.Join(projectDir, "docker-compose.yml"))
	if !strings.Contains(compose, "context: .") || strings.Contains(compose, "context: ./drupal") {
		t.Fatalf("expected drupal build context to be git root, got:\n%s", compose)
	}
	if !strings.Contains(compose, "- .:/drupal:rw") || strings.Contains(compose, "- ./drupal:/drupal:rw") {
		t.Fatalf("expected init volume to mount git root, got:\n%s", compose)
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
				if !strings.Contains(compose, `DRUPAL_DEFAULT_CANTALOUPE_URL: "http://localhost/iiif/3"`) {
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
			assertApplicationDatabaseBootstrap(t, projectDir)
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
    image: islandora/blazegraph:main
    volumes:
      - blazegraph-data:/data:rw
  drupal:
    <<: *common
    environment:
      DRUPAL_DEFAULT_FCREPO_HOST: fcrepo
      DRUPAL_DEFAULT_FCREPO_PORT: 8080
      DRUPAL_DEFAULT_FCREPO_URL: http://fcrepo.localhost/fcrepo/rest/
  fcrepo:
    <<: *common
    image: libops/fcrepo@sha256:611b9b15bf205c369aa664d119126429785da28d255635d8aeeb29ddf4ce03f0
    volumes:
      - fcrepo-data:/data:rw
  milliner:
    <<: *common
    image: islandora/milliner:6
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
	if strings.Contains(rendered, "\n  milliner:\n") {
		t.Fatalf("expected milliner removed, got:\n%s", rendered)
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
  hypercube:
    image: libops/hypercube:test
`), 0o644); err != nil {
		t.Fatalf("WriteFile(compose) error = %v", err)
	}

	if err := ApplyDerivativeServices(Options{
		Path: projectDir,
		DerivativeServices: map[string]string{
			"homarus":   DerivativeTopologyDistributed,
			"hypercube": DerivativeTopologyDistributed,
		},
	}); err != nil {
		t.Fatalf("ApplyDerivativeServices() error = %v", err)
	}

	composeData, err := os.ReadFile(filepath.Join(projectDir, "docker-compose.yml"))
	if err != nil {
		t.Fatalf("ReadFile(compose) error = %v", err)
	}
	compose := string(composeData)
	for _, service := range []string{"homarus", "hypercube"} {
		if strings.Contains(compose, "\n  "+service+":\n") {
			t.Fatalf("expected %s service removed, got:\n%s", service, compose)
		}
	}
	for _, want := range []string{
		`ALPACA_DERIVATIVE_HOMARUS_URL: "https://microservices.libops.site/homarus"`,
		`ALPACA_DERIVATIVE_OCR_URL: "https://microservices.libops.site/hypercube"`,
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
	for _, service := range []string{"homarus", "hypercube"} {
		if !strings.Contains(devCompose, "\n  "+service+":\n") {
			t.Fatalf("expected dev compose to contain %s service, got:\n%s", service, devCompose)
		}
	}
	for _, want := range []string{
		`ALPACA_DERIVATIVE_HOMARUS_URL: "http://homarus:8080/"`,
		`ALPACA_DERIVATIVE_OCR_URL: "http://hypercube:8080/"`,
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

func writeRepositoryComponentsOffFixture(t *testing.T, projectDir string) {
	t.Helper()
	writeTestFile(t, filepath.Join(projectDir, "docker-compose.yml"), `x-common: &common
  restart: unless-stopped
secrets: {}
services:
  activemq:
    <<: *common
    image: libops/activemq:5
  alpaca:
    <<: *common
    environment:
      ALPACA_FCREPO_INDEXER_ENABLED: "false"
      ALPACA_TRIPLESTORE_INDEXER_ENABLED: "false"
  database-init:
    image: libops/base:3
  drupal:
    <<: *common
    depends_on:
      database-init:
        condition: service_completed_successfully
    environment:
      DRUPAL_DEFAULT_TRIPLESTORE_NAMESPACE: ""
volumes: {}
`)
	if err := os.MkdirAll(filepath.Join(projectDir, DefaultDrupalRootfs, "config", "sync"), 0o755); err != nil {
		t.Fatalf("MkdirAll(config sync) error = %v", err)
	}
}

func assertRepositoryComponentState(t *testing.T, projectDir string, fcrepoEnabled, blazegraphEnabled bool) {
	t.Helper()
	assertApplicationDatabaseBootstrap(t, projectDir)
	compose := readTestFile(t, filepath.Join(projectDir, "docker-compose.yml"))
	for service, want := range map[string]bool{
		"fcrepo":     fcrepoEnabled,
		"milliner":   fcrepoEnabled,
		"blazegraph": blazegraphEnabled,
	} {
		got := strings.Contains(compose, "\n  "+service+":\n")
		if got != want {
			t.Fatalf("compose service %s present = %t, want %t:\n%s", service, got, want, compose)
		}
	}
	for value, want := range map[string]bool{
		`ALPACA_FCREPO_INDEXER_ENABLED: "true"`:             fcrepoEnabled,
		`ALPACA_TRIPLESTORE_INDEXER_ENABLED: "true"`:        blazegraphEnabled,
		`DRUPAL_DEFAULT_TRIPLESTORE_NAMESPACE: "islandora"`: blazegraphEnabled,
	} {
		if got := strings.Contains(compose, value); got != want {
			t.Fatalf("compose contains %q = %t, want %t:\n%s", value, got, want, compose)
		}
	}

	configDir := filepath.Join(projectDir, DefaultDrupalRootfs, "config", "sync")
	for _, spec := range repositoryContextSpecs {
		path := filepath.Join(configDir, spec.Name)
		data, exists := readOptionalTestFile(t, path)
		if !exists {
			if fcrepoEnabled || blazegraphEnabled {
				t.Fatalf("shared context %s is missing while a repository component is enabled", spec.Name)
			}
			continue
		}
		for _, reaction := range spec.BlazegraphReactions {
			assertYAMLReactionState(t, data, spec.Name, reaction, blazegraphEnabled)
		}
		for _, reaction := range spec.FcrepoReactions {
			assertYAMLReactionState(t, data, spec.Name, reaction, fcrepoEnabled)
		}
	}

	for _, name := range blazegraphCleanupFiles {
		if got := testFileExists(t, filepath.Join(configDir, name)); got != blazegraphEnabled {
			t.Fatalf("Blazegraph action %s exists = %t, want %t", name, got, blazegraphEnabled)
		}
	}
	for _, name := range fedoraCleanupFiles {
		if got := testFileExists(t, filepath.Join(configDir, name)); got != fcrepoEnabled {
			t.Fatalf("fcrepo config %s exists = %t, want %t", name, got, fcrepoEnabled)
		}
	}
}

func assertApplicationDatabaseBootstrap(t *testing.T, projectDir string) {
	t.Helper()
	composePath := filepath.Join(projectDir, "docker-compose.yml")
	data, err := os.ReadFile(composePath)
	if err != nil {
		t.Fatalf("ReadFile(compose) error = %v", err)
	}
	var document map[string]any
	if err := yaml.Unmarshal(data, &document); err != nil {
		t.Fatalf("Unmarshal(compose) error = %v", err)
	}
	secrets := yamlMap(document["secrets"])
	if _, ok := secrets["DB_ROOT_PASSWORD"]; !ok {
		t.Fatal("expected top-level DB_ROOT_PASSWORD secret")
	}
	compose, err := corecomponent.LoadComposeFile(composePath)
	if err != nil {
		t.Fatalf("LoadComposeFile() error = %v", err)
	}
	if compose.HasService(databaseInitServiceName) {
		t.Fatal("expected standalone database initializer removed")
	}
	drupalBlock, ok := compose.ServiceBlock("drupal")
	if !ok {
		t.Fatal("expected drupal service")
	}
	if !strings.Contains(drupalBlock, `DB_BOOTSTRAP_ENABLED: "true"`) {
		t.Fatalf("drupal block missing database bootstrap flag:\n%s", drupalBlock)
	}
	hasRootSecret, err := composeServiceHasSecret(data, "drupal", "DB_ROOT_PASSWORD")
	if err != nil {
		t.Fatalf("composeServiceHasSecret() error = %v", err)
	}
	if !hasRootSecret {
		t.Fatalf("drupal block missing DB_ROOT_PASSWORD secret:\n%s", drupalBlock)
	}
	if strings.Contains(drupalBlock, databaseInitServiceName+":") {
		t.Fatalf("drupal block retains standalone database initializer dependency:\n%s", drupalBlock)
	}
}

func assertYAMLReactionState(t *testing.T, data []byte, name string, reaction indexingReaction, enabled bool) {
	t.Helper()
	value, found, err := testYAMLPathValue(data, reaction.Path)
	if err != nil {
		t.Fatalf("read YAML path %s in %s: %v", reaction.Path, name, err)
	}
	if found != enabled {
		t.Fatalf("YAML path %s in %s present = %t, want %t", reaction.Path, name, found, enabled)
	}
	if enabled && fmt.Sprint(value) != reaction.Value {
		t.Fatalf("YAML path %s in %s = %v, want %q", reaction.Path, name, value, reaction.Value)
	}
}

func applyRepositoryState(t *testing.T, projectDir, fcrepo, blazegraph string) {
	t.Helper()
	if err := applyRepositoryComponents(Options{
		Path:              projectDir,
		DrupalRootfs:      DefaultDrupalRootfs,
		Fcrepo:            fcrepo,
		Blazegraph:        blazegraph,
		ISLEFileSystemURI: PrivateISLEFileSystemURI,
	}); err != nil {
		t.Fatalf("applyRepositoryComponents(fcrepo=%s, blazegraph=%s) error = %v", fcrepo, blazegraph, err)
	}
}

func seedRepositoryContextSentinels(t *testing.T, projectDir string) {
	t.Helper()
	configDir := filepath.Join(projectDir, DefaultDrupalRootfs, "config", "sync")
	for _, spec := range repositoryContextSpecs {
		path := filepath.Join(configDir, spec.Name)
		data, exists := readOptionalTestFile(t, path)
		if !exists {
			data = []byte("reactions: {}\n")
		}
		doc, err := corecomponent.LoadYAMLDocument(data)
		if err != nil {
			t.Fatalf("LoadYAMLDocument(%s) error = %v", spec.Name, err)
		}
		for path, value := range map[string]string{
			".description":         "Downstream-owned description",
			".downstream.sentinel": "preserve me",
			".reactions.index.actions.downstream_index_action":   "downstream_index_action",
			".reactions.delete.actions.downstream_delete_action": "downstream_delete_action",
		} {
			if err := doc.SetString(path, value); err != nil {
				t.Fatalf("SetString(%s, %s) error = %v", spec.Name, path, err)
			}
		}
		updated, err := doc.Bytes()
		if err != nil {
			t.Fatalf("Bytes(%s) error = %v", spec.Name, err)
		}
		writeTestFile(t, path, string(updated))
	}
}

func assertRepositoryContextSentinels(t *testing.T, projectDir string) {
	t.Helper()
	configDir := filepath.Join(projectDir, DefaultDrupalRootfs, "config", "sync")
	for _, spec := range repositoryContextSpecs {
		data := []byte(readTestFile(t, filepath.Join(configDir, spec.Name)))
		for path, want := range map[string]string{
			".description":         "Downstream-owned description",
			".downstream.sentinel": "preserve me",
			".reactions.index.actions.downstream_index_action":   "downstream_index_action",
			".reactions.delete.actions.downstream_delete_action": "downstream_delete_action",
		} {
			got, found, err := testYAMLPathValue(data, path)
			if err != nil {
				t.Fatalf("read sentinel %s in %s: %v", path, spec.Name, err)
			}
			if !found || fmt.Sprint(got) != want {
				t.Fatalf("sentinel %s in %s = %v (found=%t), want %q", path, spec.Name, got, found, want)
			}
		}
	}
}

const blazegraphActionSentinel = "x-downstream-sentinel: preserve me"

func appendActionSentinel(t *testing.T, projectDir string) {
	t.Helper()
	path := filepath.Join(projectDir, DefaultDrupalRootfs, "config", "sync", blazegraphCleanupFiles[0])
	contents := strings.TrimRight(readTestFile(t, path), "\n") + "\n" + blazegraphActionSentinel + "\n"
	writeTestFile(t, path, contents)
}

func assertActionSentinel(t *testing.T, projectDir string, want bool) {
	t.Helper()
	path := filepath.Join(projectDir, DefaultDrupalRootfs, "config", "sync", blazegraphCleanupFiles[0])
	data, exists := readOptionalTestFile(t, path)
	got := exists && strings.Contains(string(data), blazegraphActionSentinel)
	if got != want {
		t.Fatalf("Blazegraph action sentinel present = %t, want %t (file exists=%t)", got, want, exists)
	}
}

func testYAMLPathValue(data []byte, path string) (any, bool, error) {
	var root map[string]any
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, false, err
	}
	segments := strings.Split(strings.TrimPrefix(path, "."), ".")
	var current any = root
	for _, segment := range segments {
		mapping, ok := current.(map[string]any)
		if !ok {
			return nil, false, nil
		}
		current, ok = mapping[segment]
		if !ok {
			return nil, false, nil
		}
	}
	return current, true, nil
}

func yamlTreeHasMappingKey(value any, target string) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if key == target || yamlTreeHasMappingKey(child, target) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if yamlTreeHasMappingKey(child, target) {
				return true
			}
		}
	}
	return false
}

func repositoryComponentSnapshot(t *testing.T, projectDir string) string {
	t.Helper()
	paths := []string{
		"docker-compose.yml",
		"conf/traefik/fcrepo.yml",
	}
	seen := map[string]bool{}
	for _, name := range append(append([]string{}, fedoraCleanupFiles...), blazegraphCleanupFiles...) {
		rel := filepath.Join(DefaultDrupalRootfs, "config", "sync", name)
		if !seen[rel] {
			paths = append(paths, rel)
			seen[rel] = true
		}
	}
	for _, spec := range repositoryContextSpecs {
		rel := filepath.Join(DefaultDrupalRootfs, "config", "sync", spec.Name)
		if !seen[rel] {
			paths = append(paths, rel)
			seen[rel] = true
		}
	}

	var snapshot strings.Builder
	for _, rel := range paths {
		data, exists := readOptionalTestFile(t, filepath.Join(projectDir, rel))
		fmt.Fprintf(&snapshot, "--- %s exists=%t\n", filepath.ToSlash(rel), exists)
		if exists {
			snapshot.Write(data)
		}
	}
	return snapshot.String()
}

func readOptionalTestFile(t *testing.T, path string) ([]byte, bool) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err == nil {
		return data, true
	}
	if os.IsNotExist(err) {
		return nil, false
	}
	t.Fatalf("ReadFile(%s) error = %v", path, err)
	return nil, false
}

func testFileExists(t *testing.T, path string) bool {
	t.Helper()
	_, exists := readOptionalTestFile(t, path)
	return exists
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
    image: islandora/blazegraph:main
  cantaloupe:
    image: islandora/cantaloupe
  drupal:
    build:
      context: ./drupal
    environment:
      DRUPAL_DEFAULT_CANTALOUPE_URL: http://localhost/cantaloupe/iiif/2
      DRUPAL_DEFAULT_FCREPO_HOST: fcrepo
      DRUPAL_DEFAULT_FCREPO_PORT: 8080
      DRUPAL_DEFAULT_FCREPO_URL: http://fcrepo.localhost/fcrepo/rest/
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
	writeTestFile(t, filepath.Join(projectDir, "drupal", "Dockerfile"), `ARG REPOSITORY
ARG TAG
FROM ${REPOSITORY}/drupal:${TAG}

ARG TARGETARCH

COPY --link rootfs /

RUN --mount=type=cache,id=custom-drupal-composer-${TARGETARCH},sharing=locked,target=/root/.composer/cache \
    composer install -d /var/www/drupal --no-interaction --no-progress --prefer-dist --no-dev --optimize-autoloader && \
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

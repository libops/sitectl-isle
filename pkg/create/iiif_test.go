package create

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyTripletLocalReplacesCantaloupe(t *testing.T) {
	t.Parallel()

	projectDir := writeIIIFProjectFixture(t)

	if err := Apply(Options{
		Path:         projectDir,
		Fcrepo:       FcrepoStateOn,
		Blazegraph:   FcrepoStateOn,
		IIIF:         IIIFTriplet,
		IIIFTopology: IIIFTopologyLocal,
	}); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	compose := readFileForIIIFTest(t, filepath.Join(projectDir, "docker-compose.yml"))
	assertContainsIIIF(t, compose, "\n  triplet:\n")
	assertContainsIIIF(t, compose, "ghcr.io/libops/triplet:v1.1.0@sha256:ebdd90375f515e863a57372940a61a3b071c3bfb3134c699e2b5d726949603c8")
	assertContainsIIIF(t, compose, "drupal-public-files:/public:ro")
	assertContainsIIIF(t, compose, "drupal-private-files:/private:ro")
	assertContainsIIIF(t, compose, "source: fcrepo-data")
	assertContainsIIIF(t, compose, "subpath: home/data/ocfl-root")
	assertContainsIIIF(t, compose, "depends_on:\n      fcrepo:\n        condition: service_healthy")
	assertOrderIIIF(t, compose,
		"condition: service_healthy",
		"TRIPLET_PUBLIC_BASE_URL",
		"drupal-private-files:/private:ro",
		"source: fcrepo-data",
		"./certs/rootCA.pem:/etc/ssl/certs/lehigh.pem:ro",
		"./conf/triplet/config.yaml:/etc/triplet/config.yaml:ro",
		"triplet-cache:/var/lib/triplet/cache:rw",
	)
	assertContainsIIIF(t, compose, "  solr-data: {}\n  triplet-cache: {}\n\nservices:")
	assertContainsIIIF(t, compose, `DRUPAL_DEFAULT_CANTALOUPE_URL: "http://localhost/iiif/3"`)
	if strings.Contains(compose, "\n  cantaloupe:\n") || strings.Contains(compose, "\n  cantaloupe-data:") || strings.Contains(compose, "IIIF_UPSTREAM_URL") {
		t.Fatalf("expected local triplet without cantaloupe or external upstream, got:\n%s", compose)
	}

	tripletTraefik := readFileForIIIFTest(t, filepath.Join(projectDir, "conf", "traefik", "triplet.yml"))
	assertContainsIIIF(t, tripletTraefik, "PathPrefix(`/iiif`)")
	assertContainsIIIF(t, tripletTraefik, "http://triplet:8080")
	assertMissingIIIF(t, filepath.Join(projectDir, "conf", "traefik", "cantaloupe.yml"))

	tripletConfig := readFileForIIIFTest(t, filepath.Join(projectDir, "conf", "triplet", "config.yaml"))
	assertContainsIIIF(t, tripletConfig, "allowed_origins:\n      - http://localhost")
	assertContainsIIIF(t, tripletConfig, "max_source_bytes: 500GiB")
	assertContainsIIIF(t, tripletConfig, "prefix: /_flysystem/fedora")
	assertContainsIIIF(t, tripletConfig, "root: /fcrepo")
	if strings.Contains(tripletConfig, "preserve.lehigh.edu") {
		t.Fatalf("expected domain templating instead of hard-coded hosts, got:\n%s", tripletConfig)
	}

	devCompose := readFileForIIIFTest(t, filepath.Join(projectDir, "docker-compose.dev.yml"))
	if strings.Contains(devCompose, "cantaloupe:") {
		t.Fatalf("expected dev cantaloupe override removed, got:\n%s", devCompose)
	}
	robots := readFileForIIIFTest(t, filepath.Join(projectDir, DefaultDrupalRootfs, "web", "robots.txt"))
	assertContainsIIIF(t, robots, "Disallow: /iiif/*")
}

func TestApplyTripletDistributedUsesExternalUpstreamAndLocalOverride(t *testing.T) {
	t.Parallel()

	projectDir := writeIIIFProjectFixture(t)

	if err := Apply(Options{
		Path:            projectDir,
		Fcrepo:          FcrepoStateOn,
		Blazegraph:      FcrepoStateOn,
		IIIF:            IIIFTriplet,
		IIIFTopology:    IIIFTopologyExternal,
		IIIFUpstreamURL: "https://iiif.example.org",
		ComposeOverride: filepath.Join(projectDir, "docker-compose.local.yml"),
	}); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	compose := readFileForIIIFTest(t, filepath.Join(projectDir, "docker-compose.yml"))
	if strings.Contains(compose, "\n  triplet:\n") || strings.Contains(compose, "\n  cantaloupe:\n") {
		t.Fatalf("expected distributed iiif without base iiif service, got:\n%s", compose)
	}
	assertContainsIIIF(t, compose, `DRUPAL_DEFAULT_CANTALOUPE_URL: "https://iiif.example.org"`)
	assertContainsIIIF(t, compose, `IIIF_UPSTREAM_URL: "https://iiif.example.org"`)

	override := readFileForIIIFTest(t, filepath.Join(projectDir, "docker-compose.local.yml"))
	assertContainsIIIF(t, override, "\n  triplet:\n")
	assertContainsIIIF(t, override, "8080:8080")
	assertContainsIIIF(t, override, `IIIF_UPSTREAM_URL: "http://triplet:8080"`)

	devCompose := readFileForIIIFTest(t, filepath.Join(projectDir, "docker-compose.dev.yml"))
	assertContainsIIIF(t, devCompose, "\n  triplet:\n")
	assertContainsIIIF(t, devCompose, "8080:8080")
	assertContainsIIIF(t, devCompose, `IIIF_UPSTREAM_URL: "http://triplet:8080"`)
	assertContainsIIIF(t, devCompose, `DRUPAL_DEFAULT_CANTALOUPE_URL: "http://localhost/iiif/3"`)

	tripletTraefik := readFileForIIIFTest(t, filepath.Join(projectDir, "conf", "traefik", "triplet.yml"))
	assertContainsIIIF(t, tripletTraefik, `{{ env "IIIF_UPSTREAM_URL" }}`)
}

func TestApplyTripletLocalWithoutFcrepoOmitsFedoraDependencyAndMount(t *testing.T) {
	t.Parallel()

	projectDir := writeIIIFProjectFixture(t)

	if err := Apply(Options{
		Path:              projectDir,
		Fcrepo:            FcrepoStateOff,
		Blazegraph:        FcrepoStateOn,
		IIIF:              IIIFTriplet,
		IIIFTopology:      IIIFTopologyLocal,
		ISLEFileSystemURI: PrivateISLEFileSystemURI,
	}); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	compose := readFileForIIIFTest(t, filepath.Join(projectDir, "docker-compose.yml"))
	assertContainsIIIF(t, compose, "\n  triplet:\n")
	if strings.Contains(compose, "source: fcrepo-data") || strings.Contains(compose, "subpath: home/data/ocfl-root") || strings.Contains(compose, "condition: service_healthy") {
		t.Fatalf("expected triplet without Fedora dependency or mount after fcrepo off, got:\n%s", compose)
	}

	tripletConfig := readFileForIIIFTest(t, filepath.Join(projectDir, "conf", "triplet", "config.yaml"))
	if strings.Contains(tripletConfig, "/_flysystem/fedora") || strings.Contains(tripletConfig, "root: /fcrepo") {
		t.Fatalf("expected triplet config without Fedora source after fcrepo off, got:\n%s", tripletConfig)
	}
}

func TestApplyCantaloupeLocalRestoresFromTriplet(t *testing.T) {
	t.Parallel()

	projectDir := writeIIIFProjectFixture(t)
	if err := Apply(Options{
		Path:         projectDir,
		Fcrepo:       FcrepoStateOn,
		Blazegraph:   FcrepoStateOn,
		IIIF:         IIIFTriplet,
		IIIFTopology: IIIFTopologyLocal,
	}); err != nil {
		t.Fatalf("Apply(triplet) error = %v", err)
	}

	if err := Apply(Options{
		Path:         projectDir,
		Fcrepo:       FcrepoStateOn,
		Blazegraph:   FcrepoStateOn,
		IIIF:         IIIFCantaloupe,
		IIIFTopology: IIIFTopologyLocal,
	}); err != nil {
		t.Fatalf("Apply(cantaloupe) error = %v", err)
	}

	compose := readFileForIIIFTest(t, filepath.Join(projectDir, "docker-compose.yml"))
	assertContainsIIIF(t, compose, "\n  cantaloupe:\n")
	assertContainsIIIF(t, compose, "\n  cantaloupe-data: {}")
	assertContainsIIIF(t, compose, `DRUPAL_DEFAULT_CANTALOUPE_URL: "http://localhost/cantaloupe/iiif/2"`)
	if strings.Contains(compose, "\n  triplet:\n") || strings.Contains(compose, "\n  triplet-cache:") {
		t.Fatalf("expected triplet removed, got:\n%s", compose)
	}
	assertMissingIIIF(t, filepath.Join(projectDir, "conf", "traefik", "triplet.yml"))
	assertMissingIIIF(t, filepath.Join(projectDir, "conf", "triplet", "config.yaml"))
	cantaloupeTraefik := readFileForIIIFTest(t, filepath.Join(projectDir, "conf", "traefik", "cantaloupe.yml"))
	assertContainsIIIF(t, cantaloupeTraefik, "http://cantaloupe:8182")
	robots := readFileForIIIFTest(t, filepath.Join(projectDir, DefaultDrupalRootfs, "web", "robots.txt"))
	if strings.Contains(robots, "Disallow: /iiif/*") {
		t.Fatalf("expected /iiif robots rule removed, got:\n%s", robots)
	}
}

func TestApplyCantaloupeDistributedUsesExternalUpstreamAndLocalOverride(t *testing.T) {
	t.Parallel()

	projectDir := writeIIIFProjectFixture(t)

	if err := Apply(Options{
		Path:            projectDir,
		Fcrepo:          FcrepoStateOn,
		Blazegraph:      FcrepoStateOn,
		IIIF:            IIIFCantaloupe,
		IIIFTopology:    IIIFTopologyExternal,
		IIIFUpstreamURL: "https://cantaloupe.example.org",
		ComposeOverride: filepath.Join(projectDir, "docker-compose.local.yml"),
	}); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	compose := readFileForIIIFTest(t, filepath.Join(projectDir, "docker-compose.yml"))
	if strings.Contains(compose, "\n  cantaloupe:\n") || strings.Contains(compose, "\n  triplet:\n") {
		t.Fatalf("expected distributed cantaloupe without base iiif service, got:\n%s", compose)
	}
	assertContainsIIIF(t, compose, `DRUPAL_DEFAULT_CANTALOUPE_URL: "https://cantaloupe.example.org"`)
	assertContainsIIIF(t, compose, `IIIF_UPSTREAM_URL: "https://cantaloupe.example.org"`)
	if strings.Contains(compose, "CANTALOUPE_UPSTREAM_URL") {
		t.Fatalf("expected legacy cantaloupe upstream env removed, got:\n%s", compose)
	}

	override := readFileForIIIFTest(t, filepath.Join(projectDir, "docker-compose.local.yml"))
	assertContainsIIIF(t, override, "\n  cantaloupe:\n")
	assertContainsIIIF(t, override, "8182:8182")
	assertContainsIIIF(t, override, `IIIF_UPSTREAM_URL: "http://cantaloupe:8182"`)

	devCompose := readFileForIIIFTest(t, filepath.Join(projectDir, "docker-compose.dev.yml"))
	assertContainsIIIF(t, devCompose, "\n  cantaloupe:\n")
	assertContainsIIIF(t, devCompose, "8182:8182")
	assertContainsIIIF(t, devCompose, `IIIF_UPSTREAM_URL: "http://cantaloupe:8182"`)
	assertContainsIIIF(t, devCompose, `DRUPAL_DEFAULT_CANTALOUPE_URL: "http://localhost/cantaloupe/iiif/2"`)

	cantaloupeTraefik := readFileForIIIFTest(t, filepath.Join(projectDir, "conf", "traefik", "cantaloupe.yml"))
	assertContainsIIIF(t, cantaloupeTraefik, `{{ env "IIIF_UPSTREAM_URL" }}`)
}

func writeIIIFProjectFixture(t *testing.T) string {
	t.Helper()

	projectDir := t.TempDir()
	writeIIIFTestFile(t, filepath.Join(projectDir, "docker-compose.yml"), `---
x-common: &common
  restart: unless-stopped
  networks:
    default:
networks:
  default:
volumes:
  cantaloupe-data: {}
  drupal-private-files: {}
  drupal-public-files: {}
  fcrepo-data: {}
  mariadb-data: {}
  solr-data: {}

services:
  alpaca:
    <<: *common
    environment:
      ALPACA_FCREPO_INDEXER_ENABLED: "false"
      ALPACA_TRIPLESTORE_INDEXER_ENABLED: "false"
  drupal:
    <<: *common
    environment:
      DRUPAL_DEFAULT_CANTALOUPE_URL: http://localhost/cantaloupe/iiif/2
      DRUPAL_DEFAULT_TRIPLESTORE_NAMESPACE: ""
  fcrepo:
    <<: *common
    image: libops/fcrepo@sha256:611b9b15bf205c369aa664d119126429785da28d255635d8aeeb29ddf4ce03f0
    volumes:
      - fcrepo-data:/data:rw
  cantaloupe:
    <<: *common
    image: islandora/cantaloupe:main@sha256:82ac2324593018e5a5a98b44f4508a2ec0b1cda5c0e50be53e695205480a0ee2
    volumes:
      - cantaloupe-data:/data:rw
  traefik:
    <<: *common
    environment:
      CANTALOUPE_UPSTREAM_URL: http://cantaloupe:8182
`)
	writeIIIFTestFile(t, filepath.Join(projectDir, "docker-compose.dev.yml"), `services:
  cantaloupe:
    volumes:
      - cantaloupe-data:/data:Z,rw
      - ./tmp/cantaloupe:/tmp:rw
`)
	writeIIIFTestFile(t, filepath.Join(projectDir, "conf", "traefik", "cantaloupe.yml"), `http:
  services:
    cantaloupe:
      loadBalancer:
        servers:
          - url: http://cantaloupe:8182
`)
	writeIIIFTestFile(t, filepath.Join(projectDir, DefaultDrupalRootfs, "web", "robots.txt"), `User-agent: *
Disallow: /cantaloupe/*
`)
	if err := os.MkdirAll(filepath.Join(projectDir, DefaultDrupalRootfs, "config", "sync"), 0o755); err != nil {
		t.Fatalf("MkdirAll(config sync) error = %v", err)
	}
	return projectDir
}

func writeIIIFTestFile(t *testing.T, path, contents string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s) error = %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", path, err)
	}
}

func readFileForIIIFTest(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", path, err)
	}
	return string(data)
}

func assertContainsIIIF(t *testing.T, text, expected string) {
	t.Helper()

	if !strings.Contains(text, expected) {
		t.Fatalf("expected %q in:\n%s", expected, text)
	}
}

func assertOrderIIIF(t *testing.T, text string, values ...string) {
	t.Helper()

	last := -1
	for _, value := range values {
		index := strings.Index(text, value)
		if index == -1 {
			t.Fatalf("expected %q in:\n%s", value, text)
		}
		if index <= last {
			t.Fatalf("expected %q after previous value in:\n%s", value, text)
		}
		last = index
	}
}

func assertMissingIIIF(t *testing.T, path string) {
	t.Helper()

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected %s to be absent, stat err = %v", path, err)
	}
}

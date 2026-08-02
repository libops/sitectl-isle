package create

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	corecomponent "github.com/libops/sitectl/pkg/component"
)

func TestMergePDFFeatureBundleConvergesComposeAndDrupal(t *testing.T) {
	t.Parallel()
	projectDir := t.TempDir()
	writeFeatureTestFile(t, filepath.Join(projectDir, "compose.yaml"), `x-common: &common
  restart: unless-stopped
secrets:
  CERT_PUBLIC_KEY: {file: ./certs/cert.pem}
  CERT_AUTHORITY: {file: ./certs/rootCA.pem}
  JWT_ADMIN_TOKEN: {file: ./secrets/JWT_ADMIN_TOKEN}
  JWT_PUBLIC_KEY: {file: ./secrets/JWT_PUBLIC_KEY}
services:
  alpaca:
    image: islandora/alpaca:6.3.19
`)

	opts := Options{
		Path:         projectDir,
		DrupalRootfs: ".",
		FeatureBundles: map[string]string{
			FeatureBundleMergePDF: "on",
		},
	}
	if err := ApplyFeatureBundles(opts); err != nil {
		t.Fatalf("ApplyFeatureBundles(on) error = %v", err)
	}

	composePath := filepath.Join(projectDir, "compose.yaml")
	actionPath := filepath.Join(projectDir, "config", "sync", "system.action.paged_content_created_aggregated_pdf.yml")
	compose := readFeatureTestFile(t, composePath)
	for _, want := range []string{
		"  mergepdf:\n",
		"    <<: *common\n",
		"    image: islandora/mergepdf:6.3.19\n",
		"      - source: JWT_PUBLIC_KEY\n",
	} {
		if !strings.Contains(compose, want) {
			t.Fatalf("compose missing %q:\n%s", want, compose)
		}
	}
	if action := readFeatureTestFile(t, actionPath); !strings.Contains(action, "id: paged_content_created_aggregated_pdf") || !strings.Contains(action, "queue: islandora-connector-mergepdf") {
		t.Fatalf("unexpected mergepdf action:\n%s", action)
	}

	firstCompose := compose
	firstAction := readFeatureTestFile(t, actionPath)
	if err := ApplyFeatureBundles(opts); err != nil {
		t.Fatalf("ApplyFeatureBundles(second on) error = %v", err)
	}
	if got := readFeatureTestFile(t, composePath); got != firstCompose {
		t.Fatalf("second apply changed compose:\n%s", got)
	}
	if got := readFeatureTestFile(t, actionPath); got != firstAction {
		t.Fatalf("second apply changed action:\n%s", got)
	}

	opts.FeatureBundles[FeatureBundleMergePDF] = "off"
	if err := ApplyFeatureBundles(opts); err != nil {
		t.Fatalf("ApplyFeatureBundles(off) error = %v", err)
	}
	if strings.Contains(readFeatureTestFile(t, composePath), "\n  mergepdf:\n") {
		t.Fatal("mergepdf service remained after disable")
	}
	if _, err := os.Stat(actionPath); !os.IsNotExist(err) {
		t.Fatalf("mergepdf action still exists after disable: %v", err)
	}
}

func TestMergePDFFeatureBundleConvergesToHardcodedImage(t *testing.T) {
	t.Parallel()
	projectDir := t.TempDir()
	writeFeatureTestFile(t, filepath.Join(projectDir, "compose.yaml"), `x-common: &common
  restart: unless-stopped
secrets:
  CERT_PUBLIC_KEY: {}
  CERT_AUTHORITY: {}
  JWT_ADMIN_TOKEN: {}
  JWT_PUBLIC_KEY: {}
services:
  alpaca:
    image: libops/alpaca:2.4
  mergepdf:
    image: libops/mergepdf:main
    environment:
      DOWNSTREAM_DRIFT: present
`)
	opts := Options{Path: projectDir, DrupalRootfs: ".", FeatureBundles: map[string]string{FeatureBundleMergePDF: "on"}}
	if err := ApplyFeatureBundles(opts); err != nil {
		t.Fatalf("ApplyFeatureBundles(on) error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(projectDir, ".env")); !os.IsNotExist(err) {
		t.Fatalf("apply unexpectedly wrote a Compose environment file: %v", err)
	}
	if compose := readFeatureTestFile(t, filepath.Join(projectDir, "compose.yaml")); !strings.Contains(compose, "image: islandora/mergepdf:6.3.19") || strings.Contains(compose, "libops/mergepdf") || strings.Contains(compose, "DOWNSTREAM_DRIFT") {
		t.Fatalf("expected drifted service to converge to the exact upstream mergepdf contract:\n%s", compose)
	}

	opts.FeatureBundles[FeatureBundleMergePDF] = "off"
	if err := ApplyFeatureBundles(opts); err != nil {
		t.Fatalf("ApplyFeatureBundles(off) error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(projectDir, ".env")); !os.IsNotExist(err) {
		t.Fatalf("disable unexpectedly wrote a Compose environment file: %v", err)
	}
}

func TestMergePDFFeatureBundlePreservesCompatibleBumpedImage(t *testing.T) {
	t.Parallel()
	projectDir := t.TempDir()
	writeFeatureTestFile(t, filepath.Join(projectDir, "compose.yaml"), `x-common: &common
  restart: unless-stopped
secrets:
  CERT_PUBLIC_KEY: {}
  CERT_AUTHORITY: {}
  JWT_ADMIN_TOKEN: {}
  JWT_PUBLIC_KEY: {}
services:
  alpaca:
    image: libops/alpaca:2.4
  mergepdf:
    image: islandora/mergepdf:6.3.20@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
    environment:
      DOWNSTREAM_DRIFT: present
`)
	opts := Options{Path: projectDir, DrupalRootfs: ".", FeatureBundles: map[string]string{FeatureBundleMergePDF: "on"}}
	if err := ApplyFeatureBundles(opts); err != nil {
		t.Fatalf("ApplyFeatureBundles(on) error = %v", err)
	}
	compose := readFeatureTestFile(t, filepath.Join(projectDir, "compose.yaml"))
	if !strings.Contains(compose, "image: islandora/mergepdf:6.3.20@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa") {
		t.Fatalf("compatible bumped image was not preserved:\n%s", compose)
	}
	if strings.Contains(compose, "DOWNSTREAM_DRIFT") {
		t.Fatalf("non-image service drift was not converged:\n%s", compose)
	}

	first := compose
	if err := ApplyFeatureBundles(opts); err != nil {
		t.Fatalf("ApplyFeatureBundles(second on) error = %v", err)
	}
	if got := readFeatureTestFile(t, filepath.Join(projectDir, "compose.yaml")); got != first {
		t.Fatalf("second apply changed compatible bumped image service:\n%s", got)
	}
}

func TestValidateMergePDFObservedStateRequiresCompatibleDirectImage(t *testing.T) {
	t.Parallel()
	projectDir := t.TempDir()
	composePath := filepath.Join(projectDir, "compose.yaml")
	writeFeatureTestFile(t, composePath, "services:\n  mergepdf:\n    image: islandora/mergepdf:6.3.20@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n")
	if err := ValidateFeatureBundleObservedState(Options{Path: projectDir}, FeatureBundleMergePDF, true); err != nil {
		t.Fatalf("ValidateFeatureBundleObservedState(compatible) error = %v", err)
	}

	writeFeatureTestFile(t, composePath, "services:\n  mergepdf:\n    image: islandora/mergepdf:${ISLANDORA_TAG}\n")
	err := ValidateFeatureBundleObservedState(Options{Path: projectDir}, FeatureBundleMergePDF, true)
	if err == nil || !strings.Contains(err.Error(), "explicit compatible") {
		t.Fatalf("ValidateFeatureBundleObservedState(variable image) error = %v", err)
	}
}

func TestMergePDFFeatureBundleRejectsOldIslandoraAlpacaBeforeMutation(t *testing.T) {
	t.Parallel()
	projectDir := t.TempDir()
	composePath := filepath.Join(projectDir, "compose.yaml")
	writeFeatureTestFile(t, composePath, `x-common: &common
  restart: unless-stopped
secrets:
  CERT_PUBLIC_KEY: {}
  CERT_AUTHORITY: {}
  JWT_ADMIN_TOKEN: {}
  JWT_PUBLIC_KEY: {}
services:
  alpaca:
    image: islandora/alpaca:6.3.18
`)
	original := readFeatureTestFile(t, composePath)
	err := ApplyFeatureBundles(Options{Path: projectDir, DrupalRootfs: ".", FeatureBundles: map[string]string{FeatureBundleMergePDF: "on"}})
	if err == nil || !strings.Contains(err.Error(), "6.3.19 or newer") {
		t.Fatalf("expected Alpaca minimum-version error, got %v", err)
	}
	if got := readFeatureTestFile(t, composePath); got != original {
		t.Fatalf("failed preflight mutated compose:\n%s", got)
	}
}

func TestMergePDFFeatureBundleAcceptsCompatibleDesiredImageOverride(t *testing.T) {
	t.Parallel()
	projectDir := t.TempDir()
	writeFeatureTestFile(t, filepath.Join(projectDir, "compose.yaml"), `x-common: &common
  restart: unless-stopped
secrets:
  CERT_PUBLIC_KEY: {}
  CERT_AUTHORITY: {}
  JWT_ADMIN_TOKEN: {}
  JWT_PUBLIC_KEY: {}
services:
  alpaca:
    image: islandora/alpaca:6.3.16
`)

	opts := Options{
		Path:           projectDir,
		DrupalRootfs:   ".",
		FeatureBundles: map[string]string{FeatureBundleMergePDF: "on"},
		ImageOverrides: map[string]string{"alpaca": "islandora/alpaca:6.3.19"},
	}
	if err := ApplyFeatureBundles(opts); err != nil {
		t.Fatalf("ApplyFeatureBundles() error = %v", err)
	}
}

func TestMergePDFFeatureBundleRequiresCommonAnchorBeforeMutation(t *testing.T) {
	t.Parallel()
	projectDir := t.TempDir()
	composePath := filepath.Join(projectDir, "compose.yaml")
	writeFeatureTestFile(t, composePath, `x-common:
  restart: unless-stopped
secrets:
  CERT_PUBLIC_KEY: {}
  CERT_AUTHORITY: {}
  JWT_ADMIN_TOKEN: {}
  JWT_PUBLIC_KEY: {}
services:
  alpaca:
    image: islandora/alpaca:6.3.19
`)
	original := readFeatureTestFile(t, composePath)
	err := ApplyFeatureBundles(Options{Path: projectDir, DrupalRootfs: ".", FeatureBundles: map[string]string{FeatureBundleMergePDF: "on"}})
	if err == nil || !strings.Contains(err.Error(), `YAML anchor &common`) {
		t.Fatalf("expected common-anchor error, got %v", err)
	}
	if got := readFeatureTestFile(t, composePath); got != original {
		t.Fatalf("failed preflight mutated compose:\n%s", got)
	}
}

func TestHOCRSearchFeatureBundlePreflightsHostConfigBeforeMutation(t *testing.T) {
	t.Parallel()
	projectDir := t.TempDir()
	composePath := filepath.Join(projectDir, "compose.yaml")
	composerPath := filepath.Join(projectDir, "composer.json")
	writeFeatureTestFile(t, composePath, "services:\n  solr:\n    image: islandora/solr:4.2.1\n")
	writeFeatureTestFile(t, composerPath, "{\"require\": {\"php\": \"^8.3\"}}\n")

	originalCompose := readFeatureTestFile(t, composePath)
	originalComposer := readFeatureTestFile(t, composerPath)
	err := ApplyFeatureBundles(Options{Path: projectDir, DrupalRootfs: ".", FeatureBundles: map[string]string{FeatureBundleHOCRSearch: "on"}})
	if err == nil || !strings.Contains(err.Error(), "read Drupal config context.context.pages.yml") {
		t.Fatalf("expected missing host-config error, got %v", err)
	}
	if got := readFeatureTestFile(t, composePath); got != originalCompose {
		t.Fatalf("failed preflight mutated compose:\n%s", got)
	}
	if got := readFeatureTestFile(t, composerPath); got != originalComposer {
		t.Fatalf("failed preflight mutated composer.json:\n%s", got)
	}
	if _, err := os.Stat(filepath.Join(projectDir, "config", "sync", "views.view.search_in_hocr.yml")); !os.IsNotExist(err) {
		t.Fatalf("failed preflight created an hOCR asset: %v", err)
	}
}

func TestHOCRSearchFeatureBundleConvergesOwnedFilesAndComposerRequirements(t *testing.T) {
	t.Parallel()
	projectDir := t.TempDir()
	writeFeatureTestFile(t, filepath.Join(projectDir, ".env"), "SOLR_TAG=4.2.1\n")
	writeFeatureTestFile(t, filepath.Join(projectDir, "compose.yaml"), `services:
  solr:
    image: islandora/solr:${SOLR_TAG}
`)
	writeFeatureTestFile(t, filepath.Join(projectDir, "composer.json"), `{
    "name": "example/isle",
    "require": {
        "php": "^8.3"
    },
    "extra": {
        "downstream-owned": true
    }
}
`)
	configDir := filepath.Join(projectDir, "config", "sync")
	writeFeatureTestFile(t, filepath.Join(configDir, "context.context.pages.yml"), "reactions:\n  derivative:\n    actions:\n      get_ocr_from_image: get_ocr_from_image\n")
	writeFeatureTestFile(t, filepath.Join(configDir, "core.extension.yml"), "module:\n  islandora: 0\n")
	writeFeatureTestFile(t, filepath.Join(configDir, "field.field.media.file.field_media_file.yml"), "settings:\n  file_extensions: 'txt pdf'\n")
	writeFeatureTestFile(t, filepath.Join(configDir, "search_api.index.default_solr_index.yml"), "dependencies:\n  module:\n    - node\nfield_settings: {}\nprocessor_settings: {}\n")
	writeFeatureTestFile(t, filepath.Join(configDir, "views.view.iiif_manifest.yml"), `display:
  rest_export_1:
    display_options:
      style:
        options:
          iiif_tile_field:
            field_media_file: field_media_file
  rest_export_2:
    display_options:
      style:
        options:
          iiif_tile_field:
            field_media_file: field_media_file
  rest_export_3:
    display_options:
      style:
        options:
          iiif_tile_field:
            field_media_image: field_media_image
`)

	opts := Options{
		Path:         projectDir,
		DrupalRootfs: ".",
		FeatureBundles: map[string]string{
			FeatureBundleHOCRSearch: "on",
		},
		FeatureBundleOptions: map[string]map[string]string{
			FeatureBundleHOCRSearch: {HOCRStructuredTextTermOption: "99"},
		},
	}
	opts.FeatureBundleOptions[FeatureBundleHOCRSearch][HOCRStructuredTextTermOption] = "not-a-term-id"
	err := ApplyFeatureBundles(opts)
	if err == nil || !strings.Contains(err.Error(), "must be a positive integer") {
		t.Fatalf("expected hOCR term-ID validation error, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(configDir, "views.view.search_in_hocr.yml")); !os.IsNotExist(err) {
		t.Fatalf("invalid hOCR term ID created an asset: %v", err)
	}
	if _, ok := readComposerRequireForTest(t, filepath.Join(projectDir, "composer.json"))["discoverygarden/islandora_hocr"]; ok {
		t.Fatal("invalid hOCR term ID mutated composer.json")
	}
	opts.FeatureBundleOptions[FeatureBundleHOCRSearch][HOCRStructuredTextTermOption] = "99"
	if err := ApplyFeatureBundles(opts); err != nil {
		t.Fatalf("ApplyFeatureBundles(on) error = %v", err)
	}

	composer := readComposerRequireForTest(t, filepath.Join(projectDir, "composer.json"))
	if composer["born-digital/islandora_iiif_hocr"] != "^2.0" || composer["discoverygarden/islandora_hocr"] != "^1.4" || composer["php"] != "^8.3" {
		t.Fatalf("unexpected Composer requirements: %#v", composer)
	}
	if !strings.Contains(readFeatureTestFile(t, filepath.Join(projectDir, "composer.json")), `"downstream-owned": true`) {
		t.Fatal("Composer update removed downstream-owned root data")
	}

	for _, name := range []string{
		"search_api_solr.solr_field_type.islandora_hocr_und_7_0_0.yml",
		"search_api_solr.solr_request_handler.request_handler_select_islandora_hocr_7_0_0.yml",
		"system.action.get_hocr_from_image.yml",
		"views.view.search_in_hocr.yml",
	} {
		if _, err := os.Stat(filepath.Join(configDir, name)); err != nil {
			t.Fatalf("expected hOCR asset %s: %v", name, err)
		}
	}
	assertFeatureYAMLValue(t, filepath.Join(configDir, "context.context.pages.yml"), ".reactions.derivative.actions.get_hocr_from_image", "get_hocr_from_image")
	assertFeatureYAMLValue(t, filepath.Join(configDir, "core.extension.yml"), ".module.islandora_hocr", 0)
	assertFeatureYAMLValue(t, filepath.Join(configDir, "search_api.index.default_solr_index.yml"), ".field_settings.content.property_path", "islandora_hocr_field:content")
	assertFeatureYAMLValue(t, filepath.Join(configDir, "views.view.iiif_manifest.yml"), ".display.rest_export_1.display_options.style.options.structured_text_term", "99")
	field := readFeatureTestFile(t, filepath.Join(configDir, "field.field.media.file.field_media_file.yml"))
	if !strings.Contains(field, "txt pdf htm html") {
		t.Fatalf("hOCR file extensions were not appended once:\n%s", field)
	}

	first := featureBundleSnapshot(t, projectDir)
	if err := ApplyFeatureBundles(opts); err != nil {
		t.Fatalf("ApplyFeatureBundles(second on) error = %v", err)
	}
	if second := featureBundleSnapshot(t, projectDir); second != first {
		t.Fatal("hOCR bundle was not idempotent")
	}

	opts.FeatureBundles[FeatureBundleHOCRSearch] = "off"
	manifestPath := filepath.Join(configDir, "views.view.iiif_manifest.yml")
	document, err := corecomponent.LoadYAMLDocument([]byte(readFeatureTestFile(t, manifestPath)))
	if err != nil {
		t.Fatalf("LoadYAMLDocument() error = %v", err)
	}
	if err := document.SetValue(".display.rest_export_1.display_options.style.options.iiif_tile_field.field_media_file", "downstream-field"); err != nil {
		t.Fatalf("SetValue() error = %v", err)
	}
	downstreamManifest, err := document.Bytes()
	if err != nil {
		t.Fatalf("YAMLDocument.Bytes() error = %v", err)
	}
	writeFeatureTestFile(t, manifestPath, string(downstreamManifest))
	if err := ApplyFeatureBundles(opts); err == nil || !strings.Contains(err.Error(), "downstream value") {
		t.Fatalf("expected disable to protect the downstream tile-field value, got %v", err)
	}
	assertFeatureYAMLValue(t, manifestPath, ".display.rest_export_1.display_options.style.options.iiif_tile_field.field_media_file", "downstream-field")
	document, err = corecomponent.LoadYAMLDocument(downstreamManifest)
	if err != nil {
		t.Fatalf("LoadYAMLDocument() error = %v", err)
	}
	if err := document.SetValue(".display.rest_export_1.display_options.style.options.iiif_tile_field.field_media_file", "0"); err != nil {
		t.Fatalf("SetValue() error = %v", err)
	}
	featureManifest, err := document.Bytes()
	if err != nil {
		t.Fatalf("YAMLDocument.Bytes() error = %v", err)
	}
	writeFeatureTestFile(t, manifestPath, string(featureManifest))
	if err := ApplyFeatureBundles(opts); err != nil {
		t.Fatalf("ApplyFeatureBundles(off) error = %v", err)
	}
	composer = readComposerRequireForTest(t, filepath.Join(projectDir, "composer.json"))
	if _, ok := composer["born-digital/islandora_iiif_hocr"]; ok {
		t.Fatalf("hOCR Composer requirement remained after disable: %#v", composer)
	}
	if _, ok := composer["discoverygarden/islandora_hocr"]; ok {
		t.Fatalf("hOCR Composer requirement remained after disable: %#v", composer)
	}
	field = readFeatureTestFile(t, filepath.Join(configDir, "field.field.media.file.field_media_file.yml"))
	if strings.Contains(field, "htm") || strings.Contains(field, "html") {
		t.Fatalf("hOCR file extensions remained after disable:\n%s", field)
	}
	if value, ok := featureYAMLValue([]byte(readFeatureTestFile(t, filepath.Join(configDir, "core.extension.yml"))), ".module.islandora_hocr"); ok {
		t.Fatalf("hOCR module key remained after disable: %v", value)
	}
	assertFeatureYAMLValue(t, filepath.Join(configDir, "views.view.iiif_manifest.yml"), ".display.rest_export_1.display_options.style.options.iiif_tile_field.field_media_file", "field_media_file")
	assertFeatureYAMLValue(t, filepath.Join(configDir, "views.view.iiif_manifest.yml"), ".display.rest_export_2.display_options.style.options.iiif_tile_field.field_media_file", "field_media_file")
	assertFeatureYAMLValue(t, filepath.Join(configDir, "views.view.iiif_manifest.yml"), ".display.rest_export_3.display_options.style.options.iiif_tile_field.field_media_image", "field_media_image")
}

func TestLoadFeatureComposeEnvironmentUsesTrackedTemplateDefaults(t *testing.T) {
	t.Parallel()
	projectDir := t.TempDir()
	writeFeatureTestFile(t, filepath.Join(projectDir, "sample.env"), "ISLANDORA_TAG=6.3.16\n")

	env, err := loadFeatureComposeEnvironment(projectDir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := env["ISLANDORA_TAG"]; got != "6.3.16" {
		t.Fatalf("ISLANDORA_TAG = %q, want tracked template default", got)
	}
}

func TestHOCRSearchFeatureBundleImageRequirements(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		image     string
		wantError string
	}{
		{name: "minimum", image: "islandora/solr:4.2.1"},
		{name: "newer", image: "islandora/solr:4.3.0"},
		{name: "libops compatible", image: "libops/solr:9"},
		{name: "old", image: "islandora/solr:4.2.0", wantError: "4.2.1 or newer"},
		{name: "unknown", image: "solr:9", wantError: "known compatible image"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			projectDir := t.TempDir()
			writeFeatureTestFile(t, filepath.Join(projectDir, "compose.yaml"), "services:\n  solr:\n    image: "+test.image+"\n")
			err := CheckFeatureBundleRequirements(Options{Path: projectDir}, FeatureBundleHOCRSearch)
			if test.wantError == "" && err != nil {
				t.Fatalf("CheckFeatureBundleRequirements() error = %v", err)
			}
			if test.wantError != "" && (err == nil || !strings.Contains(err.Error(), test.wantError)) {
				t.Fatalf("error = %v, want %q", err, test.wantError)
			}
		})
	}
}

func TestValidateHOCRSearchObservedStateUsesExactTokensAndMatchingTermIDs(t *testing.T) {
	t.Parallel()
	projectDir := t.TempDir()
	configDir := filepath.Join(projectDir, "config", "sync")
	extensionsPath := filepath.Join(configDir, "field.field.media.file.field_media_file.yml")
	manifestPath := filepath.Join(configDir, "views.view.iiif_manifest.yml")
	writeFeatureTestFile(t, extensionsPath, "settings:\n  file_extensions: 'txt pdf html'\n")
	writeFeatureTestFile(t, manifestPath, `display:
  rest_export_1:
    display_options:
      style:
        options:
          structured_text_term: '56'
  rest_export_2:
    display_options:
      style:
        options:
          structured_text_term: '56'
  rest_export_3:
    display_options:
      style:
        options:
          structured_text_term: '57'
`)
	opts := Options{Path: projectDir, DrupalRootfs: "."}
	if err := ValidateFeatureBundleObservedState(opts, FeatureBundleHOCRSearch, true); err == nil || !strings.Contains(err.Error(), `exact file extension token "htm"`) {
		t.Fatalf("expected exact htm-token error, got %v", err)
	}
	writeFeatureTestFile(t, extensionsPath, "settings:\n  file_extensions: 'txt pdf htm html'\n")
	if err := ValidateFeatureBundleObservedState(opts, FeatureBundleHOCRSearch, true); err == nil || !strings.Contains(err.Error(), "must match") {
		t.Fatalf("expected divergent term-ID error, got %v", err)
	}
	manifest := strings.Replace(readFeatureTestFile(t, manifestPath), "structured_text_term: '57'", "structured_text_term: '56'", 1)
	writeFeatureTestFile(t, manifestPath, manifest)
	if err := ValidateFeatureBundleObservedState(opts, FeatureBundleHOCRSearch, true); err != nil {
		t.Fatalf("ValidateFeatureBundleObservedState(enabled) error = %v", err)
	}
	writeFeatureTestFile(t, extensionsPath, "settings:\n  file_extensions: 'txt pdf xhtml'\n")
	if err := ValidateFeatureBundleObservedState(opts, FeatureBundleHOCRSearch, false); err != nil {
		t.Fatalf("substring-only disabled state should be valid: %v", err)
	}
	writeFeatureTestFile(t, extensionsPath, "settings:\n  file_extensions: 'txt pdf html'\n")
	if err := ValidateFeatureBundleObservedState(opts, FeatureBundleHOCRSearch, false); err == nil || !strings.Contains(err.Error(), `token "html"`) {
		t.Fatalf("expected remaining exact html-token error, got %v", err)
	}
}

func TestExpandFeatureComposeValueAllowsEmptySubstitutions(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name  string
		value string
		env   map[string]string
		want  string
	}{
		{name: "colon dash empty default", value: "${REGISTRY:-}islandora/solr:4.2.1", env: map[string]string{}, want: "islandora/solr:4.2.1"},
		{name: "dash empty default", value: "${REGISTRY-}islandora/solr:4.2.1", env: map[string]string{}, want: "islandora/solr:4.2.1"},
		{name: "present empty", value: "${REGISTRY}islandora/solr:4.2.1", env: map[string]string{"REGISTRY": ""}, want: "islandora/solr:4.2.1"},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := expandFeatureComposeValue(test.value, test.env)
			if err != nil {
				t.Fatalf("expandFeatureComposeValue() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("expandFeatureComposeValue() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestFeatureVersionAtLeast(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		actual  string
		minimum string
		want    bool
	}{
		{actual: "v6.3.19", minimum: "6.3.19", want: true},
		{actual: "6.3.20-alpine", minimum: "6.3.19", want: true},
		{actual: "6.3.18", minimum: "6.3.19", want: false},
		{actual: "4.3", minimum: "4.2.1", want: true},
		{actual: "6.3.19-rc1", minimum: "6.3.19", want: false},
		{actual: "4.2.1-beta.2", minimum: "4.2.1", want: false},
		{actual: "6.3.20-rc1", minimum: "6.3.19", want: true},
		{actual: "6.3.19", minimum: "6.3.19-rc1", want: true},
	} {
		got, err := featureVersionAtLeast(test.actual, test.minimum)
		if err != nil {
			t.Fatalf("featureVersionAtLeast(%q, %q) error = %v", test.actual, test.minimum, err)
		}
		if got != test.want {
			t.Fatalf("featureVersionAtLeast(%q, %q) = %v, want %v", test.actual, test.minimum, got, test.want)
		}
	}
}

func assertFeatureYAMLValue(t *testing.T, path, yamlPath string, want any) {
	t.Helper()
	value, ok := featureYAMLValue([]byte(readFeatureTestFile(t, path)), yamlPath)
	if !ok || fmt.Sprint(value) != fmt.Sprint(want) {
		t.Fatalf("%s %s = %v (present=%v), want %v", path, yamlPath, value, ok, want)
	}
}

func readComposerRequireForTest(t *testing.T, path string) map[string]string {
	t.Helper()
	var document struct {
		Require map[string]string `json:"require"`
	}
	if err := json.Unmarshal([]byte(readFeatureTestFile(t, path)), &document); err != nil {
		t.Fatalf("decode composer.json: %v", err)
	}
	return document.Require
}

func featureBundleSnapshot(t *testing.T, projectDir string) string {
	t.Helper()
	paths := []string{
		"composer.json",
		"config/sync/context.context.pages.yml",
		"config/sync/core.extension.yml",
		"config/sync/field.field.media.file.field_media_file.yml",
		"config/sync/search_api.index.default_solr_index.yml",
		"config/sync/views.view.iiif_manifest.yml",
		"config/sync/views.view.search_in_hocr.yml",
	}
	var snapshot strings.Builder
	for _, path := range paths {
		snapshot.WriteString(path)
		snapshot.WriteByte('\n')
		snapshot.WriteString(readFeatureTestFile(t, filepath.Join(projectDir, filepath.FromSlash(path))))
	}
	return snapshot.String()
}

func writeFeatureTestFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s) error = %v", path, err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", path, err)
	}
}

func readFeatureTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", path, err)
	}
	return string(data)
}

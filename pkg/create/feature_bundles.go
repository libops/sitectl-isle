package create

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

	corecomponent "github.com/libops/sitectl/pkg/component"
	"gopkg.in/yaml.v3"
)

const (
	FeatureBundleMergePDF   = "mergepdf"
	FeatureBundleHOCRSearch = "hocr-search"

	featureMutationSet         = "set"
	featureMutationAppend      = "append"
	featureMutationAppendToken = "append-token"

	HOCRStructuredTextTermOption = "structured-text-term"
	IslandoraTagOption           = "islandora-tag"
	defaultIslandoraTag          = "6.3.19"
)

var dockerTagPattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$`)

//go:embed assets/feature-bundles/*/*
var featureBundleAssets embed.FS

// FeatureYAMLMutation is one narrowly owned Drupal config mutation in a
// feature bundle. Set mutations are deleted when the bundle is disabled;
// appended sequence items and scalar tokens are removed individually.
type FeatureYAMLMutation struct {
	File         string
	Path         string
	Kind         string
	Value        any
	OptionName   string
	DefaultValue string
	PresenceOnly bool
	// RestoreOnDisable preserves a value that the upstream hOCR patch replaces
	// instead of adding. DisableValue is the pre-feature starter-site value.
	RestoreOnDisable bool
	DisableValue     any
}

// FeatureImageRequirement describes a Compose image compatibility boundary.
// The minimum applies when Repository is selected. CompatibleRepositories are
// independently maintained images known to provide the required capability.
type FeatureImageRequirement struct {
	Service                string
	Repository             string
	MinimumVersion         string
	CompatibleRepositories []string
	RejectUnknown          bool
}

// FeatureBundleSpec describes a reviewable feature that may own Compose,
// Drupal config, and Composer requirements together.
type FeatureBundleSpec struct {
	Name                 string
	DefaultState         corecomponent.State
	ComposeService       string
	ComposeAsset         string
	RequiredComposeKeys  []string
	RequiredSecrets      []string
	DrupalAssets         []string
	DrupalMutations      []FeatureYAMLMutation
	ComposerRequirements map[string]string
	ImageRequirements    []FeatureImageRequirement
}

var featureBundleSpecs = []FeatureBundleSpec{
	{
		Name:           FeatureBundleMergePDF,
		DefaultState:   corecomponent.StateOn,
		ComposeService: "mergepdf",
		ComposeAsset:   "service.yml",
		RequiredComposeKeys: []string{
			"x-common",
		},
		RequiredSecrets: []string{
			"CERT_PUBLIC_KEY",
			"CERT_AUTHORITY",
			"JWT_ADMIN_TOKEN",
			"JWT_PUBLIC_KEY",
		},
		DrupalAssets: []string{
			"system.action.paged_content_created_aggregated_pdf.yml",
		},
		ImageRequirements: []FeatureImageRequirement{
			{
				Service:        "alpaca",
				Repository:     "islandora/alpaca",
				MinimumVersion: "6.3.19",
			},
		},
	},
	{
		Name:         FeatureBundleHOCRSearch,
		DefaultState: corecomponent.StateOn,
		DrupalAssets: []string{
			"search_api_solr.solr_field_type.islandora_hocr_und_7_0_0.yml",
			"search_api_solr.solr_request_handler.request_handler_select_islandora_hocr_7_0_0.yml",
			"system.action.get_hocr_from_image.yml",
			"views.view.search_in_hocr.yml",
		},
		ComposerRequirements: map[string]string{
			"born-digital/islandora_iiif_hocr": "^2.0",
			"discoverygarden/islandora_hocr":   "^1.4",
		},
		ImageRequirements: []FeatureImageRequirement{
			{
				Service:                "solr",
				Repository:             "islandora/solr",
				MinimumVersion:         "4.2.1",
				CompatibleRepositories: []string{"libops/solr", "ghcr.io/libops/solr"},
				RejectUnknown:          true,
			},
		},
		DrupalMutations: []FeatureYAMLMutation{
			{File: "context.context.pages.yml", Path: ".reactions.derivative.actions.get_hocr_from_image", Kind: featureMutationSet, Value: "get_hocr_from_image"},
			{File: "core.extension.yml", Path: ".module.islandora_hocr", Kind: featureMutationSet, Value: 0},
			{File: "core.extension.yml", Path: ".module.islandora_iiif_hocr", Kind: featureMutationSet, Value: 0},
			{File: "field.field.media.file.field_media_file.yml", Path: ".settings.file_extensions", Kind: featureMutationAppendToken, Value: "htm"},
			{File: "field.field.media.file.field_media_file.yml", Path: ".settings.file_extensions", Kind: featureMutationAppendToken, Value: "html"},
			{File: "search_api.index.default_solr_index.yml", Path: ".dependencies.module", Kind: featureMutationAppend, Value: "islandora_hocr"},
			{File: "search_api.index.default_solr_index.yml", Path: ".field_settings.content", Kind: featureMutationSet, Value: map[string]any{
				"label":         "HOCR Field » HOCR Content Field",
				"datasource_id": "entity:node",
				"property_path": "islandora_hocr_field:content",
				"type":          "solr_text_custom:islandora_hocr",
			}},
			{File: "search_api.index.default_solr_index.yml", Path: ".processor_settings.islandora_hocr_field", Kind: featureMutationSet, Value: map[string]any{}},
			{File: "views.view.iiif_manifest.yml", Path: ".display.rest_export_1.display_options.style.options.iiif_tile_field.field_media_file", Kind: featureMutationSet, Value: "0", RestoreOnDisable: true, DisableValue: "field_media_file"},
			{File: "views.view.iiif_manifest.yml", Path: ".display.rest_export_1.display_options.style.options.iiif_ocr_file_field", Kind: featureMutationSet, Value: map[string]any{"field_media_file": "0", "field_media_image": "0"}},
			{File: "views.view.iiif_manifest.yml", Path: ".display.rest_export_1.display_options.style.options.structured_text_term_uri", Kind: featureMutationSet, Value: "https://discoverygarden.ca/use#hocr"},
			{File: "views.view.iiif_manifest.yml", Path: ".display.rest_export_1.display_options.style.options.search_endpoint", Kind: featureMutationSet, Value: "paged-content-search/%node"},
			{File: "views.view.iiif_manifest.yml", Path: ".display.rest_export_1.display_options.style.options.structured_text_term", Kind: featureMutationSet, OptionName: HOCRStructuredTextTermOption, DefaultValue: "56", PresenceOnly: true},
			{File: "views.view.iiif_manifest.yml", Path: ".display.rest_export_2.display_options.style.options.iiif_tile_field.field_media_file", Kind: featureMutationSet, Value: "0", RestoreOnDisable: true, DisableValue: "field_media_file"},
			{File: "views.view.iiif_manifest.yml", Path: ".display.rest_export_2.display_options.style.options.iiif_ocr_file_field", Kind: featureMutationSet, Value: map[string]any{"field_media_file": "0", "field_media_image": "0"}},
			{File: "views.view.iiif_manifest.yml", Path: ".display.rest_export_2.display_options.style.options.structured_text_term_uri", Kind: featureMutationSet, Value: "https://discoverygarden.ca/use#hocr"},
			{File: "views.view.iiif_manifest.yml", Path: ".display.rest_export_2.display_options.style.options.search_endpoint", Kind: featureMutationSet, Value: "single-page-content-search/%node"},
			{File: "views.view.iiif_manifest.yml", Path: ".display.rest_export_2.display_options.style.options.structured_text_term", Kind: featureMutationSet, OptionName: HOCRStructuredTextTermOption, DefaultValue: "56", PresenceOnly: true},
			{File: "views.view.iiif_manifest.yml", Path: ".display.rest_export_3.display_options.style.options.iiif_tile_field.field_media_image", Kind: featureMutationSet, Value: "0", RestoreOnDisable: true, DisableValue: "field_media_image"},
			{File: "views.view.iiif_manifest.yml", Path: ".display.rest_export_3.display_options.style.options.iiif_ocr_file_field", Kind: featureMutationSet, Value: map[string]any{"field_media_file": "0", "field_media_image": "0"}},
			{File: "views.view.iiif_manifest.yml", Path: ".display.rest_export_3.display_options.style.options.structured_text_term_uri", Kind: featureMutationSet, Value: "https://discoverygarden.ca/use#hocr"},
			{File: "views.view.iiif_manifest.yml", Path: ".display.rest_export_3.display_options.style.options.search_endpoint", Kind: featureMutationSet, Value: "paged-content-search/%node"},
			{File: "views.view.iiif_manifest.yml", Path: ".display.rest_export_3.display_options.style.options.structured_text_term", Kind: featureMutationSet, OptionName: HOCRStructuredTextTermOption, DefaultValue: "56", PresenceOnly: true},
		},
	},
}

// FeatureBundleSpecs returns a copy of the canonical bundle catalog.
func FeatureBundleSpecs() []FeatureBundleSpec {
	specs := make([]FeatureBundleSpec, len(featureBundleSpecs))
	copy(specs, featureBundleSpecs)
	return specs
}

// FeatureBundleNames returns feature names in catalog order.
func FeatureBundleNames() []string {
	names := make([]string, 0, len(featureBundleSpecs))
	for _, spec := range featureBundleSpecs {
		names = append(names, spec.Name)
	}
	return names
}

// IsFeatureBundle reports whether name is a known feature bundle.
func IsFeatureBundle(name string) bool {
	_, ok := FeatureBundleSpecByName(name)
	return ok
}

// FeatureBundleSpecByName returns the canonical bundle definition.
func FeatureBundleSpecByName(name string) (FeatureBundleSpec, bool) {
	for _, spec := range featureBundleSpecs {
		if spec.Name == name {
			return spec, true
		}
	}
	return FeatureBundleSpec{}, false
}

// ApplyFeatureBundles converges all explicitly requested bundles. Every
// target-state preflight runs before any project file is changed.
func ApplyFeatureBundles(opts Options) error {
	if opts.Path == "" {
		opts.Path = "."
	}
	if len(opts.FeatureBundles) == 0 {
		return nil
	}

	names := make([]string, 0, len(opts.FeatureBundles))
	for name, rawState := range opts.FeatureBundles {
		if !IsFeatureBundle(name) {
			return fmt.Errorf("unknown feature bundle %q", name)
		}
		state, err := corecomponent.ParseState(rawState)
		if err != nil {
			return fmt.Errorf("parse feature bundle %q: %w", name, err)
		}
		if state == corecomponent.StateOn {
			if err := CheckFeatureBundleProject(opts, name); err != nil {
				return err
			}
		} else {
			spec, _ := FeatureBundleSpecByName(name)
			if err := preflightFeatureBundleDisable(opts, spec); err != nil {
				return fmt.Errorf("preflight feature bundle %q disable: %w", name, err)
			}
		}
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		spec, _ := FeatureBundleSpecByName(name)
		state, _ := corecomponent.ParseState(opts.FeatureBundles[name])
		if err := applyFeatureBundle(opts, spec, state == corecomponent.StateOn); err != nil {
			return fmt.Errorf("apply feature bundle %q: %w", name, err)
		}
	}
	return nil
}

// CheckFeatureBundleProject verifies both Compose compatibility and the
// project files an enabled bundle will mutate. It never changes the project.
func CheckFeatureBundleProject(opts Options, name string) error {
	if opts.Path == "" {
		opts.Path = "."
	}
	if err := CheckFeatureBundleRequirements(opts, name); err != nil {
		return err
	}
	spec, _ := FeatureBundleSpecByName(name)
	if err := preflightFeatureBundleFiles(opts, spec); err != nil {
		return fmt.Errorf("preflight feature bundle %q: %w", name, err)
	}
	return nil
}

// CheckFeatureBundleRequirements verifies the current Compose project without
// mutating it.
func CheckFeatureBundleRequirements(opts Options, name string) error {
	spec, ok := FeatureBundleSpecByName(name)
	if !ok {
		return fmt.Errorf("unknown feature bundle %q", name)
	}
	composePath := filepath.Join(opts.Path, "docker-compose.yml")
	data, err := os.ReadFile(composePath) // #nosec G304 -- selected project compose path.
	if err != nil {
		return fmt.Errorf("read compose file for %s: %w", name, err)
	}
	var compose map[string]any
	if err := yaml.Unmarshal(data, &compose); err != nil {
		return fmt.Errorf("parse compose file for %s: %w", name, err)
	}
	for _, key := range spec.RequiredComposeKeys {
		if _, ok := compose[key]; !ok {
			return fmt.Errorf("feature bundle %q requires top-level Compose key %q", name, key)
		}
	}

	secrets := featureMap(compose["secrets"])
	for _, secret := range spec.RequiredSecrets {
		if _, ok := secrets[secret]; !ok {
			return fmt.Errorf("feature bundle %q requires top-level Compose secret %q", name, secret)
		}
	}
	if spec.Name == FeatureBundleMergePDF && !featureYAMLMappingHasAnchor(data, "x-common", "common") {
		return fmt.Errorf("feature bundle %q requires top-level Compose key %q to declare YAML anchor &common", name, "x-common")
	}

	env, err := loadFeatureComposeEnvironment(opts.Path, opts.EnvFiles)
	if err != nil {
		return fmt.Errorf("load Compose environment for %s: %w", name, err)
	}
	if name == FeatureBundleMergePDF {
		selected, err := resolvedFeatureIslandoraTag(opts)
		if err != nil {
			return fmt.Errorf("resolve Islandora image tag for %s: %w", name, err)
		}
		env["ISLANDORA_TAG"] = selected
	}
	services := featureMap(compose["services"])
	for _, requirement := range spec.ImageRequirements {
		service := featureMap(services[requirement.Service])
		if service == nil {
			return fmt.Errorf("feature bundle %q requires Compose service %q", name, requirement.Service)
		}
		rawImage := strings.TrimSpace(fmt.Sprint(service["image"]))
		if rawImage == "" || rawImage == "<nil>" {
			return fmt.Errorf("feature bundle %q requires an image on Compose service %q", name, requirement.Service)
		}
		image, err := expandFeatureComposeValue(rawImage, env)
		if err != nil {
			return fmt.Errorf("resolve %s image %q for feature bundle %q: %w", requirement.Service, rawImage, name, err)
		}
		repository, tag := splitFeatureImageReference(image)
		if featureRepositoryMatches(repository, requirement.Repository) {
			ok, err := featureVersionAtLeast(tag, requirement.MinimumVersion)
			if err != nil {
				return fmt.Errorf("feature bundle %q cannot verify %s image %q: %w", name, requirement.Service, image, err)
			}
			if !ok {
				return fmt.Errorf("feature bundle %q requires %s:%s or newer; found %s", name, requirement.Repository, requirement.MinimumVersion, image)
			}
			continue
		}
		if featureRepositoryIn(repository, requirement.CompatibleRepositories) {
			continue
		}
		if requirement.RejectUnknown {
			return fmt.Errorf("feature bundle %q requires %s:%s or a known compatible image; found %s", name, requirement.Repository, requirement.MinimumVersion, image)
		}
	}
	return nil
}

func preflightFeatureBundleFiles(opts Options, spec FeatureBundleSpec) error {
	if spec.ComposeAsset != "" {
		if _, err := fs.ReadFile(featureBundleAssets, filepath.ToSlash(filepath.Join("assets", "feature-bundles", spec.Name, spec.ComposeAsset))); err != nil {
			return fmt.Errorf("read Compose asset: %w", err)
		}
	}
	for _, name := range spec.DrupalAssets {
		if _, err := fs.ReadFile(featureBundleAssets, filepath.ToSlash(filepath.Join("assets", "feature-bundles", spec.Name, name))); err != nil {
			return fmt.Errorf("read Drupal feature asset %s: %w", name, err)
		}
	}

	layout := corecomponent.ResolveDrupalLayout(opts.Path, resolveProjectDrupalRootfs(opts.Path, opts.DrupalRootfs))
	seen := map[string]bool{}
	for _, mutation := range spec.DrupalMutations {
		if seen[mutation.File] {
			continue
		}
		seen[mutation.File] = true
		path := filepath.Join(layout.ConfigSyncDir(), filepath.FromSlash(mutation.File))
		data, err := os.ReadFile(path) // #nosec G304 -- selected project Drupal config path.
		if err != nil {
			return fmt.Errorf("read Drupal config %s: %w", mutation.File, err)
		}
		if _, err := corecomponent.LoadYAMLDocument(data); err != nil {
			return fmt.Errorf("parse Drupal config %s: %w", mutation.File, err)
		}
	}
	for _, mutation := range spec.DrupalMutations {
		if !mutation.RestoreOnDisable {
			continue
		}
		path := filepath.Join(layout.ConfigSyncDir(), filepath.FromSlash(mutation.File))
		data, err := os.ReadFile(path) // #nosec G304 -- selected project Drupal config path.
		if err != nil {
			return fmt.Errorf("read Drupal config %s: %w", mutation.File, err)
		}
		current, exists := featureYAMLValue(data, mutation.Path)
		if !exists {
			return fmt.Errorf("drupal config %s is missing required baseline path %s", mutation.File, mutation.Path)
		}
		desired := resolveFeatureMutationValue(mutation, opts.FeatureBundleOptions[spec.Name])
		if fmt.Sprint(current) != fmt.Sprint(desired) && fmt.Sprint(current) != fmt.Sprint(mutation.DisableValue) {
			return fmt.Errorf("drupal config %s path %s has downstream value %v; expected feature value %v or baseline %v", mutation.File, mutation.Path, current, desired, mutation.DisableValue)
		}
	}

	if len(spec.ComposerRequirements) > 0 {
		if err := validateFeatureComposerFile(layout.ComposerJSONPath()); err != nil {
			return err
		}
	}
	if spec.Name == FeatureBundleHOCRSearch {
		value := strings.TrimSpace(opts.FeatureBundleOptions[spec.Name][HOCRStructuredTextTermOption])
		if value == "" {
			value = "56"
		}
		termID, err := strconv.Atoi(value)
		if err != nil || termID <= 0 {
			return fmt.Errorf("hOCR media-use term ID must be a positive integer, got %q", value)
		}
	}
	if spec.Name == FeatureBundleMergePDF {
		tag, err := resolvedFeatureIslandoraTag(opts)
		if err != nil {
			return fmt.Errorf("resolve Islandora image tag: %w", err)
		}
		if !dockerTagPattern.MatchString(tag) {
			return fmt.Errorf("islandora image tag %q is not a valid Docker tag", tag)
		}
		compatible, err := featureVersionAtLeast(tag, defaultIslandoraTag)
		if err != nil {
			return fmt.Errorf("islandora image tag %q cannot be compared with %s: %w", tag, defaultIslandoraTag, err)
		}
		if !compatible {
			return fmt.Errorf("mergepdf requires Islandora image tag %s or newer, got %s", defaultIslandoraTag, tag)
		}
	}
	return nil
}

func preflightFeatureBundleDisable(opts Options, spec FeatureBundleSpec) error {
	layout := corecomponent.ResolveDrupalLayout(opts.Path, resolveProjectDrupalRootfs(opts.Path, opts.DrupalRootfs))
	seen := map[string]bool{}
	for _, mutation := range spec.DrupalMutations {
		if seen[mutation.File] {
			continue
		}
		seen[mutation.File] = true
		path := filepath.Join(layout.ConfigSyncDir(), filepath.FromSlash(mutation.File))
		data, err := os.ReadFile(path) // #nosec G304 -- selected project Drupal config path.
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("read Drupal config %s: %w", mutation.File, err)
		}
		if _, err := corecomponent.LoadYAMLDocument(data); err != nil {
			return fmt.Errorf("parse Drupal config %s: %w", mutation.File, err)
		}
	}
	for _, mutation := range spec.DrupalMutations {
		if !mutation.RestoreOnDisable {
			continue
		}
		path := filepath.Join(layout.ConfigSyncDir(), filepath.FromSlash(mutation.File))
		data, err := os.ReadFile(path) // #nosec G304 -- selected project Drupal config path.
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("read Drupal config %s: %w", mutation.File, err)
		}
		current, exists := featureYAMLValue(data, mutation.Path)
		if !exists {
			continue
		}
		desired := resolveFeatureMutationValue(mutation, opts.FeatureBundleOptions[spec.Name])
		if fmt.Sprint(current) != fmt.Sprint(desired) && fmt.Sprint(current) != fmt.Sprint(mutation.DisableValue) {
			return fmt.Errorf("drupal config %s path %s has downstream value %v; refusing to replace it with baseline %v", mutation.File, mutation.Path, current, mutation.DisableValue)
		}
	}
	if len(spec.ComposerRequirements) == 0 {
		return nil
	}
	path := layout.ComposerJSONPath()
	data, err := os.ReadFile(path) // #nosec G304 -- selected project composer path.
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read composer.json: %w", err)
	}
	if !json.Valid(data) {
		return fmt.Errorf("parse composer.json: invalid JSON")
	}
	start, end, found, err := topLevelJSONValueSpan(data, "require")
	if err != nil {
		return fmt.Errorf("parse composer.json: %w", err)
	}
	if !found {
		return nil
	}
	var require map[string]json.RawMessage
	if err := json.Unmarshal(data[start:end], &require); err != nil || require == nil {
		return fmt.Errorf("composer.json top-level require must be an object")
	}
	return nil
}

func validateFeatureComposerFile(path string) error {
	data, err := os.ReadFile(path) // #nosec G304 -- selected project composer path.
	if err != nil {
		return fmt.Errorf("read composer.json: %w", err)
	}
	if !json.Valid(data) {
		return fmt.Errorf("parse composer.json: invalid JSON")
	}
	start, end, found, err := topLevelJSONValueSpan(data, "require")
	if err != nil {
		return fmt.Errorf("parse composer.json: %w", err)
	}
	if !found {
		return fmt.Errorf("composer.json does not contain a top-level require object")
	}
	var require map[string]json.RawMessage
	if err := json.Unmarshal(data[start:end], &require); err != nil || require == nil {
		return fmt.Errorf("composer.json top-level require must be an object")
	}
	return nil
}

// FeatureBundleCurrentOptions reads bundle-specific values from an existing
// checkout so interactive review preserves downstream choices.
func FeatureBundleCurrentOptions(projectDir, drupalRootfs string, envFiles []string, name string) map[string]string {
	options := map[string]string{}
	if name == FeatureBundleMergePDF {
		env, err := loadFeatureComposeEnvironment(projectDir, envFiles)
		if err == nil && strings.TrimSpace(env["ISLANDORA_TAG"]) != "" {
			options[IslandoraTagOption] = strings.TrimSpace(env["ISLANDORA_TAG"])
		}
		return options
	}
	if name != FeatureBundleHOCRSearch {
		return options
	}
	layout := corecomponent.ResolveDrupalLayout(projectDir, resolveProjectDrupalRootfs(projectDir, drupalRootfs))
	path := filepath.Join(layout.ConfigSyncDir(), "views.view.iiif_manifest.yml")
	data, err := os.ReadFile(path) // #nosec G304 -- selected project Drupal config path.
	if err != nil {
		return options
	}
	value, ok := featureYAMLValue(data, ".display.rest_export_1.display_options.style.options.structured_text_term")
	if ok && strings.TrimSpace(fmt.Sprint(value)) != "" {
		options[HOCRStructuredTextTermOption] = strings.TrimSpace(fmt.Sprint(value))
	}
	return options
}

// ValidateFeatureBundleObservedState checks state details that the sitectl v1
// generic YAML rules cannot express exactly, such as whitespace-delimited
// scalar tokens and equality across several Drupal configuration paths.
func ValidateFeatureBundleObservedState(opts Options, name string, enabled bool) error {
	if name != FeatureBundleHOCRSearch {
		return nil
	}
	layout := corecomponent.ResolveDrupalLayout(opts.Path, resolveProjectDrupalRootfs(opts.Path, opts.DrupalRootfs))
	extensionsPath := filepath.Join(layout.ConfigSyncDir(), "field.field.media.file.field_media_file.yml")
	extensionsData, err := os.ReadFile(extensionsPath) // #nosec G304 -- selected project Drupal config path.
	if err != nil {
		if !enabled && os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read Drupal config field.field.media.file.field_media_file.yml: %w", err)
	}
	rawExtensions, exists := featureYAMLValue(extensionsData, ".settings.file_extensions")
	if !exists {
		if enabled {
			return fmt.Errorf("drupal file extension configuration is missing .settings.file_extensions")
		}
		return nil
	}
	extensions, ok := rawExtensions.(string)
	if !ok {
		return fmt.Errorf("drupal file extension configuration must be a string")
	}
	for _, token := range []string{"htm", "html"} {
		present := featureStringTokenPresent(extensions, token)
		if enabled && !present {
			return fmt.Errorf("hOCR feature requires exact file extension token %q", token)
		}
		if !enabled && present {
			return fmt.Errorf("disabled hOCR feature still contains exact file extension token %q", token)
		}
	}
	if !enabled {
		return nil
	}

	manifestPath := filepath.Join(layout.ConfigSyncDir(), "views.view.iiif_manifest.yml")
	manifest, err := os.ReadFile(manifestPath) // #nosec G304 -- selected project Drupal config path.
	if err != nil {
		return fmt.Errorf("read Drupal config views.view.iiif_manifest.yml: %w", err)
	}
	var expected int
	for _, display := range []string{"rest_export_1", "rest_export_2", "rest_export_3"} {
		path := ".display." + display + ".display_options.style.options.structured_text_term"
		value, exists := featureYAMLValue(manifest, path)
		if !exists {
			return fmt.Errorf("drupal config views.view.iiif_manifest.yml is missing %s", path)
		}
		termID, err := strconv.Atoi(strings.TrimSpace(fmt.Sprint(value)))
		if err != nil || termID <= 0 {
			return fmt.Errorf("hOCR media-use term ID at %s must be a positive integer, got %q", path, fmt.Sprint(value))
		}
		if expected == 0 {
			expected = termID
			continue
		}
		if termID != expected {
			return fmt.Errorf("hOCR media-use term IDs must match across all IIIF exports; %s is %d, expected %d", path, termID, expected)
		}
	}
	if selected := strings.TrimSpace(opts.FeatureBundleOptions[name][HOCRStructuredTextTermOption]); selected != "" && selected != strconv.Itoa(expected) {
		return fmt.Errorf("hOCR media-use term ID is %d, expected selected value %s", expected, selected)
	}
	return nil
}

func featureStringTokenPresent(value, token string) bool {
	for _, field := range strings.Fields(value) {
		if field == token {
			return true
		}
	}
	return false
}

func applyFeatureBundle(opts Options, spec FeatureBundleSpec, enabled bool) error {
	if enabled && spec.Name == FeatureBundleMergePDF {
		if err := applyFeatureIslandoraTag(opts); err != nil {
			return err
		}
	}
	if err := applyFeatureCompose(opts.Path, spec, enabled); err != nil {
		return err
	}
	layout := corecomponent.ResolveDrupalLayout(opts.Path, resolveProjectDrupalRootfs(opts.Path, opts.DrupalRootfs))
	if err := applyFeatureDrupalAssets(layout.ConfigSyncDir(), spec, enabled); err != nil {
		return err
	}
	if err := applyFeatureDrupalMutations(layout.ConfigSyncDir(), spec, opts.FeatureBundleOptions[spec.Name], enabled); err != nil {
		return err
	}
	return applyFeatureComposerRequirements(layout.ComposerJSONPath(), spec.ComposerRequirements, enabled)
}

func applyFeatureIslandoraTag(opts Options) error {
	tag, err := resolvedFeatureIslandoraTag(opts)
	if err != nil {
		return fmt.Errorf("resolve Islandora image tag: %w", err)
	}
	path := filepath.Join(opts.Path, ".env")
	if len(opts.EnvFiles) > 0 {
		candidate := strings.TrimSpace(opts.EnvFiles[len(opts.EnvFiles)-1])
		if candidate != "" {
			path = candidate
			if !filepath.IsAbs(path) {
				path = filepath.Join(opts.Path, path)
			}
		}
	}
	data, err := os.ReadFile(path) // #nosec G304 -- selected project Compose env file.
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read Compose environment: %w", err)
	}
	updated := upsertFeatureEnvValue(data, "ISLANDORA_TAG", tag)
	if bytes.Equal(data, updated) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { // #nosec G301 -- project directory permissions.
		return fmt.Errorf("create Compose environment directory: %w", err)
	}
	if err := writeFilePreserveMode(path, updated); err != nil {
		return fmt.Errorf("write Compose environment: %w", err)
	}
	return nil
}

func resolvedFeatureIslandoraTag(opts Options) (string, error) {
	if tag := strings.TrimSpace(opts.FeatureBundleOptions[FeatureBundleMergePDF][IslandoraTagOption]); tag != "" {
		return tag, nil
	}
	env, err := loadFeatureComposeEnvironment(opts.Path, opts.EnvFiles)
	if err != nil {
		return "", err
	}
	if tag := strings.TrimSpace(env["ISLANDORA_TAG"]); tag != "" {
		return tag, nil
	}
	return defaultIslandoraTag, nil
}

func upsertFeatureEnvValue(data []byte, key, value string) []byte {
	lines := strings.Split(string(data), "\n")
	found := false
	for index, line := range lines {
		candidate := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "export "))
		parts := strings.SplitN(candidate, "=", 2)
		if len(parts) != 2 || strings.TrimSpace(parts[0]) != key {
			continue
		}
		lines[index] = key + "=" + value
		found = true
	}
	if found {
		return []byte(strings.Join(lines, "\n"))
	}
	updated := bytes.Clone(data)
	if len(updated) > 0 && updated[len(updated)-1] != '\n' {
		updated = append(updated, '\n')
	}
	return append(updated, []byte(key+"="+value+"\n")...)
}

func applyFeatureCompose(projectDir string, spec FeatureBundleSpec, enabled bool) error {
	if spec.ComposeService == "" {
		return nil
	}
	composePath := filepath.Join(projectDir, "docker-compose.yml")
	original, err := os.ReadFile(composePath) // #nosec G304 -- selected project Compose path.
	if err != nil {
		return fmt.Errorf("read compose file: %w", err)
	}
	compose, err := corecomponent.LoadComposeFile(composePath)
	if err != nil {
		return err
	}
	if !enabled {
		if !compose.HasService(spec.ComposeService) {
			return nil
		}
		if err := compose.DeleteService(spec.ComposeService); err != nil {
			return err
		}
		return saveValidatedFeatureCompose(composePath, original, compose)
	}
	data, err := fs.ReadFile(featureBundleAssets, filepath.ToSlash(filepath.Join("assets", "feature-bundles", spec.Name, spec.ComposeAsset)))
	if err != nil {
		return fmt.Errorf("read Compose asset: %w", err)
	}
	asset := strings.TrimRight(string(data), "\n")
	if current, ok := compose.ServiceBlock(spec.ComposeService); ok && strings.TrimSpace(current) == strings.TrimSpace(asset) {
		return nil
	}
	if err := compose.DeleteService(spec.ComposeService); err != nil {
		return err
	}
	if err := compose.AddServiceBlock(spec.ComposeService, asset); err != nil {
		return err
	}
	return saveValidatedFeatureCompose(composePath, original, compose)
}

func saveValidatedFeatureCompose(path string, original []byte, compose interface{ Save() error }) error {
	if err := compose.Save(); err != nil {
		return err
	}
	updated, err := os.ReadFile(path) // #nosec G304 -- selected project Compose path.
	if err == nil {
		var document yaml.Node
		err = yaml.Unmarshal(updated, &document)
	}
	if err == nil {
		return nil
	}
	if restoreErr := writeFilePreserveMode(path, original); restoreErr != nil {
		return fmt.Errorf("validate updated Compose file: %v (also failed to restore original: %v)", err, restoreErr)
	}
	return fmt.Errorf("validate updated Compose file: %w", err)
}

func applyFeatureDrupalAssets(configDir string, spec FeatureBundleSpec, enabled bool) error {
	for _, name := range spec.DrupalAssets {
		target := filepath.Join(configDir, filepath.FromSlash(name))
		if !enabled {
			if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("remove Drupal feature asset %s: %w", name, err)
			}
			continue
		}
		data, err := fs.ReadFile(featureBundleAssets, filepath.ToSlash(filepath.Join("assets", "feature-bundles", spec.Name, name)))
		if err != nil {
			return fmt.Errorf("read Drupal feature asset %s: %w", name, err)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil { // #nosec G301 -- generated config must be readable by Drupal.
			return err
		}
		if err := writeFilePreserveMode(target, append(bytes.TrimRight(data, "\n"), '\n')); err != nil {
			return err
		}
	}
	return nil
}

func applyFeatureDrupalMutations(configDir string, spec FeatureBundleSpec, options map[string]string, enabled bool) error {
	byFile := map[string][]FeatureYAMLMutation{}
	for _, mutation := range spec.DrupalMutations {
		byFile[mutation.File] = append(byFile[mutation.File], mutation)
	}
	files := make([]string, 0, len(byFile))
	for name := range byFile {
		files = append(files, name)
	}
	sort.Strings(files)
	for _, name := range files {
		path := filepath.Join(configDir, filepath.FromSlash(name))
		data, err := os.ReadFile(path) // #nosec G304 -- selected project Drupal config path.
		if err != nil {
			if !enabled && os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("read Drupal config %s: %w", name, err)
		}
		doc, err := corecomponent.LoadYAMLDocument(data)
		if err != nil {
			return fmt.Errorf("parse Drupal config %s: %w", name, err)
		}
		for _, mutation := range byFile[name] {
			value := resolveFeatureMutationValue(mutation, options)
			if !enabled {
				currentData, currentErr := doc.Bytes()
				if currentErr != nil {
					return fmt.Errorf("marshal Drupal config %s: %w", name, currentErr)
				}
				_, exists := featureYAMLValue(currentData, mutation.Path)
				if !exists {
					continue
				}
			}
			switch mutation.Kind {
			case featureMutationSet:
				if enabled {
					err = doc.SetValue(mutation.Path, value)
				} else if mutation.RestoreOnDisable {
					err = doc.SetValue(mutation.Path, mutation.DisableValue)
				} else {
					err = doc.DeletePath(mutation.Path)
				}
			case featureMutationAppend:
				if enabled {
					err = doc.AppendUniqueString(mutation.Path, fmt.Sprint(value))
				} else {
					err = doc.RemoveString(mutation.Path, fmt.Sprint(value))
				}
			case featureMutationAppendToken:
				err = applyFeatureYAMLToken(doc, mutation.Path, fmt.Sprint(value), enabled)
			default:
				err = fmt.Errorf("unsupported feature mutation kind %q", mutation.Kind)
			}
			if err != nil {
				return fmt.Errorf("apply %s mutation %s in %s: %w", spec.Name, mutation.Path, name, err)
			}
		}
		updated, err := doc.Bytes()
		if err != nil {
			return fmt.Errorf("marshal Drupal config %s: %w", name, err)
		}
		if !bytes.Equal(data, updated) {
			if err := writeFilePreserveMode(path, updated); err != nil {
				return err
			}
		}
	}
	return nil
}

func resolveFeatureMutationValue(mutation FeatureYAMLMutation, options map[string]string) any {
	value := mutation.Value
	if mutation.OptionName == "" {
		return value
	}
	value = strings.TrimSpace(options[mutation.OptionName])
	if value == "" {
		value = mutation.DefaultValue
	}
	return value
}

func applyFeatureYAMLToken(doc *corecomponent.YAMLDocument, path, token string, enabled bool) error {
	data, err := doc.Bytes()
	if err != nil {
		return err
	}
	currentValue, ok := featureYAMLValue(data, path)
	if !ok {
		return fmt.Errorf("path does not exist")
	}
	current, ok := currentValue.(string)
	if !ok {
		return fmt.Errorf("path is not a string")
	}
	values := strings.Fields(current)
	filtered := make([]string, 0, len(values)+1)
	found := false
	for _, value := range values {
		if value == token {
			found = true
			if enabled {
				filtered = append(filtered, value)
			}
			continue
		}
		filtered = append(filtered, value)
	}
	if enabled && !found {
		filtered = append(filtered, token)
	}
	return doc.SetString(path, strings.Join(filtered, " "))
}

func applyFeatureComposerRequirements(path string, requirements map[string]string, enabled bool) error {
	if len(requirements) == 0 {
		return nil
	}
	data, err := os.ReadFile(path) // #nosec G304 -- selected project composer path.
	if err != nil {
		if !enabled && os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read composer.json: %w", err)
	}
	if !json.Valid(data) {
		return fmt.Errorf("parse composer.json: invalid JSON")
	}
	start, end, found, err := topLevelJSONValueSpan(data, "require")
	if err != nil {
		return fmt.Errorf("parse composer.json: %w", err)
	}
	if !found {
		if !enabled {
			return nil
		}
		return fmt.Errorf("composer.json does not contain a top-level require object")
	}
	require := map[string]json.RawMessage{}
	if err := json.Unmarshal(data[start:end], &require); err != nil {
		return fmt.Errorf("decode composer.json require: %w", err)
	}
	for name, version := range requirements {
		if enabled {
			raw, _ := json.Marshal(version)
			require[name] = raw
		} else {
			delete(require, name)
		}
	}
	replacement, err := json.MarshalIndent(require, "", "    ")
	if err != nil {
		return fmt.Errorf("encode composer.json require: %w", err)
	}
	indent := jsonLineIndent(data, start)
	replacement = bytes.ReplaceAll(replacement, []byte("\n"), append([]byte("\n"), indent...))
	updated := append(bytes.Clone(data[:start]), replacement...)
	updated = append(updated, data[end:]...)
	if !json.Valid(updated) {
		return fmt.Errorf("updated composer.json is invalid")
	}
	if bytes.Equal(data, updated) {
		return nil
	}
	return writeFilePreserveMode(path, updated)
}

func topLevelJSONValueSpan(data []byte, want string) (int, int, bool, error) {
	i := skipJSONSpace(data, 0)
	if i >= len(data) || data[i] != '{' {
		return 0, 0, false, fmt.Errorf("root is not an object")
	}
	i++
	for {
		i = skipJSONSpaceAndComma(data, i)
		if i >= len(data) {
			return 0, 0, false, fmt.Errorf("unterminated root object")
		}
		if data[i] == '}' {
			return 0, 0, false, nil
		}
		keyStart := i
		keyEnd, err := scanJSONString(data, keyStart)
		if err != nil {
			return 0, 0, false, err
		}
		var key string
		if err := json.Unmarshal(data[keyStart:keyEnd], &key); err != nil {
			return 0, 0, false, err
		}
		i = skipJSONSpace(data, keyEnd)
		if i >= len(data) || data[i] != ':' {
			return 0, 0, false, fmt.Errorf("missing colon after key %q", key)
		}
		start := skipJSONSpace(data, i+1)
		end, err := scanJSONValue(data, start)
		if err != nil {
			return 0, 0, false, err
		}
		if key == want {
			return start, end, true, nil
		}
		i = end
	}
}

func scanJSONString(data []byte, start int) (int, error) {
	if start >= len(data) || data[start] != '"' {
		return 0, fmt.Errorf("expected JSON string at byte %d", start)
	}
	escaped := false
	for i := start + 1; i < len(data); i++ {
		if escaped {
			escaped = false
			continue
		}
		if data[i] == '\\' {
			escaped = true
			continue
		}
		if data[i] == '"' {
			return i + 1, nil
		}
	}
	return 0, fmt.Errorf("unterminated JSON string at byte %d", start)
}

func scanJSONValue(data []byte, start int) (int, error) {
	if start >= len(data) {
		return 0, fmt.Errorf("missing JSON value")
	}
	if data[start] == '"' {
		return scanJSONString(data, start)
	}
	if data[start] == '{' || data[start] == '[' {
		stack := []byte{data[start]}
		inString := false
		escaped := false
		for i := start + 1; i < len(data); i++ {
			ch := data[i]
			if inString {
				if escaped {
					escaped = false
				} else if ch == '\\' {
					escaped = true
				} else if ch == '"' {
					inString = false
				}
				continue
			}
			switch ch {
			case '"':
				inString = true
			case '{', '[':
				stack = append(stack, ch)
			case '}', ']':
				open := stack[len(stack)-1]
				if (open == '{' && ch != '}') || (open == '[' && ch != ']') {
					return 0, fmt.Errorf("mismatched JSON delimiter at byte %d", i)
				}
				stack = stack[:len(stack)-1]
				if len(stack) == 0 {
					return i + 1, nil
				}
			}
		}
		return 0, fmt.Errorf("unterminated JSON value at byte %d", start)
	}
	for i := start; i < len(data); i++ {
		if data[i] == ',' || data[i] == '}' || unicode.IsSpace(rune(data[i])) {
			return i, nil
		}
	}
	return len(data), nil
}

func skipJSONSpace(data []byte, index int) int {
	for index < len(data) && unicode.IsSpace(rune(data[index])) {
		index++
	}
	return index
}

func skipJSONSpaceAndComma(data []byte, index int) int {
	for index < len(data) && (unicode.IsSpace(rune(data[index])) || data[index] == ',') {
		index++
	}
	return index
}

func jsonLineIndent(data []byte, index int) []byte {
	lineStart := bytes.LastIndexByte(data[:index], '\n') + 1
	indent := data[lineStart:index]
	for i, ch := range indent {
		if ch != ' ' && ch != '\t' {
			return indent[:i]
		}
	}
	return indent
}

func featureYAMLValue(data []byte, path string) (any, bool) {
	var root map[string]any
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, false
	}
	segments := strings.Split(strings.TrimPrefix(path, "."), ".")
	var current any = root
	for _, segment := range segments {
		mapping := featureMap(current)
		if mapping == nil {
			return nil, false
		}
		var ok bool
		current, ok = mapping[segment]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

func featureYAMLMappingHasAnchor(data []byte, key, anchor string) bool {
	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil || len(document.Content) == 0 {
		return false
	}
	root := document.Content[0]
	if root.Kind != yaml.MappingNode {
		return false
	}
	for index := 0; index+1 < len(root.Content); index += 2 {
		if root.Content[index].Value == key {
			return root.Content[index+1].Anchor == anchor
		}
	}
	return false
}

func featureMap(value any) map[string]any {
	switch typed := value.(type) {
	case map[string]any:
		return typed
	case map[any]any:
		out := make(map[string]any, len(typed))
		for key, value := range typed {
			out[fmt.Sprint(key)] = value
		}
		return out
	default:
		return nil
	}
}

func loadFeatureComposeEnvironment(projectDir string, envFiles []string) (map[string]string, error) {
	env := map[string]string{}
	files := append([]string{}, envFiles...)
	if len(files) == 0 {
		files = []string{".env"}
	}
	for _, name := range files {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if !filepath.IsAbs(name) {
			name = filepath.Join(projectDir, name)
		}
		data, err := os.ReadFile(name) // #nosec G304 -- selected project Compose env file.
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		for key, value := range parseFeatureDotEnv(string(data)) {
			env[key] = value
		}
	}
	for _, item := range os.Environ() {
		parts := strings.SplitN(item, "=", 2)
		if len(parts) == 2 {
			env[parts[0]] = parts[1]
		}
	}
	return env, nil
}

func parseFeatureDotEnv(raw string) map[string]string {
	values := map[string]string{}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		if len(value) >= 2 && ((value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'')) {
			value = value[1 : len(value)-1]
		}
		values[key] = value
	}
	return values
}

func expandFeatureComposeValue(value string, env map[string]string) (string, error) {
	var out strings.Builder
	for i := 0; i < len(value); {
		if value[i] != '$' {
			out.WriteByte(value[i])
			i++
			continue
		}
		if i+1 < len(value) && value[i+1] == '$' {
			out.WriteByte('$')
			i += 2
			continue
		}
		if i+1 < len(value) && value[i+1] == '{' {
			end := strings.IndexByte(value[i+2:], '}')
			if end < 0 {
				return "", fmt.Errorf("unterminated variable expansion")
			}
			end += i + 2
			expression := value[i+2 : end]
			name, fallback, useEmpty, hasFallback := featureComposeExpression(expression)
			resolved, present := env[name]
			if (!present || (useEmpty && resolved == "")) && hasFallback {
				resolved = fallback
				present = true
			}
			if !present {
				return "", fmt.Errorf("environment variable %s is not set", name)
			}
			out.WriteString(resolved)
			i = end + 1
			continue
		}
		end := i + 1
		for end < len(value) && (value[end] == '_' || unicode.IsLetter(rune(value[end])) || unicode.IsDigit(rune(value[end]))) {
			end++
		}
		if end == i+1 {
			out.WriteByte('$')
			i++
			continue
		}
		name := value[i+1 : end]
		resolved, present := env[name]
		if !present {
			return "", fmt.Errorf("environment variable %s is not set", name)
		}
		out.WriteString(resolved)
		i = end
	}
	return out.String(), nil
}

func featureComposeExpression(expression string) (name, fallback string, useEmpty, hasFallback bool) {
	if index := strings.Index(expression, ":-"); index >= 0 {
		return expression[:index], expression[index+2:], true, true
	}
	if index := strings.IndexByte(expression, '-'); index >= 0 {
		return expression[:index], expression[index+1:], false, true
	}
	return expression, "", true, false
}

func splitFeatureImageReference(image string) (repository, tag string) {
	image = strings.TrimSpace(strings.SplitN(image, "@", 2)[0])
	lastSlash := strings.LastIndexByte(image, '/')
	lastColon := strings.LastIndexByte(image, ':')
	if lastColon > lastSlash {
		return image[:lastColon], image[lastColon+1:]
	}
	return image, "latest"
}

func featureRepositoryMatches(actual, expected string) bool {
	actual = strings.TrimPrefix(strings.TrimSpace(actual), "docker.io/")
	expected = strings.TrimPrefix(strings.TrimSpace(expected), "docker.io/")
	return actual == expected
}

func featureRepositoryIn(actual string, repositories []string) bool {
	for _, repository := range repositories {
		if featureRepositoryMatches(actual, repository) {
			return true
		}
	}
	return false
}

func featureVersionAtLeast(actual, minimum string) (bool, error) {
	actualVersion, err := parseFeatureVersion(actual)
	if err != nil {
		return false, err
	}
	minimumVersion, err := parseFeatureVersion(minimum)
	if err != nil {
		return false, err
	}
	actualParts := actualVersion.parts
	minimumParts := minimumVersion.parts
	length := len(actualParts)
	if len(minimumParts) > length {
		length = len(minimumParts)
	}
	for len(actualParts) < length {
		actualParts = append(actualParts, 0)
	}
	for len(minimumParts) < length {
		minimumParts = append(minimumParts, 0)
	}
	for i := 0; i < length; i++ {
		if actualParts[i] > minimumParts[i] {
			return true, nil
		}
		if actualParts[i] < minimumParts[i] {
			return false, nil
		}
	}
	return compareFeaturePrerelease(actualVersion.prerelease, minimumVersion.prerelease) >= 0, nil
}

type parsedFeatureVersion struct {
	parts      []int
	prerelease string
}

func parseFeatureVersion(value string) (parsedFeatureVersion, error) {
	value = strings.TrimPrefix(strings.TrimSpace(value), "v")
	if index := strings.IndexByte(value, '+'); index >= 0 {
		value = value[:index]
	}
	prerelease := ""
	if index := strings.IndexByte(value, '-'); index >= 0 {
		prerelease = value[index+1:]
		value = value[:index]
		if prerelease == "" {
			return parsedFeatureVersion{}, fmt.Errorf("invalid version prerelease")
		}
		for _, identifier := range strings.Split(prerelease, ".") {
			if identifier == "" {
				return parsedFeatureVersion{}, fmt.Errorf("invalid version prerelease %q", prerelease)
			}
			for _, character := range identifier {
				if !unicode.IsLetter(character) && !unicode.IsDigit(character) && character != '-' {
					return parsedFeatureVersion{}, fmt.Errorf("invalid version prerelease %q", prerelease)
				}
			}
		}
	}
	parts := strings.Split(value, ".")
	if len(parts) == 0 {
		return parsedFeatureVersion{}, fmt.Errorf("version is empty")
	}
	parsed := make([]int, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			return parsedFeatureVersion{}, fmt.Errorf("invalid version %q", value)
		}
		number, err := strconv.Atoi(part)
		if err != nil {
			return parsedFeatureVersion{}, fmt.Errorf("tag %q is not a semantic version", value)
		}
		parsed = append(parsed, number)
	}
	return parsedFeatureVersion{parts: parsed, prerelease: prerelease}, nil
}

func compareFeaturePrerelease(actual, minimum string) int {
	if actual == minimum {
		return 0
	}
	if actual == "" {
		return 1
	}
	if minimum == "" {
		return -1
	}
	actualParts := strings.Split(actual, ".")
	minimumParts := strings.Split(minimum, ".")
	length := len(actualParts)
	if len(minimumParts) > length {
		length = len(minimumParts)
	}
	for index := 0; index < length; index++ {
		if index >= len(actualParts) {
			return -1
		}
		if index >= len(minimumParts) {
			return 1
		}
		actualNumber, actualErr := strconv.Atoi(actualParts[index])
		minimumNumber, minimumErr := strconv.Atoi(minimumParts[index])
		switch {
		case actualErr == nil && minimumErr == nil:
			if actualNumber < minimumNumber {
				return -1
			}
			if actualNumber > minimumNumber {
				return 1
			}
		case actualErr == nil:
			return -1
		case minimumErr == nil:
			return 1
		default:
			if actualParts[index] < minimumParts[index] {
				return -1
			}
			if actualParts[index] > minimumParts[index] {
				return 1
			}
		}
	}
	return 0
}

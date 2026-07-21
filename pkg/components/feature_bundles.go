package components

import (
	"sort"

	createpkg "github.com/libops/sitectl-isle/pkg/create"
	corecomponent "github.com/libops/sitectl/pkg/component"
)

// FeatureBundles returns the cross-domain feature components supported by the
// ISLE plugin. Unlike a service-only component, each bundle may own Compose,
// Drupal configuration, and Composer requirements as one reviewed change.
func FeatureBundles(source TemplateSource) []Definition {
	specs := createpkg.FeatureBundleSpecs()
	definitions := make([]Definition, 0, len(specs))
	for _, spec := range specs {
		definitions = append(definitions, FeatureBundle(source, spec.Name))
	}
	return definitions
}

// FeatureBundle returns one cross-domain feature definition.
func FeatureBundle(source TemplateSource, name string) Definition {
	spec, ok := createpkg.FeatureBundleSpecByName(name)
	if !ok {
		return Definition{Name: name}
	}

	onComposeRules := []YAMLRule{}
	offComposeRules := []YAMLRule{}
	onComposeCanonical := []RepoAsset{}
	if spec.ComposeService != "" {
		onComposeRules = append(onComposeRules, YAMLRule{
			Files: []string{"docker-compose.yml"},
			Op:    OpRestore,
			Path:  ".services." + spec.ComposeService,
		})
		if spec.Name == createpkg.FeatureBundleMergePDF {
			secrets := make([]any, 0, len(spec.RequiredSecrets))
			for _, secret := range spec.RequiredSecrets {
				secrets = append(secrets, map[string]any{"source": secret})
			}
			onComposeRules = append(onComposeRules, YAMLRule{
				Files: []string{"docker-compose.yml"},
				Op:    OpSet,
				Path:  ".services.mergepdf.secrets",
				Value: secrets,
			})
		}
		offComposeRules = append(offComposeRules, YAMLRule{
			Files: []string{"docker-compose.yml"},
			Op:    OpDelete,
			Path:  ".services." + spec.ComposeService,
		})
		onComposeCanonical = append(onComposeCanonical, source.ComposeAsset("docker-compose.yml"))
	}

	onDrupalRules := []YAMLRule{}
	offDrupalRules := []YAMLRule{}
	for _, file := range spec.DrupalAssets {
		onDrupalRules = append(onDrupalRules, YAMLRule{Files: []string{file}, Op: OpRestore, Path: "."})
		offDrupalRules = append(offDrupalRules, YAMLRule{Files: []string{file}, Op: OpDelete, Path: "."})
	}
	if spec.Name == createpkg.FeatureBundleMergePDF {
		for _, rule := range []struct {
			path  string
			value any
		}{
			{path: ".id", value: "paged_content_created_aggregated_pdf"},
			{path: ".plugin", value: "emit_node_event"},
			{path: ".configuration.queue", value: "islandora-connector-mergepdf"},
			{path: ".configuration.event", value: "Generate Derivative"},
		} {
			onDrupalRules = append(onDrupalRules, YAMLRule{
				Files: []string{"system.action.paged_content_created_aggregated_pdf.yml"},
				Op:    OpSet,
				Path:  rule.path,
				Value: rule.value,
			})
		}
	}
	for _, mutation := range spec.DrupalMutations {
		onRule := YAMLRule{Files: []string{mutation.File}, Path: mutation.Path, Value: mutation.Value}
		offRule := YAMLRule{Files: []string{mutation.File}, Path: mutation.Path, Value: mutation.Value}
		switch mutation.Kind {
		case "append":
			onRule.Op = OpContains
			offRule.Op = OpNotContains
			// A missing host file is also a valid disabled state. Use a glob
			// that matches only the exact filename so the component evaluator
			// checks OpNotContains when the file exists and has no synthetic
			// missing-file target when it does not.
			offRule.Files = []string{optionalExactFilePattern(mutation.File)}
		case "append-token":
			// The v1 core containment rule is substring-based for scalar
			// strings, so exact whitespace-delimited token membership is
			// checked by ValidateFeatureBundleObservedState instead.
			continue
		case "set":
			onRule.Op = OpSet
			if mutation.PresenceOnly || mutation.OptionName != "" {
				onRule.Op = OpRestore
				onRule.Value = nil
			}
			if mutation.RestoreOnDisable {
				offRule.Op = OpSet
				offRule.Value = mutation.DisableValue
				offRule.Files = []string{optionalExactFilePattern(mutation.File)}
			} else {
				offRule.Op = OpDelete
			}
		default:
			continue
		}
		onDrupalRules = append(onDrupalRules, onRule)
		offDrupalRules = append(offDrupalRules, offRule)
	}

	onFileRules := []corecomponent.FileRule{}
	offFileRules := []corecomponent.FileRule{}
	dependencies := corecomponent.Dependencies{}
	packages := make([]string, 0, len(spec.ComposerRequirements))
	for pkg := range spec.ComposerRequirements {
		packages = append(packages, pkg)
	}
	sort.Strings(packages)
	for _, pkg := range packages {
		version := spec.ComposerRequirements[pkg]
		onFileRules = append(onFileRules, corecomponent.FileRule{
			Files: []string{"composer.json"},
			Op:    corecomponent.OpSet,
			Path:  ".require." + pkg,
			Value: version,
		})
		offFileRules = append(offFileRules, corecomponent.FileRule{
			Files: []string{"composer.json"},
			Op:    corecomponent.OpDelete,
			Path:  ".require." + pkg,
		})
	}
	if name == createpkg.FeatureBundleHOCRSearch {
		dependencies.DrupalModules = []corecomponent.DrupalModuleDependency{
			{Module: "islandora_hocr", ComposerPackage: "discoverygarden/islandora_hocr", Mode: corecomponent.DrupalModuleDependencyStrict},
			{Module: "islandora_iiif_hocr", ComposerPackage: "born-digital/islandora_iiif_hocr", Mode: corecomponent.DrupalModuleDependencyStrict},
		}
	}

	definition := Definition{
		Name:                spec.Name,
		DefaultState:        spec.DefaultState,
		DefaultDisposition:  corecomponent.StateToDisposition(spec.DefaultState),
		AllowedDispositions: []corecomponent.Disposition{corecomponent.DispositionEnabled, corecomponent.DispositionDisabled},
		PromptOnCreate:      false,
		Gates: corecomponent.GateSpec{
			LocalOnly: true,
		},
		Dependencies: dependencies,
		Behavior: corecomponent.Behavior{
			Idempotent: true,
			Enable: corecomponent.TransitionBehavior{
				DataMigration: corecomponent.DataMigrationBackfill,
			},
			Disable: corecomponent.TransitionBehavior{
				DataMigration: corecomponent.DataMigrationNone,
			},
		},
		On: DomainSpec{
			Compose: YAMLStateSpec{Canonical: onComposeCanonical, Rules: onComposeRules},
			Drupal:  YAMLStateSpec{Canonical: []RepoAsset{source.DrupalAsset("config/sync")}, Rules: onDrupalRules},
			Files:   corecomponent.FileStateSpec{Rules: onFileRules},
		},
		Off: DomainSpec{
			Compose: YAMLStateSpec{Rules: offComposeRules},
			Drupal:  YAMLStateSpec{Rules: offDrupalRules},
			Files:   corecomponent.FileStateSpec{Rules: offFileRules},
		},
	}

	switch name {
	case createpkg.FeatureBundleMergePDF:
		definition.Guidance = corecomponent.StateGuidance{
			Question:     "Enable aggregated PDF generation for paged content?",
			EnabledHelp:  "Add the mergepdf service and its Drupal event action as one feature.",
			DisabledHelp: "Remove the mergepdf service and its owned Drupal action.",
		}
		definition.Behavior.Enable.Summary = "Adds the mergepdf Compose service and the paged-content aggregated-PDF action. Existing paged content may require a derivative backfill."
		definition.Behavior.Disable.Summary = "Removes the mergepdf service and its owned Drupal action; existing PDFs are retained."
	case createpkg.FeatureBundleHOCRSearch:
		definition.Guidance = corecomponent.StateGuidance{
			Question:     "Enable hOCR generation, indexing, IIIF annotations, and search?",
			EnabledHelp:  "Add the hOCR Composer packages and narrowly owned Drupal/Solr configuration.",
			DisabledHelp: "Remove the hOCR feature's owned requirements and configuration; generated files remain.",
		}
		definition.Behavior.Enable.Summary = "Adds the hOCR modules and search configuration. Existing images require hOCR derivative generation and a Solr reindex."
		definition.Behavior.Disable.Summary = "Removes hOCR-owned configuration and Composer requirements, restores the starter IIIF tile-field selections, and retains generated derivatives and indexed data."
		definition.FollowUps = []corecomponent.FollowUpSpec{
			{
				Name:                 createpkg.HOCRStructuredTextTermOption,
				Label:                "hOCR media-use term ID",
				FlagName:             "hocr-term-id",
				FlagUsage:            "Drupal taxonomy term ID for https://discoverygarden.ca/use#hocr",
				Question:             "Enter the Drupal taxonomy term ID whose external URI is https://discoverygarden.ca/use#hocr.",
				DefaultValue:         "56",
				Required:             true,
				PromptOnCreate:       false,
				AppliesToDisposition: corecomponent.DispositionEnabled,
			},
		}
	}
	return definition
}

func optionalExactFilePattern(name string) string {
	if name == "" {
		return name
	}
	return "[" + name[:1] + "]" + name[1:]
}

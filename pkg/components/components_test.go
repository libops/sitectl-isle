package components

import (
	"testing"

	corecomponent "github.com/libops/sitectl/pkg/component"
)

func TestFcrepoDefinition(t *testing.T) {
	t.Parallel()

	definition := Fcrepo(TemplateSource{
		Repo: "libops/isle",
		Ref:  "v1.2.3",
	})

	if definition.Name != "fcrepo" {
		t.Fatalf("expected name fcrepo, got %q", definition.Name)
	}
	if definition.DefaultState != corecomponent.StateOff {
		t.Fatalf("expected default state off, got %q", definition.DefaultState)
	}
	if definition.DefaultDisposition != corecomponent.DispositionSuperseded {
		t.Fatalf("expected default disposition superseded, got %q", definition.DefaultDisposition)
	}
	if !definition.PromptOnCreate {
		t.Fatal("expected fcrepo to prompt on create")
	}
	if definition.Guidance.Question == "" {
		t.Fatal("expected fcrepo guidance question")
	}
	if definition.Guidance.EnabledHelp == "" || definition.Guidance.SupersededHelp == "" {
		t.Fatal("expected fcrepo guidance help text")
	}
	if !definition.Gates.LocalOnly {
		t.Fatal("expected fcrepo to be local-only")
	}
	if !definition.Behavior.Idempotent {
		t.Fatal("expected fcrepo to be marked idempotent")
	}
	if definition.Behavior.Enable.DataMigration != corecomponent.DataMigrationBackfill {
		t.Fatalf("expected fcrepo enable migration %q, got %q", corecomponent.DataMigrationBackfill, definition.Behavior.Enable.DataMigration)
	}
	if definition.Behavior.Disable.DataMigration != corecomponent.DataMigrationHard {
		t.Fatalf("expected fcrepo disable migration %q, got %q", corecomponent.DataMigrationHard, definition.Behavior.Disable.DataMigration)
	}
	if len(definition.FollowUps) != 1 || definition.FollowUps[0].Name != "isle-file-system-uri" {
		t.Fatalf("expected fcrepo filesystem follow-up, got %#v", definition.FollowUps)
	}

	if len(definition.Off.Compose.Canonical) != 2 {
		t.Fatalf("expected two canonical compose sources, got %d", len(definition.Off.Compose.Canonical))
	}
	if definition.Off.Compose.Canonical[0].Path != "docker-compose.yml" {
		t.Fatalf("expected docker-compose.yml canonical path, got %q", definition.Off.Compose.Canonical[0].Path)
	}
	if definition.Off.Compose.Canonical[1].Path != "conf/traefik/fcrepo.yml" {
		t.Fatalf("expected fcrepo router canonical path, got %q", definition.Off.Compose.Canonical[1].Path)
	}
	if len(definition.On.Drupal.Canonical) != 1 {
		t.Fatalf("expected one canonical drupal source, got %d", len(definition.On.Drupal.Canonical))
	}
	if definition.On.Drupal.Canonical[0].Path != "config/sync" {
		t.Fatalf("expected config/sync canonical path, got %q", definition.On.Drupal.Canonical[0].Path)
	}

	assertHasRule(t, definition.Off.Compose.Rules, OpDelete, ".services.fcrepo")
	assertHasRule(t, definition.On.Compose.Rules, OpRestore, ".services.fcrepo")
	assertHasWholeFileRule(t, definition.Off.Compose.Rules, OpDelete, "conf/traefik/fcrepo.yml")
	assertHasWholeFileRule(t, definition.On.Compose.Rules, OpRestore, "conf/traefik/fcrepo.yml")
	assertHasRule(t, definition.Off.Compose.Rules, OpDelete, ".services.fcrepo-database-init")
	assertHasRule(t, definition.On.Compose.Rules, OpSet, ".services.fcrepo-database-init.environment.DB_NAME")
	assertHasRule(t, definition.Off.Compose.Rules, OpDelete, ".services.milliner")
	assertHasRule(t, definition.On.Compose.Rules, OpRestore, ".services.milliner")
	assertHasRule(t, definition.Off.Compose.Rules, OpDelete, ".services.drupal.environment.DRUPAL_DEFAULT_FCREPO_URL")
	assertHasRule(t, definition.On.Compose.Rules, OpRestore, ".services.drupal.environment.DRUPAL_DEFAULT_FCREPO_URL")
	assertHasRule(t, definition.Off.Compose.Rules, OpSet, ".services.alpaca.environment.ALPACA_FCREPO_INDEXER_ENABLED")
	assertHasRule(t, definition.On.Compose.Rules, OpSet, ".services.alpaca.environment.ALPACA_FCREPO_INDEXER_ENABLED")
	assertHasWholeFileRule(t, definition.Off.Drupal.Rules, OpDelete, "context.context.external_files.yml")
	assertHasWholeFileRule(t, definition.On.Drupal.Rules, OpRestore, "context.context.external_files.yml")
	assertDoesNotHaveWholeFileRule(t, definition.Off.Drupal.Rules, OpDelete, "context.context.all_media.yml")
	assertDoesNotHaveWholeFileRule(t, definition.Off.Drupal.Rules, OpDelete, "context.context.repository_content.yml")
	assertDoesNotHaveWholeFileRule(t, definition.Off.Drupal.Rules, OpDelete, "context.context.taxonomy_terms.yml")
	assertDoesNotHaveWholeFileRule(t, definition.On.Drupal.Rules, OpRestore, "context.context.all_media.yml")
	assertDoesNotHaveWholeFileRule(t, definition.On.Drupal.Rules, OpRestore, "context.context.repository_content.yml")
	assertDoesNotHaveWholeFileRule(t, definition.On.Drupal.Rules, OpRestore, "context.context.taxonomy_terms.yml")
	assertHasRule(t, definition.Off.Drupal.Rules, OpDelete, ".reactions.index.actions.index_media_in_fedora")
	assertHasRule(t, definition.Off.Drupal.Rules, OpDelete, ".reactions.index.actions.index_node_in_fedora")
	assertHasRule(t, definition.Off.Drupal.Rules, OpDelete, ".reactions.delete.actions.delete_node_from_fedora")
	assertHasRule(t, definition.Off.Drupal.Rules, OpDelete, ".reactions.index.actions.index_taxonomy_term_in_fedora")
	assertHasRule(t, definition.Off.Drupal.Rules, OpDelete, ".reactions.delete.actions.delete_taxonomy_term_in_fedora")
	assertHasSetRule(t, definition.On.Drupal.Rules, ".reactions.index.actions.index_media_in_fedora", "index_media_in_fedora")
	assertHasSetRule(t, definition.On.Drupal.Rules, ".reactions.index.actions.index_node_in_fedora", "index_node_in_fedora")
	assertHasSetRule(t, definition.On.Drupal.Rules, ".reactions.delete.actions.delete_node_from_fedora", "delete_node_from_fedora")
	assertHasSetRule(t, definition.On.Drupal.Rules, ".reactions.index.actions.index_taxonomy_term_in_fedora", "index_taxonomy_term_in_fedora")
	assertHasSetRule(t, definition.On.Drupal.Rules, ".reactions.delete.actions.delete_taxonomy_term_in_fedora", "delete_taxonomy_term_in_fedora")
	assertHasRule(t, definition.Off.Drupal.Rules, OpDelete, ".**.fedoraadmin")
}

func TestBlazegraphDefinition(t *testing.T) {
	t.Parallel()

	definition := Blazegraph(TemplateSource{
		Repo: "libops/isle",
		Ref:  "v1.2.3",
	})

	if definition.Name != "blazegraph" {
		t.Fatalf("expected name blazegraph, got %q", definition.Name)
	}
	if definition.DefaultState != corecomponent.StateOff {
		t.Fatalf("expected default state off, got %q", definition.DefaultState)
	}
	if definition.DefaultDisposition != corecomponent.DispositionDisabled {
		t.Fatalf("expected default disposition disabled, got %q", definition.DefaultDisposition)
	}
	if !definition.PromptOnCreate {
		t.Fatal("expected blazegraph to prompt on create")
	}
	if definition.Guidance.Question == "" {
		t.Fatal("expected blazegraph guidance question")
	}
	if definition.Guidance.EnabledHelp == "" || definition.Guidance.DisabledHelp == "" {
		t.Fatal("expected blazegraph guidance help text")
	}
	if !definition.Gates.LocalOnly {
		t.Fatal("expected blazegraph to be local-only")
	}
	if !definition.Behavior.Idempotent {
		t.Fatal("expected blazegraph to be marked idempotent")
	}
	if definition.Behavior.Enable.DataMigration != corecomponent.DataMigrationBackfill {
		t.Fatalf("expected blazegraph enable migration %q, got %q", corecomponent.DataMigrationBackfill, definition.Behavior.Enable.DataMigration)
	}
	if definition.Behavior.Disable.DataMigration != corecomponent.DataMigrationNone {
		t.Fatalf("expected blazegraph disable migration %q, got %q", corecomponent.DataMigrationNone, definition.Behavior.Disable.DataMigration)
	}

	assertHasRule(t, definition.Off.Compose.Rules, OpDelete, ".services.blazegraph")
	assertHasRule(t, definition.On.Compose.Rules, OpRestore, ".services.blazegraph")
	assertHasRule(t, definition.Off.Compose.Rules, OpSet, ".services.alpaca.environment.ALPACA_TRIPLESTORE_INDEXER_ENABLED")
	assertHasRule(t, definition.On.Compose.Rules, OpSet, ".services.alpaca.environment.ALPACA_TRIPLESTORE_INDEXER_ENABLED")
	assertHasRule(t, definition.Off.Compose.Rules, OpSet, ".services.drupal.environment.DRUPAL_DEFAULT_TRIPLESTORE_NAMESPACE")
	assertHasRule(t, definition.On.Compose.Rules, OpSet, ".services.drupal.environment.DRUPAL_DEFAULT_TRIPLESTORE_NAMESPACE")
	assertHasRule(t, definition.Off.Compose.Rules, OpDelete, ".volumes.blazegraph-data")
	assertHasRule(t, definition.On.Compose.Rules, OpRestore, ".volumes.blazegraph-data")
	assertHasWholeFileRule(t, definition.Off.Drupal.Rules, OpDelete, "system.action.delete_media_from_triplestore.yml")
	assertHasWholeFileRule(t, definition.Off.Drupal.Rules, OpDelete, "system.action.delete_node_from_triplestore.yml")
	assertHasWholeFileRule(t, definition.Off.Drupal.Rules, OpDelete, "system.action.delete_taxonomy_term_in_triplestore.yml")
	assertHasWholeFileRule(t, definition.Off.Drupal.Rules, OpDelete, "system.action.index_media_in_triplestore.yml")
	assertHasWholeFileRule(t, definition.Off.Drupal.Rules, OpDelete, "system.action.index_node_in_triplestore.yml")
	assertHasWholeFileRule(t, definition.Off.Drupal.Rules, OpDelete, "system.action.index_taxonomy_term_in_the_triplestore.yml")
	assertHasWholeFileRule(t, definition.On.Drupal.Rules, OpRestore, "system.action.delete_media_from_triplestore.yml")
	assertHasWholeFileRule(t, definition.On.Drupal.Rules, OpRestore, "system.action.delete_node_from_triplestore.yml")
	assertHasWholeFileRule(t, definition.On.Drupal.Rules, OpRestore, "system.action.delete_taxonomy_term_in_triplestore.yml")
	assertHasWholeFileRule(t, definition.On.Drupal.Rules, OpRestore, "system.action.index_media_in_triplestore.yml")
	assertHasWholeFileRule(t, definition.On.Drupal.Rules, OpRestore, "system.action.index_node_in_triplestore.yml")
	assertHasWholeFileRule(t, definition.On.Drupal.Rules, OpRestore, "system.action.index_taxonomy_term_in_the_triplestore.yml")
	assertHasRule(t, definition.Off.Drupal.Rules, OpDelete, ".reactions.index.actions.index_media_in_triplestore")
	assertHasRule(t, definition.Off.Drupal.Rules, OpDelete, ".reactions.delete.actions.delete_media_from_triplestore")
	assertHasRule(t, definition.Off.Drupal.Rules, OpDelete, ".reactions.index.actions.index_node_in_triplestore")
	assertHasRule(t, definition.Off.Drupal.Rules, OpDelete, ".reactions.delete.actions.delete_node_from_triplestore")
	assertHasRule(t, definition.Off.Drupal.Rules, OpDelete, ".reactions.index.actions.index_taxonomy_term_in_the_triplestore")
	assertHasRule(t, definition.Off.Drupal.Rules, OpDelete, ".reactions.delete.actions.delete_taxonomy_term_in_triplestore")
	assertHasSetRule(t, definition.On.Drupal.Rules, ".reactions.index.actions.index_media_in_triplestore", "index_media_in_triplestore")
	assertHasSetRule(t, definition.On.Drupal.Rules, ".reactions.delete.actions.delete_media_from_triplestore", "delete_media_from_triplestore")
	assertHasSetRule(t, definition.On.Drupal.Rules, ".reactions.index.actions.index_node_in_triplestore", "index_node_in_triplestore")
	assertHasSetRule(t, definition.On.Drupal.Rules, ".reactions.delete.actions.delete_node_from_triplestore", "delete_node_from_triplestore")
	assertHasSetRule(t, definition.On.Drupal.Rules, ".reactions.index.actions.index_taxonomy_term_in_the_triplestore", "index_taxonomy_term_in_the_triplestore")
	assertHasSetRule(t, definition.On.Drupal.Rules, ".reactions.delete.actions.delete_taxonomy_term_in_triplestore", "delete_taxonomy_term_in_triplestore")
}

func TestExternalCantaloupeDefinition(t *testing.T) {
	t.Parallel()

	definition := ExternalCantaloupe()

	if definition.Name != "external-cantaloupe" {
		t.Fatalf("expected name external-cantaloupe, got %q", definition.Name)
	}
	if definition.DefaultState != corecomponent.StateOff {
		t.Fatalf("expected default state off, got %q", definition.DefaultState)
	}
	if definition.DefaultDisposition != corecomponent.DispositionDisabled {
		t.Fatalf("expected default disposition disabled, got %q", definition.DefaultDisposition)
	}
	if len(definition.FollowUps) != 1 || definition.FollowUps[0].Name != "upstream-url" {
		t.Fatalf("expected upstream-url follow-up, got %#v", definition.FollowUps)
	}
	if definition.Guidance.Question == "" || definition.Guidance.DistributedHelp == "" || definition.Guidance.DisabledHelp == "" {
		t.Fatal("expected external-cantaloupe guidance")
	}
	if definition.Behavior.Enable.Summary == "" || definition.Behavior.Disable.Summary == "" {
		t.Fatal("expected external-cantaloupe behavior summaries")
	}
}

func TestIIIFDefinitionDefaultsToLibOpsTemplate(t *testing.T) {
	t.Parallel()

	definition := IIIF(TemplateSource{})
	if definition.DefaultState != corecomponent.StateOn {
		t.Fatalf("expected default state on, got %q", definition.DefaultState)
	}
	if definition.DefaultDisposition != corecomponent.DispositionTriplet {
		t.Fatalf("expected default disposition triplet, got %q", definition.DefaultDisposition)
	}
}

func TestCodebaseDefinition(t *testing.T) {
	t.Parallel()

	definition := Codebase()

	if definition.Name != "codebase" {
		t.Fatalf("expected name codebase, got %q", definition.Name)
	}
	if definition.DefaultState != corecomponent.StateOn {
		t.Fatalf("expected default state on, got %q", definition.DefaultState)
	}
	if definition.DefaultDisposition != corecomponent.DispositionGitRoot {
		t.Fatalf("expected default disposition git-root, got %q", definition.DefaultDisposition)
	}
	if len(definition.AllowedDispositions) != 2 || definition.AllowedDispositions[0] != corecomponent.DispositionNested || definition.AllowedDispositions[1] != corecomponent.DispositionGitRoot {
		t.Fatalf("expected nested/git-root dispositions, got %#v", definition.AllowedDispositions)
	}
	if definition.PromptOnCreate {
		t.Fatal("expected codebase not to prompt during create")
	}
	if !definition.Gates.LocalOnly {
		t.Fatal("expected codebase to be local-only")
	}
	if !definition.Behavior.Idempotent {
		t.Fatal("expected codebase to be marked idempotent")
	}
	assertHasRule(t, definition.On.Compose.Rules, OpSet, ".services.drupal.build.context")
	assertHasRule(t, definition.Off.Compose.Rules, OpSet, ".services.drupal.build.context")
}

func TestDerivativeServiceDefinition(t *testing.T) {
	t.Parallel()

	definition := DerivativeService("homarus")

	if definition.Name != "homarus" {
		t.Fatalf("expected name homarus, got %q", definition.Name)
	}
	if definition.DefaultDisposition != corecomponent.DispositionEnabled {
		t.Fatalf("expected default disposition enabled, got %q", definition.DefaultDisposition)
	}
	if definition.PromptOnCreate {
		t.Fatal("expected derivative service not to prompt during create")
	}
	if !definition.Gates.LocalOnly {
		t.Fatal("expected derivative service to be local-only")
	}
	if len(definition.AllowedDispositions) != 2 || definition.AllowedDispositions[0] != corecomponent.DispositionEnabled || definition.AllowedDispositions[1] != corecomponent.DispositionDistributed {
		t.Fatalf("expected enabled/distributed dispositions, got %#v", definition.AllowedDispositions)
	}

	assertHasRule(t, definition.On.Compose.Rules, OpDelete, ".services.homarus")
	assertHasRule(t, definition.On.Compose.Rules, OpSet, ".services.alpaca.environment.ALPACA_DERIVATIVE_HOMARUS_URL")
	assertHasRule(t, definition.Off.Compose.Rules, OpRestore, ".services.homarus")
	assertHasRule(t, definition.Off.Compose.Rules, OpDelete, ".services.alpaca.environment.ALPACA_DERIVATIVE_HOMARUS_URL")
}

func assertHasWholeFileRule(t *testing.T, rules []YAMLRule, op RuleOp, file string) {
	t.Helper()

	for _, rule := range rules {
		if rule.Op == op && rule.Path == "." && containsString(rule.Files, file) {
			return
		}
	}

	t.Fatalf("expected whole-file rule op=%q file=%q not found", op, file)
}

func assertDoesNotHaveWholeFileRule(t *testing.T, rules []YAMLRule, op RuleOp, file string) {
	t.Helper()

	for _, rule := range rules {
		if rule.Op == op && rule.Path == "." && containsString(rule.Files, file) {
			t.Fatalf("unexpected whole-file rule op=%q file=%q", op, file)
		}
	}
}

func assertHasRule(t *testing.T, rules []YAMLRule, op RuleOp, path string) {
	t.Helper()

	for _, rule := range rules {
		if rule.Op == op && rule.Path == path {
			return
		}
	}

	t.Fatalf("expected rule op=%q path=%q not found", op, path)
}

func assertHasSetRule(t *testing.T, rules []YAMLRule, path, value string) {
	t.Helper()

	for _, rule := range rules {
		if rule.Op == OpSet && rule.Path == path && rule.Value == value {
			return
		}
	}

	t.Fatalf("expected set rule path=%q value=%q not found", path, value)
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

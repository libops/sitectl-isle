package components

import corecomponent "github.com/libops/sitectl/pkg/component"

func Blazegraph(source TemplateSource) Definition {
	return Definition{
		Name:           "blazegraph",
		DefaultState:   corecomponent.StateOn,
		PromptOnCreate: true,
		Guidance: corecomponent.StateGuidance{
			Question: `blazegraph controls triplestore indexing support.
If you do not plan to query Islandora content using SPARQL, you may want to turn this off.
`,
			OnHelp:  "Keep triplestore indexing enabled for the standard Islandora stack.",
			OffHelp: "Remove triplestore indexing services and Drupal actions if you do not need them.",
		},
		Gates: corecomponent.GateSpec{
			LocalOnly: true,
		},
		Behavior: corecomponent.Behavior{
			Idempotent: true,
			Enable: corecomponent.TransitionBehavior{
				DataMigration: corecomponent.DataMigrationBackfill,
				Summary:       "Existing content may need a triplestore backfill after enabling.",
			},
			Disable: corecomponent.TransitionBehavior{
				DataMigration: corecomponent.DataMigrationNone,
				Summary:       "Disabling Blazegraph removes indexing integrations but does not require content migration.",
			},
		},
		On: DomainSpec{
			Compose: YAMLStateSpec{
				Canonical: []RepoAsset{
					source.ComposeAsset("docker-compose.yml"),
				},
				Rules: []YAMLRule{
					{
						Files:       []string{"docker-compose.yml"},
						SourceFiles: []string{"docker-compose.yml"},
						Op:          OpRestore,
						Path:        ".services.blazegraph",
					},
					{
						Files: []string{"docker-compose.yml"},
						Op:    OpSet,
						Path:  ".services.alpaca.environment.ALPACA_TRIPLESTORE_INDEXER_ENABLED",
						Value: "true",
					},
					{
						Files: []string{"docker-compose.yml"},
						Op:    OpSet,
						Path:  ".services.drupal.environment.DRUPAL_DEFAULT_TRIPLESTORE_NAMESPACE",
						Value: "islandora",
					},
					{
						Files:       []string{"docker-compose.yml"},
						SourceFiles: []string{"docker-compose.yml"},
						Op:          OpRestore,
						Path:        ".volumes.blazegraph-data",
					},
				},
			},
			Drupal: YAMLStateSpec{
				Canonical: []RepoAsset{
					source.DrupalAsset("config/sync"),
				},
				Rules: []YAMLRule{
					{
						Files: []string{
							"system.action.delete_media_from_triplestore.yml",
							"system.action.delete_node_from_triplestore.yml",
							"system.action.delete_taxonomy_term_in_triplestore.yml",
							"system.action.index_media_in_triplestore.yml",
							"system.action.index_node_in_triplestore.yml",
							"system.action.index_taxonomy_term_in_the_triplestore.yml",
						},
						SourceFiles: []string{
							"system.action.delete_media_from_triplestore.yml",
							"system.action.delete_node_from_triplestore.yml",
							"system.action.delete_taxonomy_term_in_triplestore.yml",
							"system.action.index_media_in_triplestore.yml",
							"system.action.index_node_in_triplestore.yml",
							"system.action.index_taxonomy_term_in_the_triplestore.yml",
						},
						Op:   OpRestore,
						Path: ".",
					},
					{
						Files: []string{"context.context.all_media.yml"},
						Op:    OpRestore,
						Path:  ".reactions.index.actions.index_media_in_triplestore",
					},
					{
						Files: []string{"context.context.all_media.yml"},
						Op:    OpRestore,
						Path:  ".reactions.delete.actions.delete_media_from_triplestore",
					},
					{
						Files: []string{"context.context.repository_content.yml"},
						Op:    OpRestore,
						Path:  ".reactions.index.actions.index_node_in_triplestore",
					},
					{
						Files: []string{"context.context.repository_content.yml"},
						Op:    OpRestore,
						Path:  ".reactions.delete.actions.delete_node_from_triplestore",
					},
					{
						Files: []string{"context.context.taxonomy_terms.yml"},
						Op:    OpRestore,
						Path:  ".reactions.index.actions.index_taxonomy_term_in_the_triplestore",
					},
					{
						Files: []string{"context.context.taxonomy_terms.yml"},
						Op:    OpRestore,
						Path:  ".reactions.delete.actions.delete_taxonomy_term_in_triplestore",
					},
				},
			},
		},
		Off: DomainSpec{
			Compose: YAMLStateSpec{
				Canonical: []RepoAsset{
					source.ComposeAsset("docker-compose.yml"),
				},
				Rules: []YAMLRule{
					{
						Files:       []string{"docker-compose.yml"},
						SourceFiles: []string{"docker-compose.yml"},
						Op:          OpDelete,
						Path:        ".services.blazegraph",
					},
					{
						Files: []string{"docker-compose.yml"},
						Op:    OpSet,
						Path:  ".services.alpaca.environment.ALPACA_TRIPLESTORE_INDEXER_ENABLED",
						Value: "false",
					},
					{
						Files: []string{"docker-compose.yml"},
						Op:    OpSet,
						Path:  ".services.drupal.environment.DRUPAL_DEFAULT_TRIPLESTORE_NAMESPACE",
						Value: "",
					},
					{
						Files:       []string{"docker-compose.yml"},
						SourceFiles: []string{"docker-compose.yml"},
						Op:          OpDelete,
						Path:        ".volumes.blazegraph-data",
					},
				},
			},
			Drupal: YAMLStateSpec{
				Canonical: []RepoAsset{
					source.DrupalAsset("config/sync"),
				},
				Rules: []YAMLRule{
					{
						Files: []string{
							"system.action.delete_media_from_triplestore.yml",
							"system.action.delete_node_from_triplestore.yml",
							"system.action.delete_taxonomy_term_in_triplestore.yml",
							"system.action.index_media_in_triplestore.yml",
							"system.action.index_node_in_triplestore.yml",
							"system.action.index_taxonomy_term_in_the_triplestore.yml",
						},
						Op:   OpDelete,
						Path: ".",
					},
					{
						Files: []string{"context.context.all_media.yml"},
						Op:    OpDelete,
						Path:  ".reactions.index.actions.index_media_in_triplestore",
					},
					{
						Files: []string{"context.context.all_media.yml"},
						Op:    OpDelete,
						Path:  ".reactions.delete.actions.delete_media_from_triplestore",
					},
					{
						Files: []string{"context.context.repository_content.yml"},
						Op:    OpDelete,
						Path:  ".reactions.index.actions.index_node_in_triplestore",
					},
					{
						Files: []string{"context.context.repository_content.yml"},
						Op:    OpDelete,
						Path:  ".reactions.delete.actions.delete_node_from_triplestore",
					},
					{
						Files: []string{"context.context.taxonomy_terms.yml"},
						Op:    OpDelete,
						Path:  ".reactions.index.actions.index_taxonomy_term_in_the_triplestore",
					},
					{
						Files: []string{"context.context.taxonomy_terms.yml"},
						Op:    OpDelete,
						Path:  ".reactions.delete.actions.delete_taxonomy_term_in_triplestore",
					},
				},
			},
		},
	}
}

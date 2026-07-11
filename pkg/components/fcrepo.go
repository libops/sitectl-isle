package components

import corecomponent "github.com/libops/sitectl/pkg/component"

func Fcrepo(source TemplateSource) Definition {
	return Definition{
		Name:                "fcrepo",
		DefaultState:        corecomponent.StateOff,
		DefaultDisposition:  corecomponent.DispositionSuperseded,
		AllowedDispositions: []corecomponent.Disposition{corecomponent.DispositionEnabled, corecomponent.DispositionSuperseded},
		PromptOnCreate:      true,
		FollowUps: []corecomponent.FollowUpSpec{
			{
				Name:                 "isle-file-system-uri",
				Label:                "Drupal filesystem URI",
				FlagName:             "isle-file-system-uri",
				FlagUsage:            "Filesystem scheme to use when fcrepo is off. Common values are public or private",
				Question:             "Since you chose to disable fcrepo, choose the Drupal filesystem URI to use for stored files.",
				DefaultValue:         "private",
				PromptOnCreate:       true,
				AppliesToDisposition: corecomponent.DispositionSuperseded,
				CustomPrompt:         "Custom URI scheme: ",
				Choices: []corecomponent.Choice{
					{
						Value:   "public",
						Label:   "public",
						Help:    "Use Drupal's public URI with global web access to all files.",
						Aliases: []string{"1"},
					},
					{
						Value:   "private",
						Label:   "private",
						Help:    "Use Drupal's private URI with per-file access control.",
						Aliases: []string{"2"},
					},
					{
						Value:            "__custom__",
						Label:            "custom",
						Help:             "Enter a custom Drupal stream wrapper scheme.",
						Aliases:          []string{"3"},
						AllowCustomInput: true,
					},
				},
			},
		},
		Guidance: corecomponent.StateGuidance{
			Question: `The LibOps template stores files through Drupal without Fedora by default.
Enable Fedora only when this site requires a Fedora-backed Islandora repository.`,
			EnabledHelp:    "Add the Fedora-backed Islandora repository stack.",
			SupersededHelp: "Replace Fedora-backed storage with another storage approach and rewire Drupal to use a different filesystem URI.",
		},
		Gates: corecomponent.GateSpec{
			LocalOnly: true,
		},
		Behavior: corecomponent.Behavior{
			Idempotent: true,
			Enable: corecomponent.TransitionBehavior{
				DataMigration: corecomponent.DataMigrationBackfill,
				Summary:       "Existing filesystem-backed binaries may need to be re-ingested into Fedora after enabling.",
			},
			Disable: corecomponent.TransitionBehavior{
				DataMigration: corecomponent.DataMigrationHard,
				Summary:       "Existing Fedora-backed binaries must be migrated out of Fedora before disabling.",
			},
		},
		On: DomainSpec{
			Compose: YAMLStateSpec{
				Canonical: []RepoAsset{
					source.ComposeAsset("docker-compose.yml"),
					source.ComposeAsset("conf/traefik/fcrepo.yml"),
				},
				Rules: []YAMLRule{
					{
						Files: []string{"conf/traefik/fcrepo.yml"},
						Op:    OpRestore,
						Path:  ".",
					},
					{
						Files: []string{"docker-compose.yml"},
						Op:    OpSet,
						Path:  ".services.fcrepo-database-init.environment.DB_NAME",
						Value: "fcrepo",
					},
					{
						Files:       []string{"docker-compose.yml"},
						SourceFiles: []string{"docker-compose.yml"},
						Op:          OpRestore,
						Path:        ".services.fcrepo",
					},
					{
						Files:       []string{"docker-compose.yml"},
						SourceFiles: []string{"docker-compose.yml"},
						Op:          OpRestore,
						Path:        ".services.milliner",
					},
					{
						Files:       []string{"docker-compose.yml"},
						SourceFiles: []string{"docker-compose.yml"},
						Op:          OpRestore,
						Path:        ".services.drupal.environment.DRUPAL_DEFAULT_FCREPO_URL",
					},
					{
						Files: []string{"docker-compose.yml"},
						Op:    OpSet,
						Path:  ".services.alpaca.environment.ALPACA_FCREPO_INDEXER_ENABLED",
						Value: "true",
					},
					{
						Files:       []string{"docker-compose.yml"},
						SourceFiles: []string{"docker-compose.yml"},
						Op:          OpRestore,
						Path:        ".volumes.fcrepo-data",
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
						},
						SourceFiles: []string{
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
						},
						Op:   OpRestore,
						Path: ".",
					},
					{
						Files:   []string{"*.yml"},
						Exclude: []string{"jsonld.settings.yml"},
						Op:      OpReplace,
						Path:    ".**",
						Old:     "gs-production",
						Value:   "fedora",
					},
				},
			},
		},
		Off: DomainSpec{
			Compose: YAMLStateSpec{
				Canonical: []RepoAsset{
					source.ComposeAsset("docker-compose.yml"),
					source.ComposeAsset("conf/traefik/fcrepo.yml"),
				},
				Rules: []YAMLRule{
					{
						Files: []string{"conf/traefik/fcrepo.yml"},
						Op:    OpDelete,
						Path:  ".",
					},
					{
						Files: []string{"docker-compose.yml"},
						Op:    OpDelete,
						Path:  ".services.fcrepo-database-init",
					},
					{
						Files:       []string{"docker-compose.yml"},
						SourceFiles: []string{"docker-compose.yml"},
						Op:          OpDelete,
						Path:        ".services.fcrepo",
					},
					{
						Files:       []string{"docker-compose.yml"},
						SourceFiles: []string{"docker-compose.yml"},
						Op:          OpDelete,
						Path:        ".services.milliner",
					},
					{
						Files:       []string{"docker-compose.yml"},
						SourceFiles: []string{"docker-compose.yml"},
						Op:          OpDelete,
						Path:        ".services.drupal.environment.DRUPAL_DEFAULT_FCREPO_URL",
					},
					{
						Files: []string{"docker-compose.yml"},
						Op:    OpSet,
						Path:  ".services.alpaca.environment.ALPACA_FCREPO_INDEXER_ENABLED",
						Value: "false",
					},
					{
						Files:       []string{"docker-compose.yml"},
						SourceFiles: []string{"docker-compose.yml"},
						Op:          OpDelete,
						Path:        ".volumes.fcrepo-data",
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
						},
						Op:   OpDelete,
						Path: ".",
					},
					{
						Files: []string{"*.yml"},
						Op:    OpDelete,
						Path:  ".**.fedoraadmin",
					},
					{
						Files:   []string{"*.yml"},
						Exclude: []string{"jsonld.settings.yml"},
						Op:      OpReplace,
						Path:    ".**",
						Old:     "fedora",
						Value:   "gs-production",
					},
				},
			},
		},
	}
}

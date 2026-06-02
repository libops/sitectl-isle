package components

import corecomponent "github.com/libops/sitectl/pkg/component"

func IIIF(source TemplateSource) Definition {
	return Definition{
		Name:                "iiif",
		DefaultState:        corecomponent.StateOff,
		DefaultDisposition:  corecomponent.DispositionCantaloupe,
		AllowedDispositions: []corecomponent.Disposition{corecomponent.DispositionCantaloupe, corecomponent.DispositionTriplet},
		PromptOnCreate:      true,
		Guidance: corecomponent.StateGuidance{
			Question:     "Choose the IIIF image server implementation.",
			EnabledHelp:  "Use Triplet for IIIF Image API 3 under /iiif/3.",
			DisabledHelp: "Use Cantaloupe under /cantaloupe/iiif/2.",
		},
		Gates: corecomponent.GateSpec{
			LocalOnly: true,
		},
		Behavior: corecomponent.Behavior{
			Idempotent: true,
			Enable: corecomponent.TransitionBehavior{
				DataMigration: corecomponent.DataMigrationNone,
				Summary:       "Selecting Triplet replaces the Cantaloupe route with /iiif/3 and updates Drupal's IIIF base URL.",
			},
			Disable: corecomponent.TransitionBehavior{
				DataMigration: corecomponent.DataMigrationNone,
				Summary:       "Selecting Cantaloupe restores /cantaloupe/iiif/2 and updates Drupal's IIIF base URL.",
			},
		},
		On: DomainSpec{
			Compose: YAMLStateSpec{
				Canonical: []RepoAsset{
					source.ComposeAsset("docker-compose.yml"),
				},
				Rules: []YAMLRule{
					{
						Files: []string{"docker-compose.yml"},
						Op:    OpDelete,
						Path:  ".services.cantaloupe",
					},
					{
						Files: []string{"docker-compose.yml"},
						Op:    OpDelete,
						Path:  ".volumes.cantaloupe-data",
					},
					{
						Files: []string{"docker-compose.dev.yml", "docker-compose.dev.yaml"},
						Op:    OpDelete,
						Path:  ".services.cantaloupe",
					},
					{
						Files: []string{"conf/traefik/triplet.yml"},
						Op:    OpRestore,
						Path:  ".",
					},
					{
						Files: []string{"conf/triplet/config.yaml"},
						Op:    OpRestore,
						Path:  ".",
					},
					{
						Files: []string{"conf/traefik/cantaloupe.yml"},
						Op:    OpDelete,
						Path:  ".",
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
						Files: []string{"docker-compose.yml"},
						Op:    OpDelete,
						Path:  ".services.triplet",
					},
					{
						Files: []string{"docker-compose.yml"},
						Op:    OpDelete,
						Path:  ".volumes.triplet-cache",
					},
					{
						Files: []string{"conf/traefik/triplet.yml"},
						Op:    OpDelete,
						Path:  ".",
					},
					{
						Files: []string{"conf/triplet/config.yaml"},
						Op:    OpDelete,
						Path:  ".",
					},
					{
						Files: []string{"conf/traefik/cantaloupe.yml"},
						Op:    OpRestore,
						Path:  ".",
					},
				},
			},
		},
	}
}

func IIIFTopology() Definition {
	return Definition{
		Name:                "iiif-topology",
		DefaultState:        corecomponent.StateOff,
		DefaultDisposition:  corecomponent.DispositionDisabled,
		AllowedDispositions: []corecomponent.Disposition{corecomponent.DispositionDisabled, corecomponent.DispositionDistributed},
		PromptOnCreate:      false,
		FollowUps: []corecomponent.FollowUpSpec{
			{
				Name:                 "upstream-url",
				Label:                "External IIIF upstream URL",
				FlagName:             "iiif-upstream-url",
				FlagUsage:            "External IIIF upstream base URL to use when iiif-topology is distributed",
				Question:             "Enter the upstream base URL Traefik should use for the external IIIF service.",
				DefaultValue:         "https://iiif.example.org",
				PromptOnCreate:       true,
				AppliesToDisposition: corecomponent.DispositionDistributed,
				CustomPrompt:         "Upstream URL: ",
			},
		},
		Guidance: corecomponent.StateGuidance{
			Question:     "Choose whether the selected IIIF server runs in this compose project or behind an external upstream.",
			EnabledHelp:  "Route IIIF traffic to an external upstream and keep a local override for development.",
			DisabledHelp: "Run the selected IIIF server in this compose project.",
		},
		Gates: corecomponent.GateSpec{
			LocalOnly: true,
		},
		Behavior: corecomponent.Behavior{
			Idempotent: true,
			Enable: corecomponent.TransitionBehavior{
				DataMigration: corecomponent.DataMigrationNone,
				Summary:       "Distributed topology removes the selected IIIF service from the base stack and routes Traefik to the configured upstream.",
			},
			Disable: corecomponent.TransitionBehavior{
				DataMigration: corecomponent.DataMigrationNone,
				Summary:       "Local topology runs the selected IIIF service directly in this compose project.",
			},
		},
		On: DomainSpec{
			Compose: YAMLStateSpec{
				Rules: []YAMLRule{
					{
						Files: []string{"docker-compose.yml"},
						Op:    OpRestore,
						Path:  ".services.traefik.environment.IIIF_UPSTREAM_URL",
					},
				},
			},
		},
		Off: DomainSpec{
			Compose: YAMLStateSpec{
				Rules: []YAMLRule{
					{
						Files: []string{"docker-compose.yml"},
						Op:    OpDelete,
						Path:  ".services.traefik.environment.IIIF_UPSTREAM_URL",
					},
				},
			},
		},
	}
}

package components

import corecomponent "github.com/libops/sitectl/pkg/component"

func Codebase() Definition {
	return Definition{
		Name:                "codebase",
		DefaultState:        corecomponent.StateOff,
		DefaultDisposition:  corecomponent.DispositionNested,
		AllowedDispositions: []corecomponent.Disposition{corecomponent.DispositionNested, corecomponent.DispositionGitRoot},
		PromptOnCreate:      false,
		Guidance: corecomponent.StateGuidance{
			Question:     "Choose where the Drupal codebase is laid out in the compose repository.",
			EnabledHelp:  "Move the Drupal codebase and Dockerfile to the git root so the checkout follows libops application layout standards.",
			DisabledHelp: "Keep the upstream isle-site-template layout with Drupal code under drupal/rootfs/var/www/drupal.",
		},
		Gates: corecomponent.GateSpec{
			LocalOnly: true,
		},
		Behavior: corecomponent.Behavior{
			Idempotent: true,
			Enable: corecomponent.TransitionBehavior{
				DataMigration: corecomponent.DataMigrationNone,
				Summary:       "The Drupal codebase and Dockerfile are moved to the repository root and Compose builds drupal from `.`.",
			},
			Disable: corecomponent.TransitionBehavior{
				DataMigration: corecomponent.DataMigrationNone,
				Summary:       "The upstream nested isle-site-template codebase layout is expected.",
			},
		},
		On: DomainSpec{
			Compose: YAMLStateSpec{
				Rules: []YAMLRule{
					{
						Files: []string{"docker-compose.yml"},
						Op:    OpSet,
						Path:  ".services.drupal.build.context",
						Value: ".",
					},
				},
			},
		},
		Off: DomainSpec{
			Compose: YAMLStateSpec{
				Rules: []YAMLRule{
					{
						Files: []string{"docker-compose.yml"},
						Op:    OpSet,
						Path:  ".services.drupal.build.context",
						Value: "./drupal",
					},
				},
			},
		},
	}
}

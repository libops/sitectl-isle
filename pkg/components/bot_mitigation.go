package components

import corecomponent "github.com/libops/sitectl/pkg/component"

const (
	BotMitigationName = "bot-mitigation"
)

func BotMitigation() Definition {
	return Definition{
		Name:                BotMitigationName,
		DefaultState:        corecomponent.StateOff,
		DefaultDisposition:  corecomponent.DispositionDisabled,
		AllowedDispositions: []corecomponent.Disposition{corecomponent.DispositionDisabled, corecomponent.DispositionEnabled},
		PromptOnCreate:      true,
		Guidance: corecomponent.StateGuidance{
			Question:     "Control whether Traefik protects Drupal routes with the captcha-protect Turnstile middleware.",
			EnabledHelp:  "Enable captcha-protect as a local Traefik plugin on the Drupal router.",
			DisabledHelp: "Leave Drupal routes without the captcha-protect middleware.",
		},
		Gates: corecomponent.GateSpec{
			LocalOnly: true,
		},
		Behavior: corecomponent.Behavior{
			Idempotent: true,
			Enable: corecomponent.TransitionBehavior{
				DataMigration: corecomponent.DataMigrationNone,
				Summary:       "Enabling bot mitigation configures Traefik to load captcha-protect and challenge Drupal traffic with Turnstile.",
			},
			Disable: corecomponent.TransitionBehavior{
				DataMigration: corecomponent.DataMigrationNone,
				Summary:       "Disabling bot mitigation removes the captcha-protect Traefik command, mounts, environment, and Drupal router middleware.",
			},
		},
		On: DomainSpec{
			Compose: YAMLStateSpec{
				Rules: []YAMLRule{
					{
						Files: []string{"docker-compose.yml"},
						Op:    OpSet,
						Path:  ".services.traefik.environment.TURNSTILE_SITE_KEY",
						Value: "${TURNSTILE_SITE_KEY:-1x00000000000000000000AA}",
					},
					{
						Files: []string{"docker-compose.yml"},
						Op:    OpSet,
						Path:  ".services.traefik.environment.TURNSTILE_SECRET_KEY",
						Value: "${TURNSTILE_SECRET_KEY:-1x0000000000000000000000000000000AA}",
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
						Path:  ".services.traefik.environment.TURNSTILE_SITE_KEY",
					},
					{
						Files: []string{"docker-compose.yml"},
						Op:    OpDelete,
						Path:  ".services.traefik.environment.TURNSTILE_SECRET_KEY",
					},
				},
			},
		},
	}
}

package components

import corecomponent "github.com/libops/sitectl/pkg/component"

func ISLEEntrypoint() Definition {
	return Definition{
		Name:           "isle-tls",
		DefaultState:   corecomponent.StateOff,
		PromptOnCreate: false,
		Guidance: StateGuidance{
			Question: `Control whether the main docker-compose.yml stack is exposed over HTTP or HTTPS at the ISLE entrypoint.`,
			OnHelp:   "Serve the main stack over HTTPS using either self-managed certificates or Let's Encrypt.",
			OffHelp:  "Serve the main stack over HTTP only.",
		},
		Gates: corecomponent.GateSpec{
			LocalOnly: true,
		},
		Behavior: corecomponent.Behavior{
			Idempotent: true,
			Enable: corecomponent.TransitionBehavior{
				DataMigration: corecomponent.DataMigrationNone,
				Summary:       "Enabling isle-tls configures the main stack to serve HTTPS.",
			},
			Disable: corecomponent.TransitionBehavior{
				DataMigration: corecomponent.DataMigrationNone,
				Summary:       "Disabling isle-tls reverts the main stack to HTTP-only routing.",
			},
		},
	}
}

func ISLEEntrypointOverride() Definition {
	return Definition{
		Name:           "isle-tls-override",
		DefaultState:   corecomponent.StateOff,
		PromptOnCreate: false,
		Guidance: StateGuidance{
			Question: `Control whether docker-compose.dev.yml carries an entrypoint override that differs from the main production definition.`,
			OnHelp:   "Enable an explicit dev-only HTTP or HTTPS entrypoint override.",
			OffHelp:  "Let local development inherit the base docker-compose.yml entrypoint behavior.",
		},
		Gates: corecomponent.GateSpec{
			LocalOnly: true,
		},
		Behavior: corecomponent.Behavior{
			Idempotent: true,
			Enable: corecomponent.TransitionBehavior{
				DataMigration: corecomponent.DataMigrationNone,
				Summary:       "Enabling isle-tls-override writes an HTTP/HTTPS override to docker-compose.dev.yml.",
			},
			Disable: corecomponent.TransitionBehavior{
				DataMigration: corecomponent.DataMigrationNone,
				Summary:       "Disabling isle-tls-override removes the entrypoint override so development inherits the base stack settings.",
			},
		},
	}
}

package components

import (
	"github.com/libops/sitectl-isle/pkg/traefikconfig"
	corecomponent "github.com/libops/sitectl/pkg/component"
)

func ISLEEntrypoint() Definition {
	return Definition{
		Name:                "isle-tls",
		DefaultState:        corecomponent.StateOff,
		DefaultDisposition:  corecomponent.DispositionDisabled,
		AllowedDispositions: []corecomponent.Disposition{corecomponent.DispositionDisabled, corecomponent.DispositionEnabled},
		PromptOnCreate:      false,
		FollowUps: []corecomponent.FollowUpSpec{
			{
				Name:                 "tls-mode",
				Label:                "TLS mode",
				Question:             "Choose how the production stack frontend should be served.",
				DefaultValue:         traefikconfig.ModeSelfManaged,
				AppliesToDisposition: corecomponent.DispositionEnabled,
				Choices: []corecomponent.Choice{
					{Value: traefikconfig.ModeSelfManaged, Label: traefikconfig.ModeSelfManaged, Help: "Use HTTPS with certificates you manage yourself.", Aliases: []string{"1"}},
					{Value: traefikconfig.ModeMkcert, Label: traefikconfig.ModeMkcert, Help: "Use HTTPS with mkcert for local development.", Aliases: []string{"2"}},
					{Value: traefikconfig.ModeLetsEncrypt, Label: traefikconfig.ModeLetsEncrypt, Help: "Use HTTPS with Let's Encrypt automation.", Aliases: []string{"3"}},
				},
			},
		},
		Guidance: StateGuidance{
			Question:     `Control whether the main docker-compose.yml stack is exposed over HTTP or HTTPS at the ISLE entrypoint.`,
			EnabledHelp:  "Serve the main stack over HTTPS using either self-managed certificates or Let's Encrypt.",
			DisabledHelp: "Serve the main stack over HTTP only.",
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
		Name:                "isle-tls-override",
		DefaultState:        corecomponent.StateOff,
		DefaultDisposition:  corecomponent.DispositionDisabled,
		AllowedDispositions: []corecomponent.Disposition{corecomponent.DispositionDisabled, corecomponent.DispositionEnabled},
		PromptOnCreate:      false,
		FollowUps: []corecomponent.FollowUpSpec{
			{
				Name:                 "tls-mode",
				Label:                "TLS mode",
				Question:             "Choose how the development stack frontend should be served.",
				DefaultValue:         traefikconfig.ModeMkcert,
				AppliesToDisposition: corecomponent.DispositionEnabled,
				Choices: []corecomponent.Choice{
					{Value: traefikconfig.ModeHTTP, Label: traefikconfig.ModeHTTP, Help: "Use HTTP only for the dev override.", Aliases: []string{"1"}},
					{Value: traefikconfig.ModeMkcert, Label: traefikconfig.ModeMkcert, Help: "Use HTTPS with mkcert for local development.", Aliases: []string{"2"}},
					{Value: traefikconfig.ModeSelfManaged, Label: traefikconfig.ModeSelfManaged, Help: "Use HTTPS with certificates you manage yourself.", Aliases: []string{"3"}},
					{Value: traefikconfig.ModeLetsEncrypt, Label: traefikconfig.ModeLetsEncrypt, Help: "Use HTTPS with Let's Encrypt automation.", Aliases: []string{"4"}},
				},
			},
		},
		Guidance: StateGuidance{
			Question:     `Control whether the tracked environment-specific compose override carries an entrypoint override that differs from the main production definition.`,
			EnabledHelp:  "Enable an explicit dev-only HTTP or HTTPS entrypoint override.",
			DisabledHelp: "Let local development inherit the base docker-compose.yml entrypoint behavior.",
		},
		Gates: corecomponent.GateSpec{
			LocalOnly: true,
		},
		Behavior: corecomponent.Behavior{
			Idempotent: true,
			Enable: corecomponent.TransitionBehavior{
				DataMigration: corecomponent.DataMigrationNone,
				Summary:       "Enabling isle-tls-override writes an HTTP/HTTPS override to the tracked environment-specific compose file.",
			},
			Disable: corecomponent.TransitionBehavior{
				DataMigration: corecomponent.DataMigrationNone,
				Summary:       "Disabling isle-tls-override removes the environment-specific entrypoint override so development inherits the base stack settings.",
			},
		},
	}
}

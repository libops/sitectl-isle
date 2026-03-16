package components

import corecomponent "github.com/libops/sitectl/pkg/component"

func ExternalCantaloupe() Definition {
	return Definition{
		Name:                "external-cantaloupe",
		DefaultState:        corecomponent.StateOff,
		DefaultDisposition:  corecomponent.DispositionDisabled,
		AllowedDispositions: []corecomponent.Disposition{corecomponent.DispositionDisabled, corecomponent.DispositionDistributed},
		PromptOnCreate:      false,
		FollowUps: []corecomponent.FollowUpSpec{
			{
				Name:                 "upstream-url",
				Label:                "External upstream URL",
				Question:             "Enter the upstream base URL Traefik should use for the external Cantaloupe service.",
				DefaultValue:         "http://cantaloupe:8182",
				AppliesToDisposition: corecomponent.DispositionDistributed,
				CustomPrompt:         "Upstream URL: ",
			},
		},
		Guidance: corecomponent.StateGuidance{
			Question:        `Control whether Cantaloupe is managed by this compose project or routed to an external deployment.`,
			DistributedHelp: "Disable the local Cantaloupe service in the base stack and route /cantaloupe traffic to an external upstream. Local development keeps a tracked override to run Cantaloupe here.",
			DisabledHelp:    "Keep Cantaloupe managed directly by this compose project.",
		},
		Gates: corecomponent.GateSpec{
			LocalOnly: true,
		},
		Behavior: corecomponent.Behavior{
			Idempotent: true,
			Enable: corecomponent.TransitionBehavior{
				DataMigration: corecomponent.DataMigrationNone,
				Summary:       "Enabling external-cantaloupe removes the base Cantaloupe service, rewrites Traefik to target an external upstream, and keeps a local override copy for development.",
			},
			Disable: corecomponent.TransitionBehavior{
				DataMigration: corecomponent.DataMigrationNone,
				Summary:       "Disabling external-cantaloupe restores the base Cantaloupe service and points Traefik back at the local container.",
			},
		},
	}
}

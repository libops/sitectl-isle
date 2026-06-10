package components

import (
	createpkg "github.com/libops/sitectl-isle/pkg/create"
	corecomponent "github.com/libops/sitectl/pkg/component"
)

// DerivativeServices returns component definitions for all derivative services.
func DerivativeServices() []Definition {
	specs := createpkg.DerivativeServiceSpecs()
	defs := make([]Definition, 0, len(specs))
	for _, spec := range specs {
		defs = append(defs, DerivativeService(spec.Name))
	}
	return defs
}

// DerivativeService returns the component definition for one derivative service.
func DerivativeService(name string) Definition {
	spec, ok := derivativeComponentSpecByName(name)
	if !ok {
		spec = createpkg.DerivativeServiceSpec{Name: name}
	}

	onRules := []YAMLRule{
		{
			Files: []string{"docker-compose.yml"},
			Op:    OpDelete,
			Path:  ".services." + spec.Name,
		},
	}
	offRules := []YAMLRule{
		{
			Files: []string{"docker-compose.yml"},
			Op:    OpRestore,
			Path:  ".services." + spec.Name,
		},
	}
	if spec.AlpacaEnv != "" {
		onRules = append(onRules, YAMLRule{
			Files: []string{"docker-compose.yml"},
			Op:    OpSet,
			Path:  ".services.alpaca.environment." + spec.AlpacaEnv,
			Value: spec.ExternalURL,
		})
		offRules = append(offRules, YAMLRule{
			Files: []string{"docker-compose.yml"},
			Op:    OpDelete,
			Path:  ".services.alpaca.environment." + spec.AlpacaEnv,
		})
	}

	return Definition{
		Name:                spec.Name,
		DefaultState:        corecomponent.StateOff,
		DefaultDisposition:  corecomponent.DispositionEnabled,
		AllowedDispositions: []corecomponent.Disposition{corecomponent.DispositionEnabled, corecomponent.DispositionDistributed},
		PromptOnCreate:      false,
		Guidance: corecomponent.StateGuidance{
			Question:        "Choose whether this derivative microservice runs in this compose project or uses the managed LibOps deployment.",
			EnabledHelp:     "Run the derivative microservice directly in this compose project.",
			DistributedHelp: "Remove the local derivative microservice from the base stack and use the managed LibOps microservice endpoint.",
		},
		Gates: corecomponent.GateSpec{
			LocalOnly: true,
		},
		Behavior: corecomponent.Behavior{
			Idempotent: true,
			Enable: corecomponent.TransitionBehavior{
				DataMigration: corecomponent.DataMigrationNone,
				Summary:       "Distributed mode removes the local service and points the caller at the managed LibOps microservice endpoint.",
			},
			Disable: corecomponent.TransitionBehavior{
				DataMigration: corecomponent.DataMigrationNone,
				Summary:       "Local mode runs the derivative service directly in this compose project.",
			},
		},
		On: DomainSpec{
			Compose: YAMLStateSpec{
				Rules: onRules,
			},
		},
		Off: DomainSpec{
			Compose: YAMLStateSpec{
				Rules: offRules,
			},
		},
	}
}

func derivativeComponentSpecByName(name string) (createpkg.DerivativeServiceSpec, bool) {
	for _, spec := range createpkg.DerivativeServiceSpecs() {
		if spec.Name == name {
			return spec, true
		}
	}
	return createpkg.DerivativeServiceSpec{}, false
}

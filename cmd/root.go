package cmd

import (
	pluginjobs "github.com/libops/sitectl-isle/pkg/jobs"
	"github.com/libops/sitectl/pkg/plugin"
)

var commandSDK *plugin.SDK

// RegisterCommands registers all isle commands with the plugin SDK
func RegisterCommands(sdk *plugin.SDK) {
	commandSDK = sdk
	sdk.SetComposeProjectDiscovery(plugin.ComposeProjectDiscovery{
		RequiredServices:         []string{"drupal"},
		RequiredComposerPackages: []string{"drupal/islandora"},
		Reason:                   "drupal service with drupal/islandora in composer.json",
	})
	sdk.RegisterContextValidator(isleContextValidator)
	pluginjobs.Register(sdk)
	sdk.RegisterComponentDefinitions(orderedComponentDefinitions()...)
	sdk.AddCommand(sdk.GetDiscoveryMetadataCommand())
	sdk.AddCommand(componentExtensionCmd)
	sdk.AddCommand(cacheCmd)
	sdk.RegisterCreateRunner(createDefinition(), createRunner{})
	sdk.AddStandardComposeCommands(plugin.StandardComposeCommandOptions{
		DisplayName:     "ISLE",
		BuildCommands:   createDefinition().DockerComposeBuild,
		InitCommands:    createDefinition().DockerComposeInit,
		UpCommands:      createDefinition().DockerComposeUp,
		DownCommands:    createDefinition().DockerComposeDown,
		RolloutCommands: createDefinition().DockerComposeRollout,
	})
	sdk.RegisterDebugHandler(&isleDebugRunner{})
	sdk.RegisterConvergeRunner(&isleConvergeRunner{})
	sdk.RegisterSetRunner(&isleSetRunner{})
	sdk.RegisterValidateRunner(&isleValidateRunner{})
	sdk.AddCommand(migrateCmd)
	sdk.AddCommand(syncCmd)
	sdk.AddCommand(validateCmd)
}

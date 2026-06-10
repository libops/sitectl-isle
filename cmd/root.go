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
	pluginjobs.Register(sdk)
	sdk.RegisterComponentDefinitions(orderedComponentDefinitions()...)
	sdk.RegisterComponentCommand(componentExtensionCmd)
	sdk.AddCommand(cacheCmd)
	sdk.RegisterCreateRunner(createDefinition(), createRunner{})
	sdk.RegisterDebugRunner(&isleDebugRunner{})
	sdk.RegisterConvergeRunner(&isleConvergeRunner{})
	sdk.RegisterSetRunner(&isleSetRunner{})
	sdk.RegisterValidateRunner(&isleValidateRunner{})
	sdk.RegisterHealthcheckRunner(isleHealthcheckRunner{})
	sdk.AddCommand(migrateCmd)
	sdk.AddCommand(syncCmd)
}

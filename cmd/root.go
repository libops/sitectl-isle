package cmd

import (
	pluginjobs "github.com/libops/sitectl-isle/pkg/jobs"
	"github.com/libops/sitectl/pkg/plugin"
)

var commandSDK *plugin.SDK

// RegisterCommands registers all isle commands with the plugin SDK
func RegisterCommands(sdk *plugin.SDK) {
	commandSDK = sdk
	sdk.RegisterContextValidator(isleContextValidator)
	pluginjobs.Register(sdk)
	sdk.RegisterComponentDefinitions(orderedComponentDefinitions()...)
	sdk.AddCommand(sdk.GetDiscoveryMetadataCommand())
	sdk.AddCommand(componentExtensionCmd)
	sdk.AddCommand(cacheCmd)
	sdk.RegisterCreateRunner(createDefinition(), createRunner{})
	sdk.RegisterDebugHandler(&isleDebugRunner{})
	sdk.RegisterConvergeRunner(&isleConvergeRunner{})
	sdk.RegisterSetRunner(&isleSetRunner{})
	sdk.RegisterValidateRunner(&isleValidateRunner{})
	sdk.AddCommand(migrateCmd)
	sdk.AddCommand(syncCmd)
	sdk.AddCommand(validateCmd)
}

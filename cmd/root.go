package cmd

import (
	"github.com/libops/sitectl/pkg/plugin"
)

var commandSDK *plugin.SDK

// RegisterCommands registers all isle commands with the plugin SDK
func RegisterCommands(sdk *plugin.SDK) {
	commandSDK = sdk
	sdk.RegisterContextValidator(isleContextValidator)
	sdk.AddCommand(componentExtensionCmd)
	sdk.AddCommand(cacheCmd)
	sdk.AddCommand(createCmd)
	sdk.AddCommand(debugExtensionCmd)
	sdk.AddCommand(migrateCmd)
	sdk.AddCommand(validateCmd)
}

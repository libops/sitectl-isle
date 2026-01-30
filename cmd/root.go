package cmd

import (
	"github.com/libops/sitectl/pkg/plugin"
)

// RegisterCommands registers all isle commands with the plugin SDK
func RegisterCommands(sdk *plugin.SDK) {
	sdk.AddCommand(cacheCmd)
	sdk.AddCommand(migrateCmd)
	sdk.AddCommand(transformCmd)
	sdk.AddCommand(nodeCmd)
}

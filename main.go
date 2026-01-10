package main

import (
	"github.com/libops/sitectl-isle/cmd"
	"github.com/libops/sitectl/pkg/plugin"
)

func main() {
	sdk := plugin.NewSDK(plugin.Metadata{
		Name:        "isle",
		Version:     "1.0.0",
		Description: "Islandora (ISLE) utilities and migration tools",
		Author:      "libops",
	})

	cmd.RegisterCommands(sdk)

	sdk.Execute()
}

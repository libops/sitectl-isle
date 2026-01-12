package main

import (
	"github.com/libops/sitectl-isle/cmd"
	"github.com/libops/sitectl/pkg/plugin"
)

func main() {
	sdk := plugin.NewSDK(plugin.Metadata{
		Name:        "isle",
		Version:     "v0.0.6",
		Description: "Islandora (ISLE) utilities and migration tools",
		Author:      "libops",
	})

	cmd.RegisterCommands(sdk)

	sdk.Execute()
}

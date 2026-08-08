package main

import (
	"fmt"

	"github.com/libops/sitectl-isle/cmd"
	"github.com/libops/sitectl/pkg/plugin"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	sdk := plugin.NewSDK(plugin.Metadata{
		Name:         "isle",
		Version:      fmt.Sprintf("%s (Built on %s from Git SHA %s)", version, date, commit),
		Description:  "Islandora (ISLE) utilities and migration tools",
		Author:       "libops",
		TemplateRepo: "https://github.com/libops/isle",
		Includes:     cmd.IncludedPlugins(),
	})

	cmd.RegisterCommands(sdk)

	sdk.Execute()
}

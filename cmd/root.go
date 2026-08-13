package cmd

import (
	pluginjobs "github.com/libops/sitectl-isle/pkg/jobs"
	"github.com/libops/sitectl/pkg/config"
	"github.com/libops/sitectl/pkg/plugin"
	"github.com/spf13/cobra"
)

var commandSDK *plugin.SDK

// RegisterCommands registers all isle commands with the plugin SDK
func RegisterCommands(sdk *plugin.SDK) {
	commandSDK = sdk
	sdk.SetComposeProjectDiscovery(plugin.ComposeProjectDiscovery{
		RequiredServices: []string{"drupal", "alpaca"},
		Reason:           "drupal and alpaca services from ISLE site template",
	})
	pluginjobs.Register(sdk, func(cmd *cobra.Command, ctx *config.Context) (string, error) {
		resolved, err := isleEndpointProvider.App(cmd, ctx)
		return resolved.URL, err
	})
	sdk.RegisterComponentDefinitions(orderedComponentDefinitions()...)
	sdk.RegisterComponentCommand(componentExtensionCmd)
	sdk.AddCommand(cacheCmd)
	sdk.RegisterCreateRunner(createDefinition(), createRunner{})
	sdk.RegisterDeployRunner(isleDeployDefinition(), isleDeployRunner{})
	sdk.RegisterDebugRunner(&isleDebugRunner{})
	sdk.RegisterConvergeRunner(&isleConvergeRunner{})
	sdk.RegisterSetRunner(&isleSetRunner{})
	sdk.RegisterValidateRunner(&isleValidateRunner{})
	sdk.RegisterHealthcheckRunner(isleHealthcheckRunner{})
	sdk.RegisterIngressRouteProvider(isleEndpointProvider)
	sdk.RegisterVerifyRunner(&isleVerifyRunner{})
	sdk.AddCommand(recoveryCmd)
	sdk.AddCommand(syncCmd)
}

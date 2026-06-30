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
		RequiredServices: []string{"drupal", "alpaca"},
		Reason:           "drupal and alpaca services from ISLE site template",
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
	sdk.RegisterIngressRouteProvider(isleIngressRouteProvider{})
	sdk.RegisterVerifyRunner(&isleVerifyRunner{})
	sdk.AddCommand(syncCmd)
}

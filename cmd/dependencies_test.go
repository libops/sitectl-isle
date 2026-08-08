package cmd

import (
	"reflect"
	"testing"

	"github.com/libops/sitectl/pkg/plugin"
)

func TestIncludedPlugins(t *testing.T) {
	t.Parallel()

	want := []string{"drupal"}
	if got := IncludedPlugins(); !reflect.DeepEqual(got, want) {
		t.Fatalf("IncludedPlugins() = %v, want %v", got, want)
	}
}

func TestRegisterCommandsUsesRPCValidationAndCuratedDirectCommands(t *testing.T) {
	oldSDK := commandSDK
	t.Cleanup(func() {
		commandSDK = oldSDK
	})

	sdk := plugin.NewSDK(plugin.Metadata{Name: "isle", Includes: IncludedPlugins()})
	RegisterCommands(sdk)

	if got := len(sdk.ContextValidators()); got != 0 {
		t.Fatalf("expected no legacy context validators, got %d", got)
	}
	deploys := sdk.DeployDefinitions()
	if len(deploys) != 1 || deploys[0].Name != "default" {
		t.Fatalf("expected the outage-prevention deploy contract, got %#v", deploys)
	}
	for _, name := range []string{"component", "status", "validate"} {
		if _, _, err := sdk.RootCmd.Find([]string{name}); err == nil {
			t.Fatalf("did not expect legacy direct command %q to be registered", name)
		}
	}
	for _, name := range []string{"cache", "sync"} {
		if _, _, err := sdk.RootCmd.Find([]string{name}); err != nil {
			t.Fatalf("expected direct command %q to remain registered: %v", name, err)
		}
	}
}

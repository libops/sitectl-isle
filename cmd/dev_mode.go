package cmd

import (
	_ "embed"
	"fmt"
	"path/filepath"
	"strings"

	corecomponent "github.com/libops/sitectl/pkg/component"
	"github.com/libops/sitectl/pkg/config"
)

//go:embed assets/dev-mode/activemq.yml
var devModeActiveMQRouterYAML string

//go:embed assets/dev-mode/blazegraph.yml
var devModeBlazegraphRouterYAML string

//go:embed assets/dev-mode/solr.yml
var devModeSolrRouterYAML string

//go:embed assets/dev-mode/traefik.yml
var devModeTraefikRouterYAML string

//go:embed assets/dev-mode/traefik-override.yml
var devModeTraefikOverrideYAML string

var isleDevModeRouters = map[string]string{
	"activemq.yml":   devModeActiveMQRouterYAML,
	"blazegraph.yml": devModeBlazegraphRouterYAML,
	"solr.yml":       devModeSolrRouterYAML,
	"traefik.yml":    devModeTraefikRouterYAML,
}

func applyISLEDevMode(ctx *config.Context, enabled bool) error {
	if ctx == nil {
		return fmt.Errorf("context is nil")
	}
	if enabled {
		if err := writeISLEDevModeTraefikOverride(ctx); err != nil {
			return err
		}
		return writeISLEDevModeRouters(ctx)
	}
	return removeISLEDevModeRouters(ctx)
}

func writeISLEDevModeTraefikOverride(ctx *config.Context) error {
	overridePath := ctx.ResolveProjectPath(config.LocalDevComposeOverrideName)
	compose, err := corecomponent.LoadComposeFileOptionalForContext(ctx, overridePath)
	if err != nil {
		return err
	}
	if err := compose.DeleteService("traefik"); err != nil {
		return err
	}
	if err := compose.AddServiceBlock("traefik", strings.TrimRight(devModeTraefikOverrideYAML, "\n")); err != nil {
		return err
	}
	return compose.Save()
}

func writeISLEDevModeRouters(ctx *config.Context) error {
	for name, contents := range isleDevModeRouters {
		path := ctx.ResolveProjectPath(filepath.Join("conf", "traefik", name))
		if err := ctx.WriteFile(path, []byte(strings.TrimRight(contents, "\n")+"\n")); err != nil {
			return fmt.Errorf("write dev-mode router %q: %w", name, err)
		}
	}
	return nil
}

func removeISLEDevModeRouters(ctx *config.Context) error {
	for name := range isleDevModeRouters {
		path := ctx.ResolveProjectPath(filepath.Join("conf", "traefik", name))
		if err := ctx.RemoveFile(path); err != nil {
			return fmt.Errorf("remove dev-mode router %q: %w", name, err)
		}
	}
	return nil
}

package cmd

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	createpkg "github.com/libops/sitectl-isle/pkg/create"
	corecomponent "github.com/libops/sitectl/pkg/component"
	"github.com/libops/sitectl/pkg/config"
	coretraefik "github.com/libops/sitectl/pkg/services/traefik"
	yaml "gopkg.in/yaml.v3"
)

const drupalFcrepoInternalURL = "http://fcrepo:8080/fcrepo/rest/"

func applyISLEIngressFiles(ctx *config.Context, values map[string]string) error {
	if ctx == nil {
		return fmt.Errorf("context is nil")
	}
	if err := createpkg.SyncLocalDrupalInternalIngress(ctx.ProjectDir, ingressDomain(values) == coretraefik.DefaultIngressDomain); err != nil {
		return err
	}
	if err := applyISLEFcrepoIngressEnv(ctx, values); err != nil {
		return err
	}
	baseURL := ingressBaseURL(values)
	path := ctx.ResolveProjectPath(filepath.Join("conf", "triplet", "config.yaml"))
	data, err := ctx.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return createpkg.SyncBotMitigationBypass(ctx.ProjectDir)
		}
		return fmt.Errorf("read Triplet config: %w", err)
	}
	var root any
	if err := yaml.Unmarshal(data, &root); err != nil {
		return fmt.Errorf("parse Triplet config: %w", err)
	}
	setNestedYAMLValue(root, []string{"server", "public_base_url"}, baseURL)
	setNestedYAMLValue(root, []string{"iiif", "image", "allowed_origins"}, []any{baseURL})
	setNestedYAMLValue(root, []string{"sources", "http", "allowed_origins"}, []any{baseURL})
	updated, err := yaml.Marshal(root)
	if err != nil {
		return fmt.Errorf("marshal Triplet config: %w", err)
	}
	if err := ctx.WriteFile(path, updated); err != nil {
		return err
	}
	return createpkg.SyncBotMitigationBypass(ctx.ProjectDir)
}

func applyISLEFcrepoIngressEnv(ctx *config.Context, values map[string]string) error {
	compose, err := corecomponent.LoadComposeFile(ctx.ResolveProjectPath("docker-compose.yml"))
	if err != nil {
		return err
	}
	if !compose.HasService("fcrepo") {
		for _, key := range []string{
			"DRUPAL_DEFAULT_FCREPO_HOST",
			"DRUPAL_DEFAULT_FCREPO_PORT",
			"DRUPAL_DEFAULT_FCREPO_URL",
		} {
			if err := compose.DeleteServiceEnv("drupal", key); err != nil {
				return err
			}
		}
		return compose.Save()
	}
	if err := compose.SetServiceEnv("drupal", "DRUPAL_DEFAULT_FCREPO_URL", drupalFcrepoInternalURL); err != nil {
		return err
	}
	return compose.Save()
}

func ingressBaseURL(values map[string]string) string {
	return ingressScheme(values) + "://" + ingressDomain(values)
}

func ingressScheme(values map[string]string) string {
	mode := strings.TrimSpace(values["mode"])
	switch mode {
	case coretraefik.IngressModeHTTPSDefault, coretraefik.IngressModeHTTPSLetsEncrypt:
		return "https"
	default:
		return "http"
	}
}

func ingressDomain(values map[string]string) string {
	domain := strings.TrimSpace(values["domain"])
	if domain == "" {
		domain = coretraefik.DefaultIngressDomain
	}
	return domain
}

func setNestedYAMLValue(root any, path []string, value any) {
	if len(path) == 0 {
		return
	}
	current := asYAMLMap(root)
	for i, part := range path {
		if i == len(path)-1 {
			current[part] = value
			return
		}
		next := asYAMLMap(current[part])
		if next == nil {
			next = map[string]any{}
			current[part] = next
		}
		current = next
	}
}

func asYAMLMap(value any) map[string]any {
	switch typed := value.(type) {
	case map[string]any:
		return typed
	case map[any]any:
		out := map[string]any{}
		for key, value := range typed {
			out[fmt.Sprint(key)] = value
		}
		return out
	default:
		return nil
	}
}

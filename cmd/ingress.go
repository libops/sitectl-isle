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

const (
	drupalFcrepoInternalURL = "http://fcrepo:8080/fcrepo/rest/"
	drupalInternalHostname  = "drupal.internal"
)

func applyISLEIngressFiles(ctx *config.Context, values map[string]string) error {
	if ctx == nil {
		return fmt.Errorf("context is nil")
	}
	if ctx.DockerHostType == "" {
		localCtx := *ctx
		localCtx.DockerHostType = config.ContextLocal
		ctx = &localCtx
	}
	if err := applyISLEFcrepoIngressEnv(ctx, values); err != nil {
		return err
	}
	baseURL := ingressBaseURL(values)
	path := ctx.ResolveProjectPath(filepath.Join("conf", "triplet", "config.yaml"))
	data, err := ctx.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return createpkg.SyncBotMitigationBypassContext(ctx)
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
	return createpkg.SyncBotMitigationBypassContext(ctx)
}

func applyISLEFcrepoIngressEnv(ctx *config.Context, values map[string]string) error {
	compose, err := corecomponent.LoadComposeFileForContext(ctx, ctx.ResolveProjectPath("docker-compose.yml"))
	if err != nil {
		return err
	}
	localFcrepo := compose.HasService("fcrepo") && ingressDomain(values) == coretraefik.DefaultIngressDomain
	if err := compose.SetServiceEnv("drupal", "INGRESS_HOSTNAMES", strings.Join(isleIngressHostnames(ctx, values, localFcrepo), ",")); err != nil {
		return err
	}
	if err := createpkg.SyncLocalDrupalInternalIngressContext(ctx, localFcrepo); err != nil {
		return err
	}
	for _, key := range []string{
		"DRUPAL_DEFAULT_SITE_URL",
		"DRUPAL_ENABLE_HTTPS",
		"DRUPAL_TRUSTED_HOST_PATTERNS",
		"DRUSH_OPTIONS_URI",
	} {
		if err := compose.DeleteServiceEnv("drupal", key); err != nil {
			return err
		}
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
	if localFcrepo {
		if err := compose.SetServiceEnv("fcrepo", "FCREPO_ALLOW_EXTERNAL_DRUPAL", createpkg.LocalDrupalBaseURL+"/"); err != nil {
			return err
		}
	}
	return compose.Save()
}

func isleIngressHostnames(ctx *config.Context, values map[string]string, includeInternalDrupal bool) []string {
	update := coretraefik.IngressAppUpdate{
		Mode:   strings.TrimSpace(values["mode"]),
		Domain: ingressDomain(values),
		Scheme: ingressScheme(values),
	}
	hosts := coretraefik.SuggestedApplicationHosts(ctx, update)
	if includeInternalDrupal {
		hosts = appendUniqueISLEHostname(hosts, drupalInternalHostname)
	}
	return hosts
}

func appendUniqueISLEHostname(hosts []string, hostname string) []string {
	hostname = strings.Trim(strings.TrimSpace(hostname), "[]")
	if hostname == "" {
		return hosts
	}
	for _, existing := range hosts {
		if strings.EqualFold(existing, hostname) {
			return hosts
		}
	}
	return append(hosts, hostname)
}

func ingressBaseURL(values map[string]string) string {
	return ingressScheme(values) + "://" + ingressDomain(values)
}

func ingressScheme(values map[string]string) string {
	if coretraefik.IngressModeUsesHTTPS(values["mode"]) {
		return "https"
	}
	return "http"
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

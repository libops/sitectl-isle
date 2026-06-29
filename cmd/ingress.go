package cmd

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/libops/sitectl/pkg/config"
	coretraefik "github.com/libops/sitectl/pkg/services/traefik"
	yaml "gopkg.in/yaml.v3"
)

func applyISLEIngressFiles(ctx *config.Context, values map[string]string) error {
	if ctx == nil {
		return fmt.Errorf("context is nil")
	}
	baseURL := ingressBaseURL(values)
	path := ctx.ResolveProjectPath(filepath.Join("conf", "triplet", "config.yaml"))
	data, err := ctx.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
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
	return ctx.WriteFile(path, updated)
}

func ingressBaseURL(values map[string]string) string {
	mode := strings.TrimSpace(values["mode"])
	scheme := "http"
	switch mode {
	case coretraefik.IngressModeHTTPSDefault, coretraefik.IngressModeHTTPSLetsEncrypt:
		scheme = "https"
	}
	domain := strings.TrimSpace(values["domain"])
	if domain == "" {
		domain = coretraefik.DefaultIngressDomain
	}
	return scheme + "://" + domain
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

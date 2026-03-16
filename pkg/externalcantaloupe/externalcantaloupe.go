package externalcantaloupe

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/libops/sitectl/pkg/config"
	yaml "gopkg.in/yaml.v3"
)

const (
	DefaultLocalUpstream     = "http://cantaloupe:8182"
	DefaultTraefikConfigPath = "conf/traefik/cantaloupe.yml"
)

type Status struct {
	Enabled     bool
	Drifted     bool
	UpstreamURL string
	Detail      string
}

func Detect(projectDir, overridePath string) (Status, error) {
	baseComposePath := filepath.Join(projectDir, "docker-compose.yml")
	baseDoc, err := readCompose(baseComposePath)
	if err != nil {
		return Status{}, err
	}
	baseHasCantaloupe := hasService(baseDoc, "cantaloupe")

	overrideDoc, err := readComposeOptional(overridePath)
	if err != nil {
		return Status{}, err
	}
	overrideHasCantaloupe := hasService(overrideDoc, "cantaloupe")

	upstreamURL, err := currentUpstreamURL(filepath.Join(projectDir, DefaultTraefikConfigPath))
	if err != nil {
		return Status{}, err
	}

	switch {
	case baseHasCantaloupe && !overrideHasCantaloupe && upstreamURL == DefaultLocalUpstream:
		return Status{
			Enabled:     false,
			Drifted:     false,
			UpstreamURL: upstreamURL,
			Detail:      "base compose manages cantaloupe",
		}, nil
	case !baseHasCantaloupe && upstreamURL != "" && upstreamURL != DefaultLocalUpstream && overrideHasCantaloupe:
		return Status{
			Enabled:     true,
			Drifted:     false,
			UpstreamURL: upstreamURL,
			Detail:      "base stack externalized; local override restores cantaloupe",
		}, nil
	default:
		return Status{
			Drifted:     true,
			UpstreamURL: upstreamURL,
			Detail:      fmt.Sprintf("base_service=%t override_service=%t upstream=%s", baseHasCantaloupe, overrideHasCantaloupe, upstreamURL),
		}, nil
	}
}

func Apply(projectDir, overridePath, upstreamURL string, enabled bool) error {
	if enabled {
		if strings.TrimSpace(upstreamURL) == "" {
			return fmt.Errorf("external cantaloupe upstream URL cannot be empty")
		}
		return applyOn(projectDir, overridePath, upstreamURL)
	}
	return applyOff(projectDir, overridePath)
}

func applyOn(projectDir, overridePath, upstreamURL string) error {
	basePath := filepath.Join(projectDir, "docker-compose.yml")
	baseDoc, err := readCompose(basePath)
	if err != nil {
		return err
	}
	overrideDoc, err := readComposeOptional(overridePath)
	if err != nil {
		return err
	}
	cantaloupeSvc := getService(baseDoc, "cantaloupe")
	if cantaloupeSvc == nil {
		cantaloupeSvc = getService(overrideDoc, "cantaloupe")
		if cantaloupeSvc == nil {
			return fmt.Errorf("no cantaloupe service found in docker-compose.yml or %s", overridePath)
		}
	}
	setService(overrideDoc, "cantaloupe", cloneMap(cantaloupeSvc))
	ports := ensureStringList(ensureMap(ensureMap(overrideDoc, "services"), "cantaloupe"), "ports")
	if !containsString(ports, "8182:8182") {
		ports = append(ports, "8182:8182")
	}
	ensureMap(ensureMap(overrideDoc, "services"), "cantaloupe")["ports"] = toAnySlice(ports)
	if err := writeCompose(overridePath, overrideDoc); err != nil {
		return err
	}

	deleteService(baseDoc, "cantaloupe")
	if err := writeCompose(basePath, baseDoc); err != nil {
		return err
	}

	return replaceUpstreamURL(filepath.Join(projectDir, DefaultTraefikConfigPath), upstreamURL)
}

func applyOff(projectDir, overridePath string) error {
	basePath := filepath.Join(projectDir, "docker-compose.yml")
	baseDoc, err := readCompose(basePath)
	if err != nil {
		return err
	}
	overrideDoc, err := readComposeOptional(overridePath)
	if err != nil {
		return err
	}
	if svc := getService(overrideDoc, "cantaloupe"); svc != nil {
		restored := cloneMap(svc)
		delete(restored, "ports")
		setService(baseDoc, "cantaloupe", restored)
	}
	deleteService(overrideDoc, "cantaloupe")
	if err := writeCompose(basePath, baseDoc); err != nil {
		return err
	}
	if err := writeCompose(overridePath, overrideDoc); err != nil {
		return err
	}
	return replaceUpstreamURL(filepath.Join(projectDir, DefaultTraefikConfigPath), DefaultLocalUpstream)
}

func currentUpstreamURL(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "- url: ") {
			return strings.TrimSpace(strings.TrimPrefix(trimmed, "- url: ")), nil
		}
	}
	return "", nil
}

func replaceUpstreamURL(path, value string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "- url: ") {
			prefix := line[:len(line)-len(strings.TrimLeft(line, " "))]
			lines[i] = prefix + "- url: " + value
			return os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644)
		}
	}
	return fmt.Errorf("no Traefik cantaloupe upstream url found in %s", path)
}

func readCompose(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	doc := map[string]any{}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	return doc, nil
}

func readComposeOptional(path string) (map[string]any, error) {
	if path == "" {
		return map[string]any{}, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{}, nil
		}
		return nil, err
	}
	doc := map[string]any{}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	return doc, nil
}

func writeCompose(path string, doc map[string]any) error {
	if path == "" {
		return nil
	}
	if isEmptyDoc(doc) {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	data, err := yaml.Marshal(doc)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func isEmptyDoc(doc map[string]any) bool {
	return len(doc) == 0 || (len(doc) == 1 && len(getMap(doc, "services")) == 0)
}

func hasService(doc map[string]any, name string) bool {
	_, ok := getMap(getMap(doc, "services"), name)["image"]
	if ok {
		return true
	}
	_, ok = getMap(doc, "services")[name]
	return ok
}

func getService(doc map[string]any, name string) map[string]any {
	return getMap(getMap(doc, "services"), name)
}

func setService(doc map[string]any, name string, service map[string]any) {
	ensureMap(doc, "services")[name] = service
}

func deleteService(doc map[string]any, name string) {
	services := getMap(doc, "services")
	delete(services, name)
	if len(services) == 0 {
		delete(doc, "services")
	}
}

func getMap(doc map[string]any, key string) map[string]any {
	if doc == nil {
		return map[string]any{}
	}
	value, ok := doc[key]
	if !ok {
		return map[string]any{}
	}
	out, ok := value.(map[string]any)
	if ok {
		return out
	}
	if out2, ok := value.(map[string]interface{}); ok {
		converted := map[string]any{}
		for k, v := range out2 {
			converted[k] = v
		}
		doc[key] = converted
		return converted
	}
	return map[string]any{}
}

func ensureMap(doc map[string]any, key string) map[string]any {
	value := getMap(doc, key)
	if len(value) == 0 {
		value = map[string]any{}
		doc[key] = value
	}
	return value
}

func ensureStringList(doc map[string]any, key string) []string {
	value, ok := doc[key]
	if !ok {
		return nil
	}
	switch list := value.(type) {
	case []any:
		out := make([]string, 0, len(list))
		for _, item := range list {
			if str, ok := item.(string); ok {
				out = append(out, str)
			}
		}
		return out
	case []string:
		return append([]string{}, list...)
	default:
		return nil
	}
}

func toAnySlice(values []string) []any {
	out := make([]any, 0, len(values))
	for _, value := range values {
		out = append(out, value)
	}
	return out
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func cloneMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := map[string]any{}
	for key, value := range in {
		out[key] = cloneValue(value)
	}
	return out
}

func cloneValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneMap(typed)
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, cloneValue(item))
		}
		return out
	default:
		return typed
	}
}

func EnsureOverrideSymlink(ctx *config.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.EnsureTrackedComposeOverrideSymlink()
}

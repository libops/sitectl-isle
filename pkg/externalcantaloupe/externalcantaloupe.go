package externalcantaloupe

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	corecomponent "github.com/libops/sitectl/pkg/component"
	"github.com/libops/sitectl/pkg/config"
)

const (
	DefaultLocalUpstream     = "http://cantaloupe:8182"
	DefaultTraefikConfigPath = "conf/traefik/cantaloupe.yml"
	cantaloupeVolumeName     = "cantaloupe-data"
	traefikServiceName       = "traefik"
	traefikUpstreamEnvKey    = "CANTALOUPE_UPSTREAM_URL"
	traefikUpstreamTemplate  = `{{ env "CANTALOUPE_UPSTREAM_URL" }}`
)

type Status struct {
	Enabled     bool
	Drifted     bool
	UpstreamURL string
	Detail      string
}

func Detect(projectDir, overridePath string) (Status, error) {
	baseComposePath := filepath.Join(projectDir, "docker-compose.yml")
	baseCompose, err := corecomponent.LoadComposeFile(baseComposePath)
	if err != nil {
		return Status{}, err
	}
	baseHasCantaloupe := baseCompose.HasService("cantaloupe")

	overrideCompose, err := corecomponent.LoadComposeFileOptional(overridePath)
	if err != nil {
		return Status{}, err
	}
	overrideHasCantaloupe := overrideCompose.HasService("cantaloupe")

	upstreamURL, err := currentUpstreamURL(baseCompose, overrideCompose, filepath.Join(projectDir, DefaultTraefikConfigPath))
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
		if err := validateUpstreamURL(upstreamURL); err != nil {
			return err
		}
		return applyOn(projectDir, overridePath, upstreamURL)
	}
	return applyOff(projectDir, overridePath)
}

func validateUpstreamURL(value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fmt.Errorf("external cantaloupe upstream URL cannot be empty")
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return fmt.Errorf("invalid external cantaloupe upstream URL %q: %w", trimmed, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("invalid external cantaloupe upstream URL %q: scheme must be http or https", trimmed)
	}
	if parsed.Host == "" {
		return fmt.Errorf("invalid external cantaloupe upstream URL %q: host is required", trimmed)
	}
	return nil
}

func applyOn(projectDir, overridePath, upstreamURL string) error {
	basePath := filepath.Join(projectDir, "docker-compose.yml")
	overrideCompose, err := corecomponent.LoadComposeFileOptional(overridePath)
	if err != nil {
		return err
	}
	baseCompose, err := corecomponent.LoadComposeFile(basePath)
	if err != nil {
		return err
	}
	if err := ensureServiceEnv(overrideCompose, traefikServiceName, traefikUpstreamEnvKey, DefaultLocalUpstream); err != nil {
		return err
	}
	if !baseCompose.HasService("cantaloupe") {
		serviceBlock, err := extractServiceBlock(overridePath, "cantaloupe")
		if err != nil {
			return err
		}
		if serviceBlock == "" {
			return fmt.Errorf("no cantaloupe service found in docker-compose.yml or %s", overridePath)
		}
		if err := overrideCompose.AddServiceBlock("cantaloupe", serviceBlock); err != nil {
			return err
		}
		if err := overrideCompose.SetServiceStringList("cantaloupe", "ports", []string{"8182:8182"}); err != nil {
			return err
		}
		if err := overrideCompose.Save(); err != nil {
			return err
		}
	} else {
		serviceBlock, ok := baseCompose.ServiceBlock("cantaloupe")
		if !ok {
			return fmt.Errorf("no cantaloupe service block found in %s", basePath)
		}
		if err := overrideCompose.AddServiceBlock("cantaloupe", serviceBlock); err != nil {
			return err
		}
		if err := overrideCompose.SetServiceStringList("cantaloupe", "ports", []string{"8182:8182"}); err != nil {
			return err
		}
		if err := overrideCompose.Save(); err != nil {
			return err
		}
	}
	if err := copyVolumeDefinition(basePath, overridePath, cantaloupeVolumeName); err != nil {
		return err
	}
	if err := ensureServiceEnv(baseCompose, traefikServiceName, traefikUpstreamEnvKey, strings.TrimSpace(upstreamURL)); err != nil {
		return err
	}

	if err := baseCompose.DeleteService("cantaloupe"); err != nil {
		return err
	}
	if err := deleteVolumeDefinition(basePath, cantaloupeVolumeName); err != nil {
		return err
	}
	if err := baseCompose.DeleteVolume(cantaloupeVolumeName); err != nil {
		return err
	}
	if err := baseCompose.Save(); err != nil {
		return err
	}

	return ensureUpstreamURLTemplate(filepath.Join(projectDir, DefaultTraefikConfigPath))
}

func applyOff(projectDir, overridePath string) error {
	basePath := filepath.Join(projectDir, "docker-compose.yml")
	overrideCompose, err := corecomponent.LoadComposeFileOptional(overridePath)
	if err != nil {
		return err
	}
	baseCompose, err := corecomponent.LoadComposeFile(basePath)
	if err != nil {
		return err
	}
	if err := ensureServiceEnv(baseCompose, traefikServiceName, traefikUpstreamEnvKey, DefaultLocalUpstream); err != nil {
		return err
	}
	if overrideCompose.HasService("cantaloupe") {
		serviceBlock, err := extractServiceBlock(overridePath, "cantaloupe")
		if err != nil {
			return err
		}
		if serviceBlock == "" {
			return fmt.Errorf("no cantaloupe service block found in %s", overridePath)
		}
		if err := baseCompose.AddServiceBlock("cantaloupe", stripServicePorts(serviceBlock)); err != nil {
			return err
		}
		if err := baseCompose.Save(); err != nil {
			return err
		}
	}
	if err := copyVolumeDefinition(overridePath, basePath, cantaloupeVolumeName); err != nil {
		return err
	}
	if err := overrideCompose.DeleteServiceEnv(traefikServiceName, traefikUpstreamEnvKey); err != nil {
		return err
	}
	if err := overrideCompose.DeleteService("cantaloupe"); err != nil {
		return err
	}
	if err := deleteVolumeDefinition(overridePath, cantaloupeVolumeName); err != nil {
		return err
	}
	if err := overrideCompose.Save(); err != nil {
		return err
	}
	return ensureUpstreamURLTemplate(filepath.Join(projectDir, DefaultTraefikConfigPath))
}

func copyVolumeDefinition(sourcePath, targetPath, volumeName string) error {
	sourceCompose, err := corecomponent.LoadComposeFileOptional(sourcePath)
	if err != nil {
		return err
	}
	block, ok := sourceCompose.SectionEntryBlock("volumes", volumeName)
	if !ok {
		return nil
	}
	targetCompose, err := corecomponent.LoadComposeFileOptional(targetPath)
	if err != nil {
		return err
	}
	if err := targetCompose.AddSectionEntryBlock("volumes", volumeName, block); err != nil {
		return err
	}
	return targetCompose.Save()
}

func deleteVolumeDefinition(path, volumeName string) error {
	compose, err := corecomponent.LoadComposeFileOptional(path)
	if err != nil {
		return err
	}
	if err := compose.DeleteSectionEntry("volumes", volumeName); err != nil {
		return err
	}
	return compose.Save()
}

func extractServiceBlock(path, name string) (string, error) {
	compose, err := corecomponent.LoadComposeFile(path)
	if err != nil {
		return "", err
	}
	block, ok := compose.ServiceBlock(name)
	if !ok {
		return "", nil
	}
	return block, nil
}

func stripServicePorts(serviceBlock string) string {
	lines := strings.Split(serviceBlock, "\n")
	filtered := make([]string, 0, len(lines))
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if strings.TrimSpace(line) != "ports:" {
			filtered = append(filtered, line)
			continue
		}
		filtered = removeTrailingBlankLines(filtered)
		for i+1 < len(lines) {
			next := lines[i+1]
			if strings.TrimSpace(next) == "" {
				i++
				continue
			}
			if leadingSpaces(next) <= 4 {
				break
			}
			i++
		}
	}
	return strings.Join(filtered, "\n")
}

func removeTrailingBlankLines(lines []string) []string {
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func currentUpstreamURL(baseCompose, overrideCompose *corecomponent.ComposeFile, path string) (string, error) {
	if value := composeServiceEnvValue(overrideCompose, traefikServiceName, traefikUpstreamEnvKey); strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value), nil
	}
	if value := composeServiceEnvValue(baseCompose, traefikServiceName, traefikUpstreamEnvKey); strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value), nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "- url: ") {
			value := strings.TrimSpace(strings.TrimPrefix(trimmed, "- url: "))
			if value == traefikUpstreamTemplate {
				return DefaultLocalUpstream, nil
			}
			return value, nil
		}
	}
	return "", nil
}

func ensureUpstreamURLTemplate(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "- url: ") {
			prefix := line[:len(line)-len(strings.TrimLeft(line, " "))]
			lines[i] = prefix + "- url: " + traefikUpstreamTemplate
			return os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644)
		}
	}
	return fmt.Errorf("no Traefik cantaloupe upstream url found in %s", path)
}

func ensureServiceEnv(compose *corecomponent.ComposeFile, service, key, value string) error {
	if !compose.HasService(service) {
		if err := compose.AddServiceBlock(service, "  "+service+":"); err != nil {
			return err
		}
	}
	return compose.SetServiceEnv(service, key, value)
}

func composeServiceEnvValue(compose *corecomponent.ComposeFile, service, key string) string {
	if compose == nil {
		return ""
	}
	block, ok := compose.ServiceBlock(service)
	if !ok {
		return ""
	}
	lines := strings.Split(block, "\n")
	envIdx, envStyle, ok := findEnvironmentBlock(lines, 0)
	if !ok {
		return ""
	}
	if envStyle == envInlineEmpty {
		return ""
	}
	keyIdx, ok := findMapKey(lines, envIdx+1, key, 6)
	if !ok {
		return ""
	}
	line := strings.TrimSpace(lines[keyIdx])
	parts := strings.SplitN(line, ":", 2)
	if len(parts) != 2 {
		return ""
	}
	return strings.Trim(strings.TrimSpace(parts[1]), `"`)
}

type envBlockStyle int

const (
	envBlock envBlockStyle = iota
	envInlineEmpty
)

func findEnvironmentBlock(lines []string, serviceIdx int) (int, envBlockStyle, bool) {
	serviceEnd := findBlockEnd(lines, serviceIdx, 2)
	for i := serviceIdx + 1; i < serviceEnd; i++ {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "environment:" {
			return i, envBlock, true
		}
		if trimmed == "environment: {}" {
			return i, envInlineEmpty, true
		}
	}
	return 0, envBlock, false
}

func findMapKey(lines []string, start int, key string, indent int) (int, bool) {
	prefix := strings.Repeat(" ", indent) + key + ":"
	for i := start; i < len(lines); i++ {
		line := lines[i]
		if strings.TrimSpace(line) == "" {
			continue
		}
		currentIndent := leadingSpaces(line)
		if currentIndent < indent {
			break
		}
		if currentIndent == indent && strings.HasPrefix(line, prefix) {
			return i, true
		}
	}
	return 0, false
}

func findBlockEnd(lines []string, start int, indent int) int {
	for i := start + 1; i < len(lines); i++ {
		line := lines[i]
		if strings.TrimSpace(line) == "" {
			continue
		}
		if leadingSpaces(line) <= indent {
			return i
		}
	}
	return len(lines)
}

func leadingSpaces(line string) int {
	return len(line) - len(strings.TrimLeft(line, " "))
}

func EnsureOverrideSymlink(ctx *config.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.EnsureTrackedComposeOverrideSymlink()
}

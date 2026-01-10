package cmd

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var migrateLegacyCmd = &cobra.Command{
	Use:   "migrate-legacy",
	Short: "Migrate legacy ISLE docker-compose files to unified format",
	Long: `Migrate legacy ISLE docker-compose files to the unified Islandora compose format.

Supports migration from:
- isle-dc format (prefixed volumes, gateway network, absolute paths)
- isle-site-template format (-dev/-prod service pairs)

The tool will detect the format automatically and apply the appropriate transformations.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		inputPath, _ := cmd.Flags().GetString("input")
		outputPath, _ := cmd.Flags().GetString("output")
		force, _ := cmd.Flags().GetBool("force")

		return migrateLegacy(inputPath, outputPath, force)
	},
}

func init() {
	migrateLegacyCmd.Flags().StringP("input", "i", "docker-compose.yml", "Input docker-compose file path")
	migrateLegacyCmd.Flags().StringP("output", "o", "docker-compose.migrated.yml", "Output docker-compose file path")
	migrateLegacyCmd.Flags().BoolP("force", "f", false, "Overwrite output file if it exists")
}

func migrateLegacy(inputPath, outputPath string, force bool) error {
	// Check if input exists
	if _, err := os.Stat(inputPath); err != nil {
		// Try alternative name
		altPath := filepath.Join(filepath.Dir(inputPath), "compose.yaml")
		if _, err := os.Stat(altPath); err == nil {
			inputPath = altPath
		} else {
			return fmt.Errorf("input file not found: %s", inputPath)
		}
	}

	// Check if output exists
	if !force {
		if _, err := os.Stat(outputPath); err == nil {
			return fmt.Errorf("output file already exists: %s (use --force to overwrite)", outputPath)
		}
	}

	slog.Info("Reading input file", "path", inputPath)

	// Read input
	data, err := os.ReadFile(inputPath)
	if err != nil {
		return fmt.Errorf("error reading input file: %w", err)
	}

	// Parse YAML
	var compose map[string]interface{}
	if err := yaml.Unmarshal(data, &compose); err != nil {
		return fmt.Errorf("error parsing YAML: %w", err)
	}

	// Detect format
	format := detectFormat(compose)
	slog.Info("Detected format", "format", format)

	// Transform based on format
	var transformed map[string]interface{}
	var warnings []string

	switch format {
	case "isle-dc":
		transformed, warnings = transformISLEDC(compose)
	case "isle-site-template":
		transformed, warnings = transformISLESiteTemplate(compose)
	default:
		return fmt.Errorf("unsupported or already current format: %s", format)
	}

	// Generate header
	header := generateHeader(format, warnings)

	// Marshal back to YAML
	output, err := yaml.Marshal(transformed)
	if err != nil {
		return fmt.Errorf("error marshaling YAML: %w", err)
	}

	// Combine header and output
	finalOutput := header + "\n" + string(output)

	// Write output
	if err := os.WriteFile(outputPath, []byte(finalOutput), 0644); err != nil {
		return fmt.Errorf("error writing output file: %w", err)
	}

	slog.Info("Transformation complete", "output", outputPath)
	fmt.Printf("\n✓ Successfully migrated from %s format\n", format)
	fmt.Printf("  Input:  %s\n", inputPath)
	fmt.Printf("  Output: %s\n\n", outputPath)

	if len(warnings) > 0 {
		fmt.Println("Warnings:")
		for _, w := range warnings {
			fmt.Printf("  - %s\n", w)
		}
		fmt.Println()
	}

	fmt.Println("Next steps:")
	fmt.Println("  1. Review the generated file")
	fmt.Println("  2. Create/update your .env file with required variables")
	fmt.Println("  3. Run: docker compose -f", outputPath, "config")
	fmt.Println("  4. When ready: docker compose -f", outputPath, "up -d")

	return nil
}

func detectFormat(compose map[string]interface{}) string {
	services, _ := compose["services"].(map[string]interface{})

	// Check for isle-site-template (-dev/-prod pattern)
	devProdCount := 0
	for name := range services {
		if strings.HasSuffix(name, "-dev") || strings.HasSuffix(name, "-prod") {
			devProdCount++
		}
	}
	if devProdCount >= 3 {
		return "isle-site-template"
	}

	// Check for isle-dc (prefixed volumes, gateway network, absolute paths)
	volumes, _ := compose["volumes"].(map[string]interface{})
	prefixedVolumes := 0
	for name := range volumes {
		if strings.HasPrefix(name, "isle-dc_") || strings.HasPrefix(name, "isle_dc_") {
			prefixedVolumes++
		}
	}

	networks, _ := compose["networks"].(map[string]interface{})
	_, hasGateway := networks["gateway"]

	// Check for absolute paths
	hasAbsolutePaths := false
	for _, svc := range services {
		if svcMap, ok := svc.(map[string]interface{}); ok {
			if vols, ok := svcMap["volumes"].([]interface{}); ok {
				for _, vol := range vols {
					if volStr, ok := vol.(string); ok && strings.HasPrefix(volStr, "/") {
						parts := strings.Split(volStr, ":")
						if len(parts) >= 2 && strings.HasPrefix(parts[0], "/") {
							hasAbsolutePaths = true
							break
						}
					}
				}
			}
		}
	}

	if prefixedVolumes >= 3 || hasGateway || hasAbsolutePaths {
		return "isle-dc"
	}

	return "current"
}

func transformISLEDC(compose map[string]interface{}) (map[string]interface{}, []string) {
	warnings := []string{}
	result := make(map[string]interface{})

	projectPrefix := detectProjectPrefix(compose)

	// Transform name
	if name, ok := compose["name"].(string); ok {
		result["name"] = stripPrefix(name, projectPrefix)
	} else {
		result["name"] = "islandora"
	}

	// Transform services
	if services, ok := compose["services"].(map[string]interface{}); ok {
		transformed := make(map[string]interface{})
		for name, svc := range services {
			newName := stripPrefix(name, projectPrefix)
			transformed[newName] = transformService(svc, projectPrefix)
		}
		result["services"] = transformed
	}

	// Transform networks (remove gateway)
	if networks, ok := compose["networks"].(map[string]interface{}); ok {
		transformed := make(map[string]interface{})
		for name, net := range networks {
			if name != "gateway" && !strings.Contains(name, "gateway") {
				newName := stripPrefix(name, projectPrefix)
				transformed[newName] = net
			}
		}
		if len(transformed) > 0 {
			result["networks"] = transformed
		}
	}

	// Transform volumes
	if volumes, ok := compose["volumes"].(map[string]interface{}); ok {
		transformed := make(map[string]interface{})
		for name, vol := range volumes {
			newName := stripPrefix(name, projectPrefix)
			transformed[newName] = vol
		}
		result["volumes"] = transformed
	}

	// Transform secrets
	if secrets, ok := compose["secrets"].(map[string]interface{}); ok {
		transformed := make(map[string]interface{})
		for name, secret := range secrets {
			newName := stripPrefix(name, projectPrefix)
			transformed[newName] = transformSecret(secret, projectPrefix)
		}
		result["secrets"] = transformed
	}

	// Transform configs
	if configs, ok := compose["configs"].(map[string]interface{}); ok {
		transformed := make(map[string]interface{})
		for name, config := range configs {
			newName := stripPrefix(name, projectPrefix)
			transformed[newName] = transformConfig(config, projectPrefix)
		}
		result["configs"] = transformed
	}

	return result, warnings
}

func transformService(svc interface{}, projectPrefix string) map[string]interface{} {
	svcMap, ok := svc.(map[string]interface{})
	if !ok {
		return map[string]interface{}{}
	}

	result := make(map[string]interface{})

	for key, val := range svcMap {
		switch key {
		case "container_name":
			if str, ok := val.(string); ok {
				result[key] = stripPrefix(str, projectPrefix)
			}
		case "image":
			if str, ok := val.(string); ok {
				result[key] = replaceEnvVars(str)
			}
		case "environment":
			result[key] = transformEnvironment(val)
		case "volumes":
			result[key] = transformVolumes(val, projectPrefix)
		case "networks":
			result[key] = transformServiceNetworks(val, projectPrefix)
		case "secrets":
			result[key] = transformServiceSecrets(val, projectPrefix)
		case "depends_on":
			result[key] = transformDependsOn(val, projectPrefix)
		case "labels":
			result[key] = transformLabels(val)
		default:
			result[key] = val
		}
	}

	return result
}

func transformEnvironment(env interface{}) interface{} {
	switch e := env.(type) {
	case map[string]interface{}:
		result := make(map[string]interface{})
		for k, v := range e {
			if str, ok := v.(string); ok {
				result[k] = replaceEnvVars(str)
			} else {
				result[k] = v
			}
		}
		return result
	case []interface{}:
		result := make([]interface{}, len(e))
		for i, item := range e {
			if str, ok := item.(string); ok {
				result[i] = replaceEnvVars(str)
			} else {
				result[i] = item
			}
		}
		return result
	}
	return env
}

func transformVolumes(vols interface{}, projectPrefix string) interface{} {
	volList, ok := vols.([]interface{})
	if !ok {
		return vols
	}

	result := make([]interface{}, 0, len(volList))
	for _, vol := range volList {
		switch v := vol.(type) {
		case string:
			result = append(result, transformVolumeString(v, projectPrefix))
		case map[string]interface{}:
			if source, ok := v["source"].(string); ok {
				v["source"] = stripPrefix(source, projectPrefix)
			}
			result = append(result, v)
		default:
			result = append(result, vol)
		}
	}
	return result
}

func transformVolumeString(vol, projectPrefix string) string {
	parts := strings.Split(vol, ":")
	if len(parts) >= 2 {
		source := normalizePath(parts[0], projectPrefix)
		if len(parts) == 2 {
			return source + ":" + parts[1]
		} else {
			return source + ":" + parts[1] + ":" + parts[2]
		}
	}
	return vol
}

func transformServiceNetworks(nets interface{}, projectPrefix string) interface{} {
	switch n := nets.(type) {
	case []interface{}:
		result := make([]interface{}, 0)
		for _, net := range n {
			if str, ok := net.(string); ok {
				if str != "gateway" && !strings.Contains(str, "gateway") {
					result = append(result, stripPrefix(str, projectPrefix))
				}
			}
		}
		return result
	case map[string]interface{}:
		result := make(map[string]interface{})
		for name, config := range n {
			if name != "gateway" && !strings.Contains(name, "gateway") {
				result[stripPrefix(name, projectPrefix)] = config
			}
		}
		return result
	}
	return nets
}

func transformServiceSecrets(secrets interface{}, projectPrefix string) interface{} {
	secList, ok := secrets.([]interface{})
	if !ok {
		return secrets
	}

	result := make([]interface{}, 0, len(secList))
	for _, sec := range secList {
		switch s := sec.(type) {
		case string:
			result = append(result, stripPrefix(s, projectPrefix))
		case map[string]interface{}:
			if source, ok := s["source"].(string); ok {
				s["source"] = stripPrefix(source, projectPrefix)
			}
			result = append(result, s)
		default:
			result = append(result, sec)
		}
	}
	return result
}

func transformDependsOn(deps interface{}, projectPrefix string) interface{} {
	switch d := deps.(type) {
	case []interface{}:
		result := make([]interface{}, len(d))
		for i, dep := range d {
			if str, ok := dep.(string); ok {
				result[i] = stripPrefix(str, projectPrefix)
			} else {
				result[i] = dep
			}
		}
		return result
	case map[string]interface{}:
		result := make(map[string]interface{})
		for name, config := range d {
			result[stripPrefix(name, projectPrefix)] = config
		}
		return result
	}
	return deps
}

func transformLabels(labels interface{}) interface{} {
	labelMap, ok := labels.(map[string]interface{})
	if !ok {
		return labels
	}

	result := make(map[string]interface{})
	for k, v := range labelMap {
		if str, ok := v.(string); ok {
			result[k] = replaceEnvVars(str)
		} else {
			result[k] = v
		}
	}
	return result
}

func transformSecret(secret interface{}, projectPrefix string) interface{} {
	secMap, ok := secret.(map[string]interface{})
	if !ok {
		return secret
	}

	result := make(map[string]interface{})
	for k, v := range secMap {
		if k == "file" {
			if str, ok := v.(string); ok {
				result[k] = normalizePath(str, projectPrefix)
			} else {
				result[k] = v
			}
		} else {
			result[k] = v
		}
	}
	return result
}

func transformConfig(config interface{}, projectPrefix string) interface{} {
	cfgMap, ok := config.(map[string]interface{})
	if !ok {
		return config
	}

	result := make(map[string]interface{})
	for k, v := range cfgMap {
		if k == "file" {
			if str, ok := v.(string); ok {
				result[k] = normalizePath(str, projectPrefix)
			} else {
				result[k] = v
			}
		} else {
			result[k] = v
		}
	}
	return result
}

func transformISLESiteTemplate(compose map[string]interface{}) (map[string]interface{}, []string) {
	warnings := []string{}
	result := make(map[string]interface{})

	// Copy name
	if name, ok := compose["name"].(string); ok {
		result["name"] = name
	} else {
		result["name"] = "islandora"
	}

	// Merge services
	if services, ok := compose["services"].(map[string]interface{}); ok {
		result["services"] = mergeSiteTemplateServices(services)
	}

	// Copy networks (no transformation needed)
	if networks, ok := compose["networks"].(map[string]interface{}); ok {
		result["networks"] = networks
	}

	// Copy volumes (no transformation needed)
	if volumes, ok := compose["volumes"].(map[string]interface{}); ok {
		result["volumes"] = volumes
	}

	// Consolidate secrets (remove duplicates from -dev/-prod)
	if secrets, ok := compose["secrets"].(map[string]interface{}); ok {
		result["secrets"] = consolidateSecrets(secrets)
	}

	// Copy configs
	if configs, ok := compose["configs"].(map[string]interface{}); ok {
		result["configs"] = configs
	}

	return result, warnings
}

func mergeSiteTemplateServices(services map[string]interface{}) map[string]interface{} {
	merged := make(map[string]interface{})
	processed := make(map[string]bool)

	for name := range services {
		// Get base name without -dev/-prod suffix
		baseName := strings.TrimSuffix(strings.TrimSuffix(name, "-dev"), "-prod")

		if processed[baseName] {
			continue
		}

		devName := baseName + "-dev"
		prodName := baseName + "-prod"

		devSvc, hasDev := services[devName]
		prodSvc, hasProd := services[prodName]

		if hasDev && hasProd {
			// Merge dev and prod into single service
			merged[baseName] = mergeDevProdService(devSvc, prodSvc)
			processed[baseName] = true
		} else if hasDev {
			// Only dev exists, use it without suffix
			merged[baseName] = devSvc
			processed[baseName] = true
		} else if hasProd {
			// Only prod exists, use it without suffix
			merged[baseName] = prodSvc
			processed[baseName] = true
		} else if !strings.HasSuffix(name, "-dev") && !strings.HasSuffix(name, "-prod") {
			// Service without suffix, keep as-is
			merged[name] = services[name]
			processed[name] = true
		}
	}

	return merged
}

func mergeDevProdService(dev, prod interface{}) map[string]interface{} {
	// Use prod as base
	prodMap, ok := prod.(map[string]interface{})
	if !ok {
		return make(map[string]interface{})
	}

	result := make(map[string]interface{})

	// Copy all prod settings
	for k, v := range prodMap {
		result[k] = v
	}

	// Remove profile if present
	delete(result, "profiles")

	return result
}

func consolidateSecrets(secrets map[string]interface{}) map[string]interface{} {
	consolidated := make(map[string]interface{})
	processed := make(map[string]bool)

	for name, secret := range secrets {
		// Get base name without -dev/-prod suffix
		baseName := strings.TrimSuffix(strings.TrimSuffix(name, "-dev"), "-prod")

		if processed[baseName] {
			continue
		}

		// Prefer prod version if both exist, otherwise use what's available
		if prodSecret, ok := secrets[baseName+"-prod"]; ok {
			consolidated[baseName] = prodSecret
			processed[baseName] = true
		} else if devSecret, ok := secrets[baseName+"-dev"]; ok {
			consolidated[baseName] = devSecret
			processed[baseName] = true
		} else {
			// No suffix version
			consolidated[name] = secret
			processed[name] = true
		}
	}

	return consolidated
}

func stripPrefix(s, prefix string) string {
	// Keep secret names intact - only strip project prefix at the beginning
	if strings.HasPrefix(s, prefix+"_") {
		return strings.TrimPrefix(s, prefix+"_")
	}
	if strings.HasPrefix(s, prefix+"-") {
		return strings.TrimPrefix(s, prefix+"-")
	}
	return s
}

func normalizePath(path, projectPrefix string) string {
	path = strings.TrimSpace(path)

	// Common absolute path patterns - convert to relative
	patterns := map[string]string{
		`^/Users/[^/]+/` + projectPrefix:   ".",
		`^/home/[^/]+/` + projectPrefix:    ".",
		`^/opt/` + projectPrefix:           ".",
		`^/var/www/` + projectPrefix:       ".",
		`^/workspace/` + projectPrefix:     ".",
	}

	for pattern, replacement := range patterns {
		re := regexp.MustCompile(pattern)
		if re.MatchString(path) {
			path = re.ReplaceAllString(path, replacement)
			break
		}
	}

	// Remove /live/ from secrets paths
	if strings.Contains(path, "secrets/live/") {
		path = strings.ReplaceAll(path, "secrets/live/", "secrets/")
	}
	if strings.Contains(path, "secrets\\live\\") {
		path = strings.ReplaceAll(path, "secrets\\live\\", "secrets/")
	}

	return path
}

func replaceEnvVars(s string) string {
	domains := []string{
		"islandora.dev",
		"islandora.local",
		"isle.localdomain",
		"islandora.traefik.me",
	}

	for _, domain := range domains {
		s = strings.ReplaceAll(s, domain, "${DOMAIN}")
	}

	return s
}

func detectProjectPrefix(compose map[string]interface{}) string {
	// Try to detect from volumes
	if volumes, ok := compose["volumes"].(map[string]interface{}); ok {
		for name := range volumes {
			if strings.Contains(name, "_") {
				return strings.Split(name, "_")[0]
			}
		}
	}

	// Try from name
	if name, ok := compose["name"].(string); ok {
		if strings.Contains(name, "_") {
			return strings.Split(name, "_")[0]
		}
		if strings.Contains(name, "-") {
			return strings.Split(name, "-")[0]
		}
	}

	return "isle-dc"
}

func generateHeader(format string, warnings []string) string {
	tmplStr := `# ============================================================================
# GENERATED BY COMPOSE BRIDGE - ISLANDORA MIGRATION
# ============================================================================
#
# Source Format: {{ .Format }}
# Target Format: Unified Islandora Compose
#
# CHANGES APPLIED:
{{- if eq .Format "isle-dc" }}
# - Removed 'isle-dc_' prefixes from volumes, networks, and secrets
# - Converted absolute paths to relative paths (e.g., ./codebase)
# - Replaced hardcoded domains with ${DOMAIN} environment variable
# - Removed gateway network (now using single default network)
# - Updated Traefik labels for new network structure
# - Converted secret paths: ./secrets/live/SECRET -> ./secrets/SECRET
{{- else if eq .Format "isle-site-template" }}
# - Merged -dev and -prod service pairs into single services
# - Removed profile system (use DEVELOPMENT_ENVIRONMENT=true/false)
# - Consolidated duplicate secrets
{{- end }}
#
# REQUIRED ACTIONS:
# 1. Create a .env file with required variables (see .env.example)
{{- if eq .Format "isle-dc" }}
# 2. Move secrets from secrets/live/ to secrets/ directory, or
#    update secret paths in this file to match your structure
{{- end }}
# 3. Review and update any absolute paths in bind mounts
# 4. Run: docker compose config (to validate)
# 5. Run: docker compose up -d (when ready)
#
{{- if .Warnings }}
# WARNINGS:
{{- range .Warnings }}
#   - {{ . }}
{{- end }}
#
{{- end }}
# ============================================================================`

	tmpl := template.Must(template.New("header").Parse(tmplStr))

	var buf bytes.Buffer
	tmpl.Execute(&buf, map[string]interface{}{
		"Format":   format,
		"Warnings": warnings,
	})

	return buf.String()
}

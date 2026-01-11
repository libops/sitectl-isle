package cmd

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

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
	migrateLegacyCmd.Flags().StringP("output", "o", "docker-compose.yml", "Output docker-compose file path")
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

	// Build custom YAML output with proper anchors
	finalOutput, err := buildYAMLOutput(transformed)
	if err != nil {
		return fmt.Errorf("error building YAML output: %w", err)
	}

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

	// Add x-common anchor - order matters for output
	xCommon := map[string]interface{}{
		"restart": "unless-stopped",
		"tty":     true,
		"security_opt": []interface{}{
			"label=type:container_runtime_t",
		},
		"networks": map[string]interface{}{
			"default": interface{}(nil),
		},
	}
	result["x-common"] = xCommon

	// Transform networks (remove gateway, keep only default)
	result["networks"] = map[string]interface{}{
		"default": map[string]interface{}{},
	}

	// Transform volumes - ensure acme-data is included
	if volumes, ok := compose["volumes"].(map[string]interface{}); ok {
		transformed := make(map[string]interface{})
		for name := range volumes {
			newName := stripPrefix(name, projectPrefix)
			transformed[newName] = map[string]interface{}{}
		}
		// Add acme-data for traefik
		transformed["acme-data"] = map[string]interface{}{}
		result["volumes"] = transformed
	}

	// Transform secrets - add required dev secrets
	if secrets, ok := compose["secrets"].(map[string]interface{}); ok {
		transformed := make(map[string]interface{})
		for name, secret := range secrets {
			newName := stripPrefix(name, projectPrefix)
			transformed[newName] = transformSecret(secret, projectPrefix)
		}
		// Ensure dev secrets exist
		ensureDevSecrets(transformed)
		result["secrets"] = transformed
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

	// Add reference to x-common anchor
	result["<<"] = "*common"

	for key, val := range svcMap {
		switch key {
		case "container_name":
			// Skip container_name - not used in target format
			continue
		case "restart", "tty", "security_opt":
			// Skip these - they're in x-common
			continue
		case "networks":
			// Skip networks if it's just default - covered by x-common
			if isJustDefaultNetwork(val) {
				continue
			}
			result[key] = transformServiceNetworks(val, projectPrefix)
		case "image":
			if str, ok := val.(string); ok {
				result[key] = replaceEnvVars(str)
			}
		case "environment":
			result[key] = transformEnvironment(val)
		case "volumes":
			result[key] = transformVolumes(val, projectPrefix)
		case "secrets":
			result[key] = transformServiceSecrets(val, projectPrefix)
		case "depends_on":
			result[key] = transformDependsOn(val, projectPrefix)
		case "labels":
			// Skip labels entirely - traefik config is now in ./conf/traefik
			continue
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
			// Create a new map to avoid modifying the original
			newVol := make(map[string]interface{})
			for k, val := range v {
				if k == "source" {
					if source, ok := val.(string); ok {
						// Normalize path for bind mounts
						newVol[k] = normalizePath(stripPrefix(source, projectPrefix), projectPrefix)
					} else {
						newVol[k] = val
					}
				} else {
					newVol[k] = val
				}
			}
			result = append(result, newVol)
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
		result := make([]interface{}, 0)
		for _, dep := range d {
			if str, ok := dep.(string); ok {
				stripped := stripPrefix(str, projectPrefix)
				// Skip traefik dependency - not used in target format
				if stripped != "traefik" {
					result = append(result, stripped)
				}
			} else {
				result = append(result, dep)
			}
		}
		return result
	case map[string]interface{}:
		result := make(map[string]interface{})
		for name, config := range d {
			stripped := stripPrefix(name, projectPrefix)
			// Skip traefik dependency - not used in target format
			if stripped != "traefik" {
				result[stripped] = config
			}
		}
		return result
	}
	return deps
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
		} else if k == "name" {
			// Skip name field - not needed in target format
			continue
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

	// Add x-common anchor - order matters for output
	xCommon := map[string]interface{}{
		"restart": "unless-stopped",
		"tty":     true,
		"security_opt": []interface{}{
			"label=type:container_runtime_t",
		},
		"networks": map[string]interface{}{
			"default": interface{}(nil),
		},
	}
	result["x-common"] = xCommon

	// Transform networks - keep only default
	result["networks"] = map[string]interface{}{
		"default": map[string]interface{}{},
	}

	// Transform volumes - add acme-data if not present
	if volumes, ok := compose["volumes"].(map[string]interface{}); ok {
		transformed := make(map[string]interface{})
		for name := range volumes {
			transformed[name] = map[string]interface{}{}
		}
		// Ensure acme-data exists
		transformed["acme-data"] = map[string]interface{}{}
		result["volumes"] = transformed
	}

	// Consolidate secrets
	if secrets, ok := compose["secrets"].(map[string]interface{}); ok {
		consolidated := consolidateSecrets(secrets)
		// Ensure dev secrets exist
		ensureDevSecrets(consolidated)
		result["secrets"] = consolidated
	}

	// Merge services
	if services, ok := compose["services"].(map[string]interface{}); ok {
		merged := mergeSiteTemplateServices(services)

		// Add init service if missing (it's standard in unified format)
		if _, exists := merged["init"]; !exists {
			merged["init"] = map[string]interface{}{
				"image":   "islandora/base:${ISLANDORA_TAG}",
				"restart": "no",
				"networks": map[string]interface{}{
					"default": interface{}(nil),
				},
				"volumes": []interface{}{
					"./.env:/.env",
					"./certs:/certs:rw",
					"./secrets:/secrets:rw",
					"./scripts:/scripts:ro",
					"./docker-compose.yml:/docker-compose.yml:ro",
					"./drupal:/drupal:rw",
					"/var/run/docker.sock:/var/run/docker.sock:z",
				},
				"security_opt": []interface{}{
					"label=type:container_runtime_t",
				},
				"entrypoint": "/scripts/init-entrypoint.sh",
				"profiles":   []interface{}{"none"},
			}
		}

		result["services"] = merged
	}

	return result, warnings
}

func mergeSiteTemplateServices(services map[string]interface{}) map[string]interface{} {
	merged := make(map[string]interface{})
	processed := make(map[string]bool)

	// Use specific order to match target
	targetOrder := []string{"init", "alpaca", "crayfits", "fits", "homarus", "houdini", "hypercube", "mariadb", "milliner", "activemq", "blazegraph", "cantaloupe", "drupal", "fcrepo", "solr", "traefik"}

	for _, baseName := range targetOrder {
		devName := baseName + "-dev"
		prodName := baseName + "-prod"

		devSvc, hasDev := services[devName]
		prodSvc, hasProd := services[prodName]
		baseSvc, hasBase := services[baseName]

		if hasDev && hasProd {
			merged[baseName] = mergeDevProdService(baseName, devSvc, prodSvc)
			processed[baseName] = true
			processed[devName] = true
			processed[prodName] = true
		} else if hasBase {
			merged[baseName] = transformService(baseSvc, "")
			processed[baseName] = true
		}
	}

	// Catch any remaining services
	for name, svc := range services {
		if !processed[name] {
			baseName := strings.TrimSuffix(strings.TrimSuffix(name, "-dev"), "-prod")
			if !processed[baseName] {
				merged[baseName] = transformService(svc, "")
				processed[baseName] = true
			}
			processed[name] = true
		}
	}

	return merged
}

func mergeDevProdService(name string, dev, prod interface{}) map[string]interface{} {
	// Use prod as base
	prodMap, ok := prod.(map[string]interface{})
	if !ok {
		return make(map[string]interface{})
	}

	devMap, ok := dev.(map[string]interface{})
	if !ok {
		return make(map[string]interface{})
	}

	result := make(map[string]interface{})

	// Add reference to x-common anchor
	result["<<"] = "*common"

	// Copy all prod settings except common fields
	for k, v := range prodMap {
		switch k {
		case "profiles", "restart", "tty", "security_opt", "<<", "labels":
			continue
		case "networks":
			// Skip if just default - it's covered by x-common
			if isJustDefaultNetwork(v) {
				continue
			}
			result[k] = v
		case "depends_on":
			result[k] = stripDevProdFromDependsOn(v)
		case "image":
			if str, ok := v.(string); ok {
				result[k] = normalizeImage(name, str)
			} else {
				result[k] = v
			}
		case "environment":
			result[k] = transformEnvironment(v)
		case "build":
			result[k] = normalizeBuild(v)
		default:
			result[k] = v
		}
	}

	// Merge secrets
	prodSecrets := getSecretsList(prodMap)
	devSecrets := getSecretsList(devMap)
	mergedSecrets := mergeSecretLists(devSecrets, prodSecrets)

	// Filter secrets based on service to match target
	result["secrets"] = filterSecretsForService(name, mergedSecrets)

	// Special case for Traefik - keep exact format
	if name == "traefik" {
		result["image"] = "traefik:v3.6.6@sha256:2979bff651c98e70345dd886186a7a15ee3ce18b636af208d4ccbf2d56dbdddd"
		result["command"] = "--ping=true\n--log.level=INFO\n--entryPoints.http.address=:80\n--entryPoints.http.forwardedHeaders.trustedIPs=${FRONTEND_IP_1},${FRONTEND_IP_2},${FRONTEND_IP_3}\n--entryPoints.http.http.encodedCharacters.allowEncodedSlash=true\n--entryPoints.http.http.encodedCharacters.allowEncodedPercent=true\n--entryPoints.http.http.encodedCharacters.allowEncodedQuestionMark=true\n--entryPoints.http.http.encodedCharacters.allowEncodedHash=true\n--entryPoints.http.transport.respondingTimeouts.readTimeout=60\n--entryPoints.https.address=:443\n--entryPoints.https.forwardedHeaders.trustedIPs=${FRONTEND_IP_1},${FRONTEND_IP_2},${FRONTEND_IP_3}\n--entryPoints.http.http.encodedCharacters.allowEncodedSlash=true\n--entryPoints.http.http.encodedCharacters.allowEncodedPercent=true\n--entryPoints.http.http.encodedCharacters.allowEncodedQuestionMark=true\n--entryPoints.http.http.encodedCharacters.allowEncodedHash=true\n--entryPoints.https.transport.respondingTimeouts.readTimeout=60\n--providers.file.directory=/etc/traefik/dynamic\n--providers.docker=true\n--providers.docker.network=default\n--providers.docker.exposedByDefault=false\n--api.insecure=${DEVELOPMENT_ENVIRONMENT:-false}\n--api.dashboard=${DEVELOPMENT_ENVIRONMENT:-false}\n--api.debug=${DEVELOPMENT_ENVIRONMENT:-false}"
		result["environment"] = map[string]interface{}{
			"TLS_PROVIDER":            "${TLS_PROVIDER:-self-managed}",
			"URI_SCHEME":              "${URI_SCHEME:-http}",
			"DOMAIN":                  "${DOMAIN}",
			"DEVELOPMENT_ENVIRONMENT": "${DEVELOPMENT_ENVIRONMENT:-false}",
		}
		result["secrets"] = []interface{}{
			map[string]interface{}{"source": "CERT_PUBLIC_KEY"},
			map[string]interface{}{"source": "CERT_PRIVATE_KEY"},
		}
		result["ports"] = []interface{}{
			"${HOST_INSECURE_PORT:-80}:80",
			"${HOST_SECURE_PORT:-443}:443",
		}
		result["security_opt"] = []interface{}{
			"label=type:container_runtime_t",
		}
		result["volumes"] = []interface{}{
			"/var/run/docker.sock:/var/run/docker.sock:z",
			"./conf/traefik:/etc/traefik/dynamic:ro",
			"acme-data:/acme:rw",
		}
		result["healthcheck"] = map[string]interface{}{
			"test":         "traefik healthcheck --ping",
			"start_period": "10s",
		}
		result["networks"] = map[string]interface{}{
			"default": map[string]interface{}{
				"aliases": []interface{}{
					"activemq.${DOMAIN}",
					"blazegraph.${DOMAIN}",
					"fcrepo.${DOMAIN}",
					"${DOMAIN}",
					"solr.${DOMAIN}",
				},
			},
		}
		result["depends_on"] = map[string]interface{}{
			"drupal": map[string]interface{}{
				"condition": "service_healthy",
			},
		}
	}

	return result
}

func isJustDefaultNetwork(v interface{}) bool {
	if netMap, ok := v.(map[string]interface{}); ok {
		if len(netMap) == 1 {
			if _, hasDefault := netMap["default"]; hasDefault {
				// Skip default network even if it has aliases - services can communicate by name
				return true
			}
		}
	} else if netList, ok := v.([]interface{}); ok {
		if len(netList) == 1 {
			if netStr, ok := netList[0].(string); ok && netStr == "default" {
				return true
			}
		}
	}
	return false
}

func normalizeBuild(build interface{}) interface{} {
	bMap, ok := build.(map[string]interface{})
	if !ok {
		return build
	}
	if args, ok := bMap["args"].(map[string]interface{}); ok {
		if repo, ok := args["REPOSITORY"].(string); ok {
			args["REPOSITORY"] = strings.ReplaceAll(repo, "${ISLANDORA_REPOSITORY}", "islandora")
		}
	}
	return bMap
}

func normalizeImage(name, img string) string {
	if name == "drupal" {
		return img
	}
	if strings.Contains(img, "fcrepo") {
		return "islandora/fcrepo6:${ISLANDORA_TAG}"
	}
	img = strings.ReplaceAll(img, "${ISLANDORA_REPOSITORY}", "islandora")
	return img
}

func filterSecretsForService(serviceName string, secrets []interface{}) []interface{} {
	result := []interface{}{}

	certSecrets := map[string]bool{
		"CERT_PUBLIC_KEY":  true,
		"CERT_PRIVATE_KEY": true,
		"CERT_AUTHORITY":   true,
		"UID":              true,
	}

	for _, sec := range secrets {
		name := getSecretName(sec)
		if certSecrets[name] {
			switch serviceName {
			case "crayfits", "homarus", "houdini", "hypercube", "milliner", "cantaloupe", "drupal":
				if name != "CERT_PRIVATE_KEY" {
					result = append(result, sec)
				}
			case "traefik":
				if name == "CERT_PUBLIC_KEY" || name == "CERT_PRIVATE_KEY" {
					result = append(result, sec)
				}
			case "init":
				result = append(result, sec)
			}
		} else {
			result = append(result, sec)
		}
	}

	sort.Slice(result, func(i, j int) bool {
		return secretWeight(getSecretName(result[i])) < secretWeight(getSecretName(result[j]))
	})

	return result
}

func getSecretName(sec interface{}) string {
	if s, ok := sec.(string); ok {
		return s
	} else if m, ok := sec.(map[string]interface{}); ok {
		if s, ok := m["source"].(string); ok {
			return s
		}
	}
	return ""
}

func stripDevProdFromDependsOn(deps interface{}) interface{} {
	switch d := deps.(type) {
	case []interface{}:
		result := make([]interface{}, 0)
		for _, dep := range d {
			if str, ok := dep.(string); ok {
				// Remove -dev/-prod suffix
				stripped := strings.TrimSuffix(strings.TrimSuffix(str, "-dev"), "-prod")
				result = append(result, stripped)
			} else {
				result = append(result, dep)
			}
		}
		return result
	case map[string]interface{}:
		result := make(map[string]interface{})
		for name, config := range d {
			// Remove -dev/-prod suffix
			stripped := strings.TrimSuffix(strings.TrimSuffix(name, "-dev"), "-prod")
			result[stripped] = config
		}
		return result
	}
	return deps
}

func getSecretsList(svcMap map[string]interface{}) []interface{} {
	if secrets, ok := svcMap["secrets"].([]interface{}); ok {
		return secrets
	}
	return nil
}

func mergeSecretLists(list1, list2 []interface{}) []interface{} {
	secretSet := make(map[string]interface{})

	// Add all from list1
	for _, secret := range list1 {
		if secretMap, ok := secret.(map[string]interface{}); ok {
			if source, ok := secretMap["source"].(string); ok {
				secretSet[source] = secret
			}
		} else if secretStr, ok := secret.(string); ok {
			secretSet[secretStr] = secret
		}
	}

	// Add all from list2
	for _, secret := range list2 {
		if secretMap, ok := secret.(map[string]interface{}); ok {
			if source, ok := secretMap["source"].(string); ok {
				secretSet[source] = secret
			}
		} else if secretStr, ok := secret.(string); ok {
			secretSet[secretStr] = secret
		}
	}

	// Convert back to list
	result := make([]interface{}, 0, len(secretSet))
	for _, v := range secretSet {
		result = append(result, v)
	}
	return result
}

func ensureDevSecrets(secrets map[string]interface{}) {
	// Ensure dev certificate secrets exist
	devSecrets := map[string]string{
		"CERT_PUBLIC_KEY":  "./certs/cert.pem",
		"CERT_PRIVATE_KEY": "./certs/privkey.pem",
		"CERT_AUTHORITY":   "./certs/rootCA.pem",
		"UID":              "./certs/UID",
	}

	for name, path := range devSecrets {
		if _, exists := secrets[name]; !exists {
			secrets[name] = map[string]interface{}{
				"file": path,
			}
		}
	}
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

	// Look for project prefix in the path and replace everything up to and including it with '.'
	// Match pattern: any absolute path that contains the project prefix followed by /
	pattern := `^/.*/` + regexp.QuoteMeta(projectPrefix) + `/`
	re := regexp.MustCompile(pattern)
	if re.MatchString(path) {
		// Replace the entire prefix up to project directory with ./
		path = re.ReplaceAllString(path, "./")
	}

	// Remove /live/ from secrets paths
	if strings.Contains(path, "secrets/live/") {
		path = strings.ReplaceAll(path, "secrets/live/", "secrets/")
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

func buildYAMLOutput(compose map[string]interface{}) (string, error) {
	var buf bytes.Buffer
	buf.WriteString("---\n")

	// Write x-common anchor first if it exists
	if xCommon, ok := compose["x-common"].(map[string]interface{}); ok {
		buf.WriteString("# Common to all services\n")
		buf.WriteString("x-common: &common\n")

		if restart, ok := xCommon["restart"].(string); ok {
			buf.WriteString(fmt.Sprintf("  restart: %s\n", restart))
		}
		if tty, ok := xCommon["tty"].(bool); ok {
			buf.WriteString(fmt.Sprintf("  tty: %t # Required for non-root users with selinux enabled.\n", tty))
		}
		if secOpts, ok := xCommon["security_opt"].([]interface{}); ok {
			buf.WriteString("  security_opt:\n")
			for _, opt := range secOpts {
				buf.WriteString(fmt.Sprintf("    - %s # Required for selinux to access the docker socket and bind mount files.\n", opt))
			}
		}
		buf.WriteString("  networks:\n")
		buf.WriteString("    default:\n")
	}

	// Write networks
	if _, ok := compose["networks"]; ok {
		buf.WriteString("networks:\n")
		buf.WriteString("  default:\n")
		buf.WriteString("\n")
	}

	// Write volumes
	if volumes, ok := compose["volumes"].(map[string]interface{}); ok {
		buf.WriteString("volumes:\n")
		var volNames []string
		for name := range volumes {
			volNames = append(volNames, name)
		}
		sort.Strings(volNames)
		for _, name := range volNames {
			buf.WriteString(fmt.Sprintf("  %s: {}\n", name))
		}
		buf.WriteString("\n")
	}

	// Write secrets
	if secrets, ok := compose["secrets"].(map[string]interface{}); ok {
		buf.WriteString("secrets:\n")
		buf.WriteString("  # Certificates are only used for development environments.\n")

		certSecrets := []string{"CERT_PUBLIC_KEY", "CERT_PRIVATE_KEY", "CERT_AUTHORITY", "UID"}
		for _, name := range certSecrets {
			if secret, ok := secrets[name].(map[string]interface{}); ok {
				buf.WriteString(fmt.Sprintf("  %s:\n", name))
				if file, ok := secret["file"].(string); ok {
					// Path should already be normalized, but ensure it's relative
					if !strings.HasPrefix(file, "./") && !strings.HasPrefix(file, "../") {
						// Path is absolute, extract relative part
						if idx := strings.Index(file, "/certs/"); idx >= 0 {
							file = "." + file[idx:]
						} else if idx := strings.Index(file, "/secrets/"); idx >= 0 {
							file = "." + file[idx:]
						}
					}
					buf.WriteString(fmt.Sprintf("    file: %s\n", file))
				}
			}
		}

		buf.WriteString("  # Production secrets:\n")
		var otherSecrets []string
		for name := range secrets {
			if !contains(certSecrets, name) {
				otherSecrets = append(otherSecrets, name)
			}
		}

		sort.Slice(otherSecrets, func(i, j int) bool {
			return secretWeight(otherSecrets[i]) < secretWeight(otherSecrets[j])
		})

		for _, name := range otherSecrets {
			if secret, ok := secrets[name].(map[string]interface{}); ok {
				buf.WriteString(fmt.Sprintf("  %s:\n", name))
				if file, ok := secret["file"].(string); ok {
					// Path should already be normalized, but ensure it's relative
					if !strings.HasPrefix(file, "./") && !strings.HasPrefix(file, "../") {
						// Path is absolute, extract relative part
						if idx := strings.Index(file, "/certs/"); idx >= 0 {
							file = "." + file[idx:]
						} else if idx := strings.Index(file, "/secrets/"); idx >= 0 {
							file = "." + file[idx:]
						}
					}
					// Use double quotes for all production secrets to match target
					buf.WriteString(fmt.Sprintf("    file: \"%s\"\n", file))
				}
			}
		}
		buf.WriteString("\n")
	}

	// Write services
	if services, ok := compose["services"].(map[string]interface{}); ok {
		buf.WriteString("services:\n")

		targetOrder := []string{"init", "alpaca", "crayfits", "fits", "homarus", "houdini", "hypercube", "mariadb", "milliner", "activemq", "blazegraph", "cantaloupe", "drupal", "fcrepo", "solr", "traefik"}
		var serviceNames []string
		for _, name := range targetOrder {
			if _, exists := services[name]; exists {
				serviceNames = append(serviceNames, name)
			}
		}
		for name := range services {
			if !contains(serviceNames, name) {
				serviceNames = append(serviceNames, name)
			}
		}

		for _, name := range serviceNames {
			svc := services[name]
			buf.WriteString(fmt.Sprintf("  %s:\n", name))

			svcMap, ok := svc.(map[string]interface{})
			if !ok {
				continue
			}

			if ref, ok := svcMap["<<"].(string); ok && ref == "*common" {
				buf.WriteString("    <<: *common\n")
				delete(svcMap, "<<")
			}

			keys := []string{"image", "build", "restart", "networks", "volumes", "command", "environment", "healthcheck", "secrets", "ports", "security_opt", "depends_on", "entrypoint", "profiles"}

			for _, k := range keys {
				if val, exists := svcMap[k]; exists {
					writeServiceField(&buf, k, val)
					delete(svcMap, k)
				}
			}

			var remainingKeys []string
			for k := range svcMap {
				remainingKeys = append(remainingKeys, k)
			}
			sort.Strings(remainingKeys)
			for _, k := range remainingKeys {
				writeServiceField(&buf, k, svcMap[k])
			}
			buf.WriteString("\n")
		}
	}

	return buf.String(), nil
}

func secretWeight(name string) int {
	weights := map[string]int{
		"ACTIVEMQ_PASSWORD":               1,
		"ACTIVEMQ_WEB_ADMIN_PASSWORD":     2,
		"ALPACA_JMS_PASSWORD":             3,
		"DB_ROOT_PASSWORD":                4,
		"DRUPAL_DEFAULT_ACCOUNT_PASSWORD": 5,
		"DRUPAL_DEFAULT_DB_PASSWORD":      6,
		"DRUPAL_DEFAULT_SALT":             7,
		"FCREPO_DB_PASSWORD":              8,
		"JWT_ADMIN_TOKEN":                 9,
		"JWT_PUBLIC_KEY":                  10,
		"JWT_PRIVATE_KEY":                 11,
		"CERT_PUBLIC_KEY":                 12,
		"CERT_PRIVATE_KEY":                13,
		"CERT_AUTHORITY":                  14,
		"UID":                             15,
	}
	if w, ok := weights[name]; ok {
		return w
	}
	return 100
}

func writeServiceField(buf *bytes.Buffer, key string, val interface{}) {
	if key == "command" {
		if s, ok := val.(string); ok && strings.Contains(s, "\n") {
			buf.WriteString("    command: >-\n")
			lines := strings.Split(s, "\n")
			for _, line := range lines {
				buf.WriteString("      " + line + "\n")
			}
			return
		}
	}

	if key == "networks" {
		if m, ok := val.(map[string]interface{}); ok {
			if len(m) == 1 {
				if _, exists := m["default"]; exists && m["default"] == nil {
					buf.WriteString("    networks:\n      default:\n")
					return
				}
			}
		}
	}

	// Normalize volume paths before writing
	if key == "volumes" {
		if volList, ok := val.([]interface{}); ok {
			for _, vol := range volList {
				if volMap, ok := vol.(map[string]interface{}); ok {
					if source, ok := volMap["source"].(string); ok {
						// Ensure absolute paths are normalized to relative
						if strings.HasPrefix(source, "/") && !strings.HasPrefix(source, "/var/") {
							// Extract relative part after finding project directory patterns
							if idx := strings.Index(source, "/codebase"); idx >= 0 {
								volMap["source"] = "." + source[idx:]
							} else if idx := strings.Index(source, "/build/"); idx >= 0 {
								volMap["source"] = "." + source[idx:]
							} else if idx := strings.LastIndex(source, "/certs"); idx >= 0 {
								volMap["source"] = "." + source[idx:]
							} else if idx := strings.LastIndex(source, "/secrets"); idx >= 0 {
								volMap["source"] = "." + source[idx:]
							}
						}
					}
				}
			}
		}
	}

	var svcBuf bytes.Buffer
	encoder := yaml.NewEncoder(&svcBuf)
	encoder.SetIndent(2)

	fieldMap := map[string]interface{}{key: val}
	if err := encoder.Encode(fieldMap); err != nil {
		return
	}
	encoder.Close()

	lines := strings.SplitSeq(strings.TrimSpace(svcBuf.String()), "\n")
	for line := range lines {
		buf.WriteString("    " + line + "\n")
	}
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

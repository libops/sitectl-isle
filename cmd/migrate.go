package cmd

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Migration helpers",
}

var mergeProfilesCmd = &cobra.Command{
	Use:   "merge-compose-profiles",
	Short: "Merge compose profiles into single service definitions",
	Long: `Merge compose profiles into single service definitions.
Move traefik labels into their own conf files

Supports migration from:
- legacy isle-site-template format (-dev/-prod service pairs)

The tool will detect the format automatically and apply the appropriate transformations.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		inputPath, _ := cmd.Flags().GetString("input")
		outputPath, _ := cmd.Flags().GetString("output")
		force, _ := cmd.Flags().GetBool("force")

		return migrateLegacy(inputPath, outputPath, force)
	},
}

func init() {
	migrateCmd.AddCommand(mergeProfilesCmd)
	mergeProfilesCmd.Flags().StringP("input", "i", "docker-compose.yml", "Input docker-compose file path")
	mergeProfilesCmd.Flags().StringP("output", "o", "docker-compose.yml", "Output docker-compose file path")
	mergeProfilesCmd.Flags().BoolP("force", "f", false, "Overwrite output file if it exists")
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

	return "current"
}

func transformISLESiteTemplate(compose map[string]interface{}) (map[string]interface{}, []string) {
	warnings := []string{}
	result := make(map[string]interface{})

	// Add x-common anchor
	result["x-common"] = map[string]interface{}{
		"restart": "unless-stopped",
		"tty":     true,
		"security_opt": []interface{}{
			"label=type:container_runtime_t",
		},
		"networks": map[string]interface{}{
			"default": interface{}(nil),
		},
	}

	// Networks
	result["networks"] = map[string]interface{}{
		"default": map[string]interface{}{},
	}

	// Volumes - copy and add acme-data
	if volumes, ok := compose["volumes"].(map[string]interface{}); ok {
		transformed := make(map[string]interface{})
		for name := range volumes {
			transformed[name] = map[string]interface{}{}
		}
		transformed["acme-data"] = map[string]interface{}{}
		result["volumes"] = transformed
	}

	// Secrets - just copy as-is (already correct format)
	result["secrets"] = compose["secrets"]

	// Merge dev/prod service pairs
	if services, ok := compose["services"].(map[string]interface{}); ok {
		result["services"] = mergeSiteTemplateServices(services)
	}

	return result, warnings
}

func mergeSiteTemplateServices(services map[string]interface{}) map[string]interface{} {
	merged := make(map[string]interface{})

	// Service order to match target
	targetOrder := []string{"init", "alpaca", "crayfits", "fits", "homarus", "houdini", "hypercube", "mariadb", "milliner", "activemq", "blazegraph", "cantaloupe", "drupal", "fcrepo", "solr", "traefik"}

	for _, baseName := range targetOrder {
		devName := baseName + "-dev"
		prodName := baseName + "-prod"

		devSvc, hasDev := services[devName]
		prodSvc, hasProd := services[prodName]

		if hasDev && hasProd {
			merged[baseName] = mergeDevProdService(baseName, devSvc, prodSvc)
		}
	}

	// Add init service if not present (standard in unified format)
	if _, exists := merged["init"]; !exists {
		merged["init"] = mergeDevProdService("init", nil, nil)
	}

	return merged
}

func mergeDevProdService(name string, dev, prod interface{}) map[string]interface{} {
	// Special case for init service
	if name == "init" {
		return map[string]interface{}{
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

	prodMap, ok := prod.(map[string]interface{})
	if !ok {
		return make(map[string]interface{})
	}

	result := make(map[string]interface{})
	result["<<"] = "*common"

	// Copy from prod, skipping fields we don't want
	skipFields := map[string]bool{
		"profiles":     true,
		"restart":      true,
		"tty":          true,
		"security_opt": true,
		"<<":           true,
		"labels":       true,
	}

	for k, v := range prodMap {
		if skipFields[k] {
			continue
		}

		// Skip default network (covered by x-common)
		if k == "networks" {
			if netMap, ok := v.(map[string]interface{}); ok {
				if len(netMap) == 1 {
					if _, hasDefault := netMap["default"]; hasDefault {
						continue
					}
				}
			}
		}

		// Fix image names: ${ISLANDORA_REPOSITORY}/name → islandora/name
		if k == "image" {
			if str, ok := v.(string); ok {
				v = strings.ReplaceAll(str, "${ISLANDORA_REPOSITORY}", "islandora")
			}
		}

		// Fix build args
		if k == "build" {
			if bMap, ok := v.(map[string]interface{}); ok {
				if args, ok := bMap["args"].(map[string]interface{}); ok {
					if repo, ok := args["REPOSITORY"].(string); ok {
						args["REPOSITORY"] = strings.ReplaceAll(repo, "${ISLANDORA_REPOSITORY}", "islandora")
					}
				}
			}
		}

		// Strip -dev/-prod from depends_on
		if k == "depends_on" {
			v = stripDevProdFromDependsOn(v)
		}

		result[k] = v
	}

	// Handle secrets: certain services need dev certs + prod secrets
	jwtServices := map[string]bool{
		"crayfits":   true,
		"homarus":    true,
		"houdini":    true,
		"hypercube":  true,
		"milliner":   true,
		"cantaloupe": true,
		"drupal":     true,
	}

	if jwtServices[name] {
		// These services need dev cert secrets (minus PRIVATE_KEY) + their prod secrets
		prodSecrets, _ := result["secrets"].([]interface{})
		devCerts := []interface{}{
			map[string]interface{}{"source": "CERT_PUBLIC_KEY"},
			map[string]interface{}{"source": "CERT_AUTHORITY"},
			map[string]interface{}{"source": "UID"},
		}
		// Prepend dev certs to prod secrets
		result["secrets"] = append(devCerts, prodSecrets...)
	} else if secrets, hasSecrets := result["secrets"]; hasSecrets {
		// If secrets is an empty list, remove the key entirely
		if secList, ok := secrets.([]interface{}); ok && len(secList) == 0 {
			delete(result, "secrets")
		}
	}

	// Special cases by service name
	switch name {
	case "traefik":
		return buildTraefikService()
	case "drupal":
		// Remove dev bind mounts, keep only volume mounts
		if vols, ok := result["volumes"].([]interface{}); ok {
			filtered := []interface{}{}
			for _, vol := range vols {
				// Keep if it's a named volume (not a bind mount starting with ./)
				if volStr, isString := vol.(string); isString && !strings.HasPrefix(volStr, "./") {
					filtered = append(filtered, vol)
				} else if _, isMap := vol.(map[string]interface{}); isMap {
					filtered = append(filtered, vol)
				}
			}
			result["volumes"] = filtered
		}

		// Update environment for unified format
		if env, ok := result["environment"].(map[string]interface{}); ok {
			env["DEVELOPMENT_ENVIRONMENT"] = "${DEVELOPMENT_ENVIRONMENT:-false}"
			env["DRUPAL_DEFAULT_CANTALOUPE_URL"] = "${URI_SCHEME}://${DOMAIN}/cantaloupe/iiif/2"
			env["DRUPAL_DEFAULT_FCREPO_URL"] = "${URI_SCHEME}://fcrepo.${DOMAIN}/fcrepo/rest/"
			env["DRUSH_OPTIONS_URI"] = "${URI_SCHEME}://${DOMAIN}"
			env["DRUPAL_ENABLE_HTTPS"] = "false"
		}
	case "milliner":
		// Update environment variable name
		if env, ok := result["environment"].(map[string]interface{}); ok {
			if _, ok := env["MILLINER_FEDORA6"]; ok {
				env["MILLINER_FEDORA6"] = true
			}
		}
	}

	return result
}

func buildTraefikService() map[string]interface{} {
	return map[string]interface{}{
		"<<":      "*common",
		"image":   "traefik:v3.6.6@sha256:2979bff651c98e70345dd886186a7a15ee3ce18b636af208d4ccbf2d56dbdddd",
		"command": "--ping=true\n--log.level=INFO\n--entryPoints.http.address=:80\n--entryPoints.http.forwardedHeaders.trustedIPs=${FRONTEND_IP_1},${FRONTEND_IP_2},${FRONTEND_IP_3}\n--entryPoints.http.http.encodedCharacters.allowEncodedSlash=true\n--entryPoints.http.http.encodedCharacters.allowEncodedPercent=true\n--entryPoints.http.http.encodedCharacters.allowEncodedQuestionMark=true\n--entryPoints.http.http.encodedCharacters.allowEncodedHash=true\n--entryPoints.http.transport.respondingTimeouts.readTimeout=60\n--entryPoints.https.address=:443\n--entryPoints.https.forwardedHeaders.trustedIPs=${FRONTEND_IP_1},${FRONTEND_IP_2},${FRONTEND_IP_3}\n--entryPoints.http.http.encodedCharacters.allowEncodedSlash=true\n--entryPoints.http.http.encodedCharacters.allowEncodedPercent=true\n--entryPoints.http.http.encodedCharacters.allowEncodedQuestionMark=true\n--entryPoints.http.http.encodedCharacters.allowEncodedHash=true\n--entryPoints.https.transport.respondingTimeouts.readTimeout=60\n--providers.file.directory=/etc/traefik/dynamic\n--providers.docker=true\n--providers.docker.network=default\n--providers.docker.exposedByDefault=false\n--api.insecure=${DEVELOPMENT_ENVIRONMENT:-false}\n--api.dashboard=${DEVELOPMENT_ENVIRONMENT:-false}\n--api.debug=${DEVELOPMENT_ENVIRONMENT:-false}",
		"environment": map[string]interface{}{
			"TLS_PROVIDER":            "${TLS_PROVIDER:-self-managed}",
			"URI_SCHEME":              "${URI_SCHEME:-http}",
			"DOMAIN":                  "${DOMAIN}",
			"DEVELOPMENT_ENVIRONMENT": "${DEVELOPMENT_ENVIRONMENT:-false}",
		},
		"secrets": []interface{}{
			map[string]interface{}{"source": "CERT_PUBLIC_KEY"},
			map[string]interface{}{"source": "CERT_PRIVATE_KEY"},
		},
		"ports": []interface{}{
			"${HOST_INSECURE_PORT:-80}:80",
			"${HOST_SECURE_PORT:-443}:443",
		},
		"security_opt": []interface{}{
			"label=type:container_runtime_t",
		},
		"volumes": []interface{}{
			"/var/run/docker.sock:/var/run/docker.sock:z",
			"./conf/traefik:/etc/traefik/dynamic:ro",
			"acme-data:/acme:rw",
		},
		"healthcheck": map[string]interface{}{
			"test":         "traefik healthcheck --ping",
			"start_period": "10s",
		},
		"networks": map[string]interface{}{
			"default": map[string]interface{}{
				"aliases": []interface{}{
					"activemq.${DOMAIN}",
					"blazegraph.${DOMAIN}",
					"fcrepo.${DOMAIN}",
					"${DOMAIN}",
					"solr.${DOMAIN}",
				},
			},
		},
		"depends_on": map[string]interface{}{
			"drupal": map[string]interface{}{
				"condition": "service_healthy",
			},
		},
	}
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

		// Sort service names alphabetically
		var serviceNames []string
		for name := range services {
			serviceNames = append(serviceNames, name)
		}
		sort.Strings(serviceNames)

		for _, name := range serviceNames {
			svc := services[name]
			buf.WriteString(fmt.Sprintf("  %s:\n", name))

			svcMap, ok := svc.(map[string]interface{})
			if !ok {
				continue
			}

			// Handle <<: *common anchor first if it exists
			if ref, ok := svcMap["<<"].(string); ok && ref == "*common" {
				buf.WriteString("    <<: *common\n")
				delete(svcMap, "<<")
			}

			// Sort all keys alphabetically
			var keys []string
			for k := range svcMap {
				keys = append(keys, k)
			}
			sort.Strings(keys)

			for _, k := range keys {
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

// sortMapKeysRecursively sorts all map keys alphabetically in a data structure
func sortMapKeysRecursively(data interface{}) interface{} {
	switch v := data.(type) {
	case map[string]interface{}:
		// Create a new yaml.Node to preserve order
		sorted := make(map[string]interface{})
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			sorted[k] = sortMapKeysRecursively(v[k])
		}
		return sorted
	case []interface{}:
		result := make([]interface{}, len(v))
		for i, item := range v {
			result[i] = sortMapKeysRecursively(item)
		}
		return result
	default:
		return data
	}
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

	// Sort maps recursively before encoding
	val = sortMapKeysRecursively(val)

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

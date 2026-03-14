package traefikconfig

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	ModeHTTP                 = "http"
	ModeSelfManaged          = "self-managed"
	ModeMkcert               = "mkcert"
	ModeLetsEncrypt          = "letsencrypt"
	ModeInherited            = "inherit"
	defaultDevTraefikCommand = `--ping=true
--log.level=INFO
--entryPoints.http.address=:80
--entryPoints.http.forwardedHeaders.trustedIPs=${FRONTEND_IP_1},${FRONTEND_IP_2},${FRONTEND_IP_3}
--entryPoints.http.transport.respondingTimeouts.readTimeout=60
--entryPoints.https.address=:443
--entryPoints.https.forwardedHeaders.trustedIPs=${FRONTEND_IP_1},${FRONTEND_IP_2},${FRONTEND_IP_3}
--entryPoints.https.transport.respondingTimeouts.readTimeout=60
--providers.file.directory=/etc/traefik/dynamic
--providers.docker=true
--providers.docker.network=default
--providers.docker.exposedByDefault=false
--api.insecure=${DEVELOPMENT_ENVIRONMENT:-false}
--api.dashboard=${DEVELOPMENT_ENVIRONMENT:-false}
--api.debug=${DEVELOPMENT_ENVIRONMENT:-false}`
)

var letsEncryptCommandLines = []string{
	"--entrypoints.https.http.tls.certResolver=letsencrypt",
	"--certificatesresolvers.letsencrypt.acme.httpchallenge=true",
	"--certificatesresolvers.letsencrypt.acme.httpchallenge.entrypoint=http",
	"--certificatesresolvers.letsencrypt.acme.storage=/acme/acme.json",
	"--certificatesresolvers.letsencrypt.acme.email=${ACME_EMAIL}",
	"--certificatesresolvers.letsencrypt.acme.caserver=${ACME_URL}",
}

type Status struct {
	Enabled bool
	Drifted bool
	Mode    string
	Detail  string
}

func DetectProd(projectDir string) (Status, error) {
	env, err := readDotEnv(filepath.Join(projectDir, ".env"))
	if err != nil {
		return Status{}, err
	}
	composePath := filepath.Join(projectDir, "docker-compose.yml")
	composeText, err := os.ReadFile(composePath)
	if err != nil {
		return Status{}, err
	}

	uriScheme := firstNonEmpty(env["URI_SCHEME"], ModeHTTP)
	tlsProvider := firstNonEmpty(env["TLS_PROVIDER"], ModeSelfManaged)
	drupalHTTPS, drupalFound := detectDrupalHTTPS(string(composeText))
	hasLE := hasLetsEncryptCommand(string(composeText))
	mode := detectSelfManagedMode(projectDir)

	switch {
	case uriScheme == ModeHTTP && drupalFound && !drupalHTTPS && !hasLE:
		return Status{Mode: ModeHTTP, Detail: "docker-compose.yml + .env"}, nil
	case uriScheme == "https" && drupalFound && drupalHTTPS && tlsProvider == ModeLetsEncrypt && hasLE:
		return Status{Enabled: true, Mode: ModeLetsEncrypt, Detail: "docker-compose.yml + .env"}, nil
	case uriScheme == "https" && drupalFound && drupalHTTPS && tlsProvider == ModeSelfManaged && !hasLE:
		return Status{Enabled: true, Mode: mode, Detail: "docker-compose.yml + .env"}, nil
	default:
		return Status{
			Drifted: true,
			Mode:    tlsProvider,
			Detail:  fmt.Sprintf("uri_scheme=%s tls_provider=%s drupal_enable_https=%t letsencrypt=%t", uriScheme, tlsProvider, drupalHTTPS, hasLE),
		}, nil
	}
}

func DetectDev(projectDir string) (Status, error) {
	devPath := filepath.Join(projectDir, "docker-compose.dev.yml")
	data, err := os.ReadFile(devPath)
	if err != nil {
		if os.IsNotExist(err) {
			return Status{Mode: ModeInherited, Detail: "docker-compose.dev.yml not present"}, nil
		}
		return Status{}, err
	}

	doc := map[string]any{}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return Status{}, fmt.Errorf("parse docker-compose.dev.yml: %w", err)
	}
	services := getMap(doc, "services")
	if len(services) == 0 {
		return Status{Mode: ModeInherited, Detail: "docker-compose.dev.yml has no service overrides"}, nil
	}

	drupalEnv := getMap(getMap(services, "drupal"), "environment")
	traefikSvc := getMap(services, "traefik")
	traefikEnv := getMap(traefikSvc, "environment")
	commandValue, _ := traefikSvc["command"].(string)

	hasOverride := len(drupalEnv) > 0 || len(traefikEnv) > 0 || commandValue != ""
	if !hasOverride {
		return Status{Mode: ModeInherited, Detail: "docker-compose.dev.yml has no TLS override"}, nil
	}

	drupalHTTPS, drupalFound := mapStringValue(drupalEnv, "DRUPAL_ENABLE_HTTPS")
	uriScheme, uriFound := mapStringValue(traefikEnv, "URI_SCHEME")
	tlsProvider, providerFound := mapStringValue(traefikEnv, "TLS_PROVIDER")
	hasLE := hasLetsEncryptCommand(commandValue)

	switch {
	case !drupalFound && !uriFound && !providerFound && !hasLE:
		return Status{Mode: ModeInherited, Detail: "docker-compose.dev.yml inherits docker-compose.yml"}, nil
	case drupalFound && uriFound && providerFound && drupalHTTPS == "false" && uriScheme == ModeHTTP && tlsProvider == ModeSelfManaged && !hasLE:
		return Status{Enabled: true, Mode: ModeHTTP, Detail: "docker-compose.dev.yml"}, nil
	case drupalFound && uriFound && providerFound && drupalHTTPS == "true" && uriScheme == "https" && tlsProvider == ModeLetsEncrypt && hasLE:
		return Status{Enabled: true, Mode: ModeLetsEncrypt, Detail: "docker-compose.dev.yml"}, nil
	case drupalFound && uriFound && providerFound && drupalHTTPS == "true" && uriScheme == "https" && tlsProvider == ModeSelfManaged && !hasLE:
		return Status{Enabled: true, Mode: detectSelfManagedMode(projectDir), Detail: "docker-compose.dev.yml"}, nil
	default:
		return Status{
			Drifted: true,
			Mode:    tlsProvider,
			Detail:  fmt.Sprintf("dev_override drupal_enable_https=%s uri_scheme=%s tls_provider=%s letsencrypt=%t", drupalHTTPS, uriScheme, tlsProvider, hasLE),
		}, nil
	}
}

func ApplyProd(projectDir, mode string) error {
	if err := validateMode(mode, true); err != nil {
		return err
	}

	enableHTTPS := mode != ModeHTTP
	if err := updateEnvFile(filepath.Join(projectDir, ".env"), map[string]string{
		"URI_SCHEME":   ternary(enableHTTPS, "https", ModeHTTP),
		"TLS_PROVIDER": ternary(mode == ModeLetsEncrypt, ModeLetsEncrypt, ModeSelfManaged),
	}); err != nil {
		return err
	}

	composePath := filepath.Join(projectDir, "docker-compose.yml")
	composeText, err := os.ReadFile(composePath)
	if err != nil {
		return err
	}
	updated := setDrupalEnableHTTPSLine(string(composeText), enableHTTPS)
	updated = setLetsEncryptCommand(updated, mode == ModeLetsEncrypt)
	return os.WriteFile(composePath, []byte(updated), 0o644)
}

func ApplyDev(projectDir string, enabled bool, mode string) error {
	if enabled {
		if err := validateMode(mode, true); err != nil {
			return err
		}
	} else {
		mode = ModeInherited
	}

	devPath := filepath.Join(projectDir, "docker-compose.dev.yml")
	doc := map[string]any{}
	if data, err := os.ReadFile(devPath); err == nil {
		if err := yaml.Unmarshal(data, &doc); err != nil {
			return fmt.Errorf("parse docker-compose.dev.yml: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	services := ensureMap(doc, "services")
	drupal := ensureMap(services, "drupal")
	fcrepo := ensureMap(services, "fcrepo")
	traefik := ensureMap(services, "traefik")

	if enabled {
		scheme := "https"
		provider := ModeSelfManaged
		if mode == ModeHTTP {
			scheme = ModeHTTP
		}
		if mode == ModeLetsEncrypt {
			provider = ModeLetsEncrypt
		}

		drupalEnv := ensureMap(drupal, "environment")
		drupalEnv["DRUPAL_DEFAULT_CANTALOUPE_URL"] = fmt.Sprintf("%s://${DOMAIN}/cantaloupe/iiif/2", scheme)
		drupalEnv["DRUPAL_DEFAULT_FCREPO_URL"] = fmt.Sprintf("%s://fcrepo.${DOMAIN}/fcrepo/rest/", scheme)
		drupalEnv["DRUPAL_ENABLE_HTTPS"] = ternary(scheme == "https", "true", "false")
		drupalEnv["DRUSH_OPTIONS_URI"] = fmt.Sprintf("%s://${DOMAIN}", scheme)

		fcrepoEnv := ensureMap(fcrepo, "environment")
		fcrepoEnv["FCREPO_ALLOW_EXTERNAL_DRUPAL"] = fmt.Sprintf("%s://${DOMAIN}/", scheme)

		traefikEnv := ensureMap(traefik, "environment")
		traefikEnv["DEVELOPMENT_ENVIRONMENT"] = "true"
		traefikEnv["TLS_PROVIDER"] = provider
		traefikEnv["URI_SCHEME"] = scheme
		traefik["command"] = buildDevTraefikCommand(mode == ModeLetsEncrypt)
	} else {
		deleteKeys(getMap(drupal, "environment"),
			"DRUPAL_DEFAULT_CANTALOUPE_URL",
			"DRUPAL_DEFAULT_FCREPO_URL",
			"DRUPAL_ENABLE_HTTPS",
			"DRUSH_OPTIONS_URI",
		)
		deleteKeys(getMap(fcrepo, "environment"), "FCREPO_ALLOW_EXTERNAL_DRUPAL")
		deleteKeys(getMap(traefik, "environment"), "DEVELOPMENT_ENVIRONMENT", "TLS_PROVIDER", "URI_SCHEME")
		delete(traefik, "command")
	}

	cleanupEmptyService(services, "drupal")
	cleanupEmptyService(services, "fcrepo")
	cleanupEmptyService(services, "traefik")
	if len(services) == 0 {
		delete(doc, "services")
	}

	out, err := marshalCompose(doc)
	if err != nil {
		return err
	}
	return os.WriteFile(devPath, out, 0o644)
}

func validateMode(mode string, allowHTTP bool) error {
	allowed := map[string]bool{
		ModeSelfManaged: true,
		ModeMkcert:      true,
		ModeLetsEncrypt: true,
	}
	if allowHTTP {
		allowed[ModeHTTP] = true
	}
	if !allowed[mode] {
		valid := make([]string, 0, len(allowed))
		for candidate := range allowed {
			valid = append(valid, candidate)
		}
		sort.Strings(valid)
		return fmt.Errorf("invalid tls mode %q: expected one of %s", mode, strings.Join(valid, ", "))
	}
	return nil
}

func buildDevTraefikCommand(withLetsEncrypt bool) string {
	lines := []string{defaultDevTraefikCommand}
	if withLetsEncrypt {
		lines = append(lines, letsEncryptCommandLines...)
	}
	return strings.Join(lines, "\n")
}

func setDrupalEnableHTTPSLine(contents string, enabled bool) string {
	value := ternary(enabled, `"true"`, `"false"`)
	lines := strings.Split(contents, "\n")
	for i, line := range lines {
		if strings.Contains(line, "DRUPAL_ENABLE_HTTPS:") {
			prefix := line[:strings.Index(line, "DRUPAL_ENABLE_HTTPS:")]
			lines[i] = prefix + "DRUPAL_ENABLE_HTTPS: " + value
			return strings.Join(lines, "\n")
		}
	}
	return contents
}

func setLetsEncryptCommand(contents string, enabled bool) string {
	lines := strings.Split(contents, "\n")
	filtered := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if isLetsEncryptCommandLine(trimmed) {
			continue
		}
		filtered = append(filtered, line)
	}
	if !enabled {
		return strings.Join(filtered, "\n")
	}
	for i, line := range filtered {
		if strings.Contains(line, "command: >-") {
			inserted := append([]string{}, filtered[:i+1]...)
			for _, leLine := range letsEncryptCommandLines {
				inserted = append(inserted, "      "+leLine)
			}
			inserted = append(inserted, filtered[i+1:]...)
			return strings.Join(inserted, "\n")
		}
	}
	return strings.Join(filtered, "\n")
}

func hasLetsEncryptCommand(contents string) bool {
	for _, line := range strings.Split(contents, "\n") {
		if isLetsEncryptCommandLine(strings.TrimSpace(line)) {
			return true
		}
	}
	return false
}

func isLetsEncryptCommandLine(line string) bool {
	for _, candidate := range letsEncryptCommandLines {
		if line == candidate {
			return true
		}
	}
	return false
}

func detectDrupalHTTPS(contents string) (bool, bool) {
	for _, line := range strings.Split(contents, "\n") {
		trimmed := strings.TrimSpace(line)
		switch trimmed {
		case `DRUPAL_ENABLE_HTTPS: "true"`:
			return true, true
		case `DRUPAL_ENABLE_HTTPS: "false"`:
			return false, true
		}
	}
	return false, false
}

func detectSelfManagedMode(projectDir string) string {
	rootCA := filepath.Join(projectDir, "certs", "rootCA.pem")
	rootCAKey := filepath.Join(projectDir, "certs", "rootCA-key.pem")
	if fileExists(rootCA) && fileExists(rootCAKey) {
		return ModeMkcert
	}
	return ModeSelfManaged
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func readDotEnv(path string) (map[string]string, error) {
	values := map[string]string{}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return values, nil
		}
		return nil, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		values[parts[0]] = strings.Trim(parts[1], `"`)
	}
	return values, nil
}

func updateEnvFile(path string, values map[string]string) error {
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	lines := []string{}
	if len(data) > 0 {
		lines = strings.Split(string(data), "\n")
	}

	for key, value := range values {
		updated := false
		for i, line := range lines {
			if strings.HasPrefix(line, key+"=") {
				lines[i] = fmt.Sprintf(`%s="%s"`, key, value)
				updated = true
				break
			}
		}
		if !updated {
			lines = append(lines, fmt.Sprintf(`%s="%s"`, key, value))
		}
	}

	return os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644)
}

func marshalCompose(doc map[string]any) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func cleanupEmptyService(services map[string]any, name string) {
	service := getMap(services, name)
	for _, key := range []string{"environment"} {
		if child := getMap(service, key); len(child) == 0 {
			delete(service, key)
		}
	}
	if len(service) == 0 {
		delete(services, name)
	}
}

func deleteKeys(values map[string]any, keys ...string) {
	for _, key := range keys {
		delete(values, key)
	}
}

func ensureMap(parent map[string]any, key string) map[string]any {
	if existing, ok := parent[key]; ok {
		if typed, ok := existing.(map[string]any); ok {
			return typed
		}
	}
	child := map[string]any{}
	parent[key] = child
	return child
}

func getMap(parent map[string]any, key string) map[string]any {
	if existing, ok := parent[key]; ok {
		if typed, ok := existing.(map[string]any); ok {
			return typed
		}
	}
	return map[string]any{}
}

func mapStringValue(values map[string]any, key string) (string, bool) {
	if raw, ok := values[key]; ok {
		return fmt.Sprintf("%v", raw), true
	}
	return "", false
}

func firstNonEmpty(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func ternary[T any](condition bool, whenTrue, whenFalse T) T {
	if condition {
		return whenTrue
	}
	return whenFalse
}

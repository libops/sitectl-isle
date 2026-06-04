package create

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	BotMitigationStateOn  = "on"
	BotMitigationStateOff = "off"

	captchaProtectCommand       = "--experimental.localPlugins.captcha-protect.modulename=github.com/libops/captcha-protect"
	captchaProtectPluginVolume  = "./conf/traefik/plugins/captcha-protect:/plugins-local/src/github.com/libops/captcha-protect:r"
	captchaProtectTemplateMount = "./conf/traefik/challenge.tmpl.html:/challenge.tmpl.html:ro"
	turnstileSiteKeyDefault     = "${TURNSTILE_SITE_KEY:-1x00000000000000000000AA}"
	turnstileSecretKeyDefault   = "${TURNSTILE_SECRET_KEY:-1x0000000000000000000000000000000AA}"
	captchaProtectSourceURL     = "https://github.com/libops/captcha-protect/archive/refs/tags/v1.12.3.zip"
	captchaProtectSourceSHA256  = "0492f8c2c5d951d499370a95d2232d7bc07581567e4d3c9348a848b172585b09"
)

var captchaProtectVolumes = []string{
	captchaProtectPluginVolume,
	captchaProtectTemplateMount,
}

//go:embed assets/bot-mitigation/*
var botMitigationAssets embed.FS

func ApplyBotMitigation(projectDir, state string) error {
	if projectDir == "" {
		projectDir = "."
	}
	switch state {
	case BotMitigationStateOn:
		return enableBotMitigation(projectDir)
	case BotMitigationStateOff:
		return disableBotMitigation(projectDir)
	default:
		return fmt.Errorf("invalid bot mitigation state %q: expected on or off", state)
	}
}

func enableBotMitigation(projectDir string) error {
	if err := updateComposeForBotMitigation(projectDir, true); err != nil {
		return err
	}
	if err := updateDrupalTraefikForBotMitigation(filepath.Join(projectDir, "conf", "traefik", "drupal.yml"), true); err != nil {
		return err
	}
	if err := ensureBotMitigationFiles(projectDir); err != nil {
		return err
	}
	return nil
}

func disableBotMitigation(projectDir string) error {
	if err := updateComposeForBotMitigation(projectDir, false); err != nil {
		return err
	}
	if err := updateDrupalTraefikForBotMitigation(filepath.Join(projectDir, "conf", "traefik", "drupal.yml"), false); err != nil {
		return err
	}
	return nil
}

func updateComposeForBotMitigation(projectDir string, enabled bool) error {
	path := filepath.Join(projectDir, "docker-compose.yml")
	data, err := os.ReadFile(path) // #nosec G304 -- compose file path is an explicit project configuration path.
	if err != nil {
		return fmt.Errorf("read compose file: %w", err)
	}

	lines := splitYAMLLines(string(data))
	traefikIdx, err := findComposeService(lines, "traefik")
	if err != nil {
		return err
	}

	if enabled {
		lines = ensureComposeCommandLine(lines, traefikIdx, captchaProtectCommand)
		traefikIdx, _ = findComposeService(lines, "traefik")
		for _, volume := range captchaProtectVolumes {
			lines = ensureComposeListItem(lines, traefikIdx, "volumes", volume)
			traefikIdx, _ = findComposeService(lines, "traefik")
		}
		lines = ensureComposeEnvLine(lines, traefikIdx, "TURNSTILE_SITE_KEY", turnstileSiteKeyDefault)
		traefikIdx, _ = findComposeService(lines, "traefik")
		lines = ensureComposeEnvLine(lines, traefikIdx, "TURNSTILE_SECRET_KEY", turnstileSecretKeyDefault)
	} else {
		lines = removeComposeServiceLines(lines, traefikIdx,
			captchaProtectCommand,
			captchaProtectPluginVolume,
			captchaProtectTemplateMount,
			"TURNSTILE_SITE_KEY:",
			"TURNSTILE_SECRET_KEY:",
		)
	}

	return os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600)
}

func ensureBotMitigationFiles(projectDir string) error {
	pluginDir := filepath.Join(projectDir, "conf", "traefik", "plugins", "captcha-protect")
	if err := installCaptchaProtectPlugin(pluginDir); err != nil {
		return err
	}

	templatePath := filepath.Join(projectDir, "conf", "traefik", "challenge.tmpl.html")
	if _, err := os.Stat(templatePath); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat challenge template: %w", err)
	}
	templateData, err := os.ReadFile(filepath.Join(pluginDir, "challenge.tmpl.html")) // #nosec G304 -- plugin source path is controlled by this installer.
	if err != nil {
		return fmt.Errorf("read captcha-protect default challenge template: %w", err)
	}
	return os.WriteFile(templatePath, templateData, 0o644)
}

func installCaptchaProtectPlugin(targetDir string) error {
	archiveData, err := downloadCaptchaProtectArchive(captchaProtectSourceURL)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(archiveData)
	if got := hex.EncodeToString(sum[:]); got != captchaProtectSourceSHA256 {
		return fmt.Errorf("captcha-protect archive sha256 mismatch: expected %s, got %s", captchaProtectSourceSHA256, got)
	}

	if err := os.MkdirAll(filepath.Dir(targetDir), 0o755); err != nil {
		return fmt.Errorf("create captcha-protect plugin parent directory: %w", err)
	}
	tmpDir, err := os.MkdirTemp(filepath.Dir(targetDir), ".captcha-protect-*")
	if err != nil {
		return fmt.Errorf("create temporary captcha-protect extraction directory: %w", err)
	}
	cleanupTemp := true
	defer func() {
		if cleanupTemp {
			_ = os.RemoveAll(tmpDir)
		}
	}()

	if err := extractCaptchaProtectArchive(archiveData, tmpDir); err != nil {
		return err
	}
	if err := os.RemoveAll(targetDir); err != nil {
		return fmt.Errorf("replace captcha-protect plugin directory: %w", err)
	}
	if err := os.Rename(tmpDir, targetDir); err != nil {
		return fmt.Errorf("install captcha-protect plugin source: %w", err)
	}
	cleanupTemp = false
	return nil
}

func downloadCaptchaProtectArchive(sourceURL string) ([]byte, error) {
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Get(sourceURL) // #nosec G107 -- source URL is a pinned constant with hash verification.
	if err != nil {
		return nil, fmt.Errorf("download captcha-protect archive: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download captcha-protect archive: unexpected HTTP status %s", resp.Status)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read captcha-protect archive: %w", err)
	}
	return data, nil
}

func extractCaptchaProtectArchive(archiveData []byte, targetDir string) error {
	reader, err := zip.NewReader(bytes.NewReader(archiveData), int64(len(archiveData)))
	if err != nil {
		return fmt.Errorf("open captcha-protect archive: %w", err)
	}

	for _, file := range reader.File {
		rel, ok := stripZipRoot(file.Name)
		if !ok {
			continue
		}
		if shouldSkipCaptchaProtectArchivePath(rel) {
			continue
		}
		targetPath := filepath.Join(targetDir, filepath.FromSlash(rel))
		cleanTarget := filepath.Clean(targetPath)
		cleanRoot := filepath.Clean(targetDir)
		if cleanTarget != cleanRoot && !strings.HasPrefix(cleanTarget, cleanRoot+string(os.PathSeparator)) {
			return fmt.Errorf("captcha-protect archive contains unsafe path %q", file.Name)
		}

		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(cleanTarget, 0o755); err != nil {
				return fmt.Errorf("create captcha-protect directory %s: %w", rel, err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(cleanTarget), 0o755); err != nil {
			return fmt.Errorf("create captcha-protect parent directory %s: %w", filepath.Dir(rel), err)
		}
		source, err := file.Open()
		if err != nil {
			return fmt.Errorf("open captcha-protect archive file %s: %w", rel, err)
		}
		if err := writeExtractedFile(cleanTarget, source, file.Mode()); err != nil {
			_ = source.Close()
			return err
		}
		if err := source.Close(); err != nil {
			return fmt.Errorf("close captcha-protect archive file %s: %w", rel, err)
		}
	}
	return nil
}

func shouldSkipCaptchaProtectArchivePath(rel string) bool {
	clean := pathCleanSlash(rel)
	if clean == "ci" || strings.HasPrefix(clean, "ci/") {
		return true
	}
	if clean == ".github" || strings.HasPrefix(clean, ".github/") {
		return true
	}
	if clean == "renovate.json5" {
		return true
	}
	return strings.HasSuffix(clean, "_test.go")
}

func stripZipRoot(name string) (string, bool) {
	clean := pathCleanSlash(name)
	if clean == "." || clean == "" {
		return "", false
	}
	parts := strings.SplitN(clean, "/", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[1]) == "" {
		return "", false
	}
	return parts[1], true
}

func pathCleanSlash(name string) string {
	return strings.TrimPrefix(filepath.ToSlash(filepath.Clean(filepath.FromSlash(name))), "/")
}

func writeExtractedFile(path string, source io.Reader, mode os.FileMode) error {
	target, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode.Perm())
	if err != nil {
		return fmt.Errorf("create captcha-protect archive file %s: %w", path, err)
	}
	defer target.Close()
	if _, err := io.Copy(target, source); err != nil {
		return fmt.Errorf("write captcha-protect archive file %s: %w", path, err)
	}
	return nil
}

func updateDrupalTraefikForBotMitigation(path string, enabled bool) error {
	data, err := os.ReadFile(path) // #nosec G304 -- traefik config path is an explicit project configuration path.
	if err != nil {
		return fmt.Errorf("read drupal traefik config: %w", err)
	}

	lines := splitYAMLLines(string(data))
	lines = removeCaptchaProtectMiddlewareReference(lines)
	lines = removeCaptchaProtectMiddlewareBlock(lines)

	if enabled {
		lines, err = ensureDrupalRouterCaptchaMiddleware(lines)
		if err != nil {
			return err
		}
		lines, err = ensureCaptchaProtectMiddlewareBlock(lines)
		if err != nil {
			return err
		}
	}

	return os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600)
}

func splitYAMLLines(data string) []string {
	trimmed := strings.TrimRight(data, "\r\n")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "\n")
}

func ensureDrupalRouterCaptchaMiddleware(lines []string) ([]string, error) {
	routerIdx, ok := findIndentedYAMLKey(lines, 0, "routers", 2)
	if !ok {
		return nil, fmt.Errorf("conf/traefik/drupal.yml does not define http.routers")
	}
	drupalIdx, ok := findIndentedYAMLKey(lines, routerIdx+1, "drupal", 4)
	if !ok {
		return nil, fmt.Errorf("conf/traefik/drupal.yml does not define http.routers.drupal")
	}

	drupalEnd := yamlBlockEnd(lines, drupalIdx, 4)
	middlewaresIdx, ok := findIndentedYAMLKeyBefore(lines, drupalIdx+1, drupalEnd, "middlewares", 6)
	if ok {
		insertAt := yamlBlockEnd(lines, middlewaresIdx, 6)
		return insertTextLines(lines, insertAt, []string{"        - captcha-protect"}), nil
	}

	block := []string{
		"      middlewares:",
		"        - captcha-protect",
	}
	return insertTextLines(lines, drupalEnd, block), nil
}

func ensureCaptchaProtectMiddlewareBlock(lines []string) ([]string, error) {
	middlewaresIdx, ok := findIndentedYAMLKey(lines, 0, "middlewares", 2)
	if !ok {
		httpIdx, httpOK := findIndentedYAMLKey(lines, 0, "http", 0)
		insertAt := len(lines)
		if httpOK {
			insertAt = yamlBlockEnd(lines, httpIdx, 0)
		}
		lines = insertTextLines(lines, insertAt, []string{"  middlewares:"})
		middlewaresIdx, _ = findIndentedYAMLKey(lines, 0, "middlewares", 2)
	}

	block, err := botMitigationAssetLines("captcha-protect-middleware.yml")
	if err != nil {
		return nil, err
	}
	insertAt := yamlBlockEnd(lines, middlewaresIdx, 2)
	return insertTextLines(lines, insertAt, block), nil
}

func removeCaptchaProtectMiddlewareReference(lines []string) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == "- captcha-protect" {
			continue
		}
		out = append(out, line)
	}
	return out
}

func removeCaptchaProtectMiddlewareBlock(lines []string) []string {
	idx, ok := findIndentedYAMLKey(lines, 0, "captcha-protect", 4)
	if !ok {
		return lines
	}
	end := yamlBlockEnd(lines, idx, 4)
	return append(lines[:idx], lines[end:]...)
}

func botMitigationAssetLines(name string) ([]string, error) {
	data, err := botMitigationAssets.ReadFile("assets/bot-mitigation/" + name)
	if err != nil {
		return nil, err
	}
	return splitYAMLLines(string(data)), nil
}

func findIndentedYAMLKey(lines []string, start int, key string, indent int) (int, bool) {
	return findIndentedYAMLKeyBefore(lines, start, len(lines), key, indent)
}

func findIndentedYAMLKeyBefore(lines []string, start, end int, key string, indent int) (int, bool) {
	prefix := strings.Repeat(" ", indent) + key + ":"
	for i := start; i < end && i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "" {
			continue
		}
		currentIndent := leadingYAMLSpaces(lines[i])
		if currentIndent < indent {
			if start > 0 {
				break
			}
			continue
		}
		if currentIndent == indent && strings.HasPrefix(lines[i], prefix) {
			return i, true
		}
	}
	return 0, false
}

func yamlBlockEnd(lines []string, start int, indent int) int {
	for i := start + 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "" {
			continue
		}
		if leadingYAMLSpaces(lines[i]) <= indent {
			return i
		}
	}
	return len(lines)
}

func leadingYAMLSpaces(line string) int {
	return len(line) - len(strings.TrimLeft(line, " "))
}

func insertTextLines(lines []string, index int, inserted []string) []string {
	out := make([]string, 0, len(lines)+len(inserted))
	out = append(out, lines[:index]...)
	out = append(out, inserted...)
	out = append(out, lines[index:]...)
	return out
}

func findComposeService(lines []string, service string) (int, error) {
	servicesIdx, ok := findIndentedYAMLKey(lines, 0, "services", 0)
	if !ok {
		return 0, fmt.Errorf("docker-compose.yml does not define services")
	}
	serviceIdx, ok := findIndentedYAMLKey(lines, servicesIdx+1, service, 2)
	if !ok {
		return 0, fmt.Errorf("docker-compose.yml does not define services.%s", service)
	}
	return serviceIdx, nil
}

func ensureComposeCommandLine(lines []string, serviceIdx int, command string) []string {
	serviceEnd := yamlBlockEnd(lines, serviceIdx, 2)
	commandIdx, ok := findIndentedYAMLKeyBefore(lines, serviceIdx+1, serviceEnd, "command", 4)
	if !ok {
		return insertTextLines(lines, serviceEnd, []string{
			"    command:",
			"      - " + command,
		})
	}
	commandEnd := yamlBlockEnd(lines, commandIdx, 4)
	for i := commandIdx + 1; i < commandEnd; i++ {
		if normalizeComposeListOrScalarLine(lines[i]) == command {
			return lines
		}
	}
	if strings.Contains(lines[commandIdx], ">") || strings.Contains(lines[commandIdx], "|") {
		return insertTextLines(lines, commandEnd, []string{"      " + command})
	}
	return insertTextLines(lines, commandEnd, []string{"      - " + command})
}

func ensureComposeListItem(lines []string, serviceIdx int, key, value string) []string {
	serviceEnd := yamlBlockEnd(lines, serviceIdx, 2)
	keyIdx, ok := findIndentedYAMLKeyBefore(lines, serviceIdx+1, serviceEnd, key, 4)
	if !ok {
		return insertTextLines(lines, serviceEnd, []string{
			"    " + key + ":",
			"      - " + value,
		})
	}
	keyEnd := yamlBlockEnd(lines, keyIdx, 4)
	for i := keyIdx + 1; i < keyEnd; i++ {
		if normalizeComposeListOrScalarLine(lines[i]) == value {
			return lines
		}
	}
	return insertTextLines(lines, keyEnd, []string{"      - " + value})
}

func ensureComposeEnvLine(lines []string, serviceIdx int, key, value string) []string {
	serviceEnd := yamlBlockEnd(lines, serviceIdx, 2)
	envIdx, ok := findIndentedYAMLKeyBefore(lines, serviceIdx+1, serviceEnd, "environment", 4)
	if !ok {
		return insertTextLines(lines, serviceEnd, []string{
			"    environment:",
			"      " + key + ": " + value,
		})
	}
	if strings.TrimSpace(lines[envIdx]) == "environment: {}" {
		lines[envIdx] = "    environment:"
	}
	envEnd := yamlBlockEnd(lines, envIdx, 4)
	prefix := key + ":"
	for i := envIdx + 1; i < envEnd; i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), prefix) {
			lines[i] = "      " + key + ": " + value
			return lines
		}
	}
	return insertTextLines(lines, envEnd, []string{"      " + key + ": " + value})
}

func removeComposeServiceLines(lines []string, serviceIdx int, values ...string) []string {
	remove := map[string]bool{}
	for _, value := range values {
		remove[value] = true
	}
	serviceEnd := yamlBlockEnd(lines, serviceIdx, 2)
	out := make([]string, 0, len(lines))
	for i, line := range lines {
		if i > serviceIdx && i < serviceEnd {
			normalized := normalizeComposeListOrScalarLine(line)
			if remove[normalized] {
				continue
			}
			trimmed := strings.TrimSpace(line)
			if remove[trimmed] {
				continue
			}
			if removeComposeLineByPrefix(trimmed, remove) {
				continue
			}
		}
		out = append(out, line)
	}
	return out
}

func removeComposeLineByPrefix(trimmed string, remove map[string]bool) bool {
	for value := range remove {
		if strings.HasSuffix(value, ":") && strings.HasPrefix(trimmed, value) {
			return true
		}
	}
	return false
}

func normalizeComposeListOrScalarLine(line string) string {
	trimmed := strings.TrimSpace(line)
	trimmed = strings.TrimPrefix(trimmed, "- ")
	return strings.TrimSpace(trimmed)
}

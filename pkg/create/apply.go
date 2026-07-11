package create

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	corecomponent "github.com/libops/sitectl/pkg/component"
	"github.com/libops/sitectl/pkg/config"
	coretraefik "github.com/libops/sitectl/pkg/services/traefik"
	"gopkg.in/yaml.v3"
)

const (
	FcrepoStateOn  = "on"
	FcrepoStateOff = "off"

	blazegraphDataVolumeName = "blazegraph-data"
	blazegraphImageRef       = "libops/blazegraph:2.1.5@sha256:3127324525a28f4905b56d24fa7e866c4bf4588f85f6f21df44ffc93b24666fc"
	blazegraphServiceName    = "blazegraph"

	IIIFCantaloupe       = "cantaloupe"
	IIIFTriplet          = "triplet"
	IIIFTopologyLocal    = "local"
	IIIFTopologyExternal = "external"

	DerivativeTopologyLocal       = "local"
	DerivativeTopologyDistributed = "distributed"

	CodebaseNested  = "nested"
	CodebaseGitRoot = "git-root"

	DefaultDrupalRootfs      = "drupal/rootfs/var/www/drupal"
	DefaultISLEFileSystemURI = "private"
	PublicISLEFileSystemURI  = "public"
	PrivateISLEFileSystemURI = "private"
	drupalFcrepoInternalURL  = "http://fcrepo:8080/fcrepo/rest/"

	drupalRouterName = "drupal"
	localDrupalHost  = "drupal.internal"
	// LocalDrupalBaseURL is the internal Traefik route used by local Fcrepo
	// clients that cannot reach the host machine's localhost.
	LocalDrupalBaseURL            = "http://" + localDrupalHost
	localDrupalHostRule           = "Host(`drupal.internal`)"
	localDrupalHostMiddlewareName = "drupal-internal-host"
	localDrupalRouterName         = "drupal-internal"
	localDrupalRouterPriority     = 9000
	workbenchClientRouterName     = "islandora-workbench-client"
	workbenchClientUserAgentRule  = "HeaderRegexp(`User-Agent`, `(?i)^Islandora Workbench$`)"
	workbenchClientRouterPriority = 100000
	defaultDrupalHostRule         = "Host(`localhost`)"
)

// DefaultTrustedHostPatterns is the Drupal trusted-host regex for local sites.
const DefaultTrustedHostPatterns = "^localhost$"

var fedoraCleanupFiles = []string{
	"context.context.all_media.yml",
	"context.context.external_files.yml",
	"context.context.repository_content.yml",
	"context.context.taxonomy_terms.yml",
	"system.action.delete_file_as_fedora_external_content.yml",
	"system.action.delete_node_from_fedora.yml",
	"system.action.delete_taxonomy_term_in_fedora.yml",
	"system.action.index_file_as_fedora_external_content.yml",
	"system.action.index_media_in_fedora.yml",
	"system.action.index_node_in_fedora.yml",
	"system.action.index_taxonomy_term_in_fedora.yml",
	"system.action.user_add_role_action.fedoraadmin.yml",
	"system.action.user_remove_role_action.fedoraadmin.yml",
	"user.role.fedoraadmin.yml",
	"views.view.non_fedora_files.yml",
}

var blazegraphCleanupFiles = []string{
	"system.action.delete_media_from_triplestore.yml",
	"system.action.delete_node_from_triplestore.yml",
	"system.action.delete_taxonomy_term_in_triplestore.yml",
	"system.action.index_media_in_triplestore.yml",
	"system.action.index_node_in_triplestore.yml",
	"system.action.index_taxonomy_term_in_the_triplestore.yml",
}

var mediaSchemeFiles = []string{
	"field.storage.media.field_media_audio_file.yml",
	"field.storage.media.field_media_document.yml",
	"field.storage.media.field_media_file.yml",
	"field.storage.media.field_media_image.yml",
	"field.storage.media.field_media_video_file.yml",
	"field.storage.media.field_track.yml",
}

type Options struct {
	Path               string
	DrupalRootfs       string
	Fcrepo             string
	Blazegraph         string
	IIIF               string
	IIIFTopology       string
	IIIFUpstreamURL    string
	BotMitigation      string
	ComposeOverride    string
	ISLEFileSystemURI  string
	DerivativeServices map[string]string
	Codebase           string
}

func Apply(opts Options) error {
	if opts.Path == "" {
		opts.Path = "."
	}
	if opts.DrupalRootfs == "" {
		opts.DrupalRootfs = DefaultDrupalRootfs
	}
	opts.DrupalRootfs = resolveProjectDrupalRootfs(opts.Path, opts.DrupalRootfs)
	if opts.Fcrepo == "" {
		opts.Fcrepo = FcrepoStateOn
	}
	if opts.Blazegraph == "" {
		opts.Blazegraph = FcrepoStateOn
	}
	if opts.IIIF == "" {
		opts.IIIF = IIIFCantaloupe
	}
	if opts.IIIFTopology == "" {
		opts.IIIFTopology = IIIFTopologyLocal
	}
	if opts.BotMitigation == "" {
		opts.BotMitigation = coretraefik.BotMitigationStateOff
	}
	if opts.ISLEFileSystemURI == "" {
		opts.ISLEFileSystemURI = DefaultISLEFileSystemURI
	}
	if opts.Codebase == "" {
		opts.Codebase = CodebaseNested
	}

	if opts.Fcrepo != FcrepoStateOn && opts.Fcrepo != FcrepoStateOff {
		return fmt.Errorf("invalid --fcrepo value %q: expected on or off", opts.Fcrepo)
	}
	if opts.Blazegraph != FcrepoStateOn && opts.Blazegraph != FcrepoStateOff {
		return fmt.Errorf("invalid --blazegraph value %q: expected on or off", opts.Blazegraph)
	}
	if opts.IIIF != IIIFCantaloupe && opts.IIIF != IIIFTriplet {
		return fmt.Errorf("invalid --iiif value %q: expected cantaloupe or triplet", opts.IIIF)
	}
	if opts.IIIFTopology != IIIFTopologyLocal && opts.IIIFTopology != IIIFTopologyExternal {
		return fmt.Errorf("invalid --iiif-topology value %q: expected local or external", opts.IIIFTopology)
	}
	if opts.BotMitigation != coretraefik.BotMitigationStateOn && opts.BotMitigation != coretraefik.BotMitigationStateOff {
		return fmt.Errorf("invalid --bot-mitigation value %q: expected on or off", opts.BotMitigation)
	}
	if opts.IIIFTopology == IIIFTopologyExternal && strings.TrimSpace(opts.IIIFUpstreamURL) == "" {
		return fmt.Errorf("invalid --iiif-upstream-url value %q: expected a non-empty upstream URL when --iiif-topology=external", opts.IIIFUpstreamURL)
	}
	if strings.TrimSpace(opts.ISLEFileSystemURI) == "" {
		return fmt.Errorf("invalid --isle-file-system-uri value %q: expected a non-empty filesystem URI", opts.ISLEFileSystemURI)
	}
	if opts.Codebase != CodebaseNested && opts.Codebase != CodebaseGitRoot {
		return fmt.Errorf("invalid --codebase value %q: expected nested or git-root", opts.Codebase)
	}

	if opts.Codebase == CodebaseGitRoot {
		if err := applyCodebaseGitRoot(opts.Path); err != nil {
			return fmt.Errorf("apply codebase=git-root: %w", err)
		}
		opts.DrupalRootfs = corecomponent.DefaultDrupalRootfs
	}

	if opts.Fcrepo == FcrepoStateOn {
		if err := applyFcrepoOn(opts.Path, opts.DrupalRootfs); err != nil {
			return fmt.Errorf("apply fcrepo=on: %w", err)
		}
	} else {
		if err := applyFcrepoOff(opts.Path, opts.DrupalRootfs, opts.ISLEFileSystemURI); err != nil {
			return fmt.Errorf("apply fcrepo=off: %w", err)
		}
	}

	if opts.Blazegraph == FcrepoStateOn {
		if err := applyBlazegraphOn(opts.Path); err != nil {
			return fmt.Errorf("apply blazegraph=on: %w", err)
		}
	} else {
		if err := applyBlazegraphOff(opts.Path, opts.DrupalRootfs); err != nil {
			return fmt.Errorf("apply blazegraph=off: %w", err)
		}
	}

	if err := ApplyIIIF(opts); err != nil {
		return fmt.Errorf("apply iiif=%s topology=%s: %w", opts.IIIF, opts.IIIFTopology, err)
	}
	if len(opts.DerivativeServices) > 0 {
		if err := ApplyDerivativeServices(opts); err != nil {
			return fmt.Errorf("apply derivative services: %w", err)
		}
	}
	if opts.BotMitigation == coretraefik.BotMitigationStateOn {
		if err := ApplyBotMitigation(opts.Path, opts.BotMitigation); err != nil {
			return fmt.Errorf("apply bot-mitigation=%s: %w", opts.BotMitigation, err)
		}
	}
	if err := SyncLocalDrupalInternalIngress(opts.Path, opts.Fcrepo == FcrepoStateOn); err != nil {
		return fmt.Errorf("sync local Drupal ingress: %w", err)
	}

	return nil
}

func resolveProjectDrupalRootfs(projectDir, drupalRootfs string) string {
	root := strings.TrimSpace(drupalRootfs)
	if root == "" {
		root = DefaultDrupalRootfs
	}
	if filepath.IsAbs(root) || root != DefaultDrupalRootfs {
		return root
	}
	if drupalLayoutExists(projectDir, root) {
		return root
	}
	for _, candidate := range []string{"drupal", corecomponent.DefaultDrupalRootfs} {
		if candidate == root {
			continue
		}
		if drupalLayoutExists(projectDir, candidate) {
			return candidate
		}
	}
	return root
}

func drupalLayoutExists(projectDir, drupalRootfs string) bool {
	layout := corecomponent.ResolveDrupalLayout(projectDir, drupalRootfs)
	for _, path := range []string{
		layout.ConfigSyncDir(),
		layout.ComposerJSONPath(),
		filepath.Join(layout.Root, "web", "robots.txt"),
	} {
		if _, err := os.Stat(path); err == nil {
			return true
		}
	}
	return false
}

func BotMitigationOptions() coretraefik.BotMitigationOptions {
	return coretraefik.BotMitigationOptions{
		RouterName:       "drupal",
		RouterConfigPath: "conf/traefik/drupal.yml",
	}
}

func ApplyBotMitigation(projectDir, state string) error {
	if err := coretraefik.ApplyBotMitigation(projectDir, state, BotMitigationOptions()); err != nil {
		return err
	}
	return updateWorkbenchClientBypass(projectDir, state == coretraefik.BotMitigationStateOn)
}

func SyncBotMitigationBypass(projectDir string) error {
	return SyncBotMitigationBypassContext(localProjectContext(projectDir))
}

func SyncBotMitigationBypassContext(ctx *config.Context) error {
	if ctx == nil {
		return fmt.Errorf("context is nil")
	}
	path := ctx.ResolveProjectPath(filepath.FromSlash(BotMitigationOptions().RouterConfigPath))
	exists, err := ctx.FileExists(path)
	if err != nil {
		return fmt.Errorf("stat bot mitigation router config: %w", err)
	}
	if !exists {
		return nil
	}
	data, err := ctx.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read bot mitigation router config: %w", err)
	}
	return updateWorkbenchClientBypassContext(ctx, strings.Contains(string(data), "captcha-protect"))
}

func SyncLocalDrupalInternalIngress(projectDir string, enabled bool) error {
	return SyncLocalDrupalInternalIngressContext(localProjectContext(projectDir), enabled)
}

func SyncLocalDrupalInternalIngressContext(ctx *config.Context, enabled bool) error {
	if ctx == nil {
		return fmt.Errorf("context is nil")
	}
	routerPath := ctx.ResolveProjectPath(filepath.FromSlash(BotMitigationOptions().RouterConfigPath))
	exists, err := ctx.FileExists(routerPath)
	if err != nil {
		return fmt.Errorf("stat local Drupal router config: %w", err)
	}
	if !exists {
		return nil
	}
	if err := syncLocalDrupalTraefikAliasContext(ctx, enabled); err != nil {
		return err
	}
	return syncLocalDrupalRouterContext(ctx, enabled)
}

func syncLocalDrupalTraefikAliasContext(ctx *config.Context, enabled bool) error {
	path := ctx.ResolveProjectPath("docker-compose.yml")
	exists, err := ctx.FileExists(path)
	if err != nil {
		return fmt.Errorf("stat docker-compose.yml: %w", err)
	}
	if !exists {
		return nil
	}
	data, err := ctx.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read docker-compose.yml: %w", err)
	}
	var root map[string]any
	if err := yaml.Unmarshal(data, &root); err != nil {
		return fmt.Errorf("parse docker-compose.yml: %w", err)
	}
	services := yamlMap(root["services"])
	traefik := yamlMap(services["traefik"])
	if traefik == nil {
		return nil
	}
	networks := yamlMap(traefik["networks"])
	defaultNetwork := yamlMap(networks["default"])
	if defaultNetwork == nil && !enabled {
		return nil
	}
	aliases := stringSlice(defaultNetwork["aliases"])
	aliases = setStringPresent(aliases, localDrupalHost, enabled)
	if !enabled && len(aliases) == len(stringSlice(defaultNetwork["aliases"])) {
		return nil
	}
	doc, err := corecomponent.LoadYAMLDocument(data)
	if err != nil {
		return fmt.Errorf("load docker-compose.yml: %w", err)
	}
	if err := doc.SetValue(".services.traefik.networks.default.aliases", aliases); err != nil {
		return fmt.Errorf("set Traefik local Drupal alias: %w", err)
	}
	updated, err := doc.Bytes()
	if err != nil {
		return fmt.Errorf("marshal docker-compose.yml: %w", err)
	}
	return ctx.WriteFile(path, updated)
}

func syncLocalDrupalRouterContext(ctx *config.Context, enabled bool) error {
	path := ctx.ResolveProjectPath(filepath.FromSlash(BotMitigationOptions().RouterConfigPath))
	exists, err := ctx.FileExists(path)
	if err != nil {
		return fmt.Errorf("stat local Drupal router config: %w", err)
	}
	if !exists {
		return nil
	}
	data, err := ctx.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read local Drupal router config: %w", err)
	}
	doc, err := corecomponent.LoadYAMLDocument(data)
	if err != nil {
		return fmt.Errorf("parse local Drupal router config: %w", err)
	}
	routerPath := ".http.routers." + localDrupalRouterName
	if !enabled {
		if err := doc.DeletePath(routerPath); err != nil {
			return err
		}
		if err := doc.DeletePath(".http.middlewares." + localDrupalHostMiddlewareName); err != nil {
			return err
		}
		if err := deletePathIfEmptyYAMLMap(doc, ".http.middlewares"); err != nil {
			return err
		}
		updated, err := doc.Bytes()
		if err != nil {
			return fmt.Errorf("marshal local Drupal router config: %w", err)
		}
		return ctx.WriteFile(path, updated)
	}
	router, err := localDrupalRouter(data)
	if err != nil {
		return err
	}
	if err := doc.SetValue(routerPath, router); err != nil {
		return err
	}
	if err := doc.DeletePath(".http.middlewares." + localDrupalHostMiddlewareName); err != nil {
		return err
	}
	if err := deletePathIfEmptyYAMLMap(doc, ".http.middlewares"); err != nil {
		return err
	}
	updated, err := doc.Bytes()
	if err != nil {
		return fmt.Errorf("marshal local Drupal router config: %w", err)
	}
	return ctx.WriteFile(path, updated)
}

func updateWorkbenchClientBypass(projectDir string, enabled bool) error {
	return updateWorkbenchClientBypassContext(localProjectContext(projectDir), enabled)
}

func updateWorkbenchClientBypassContext(ctx *config.Context, enabled bool) error {
	path := ctx.ResolveProjectPath(filepath.FromSlash(BotMitigationOptions().RouterConfigPath))
	exists, err := ctx.FileExists(path)
	if err != nil {
		return fmt.Errorf("stat bot mitigation router config: %w", err)
	}
	if !exists {
		return nil
	}
	data, err := ctx.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read bot mitigation router config: %w", err)
	}
	doc, err := corecomponent.LoadYAMLDocument(data)
	if err != nil {
		return fmt.Errorf("parse bot mitigation router config: %w", err)
	}
	routerPath := ".http.routers." + workbenchClientRouterName
	if !enabled {
		if err := doc.DeletePath(routerPath); err != nil {
			return err
		}
		updated, err := doc.Bytes()
		if err != nil {
			return fmt.Errorf("marshal bot mitigation router config: %w", err)
		}
		return ctx.WriteFile(path, updated)
	}
	router, err := workbenchClientRouter(data)
	if err != nil {
		return err
	}
	if err := doc.SetValue(routerPath, router); err != nil {
		return err
	}
	updated, err := doc.Bytes()
	if err != nil {
		return fmt.Errorf("marshal bot mitigation router config: %w", err)
	}
	return ctx.WriteFile(path, updated)
}

func localProjectContext(projectDir string) *config.Context {
	return &config.Context{
		DockerHostType: config.ContextLocal,
		ProjectDir:     projectDir,
	}
}

func workbenchClientRouter(data []byte) (map[string]any, error) {
	source, err := drupalRouter(data)
	if err != nil {
		return nil, err
	}
	rule := strings.TrimSpace(stringMapValue(source, "rule"))
	if rule == "" {
		rule = defaultDrupalHostRule
	}
	router := map[string]any{
		"rule":     "(" + rule + ") && " + workbenchClientUserAgentRule,
		"service":  "drupal",
		"priority": workbenchClientRouterPriority,
	}
	for _, key := range []string{"entryPoints", "tls"} {
		if value, ok := source[key]; ok {
			router[key] = value
		}
	}
	return router, nil
}

func localDrupalRouter(data []byte) (map[string]any, error) {
	source, err := drupalRouter(data)
	if err != nil {
		return nil, err
	}
	router := map[string]any{
		"rule":     localDrupalHostRule,
		"service":  drupalRouterName,
		"priority": localDrupalRouterPriority,
	}
	for _, key := range []string{"entryPoints", "tls"} {
		if value, ok := source[key]; ok {
			router[key] = value
		}
	}
	return router, nil
}

func drupalRouter(data []byte) (map[string]any, error) {
	var root map[string]any
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("parse Traefik router config: %w", err)
	}
	httpMap := yamlMap(root["http"])
	routers := yamlMap(httpMap["routers"])
	router := yamlMap(routers["drupal"])
	if router == nil {
		return map[string]any{}, nil
	}
	return router, nil
}

func stringSlice(value any) []string {
	switch typed := value.(type) {
	case []string:
		return append([]string{}, typed...)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if item != nil {
				out = append(out, fmt.Sprint(item))
			}
		}
		return out
	default:
		return nil
	}
}

func setStringPresent(values []string, target string, present bool) []string {
	out := make([]string, 0, len(values)+1)
	found := false
	for _, value := range values {
		if value == target {
			found = true
			if present {
				out = append(out, value)
			}
			continue
		}
		out = append(out, value)
	}
	if present && !found {
		out = append(out, target)
	}
	return out
}

func yamlMap(value any) map[string]any {
	switch typed := value.(type) {
	case map[string]any:
		return typed
	case map[any]any:
		out := make(map[string]any, len(typed))
		for key, value := range typed {
			out[fmt.Sprint(key)] = value
		}
		return out
	default:
		return nil
	}
}

func deletePathIfEmptyYAMLMap(doc *corecomponent.YAMLDocument, path string) error {
	if doc == nil {
		return nil
	}
	data, err := doc.Bytes()
	if err != nil {
		return err
	}
	var root any
	if err := yaml.Unmarshal(data, &root); err != nil {
		return err
	}
	current := yamlMap(root)
	for _, segment := range strings.Split(strings.TrimPrefix(path, "."), ".") {
		if current == nil {
			return nil
		}
		next, ok := current[segment]
		if !ok {
			return nil
		}
		current = yamlMap(next)
	}
	if len(current) != 0 {
		return nil
	}
	return doc.DeletePath(path)
}

func stringMapValue(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	value, ok := values[key]
	if !ok || value == nil {
		return ""
	}
	switch value := value.(type) {
	case string:
		return value
	default:
		return fmt.Sprint(value)
	}
}

func applyCodebaseGitRoot(projectDir string) error {
	projectDir = filepath.Clean(projectDir)
	if err := moveIfExists(filepath.Join(projectDir, "drupal", "Dockerfile"), filepath.Join(projectDir, "Dockerfile")); err != nil {
		return err
	}
	if err := moveIfExists(filepath.Join(projectDir, "drupal", ".dockerignore"), filepath.Join(projectDir, ".dockerignore")); err != nil {
		return err
	}
	if err := moveDrupalCodebaseContents(filepath.Join(projectDir, "drupal"), projectDir); err != nil {
		return err
	}
	if err := moveDirectoryContents(filepath.Join(projectDir, "drupal", "rootfs", "var", "www", "drupal"), projectDir, false); err != nil {
		return err
	}
	if err := rewriteGitRootDockerfile(filepath.Join(projectDir, "Dockerfile")); err != nil {
		return err
	}
	if err := writeGitRootDockerignore(filepath.Join(projectDir, ".dockerignore")); err != nil {
		return err
	}
	if err := rewriteCodebaseComposePaths(filepath.Join(projectDir, "docker-compose.yml")); err != nil {
		return err
	}
	if err := rewriteCodebaseDevComposePaths(filepath.Join(projectDir, "docker-compose.dev.yml")); err != nil {
		return err
	}
	return nil
}

func moveDrupalCodebaseContents(sourceDir, targetDir string) error {
	entries, err := os.ReadDir(sourceDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read %s: %w", sourceDir, err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") || name == "Dockerfile" || name == "README.md" || name == "rootfs" {
			continue
		}
		source := filepath.Join(sourceDir, name)
		target := filepath.Join(targetDir, name)
		if err := os.Rename(source, target); err != nil {
			return fmt.Errorf("move %s to %s: %w", source, target, err)
		}
	}
	return nil
}

func moveIfExists(source, target string) error {
	if _, err := os.Stat(source); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat %s: %w", source, err)
	}
	if err := os.Rename(source, target); err != nil {
		return fmt.Errorf("move %s to %s: %w", source, target, err)
	}
	return nil
}

func moveDirectoryContents(sourceDir, targetDir string, includeDotfiles bool) error {
	entries, err := os.ReadDir(sourceDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read %s: %w", sourceDir, err)
	}
	for _, entry := range entries {
		if !includeDotfiles && strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		source := filepath.Join(sourceDir, entry.Name())
		target := filepath.Join(targetDir, entry.Name())
		if err := os.Rename(source, target); err != nil {
			return fmt.Errorf("move %s to %s: %w", source, target, err)
		}
	}
	return nil
}

func rewriteGitRootDockerfile(path string) error {
	data, err := os.ReadFile(path) // #nosec G304 -- Dockerfile path is scoped to the selected project.
	if err != nil {
		return fmt.Errorf("read Dockerfile: %w", err)
	}
	if strings.Contains(string(data), "COPY --link composer.json composer.lock /var/www/drupal/") &&
		strings.Contains(string(data), "COPY --link drupal/rootfs/opt/ /opt/") {
		return nil
	}

	header := dockerfileHeader(string(data))
	contents := strings.TrimRight(header, "\n") + `

COPY --link composer.json composer.lock /var/www/drupal/
COPY --link assets/ /var/www/drupal/assets/

RUN --mount=type=cache,id=custom-drupal-composer-${TARGETARCH},sharing=locked,target=/root/.composer/cache \
    composer install -d /var/www/drupal --no-interaction --no-progress --prefer-dist --no-dev --optimize-autoloader && \
    cleanup.sh

COPY --link config/ /var/www/drupal/config/
COPY --link recipes/ /var/www/drupal/recipes/
COPY --link web/modules/custom/ /var/www/drupal/web/modules/custom/
COPY --link web/themes/custom/ /var/www/drupal/web/themes/custom/
COPY --link drupal/rootfs/opt/ /opt/

RUN chown -R nginx:nginx /var/www/drupal && \
    cleanup.sh
`
	return writeFilePreserveMode(path, []byte(contents))
}

func dockerfileHeader(contents string) string {
	lines := strings.Split(contents, "\n")
	out := []string{}
	foundTargetArch := false
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "# syntax=") {
			continue
		}
		out = append(out, line)
		if strings.TrimSpace(line) == "ARG TARGETARCH" {
			foundTargetArch = true
			break
		}
	}
	if foundTargetArch {
		header := strings.Join(out, "\n")
		if strings.Contains(header, "FROM ${REPOSITORY}/drupal:${TAG}") {
			return `ARG BASE_IMAGE=libops/islandora:nginx-1.30.3-php84
FROM ${BASE_IMAGE}

ARG TARGETARCH`
		}
		return header
	}
	return `ARG BASE_IMAGE=libops/islandora:nginx-1.30.3-php84
FROM ${BASE_IMAGE}

ARG TARGETARCH

ENV \
    COMPOSER_ALLOW_SUPERUSER=1 \
    COMPOSER_MEMORY_LIMIT=-1
WORKDIR /var/www/drupal`
}

func writeGitRootDockerignore(path string) error {
	contents := strings.Join([]string{
		".git",
		".cache",
		"certs",
		"secrets",
		"vendor",
		"web/core",
		"web/modules/contrib",
		"web/themes/contrib",
		"drupal/rootfs/var/www/drupal",
		"",
	}, "\n")
	return writeFilePreserveMode(path, []byte(contents))
}

func rewriteCodebaseComposePaths(path string) error {
	replacements := map[string]string{
		"context: ./drupal":     "context: .",
		"- ./drupal:/drupal:rw": "- .:/drupal:rw",
	}
	return replaceInFile(path, replacements)
}

func rewriteCodebaseDevComposePaths(path string) error {
	return replaceInFile(path, map[string]string{
		"./drupal/rootfs/var/www/drupal/": "./",
	})
}

func replaceInFile(path string, replacements map[string]string) error {
	data, err := os.ReadFile(path) // #nosec G304 -- file path is scoped to the selected project.
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read %s: %w", path, err)
	}
	updated := string(data)
	for old, replacement := range replacements {
		updated = strings.ReplaceAll(updated, old, replacement)
	}
	if updated == string(data) {
		return nil
	}
	return writeFilePreserveMode(path, []byte(updated))
}

func writeFilePreserveMode(path string, data []byte) error {
	mode := fs.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat %s: %w", path, err)
	}
	return os.WriteFile(path, data, mode) // #nosec G306 -- generated project files are non-secret.
}

func applyFcrepoOn(projectDir, drupalRootfs string) error {
	composePath := filepath.Join(projectDir, "docker-compose.yml")
	compose, err := corecomponent.LoadComposeFile(composePath)
	if err != nil {
		return err
	}
	if err := ensureFcrepoDatabaseInitScript(projectDir); err != nil {
		return err
	}
	images := inferFcrepoRestoreImages(compose)
	commonMerge := commonMergeLine(composePath)
	secretBlocks := []struct {
		name  string
		block string
	}{
		{name: "FCREPO_DB_PASSWORD", block: `  FCREPO_DB_PASSWORD:
    file: "./secrets/FCREPO_DB_PASSWORD"`},
		{name: "TOMCAT_ADMIN_PASSWORD", block: `  TOMCAT_ADMIN_PASSWORD:
    file: "./secrets/TOMCAT_ADMIN_PASSWORD"`},
	}
	for _, secret := range secretBlocks {
		if err := compose.AddSectionEntryBlock("secrets", secret.name, secret.block); err != nil {
			return err
		}
	}
	if !compose.HasVolume("fcrepo-data") {
		if err := compose.AddVolumeBlock("fcrepo-data", "  fcrepo-data: {}"); err != nil {
			return err
		}
	}
	if !compose.HasService("fcrepo") {
		if err := compose.AddServiceBlock("fcrepo", fcrepoRestoreServiceBlock(images.Fcrepo, commonMerge)); err != nil {
			return err
		}
	}
	if !compose.HasService("fcrepo-database-init") {
		if err := compose.AddServiceBlock("fcrepo-database-init", fcrepoDatabaseInitServiceBlock(databaseInitImage(compose))); err != nil {
			return err
		}
	}
	if !compose.HasService("milliner") {
		if err := compose.AddServiceBlock("milliner", millinerRestoreServiceBlock(images.Milliner, commonMerge)); err != nil {
			return err
		}
	}
	for key, value := range map[string]string{
		"DRUPAL_DEFAULT_FCREPO_HOST": "fcrepo",
		"DRUPAL_DEFAULT_FCREPO_PORT": "8080",
		"DRUPAL_DEFAULT_FCREPO_URL":  drupalFcrepoInternalURL,
	} {
		if err := compose.SetServiceEnv("drupal", key, value); err != nil {
			return err
		}
	}
	if shouldUseLocalDrupalInternalURL(serviceEnvValue(compose, "drupal", "DRUPAL_DEFAULT_SITE_URL")) {
		if err := compose.SetServiceEnv("drupal", "DRUPAL_DEFAULT_SITE_URL", publicSiteURLExpr); err != nil {
			return err
		}
		if err := compose.SetServiceEnv("drupal", "DRUSH_OPTIONS_URI", LocalDrupalBaseURL); err != nil {
			return err
		}
		if err := compose.SetServiceEnv("drupal", "DRUPAL_TRUSTED_HOST_PATTERNS", TrustedHostPatterns(coretraefik.DefaultIngressDomain, true)); err != nil {
			return err
		}
	}
	if err := compose.SetServiceEnv("fcrepo", "FCREPO_ALLOW_EXTERNAL_DRUPAL", fcrepoAllowedDrupalURL(firstServiceEnvValue(compose, "drupal", "DRUSH_OPTIONS_URI", "DRUPAL_DEFAULT_SITE_URL"))); err != nil {
		return err
	}
	if err := compose.SetServiceEnv("alpaca", "ALPACA_FCREPO_INDEXER_ENABLED", "true"); err != nil {
		return err
	}
	if err := compose.Save(); err != nil {
		return err
	}
	if err := restoreFcrepoTraefikRoute(projectDir); err != nil {
		return err
	}
	configDir := corecomponent.ResolveDrupalLayout(projectDir, drupalRootfs).ConfigSyncDir()
	return restoreFcrepoDrupalConfig(configDir)
}

func ensureFcrepoDatabaseInitScript(projectDir string) error {
	path := filepath.Join(projectDir, "scripts", "init-database.sh")
	info, err := os.Lstat(path)
	if err == nil {
		if info.IsDir() {
			return fmt.Errorf("fcrepo database initializer %s is a directory", path)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("fcrepo database initializer %s is not a regular file", path)
		}
		mode := info.Mode().Perm() | 0o111
		if mode == info.Mode().Perm() {
			return nil
		}
		if err := os.Chmod(path, mode); err != nil {
			return fmt.Errorf("make fcrepo database initializer executable: %w", err)
		}
		return nil
	}
	if !os.IsNotExist(err) {
		return fmt.Errorf("inspect fcrepo database initializer: %w", err)
	}

	data, err := readFcrepoAsset("init-database.sh")
	if err != nil {
		return fmt.Errorf("read fcrepo database initializer asset: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { // #nosec G301 -- generated project script directories must be traversable by the compose stack.
		return fmt.Errorf("create fcrepo database initializer directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o755) // #nosec G304,G306 -- the project-scoped non-secret script must be executable inside the compose service.
	if err != nil {
		if os.IsExist(err) {
			return ensureFcrepoDatabaseInitScript(projectDir)
		}
		return fmt.Errorf("create fcrepo database initializer: %w", err)
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return fmt.Errorf("write fcrepo database initializer: %w", err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("close fcrepo database initializer: %w", err)
	}
	if err := os.Chmod(path, 0o755); err != nil {
		return fmt.Errorf("make generated fcrepo database initializer executable: %w", err)
	}
	return nil
}

func restoreFcrepoTraefikRoute(projectDir string) error {
	data, err := readFcrepoAsset("traefik.yml")
	if err != nil {
		return fmt.Errorf("read fcrepo Traefik route asset: %w", err)
	}
	if err := writeProjectFile(projectDir, "conf/traefik/fcrepo.yml", string(data)); err != nil {
		return fmt.Errorf("restore fcrepo Traefik route: %w", err)
	}
	return nil
}

func removeFcrepoTraefikRoute(projectDir string) error {
	path := filepath.Join(projectDir, "conf", "traefik", "fcrepo.yml")
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove fcrepo Traefik route: %w", err)
	}
	return nil
}

func restoreFcrepoDrupalConfig(configDir string) error {
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	for _, name := range fedoraCleanupFiles {
		data, err := readFcrepoAsset(name)
		if err != nil {
			return fmt.Errorf("read fcrepo config asset %s: %w", name, err)
		}
		if err := os.WriteFile(filepath.Join(configDir, name), data, 0o644); err != nil { // #nosec G306 -- generated Drupal config is non-secret project configuration.
			return fmt.Errorf("write %s: %w", name, err)
		}
	}
	return nil
}

type fcrepoRestoreImages struct {
	Fcrepo   string
	Milliner string
}

func inferFcrepoRestoreImages(compose *corecomponent.ComposeFile) fcrepoRestoreImages {
	repository, tag := inferIslandoraStackImageConvention(compose)
	fcrepoImage := repository + "/fcrepo6:" + tag
	if repository == "libops" {
		fcrepoImage = "libops/fcrepo:7@sha256:d4d0fd92424e751199ee87117f85a99b147ef92d0b65544794184e0a52cb4db3"
	}
	return fcrepoRestoreImages{
		Fcrepo:   fcrepoImage,
		Milliner: "islandora/milliner:main@sha256:b8032d819de5412d0a4db6a8ac8d5dd3a61b2e097af0a707d0ae4fcd03f22ca2",
	}
}

func inferIslandoraStackImageConvention(compose *corecomponent.ComposeFile) (string, string) {
	repository := "islandora"
	tag := "main"
	for _, service := range []string{"fcrepo", "activemq", "alpaca", "init", "fits", "solr", "mariadb", "milliner"} {
		block, ok := compose.ServiceBlock(service)
		if !ok {
			continue
		}
		ref, ok := composeImageRef(block)
		if !ok {
			continue
		}
		if repo := imageRepository(ref); repo == "libops" || repo == "islandora" {
			repository = repo
		}
		if value := imageTag(ref); value != "" {
			tag = value
		}
		return repository, tag
	}
	return repository, tag
}

func composeImageRef(serviceBlock string) (string, bool) {
	for _, line := range strings.Split(serviceBlock, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "image:") {
			continue
		}
		ref := strings.TrimSpace(strings.TrimPrefix(trimmed, "image:"))
		ref = strings.Trim(ref, `"'`)
		if ref != "" {
			return ref, true
		}
	}
	return "", false
}

func imageRepository(ref string) string {
	left, _, ok := strings.Cut(ref, "/")
	if !ok {
		return ""
	}
	return strings.TrimSpace(left)
}

func imageTag(ref string) string {
	beforeDigest, _, _ := strings.Cut(strings.TrimSpace(ref), "@")
	slash := strings.LastIndex(beforeDigest, "/")
	colon := strings.LastIndex(beforeDigest, ":")
	if colon <= slash || colon == len(beforeDigest)-1 {
		return ""
	}
	return beforeDigest[colon+1:]
}

func fcrepoRestoreServiceBlock(image, commonMerge string) string {
	return `  fcrepo:
` + commonMerge + `    depends_on:
      activemq:
        condition: service_healthy
      fcrepo-database-init:
        condition: service_completed_successfully
    environment:
      DB_HOST: mariadb
      DB_PORT: 3306
      FCREPO_ALLOW_EXTERNAL_DEFAULT: http://default/
      FCREPO_ALLOW_EXTERNAL_DRUPAL: http://localhost/
      FCREPO_PERSISTENCE_TYPE: mysql
    image: ` + image + `
    secrets:
      - source: FCREPO_DB_PASSWORD
        target: DB_PASSWORD
      - source: TOMCAT_ADMIN_PASSWORD
      - source: JWT_ADMIN_TOKEN
      - source: JWT_PUBLIC_KEY
    volumes:
      - fcrepo-data:/data:rw`
}

func databaseInitImage(compose *corecomponent.ComposeFile) string {
	if block, ok := compose.ServiceBlock("database-init"); ok {
		if image, ok := composeImageRef(block); ok {
			return image
		}
	}
	return "libops/base:3"
}

func fcrepoDatabaseInitServiceBlock(image string) string {
	return `  fcrepo-database-init:
    image: ` + image + `
    restart: "no"
    networks:
      default:
    environment:
      DB_HOST: mariadb
      DB_PORT: "3306"
      DB_NAME: fcrepo
      DB_USER: fcrepo
      DB_CHARACTER_SET: utf8mb4
      DB_COLLATION: utf8mb4_unicode_ci
    secrets:
      - source: DB_ROOT_PASSWORD
      - source: FCREPO_DB_PASSWORD
        target: DB_PASSWORD
    volumes:
      - ./scripts/init-database.sh:/usr/local/bin/init-database.sh:ro,z
    entrypoint: /usr/local/bin/init-database.sh
    depends_on:
      mariadb:
        condition: service_healthy`
}

func millinerRestoreServiceBlock(image, commonMerge string) string {
	return `  milliner:
` + commonMerge + `    environment:
      MILLINER_FEDORA6: true
    image: ` + image + `
    secrets:
      - source: CERT_PUBLIC_KEY
      - source: CERT_AUTHORITY
      - source: JWT_ADMIN_TOKEN
      - source: JWT_PUBLIC_KEY`
}

func applyBlazegraphOn(projectDir string) error {
	composePath := filepath.Join(projectDir, "docker-compose.yml")
	compose, err := corecomponent.LoadComposeFile(composePath)
	if err != nil {
		return err
	}
	if !compose.HasVolume(blazegraphDataVolumeName) {
		if err := compose.AddVolumeBlock(blazegraphDataVolumeName, "  "+blazegraphDataVolumeName+": {}"); err != nil {
			return err
		}
	}
	if !compose.HasService(blazegraphServiceName) {
		block, err := blazegraphRestoreServiceBlock(composePath)
		if err != nil {
			return err
		}
		if err := compose.AddServiceBlock(blazegraphServiceName, block); err != nil {
			return err
		}
	}
	if err := compose.SetServiceScalar(blazegraphServiceName, "image", blazegraphImageRef); err != nil {
		return err
	}
	if err := compose.SetServiceEnv("alpaca", "ALPACA_TRIPLESTORE_INDEXER_ENABLED", "true"); err != nil {
		return err
	}
	if err := compose.SetServiceEnv("drupal", "DRUPAL_DEFAULT_TRIPLESTORE_NAMESPACE", "islandora"); err != nil {
		return err
	}
	return compose.Save()
}

func blazegraphRestoreServiceBlock(composePath string) (string, error) {
	return renderApplyAsset("blazegraph-service.yml", map[string]string{
		"BLAZEGRAPH_IMAGE": blazegraphImageRef,
		"COMMON_MERGE":     commonMergeLine(composePath),
	})
}

func applyFcrepoOff(projectDir, drupalRootfs, targetScheme string) error {
	if err := updateComposeForFcrepoOff(filepath.Join(projectDir, "docker-compose.yml")); err != nil {
		return err
	}
	if err := removeFcrepoTraefikRoute(projectDir); err != nil {
		return err
	}

	configDir := corecomponent.ResolveDrupalLayout(projectDir, drupalRootfs).ConfigSyncDir()
	if err := cleanupDrupalConfig(configDir, targetScheme); err != nil {
		return err
	}

	return nil
}

func applyBlazegraphOff(projectDir, drupalRootfs string) error {
	if err := updateComposeForBlazegraphOff(filepath.Join(projectDir, "docker-compose.yml")); err != nil {
		return err
	}

	configDir := corecomponent.ResolveDrupalLayout(projectDir, drupalRootfs).ConfigSyncDir()
	if err := cleanupBlazegraphConfig(configDir); err != nil {
		return err
	}

	return nil
}

func updateComposeForFcrepoOff(composePath string) error {
	compose, err := corecomponent.LoadComposeFile(composePath)
	if err != nil {
		return err
	}
	if err := compose.DeleteService("fcrepo"); err != nil {
		return err
	}
	if err := compose.DeleteService("fcrepo-database-init"); err != nil {
		return err
	}
	if err := compose.DeleteService("milliner"); err != nil {
		return err
	}
	if serviceEnvValue(compose, "drupal", "DRUPAL_DEFAULT_SITE_URL") == LocalDrupalBaseURL {
		if err := compose.SetServiceEnv("drupal", "DRUPAL_DEFAULT_SITE_URL", publicSiteURLExpr); err != nil {
			return err
		}
	}
	if serviceEnvValue(compose, "drupal", "DRUSH_OPTIONS_URI") == LocalDrupalBaseURL {
		if err := compose.SetServiceEnv("drupal", "DRUSH_OPTIONS_URI", publicSiteURLExpr); err != nil {
			return err
		}
	}
	for _, key := range []string{
		"DRUPAL_DEFAULT_FCREPO_HOST",
		"DRUPAL_DEFAULT_FCREPO_PORT",
		"DRUPAL_DEFAULT_FCREPO_URL",
	} {
		if err := compose.DeleteServiceEnv("drupal", key); err != nil {
			return err
		}
	}
	if err := compose.DeleteVolume("fcrepo-data"); err != nil {
		return err
	}
	if err := compose.SetServiceEnv("alpaca", "ALPACA_FCREPO_INDEXER_ENABLED", "false"); err != nil {
		return err
	}
	return compose.Save()
}

func shouldUseLocalDrupalInternalURL(siteURL string) bool {
	siteURL = strings.TrimSpace(siteURL)
	if siteURL == "" {
		return true
	}
	host := serviceURLHost(siteURL)
	return host == "" || host == "localhost" || host == "127.0.0.1"
}

// TrustedHostPatterns returns comma-separated Drupal trusted host regexes.
func TrustedHostPatterns(domain string, includeLocalDrupal bool) string {
	patterns := []string{trustedHostPattern(domain)}
	if includeLocalDrupal {
		patterns = append(patterns, trustedHostPattern(localDrupalHost))
	}
	return strings.Join(uniqueStrings(patterns), ",")
}

func trustedHostPattern(domain string) string {
	domain = strings.TrimSpace(domain)
	if domain == "" {
		domain = coretraefik.DefaultIngressDomain
	}
	if domain == coretraefik.DefaultIngressDomain {
		return DefaultTrustedHostPatterns
	}
	return "^" + regexp.QuoteMeta(domain) + "$"
}

func uniqueStrings(values []string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func fcrepoAllowedDrupalURL(siteURL string) string {
	siteURL = strings.TrimRight(strings.TrimSpace(siteURL), "/")
	if siteURL == "" {
		siteURL = publicSiteURLExpr
	}
	return siteURL + "/"
}

func firstServiceEnvValue(compose *corecomponent.ComposeFile, service string, keys ...string) string {
	for _, key := range keys {
		value := serviceEnvValue(compose, service, key)
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func serviceEnvValue(compose *corecomponent.ComposeFile, service, key string) string {
	if compose == nil {
		return ""
	}
	block, ok := compose.ServiceBlock(service)
	if !ok {
		return ""
	}
	for _, line := range strings.Split(block, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, key+":") {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(trimmed, key+":"))
		return strings.Trim(value, `"'`)
	}
	return ""
}

func serviceURLHost(value string) string {
	value = strings.TrimSpace(value)
	scheme, rest, ok := strings.Cut(value, "://")
	if !ok || strings.TrimSpace(scheme) == "" {
		return ""
	}
	host, _, _ := strings.Cut(rest, "/")
	host, _, _ = strings.Cut(host, ":")
	return strings.ToLower(strings.TrimSpace(host))
}

func updateComposeForBlazegraphOff(composePath string) error {
	compose, err := corecomponent.LoadComposeFile(composePath)
	if err != nil {
		return err
	}
	if err := compose.DeleteService("blazegraph"); err != nil {
		return err
	}
	if err := compose.DeleteVolume("blazegraph-data"); err != nil {
		return err
	}
	if err := compose.SetServiceEnv("drupal", "DRUPAL_DEFAULT_TRIPLESTORE_NAMESPACE", ""); err != nil {
		return err
	}
	if err := compose.SetServiceEnv("alpaca", "ALPACA_TRIPLESTORE_INDEXER_ENABLED", "false"); err != nil {
		return err
	}
	return compose.Save()
}

func cleanupDrupalConfig(configDir, targetScheme string) error {
	root, err := os.OpenRoot(filepath.Clean(configDir))
	if err != nil {
		return fmt.Errorf("open config dir: %w", err)
	}
	defer root.Close()

	for _, name := range fedoraCleanupFiles {
		if err := root.Remove(name); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove %s: %w", name, err)
		}
	}

	files, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return fmt.Errorf("read config dir: %w", err)
	}

	for _, entry := range files {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yml" {
			continue
		}
		data, err := root.ReadFile(entry.Name())
		if err != nil {
			return fmt.Errorf("read %s: %w", entry.Name(), err)
		}

		updated := removeFedoraAdminLines(string(data))
		if entry.Name() != "jsonld.settings.yml" {
			updated = strings.ReplaceAll(updated, "fedora://", targetScheme+"://")
		}

		if contains(mediaSchemeFiles, entry.Name()) {
			updated, err = setTopLevelScalar(updated, "settings.uri_scheme", targetScheme)
			if err != nil {
				return fmt.Errorf("set uri_scheme in %s: %w", entry.Name(), err)
			}
		}

		if err := root.WriteFile(entry.Name(), []byte(updated), 0o644); err != nil { // #nosec G306 -- generated Drupal config is non-secret project configuration.
			return fmt.Errorf("write %s: %w", entry.Name(), err)
		}
	}

	return nil
}

func cleanupBlazegraphConfig(configDir string) error {
	root, err := os.OpenRoot(filepath.Clean(configDir))
	if err != nil {
		return fmt.Errorf("open config dir: %w", err)
	}
	defer root.Close()

	for _, name := range blazegraphCleanupFiles {
		if err := root.Remove(name); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove %s: %w", name, err)
		}
	}

	contextReplacements := map[string][]string{
		"context.context.all_media.yml": {
			"      index_media_in_triplestore: index_media_in_triplestore",
			"      delete_media_from_triplestore: delete_media_from_triplestore",
		},
		"context.context.repository_content.yml": {
			"      index_node_in_triplestore: index_node_in_triplestore",
			"      delete_node_from_triplestore: delete_node_from_triplestore",
		},
		"context.context.taxonomy_terms.yml": {
			"      index_taxonomy_term_in_the_triplestore: index_taxonomy_term_in_the_triplestore",
			"      delete_taxonomy_term_in_triplestore: delete_taxonomy_term_in_triplestore",
		},
	}

	for name, removals := range contextReplacements {
		data, err := root.ReadFile(name)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("read %s: %w", name, err)
		}

		updated := string(data)
		for _, removal := range removals {
			updated = strings.ReplaceAll(updated, removal+"\n", "")
		}

		if err := root.WriteFile(name, []byte(updated), 0o644); err != nil { // #nosec G306 -- generated Drupal config is non-secret project configuration.
			return fmt.Errorf("write %s: %w", name, err)
		}
	}

	return nil
}

func removeFedoraAdminLines(input string) string {
	lines := strings.Split(input, "\n")
	filtered := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.Contains(line, "fedoraadmin: '0'") {
			continue
		}
		filtered = append(filtered, line)
	}
	return strings.Join(filtered, "\n")
}

func setTopLevelScalar(input, dottedPath, value string) (string, error) {
	doc, err := corecomponent.LoadYAMLDocument([]byte(input))
	if err != nil {
		return "", err
	}
	if err := doc.SetString("."+dottedPath, value); err != nil {
		return "", err
	}

	data, err := doc.Bytes()
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func contains(values []string, value string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}

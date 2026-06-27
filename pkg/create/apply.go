package create

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	corecomponent "github.com/libops/sitectl/pkg/component"
	coretraefik "github.com/libops/sitectl/pkg/services/traefik"
)

const (
	FcrepoStateOn  = "on"
	FcrepoStateOff = "off"

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
)

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

	composePath := filepath.Join(opts.Path, "docker-compose.yml")
	if opts.Fcrepo == FcrepoStateOn {
		if err := applyFcrepoOn(opts.Path); err != nil {
			return fmt.Errorf("apply fcrepo=on: %w", err)
		}
	} else {
		if err := applyFcrepoOff(opts.Path, opts.DrupalRootfs, opts.ISLEFileSystemURI); err != nil {
			return fmt.Errorf("apply fcrepo=off: %w", err)
		}
	}

	if opts.Blazegraph == FcrepoStateOn {
		if err := setComposeEnv(composePath, "alpaca", "ALPACA_TRIPLESTORE_INDEXER_ENABLED", "true"); err != nil {
			return err
		}
		if err := setComposeEnv(composePath, "drupal", "DRUPAL_DEFAULT_TRIPLESTORE_NAMESPACE", "islandora"); err != nil {
			return err
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
		if err := coretraefik.ApplyBotMitigation(opts.Path, opts.BotMitigation, isleBotMitigationOptions()); err != nil {
			return fmt.Errorf("apply bot-mitigation=%s: %w", opts.BotMitigation, err)
		}
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

func isleBotMitigationOptions() coretraefik.BotMitigationOptions {
	return coretraefik.BotMitigationOptions{
		RouterName:       "drupal",
		RouterConfigPath: "conf/traefik/drupal.yml",
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
		strings.Contains(string(data), "COPY --link drupal/rootfs/etc/ /etc/") {
		return nil
	}

	header := dockerfileHeader(string(data))
	contents := strings.TrimRight(header, "\n") + `

COPY --link composer.json composer.lock /var/www/drupal/
COPY --link assets/ /var/www/drupal/assets/

RUN --mount=type=cache,id=custom-drupal-composer-${TARGETARCH},sharing=locked,target=/root/.composer/cache \
    composer install -d /var/www/drupal && \
    cleanup.sh

COPY --link config/ /var/www/drupal/config/
COPY --link recipes/ /var/www/drupal/recipes/
COPY --link web/modules/custom/ /var/www/drupal/web/modules/custom/
COPY --link web/themes/custom/ /var/www/drupal/web/themes/custom/
COPY --link drupal/rootfs/etc/ /etc/
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
		out = append(out, line)
		if strings.TrimSpace(line) == "ARG TARGETARCH" {
			foundTargetArch = true
			break
		}
	}
	if foundTargetArch {
		return strings.Join(out, "\n")
	}
	return `# syntax=docker/dockerfile:1.23.0
ARG REPOSITORY
ARG TAG
FROM ${REPOSITORY}/drupal:${TAG}

ARG TARGETARCH`
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

func applyFcrepoOn(projectDir string) error {
	composePath := filepath.Join(projectDir, "docker-compose.yml")
	compose, err := corecomponent.LoadComposeFile(composePath)
	if err != nil {
		return err
	}
	images := inferFcrepoRestoreImages(compose)
	commonMerge := commonMergeLine(composePath)
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
	if !compose.HasService("milliner") {
		if err := compose.AddServiceBlock("milliner", millinerRestoreServiceBlock(images.Milliner, commonMerge)); err != nil {
			return err
		}
	}
	for key, value := range map[string]string{
		"DRUPAL_DEFAULT_FCREPO_HOST": "fcrepo",
		"DRUPAL_DEFAULT_FCREPO_PORT": "8080",
		"DRUPAL_DEFAULT_FCREPO_URL":  "${URI_SCHEME}://fcrepo.${DOMAIN}/fcrepo/rest/",
	} {
		if err := compose.SetServiceEnv("drupal", key, value); err != nil {
			return err
		}
	}
	if err := compose.SetServiceEnv("alpaca", "ALPACA_FCREPO_INDEXER_ENABLED", "true"); err != nil {
		return err
	}
	return compose.Save()
}

type fcrepoRestoreImages struct {
	Fcrepo   string
	Milliner string
}

func inferFcrepoRestoreImages(compose *corecomponent.ComposeFile) fcrepoRestoreImages {
	repository, tag := inferIslandoraStackImageConvention(compose)
	fcrepoName := "fcrepo6"
	if repository == "libops" {
		fcrepoName = "fcrepo"
	}
	return fcrepoRestoreImages{
		Fcrepo:   repository + "/" + fcrepoName + ":" + tag,
		Milliner: "islandora/milliner:" + tag,
	}
}

func inferIslandoraStackImageConvention(compose *corecomponent.ComposeFile) (string, string) {
	repository := "islandora"
	tag := "${ISLANDORA_TAG}"
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
    environment:
      DB_HOST: mariadb
      DB_PORT: 3306
      FCREPO_ALLOW_EXTERNAL_DEFAULT: http://default/
      FCREPO_ALLOW_EXTERNAL_DRUPAL: ${URI_SCHEME}://${DOMAIN}/
      FCREPO_PERSISTENCE_TYPE: mysql
    image: ` + image + `
    secrets:
      - source: DB_ROOT_PASSWORD
      - source: FCREPO_DB_PASSWORD
      - source: JWT_ADMIN_TOKEN
      - source: JWT_PUBLIC_KEY
    volumes:
      - fcrepo-data:/data:rw`
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

func applyFcrepoOff(projectDir, drupalRootfs, targetScheme string) error {
	if err := updateComposeForFcrepoOff(filepath.Join(projectDir, "docker-compose.yml")); err != nil {
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
	if err := compose.DeleteService("milliner"); err != nil {
		return err
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

func setComposeEnv(composePath, serviceName, key, value string) error {
	compose, err := corecomponent.LoadComposeFile(composePath)
	if err != nil {
		return err
	}
	if err := compose.SetServiceEnv(serviceName, key, value); err != nil {
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

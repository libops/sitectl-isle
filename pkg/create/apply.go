package create

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	corecomponent "github.com/libops/sitectl/pkg/component"
)

const (
	FcrepoStateOn  = "on"
	FcrepoStateOff = "off"

	IIIFCantaloupe       = "cantaloupe"
	IIIFTriplet          = "triplet"
	IIIFTopologyLocal    = "local"
	IIIFTopologyExternal = "external"

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
	Path              string
	DrupalRootfs      string
	Fcrepo            string
	Blazegraph        string
	IIIF              string
	IIIFTopology      string
	IIIFUpstreamURL   string
	BotMitigation     string
	ComposeOverride   string
	ISLEFileSystemURI string
}

func Apply(opts Options) error {
	if opts.Path == "" {
		opts.Path = "."
	}
	if opts.DrupalRootfs == "" {
		opts.DrupalRootfs = DefaultDrupalRootfs
	}
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
		opts.BotMitigation = BotMitigationStateOff
	}
	if opts.ISLEFileSystemURI == "" {
		opts.ISLEFileSystemURI = DefaultISLEFileSystemURI
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
	if opts.BotMitigation != BotMitigationStateOn && opts.BotMitigation != BotMitigationStateOff {
		return fmt.Errorf("invalid --bot-mitigation value %q: expected on or off", opts.BotMitigation)
	}
	if opts.IIIFTopology == IIIFTopologyExternal && strings.TrimSpace(opts.IIIFUpstreamURL) == "" {
		return fmt.Errorf("invalid --iiif-upstream-url value %q: expected a non-empty upstream URL when --iiif-topology=external", opts.IIIFUpstreamURL)
	}
	if strings.TrimSpace(opts.ISLEFileSystemURI) == "" {
		return fmt.Errorf("invalid --isle-file-system-uri value %q: expected a non-empty filesystem URI", opts.ISLEFileSystemURI)
	}

	composePath := filepath.Join(opts.Path, "docker-compose.yml")
	if opts.Fcrepo == FcrepoStateOn {
		if err := setComposeEnv(composePath, "alpaca", "ALPACA_FCREPO_INDEXER_ENABLED", "true"); err != nil {
			return err
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
	if opts.BotMitigation == BotMitigationStateOn {
		if err := ApplyBotMitigation(opts.Path, opts.BotMitigation); err != nil {
			return fmt.Errorf("apply bot-mitigation=%s: %w", opts.BotMitigation, err)
		}
	}

	return nil
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

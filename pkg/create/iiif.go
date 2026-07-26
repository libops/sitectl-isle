package create

import (
	"embed"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	corecomponent "github.com/libops/sitectl/pkg/component"
)

const (
	iiifUpstreamEnvKey       = "IIIF_UPSTREAM_URL"
	legacyCantaloupeEnvKey   = "CANTALOUPE_UPSTREAM_URL"
	localCantaloupeUpstream  = "http://cantaloupe:8182"
	localTripletUpstream     = "http://triplet:8080"
	cantaloupeDataVolumeName = "cantaloupe-data"
	tripletCacheVolumeName   = "triplet-cache"
	publicSiteURLExpr        = "http://localhost"
	composePublicSiteURLExpr = "${URI_SCHEME}://${DOMAIN}"
)

//go:embed assets/iiif/*
var iiifAssets embed.FS

func applyIIIF(opts Options) error {
	opts = normalizeIIIFOptions(opts)
	if opts.IIIFTopology == IIIFTopologyExternal {
		if err := validateIIIFUpstreamURL(opts.IIIFUpstreamURL); err != nil {
			return err
		}
	}

	composePath := filepath.Join(opts.Path, "docker-compose.yml")
	overridePath := strings.TrimSpace(opts.ComposeOverride)
	if overridePath == "" {
		overridePath = filepath.Join(opts.Path, "docker-compose.local.yml")
	}

	switch opts.IIIF {
	case IIIFTriplet:
		includeFcrepo, err := applyTripletIIIF(composePath, overridePath, opts.IIIFTopology, opts.IIIFUpstreamURL)
		if err != nil {
			return err
		}
		if err := writeTripletFiles(opts.Path, opts.IIIFTopology, includeFcrepo); err != nil {
			return err
		}
		if err := removeCantaloupeFiles(opts.Path); err != nil {
			return err
		}
		if err := updateRobotsIIIF(opts.Path, opts.DrupalRootfs, true); err != nil {
			return err
		}
	case IIIFCantaloupe:
		if err := applyCantaloupeIIIF(composePath, overridePath, opts.IIIFTopology, opts.IIIFUpstreamURL); err != nil {
			return err
		}
		if err := writeCantaloupeFiles(opts.Path, opts.IIIFTopology); err != nil {
			return err
		}
		if err := removeTripletFiles(opts.Path); err != nil {
			return err
		}
		if err := updateRobotsIIIF(opts.Path, opts.DrupalRootfs, false); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported iiif implementation %q", opts.IIIF)
	}

	return nil
}

func ApplyIIIF(opts Options) error {
	opts = normalizeIIIFOptions(opts)
	if opts.IIIF != IIIFCantaloupe && opts.IIIF != IIIFTriplet {
		return fmt.Errorf("invalid --iiif value %q: expected cantaloupe or triplet", opts.IIIF)
	}
	if opts.IIIFTopology != IIIFTopologyLocal && opts.IIIFTopology != IIIFTopologyExternal {
		return fmt.Errorf("invalid --iiif-topology value %q: expected local or external", opts.IIIFTopology)
	}
	return applyIIIF(opts)
}

func normalizeIIIFOptions(opts Options) Options {
	if opts.Path == "" {
		opts.Path = "."
	}
	if opts.DrupalRootfs == "" {
		opts.DrupalRootfs = DefaultDrupalRootfs
	}
	opts.DrupalRootfs = resolveProjectDrupalRootfs(opts.Path, opts.DrupalRootfs)
	if opts.IIIF == "" {
		opts.IIIF = IIIFCantaloupe
	}
	if opts.IIIFTopology == "" {
		opts.IIIFTopology = IIIFTopologyLocal
	}
	return opts
}

func validateIIIFUpstreamURL(value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fmt.Errorf("external IIIF upstream URL cannot be empty")
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return fmt.Errorf("invalid external IIIF upstream URL %q: %w", trimmed, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("invalid external IIIF upstream URL %q: scheme must be http or https", trimmed)
	}
	if parsed.Host == "" {
		return fmt.Errorf("invalid external IIIF upstream URL %q: host is required", trimmed)
	}
	return nil
}

func applyTripletIIIF(composePath, overridePath, topology, upstreamURL string) (bool, error) {
	compose, err := corecomponent.LoadComposeFile(composePath)
	if err != nil {
		return false, err
	}
	hasFcrepo := compose.HasService("fcrepo") && compose.HasVolume("fcrepo-data")
	tripletBlock, err := tripletServiceBlock(composePath, hasFcrepo)
	if err != nil {
		return false, err
	}

	if err := compose.DeleteService("cantaloupe"); err != nil {
		return false, err
	}
	if err := compose.DeleteVolume(cantaloupeDataVolumeName); err != nil {
		return false, err
	}
	drupalIIIFURL := publicSiteURLExpr + "/iiif/3"
	if topology == IIIFTopologyExternal {
		drupalIIIFURL = strings.TrimSpace(upstreamURL)
	}
	if err := compose.SetServiceEnv("drupal", "DRUPAL_DEFAULT_CANTALOUPE_URL", drupalIIIFURL); err != nil {
		return false, err
	}
	if err := compose.DeleteServiceEnv("traefik", legacyCantaloupeEnvKey); err != nil {
		return false, err
	}

	switch topology {
	case IIIFTopologyLocal:
		if err := compose.DeleteService("triplet"); err != nil {
			return false, err
		}
		if err := compose.AddServiceBlock("triplet", tripletBlock); err != nil {
			return false, err
		}
		if err := ensureVolumeBlock(compose, tripletCacheVolumeName, "  triplet-cache: {}"); err != nil {
			return false, err
		}
		if err := compose.DeleteServiceEnv("traefik", iiifUpstreamEnvKey); err != nil {
			return false, err
		}
	case IIIFTopologyExternal:
		if err := ensureExternalOverride(overridePath, "triplet", tripletBlock, tripletCacheVolumeName, "  triplet-cache: {}", localTripletUpstream, []string{"8080:8080"}); err != nil {
			return false, err
		}
		if err := ensureDevIIIFOverride(filepath.Dir(composePath), "triplet", tripletBlock, tripletCacheVolumeName, "  triplet-cache: {}", localTripletUpstream, []string{"8080:8080"}, publicSiteURLExpr+"/iiif/3"); err != nil {
			return false, err
		}
		if err := compose.DeleteService("triplet"); err != nil {
			return false, err
		}
		if err := compose.DeleteVolume(tripletCacheVolumeName); err != nil {
			return false, err
		}
		if err := ensureServiceEnv(compose, "traefik", iiifUpstreamEnvKey, strings.TrimSpace(upstreamURL)); err != nil {
			return false, err
		}
	default:
		return false, fmt.Errorf("unsupported iiif topology %q", topology)
	}

	if err := compose.Save(); err != nil {
		return false, err
	}
	if err := removeOverrideIIIFService(overridePath, "cantaloupe", cantaloupeDataVolumeName); err != nil {
		return false, err
	}
	if err := removeDevIIIFService(filepath.Dir(composePath), "cantaloupe", cantaloupeDataVolumeName); err != nil {
		return false, err
	}
	if topology == IIIFTopologyLocal {
		if err := removeOverrideIIIFService(overridePath, "triplet", tripletCacheVolumeName); err != nil {
			return false, err
		}
		if err := removeDevIIIFService(filepath.Dir(composePath), "triplet", tripletCacheVolumeName); err != nil {
			return false, err
		}
		if err := removeOverrideIIIFUpstreamEnv(overridePath); err != nil {
			return false, err
		}
		return hasFcrepo, removeDevIIIFEnv(filepath.Dir(composePath))
	}
	return hasFcrepo, nil
}

func applyCantaloupeIIIF(composePath, overridePath, topology, upstreamURL string) error {
	compose, err := corecomponent.LoadComposeFile(composePath)
	if err != nil {
		return err
	}
	defaultCantaloupeBlock, err := cantaloupeServiceBlock(composePath)
	if err != nil {
		return err
	}
	cantaloupeBlock := currentOrDefaultServiceBlock(compose, "cantaloupe", defaultCantaloupeBlock)

	if err := compose.DeleteService("triplet"); err != nil {
		return err
	}
	if err := compose.DeleteVolume(tripletCacheVolumeName); err != nil {
		return err
	}
	drupalIIIFURL := publicSiteURLExpr + "/cantaloupe/iiif/2"
	if topology == IIIFTopologyExternal {
		drupalIIIFURL = strings.TrimSpace(upstreamURL)
	}
	if err := compose.SetServiceEnv("drupal", "DRUPAL_DEFAULT_CANTALOUPE_URL", drupalIIIFURL); err != nil {
		return err
	}
	if err := compose.DeleteServiceEnv("traefik", legacyCantaloupeEnvKey); err != nil {
		return err
	}

	switch topology {
	case IIIFTopologyLocal:
		if err := compose.DeleteService("cantaloupe"); err != nil {
			return err
		}
		if err := compose.AddServiceBlock("cantaloupe", cantaloupeBlock); err != nil {
			return err
		}
		if err := ensureVolumeBlock(compose, cantaloupeDataVolumeName, "  cantaloupe-data: {}"); err != nil {
			return err
		}
		if err := compose.DeleteServiceEnv("traefik", iiifUpstreamEnvKey); err != nil {
			return err
		}
	case IIIFTopologyExternal:
		if err := ensureExternalOverride(overridePath, "cantaloupe", cantaloupeBlock, cantaloupeDataVolumeName, "  cantaloupe-data: {}", localCantaloupeUpstream, []string{"8182:8182"}); err != nil {
			return err
		}
		if err := ensureDevIIIFOverride(filepath.Dir(composePath), "cantaloupe", cantaloupeBlock, cantaloupeDataVolumeName, "  cantaloupe-data: {}", localCantaloupeUpstream, []string{"8182:8182"}, publicSiteURLExpr+"/cantaloupe/iiif/2"); err != nil {
			return err
		}
		if err := compose.DeleteService("cantaloupe"); err != nil {
			return err
		}
		if err := compose.DeleteVolume(cantaloupeDataVolumeName); err != nil {
			return err
		}
		if err := ensureServiceEnv(compose, "traefik", iiifUpstreamEnvKey, strings.TrimSpace(upstreamURL)); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported iiif topology %q", topology)
	}

	if err := compose.Save(); err != nil {
		return err
	}
	if err := removeOverrideIIIFService(overridePath, "triplet", tripletCacheVolumeName); err != nil {
		return err
	}
	if err := removeDevIIIFService(filepath.Dir(composePath), "triplet", tripletCacheVolumeName); err != nil {
		return err
	}
	if topology == IIIFTopologyLocal {
		if err := removeOverrideIIIFService(overridePath, "cantaloupe", cantaloupeDataVolumeName); err != nil {
			return err
		}
		if err := removeDevIIIFService(filepath.Dir(composePath), "cantaloupe", cantaloupeDataVolumeName); err != nil {
			return err
		}
		if err := removeOverrideIIIFUpstreamEnv(overridePath); err != nil {
			return err
		}
		return removeDevIIIFEnv(filepath.Dir(composePath))
	}
	return nil
}

func ensureExternalOverride(overridePath, serviceName, serviceBlock, volumeName, volumeBlock, localUpstream string, ports []string) error {
	overrideCompose, err := corecomponent.LoadComposeFileOptional(overridePath)
	if err != nil {
		return err
	}
	if err := overrideCompose.DeleteService(serviceName); err != nil {
		return err
	}
	if err := overrideCompose.AddServiceBlock(serviceName, serviceBlock); err != nil {
		return err
	}
	if len(ports) > 0 {
		if err := overrideCompose.SetServiceStringList(serviceName, "ports", ports); err != nil {
			return err
		}
	}
	if err := ensureVolumeBlock(overrideCompose, volumeName, volumeBlock); err != nil {
		return err
	}
	if err := ensureServiceEnv(overrideCompose, "traefik", iiifUpstreamEnvKey, localUpstream); err != nil {
		return err
	}
	if err := overrideCompose.DeleteServiceEnv("traefik", legacyCantaloupeEnvKey); err != nil {
		return err
	}
	return overrideCompose.Save()
}

func removeOverrideIIIFService(overridePath, serviceName, volumeName string) error {
	overrideCompose, err := corecomponent.LoadComposeFileOptional(overridePath)
	if err != nil {
		return err
	}
	if err := overrideCompose.DeleteService(serviceName); err != nil {
		return err
	}
	if volumeName != "" {
		if err := overrideCompose.DeleteVolume(volumeName); err != nil {
			return err
		}
	}
	if err := overrideCompose.DeleteServiceEnv("traefik", legacyCantaloupeEnvKey); err != nil {
		return err
	}
	return overrideCompose.Save()
}

func removeOverrideIIIFUpstreamEnv(overridePath string) error {
	overrideCompose, err := corecomponent.LoadComposeFileOptional(overridePath)
	if err != nil {
		return err
	}
	if err := overrideCompose.DeleteServiceEnv("traefik", iiifUpstreamEnvKey); err != nil {
		return err
	}
	return overrideCompose.Save()
}

func ensureDevIIIFOverride(projectDir, serviceName, serviceBlock, volumeName, volumeBlock, localUpstream string, ports []string, drupalURL string) error {
	devCompose, err := corecomponent.LoadComposeFileOptional(dockerComposeDevPath(projectDir))
	if err != nil {
		return err
	}
	if err := devCompose.DeleteService(serviceName); err != nil {
		return err
	}
	if err := devCompose.AddServiceBlock(serviceName, serviceBlock); err != nil {
		return err
	}
	if len(ports) > 0 {
		if err := devCompose.SetServiceStringList(serviceName, "ports", ports); err != nil {
			return err
		}
	}
	if err := ensureVolumeBlock(devCompose, volumeName, volumeBlock); err != nil {
		return err
	}
	if err := ensureServiceEnv(devCompose, "traefik", iiifUpstreamEnvKey, localUpstream); err != nil {
		return err
	}
	if err := ensureServiceEnv(devCompose, "drupal", "DRUPAL_DEFAULT_CANTALOUPE_URL", drupalURL); err != nil {
		return err
	}
	if err := devCompose.DeleteServiceEnv("traefik", legacyCantaloupeEnvKey); err != nil {
		return err
	}
	return devCompose.Save()
}

func removeDevIIIFService(projectDir, serviceName, volumeName string) error {
	devCompose, err := corecomponent.LoadComposeFileOptional(dockerComposeDevPath(projectDir))
	if err != nil {
		return err
	}
	if err := devCompose.DeleteService(serviceName); err != nil {
		return err
	}
	if volumeName != "" {
		if err := devCompose.DeleteVolume(volumeName); err != nil {
			return err
		}
	}
	if err := devCompose.DeleteServiceEnv("traefik", legacyCantaloupeEnvKey); err != nil {
		return err
	}
	return devCompose.Save()
}

func removeDevIIIFEnv(projectDir string) error {
	devCompose, err := corecomponent.LoadComposeFileOptional(dockerComposeDevPath(projectDir))
	if err != nil {
		return err
	}
	if err := devCompose.DeleteServiceEnv("traefik", iiifUpstreamEnvKey); err != nil {
		return err
	}
	if err := devCompose.DeleteServiceEnv("drupal", "DRUPAL_DEFAULT_CANTALOUPE_URL"); err != nil {
		return err
	}
	if err := devCompose.DeleteServiceEnv("traefik", legacyCantaloupeEnvKey); err != nil {
		return err
	}
	return devCompose.Save()
}

func currentOrDefaultServiceBlock(compose *corecomponent.ComposeFile, serviceName, fallback string) string {
	if block, ok := compose.ServiceBlock(serviceName); ok && strings.TrimSpace(block) != "" {
		return block
	}
	return fallback
}

func ensureVolumeBlock(compose *corecomponent.ComposeFile, name, block string) error {
	if compose.HasVolume(name) {
		return nil
	}
	return compose.AddVolumeBlock(name, block)
}

func ensureServiceEnv(compose *corecomponent.ComposeFile, service, key, value string) error {
	if !compose.HasService(service) {
		if err := compose.AddServiceBlock(service, "  "+service+":"); err != nil {
			return err
		}
	}
	return compose.SetServiceEnv(service, key, value)
}

func tripletServiceBlock(composePath string, includeFcrepo bool) (string, error) {
	fcrepoDependsOn := ""
	fcrepoVolume := ""
	if includeFcrepo {
		var err error
		fcrepoDependsOn, err = renderIIIFAsset("triplet-fcrepo-depends-on.yml", nil)
		if err != nil {
			return "", err
		}
		fcrepoVolume, err = renderIIIFAsset("triplet-fcrepo-volume.yml", nil)
		if err != nil {
			return "", err
		}
	}

	return renderIIIFAsset("triplet-service.yml", map[string]string{
		"COMMON_MERGE":      commonMergeLine(composePath),
		"FCREPO_DEPENDS_ON": fcrepoDependsOn,
		"FCREPO_VOLUME":     fcrepoVolume,
	})
}

func cantaloupeServiceBlock(composePath string) (string, error) {
	return renderIIIFAsset("cantaloupe-service.yml", map[string]string{
		"COMMON_MERGE": commonMergeLine(composePath),
	})
}

func commonMergeLine(composePath string) string {
	data, err := os.ReadFile(composePath) // #nosec G304 -- compose path is resolved inside the selected project.
	if err != nil {
		return ""
	}
	if strings.Contains(string(data), "&common") {
		return "    <<: *common\n"
	}
	return ""
}

func writeTripletFiles(projectDir, topology string, includeFcrepo bool) error {
	upstream := localTripletUpstream
	if topology == IIIFTopologyExternal {
		upstream = `{{ env "IIIF_UPSTREAM_URL" }}`
	}
	traefikConfig, err := tripletTraefikConfig(upstream)
	if err != nil {
		return err
	}
	tripletConfig, err := tripletConfigYAML(includeFcrepo)
	if err != nil {
		return err
	}
	if err := writeProjectFile(projectDir, "conf/traefik/triplet.yml", traefikConfig); err != nil {
		return err
	}
	return writeProjectFile(projectDir, "conf/triplet/config.yaml", tripletConfig)
}

func writeCantaloupeFiles(projectDir, topology string) error {
	upstream := localCantaloupeUpstream
	if topology == IIIFTopologyExternal {
		upstream = `{{ env "IIIF_UPSTREAM_URL" }}`
	}
	traefikConfig, err := cantaloupeTraefikConfig(upstream)
	if err != nil {
		return err
	}
	return writeProjectFile(projectDir, "conf/traefik/cantaloupe.yml", traefikConfig)
}

func removeTripletFiles(projectDir string) error {
	for _, rel := range []string{"conf/traefik/triplet.yml", "conf/triplet/config.yaml"} {
		if err := os.Remove(filepath.Join(projectDir, rel)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func removeCantaloupeFiles(projectDir string) error {
	if err := os.Remove(filepath.Join(projectDir, "conf/traefik/cantaloupe.yml")); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func writeProjectFile(projectDir, rel, contents string) error {
	path := filepath.Join(projectDir, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { // #nosec G301 -- generated project config directories must remain readable by the compose stack.
		return err
	}
	return os.WriteFile(path, []byte(strings.TrimRight(contents, "\n")+"\n"), 0o644) // #nosec G306 -- generated project configuration is non-secret.
}

func updateRobotsIIIF(projectDir, drupalRootfs string, enabled bool) error {
	layout := corecomponent.ResolveDrupalLayout(projectDir, resolveProjectDrupalRootfs(projectDir, drupalRootfs))
	path := filepath.Join(layout.Root, "web", "robots.txt")
	data, err := os.ReadFile(path) // #nosec G304 -- robots path is resolved inside the selected project.
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	lines := strings.Split(string(data), "\n")
	out := make([]string, 0, len(lines)+1)
	seen := false
	inserted := false
	for _, line := range lines {
		if strings.TrimSpace(line) == "Disallow: /iiif/*" {
			seen = true
			if !enabled {
				continue
			}
		}
		out = append(out, line)
		if enabled && !seen && !inserted && strings.TrimSpace(line) == "Disallow: /cantaloupe/*" {
			out = append(out, "Disallow: /iiif/*")
			inserted = true
		}
	}
	if enabled && !seen && !inserted {
		out = append(out, "Disallow: /iiif/*")
	}
	return os.WriteFile(path, []byte(strings.Join(out, "\n")), 0o644) // #nosec G306,G703 -- generated project configuration is non-secret and scoped under the selected project rootfs.
}

func renderIIIFAsset(name string, replacements map[string]string) (string, error) {
	return renderEmbeddedAsset(iiifAssets, "assets/iiif/"+name, replacements)
}

func tripletTraefikConfig(upstream string) (string, error) {
	return renderIIIFAsset("triplet-traefik.yml", map[string]string{
		"UPSTREAM_URL": upstream,
	})
}

func cantaloupeTraefikConfig(upstream string) (string, error) {
	return renderIIIFAsset("cantaloupe-traefik.yml", map[string]string{
		"UPSTREAM_URL": upstream,
	})
}

func tripletConfigYAML(includeFcrepo bool) (string, error) {
	fcrepoSource := ""
	if includeFcrepo {
		var err error
		fcrepoSource, err = renderIIIFAsset("triplet-fcrepo-source.yml", nil)
		if err != nil {
			return "", err
		}
	}
	return renderIIIFAsset("triplet-config.yaml", map[string]string{
		"FCREPO_SOURCE": fcrepoSource,
	})
}

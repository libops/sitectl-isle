package create

import (
	"embed"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	corecomponent "github.com/libops/sitectl/pkg/component"
)

const (
	microservicesBaseURL      = "https://microservices.libops.site"
	externalFITSWebserviceURL = microservicesBaseURL + "/fits/examine"
	localFITSWebserviceURL    = "http://fits:8080/fits/examine"
)

//go:embed assets/derivatives/*
var derivativeAssets embed.FS

// DerivativeServiceSpec describes one derivative microservice and the URLs used
// when it runs locally or through the managed LibOps endpoint.
type DerivativeServiceSpec struct {
	Name        string
	ImageRef    string
	AlpacaEnv   string
	ExternalURL string
	LocalURL    string
	NeedsJWT    bool
}

var derivativeServiceSpecs = []DerivativeServiceSpec{
	{
		Name:        "fits",
		ImageRef:    "libops/fits@sha256:f945c3aa9b1011261a038d6a2a1ddc7557d1d0c59e00add9a82a47334e78b76f",
		LocalURL:    localFITSWebserviceURL,
		ExternalURL: externalFITSWebserviceURL,
	},
	{
		Name:        "crayfits",
		ImageRef:    "libops/crayfits:main@sha256:9939b6288328e42ba8f82e79a99dd85ca4a0bb0d0f52b29466b66eaa4504e9cb",
		AlpacaEnv:   "ALPACA_DERIVATIVE_FITS_URL",
		LocalURL:    "http://crayfits:8080/",
		ExternalURL: microservicesBaseURL + "/crayfits",
		NeedsJWT:    true,
	},
	{
		Name:        "homarus",
		ImageRef:    "libops/homarus:8.1.2@sha256:dede2629a054497e8220f5cd4602f2f5d5bc249a430cb12ece61f8ef15819e9a",
		AlpacaEnv:   "ALPACA_DERIVATIVE_HOMARUS_URL",
		LocalURL:    "http://homarus:8080/",
		ExternalURL: microservicesBaseURL + "/homarus",
		NeedsJWT:    true,
	},
	{
		Name:        "houdini",
		ImageRef:    "libops/houdini:8.18.2@sha256:347005f6ba008b48e0f0e5b10d5cd30582883dab9bd514dae72ff59bd5eac8e2",
		AlpacaEnv:   "ALPACA_DERIVATIVE_HOUDINI_URL",
		LocalURL:    "http://houdini:8080/",
		ExternalURL: microservicesBaseURL + "/houdini",
		NeedsJWT:    true,
	},
	{
		Name:        "hypercube",
		ImageRef:    "libops/hypercube:5.5.2@sha256:d9ebaf350cb09ae814ae4fb2b4f38182c6f04032b05fff4cb8c2ad24f8006e9a",
		AlpacaEnv:   "ALPACA_DERIVATIVE_OCR_URL",
		LocalURL:    "http://hypercube:8080/",
		ExternalURL: microservicesBaseURL + "/hypercube",
		NeedsJWT:    true,
	},
}

// DerivativeServiceSpecs returns the canonical derivative service catalog.
func DerivativeServiceSpecs() []DerivativeServiceSpec {
	specs := make([]DerivativeServiceSpec, len(derivativeServiceSpecs))
	copy(specs, derivativeServiceSpecs)
	return specs
}

// DerivativeServiceNames returns the derivative service names in catalog order.
func DerivativeServiceNames() []string {
	names := make([]string, 0, len(derivativeServiceSpecs))
	for _, spec := range derivativeServiceSpecs {
		names = append(names, spec.Name)
	}
	return names
}

// IsDerivativeService reports whether name is a known derivative service.
func IsDerivativeService(name string) bool {
	_, ok := derivativeServiceSpecByName(name)
	return ok
}

// ApplyDerivativeServices applies the requested local or distributed topology
// for derivative services in compose.yaml and docker-compose.dev.yml.
func ApplyDerivativeServices(opts Options) error {
	if opts.Path == "" {
		opts.Path = "."
	}
	if len(opts.DerivativeServices) == 0 {
		return nil
	}

	composePath := filepath.Join(opts.Path, "compose.yaml")
	compose, err := corecomponent.LoadComposeFile(composePath)
	if err != nil {
		return err
	}
	devComposePath := dockerComposeDevPath(opts.Path)
	devCompose, err := corecomponent.LoadComposeFileOptional(devComposePath)
	if err != nil {
		return err
	}

	names := make([]string, 0, len(opts.DerivativeServices))
	for name := range opts.DerivativeServices {
		if _, ok := derivativeServiceSpecByName(name); !ok {
			return fmt.Errorf("unknown derivative service %q", name)
		}
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		spec, _ := derivativeServiceSpecByName(name)
		topology := strings.TrimSpace(opts.DerivativeServices[name])
		switch topology {
		case DerivativeTopologyDistributed:
			if err := applyDerivativeDistributed(compose, devCompose, composePath, spec); err != nil {
				return fmt.Errorf("apply %s distributed: %w", name, err)
			}
		case DerivativeTopologyLocal, "":
			if err := applyDerivativeLocal(compose, devCompose, composePath, spec); err != nil {
				return fmt.Errorf("apply %s local: %w", name, err)
			}
		default:
			return fmt.Errorf("invalid %s derivative topology %q: expected %s or %s", name, topology, DerivativeTopologyLocal, DerivativeTopologyDistributed)
		}
	}

	if err := reconcileCrayfitsFITSUpstream(compose); err != nil {
		return err
	}
	if err := reconcileDevCrayfitsFITSUpstream(compose, devCompose); err != nil {
		return err
	}
	if err := devCompose.Save(); err != nil {
		return err
	}
	return compose.Save()
}

func applyDerivativeDistributed(compose, devCompose *corecomponent.ComposeFile, composePath string, spec DerivativeServiceSpec) error {
	serviceBlock, err := currentOrDefaultDerivativeServiceBlock(compose, devCompose, composePath, spec)
	if err != nil {
		return err
	}
	if err := compose.DeleteService(spec.Name); err != nil {
		return err
	}
	if err := devCompose.DeleteService(spec.Name); err != nil {
		return err
	}
	if err := devCompose.AddServiceBlock(spec.Name, serviceBlock); err != nil {
		return err
	}
	if spec.AlpacaEnv != "" {
		if err := compose.SetServiceEnv("alpaca", spec.AlpacaEnv, spec.ExternalURL); err != nil {
			return err
		}
		if err := ensureServiceEnv(devCompose, "alpaca", spec.AlpacaEnv, spec.LocalURL); err != nil {
			return err
		}
	}
	return nil
}

func applyDerivativeLocal(compose, devCompose *corecomponent.ComposeFile, composePath string, spec DerivativeServiceSpec) error {
	if !compose.HasService(spec.Name) {
		block, err := currentOrDefaultDerivativeServiceBlock(compose, devCompose, composePath, spec)
		if err != nil {
			return err
		}
		if err := compose.AddServiceBlock(spec.Name, block); err != nil {
			return err
		}
	}
	if spec.AlpacaEnv != "" {
		if err := compose.DeleteServiceEnv("alpaca", spec.AlpacaEnv); err != nil {
			return err
		}
		if err := devCompose.DeleteServiceEnv("alpaca", spec.AlpacaEnv); err != nil {
			return err
		}
	}
	if err := devCompose.DeleteService(spec.Name); err != nil {
		return err
	}
	return nil
}

func reconcileCrayfitsFITSUpstream(compose *corecomponent.ComposeFile) error {
	if !compose.HasService("crayfits") {
		return nil
	}
	if compose.HasService("fits") {
		return compose.DeleteServiceEnv("crayfits", "CRAYFITS_WEBSERVICE_URI")
	}
	return compose.SetServiceEnv("crayfits", "CRAYFITS_WEBSERVICE_URI", externalFITSWebserviceURL)
}

func reconcileDevCrayfitsFITSUpstream(compose, devCompose *corecomponent.ComposeFile) error {
	if !compose.HasService("crayfits") && !devCompose.HasService("crayfits") {
		return nil
	}
	if compose.HasService("fits") && !devCompose.HasService("fits") {
		return devCompose.DeleteServiceEnv("crayfits", "CRAYFITS_WEBSERVICE_URI")
	}
	return ensureServiceEnv(devCompose, "crayfits", "CRAYFITS_WEBSERVICE_URI", localFITSWebserviceURL)
}

func currentOrDefaultDerivativeServiceBlock(compose, devCompose *corecomponent.ComposeFile, composePath string, spec DerivativeServiceSpec) (string, error) {
	if block, ok := compose.ServiceBlock(spec.Name); ok && strings.TrimSpace(block) != "" {
		return block, nil
	}
	if block, ok := devCompose.ServiceBlock(spec.Name); ok && strings.TrimSpace(block) != "" {
		return block, nil
	}
	return derivativeServiceBlock(composePath, spec)
}

func derivativeServiceBlock(composePath string, spec DerivativeServiceSpec) (string, error) {
	jwtSecrets := ""
	if spec.NeedsJWT {
		jwtSecrets = strings.Join([]string{
			"    secrets:",
			"      - source: CERT_PUBLIC_KEY",
			"      - source: CERT_AUTHORITY",
			"      - source: JWT_ADMIN_TOKEN",
			"      - source: JWT_PUBLIC_KEY",
		}, "\n")
	}
	return renderDerivativeAsset("service.yml", map[string]string{
		"COMMON_MERGE": commonMergeLine(composePath),
		"IMAGE_REF":    spec.ImageRef,
		"JWT_SECRETS":  jwtSecrets,
		"SERVICE_NAME": spec.Name,
	})
}

func renderDerivativeAsset(name string, replacements map[string]string) (string, error) {
	return renderEmbeddedAsset(derivativeAssets, "assets/derivatives/"+name, replacements)
}

func dockerComposeDevPath(projectDir string) string {
	return filepath.Join(projectDir, "docker-compose.dev.yml")
}

func derivativeServiceSpecByName(name string) (DerivativeServiceSpec, bool) {
	for _, spec := range derivativeServiceSpecs {
		if spec.Name == name {
			return spec, true
		}
	}
	return DerivativeServiceSpec{}, false
}

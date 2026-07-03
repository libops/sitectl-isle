package cmd

import (
	"net/url"
	"strings"

	"github.com/libops/sitectl/pkg/config"
	"github.com/libops/sitectl/pkg/healthcheck"
	"github.com/libops/sitectl/pkg/plugin"
	sitevalidate "github.com/libops/sitectl/pkg/validate"
	"github.com/spf13/cobra"
)

type isleHealthcheckRunner struct{}

func (isleHealthcheckRunner) BindFlags(cmd *cobra.Command) {}

func (isleHealthcheckRunner) Run(cmd *cobra.Command, ctx *config.Context) ([]sitevalidate.Result, error) {
	checker, err := healthcheck.NewDockerChecker(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = checker.Close() }()

	results := []sitevalidate.Result{
		checker.CheckHTTPRoute(
			cmd.Context(),
			"http:drupal",
			"drupal",
			isleDrupalPublicURL(ctx),
		),
	}

	results = append(results,
		checker.CheckMariaDB(cmd.Context(), "mariadb"),
		checker.CheckComposeServiceDependsOnHealthy(cmd.Context(), "drupal", "mariadb"),
		checker.CheckSolrCore(cmd.Context(), "solr", "default"),
	)

	optionalResults, err := checker.CheckOptionalHTTPServices(cmd.Context(),
		healthcheck.OptionalHTTPServiceCheck{Service: "homarus", Name: "http:homarus-healthcheck", URL: "http://127.0.0.1:8080/healthcheck"},
		healthcheck.OptionalHTTPServiceCheck{Service: "houdini", Name: "http:houdini-healthcheck", URL: "http://127.0.0.1:8080/healthcheck"},
		healthcheck.OptionalHTTPServiceCheck{Service: "hypercube", Name: "http:hypercube-healthcheck", URL: "http://127.0.0.1:8080/healthcheck"},
	)
	if err != nil {
		return nil, err
	}
	results = append(results, optionalResults...)

	return results, nil
}

var _ plugin.HealthcheckRunner = isleHealthcheckRunner{}

func isleDrupalPublicURL(ctx *config.Context) string {
	target := isleDrupalURLFromServiceEnvironment(ctx)
	if target == "" {
		target = healthcheck.PublicURLFromEnv(ctx, "http", "localhost")
	}
	if traefikURL, ok, err := healthcheck.PublicURLFromTraefik(ctx, healthcheck.TraefikRouteOptions{
		AppService:    "drupal",
		Router:        "drupal",
		DefaultScheme: "http",
		DefaultDomain: "localhost",
	}); err == nil && ok && preferISLETraefikHealthcheckURL(target, traefikURL) {
		target = traefikURL
	}
	return target
}

func isleDrupalURLFromServiceEnvironment(ctx *config.Context) string {
	env, err := plugin.ContextServiceEnvironment(ctx, "drupal")
	if err != nil {
		return ""
	}
	scheme := "http"
	domain := "localhost"
	if ingressDomain := firstISLEIngressHostname(env["INGRESS_HOSTNAMES"]); ingressDomain != "" || strings.TrimSpace(env["INGRESS_SCHEME"]) != "" {
		if strings.TrimSpace(env["INGRESS_SCHEME"]) != "" {
			scheme = strings.TrimSpace(env["INGRESS_SCHEME"])
		}
		if ingressDomain != "" {
			domain = ingressDomain
		}
	} else {
		for _, key := range []string{"DRUPAL_DEFAULT_SITE_URL", "DRUSH_OPTIONS_URI"} {
			parsedScheme, parsedDomain := schemeDomainFromRawURL(env[key])
			if parsedDomain != "" {
				if parsedScheme != "" {
					scheme = parsedScheme
				}
				domain = parsedDomain
				break
			}
		}
		if strings.EqualFold(strings.TrimSpace(env["DRUPAL_ENABLE_HTTPS"]), "true") {
			scheme = "https"
		}
	}
	if strings.TrimSpace(domain) == "" {
		return ""
	}
	return (&url.URL{Scheme: scheme, Host: domain, Path: "/"}).String()
}

func firstISLEIngressHostname(value string) string {
	for _, hostname := range strings.Split(value, ",") {
		hostname = strings.TrimSpace(hostname)
		if hostname != "" {
			return hostname
		}
	}
	return ""
}

func preferISLETraefikHealthcheckURL(envURL, traefikURL string) bool {
	envHost := healthcheckURLHostname(envURL)
	traefikHost := healthcheckURLHostname(traefikURL)
	if envHost != "" && !localHealthcheckHost(envHost) && localHealthcheckHost(traefikHost) {
		return false
	}
	return true
}

func schemeDomainFromRawURL(raw string) (string, string) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Hostname() == "" {
		return "", ""
	}
	host := parsed.Hostname()
	if port := parsed.Port(); port != "" {
		host = host + ":" + port
	}
	return parsed.Scheme, host
}

func healthcheckURLHostname(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	return parsed.Hostname()
}

func localHealthcheckHost(host string) bool {
	host = strings.ToLower(strings.Trim(host, "[]"))
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

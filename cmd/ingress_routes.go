package cmd

import (
	"strings"

	"github.com/libops/sitectl/pkg/config"
	"github.com/libops/sitectl/pkg/plugin"
	coretraefik "github.com/libops/sitectl/pkg/services/traefik"
	"github.com/spf13/cobra"
)

type isleIngressRouteProvider struct{}

func (isleIngressRouteProvider) BindFlags(cmd *cobra.Command) {}

func (isleIngressRouteProvider) Routes(cmd *cobra.Command, ctx *config.Context) (plugin.IngressRoutes, error) {
	services, err := plugin.ContextComposeServices(ctx)
	if err != nil {
		return plugin.IngressRoutes{}, err
	}
	followUps := currentIngressFollowUps(ctx)
	domain := firstIngressRouteValue(followUps["domain"], coretraefik.DefaultIngressDomain, "localhost")
	scheme := "http"
	switch strings.TrimSpace(followUps["mode"]) {
	case coretraefik.IngressModeHTTPSDefault, coretraefik.IngressModeHTTPSLetsEncrypt:
		scheme = "https"
	}
	routes := []plugin.IngressRoute{
		{
			Name:          "app",
			Service:       "drupal",
			Router:        "drupal",
			DefaultScheme: scheme,
			DefaultDomain: domain,
			Primary:       true,
		},
	}
	if services["fcrepo"] {
		routes = append(routes, plugin.IngressRoute{
			Name:          "fcrepo",
			Service:       "fcrepo",
			Router:        "fcrepo",
			DefaultScheme: scheme,
			DefaultDomain: subdomainIngressRouteDomain("fcrepo", domain),
		})
	}
	if services["triplet"] {
		routes = append(routes, plugin.IngressRoute{
			Name:          "iiif",
			Service:       "triplet",
			Router:        "triplet",
			DefaultScheme: scheme,
			DefaultDomain: domain,
			Path:          "/iiif",
		})
	}
	if services["cantaloupe"] {
		routes = append(routes, plugin.IngressRoute{
			Name:          "cantaloupe",
			Service:       "cantaloupe",
			Router:        "cantaloupe",
			DefaultScheme: scheme,
			DefaultDomain: domain,
			Path:          "/cantaloupe",
		})
	}
	if services["blazegraph"] {
		routes = append(routes, plugin.IngressRoute{
			Name:          "blazegraph",
			Service:       "blazegraph",
			Router:        "blazegraph",
			DefaultScheme: scheme,
			DefaultDomain: subdomainIngressRouteDomain("blazegraph", domain),
		})
	}
	return plugin.IngressRoutes{
		Domain: domain,
		Scheme: scheme,
		Routes: routes,
	}, nil
}

func subdomainIngressRouteDomain(prefix, domain string) string {
	prefix = strings.Trim(strings.TrimSpace(prefix), ".")
	domain = strings.TrimSpace(domain)
	if prefix == "" || domain == "" {
		return domain
	}
	return prefix + "." + domain
}

func firstIngressRouteValue(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

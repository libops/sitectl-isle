// Package endpoint resolves public Islandora service endpoints from sitectl
// ingress route catalogs.
package endpoint

import (
	"strings"

	"github.com/libops/sitectl/pkg/config"
	"github.com/libops/sitectl/pkg/plugin"
	"github.com/spf13/cobra"
)

var (
	ErrRouteNotFound      = plugin.ErrIngressRouteNotFound
	ErrRouteURLUnresolved = plugin.ErrIngressRouteURLUnresolved
)

// Resolution identifies the route source selected for an endpoint.
type Resolution = plugin.IngressRouteResolution

const (
	ResolutionTraefik = plugin.IngressRouteResolutionTraefik
	ResolutionCatalog = plugin.IngressRouteResolutionCatalog
)

// Resolved pairs a plugin-owned ingress route with its public URL.
type Resolved = plugin.ResolvedIngressRoute

const (
	// AppRoute is the public Drupal application route name.
	AppRoute = "app"
	// FCRepoRoute is the public Fedora repository route name.
	FCRepoRoute = "fcrepo"
	// IIIFRoute is the public Triplet IIIF route name.
	IIIFRoute = "iiif"
	// CantaloupeRoute is the public Cantaloupe route name.
	CantaloupeRoute = "cantaloupe"
	// BlazegraphRoute is the public Blazegraph route name.
	BlazegraphRoute = "blazegraph"
)

// Defaults contains context-specific public ingress defaults. It carries no
// authentication or credential material.
type Defaults struct {
	Scheme string
	Domain string
}

// DefaultsResolver returns public ingress defaults for a sitectl context.
type DefaultsResolver func(ctx *config.Context) Defaults

// Provider supplies and resolves the public routes for an ISLE context.
type Provider struct {
	Defaults DefaultsResolver
}

// BindFlags implements plugin.IngressRouteProvider.
func (Provider) BindFlags(cmd *cobra.Command) {}

// Routes returns the routes whose Compose services are present in the context.
func (p Provider) Routes(cmd *cobra.Command, ctx *config.Context) (plugin.IngressRoutes, error) {
	services, err := plugin.ContextComposeServices(ctx)
	if err != nil {
		return plugin.IngressRoutes{}, err
	}
	defaults := Defaults{Scheme: "http", Domain: defaultDomain(ctx)}
	if p.Defaults != nil {
		resolved := p.Defaults(ctx)
		defaults.Scheme = firstValue(resolved.Scheme, defaults.Scheme)
		defaults.Domain = firstValue(resolved.Domain, defaults.Domain)
	}
	routes := []plugin.IngressRoute{
		{
			Name:          AppRoute,
			Service:       "drupal",
			Router:        "drupal",
			DefaultScheme: defaults.Scheme,
			DefaultDomain: defaults.Domain,
			Primary:       true,
		},
	}
	if services[FCRepoRoute] {
		routes = append(routes, plugin.IngressRoute{
			Name:          FCRepoRoute,
			Service:       "fcrepo",
			Router:        "fcrepo",
			DefaultScheme: defaults.Scheme,
			DefaultDomain: subdomain("fcrepo", defaults.Domain),
		})
	}
	if services["triplet"] {
		routes = append(routes, plugin.IngressRoute{
			Name:          IIIFRoute,
			Service:       "triplet",
			Router:        "triplet",
			DefaultScheme: defaults.Scheme,
			DefaultDomain: defaults.Domain,
			Path:          "/iiif",
		})
	}
	if services[CantaloupeRoute] {
		routes = append(routes, plugin.IngressRoute{
			Name:          CantaloupeRoute,
			Service:       "cantaloupe",
			Router:        "cantaloupe",
			DefaultScheme: defaults.Scheme,
			DefaultDomain: defaults.Domain,
			Path:          "/cantaloupe",
		})
	}
	if services[BlazegraphRoute] {
		routes = append(routes, plugin.IngressRoute{
			Name:          BlazegraphRoute,
			Service:       "blazegraph",
			Router:        "blazegraph",
			DefaultScheme: defaults.Scheme,
			DefaultDomain: subdomain("blazegraph", defaults.Domain),
		})
	}
	return plugin.IngressRoutes{
		Domain: defaults.Domain,
		Scheme: defaults.Scheme,
		Routes: routes,
	}, nil
}

func defaultDomain(ctx *config.Context) string {
	if ctx == nil || ctx.DockerHostType == config.ContextLocal {
		return "localhost"
	}
	return ""
}

// Resolve resolves a named ISLE public endpoint.
func (p Provider) Resolve(cmd *cobra.Command, ctx *config.Context, name string) (Resolved, error) {
	return plugin.ResolveIngressRouteFromProvider(cmd, ctx, p, name)
}

// App resolves the public Drupal application endpoint.
func (p Provider) App(cmd *cobra.Command, ctx *config.Context) (Resolved, error) {
	return p.Resolve(cmd, ctx, AppRoute)
}

// FCRepo resolves the public Fedora repository endpoint.
func (p Provider) FCRepo(cmd *cobra.Command, ctx *config.Context) (Resolved, error) {
	return p.Resolve(cmd, ctx, FCRepoRoute)
}

// IIIF resolves the public Triplet IIIF endpoint.
func (p Provider) IIIF(cmd *cobra.Command, ctx *config.Context) (Resolved, error) {
	return p.Resolve(cmd, ctx, IIIFRoute)
}

// Cantaloupe resolves the public Cantaloupe endpoint.
func (p Provider) Cantaloupe(cmd *cobra.Command, ctx *config.Context) (Resolved, error) {
	return p.Resolve(cmd, ctx, CantaloupeRoute)
}

// Blazegraph resolves the public Blazegraph endpoint.
func (p Provider) Blazegraph(cmd *cobra.Command, ctx *config.Context) (Resolved, error) {
	return p.Resolve(cmd, ctx, BlazegraphRoute)
}

func subdomain(prefix, domain string) string {
	prefix = strings.Trim(strings.TrimSpace(prefix), ".")
	domain = strings.TrimSpace(domain)
	if prefix == "" || domain == "" {
		return domain
	}
	return prefix + "." + domain
}

func firstValue(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

var _ plugin.IngressRouteProvider = Provider{}

package cmd

import (
	"strings"

	isleendpoint "github.com/libops/sitectl-isle/pkg/endpoint"
	"github.com/libops/sitectl/pkg/config"
	coretraefik "github.com/libops/sitectl/pkg/services/traefik"
)

var isleEndpointProvider = isleendpoint.Provider{Defaults: isleIngressRouteDefaults}

func isleIngressRouteDefaults(ctx *config.Context) isleendpoint.Defaults {
	followUps := currentIngressFollowUps(ctx)
	domain := strings.TrimSpace(followUps["domain"])
	if domain == "" {
		domain = isleDefaultIngressDomain(ctx)
	}
	scheme := "http"
	if coretraefik.IngressModeUsesHTTPS(followUps["mode"]) {
		scheme = "https"
	}
	return isleendpoint.Defaults{Scheme: scheme, Domain: domain}
}

func isleDefaultIngressDomain(ctx *config.Context) string {
	if ctx == nil || ctx.DockerHostType == config.ContextLocal {
		return coretraefik.DefaultIngressDomain
	}
	return ""
}

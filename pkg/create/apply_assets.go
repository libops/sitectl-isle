package create

import (
	"embed"
	"io/fs"
	"strings"
)

//go:embed assets/apply/*
var applyAssets embed.FS

//go:embed assets/fcrepo/*
var fcrepoAssets embed.FS

//go:embed assets/blazegraph/*
var blazegraphAssets embed.FS

func renderApplyAsset(name string, replacements map[string]string) (string, error) {
	return renderEmbeddedAsset(applyAssets, "assets/apply/"+name, replacements)
}

func readFcrepoAsset(name string) ([]byte, error) {
	return fs.ReadFile(fcrepoAssets, "assets/fcrepo/"+name)
}

func readBlazegraphAsset(name string) ([]byte, error) {
	return fs.ReadFile(blazegraphAssets, "assets/blazegraph/"+name)
}

func renderEmbeddedAsset(assetFS fs.FS, path string, replacements map[string]string) (string, error) {
	data, err := fs.ReadFile(assetFS, path)
	if err != nil {
		return "", err
	}
	contents := string(data)
	for key, value := range replacements {
		if value == "" {
			contents = strings.ReplaceAll(contents, "{{"+key+"}}\n", "")
		}
		contents = strings.ReplaceAll(contents, "{{"+key+"}}", value)
	}
	return strings.TrimRight(contents, "\n"), nil
}

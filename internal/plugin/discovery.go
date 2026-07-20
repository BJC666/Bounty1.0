package plugin

import (
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

type Manifest struct {
	Name        string   `toml:"name"`
	Version     string   `toml:"version"`
	Description string   `toml:"description"`
	Permissions []string `toml:"permissions"`
}

func Discover(paths []string) []string {
	var plugins []string
	for _, p := range paths {
		manifestPath := filepath.Join(p, "plugin.toml")
		if _, err := os.Stat(manifestPath); err == nil {
			plugins = append(plugins, p)
		}
	}
	return plugins
}

func LoadManifest(pluginDir string) (*Manifest, error) {
	manifestPath := filepath.Join(pluginDir, "plugin.toml")
	var m Manifest
	if _, err := toml.DecodeFile(manifestPath, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

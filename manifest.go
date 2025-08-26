package plugins

//revive:disable:line-length-limit

//go:generate go tool go-jsonschema --schema-root-type bitmagnet://plugins/manifest=PluginManifest -p schema --output schema/manifest_gen.go schema/manifest.schema.json

//revive:enable:line-length-limit

import (
	_ "embed" // review for removal
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/rraymondgh/plugins/schema"
)

// LoadManifest loads and parses the manifest.json file from the given plugin directory.
// Returns the generated schema.PluginManifest type with full validation and type safety.
func LoadManifest(pluginDir string) (*schema.PluginManifest, error) {
	manifestPath := filepath.Join(pluginDir, "manifest.json")

	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read manifest file: %w", err)
	}

	var manifest schema.PluginManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("invalid manifest: %w", err)
	}

	return &manifest, nil
}

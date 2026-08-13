package mcp

import (
	"encoding/json"
	"fmt"
	"os"
)

// mcpFile is the on-disk shape of an mcp.json config file.
type mcpFile struct {
	Servers []Spec `json:"servers"`
}

// LoadSpecs reads the user-level and project-level mcp.json files and merges
// them: project entries override user entries with the same server name,
// and the merged list keeps user servers first (project order appended).
// Missing files are not an error.
func LoadSpecs(userPath, projectPath string) ([]Spec, error) {
	var out []Spec
	seen := make(map[string]int)

	load := func(path string, project bool) error {
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return fmt.Errorf("read %s: %w", path, err)
		}
		var f mcpFile
		if err := json.Unmarshal(data, &f); err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		for _, s := range f.Servers {
			if s.Name == "" {
				continue
			}
			if idx, ok := seen[s.Name]; ok {
				out[idx] = s // project overrides user by name
				continue
			}
			seen[s.Name] = len(out)
			out = append(out, s)
		}
		return nil
	}

	if err := load(userPath, false); err != nil {
		return nil, err
	}
	if err := load(projectPath, true); err != nil {
		return nil, err
	}
	return out, nil
}

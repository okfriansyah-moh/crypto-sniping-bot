package operator

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"crypto-sniping-bot/shared/contracts"

	"gopkg.in/yaml.v3"
)

// BuildConfigManifest lists YAML files under configDir with structural metadata only.
// Values are never returned — only filenames, content hash prefix, mtime, and top-level keys.
func BuildConfigManifest(configDir string) ([]contracts.ConfigManifestEntryDTO, error) {
	configDir = strings.TrimSpace(configDir)
	if configDir == "" {
		return []contracts.ConfigManifestEntryDTO{}, nil
	}

	entries, err := os.ReadDir(configDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []contracts.ConfigManifestEntryDTO{}, nil
		}
		return nil, fmt.Errorf("read config dir: %w", err)
	}

	out := make([]contracts.ConfigManifestEntryDTO, 0, len(entries))
	for _, ent := range entries {
		if ent.IsDir() || !strings.HasSuffix(ent.Name(), ".yaml") {
			continue
		}
		path := filepath.Join(configDir, ent.Name())
		info, err := ent.Info()
		if err != nil {
			return nil, fmt.Errorf("stat %s: %w", ent.Name(), err)
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", ent.Name(), err)
		}

		sum := sha256.Sum256(data)
		keys, err := yamlTopLevelKeys(data)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", ent.Name(), err)
		}

		out = append(out, contracts.ConfigManifestEntryDTO{
			Filename:     ent.Name(),
			SHA256Prefix: hex.EncodeToString(sum[:])[:8],
			LastModified: info.ModTime().UTC().Format(time.RFC3339),
			TopLevelKeys: keys,
		})
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].Filename < out[j].Filename
	})
	return out, nil
}

func yamlTopLevelKeys(data []byte) ([]string, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, err
	}
	if len(root.Content) == 0 {
		return []string{}, nil
	}
	doc := root.Content[0]
	if doc.Kind != yaml.MappingNode {
		return []string{}, nil
	}
	keys := make([]string, 0, len(doc.Content)/2)
	for i := 0; i+1 < len(doc.Content); i += 2 {
		keyNode := doc.Content[i]
		if keyNode.Kind == yaml.ScalarNode && keyNode.Value != "" {
			keys = append(keys, keyNode.Value)
		}
	}
	sort.Strings(keys)
	return keys, nil
}

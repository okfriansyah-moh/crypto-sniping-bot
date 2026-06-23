package operator

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"crypto-sniping-bot/shared/contracts"
)

const maxGateBriefBytes = 4096

// BuildGateBrief loads the newest gate_brief_*.txt snippet from logsDir.
func BuildGateBrief(logsDir string) (*contracts.GateBriefDTO, error) {
	path, err := findLatestGateBrief(logsDir)
	if err != nil {
		return nil, fmt.Errorf("find gate brief: %w", err)
	}
	if path == "" {
		return &contracts.GateBriefDTO{}, nil
	}

	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &contracts.GateBriefDTO{}, nil
		}
		return nil, err
	}
	defer f.Close()

	limited := io.LimitReader(f, maxGateBriefBytes)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}

	return &contracts.GateBriefDTO{
		Path:    filepath.Base(path),
		Content: strings.TrimSpace(string(data)),
	}, nil
}

func findLatestGateBrief(dir string) (string, error) {
	if strings.TrimSpace(dir) == "" {
		return "", nil
	}
	matches, err := filepath.Glob(filepath.Join(dir, "gate_brief_*.txt"))
	if err != nil {
		return "", err
	}
	if len(matches) == 0 {
		return "", nil
	}
	sort.Slice(matches, func(i, j int) bool {
		ii, errI := os.Stat(matches[i])
		jj, errJ := os.Stat(matches[j])
		if errI != nil || errJ != nil {
			return matches[i] > matches[j]
		}
		return ii.ModTime().After(jj.ModTime())
	})
	return matches[0], nil
}

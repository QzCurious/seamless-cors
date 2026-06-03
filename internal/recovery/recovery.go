package recovery

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type Marker struct {
	Kind  string   `json:"kind"`
	Path  string   `json:"path"`
	Files []string `json:"files"`
}

func WriteMarker(path string, marker Marker) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(marker, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func ReadMarker(path string) (Marker, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Marker{}, err
	}
	var marker Marker
	if err := json.Unmarshal(data, &marker); err != nil {
		return Marker{}, err
	}
	return marker, nil
}

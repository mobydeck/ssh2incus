package server

import (
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// InstanceProfile represents the structure of instance-profile.yaml
type InstanceProfile struct {
	Image     string                       `yaml:"image"`
	Ephemeral bool                         `yaml:"ephemeral"`
	Memory    string                       `yaml:"memory"`
	CPU       string                       `yaml:"cpu"`
	VM        bool                         `yaml:"vm"`
	Devices   map[string]map[string]string `yaml:"devices"`
	Config    map[string]string            `yaml:"config"`
}

// LoadInstanceProfile loads the instance profile from a YAML file.
func LoadInstanceProfile(path string) (*InstanceProfile, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}

	var profile InstanceProfile
	err = yaml.Unmarshal(data, &profile)
	if err != nil {
		return nil, err
	}

	return &profile, nil
}

// LoadInstanceProfileWithFallback tries to load the instance profile from multiple paths
// in order of preference until one succeeds.
func LoadInstanceProfileWithFallback(paths []string) (*InstanceProfile, error) {
	// Resolve relative paths to absolute paths
	absolutePaths := make([]string, 0, len(paths))

	cwd, err := os.Getwd()
	isCwdAvailable := err == nil

	for _, p := range paths {
		if filepath.IsAbs(p) {
			absolutePaths = append(absolutePaths, p)
		} else if isCwdAvailable {
			// Resolve relative path against current working directory
			absolutePaths = append(absolutePaths, path.Join(cwd, p))
		}
		// If cwd is not available or path is relative but we can't get cwd, skip this path
	}

	// Try each absolute path in order
	var lastErr error
	for _, p := range absolutePaths {
		prof, err := LoadInstanceProfile(p)
		if err == nil {
			return prof, nil
		}
		lastErr = err
	}

	return nil, fmt.Errorf("no instance profile found in any of the provided paths: %v", lastErr)
}

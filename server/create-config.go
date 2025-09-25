package server

import (
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strconv"

	"gopkg.in/yaml.v3"
)

var (
	createConfigFilename = "create-config.yaml"
)

// CreateConfig represents the structure of create-config.yaml
type InstanceCreateConfigV1 struct {
	Image     string                       `yaml:"image" json:"image"`
	Ephemeral bool                         `yaml:"ephemeral" json:"ephemeral"`
	Memory    string                       `yaml:"memory" json:"memory"`
	Cpu       string                       `yaml:"cpu" json:"cpu"`
	Disk      string                       `yaml:"disk" json:"disk"`
	Vm        bool                         `yaml:"vm" json:"vm"`
	Devices   map[string]map[string]string `yaml:"devices" json:"devices"`
	Config    map[string]string            `yaml:"config" json:"config"`
}

func (c *InstanceCreateConfigV1) MemoryInt() int {
	i, _ := strconv.Atoi(c.Memory)
	return i
}

func (c *InstanceCreateConfigV1) CpuInt() int {
	i, _ := strconv.Atoi(c.Cpu)
	return i
}

func (c *InstanceCreateConfigV1) DiskInt() int {
	i, _ := strconv.Atoi(c.Disk)
	return i
}

// LoadCreateConfig loads the instance config from a YAML file.
func LoadCreateConfig(path string) (*InstanceCreateConfigV1, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}

	var c InstanceCreateConfigV1
	err = yaml.Unmarshal(data, &c)
	if err != nil {
		return nil, err
	}

	return &c, nil
}

// LoadCreateConfigWithFallback tries to load the instance config from multiple paths
// in order of preference until one succeeds.
func LoadCreateConfigWithFallback(paths []string) (*InstanceCreateConfigV1, error) {
	// Resolve relative paths to absolute paths
	absolutePaths := make([]string, 0, len(paths))

	cwd, err := os.Getwd()
	isCwdAvailable := err == nil

	for _, p := range paths {
		if filepath.IsAbs(p) {
			absolutePaths = append(absolutePaths, path.Join(p, createConfigFilename))
		} else if isCwdAvailable {
			// Resolve relative path against current working directory
			absolutePaths = append(absolutePaths, path.Join(cwd, p, createConfigFilename))
		}
		// If cwd is not available or path is relative but we can't get cwd, skip this path
	}

	// Try each absolute path in order
	var lastErr error
	for _, p := range absolutePaths {
		prof, err := LoadCreateConfig(p)
		if err == nil {
			return prof, nil
		}
		lastErr = err
	}

	return nil, fmt.Errorf("no instance create config found: %v", lastErr)
}

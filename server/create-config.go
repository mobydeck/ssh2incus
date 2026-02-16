package server

import (
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

var (
	createConfigFilename = "create-config.yaml"
)

// CreateConfig represents the structure of create-config.yaml
type CreateConfig struct {
	Version string `yaml:"version"`

	Defaults InstanceCreateConfig `yaml:"defaults"`

	Profiles map[string]InstanceCreateConfig `yaml:"profiles"`

	config     InstanceCreateConfig `yaml:"-"`
	configFile string               `yaml:"-"`
}

type InstanceCreateConfig struct {
	Image     *string                      `yaml:"image,omitempty" json:"image"`
	Memory    *string                      `yaml:"memory,omitempty" json:"memory"`
	CPU       *string                      `yaml:"cpu,omitempty" json:"cpu"`
	Disk      *string                      `yaml:"disk,omitempty" json:"disk"`
	Ephemeral *bool                        `yaml:"ephemeral,omitempty" json:"ephemeral"`
	VM        *bool                        `yaml:"vm,omitempty" json:"vm"`
	Devices   map[string]map[string]string `yaml:"devices,omitempty" json:"devices"`
	Config    map[string]string            `yaml:"config,omitempty" json:"config"`
}

func (c *CreateConfig) Image() string {
	if c.config.Image != nil {
		return *c.config.Image
	}
	return ""
}

func (c *CreateConfig) Memory() int {
	if c.config.Memory != nil {
		i, _ := strconv.Atoi(*c.config.Memory)
		return i
	}
	return 0
}

func (c *CreateConfig) CPU() int {
	if c.config.CPU != nil {
		i, _ := strconv.Atoi(*c.config.CPU)
		return i
	}
	return 0
}

func (c *CreateConfig) Disk() int {
	if c.config.Disk != nil {
		i, _ := strconv.Atoi(*c.config.Disk)
		return i
	}
	return 0
}

func (c *CreateConfig) Ephemeral() bool {
	if c.config.Ephemeral != nil {
		return *c.config.Ephemeral
	}
	return false
}

func (c *CreateConfig) VM() bool {
	if c.config.VM != nil {
		return *c.config.VM
	}
	return false
}

func (c *CreateConfig) Config() map[string]string {
	if c.config.Config != nil {
		return c.config.Config
	}
	return make(map[string]string)
}

func (c *CreateConfig) Devices() map[string]map[string]string {
	if c.config.Devices != nil {
		return c.config.Devices
	}
	return make(map[string]map[string]string)
}

func (c *CreateConfig) ConfigFile() string {
	return c.configFile
}

// processIncludes processes file include directives in config maps
func processIncludes(configMap map[string]string, configDir string) error {
	if configMap == nil {
		return nil
	}

	for key, value := range configMap {
		trimmed := strings.TrimSpace(value)
		var filename string

		if strings.HasPrefix(trimmed, "!include ") {
			filename = strings.TrimSpace(trimmed[9:]) // len("!include ") = 9
		} else if strings.HasPrefix(trimmed, "<@") {
			filename = strings.TrimSpace(trimmed[2:]) // len("<@") = 2
		} else {
			continue // Not an include directive
		}

		// Resolve relative paths
		var filePath string
		if filepath.IsAbs(filename) {
			filePath = filename
		} else {
			// Try relative to config file directory first
			configRelativePath := filepath.Join(configDir, filename)
			if _, err := os.Stat(configRelativePath); err == nil {
				filePath = configRelativePath
			} else {
				// Fall back to current working directory
				cwd, err := os.Getwd()
				if err != nil {
					return fmt.Errorf("could not get current working directory: %v", err)
				}
				filePath = filepath.Join(cwd, filename)
			}
		}

		// Read file content
		content, err := os.ReadFile(filePath)
		if err != nil {
			return fmt.Errorf("could not read include file %s: %v", filePath, err)
		}

		// Replace the value with file content
		configMap[key] = string(content)
	}

	return nil
}

// MergeProfiles merges the specified profiles with defaults and returns the result
// without modifying the receiver's state
func (c *CreateConfig) MergeProfiles(profiles []string) (InstanceCreateConfig, error) {
	result := c.Defaults

	if len(profiles) == 0 {
		return result, nil
	}

	for _, name := range profiles {
		if name == "" {
			continue
		}
		profile, ok := c.Profiles[name]
		if !ok {
			return InstanceCreateConfig{}, fmt.Errorf("profile %q does not exist", name)
		}

		if profile.Image != nil {
			result.Image = profile.Image
		}
		if profile.Ephemeral != nil {
			result.Ephemeral = profile.Ephemeral
		}
		if profile.Memory != nil {
			result.Memory = profile.Memory
		}
		if profile.CPU != nil {
			result.CPU = profile.CPU
		}
		if profile.Disk != nil {
			result.Disk = profile.Disk
		}
		if profile.VM != nil {
			result.VM = profile.VM
		}

		if profile.Config != nil {
			if result.Config == nil {
				result.Config = make(map[string]string)
			}
			for k, v := range profile.Config {
				result.Config[k] = v
			}
		}

		if profile.Devices != nil {
			if result.Devices == nil {
				result.Devices = make(map[string]map[string]string)
			}
			for deviceName, deviceConfig := range profile.Devices {
				if deviceConfig == nil {
					continue
				}
				cloned := make(map[string]string, len(deviceConfig))
				for k, v := range deviceConfig {
					cloned[k] = v
				}
				result.Devices[deviceName] = cloned
			}
		}
	}

	return result, nil
}

// GetProfiles returns all available profiles from the create config
func (c *CreateConfig) GetProfiles() map[string]InstanceCreateConfig {
	return c.Profiles
}

// LoadCreateConfig loads the instance config from a YAML file.
func LoadCreateConfig(path string) (*CreateConfig, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}

	var c CreateConfig
	err = yaml.Unmarshal(data, &c)
	if err != nil {
		return nil, err
	}

	// Get the directory of the config file for resolving relative includes
	configDir := filepath.Dir(path)

	// Process file includes in defaults config
	err = processIncludes(c.Defaults.Config, configDir)
	if err != nil {
		return nil, fmt.Errorf("error processing includes in defaults config: %v", err)
	}

	// Process file includes in profile configs
	for profileName, profile := range c.Profiles {
		err = processIncludes(profile.Config, configDir)
		if err != nil {
			return nil, fmt.Errorf("error processing includes in profile %s config: %v", profileName, err)
		}
	}

	c.config = c.Defaults
	c.configFile = path

	return &c, nil
}

// LoadCreateConfigWithFallback tries to load the instance config from multiple paths
// in order of preference until one succeeds.
func LoadCreateConfigWithFallback(paths []string) (*CreateConfig, error) {
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
		c, err := LoadCreateConfig(p)
		if err == nil {
			return c, nil
		}
		lastErr = err
	}

	return nil, fmt.Errorf("no instance create config found: %v", lastErr)
}

func (c *CreateConfig) ApplyProfiles(profiles []string) error {
	c.config = c.Defaults

	if len(profiles) == 0 {
		return nil
	}

	for _, name := range profiles {
		if name == "" {
			continue
		}
		profile, ok := c.Profiles[name]
		if !ok {
			return fmt.Errorf("profile %q does not exist", name)
		}

		if profile.Image != nil {
			c.config.Image = profile.Image
		}
		if profile.Ephemeral != nil {
			c.config.Ephemeral = profile.Ephemeral
		}
		if profile.Memory != nil {
			c.config.Memory = profile.Memory
		}
		if profile.CPU != nil {
			c.config.CPU = profile.CPU
		}
		if profile.Disk != nil {
			c.config.Disk = profile.Disk
		}
		if profile.VM != nil {
			c.config.VM = profile.VM
		}

		if profile.Config != nil {
			if c.config.Config == nil {
				c.config.Config = make(map[string]string)
			}
			for k, v := range profile.Config {
				c.config.Config[k] = v
			}
		}

		if profile.Devices != nil {
			if c.config.Devices == nil {
				c.config.Devices = make(map[string]map[string]string)
			}
			for deviceName, deviceConfig := range profile.Devices {
				if deviceConfig == nil {
					continue
				}
				cloned := make(map[string]string, len(deviceConfig))
				for k, v := range deviceConfig {
					cloned[k] = v
				}
				c.config.Devices[deviceName] = cloned
			}
		}
	}

	return nil
}

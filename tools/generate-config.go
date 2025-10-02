//go:build ignore
// +build ignore

package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"unicode"

	"ssh2incus/server"

	"go.yaml.in/yaml/v3"
)

func main() {
	// Read options.yaml
	optionsData, err := os.ReadFile("server/options.yaml")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading server/options.yaml: %v\n", err)
		os.Exit(1)
	}

	var options []server.Option
	err = yaml.Unmarshal(optionsData, &options)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing options.yaml: %v\n", err)
		os.Exit(1)
	}

	// Generate config.yaml content
	var sb strings.Builder
	sb.WriteString("# Default configuration template for ssh2incus.\n")
	sb.WriteString("# Each setting corresponds to a command-line flag; uncomment and adjust as needed.\n")
	sb.WriteString("# Flags set in /etc/default/ssh2incus have precedence over configuration file settings.\n\n")

	// Collect non-deprecated options and write them alphabetically
	filteredOpts := make([]server.Option, 0, len(options))
	for _, opt := range options {
		if opt.Deprecated {
			continue
		}

		filteredOpts = append(filteredOpts, opt)
	}

	writeOptionsAlphabetically(&sb, filteredOpts)

	// Create packaging directory if it doesn't exist
	err = os.MkdirAll("packaging", 0755)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating packaging directory: %v\n", err)
		os.Exit(1)
	}

	// Write to packaging/config.yaml
	err = os.WriteFile("packaging/config.yaml", []byte(sb.String()), 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error writing packaging/config.yaml: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Successfully generated packaging/config.yaml")
}

func writeOptionsAlphabetically(sb *strings.Builder, options []server.Option) {
	sort.SliceStable(options, func(i, j int) bool {
		return options[i].Name < options[j].Name
	})

	for _, opt := range options {
		writeOption(sb, opt)
	}
}

func writeOption(sb *strings.Builder, opt server.Option) {
	// Skip help and version flags
	if opt.Name == "help" || opt.Name == "version" || strings.HasPrefix(opt.Name, "dump") {
		return
	}

	// Create comment with help description and flag info
	flagInfo := fmt.Sprintf("--%s", opt.Name)
	if opt.Short != "" {
		flagInfo += fmt.Sprintf(", -%s", opt.Short)
	}

	help := string(unicode.ToUpper(rune(opt.Help[0]))) + opt.Help[1:]
	sb.WriteString(fmt.Sprintf("# %s (flag: %s).\n", help, flagInfo))

	// Convert kebab-case to snake_case for YAML keys
	// yamlKey := strings.ReplaceAll(opt.Name, "-", "_")
	yamlKey := opt.Name

	// Handle special cases and determine if option should be commented out
	commented := shouldCommentOut(opt)
	prefix := ""
	if commented {
		prefix = "# "
	}

	// Handle special formatting for certain options
	switch opt.Name {
	// case "groups":
	// 	// Special handling for groups - show as array format
	default:
		// Standard key: value format
		if opt.Default != nil {
			switch val := opt.Default.(type) {
			case string:
				if val == "" {
					sb.WriteString(fmt.Sprintf("%s%s: \"\"\n", prefix, yamlKey))
				} else {
					sb.WriteString(fmt.Sprintf("%s%s: \"%s\"\n", prefix, yamlKey, val))
				}
			case bool:
				sb.WriteString(fmt.Sprintf("%s%s: %t\n", prefix, yamlKey, val))
			default:
				sb.WriteString(fmt.Sprintf("%s%s: %v\n", prefix, yamlKey, val))
			}
		} else {
			sb.WriteString(fmt.Sprintf("%s%s: \n", prefix, yamlKey))
		}
	}

	sb.WriteString("\n")
}

func shouldCommentOut(opt server.Option) bool {
	// Options that should be uncommented (active by default)
	activeOptions := map[string]bool{
		// "listen":        true,
	}

	// Comment out string options with empty defaults (except listen)
	if opt.Type == server.StringType {
		if defaultVal, ok := opt.Default.(string); ok {
			if defaultVal == "" && opt.Name != "socket" {
				return true
			}
		}
	}

	return !activeOptions[opt.Name]
}

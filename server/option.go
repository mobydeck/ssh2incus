package server

import (
	_ "embed"

	log "github.com/sirupsen/logrus"
	"go.yaml.in/yaml/v3"
)

//go:embed options.yaml
var configOptionsYaml []byte
var configOptions []Option

const (
	BooleanType  OptionType = "boolean"
	StringType   OptionType = "string"
	IntegerType  OptionType = "integer"
	DurationType OptionType = "duration"
)

type OptionType string

type Option struct {
	Name       string     `yaml:"name"`
	Short      string     `yaml:"short"`
	Alias      string     `yaml:"alias"`
	Help       string     `yaml:"help"`
	Type       OptionType `yaml:"type"`
	Default    any        `yaml:"default"`
	Deprecated bool       `yaml:"deprecated"`
}

func init() {
	err := yaml.Unmarshal(configOptionsYaml, &configOptions)
	if err != nil {
		log.Fatal(err)
	}
}

func ConfigOptions() []Option {
	return configOptions
}

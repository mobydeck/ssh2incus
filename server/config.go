package server

import (
	"os"
	"strconv"
	"strings"
	"time"

	"ssh2incus/pkg"
)

var config *Config

type Config struct {
	App  *pkg.App
	Args []string

	Listen        string
	Socket        string
	Shell         string
	Groups        string
	HealthCheck   string
	IncusSocket   string
	Remote        string
	URL           string
	ClientCert    string
	ClientKey     string
	ServerCert    string
	Master        bool
	Debug         bool
	Banner        bool
	NoAuth        bool
	InAuth        bool
	Welcome       bool
	AllowedGroups []string
	IdleTimeout   time.Duration

	IncusInfo map[string]interface{}
}

func SetConfig(c *Config) {
	config = c
}

func (c *Config) SocketFdEnvName() string {
	return config.App.NAME() + "_SOCKET_FD"
}

func (c *Config) SocketFdEnvValue(f *os.File) string {
	return strconv.Itoa(int(f.Fd()))
}

func (c *Config) SocketFdEnvString(f *os.File) string {
	return config.SocketFdEnvName() + "=" + config.SocketFdEnvValue(f)
}

func (c *Config) ArgsEnvName() string {
	return config.App.NAME() + "_ARGS"
}

func (c *Config) ArgsEnvValue() string {
	return strings.Join(config.Args, " ")
}

func (c *Config) ArgsEnvString() string {
	return config.ArgsEnvName() + "=" + config.ArgsEnvValue()

}

package ssh2incus

import (
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"os"
	"path"
	"reflect"
	"runtime"
	"strings"
	"time"

	"ssh2incus/pkg"
	"ssh2incus/server"

	log "github.com/sirupsen/logrus"
	flag "github.com/spf13/pflag"
)

var (
	app     *pkg.App
	version = "devel"
	edition = "ce"
	gitHash = ""
	builtAt = ""
)

var (
	idleTimeout = 180 * time.Second

	flagHelp        = false
	flagDebug       = false
	flagPprof       = false
	flagMaster      = false
	flagBanner      = false
	flagNoauth      = false
	flagWelcome     = false
	flagListen      = ":2222"
	flagPprofListen = ":6060"
	flagGroups      = "incus"
	flagSocket      = ""
	flagURL         = ""
	flagRemote      = ""
	flagClientCert  = ""
	flagClientKey   = ""
	flagServerCert  = ""
	flagShell       = ""

	flagHealthCheck = ""

	flagVersion = false

	allowedGroups []string
)

type Package struct{}

func init() {
	app = pkg.NewApp(pkg.AppConfig{
		Name:    reflect.TypeOf(Package{}).PkgPath(),
		Version: version,
		Edition: edition,
		GitHash: gitHash,
		BuiltAt: builtAt,
	})

	var args []string
	argsEnv := os.Getenv(app.NAME() + "_ARGS")
	if argsEnv != "" {
		args = parseArgs(argsEnv)
	} else if len(os.Args) > 1 {
		args = os.Args[1:]
	}

	flags := flag.NewFlagSet("flags", flag.ExitOnError)
	flags.BoolVarP(&flagHelp, "help", "h", flagHelp, "print help")
	flags.BoolVarP(&flagDebug, "debug", "d", flagDebug, "enable debug log")
	flags.BoolVarP(&flagPprof, "pprof", "", flagPprof, "enable pprof")
	flags.BoolVarP(&flagMaster, "master", "m", flagMaster, "start master process and spawn workers")
	flags.BoolVarP(&flagBanner, "banner", "b", flagBanner, "show banner on login")
	flags.BoolVarP(&flagNoauth, "noauth", "", flagNoauth, "disable SSH authentication completely")
	flags.BoolVarP(&flagWelcome, "welcome", "w", flagWelcome, "show welcome message to shell users")
	flags.BoolVarP(&flagVersion, "version", "v", flagVersion, "print version")
	flags.StringVarP(&flagShell, "shell", "", flagShell, "shell access command: login, su or default shell")
	flags.StringVarP(&flagListen, "listen", "l", flagListen, "listen on :2222 or 127.0.0.1:2222")
	flags.StringVarP(&flagSocket, "socket", "s", flagSocket, "Incus socket or use INCUS_SOCKET")
	flags.StringVarP(&flagRemote, "url", "u", flagURL, "Incus remote url starting with https://")
	flags.StringVarP(&flagRemote, "remote", "r", flagRemote, "Incus remote defined in config.yml, e.g. my-remote")
	flags.StringVarP(&flagClientCert, "client-cert", "c", flagClientCert, "client certificate for remote")
	flags.StringVarP(&flagClientKey, "client-key", "k", flagClientKey, "client key for remote")
	flags.StringVarP(&flagServerCert, "server-cert", "t", flagServerCert, "server certificate for remote")
	flags.StringVarP(&flagGroups, "groups", "g", flagGroups, "list of groups members of which allowed to connect")
	flags.StringVarP(&flagPprofListen, "pprof-listen", "", flagPprofListen, "pprof listen on :6060 or 127.0.0.1:6060")
	flags.StringVarP(&flagHealthCheck, "healthcheck", "", flagHealthCheck, "enable Incus health check every X minutes, e.g. \"5m\"")
	err := flags.Parse(args)
	if err != nil {
		log.Fatal(err)
	}

	if flagHelp {
		fmt.Printf("%s\n\n", app.LongName())
		flags.PrintDefaults()
		fmt.Println()
		os.Exit(0)
	}

	if flagVersion {
		fmt.Printf("%s\nBuilt at: %s\n", app.LongName(), app.BuiltAt())
		os.Exit(0)
	}

	if flagPprof {
		if flagMaster {
			log.Warn("pprof is not supported in master mode")
			flagPprof = false
		} else {
			log.Infof("Enabling pprof on %s", flagPprofListen)
			go func() {
				err := http.ListenAndServe(flagPprofListen, nil)
				if err != nil {
					log.Fatal(err)
				}
			}()
		}
	}

	if flagSocket == "" {
		flagSocket = os.Getenv("INCUS_SOCKET")
	}

	if flagClientCert == "" {
		flagClientCert = os.Getenv("INCUS_CLIENT_CERT")
	}
	if flagClientKey == "" {
		flagClientKey = os.Getenv("INCUS_CLIENT_KEY")
	}

	allowedGroups = strings.Split(flagGroups, ",")

	log.SetOutput(os.Stdout)
	log.SetReportCaller(true)
	if flagDebug {
		log.SetLevel(log.DebugLevel)
		log.SetFormatter(&log.TextFormatter{
			CallerPrettyfier: func(f *runtime.Frame) (string, string) {
				filename := path.Base(f.File)
				return fmt.Sprintf("> %s()", f.Function), fmt.Sprintf("%s:%d", filename, f.Line)
			},
		})
	} else {
		log.SetLevel(log.InfoLevel)
	}

	if flagNoauth {
		log.Warn("ssh authentication disabled")
	}

	log.Debugf("DEBUG logging enabled")

	config := &server.Config{
		App:           app,
		Args:          args,
		Master:        flagMaster,
		Debug:         flagDebug,
		Banner:        flagBanner,
		Listen:        flagListen,
		Socket:        flagSocket,
		Noauth:        flagNoauth,
		Welcome:       flagWelcome,
		Shell:         flagShell,
		Groups:        flagGroups,
		HealthCheck:   flagHealthCheck,
		AllowedGroups: allowedGroups,
		ClientCert:    flagClientCert,
		ClientKey:     flagClientKey,
		ServerCert:    flagServerCert,
		URL:           flagURL,
		Remote:        flagRemote,
		IdleTimeout:   idleTimeout,
	}

	server.WithConfig(config).Run()
}

// parseArgs parses a string into command-line arguments,
// handling quoted sections properly
func parseArgs(s string) []string {
	var args []string
	var currentArg strings.Builder
	inQuotes := false
	escapeNext := false

	for _, r := range s {
		if escapeNext {
			currentArg.WriteRune(r)
			escapeNext = false
			continue
		}

		if r == '\\' {
			escapeNext = true
			continue
		}

		if r == '"' {
			inQuotes = !inQuotes
			continue
		}

		if r == ' ' && !inQuotes {
			if currentArg.Len() > 0 {
				args = append(args, currentArg.String())
				currentArg.Reset()
			}
			continue
		}

		currentArg.WriteRune(r)
	}

	if currentArg.Len() > 0 {
		args = append(args, currentArg.String())
	}

	return args
}

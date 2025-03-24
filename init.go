package ssh2incus

import (
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"os"
	"path"
	"reflect"
	"runtime"
	"ssh2incus/server"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
	flag "github.com/spf13/pflag"
)

var (
	version = "devel"
	edition = "ce"
	githash = ""
	builtat = ""
)

type App struct {
	Name     string
	Version  string
	Edition  string
	GitHash  string
	LongName string
	BuiltAt  string
}

var app *App

var (
	idleTimeout = 180 * time.Second

	flagDebug       = false
	flagPprof       = false
	flagBanner      = false
	flagListen      = ":2222"
	flagHelp        = false
	flagSocket      = ""
	flagURL         = ""
	flagRemote      = ""
	flagClientCert  = ""
	flagClientKey   = ""
	flagServerCert  = ""
	flagNoauth      = false
	flagGroups      = "incus"
	flagPprofListen = ":6060"
	flagShell       = ""

	flagHealthCheck = ""

	flagVersion = false

	allowedGroups []string
)

func init() {
	app = new(App)
	app.Name = reflect.TypeOf(App{}).PkgPath()
	app.Edition = edition
	app.Version = version
	app.GitHash = githash
	app.BuiltAt = builtat
	app.LongName = fmt.Sprintf("%s %s", app.Name, app.Version)
	if app.GitHash != "" {
		app.LongName += fmt.Sprintf(" (%s)", app.GitHash)
	}

	flag.BoolVarP(&flagHelp, "help", "h", flagHelp, "print help")
	flag.BoolVarP(&flagDebug, "debug", "d", flagDebug, "enable debug log")
	flag.BoolVarP(&flagPprof, "pprof", "", flagPprof, "enable pprof")
	flag.BoolVarP(&flagBanner, "banner", "b", flagBanner, "show banner on login")
	flag.BoolVarP(&flagNoauth, "noauth", "", flagNoauth, "disable SSH authentication completely")
	flag.StringVarP(&flagShell, "shell", "", flagShell, "shell access command: login, su or default shell")
	flag.BoolVarP(&flagVersion, "version", "v", flagVersion, "print version")
	flag.StringVarP(&flagListen, "listen", "l", flagListen, "listen on :2222 or 127.0.0.1:2222")
	flag.StringVarP(&flagSocket, "socket", "s", flagSocket, "Incus socket or use INCUS_SOCKET")
	flag.StringVarP(&flagRemote, "url", "u", flagURL, "Incus remote url starting with https://")
	flag.StringVarP(&flagRemote, "remote", "r", flagRemote, "Incus remote defined in config.yml, e.g. my-remote")
	flag.StringVarP(&flagClientCert, "client-cert", "c", flagClientCert, "client certificate for remote")
	flag.StringVarP(&flagClientKey, "client-key", "k", flagClientKey, "client key for remote")
	flag.StringVarP(&flagServerCert, "server-cert", "t", flagServerCert, "server certificate for remote")
	flag.StringVarP(&flagGroups, "groups", "g", flagGroups, "list of groups members of which allowed to connect")
	flag.StringVarP(&flagPprofListen, "pprof-listen", "", flagPprofListen, "pprof listen on :6060 or 127.0.0.1:6060")
	flag.StringVarP(&flagHealthCheck, "healthcheck", "", flagHealthCheck, "enable Incus health check every X minutes, e.g. \"5m\"")
	flag.Parse()

	if flagPprof {
		go func() {
			http.ListenAndServe(flagPprofListen, nil)
		}()
	}

	if flagHelp {
		fmt.Printf("%s\n\n", app.LongName)
		flag.PrintDefaults()
		os.Exit(0)
	}

	if flagVersion {
		fmt.Printf("%s\nBuilt at: %s\n", app.LongName, app.BuiltAt)
		os.Exit(0)
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

	fmt.Printf("Starting %s %s on %s\n", app.Name, app.Version, flagListen)

	if flagNoauth {
		log.Warn("SSH authentication disabled")
	}

	log.Debugf("Debug logging enabled")

	config := &server.Config{
		IdleTimeout:   idleTimeout,
		Debug:         flagDebug,
		Banner:        flagBanner,
		Listen:        flagListen,
		Socket:        flagSocket,
		Noauth:        flagNoauth,
		Shell:         flagShell,
		Groups:        flagGroups,
		HealthCheck:   flagHealthCheck,
		AllowedGroups: allowedGroups,
		ClientCert:    flagClientCert,
		ClientKey:     flagClientKey,
		ServerCert:    flagServerCert,
		URL:           flagURL,
		Remote:        flagRemote,
	}
	server.Run(config)
}

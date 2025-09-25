package ssh2incus

import (
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"os"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"time"

	"ssh2incus/pkg"
	"ssh2incus/pkg/ssh"
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
	flagNoAuth      = false
	flagInAuth      = false
	flagPassAuth    = false
	flagAllowCreate = false
	flagWelcome     = false
	flagListen      = ":2222"
	flagPprofListen = ":6060"
	flagGroups      = "incus,incus-admin"
	flagTermMux     = "tmux"
	flagSocket      = ""
	flagURL         = ""
	flagRemote      = ""
	flagAuthMethods = ""
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
	flags.BoolVarP(&flagNoAuth, "noauth", "", flagNoAuth, "disable SSH authentication completely")
	flags.BoolVarP(&flagInAuth, "inauth", "I", flagInAuth, "enable authentication using instance keys")
	flags.BoolVarP(&flagPassAuth, "password-auth", "P", flagPassAuth, "enable password authentication")
	flags.BoolVarP(&flagAllowCreate, "allow-create", "C", flagAllowCreate, "allow creating new instances")
	flags.BoolVarP(&flagWelcome, "welcome", "w", flagWelcome, "show welcome message to users connecting to shell")
	flags.BoolVarP(&flagVersion, "version", "v", flagVersion, "print version")
	flags.StringVarP(&flagShell, "shell", "S", flagShell, "shell access command: login, su, sush or user shell (default)")
	flags.StringVarP(&flagListen, "listen", "l", flagListen, `listen on ":port" or "host:port"`)
	flags.StringVarP(&flagSocket, "socket", "s", flagSocket, "Incus socket to connect to (optional, defaults to INCUS_SOCKET env)")
	flags.StringVarP(&flagURL, "url", "u", flagURL, "Incus remote url to connect to (should start with https://)")
	flags.StringVarP(&flagRemote, "remote", "r", flagRemote, "default Incus remote to use")
	flags.StringVarP(&flagAuthMethods, "auth-methods", "", flagAuthMethods, `enable auth method chain, e.g.: "publickey,password"`)
	flags.StringVarP(&flagClientCert, "client-cert", "c", flagClientCert, "client certificate for remote")
	flags.StringVarP(&flagClientKey, "client-key", "k", flagClientKey, "client key for remote")
	flags.StringVarP(&flagServerCert, "server-cert", "t", flagServerCert, "server certificate for remote")
	flags.StringVarP(&flagGroups, "groups", "g", flagGroups, "list of groups members of which allowed to connect")
	flags.StringVarP(&flagTermMux, "term-mux", "T", flagGroups, "terminal multiplexer: tmux (default) or screen")
	flags.StringVarP(&flagPprofListen, "pprof-listen", "", flagPprofListen, `pprof listen on ":port" or "host:port"`)
	flags.StringVarP(&flagHealthCheck, "healthcheck", "H", flagHealthCheck, `enable Incus health check every X minutes, e.g. "5m"`)
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

	setupLogger()

	if flagPprof {
		if flagMaster {
			log.Warn("pprof: not enabling in master mode")
			flagPprof = false
		} else {
			log.Infof("pprof: enabling on %s", flagPprofListen)
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

	if flagNoAuth && !flagInAuth {
		log.Warn("ssh: authentication disabled")
	}

	authMethods, err := parseAuthMethods(flagAuthMethods)
	if err != nil {
		log.Fatalf("auth-methods: %v", err)
	} else {
		if len(authMethods) > 0 {
			log.Warnf("auth: enabled authentication methods chain: %s", strings.Join(authMethods, ","))
			if flagNoAuth {
				log.Warn("auth: noauth cannot be enabled with auth-methods")
				flagNoAuth = false
			}
		}
		// Enabled password auth if it's part of auth methods chain
		if slices.Contains(authMethods, ssh.PasswordAuthMethod) {
			flagPassAuth = true
		}
	}

	log.Debugf("log: DEBUG enabled")

	config := &server.Config{
		App:           app,
		Args:          args,
		Master:        flagMaster,
		Debug:         flagDebug,
		Banner:        flagBanner,
		Listen:        flagListen,
		Socket:        flagSocket,
		NoAuth:        flagNoAuth && !flagInAuth, // inauth overrides noauth
		InAuth:        flagInAuth,
		PassAuth:      flagPassAuth,
		AllowCreate:   flagAllowCreate,
		Welcome:       flagWelcome,
		Shell:         flagShell,
		Groups:        flagGroups,
		TermMux:       flagTermMux,
		HealthCheck:   flagHealthCheck,
		AuthMethods:   authMethods,
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

func setupLogger() {
	log.SetOutput(os.Stdout)
	isTerminal := app.IsTerminal()
	logFormatter := &log.TextFormatter{
		DisableQuote:              !isTerminal,
		DisableTimestamp:          !isTerminal,
		EnvironmentOverrideColors: true,
	}
	if flagDebug {
		log.SetLevel(log.DebugLevel)
		log.SetReportCaller(true)
		logFormatter.CallerPrettyfier = func(f *runtime.Frame) (string, string) {
			return fmt.Sprintf("%s()", f.Function), fmt.Sprintf("%s:%d", f.File, f.Line)
		}
	} else {
		log.SetLevel(log.InfoLevel)
	}
	log.SetFormatter(logFormatter)
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

// parseAuthMethods parses the flagAuthMethods string into a slice of valid authentication methods.
// It supports comma-separated values from the set: "publickey", "password", etc.
// Returns an error if any invalid method is specified.
func parseAuthMethods(flagAuthMethods string) ([]string, error) {
	defaultMethods := []string{}
	if flagAuthMethods == "" {
		return defaultMethods, nil
	}

	methods := strings.Split(flagAuthMethods, ",")
	validMethods := map[string]bool{
		ssh.PasswordAuthMethod:  true,
		ssh.PublickeyAuthMethod: true,
		//ssh.KeyboardInteractiveAuthMethod: true, // not yet supported
	}

	result := make([]string, 0, len(methods))

	for _, method := range methods {
		method = strings.TrimSpace(method)
		if !validMethods[method] {
			return nil, fmt.Errorf("invalid authentication method: %s", method)
		}
		result = append(result, method)
	}

	// Ensure at least one method is specified
	if len(result) == 0 {
		return defaultMethods, nil
	}

	return result, nil
}

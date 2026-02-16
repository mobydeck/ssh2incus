package ssh2incus

//go:generate go run ./tools/generate-config.go

import (
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"os"
	"path"
	"path/filepath"
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
	"github.com/spf13/viper"
	"go.yaml.in/yaml/v3"
)

var (
	app     *pkg.App
	version = "devel"
	edition = "ce"
	gitHash = ""
	builtAt = ""
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

	viper.SetEnvPrefix(app.NAME())
	//viper.AutomaticEnv()
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")
	viper.SetOptions()
	if home, err := os.UserHomeDir(); err == nil {
		viper.AddConfigPath(filepath.Join(home, ".config", app.Name()))
	}
	viper.AddConfigPath(filepath.Join("/etc", app.Name()))
	err := viper.ReadInConfig()
	if err != nil {
		log.Warnf("failed to read config file: %v", err)
	}

	flags := flag.NewFlagSet("flags", flag.ExitOnError)
	options := server.ConfigOptions()
	for _, o := range options {
		if o.Alias != "" {
			viper.RegisterAlias(o.Alias, o.Name)
		}
		switch o.Type {
		case server.BooleanType:
			flags.BoolP(o.Name, o.Short, o.Default.(bool), o.Help)
		case server.StringType:
			flags.StringP(o.Name, o.Short, o.Default.(string), o.Help)
		case server.IntegerType:
			flags.IntP(o.Name, o.Short, o.Default.(int), o.Help)
		case server.DurationType:
			dur, _ := time.ParseDuration(o.Default.(string))
			flags.DurationP(o.Name, o.Short, dur, o.Help)
		}
		//if o.Deprecated {
		//	flags.MarkDeprecated(o.Name, o.Help)
		//}
	}

	err = flags.Parse(args)
	if err != nil {
		log.Fatal(err)
	}
	viper.BindPFlags(flags)

	if viper.GetBool("help") {
		fmt.Printf("%s\n\n", app.LongName())
		flags.SetOutput(os.Stdout)
		flags.PrintDefaults()
		fmt.Println()
		os.Exit(0)
	}

	if viper.GetBool("version") {
		fmt.Printf("%s\nBuilt at: %s\n", app.LongName(), app.BuiltAt())
		os.Exit(0)
	}

	setupLogger()

	if viper.GetBool("inauth") {
		viper.Set("instance-auth", true)
		log.Warn("flag --inauth is deprecated, use --instance-auth or -I")
	}

	if viper.GetBool("pprof") {
		if viper.GetBool("master") {
			log.Warn("pprof: not enabling in master mode")
			viper.Set("pprof", false)
		} else {
			log.Infof("pprof: enabling on %s", viper.GetString("pprof-listen"))
			go func() {
				err := http.ListenAndServe(viper.GetString("pprof-listen"), nil)
				if err != nil {
					log.Fatal(err)
				}
			}()
		}
	}

	if viper.GetString("socket") == "" {
		viper.Set("socket", os.Getenv("INCUS_SOCKET"))
	}

	if viper.GetString("client-cert") == "" {
		viper.Set("client-cert", os.Getenv("INCUS_CLIENT_CERT"))
	}
	if viper.GetString("client-key") == "" {
		viper.Set("client-key", os.Getenv("INCUS_CLIENT_KEY"))
	}

	rawGroups := strings.Split(viper.GetString("groups"), ",")
	allowedGroups := make([]string, 0, len(rawGroups))
	for _, group := range rawGroups {
		group = strings.TrimSpace(group)
		if group == "" {
			continue
		}
		allowedGroups = append(allowedGroups, group)
	}
	viper.Set("groups", strings.Join(allowedGroups, ","))

	authMethods, err := parseAuthMethods(viper.GetString("auth-methods"))
	if err != nil {
		log.Fatalf("auth-methods: %v", err)
	}
	if len(authMethods) > 0 && viper.GetBool("noauth") {
		log.Warn("auth: authentication cannot be disabled with auth-methods")
		viper.Set("noauth", false)
	} else if viper.GetBool("noauth") && !viper.GetBool("instance-auth") {
		log.Warn("ssh: authentication disabled")
	}

	// Enabled password auth if it's part of auth methods chain
	if slices.Contains(authMethods, ssh.PasswordAuthMethod) {
		viper.Set("password-auth", true)
	}

	if viper.GetBool("dump") {
		dumpConfig()
		os.Exit(0)
	}

	if viper.GetBool("dump-create") {
		dumpCreateConfig()
		os.Exit(0)
	}

	if len(authMethods) > 0 {
		log.Warnf("auth: enabled authentication method chain: %s", strings.Join(authMethods, ","))
	}

	log.Debugf("log: DEBUG enabled")

	config := &server.Config{
		App:           app,
		Args:          args,
		Master:        viper.GetBool("master"),
		Debug:         viper.GetBool("debug"),
		Banner:        viper.GetBool("banner"),
		Listen:        viper.GetString("listen"),
		Socket:        viper.GetString("socket"),
		NoAuth:        viper.GetBool("noauth") && !viper.GetBool("instance-auth"), // instance-auth overrides noauth
		InstanceAuth:  viper.GetBool("instance-auth"),
		PassAuth:      viper.GetBool("password-auth"),
		AllowCreate:   viper.GetBool("allow-create"),
		ChrootSFTP:    viper.GetBool("chroot-sftp"),
		Welcome:       viper.GetBool("welcome"),
		Shell:         viper.GetString("shell"),
		Groups:        viper.GetString("groups"),
		TermMux:       viper.GetString("term-mux"),
		HealthCheck:   viper.GetString("health-check"),
		AuthMethods:   authMethods,
		AllowedGroups: allowedGroups,
		ClientCert:    viper.GetString("client-cert"),
		ClientKey:     viper.GetString("client-key"),
		ServerCert:    viper.GetString("server-cert"),
		URL:           viper.GetString("url"),
		Remote:        viper.GetString("remote"),
		IdleTimeout:   viper.GetDuration("idle-timeout"),
		Pprof:         viper.GetBool("pprof"),
		PprofListen:   viper.GetString("pprof-listen"),
		ConfigFile:    viper.ConfigFileUsed(),
		Web:           viper.GetBool("web"),
		WebListen:     viper.GetString("web-listen"),
		WebAuth:       viper.GetString("web-auth"),
	}

	server.WithConfig(config).Run()
}

func dumpConfig() {
	options := server.ConfigOptions()
	optionNames := make(map[string]bool)
	for _, o := range options {
		if o.Deprecated {
			continue
		}
		optionNames[o.Name] = true
	}

	allSettings := viper.AllSettings()
	filteredSettings := make(map[string]interface{})

	for key, value := range allSettings {
		if optionNames[key] {
			filteredSettings[key] = value
		}
	}

	c, err := yaml.Marshal(filteredSettings)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("# Parsed config file:", viper.ConfigFileUsed())
	fmt.Println("---")
	fmt.Print(string(c))
}

func dumpCreateConfig() {
	home, _ := os.UserHomeDir()
	config, err := server.LoadCreateConfigWithFallback(
		[]string{
			"",
			path.Join(home, ".config", app.Name()),
			path.Join("/etc", app.Name()),
		})
	if err != nil {
		log.Fatal(err)
	}

	c, err := yaml.Marshal(config)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("# Parsed config file:", config.ConfigFile())
	fmt.Println("---")
	fmt.Print(string(c))
}

func setupLogger() {
	log.SetOutput(os.Stdout)
	isTerminal := app.IsTerminal()
	logFormatter := &log.TextFormatter{
		DisableQuote:              !isTerminal,
		DisableTimestamp:          !isTerminal,
		EnvironmentOverrideColors: true,
	}
	if viper.GetBool("debug") {
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

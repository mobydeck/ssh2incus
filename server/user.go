package server

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"ssh2incus/pkg/cache"
	"ssh2incus/pkg/incus"
	"ssh2incus/pkg/ssh"
	"ssh2incus/pkg/user"
	"ssh2incus/pkg/util/shadow"

	"github.com/muhlemmer/gu"
	log "github.com/sirupsen/logrus"
)

const (
	PersistentSessionFlag = "%"
	EphemeralInstanceFlag = "~"
	CreateInstanceFlag    = "+"
	CommandShellFlag      = "/"
)

var (
	ContextKeyLoginUser = &contextKey{"loginUser"}

	loginUserCache       *cache.Cache
	loginUserFailedCache *cache.Cache
)

func init() {
	loginUserCache = cache.New(15*time.Minute, 20*time.Minute)
	loginUserFailedCache = cache.New(1*time.Minute, 2*time.Minute)
}

type LoginUser struct {
	OrigUser       string
	Remote         string
	User           string
	Instance       string
	Project        string
	InstanceUser   string
	Command        string
	Persistent     bool
	CreateInstance bool
	CreateConfig   LoginCreateConfig
	PublicKey      ssh.PublicKey
	ExplainUser    *LoginUser
	RemoveUser     *LoginUser
	ForceRemove    bool

	ctx ssh.Context
}

type LoginCreateConfig struct {
	Image      *string
	Memory     *int
	CPU        *int
	Disk       *int
	Ephemeral  *bool
	Nesting    *bool
	Privileged *bool
	VM         *bool
	Profiles   []string
}

func LoginUserFromContext(ctx ssh.Context) *LoginUser {
	if lu, ok := ctx.Value(ContextKeyLoginUser).(*LoginUser); ok {
		return lu
	}
	lu := &LoginUser{}
	lu.ParseFrom(ctx.User())
	lu.ctx = ctx
	if lu.Remote == "" {
		lu.Remote = config.Remote
	}
	ctx.SetValue(ContextKeyLoginUser, lu)
	return lu
}

func (lu *LoginUser) String() string {
	if lu == nil {
		return ""
	}

	remote := ""
	if lu.Remote != "" {
		remote = lu.Remote + ":"
	}

	if lu.CreateInstance {
		create := CreateInstanceFlag
		if lu.CreateConfig.Ephemeral != nil && *lu.CreateConfig.Ephemeral {
			create = EphemeralInstanceFlag
		}
		cc := []string{""}
		if lu.CreateConfig.Image != nil {
			cc = append(cc, *lu.CreateConfig.Image)
		}
		if len(lu.CreateConfig.Profiles) > 0 {
			for _, p := range lu.CreateConfig.Profiles {
				cc = append(cc, fmt.Sprintf("%%%s", p))
			}
		}
		if lu.CreateConfig.Memory != nil {
			cc = append(cc, fmt.Sprintf("m%d", *lu.CreateConfig.Memory))
		}
		if lu.CreateConfig.CPU != nil {
			cc = append(cc, fmt.Sprintf("c%d", *lu.CreateConfig.CPU))
		}
		if lu.CreateConfig.Disk != nil {
			cc = append(cc, fmt.Sprintf("d%d", *lu.CreateConfig.Disk))
		}
		conf := []string{""}
		if lu.CreateConfig.Nesting != nil && *lu.CreateConfig.Nesting {
			conf = append(conf, "nest")
		}
		if lu.CreateConfig.Privileged != nil && *lu.CreateConfig.Privileged {
			conf = append(conf, "priv")
		}
		if lu.CreateConfig.VM != nil && *lu.CreateConfig.VM {
			conf = append(conf, "vm")
		}
		options := ""
		if len(cc) > 1 {
			options += strings.Join(cc, "+")
		}
		if len(conf) > 1 {
			options += strings.Join(conf, "+")
		}

		i := fmt.Sprintf("%s%s%s.%s", create, remote, lu.Instance, lu.Project)

		return i + options
	}

	persistent := ""
	if lu.Persistent {
		persistent = PersistentSessionFlag
	}
	if lu.Command != "" {
		switch lu.Command {
		case "explain":
			if lu.ExplainUser != nil {
				return fmt.Sprintf("%sexplain/%s", CommandShellFlag, lu.ExplainUser.String())
			}
		case "remove":
			if lu.RemoveUser != nil {
				cmd := "remove"
				if lu.ForceRemove {
					cmd = "remove-force"
				}
				return fmt.Sprintf("%s%s/%s", CommandShellFlag, cmd, lu.RemoveUser.String())
			}
		}
		return fmt.Sprintf("%s%s@%s", CommandShellFlag, lu.Command, lu.User)
	}
	return fmt.Sprintf("%s%s%s@%s.%s~%s", remote, persistent, lu.InstanceUser, lu.Instance, lu.Project, lu.User)
}

func (lu *LoginUser) FullInstance() string {
	if lu == nil {
		return ""
	}
	return fmt.Sprintf("%s.%s", lu.Instance, lu.Project)
}

func (lu *LoginUser) IsDefaultProject() bool {
	return incus.IsDefaultProject(lu.Project)
}

func (lu *LoginUser) IsValid() bool {
	log := log.WithField("session", lu.ctx.ShortSessionID())

	if lu == nil {
		return false
	}

	if lu.IsCommand() {
		switch lu.Command {
		case "shell":
			return true
		case "explain":
			return true
		case "remove":
			return true
		default:
			return false
		}
	}

	if _, ok := loginUserFailedCache.Get(lu.Hash()); ok {
		return false
	}
	if _, ok := loginUserCache.Get(lu.Hash()); ok {
		return true
	}

	client, err := NewDefaultIncusClientWithContext(lu.ctx)
	if err != nil {
		log.Errorf("failed to initialize incus client for %s: %v", lu, err)
		return false
	}

	if lu.CreateInstance {
		in, err := client.GetCachedInstance(lu.Project, lu.Instance)
		if err == nil || in != nil {
			log.Errorf("instance %s.%s already exists", lu.Instance, lu.Project)
		}
		return true
	}

	iu, err := client.GetCachedInstanceUser(lu.Project, lu.Instance, lu.InstanceUser)
	if err != nil || iu == nil {
		log.Errorf("instance user %s for %s error: %s", lu.InstanceUser, lu, err)
		loginUserFailedCache.SetDefault(lu.Hash(), time.Now())
		return false
	}

	loginUserFailedCache.Delete(lu.Hash())
	loginUserCache.SetDefault(lu.Hash(), time.Now())
	return true
}

func (lu *LoginUser) IsCommand() bool {
	return lu.Command != ""
}

func (lu *LoginUser) Hash() string {
	if lu == nil {
		return ""
	}
	return fmt.Sprintf("%s/%s/%s/%s/%s", lu.Remote, lu.User, lu.Project, lu.Instance, lu.InstanceUser)
}

func (lu *LoginUser) InstanceHash() string {
	if lu == nil {
		return ""
	}
	return fmt.Sprintf("%s/%s/%s", lu.Remote, lu.Project, lu.Instance)
}

func getHostUser(username string) (*user.User, error) {
	u, err := user.Lookup(username)
	if err != nil {
		return nil, err
	}
	return u, nil
}

func checkHostUser(lu *LoginUser) (*user.User, error) {
	hostUser, err := getHostUser(lu.User)
	if err != nil {
		return nil, err
	}

	if hostUser.Uid != "0" && len(config.AllowedGroups) > 0 {
		userGroups, err := getUserGroups(hostUser)
		if err != nil {
			return nil, err
		}
		if gid, match := groupMatch(config.AllowedGroups, userGroups); !match {
			return nil, fmt.Errorf("no group match for %s %v in %v", lu.User, userGroups, config.AllowedGroups)
		} else {
			group, err := user.LookupGroupId(gid)
			if err != nil {
				return nil, err
			}
			log.Debugf("auth(host): user %q matched %q group", lu.User, group.Name)
		}
	}
	return hostUser, nil
}

func checkHostShadowPassword(user, password string) error {
	s := shadow.New()
	err := s.Read()
	if err != nil {
		return err
	}

	e, err := s.Lookup(user)
	if err != nil {
		return err
	}

	err = e.VerifyPassword(password)
	return err
}

func checkShadowPassword(user, password string, shadowContent string) error {
	s, err := shadow.NewFromString(shadowContent)
	if err != nil {
		return err
	}

	e, err := s.Lookup(user)
	if err != nil {
		return err
	}

	err = e.VerifyPassword(password)
	return err
}

func getUserAuthKeys(u *user.User) ([][]byte, error) {
	var keys [][]byte

	// Try multiple locations for authorized_keys
	authKeysPaths := []string{
		filepath.Clean(u.HomeDir + "/.ssh/authorized_keys"),
		filepath.Clean("/etc/ssh/authorized_keys.d/" + u.Username), // NixOS default
	}

	var lastErr error
	for _, path := range authKeysPaths {
		f, err := os.Open(path)
		if err != nil {
			lastErr = err
			continue
		}
		defer f.Close()

		s := bufio.NewScanner(f)
		for s.Scan() {
			keys = append(keys, s.Bytes())
		}

		// Successfully read at least one file
		if len(keys) > 0 {
			return keys, nil
		}
	}

	// If we found no keys in any location, return the last error
	if lastErr != nil {
		return nil, lastErr
	}

	return keys, nil
}

func getUserGroups(u *user.User) ([]string, error) {
	groups, err := u.GroupIds()
	if err != nil {
		return nil, err
	}
	return groups, nil
}

// ParseFrom parses a user string into the LoginUser struct
func (lu *LoginUser) ParseFrom(user string) {
	lu.setDefaults()
	lu.OrigUser = user

	if lu.isCreationFormat(user) {
		lu.parseCreationFormat(user)
		return
	}

	lu.parseRegularFormat(user)
}

// setDefaults initializes the LoginUser with default values
func (lu *LoginUser) setDefaults() {
	lu.InstanceUser = "root"
	lu.Project = "default"

	// Auto-detect the default user based on the process owner
	// This allows ssh2incus to work when not running as root
	if uid := os.Getuid(); uid == 0 {
		lu.User = "root"
	} else {
		if u, err := user.LookupId(strconv.Itoa(uid)); err == nil {
			lu.User = u.Username
		} else {
			// Fallback to root if lookup fails
			lu.User = "root"
		}
	}
}

// isCreationFormat checks if the user string is in creation format (starts with + or ~)
func (lu *LoginUser) isCreationFormat(user string) bool {
	return user != "" && (string(user[0]) == CreateInstanceFlag || string(user[0]) == EphemeralInstanceFlag)
}

// parseCreationFormat parses creation format: [+|~][remote:]instance-name[.project-name][+image][+memory][+cpu][+disk][+options][~host-user]
func (lu *LoginUser) parseCreationFormat(user string) {
	conf := user[1:]
	lu.CreateInstance = true
	lu.CreateConfig = LoginCreateConfig{}

	if string(user[0]) == EphemeralInstanceFlag {
		lu.CreateConfig.Ephemeral = gu.Ptr(true)
	}

	// Parse host user
	if c, u, ok := strings.Cut(conf, "~"); ok {
		conf = c
		lu.User = u
	}

	instance, configStr, _ := strings.Cut(conf, "+")
	lu.parseRemoteAndInstance(instance)
	lu.parseCreateConfig(configStr)
}

// parseRegularFormat parses regular format: [/%][remote:][instance-user@]instance-name[.project-name][[~|+]host-user]
func (lu *LoginUser) parseRegularFormat(user string) {
	// Handle commands
	if cmd, ok := strings.CutPrefix(user, CommandShellFlag); ok {
		// Split command and target: /command/target
		parts := strings.SplitN(cmd, "/", 2)
		if len(parts) < 2 {
			// No slash after command, just the command itself
			lu.Command = parts[0]
		} else {
			cmdName := parts[0]
			target := parts[1]

			switch cmdName {
			case "explain":
				lu.Command = "explain"
				explainLu := &LoginUser{}
				explainLu.ParseFrom(target)
				lu.ExplainUser = explainLu
			case "rm", "rm-f", "remove", "remove-force":
				lu.Command = "remove"
				lu.ForceRemove = cmdName == "rm-f" || cmdName == "remove-force"
				removeLu := &LoginUser{}
				removeLu.ParseFrom(target)
				lu.RemoveUser = removeLu
			default:
				lu.Command = cmdName
			}
		}
		lu.Instance = ""
		lu.Project = ""
		lu.InstanceUser = ""
		return
	}

	// Handle persistent session flag
	if u, ok := strings.CutPrefix(user, PersistentSessionFlag); ok {
		user = u
		lu.Persistent = true
	}

	// Parse instance and host user
	instance := user
	if i, u, ok := strings.Cut(user, "~"); ok {
		instance = i
		lu.User = u
	} else if i, u, ok := strings.Cut(user, "+"); ok {
		instance = i
		lu.User = u
	}

	// Parse remote and instance
	lu.parseRemoteAndInstance(instance)
}

// parseRemoteAndInstance parses remote:instance.project format
func (lu *LoginUser) parseRemoteAndInstance(instance string) {
	if r, i, ok := strings.Cut(instance, ":"); ok {
		lu.Remote = r
		instance = i
	}
	lu.parseInstanceAndUser(instance)
}

// parseInstanceAndUser parses user@instance format
func (lu *LoginUser) parseInstanceAndUser(instance string) {
	// URL-decode @
	instance = strings.ReplaceAll(instance, "%40", "@")
	instance = strings.ReplaceAll(instance, "%2540", "@")

	// Parse instance user and instance name
	if u, i, ok := strings.Cut(instance, "@"); ok {
		instance = i
		lu.InstanceUser = u
	}
	lu.parseInstanceAndProject(instance)
}

// parseInstanceAndProject parses instance.project format
func (lu *LoginUser) parseInstanceAndProject(instance string) {
	if i, p, ok := strings.Cut(instance, "."); ok {
		lu.Instance = i
		lu.Project = p
	} else {
		lu.Instance = instance
	}

	if lu.Project == "" {
		lu.Project = "default"
	}
}

// parseCreateConfig parses the creation configuration options
func (lu *LoginUser) parseCreateConfig(configStr string) {
	if configStr == "" {
		return
	}

	configs := strings.Split(configStr, "+")
	for _, c := range configs {
		switch {
		case len(c) == 0:
			continue
		case strings.HasPrefix(c, "%"):
			c = strings.TrimPrefix(c, "%")
			pp := strings.Split(c, ",")
			lu.CreateConfig.Profiles = append(lu.CreateConfig.Profiles, pp...)
		case strings.Contains(c, "/"):
			lu.CreateConfig.Image = &c
		case c[0] == 'm':
			if n, err := strconv.Atoi(c[1:]); err == nil {
				lu.CreateConfig.Memory = &n
			}
		case c[0] == 'c':
			if n, err := strconv.Atoi(c[1:]); err == nil {
				lu.CreateConfig.CPU = &n
			}
		case c[0] == 'd':
			if n, err := strconv.Atoi(c[1:]); err == nil {
				lu.CreateConfig.Disk = &n
			}
		case c == "n" || strings.HasPrefix(c, "nest"):
			lu.CreateConfig.Nesting = gu.Ptr(true)
		case c == "p" || strings.HasPrefix(c, "priv"):
			lu.CreateConfig.Privileged = gu.Ptr(true)
		case c == "e" || strings.HasPrefix(c, "ephe"):
			lu.CreateConfig.Ephemeral = gu.Ptr(true)
		case c == "v" || c == "vm":
			lu.CreateConfig.VM = gu.Ptr(true)
		}
	}
}

// parseLoginUser is kept for backward compatibility but now uses the new ParseFrom method
func parseLoginUser(user string) *LoginUser {
	lu := &LoginUser{}
	lu.ParseFrom(user)
	return lu
}

func getGroupIds(groups []string) []string {
	var ids []string
	for _, g := range groups {
		group, err := user.LookupGroup(g)
		if err != nil {
			log.Errorf("group lookup: %v", err)
			continue
		}
		ids = append(ids, group.Gid)
	}
	return ids
}

func groupMatch(a []string, b []string) (string, bool) {
	for _, i := range a {
		for _, j := range b {
			if i == j {
				return i, true
			}
		}
	}
	return "", false
}

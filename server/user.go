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
	PersistentSessionFlag = "/"
	EphemeralInstanceFlag = "~"
	CreateInstanceFlag    = "+"
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

	ctx ssh.Context
}

type LoginCreateConfig struct {
	Image      *string
	Memory     *int
	Cpu        *int
	Disk       *int
	Ephemeral  *bool
	Nesting    *bool
	Privileged *bool
	Vm         *bool
}

func LoginUserFromContext(ctx ssh.Context) *LoginUser {
	if lu, ok := ctx.Value(ContextKeyLoginUser).(*LoginUser); ok {
		return lu
	}
	lu := parseLoginUser(ctx.User())
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
		if lu.CreateConfig.Memory != nil {
			cc = append(cc, fmt.Sprintf("m%d", *lu.CreateConfig.Memory))
		}
		if lu.CreateConfig.Cpu != nil {
			cc = append(cc, fmt.Sprintf("c%d", *lu.CreateConfig.Cpu))
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
		if lu.CreateConfig.Vm != nil && *lu.CreateConfig.Vm {
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
		return fmt.Sprintf("%%%s@%s", lu.Command, lu.User)
	}
	return fmt.Sprintf("%s%s%s@%s.%s+%s", remote, persistent, lu.InstanceUser, lu.Instance, lu.Project, lu.User)
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

	f, err := os.Open(filepath.Clean(u.HomeDir + "/.ssh/authorized_keys"))
	if err != nil {
		return nil, err
	}
	defer f.Close()

	s := bufio.NewScanner(f)
	for s.Scan() {
		keys = append(keys, s.Bytes())
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

func parseLoginUser(user string) *LoginUser {
	lu := new(LoginUser)
	lu.OrigUser = user
	lu.InstanceUser = "root"
	lu.Project = "default"

	if user != "" && (user[0] == CreateInstanceFlag[0] || user[0] == EphemeralInstanceFlag[0]) {
		conf := user[1:]
		lu.User = "root"
		lu.CreateInstance = true
		lu.CreateConfig = LoginCreateConfig{}
		if user[0] == '~' {
			lu.CreateConfig.Ephemeral = gu.Ptr(true)
		}

		instance, conf, _ := strings.Cut(conf, "+")
		if r, i, ok := strings.Cut(instance, ":"); ok {
			lu.Remote = r
			instance = i
		}
		if i, p, ok := strings.Cut(instance, "."); ok {
			lu.Instance = i
			lu.Project = p
		} else {
			lu.Instance = instance
		}

		configs := strings.Split(conf, "+")
		for _, c := range configs {
			switch {
			case len(c) == 0:
				continue
			case strings.Contains(c, "/"):
				lu.CreateConfig.Image = &c
			case c[0] == 'm':
				if n, err := strconv.Atoi(c[1:]); err == nil {
					lu.CreateConfig.Memory = &n
				}
			case c[0] == 'c':
				if n, err := strconv.Atoi(c[1:]); err == nil {
					lu.CreateConfig.Cpu = &n
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
				lu.CreateConfig.Vm = gu.Ptr(true)
			}
		}
		return lu
	}

	if u, ok := strings.CutPrefix(user, PersistentSessionFlag); ok {
		user = u
		lu.Persistent = true
	}

	if r, u, ok := strings.Cut(user, ":"); ok {
		lu.Remote = r
		user = u
	}

	instance := user
	if i, u, ok := strings.Cut(user, "+"); ok {
		instance = i
		lu.User = u
	} else {
		lu.User = "root"
	}

	if u, i, ok := strings.Cut(instance, "@"); ok {
		instance = i
		lu.InstanceUser = u
	}

	if i, p, ok := strings.Cut(instance, "."); ok {
		lu.Instance = i
		lu.Project = p
	} else {
		lu.Instance = instance
	}

	if lu.Project == "" {
		lu.Project = "default"
	}

	if strings.HasPrefix(lu.Instance, "%") {
		lu.Command = strings.TrimPrefix(lu.Instance, "%")
		lu.Instance = ""
		lu.Project = ""
		lu.InstanceUser = ""
	}

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

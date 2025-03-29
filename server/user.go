package server

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"ssh2incus/pkg/cache"
	"ssh2incus/pkg/incus"
	"ssh2incus/pkg/ssh"
	"ssh2incus/pkg/user"

	log "github.com/sirupsen/logrus"
)

var (
	// ContextKeyLoginUser is a context key for use with Contexts in this package.
	ContextKeyLoginUser = &contextKey{"loginUser"}
)

var (
	loginUserCache *cache.Cache
)

func init() {
	loginUserCache = cache.New(15*time.Minute, 20*time.Minute)
}

type LoginUser struct {
	OrigUser     string
	User         string
	Instance     string
	Project      string
	InstanceUser string
	PublicKey    ssh.PublicKey
}

func (lu *LoginUser) String() string {
	if lu == nil {
		return ""
	}
	return fmt.Sprintf("%s@%s.%s+%s", lu.InstanceUser, lu.Instance, lu.Project, lu.User)
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
	if lu == nil {
		return false
	}

	if lu.Instance == "%shell" {
		return true
	}

	if _, ok := loginUserCache.Get(lu.Hash()); ok {
		return true
	}

	client, err := NewIncusClientWithContext(context.Background(), DefaultParams)
	if err != nil {
		log.Error(err)
		return false
	}
	defer client.Disconnect()

	_, _, err = client.GetInstance(lu.Project, lu.Instance)
	if err != nil {
		log.Errorf("getting instance %s error: %s", lu.Instance, err)
		return false
	}
	loginUserCache.SetDefault(lu.Hash(), time.Now())
	return true
}

func (lu *LoginUser) Hash() string {
	if lu == nil {
		return ""
	}
	return fmt.Sprintf("%s/%s/%s/%s", lu.User, lu.Project, lu.Instance, lu.InstanceUser)
}

func (lu *LoginUser) InstanceHash() string {
	if lu == nil {
		return ""
	}
	return fmt.Sprintf("%s/%s", lu.Project, lu.Instance)
}

func getOsUser(username string) (*user.User, error) {
	u, err := user.Lookup(username)
	if err != nil {
		log.Errorf("user lookup: %v", err)
		return nil, err
	}
	return u, nil
}

func getUserAuthKeys(u *user.User) ([][]byte, error) {
	var keys [][]byte

	f, err := os.Open(filepath.Clean(u.HomeDir + "/.ssh/authorized_keys"))
	if err != nil {
		log.Errorf("error with authorized_keys: %v", err)
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
		log.Errorf("user groups: %v", err)
		return nil, err
	}
	return groups, nil
}

func parseLoginUser(user string) *LoginUser {
	lu := new(LoginUser)
	lu.OrigUser = user
	lu.InstanceUser = "root"
	lu.Project = "default"

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

func isGroupMatch(a []string, b []string) bool {
	for _, i := range a {
		for _, j := range b {
			if i == j {
				return true
			}
		}
	}
	return false
}

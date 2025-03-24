package server

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"ssh2incus/pkg/incus"
	"ssh2incus/pkg/util/user"

	log "github.com/sirupsen/logrus"
)

type LoginUser struct {
	User         string
	Instance     string
	Project      string
	InstanceUser string
}

var (
	loginUserCache    = make(map[string]time.Time)
	loginUserCacheTtl = time.Minute * 15
)

func (lu LoginUser) IsDefaultProject() bool {
	return incus.IsDefaultProject(lu.Project)
}

func (lu LoginUser) IsValid() bool {
	if t, ok := loginUserCache[lu.Hash()]; ok {
		if time.Now().Sub(t) < loginUserCacheTtl {
			return true
		}
		delete(loginUserCache, lu.Hash())
	}

	server, err := NewIncusServer()
	if err != nil {
		log.Errorf("failed to initialize incus client: %w", err)
		return false
	}

	err = server.Connect(context.Background())
	if err != nil {
		log.Errorf("failed to connect to incus: %w", err)
		return false
	}
	defer server.Disconnect()

	if lu.Instance == "%shell" {
		return true
	}

	if !lu.IsDefaultProject() {
		err = server.UseProject(lu.Project)
		if err != nil {
			log.Errorf("using project %s error: %s", lu.Project, err)
			return false
		}
	}

	_, _, err = server.GetInstance(lu.Instance)
	if err != nil {
		log.Errorf("getting instance %s error: %s", lu.Instance, err)
		return false
	}
	loginUserCache[lu.Hash()] = time.Now()
	return true
}

func (lu LoginUser) Hash() string {
	return fmt.Sprintf("%s/%s/%s/%s", lu.User, lu.Project, lu.Instance, lu.InstanceUser)
}

func (lu LoginUser) InstanceHash() string {
	return fmt.Sprintf("%s/%s", lu.Project, lu.Instance)
}

func getOsUser(username string) (*user.User, error) {
	u, err := user.Lookup(username)
	if err != nil {
		log.Errorf("user lookup: %w", err)
		return nil, err
	}
	return u, nil
}

func getUserAuthKeys(u *user.User) ([][]byte, error) {
	var keys [][]byte

	f, err := os.Open(filepath.Clean(u.HomeDir + "/.ssh/authorized_keys"))
	if err != nil {
		log.Errorf("error with authorized_keys", err)
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
		log.Errorf("user groups: %w", err)
		return nil, err
	}
	return groups, nil
}

func parseUser(user string) LoginUser {
	lu := LoginUser{}
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
			log.Errorf("group lookup: %w", err)
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

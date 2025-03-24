package incus

import (
	"fmt"
	"ssh2incus/pkg/util/cache"
	"strconv"
	"strings"
	"time"

	"ssh2incus/pkg/util/buffer"

	log "github.com/sirupsen/logrus"
)

var (
	instanceUserCache *cache.Cache
)

func init() {
	instanceUserCache = cache.New(20*time.Minute, 30*time.Minute)
}

type InstanceUser struct {
	Instance string
	User     string
	Uid      int
	Gid      int
	Dir      string
	Shell    string
	Ent      string
}

func (s *Server) GetInstanceUser(project, instance, user string) *InstanceUser {
	cacheKey := instanceUserKey(project, instance, user)
	if iu, ok := instanceUserCache.Get(cacheKey); ok {
		return iu.(*InstanceUser)
	}

	cmd := fmt.Sprintf("getent passwd %s", user)
	stdout := buffer.NewOutputBuffer()
	stderr := buffer.NewOutputBuffer()

	ie := s.NewInstanceExec(InstanceExec{
		Instance: instance,
		Cmd:      cmd,
		Stdout:   stdout,
		Stderr:   stderr,
	})

	ret, _ := ie.Exec()

	if ret == 0 && len(stdout.Lines()) > 0 {
		ent := strings.Split(stdout.Lines()[0], ":")
		user = ent[0]
		uid, _ := strconv.Atoi(ent[2])
		gid, _ := strconv.Atoi(ent[3])
		dir := ent[5]
		shell := ent[6]
		iu := &InstanceUser{
			Instance: instance,
			User:     user,
			Uid:      uid,
			Gid:      gid,
			Dir:      dir,
			Shell:    shell,
			Ent:      stdout.Lines()[0],
		}

		instanceUserCache.SetDefault(cacheKey, iu)

		return iu
	}

	log.Debugf("couldn't find user %s or instance %s", user, instance)

	return nil
}

func instanceUserKey(project, instance, user string) string {
	return fmt.Sprintf("%s/%s/%s", project, instance, user)
}

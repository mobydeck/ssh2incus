package incus

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"ssh2incus/pkg/cache"
	"ssh2incus/pkg/queue"
	"ssh2incus/pkg/util/buffer"
)

var (
	instanceUserCache *cache.Cache
	instanceUserQueue *queue.Queueable[*InstanceUser]
	instanceUserOnce  sync.Once
)

func init() {
	instanceUserOnce.Do(func() {
		instanceUserCache = cache.New(1*time.Minute, 2*time.Minute)
		instanceUserQueue = queue.New[*InstanceUser](100)
	})
}

type InstanceUser struct {
	Project  string
	Instance string
	User     string
	Uid      int
	Gid      int
	Dir      string
	Shell    string
	Ent      string
}

func (i *InstanceUser) Welcome() string {
	return fmt.Sprintf("Welcome %q to incus shell on %s", i.User, i.FullInstance())
}

func (i *InstanceUser) FullInstance() string {
	return fmt.Sprintf("%s.%s", i.Instance, i.Project)
}

func (c *Client) GetInstanceUser(project, instance, user string) (*InstanceUser, error) {
	iu, err := queue.EnqueueWithParam(instanceUserQueue, func(i string) (*InstanceUser, error) {
		stdout := buffer.NewOutputBuffer()
		stderr := buffer.NewOutputBuffer()

		err := c.UseProject(project)
		if err != nil {
			return nil, err
		}

		cmd := fmt.Sprintf("getent passwd %s", user)

		ie := c.NewInstanceExec(InstanceExec{
			Instance: instance,
			Cmd:      cmd,
			Stdout:   stdout,
			Stderr:   stderr,
		})

		ret, err := ie.Exec()
		if err != nil {
			return nil, err
		}
		if ret != 0 {
			return nil, errors.New("user not found")
		}

		out := stdout.Lines()

		if len(out) < 1 {
			return nil, errors.New("user not found")
		}
		ent := strings.Split(out[0], ":")
		user = ent[0]
		uid, _ := strconv.Atoi(ent[2])
		gid, _ := strconv.Atoi(ent[3])
		dir := ent[5]
		shell := ent[6]
		iu := &InstanceUser{
			Instance: instance,
			Project:  project,
			User:     user,
			Uid:      uid,
			Gid:      gid,
			Dir:      dir,
			Shell:    shell,
			Ent:      out[0],
		}
		return iu, nil
	}, instance)

	return iu, err
}

func (c *Client) GetCachedInstanceUser(project, instance, user string) (*InstanceUser, error) {
	cacheKey := instanceUserKey(project, instance, user)
	if iu, ok := instanceUserCache.Get(cacheKey); ok {
		return iu.(*InstanceUser), nil
	}

	iu, err := c.GetInstanceUser(project, instance, user)

	if err == nil {
		instanceUserCache.SetDefault(cacheKey, iu)
	}

	return iu, err
}

func (c *Client) UncacheInstanceUser(project, instance, user string) {
	cacheKey := instanceUserKey(project, instance, user)
	instanceUserCache.Delete(cacheKey)
}

func instanceUserKey(project, instance, user string) string {
	return fmt.Sprintf("%s/%s/%s", project, instance, user)
}

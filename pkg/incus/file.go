package incus

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"ssh2incus/pkg/util/buffer"
	"ssh2incus/pkg/util/cache"
	uio "ssh2incus/pkg/util/io"

	incus "github.com/lxc/incus/v6/client"
	log "github.com/sirupsen/logrus"
)

var (
	fileExistsCache *cache.Cache
)

func init() {
	fileExistsCache = cache.New(20*time.Minute, 30*time.Minute)
}

func (s *Server) UploadFile(project, instance string, src string, dest string) error {
	info, err := os.Stat(src)
	if err != nil {
		log.Errorf("couldn't stat file %s", src)
		return err
	}

	mode, uid, gid := uio.GetOwnerMode(info)

	f, err := os.OpenFile(src, os.O_RDONLY, 0)
	if err != nil {
		log.Errorf("couldn't open file %s for reading", src)
		return err
	}
	defer f.Close()

	err = s.UploadBytes(project, instance, dest, f, int64(uid), int64(gid), int(mode.Perm()))

	return err
}

func (s *Server) UploadBytes(project, instance, dest string, b io.ReadSeeker, uid, gid int64, mode int) error {
	args := incus.InstanceFileArgs{
		Content:   b,
		UID:       uid,
		GID:       gid,
		Mode:      mode,
		Type:      "file",
		WriteMode: "overwrite",
	}

	err := s.srv.CreateInstanceFile(instance, dest, args)

	return err
}

func (s *Server) FileExists(project, instance, path, md5sum string, cache bool) bool {
	var fileHash string
	if cache {
		fileHash = FileHash(project, instance, path, md5sum)
		if _, ok := fileExistsCache.Get(fileHash); ok {
			return true
		}
	}

	stdout := buffer.NewOutputBuffer()
	stderr := buffer.NewOutputBuffer()
	cmd := fmt.Sprintf("test -f %s", path)
	ie := s.NewInstanceExec(InstanceExec{
		Instance: instance,
		Cmd:      cmd,
		Stdout:   stdout,
		Stderr:   stderr,
	})
	ret, _ := ie.Exec()

	if ret != 0 {
		return false
	}

	if md5sum != "" {
		ie.Cmd = fmt.Sprintf("md5sum %s", path)
		ret, _ := ie.Exec()
		if ret != 0 {
			log.Error(stderr.Lines()[0])
			return false
		}
		m := strings.Split(stdout.Lines()[0], " ")
		log.Debugf("comparing md5 for %s: %s %s", path, md5sum, m[0])
		if md5sum == m[0] {
			return true
		} else {
			return false
		}
	}
	if cache {
		fileExistsCache.SetDefault(fileHash, true)
	}
	return true
}

func FileHash(project, instance, path, md5sum string) string {
	return fmt.Sprintf("%s/%s/%s:%s", project, instance, path, md5sum)
}

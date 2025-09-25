package incus

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"ssh2incus/pkg/cache"
	"ssh2incus/pkg/queue"
	"ssh2incus/pkg/util/buffer"
	uio "ssh2incus/pkg/util/io"

	incus "github.com/lxc/incus/v6/client"
)

var (
	fileExistsCache *cache.Cache
	fileExistsQueue *queue.Queueable[bool]
	fileExistsOnce  sync.Once
)

func init() {
	fileExistsOnce.Do(func() {
		fileExistsCache = cache.New(20*time.Minute, 30*time.Minute)
		fileExistsQueue = queue.New[bool](10000)
	})
}

func (c *Client) UploadFile(project, instance string, src string, dest string) error {
	info, err := os.Stat(src)
	if err != nil {
		//log.Debugf("couldn't stat file %s", src)
		return err
	}

	mode, uid, gid := uio.GetOwnerMode(info)

	f, err := os.OpenFile(src, os.O_RDONLY, 0)
	if err != nil {
		//log.Debugf("couldn't open file %s for reading", src)
		return err
	}
	defer f.Close()

	err = c.UploadBytes(project, instance, dest, f, int64(uid), int64(gid), int(mode.Perm()))

	return err
}

func (c *Client) UploadBytes(project, instance, dest string, b io.ReadSeeker, uid, gid int64, mode int) error {
	args := incus.InstanceFileArgs{
		Content:   b,
		UID:       uid,
		GID:       gid,
		Mode:      mode,
		Type:      "file",
		WriteMode: "overwrite",
	}

	err := c.UseProject(project)
	if err != nil {
		return err
	}

	err = c.srv.CreateInstanceFile(instance, dest, args)

	return err
}

type FileExistsParams struct {
	Project     string
	Instance    string
	Path        string
	Md5sum      string
	ShouldCache bool
}

func (c *Client) FileExists(params *FileExistsParams) bool {
	return queue.EnqueueFnWithParam(fileExistsQueue, func(p *FileExistsParams) bool {
		var fileHash string
		if p.ShouldCache {
			fileHash = FileHash(p.Project, p.Instance, p.Path, p.Md5sum)
			if exists, ok := fileExistsCache.Get(fileHash); ok {
				//log.Debugf("file cache hit for %s", fileHash)
				return exists.(bool)
			}
			//log.Debugf("file cache miss for %s", fileHash)
		}

		stdout := buffer.NewOutputBuffer()
		stderr := buffer.NewOutputBuffer()
		cmd := fmt.Sprintf("test -f %s", p.Path)
		ie := c.NewInstanceExec(InstanceExec{
			Instance: p.Instance,
			Cmd:      cmd,
			Stdout:   stdout,
			Stderr:   stderr,
		})
		ret, _ := ie.Exec()

		if ret != 0 {
			return false
		}

		exists := true

		if p.Md5sum != "" {
			ie.Cmd = fmt.Sprintf("md5sum %s", p.Path)
			ret, _ := ie.Exec()
			if ret != 0 {
				//log.Debug(stderr.Lines())
				return false
			}
			out := stdout.Lines()
			if len(out) == 0 {
				return false
			}
			m := strings.Split(out[0], " ")
			if len(m) < 2 {
				return false
			}
			//log.Debugf("comparing md5 for %s: %s <=> %s", p.Path, p.Md5sum, m[0])
			exists = p.Md5sum == m[0]
		}

		if p.ShouldCache && exists {
			fileExistsCache.SetDefault(fileHash, exists)
		}

		return exists
	}, params)
}

type CommandExistsParams struct {
	Project     string
	Instance    string
	Path        string
	ShouldCache bool
}

func (c *Client) CommandExists(params *CommandExistsParams) bool {
	return queue.EnqueueFnWithParam(fileExistsQueue, func(p *CommandExistsParams) bool {
		var fileHash string
		if p.ShouldCache {
			fileHash = FileHash(p.Project, p.Instance, p.Path, "")
			if exists, ok := fileExistsCache.Get(fileHash); ok {
				return exists.(bool)
			}
		}

		cmd := fmt.Sprintf(`sh -c "command -v %s"`, p.Path)
		ie := c.NewInstanceExec(InstanceExec{
			Instance: p.Instance,
			Cmd:      cmd,
		})
		ret, _ := ie.Exec()

		if ret != 0 {
			return false
		}

		if p.ShouldCache {
			fileExistsCache.SetDefault(fileHash, true)
		}

		return true
	}, params)
}

type InstanceFile struct {
	Project  string
	Instance string
	Name     string
	Path     string
	Size     int64
	Mode     int
	Uid      int
	Gid      int
	Type     string
	Content  *buffer.BytesBuffer
}

func (c *Client) DownloadFile(project, instance string, path string) (*InstanceFile, error) {
	content, resp, err := c.srv.GetInstanceFile(instance, path)
	if err != nil {
		return nil, err
	}

	if resp.Type != "file" {
		return nil, fmt.Errorf("not a file: %s", path)
	}

	//sftpConn, err := c.srv.GetInstanceFileSFTP(instance)
	//if err != nil {
	//	return nil, err
	//}
	//defer sftpConn.Close()
	//
	//src, err := sftpConn.Open(path)
	//if err != nil {
	//	return nil, err
	//}

	buf := buffer.NewBytesBuffer()
	defer buf.Close()

	for {
		_, err = io.CopyN(buf, content, 1024*1024)
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
	}
	content.Close()

	//contentBytes, err := io.ReadAll(content)
	//if err != nil {
	//	return nil, err
	//}

	//srcInfo, err := sftpConn.Lstat(path)
	//if err != nil {
	//	return nil, err
	//}

	//targetIsLink := false
	//if srcInfo.Mode()&os.ModeSymlink == os.ModeSymlink {
	//	targetIsLink = true
	//}

	//var linkName string
	//if targetIsLink {
	//	linkName, err = sftpConn.ReadLink(path)
	//	if err != nil {
	//		return nil, err
	//	}
	//}

	//log.Debugf("read %d bytes from %s", buf.Size(), path)
	//log.Debugf("GetInstanceFile resp %#v", resp)

	return &InstanceFile{
		Project:  project,
		Instance: instance,
		Name:     filepath.Base(path),
		Path:     path,
		Size:     buf.Size(),
		Mode:     resp.Mode,
		Uid:      int(resp.UID),
		Gid:      int(resp.GID),
		Type:     resp.Type,
		Content:  buf,
	}, nil
}

func FileHash(project, instance, path, md5sum string) string {
	return fmt.Sprintf("%s/%s/%s:%s", project, instance, path, md5sum)
}

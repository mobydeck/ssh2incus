package incus

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"

	"ssh2incus/pkg/util/buffer"
	"ssh2incus/pkg/util/shlex"

	"github.com/gorilla/websocket"
	"github.com/lxc/incus/v6/client"
	"github.com/lxc/incus/v6/shared/api"
)

func (s *Server) GetInstanceMeta(instance string) (*api.ImageMetadata, string, error) {
	meta, etag, err := s.srv.GetInstanceMetadata(instance)
	return meta, etag, err
}

func (s *Server) DeleteInstanceDevice(i *api.Instance, name string) error {
	if !strings.HasPrefix(name, ProxyDevicePrefix) {
		return nil
	}

	// Need new ETag for each operation
	i, etag, err := s.GetInstance(i.Name)
	if err != nil {
		return fmt.Errorf("failed to get instance %s.%s: %v", i.Name, i.Project, err)
	}

	device, ok := i.Devices[name]
	if !ok {
		return fmt.Errorf("device %s does not exist for %s.%s", device, i.Name, i.Project)
	}
	delete(i.Devices, name)

	op, err := s.UpdateInstance(i.Name, i.Writable(), etag)
	if err != nil {
		return err
	}

	err = op.Wait()
	if err != nil {
		return err
	}

	// Cleanup socket files
	if strings.HasPrefix(device["connect"], "unix:") {
		source := strings.TrimPrefix(device["connect"], "unix:")
		os.RemoveAll(path.Dir(source))
	}

	if strings.HasPrefix(device["listen"], "unix:") {
		target := strings.TrimPrefix(device["listen"], "unix:")
		cmd := fmt.Sprintf("rm -f %s", target)
		stdout := buffer.NewOutputBuffer()
		stderr := buffer.NewOutputBuffer()
		defer stdout.Close()
		defer stderr.Close()
		ie := s.NewInstanceExec(InstanceExec{
			Instance: i.Name,
			Cmd:      cmd,
			Stdout:   stdout,
			Stderr:   stderr,
		})
		ret, err := ie.Exec()

		if ret != 0 {
			return err
		}
	}

	return nil
}

type Window struct {
	Width  int
	Height int
}

type WindowChannel chan Window

func (s *Server) NewInstanceExec(e InstanceExec) *InstanceExec {
	return &InstanceExec{
		srv:      s,
		Instance: e.Instance,
		Cmd:      e.Cmd,
		Env:      e.Env,
		IsPty:    e.IsPty,
		Window:   e.Window,
		WinCh:    e.WinCh,
		User:     e.User,
		Group:    e.Group,
		Cwd:      e.Cwd,
		Stdin:    e.Stdin,
		Stdout:   e.Stdout,
		Stderr:   e.Stderr,
	}
}

type InstanceExec struct {
	srv      *Server
	Instance string
	Cmd      string
	Env      map[string]string
	IsPty    bool
	Window
	WinCh WindowChannel
	User  int
	Group int
	Cwd   string

	Stdin  io.ReadCloser
	Stdout io.WriteCloser
	Stderr io.WriteCloser

	execPost api.InstanceExecPost
	execArgs *incus.InstanceExecArgs

	ctx    context.Context
	cancel context.CancelFunc
}

// BuildExecRequest prepares the execution parameters
func (e *InstanceExec) BuildExecRequest() {
	args, _ := shlex.Split(e.Cmd, true)

	e.execPost = api.InstanceExecPost{
		Command:     args,
		WaitForWS:   true,
		Interactive: e.IsPty,
		Environment: e.Env,
		Width:       e.Window.Width,
		Height:      e.Window.Height,
		User:        uint32(e.User),
		Group:       uint32(e.Group),
		Cwd:         e.Cwd,
	}

	// Setup context with cancellation if not already done
	if e.ctx == nil {
		e.ctx, e.cancel = context.WithCancel(context.Background())
	}
}

func (e *InstanceExec) Exec() (int, error) {
	server := e.srv

	e.BuildExecRequest()

	// Setup error capturing
	errWriter, errBuf := e.setupErrorCapture()

	// Setup websocket control handler
	control, wg := e.setupControlHandler()

	// Setup execution args
	dataDone := make(chan bool)
	e.execArgs = &incus.InstanceExecArgs{
		Stdin:    e.Stdin,
		Stdout:   e.Stdout,
		Stderr:   errWriter,
		Control:  control,
		DataDone: dataDone,
	}

	// Execute the command
	op, err := server.srv.ExecInstance(e.Instance, e.execPost, e.execArgs)
	if err != nil {
		return -1, fmt.Errorf("exec instance: %w", err)
	}

	// Wait for operation to complete
	if err = op.Wait(); err != nil {
		return -1, fmt.Errorf("operation wait: %w", err)
	}

	// Wait for data transfer to complete
	<-dataDone

	// Wait for control handler to finish
	wg.Wait()

	// Get execution result
	opAPI := op.Get()
	ret := int(opAPI.Metadata["return"].(float64))

	errs := errBuf.String()
	if errs != "" {
		return ret, fmt.Errorf("exec errors: %s", errs)
	}

	return ret, nil
}

// setupControlHandler prepares the websocket control handler
func (e *InstanceExec) setupControlHandler() (func(*websocket.Conn), *sync.WaitGroup) {
	var ws *websocket.Conn
	var wg sync.WaitGroup

	control := func(conn *websocket.Conn) {
		ws = conn
		wg.Add(1)
		defer wg.Done()

		// Start window resize listener if channel is provided
		if e.WinCh != nil {
			go windowResizeListener(e.WinCh, ws)
		}

		// Read messages until connection is closed or context is canceled
		done := make(chan struct{})
		go func() {
			defer close(done)
			for {
				_, _, err := ws.ReadMessage()
				if err != nil {
					return
				}
			}
		}()

		select {
		case <-done:
			return
		case <-e.ctx.Done():
			ws.Close()
			return
		}
	}

	return control, &wg
}

// setupErrorCapture configures error capturing and returns a MultiWriter
func (e *InstanceExec) setupErrorCapture() (io.Writer, *bytes.Buffer) {
	var errBuf bytes.Buffer
	errWriter := io.MultiWriter(e.Stderr, &errBuf)
	return errWriter, &errBuf
}

func windowResizeListener(c WindowChannel, ws *websocket.Conn) {
	for win := range c {
		resizeWindow(ws, win.Width, win.Height)
	}
}

func resizeWindow(ws *websocket.Conn, width int, height int) {
	msg := api.InstanceExecControl{}
	msg.Command = "window-resize"
	msg.Args = make(map[string]string)
	msg.Args["width"] = strconv.Itoa(width)
	msg.Args["height"] = strconv.Itoa(height)

	ws.WriteJSON(msg)
}

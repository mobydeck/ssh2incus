package incus

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strconv"
	"sync"

	"ssh2incus/pkg/shlex"

	"github.com/gorilla/websocket"
	incus "github.com/lxc/incus/v6/client"
	"github.com/lxc/incus/v6/shared/api"
)

type Window struct {
	Width  int
	Height int
}

type WindowChannel chan Window

func (c *Client) NewInstanceExec(e InstanceExec) *InstanceExec {
	return &InstanceExec{
		client:   c,
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
	client   *Client
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
	client := e.client

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
	op, err := client.srv.ExecInstance(e.Instance, e.execPost, e.execArgs)
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
		return ret, fmt.Errorf("stderr: %s", errs)
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
			defer func() {
				if r := recover(); r != nil {
					// gorilla websocket may panic sometimes
				}
			}()

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

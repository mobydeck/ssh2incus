package incus

import (
	"bytes"
	"io"
	"strconv"

	"ssh2incus/pkg/util/shlex"

	"github.com/gorilla/websocket"
	"github.com/lxc/incus/v6/client"
	"github.com/lxc/incus/v6/shared/api"
	log "github.com/sirupsen/logrus"
)

func GetInstanceMeta(server incus.InstanceServer, instance string) (*api.ImageMetadata, string, error) {
	meta, etag, err := server.GetInstanceMetadata(instance)
	return meta, etag, err
}

type Window struct {
	Width  int
	Height int
}

type WindowChannel chan Window

type InstanceExec struct {
	Server   *incus.InstanceServer
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
}

func (e *InstanceExec) Exec() (int, error) {
	args, _ := shlex.Split(e.Cmd, true)

	log.Debugf("exec: %#v", args)

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

	var ws *websocket.Conn
	defer func() {
		if ws != nil {
			ws.Close()
		}
	}()

	control := func(conn *websocket.Conn) {
		ws = conn
		go windowResizeListener(e.WinCh, ws)
		for {
			_, _, err := ws.ReadMessage()
			if err != nil {
				break
			}
		}
	}

	var errBuf bytes.Buffer

	errmw := io.MultiWriter(e.Stderr, &errBuf)
	defer func() {
		errs := errBuf.String()
		if errs != "" {
			log.Errorln("exec errors: ", errs)
		}
	}()

	e.execArgs = &incus.InstanceExecArgs{
		Stdin:    e.Stdin,
		Stdout:   e.Stdout,
		Stderr:   errmw,
		Control:  control,
		DataDone: make(chan bool),
	}

	return e.exec()
}

func (e *InstanceExec) exec() (int, error) {

	op, err := (*e.Server).ExecInstance(e.Instance, e.execPost, e.execArgs)
	if err != nil {
		log.Errorln(err.Error())
		return -1, err
	}

	log.Debugf("exec meta: %#v", op.Get().Metadata)

	err = op.Wait()
	if err != nil {
		log.Errorln(err.Error())
		return -1, err
	}

	<-e.execArgs.DataDone
	opAPI := op.Get()

	ret := int(opAPI.Metadata["return"].(float64))

	return ret, nil
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

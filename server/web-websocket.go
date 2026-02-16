package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"ssh2incus/pkg/incus"
	"ssh2incus/pkg/ssh"

	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
	log "github.com/sirupsen/logrus"
)

var wsUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		// Allow all origins for now; restrict in production
		return true
	},
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

// WebSocketMessage represents messages sent over WebSocket
type WebSocketMessage struct {
	Type string          `json:"type"` // "resize", "ping", "pong", "error"
	Data json.RawMessage `json:"data,omitempty"`
}

// ResizeMessage represents terminal resize data
type ResizeMessage struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

// websocketTerminalHandler handles WebSocket terminal connections
func (ws *WebServer) websocketTerminalHandler(c echo.Context) error {
	// Get login parameter and decode it
	loginParam := c.Param("login")
	login, err := url.QueryUnescape(loginParam)
	if err != nil {
		log.Errorf("web: failed to decode login parameter: %v", err)
		return c.JSON(400, map[string]string{
			"error": "Invalid login parameter",
		})
	}

	// Replace $ with % for persistent session flag (used in URL to avoid encoding issues)
	if l, ok := strings.CutPrefix(login, "$"); ok {
		login = fmt.Sprintf("%%%s", l)
	}

	// Parse login string
	lu := &LoginUser{}
	lu.ParseFrom(login)

	log.Infof("web: websocket connection request for %s", lu)

	// Upgrade to WebSocket
	conn, err := wsUpgrader.Upgrade(c.Response(), c.Request(), nil)
	if err != nil {
		log.Errorf("web: websocket upgrade failed: %v", err)
		return err
	}
	defer conn.Close()

	// Setup keepalive with ping/pong frames
	conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	// Start keepalive goroutine to send periodic pings
	done := make(chan struct{})
	defer close(done)
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
				if err := conn.WriteMessage(websocket.PingMessage, []byte{}); err != nil {
					log.Debugf("web: failed to send ping: %v", err)
					return
				}
				conn.SetWriteDeadline(time.Time{})
			}
		}
	}()

	// Create session context
	ctx, cancel := ssh.NewContext(nil)
	defer cancel()
	lu.ctx = ctx

	// Validate login (this also populates cache)
	if !lu.IsValid() {
		log.Warnf("web: invalid login for %s", lu)
		sendErrorMessage(conn, fmt.Sprintf("Invalid login for %q", lu.OrigUser))
		return nil
	}

	// Log parsed login details for debugging
	log.Debugf("web: parsed login - Instance=%s, Project=%s, InstanceUser=%s, User=%s, Persistent=%v", lu.Instance, lu.Project, lu.InstanceUser, lu.User, lu.Persistent)

	// Create Incus client
	client, err := NewDefaultIncusClientWithContext(ctx)
	if err != nil {
		log.Errorf("web: failed to create incus client: %v", err)
		sendErrorMessage(conn, "Failed to connect to Incus")
		return nil
	}
	defer client.Disconnect()

	// Get instance user
	iu, err := client.GetCachedInstanceUser(lu.Project, lu.Instance, lu.InstanceUser)
	if err != nil || iu == nil {
		log.Errorf("web: failed to get instance user %q for %s: %v", lu.InstanceUser, lu, err)
		sendErrorMessage(conn, fmt.Sprintf("Instance user %q not found", lu.InstanceUser))
		return nil
	}

	// Setup environment
	env := map[string]string{
		"TERM":  "xterm-256color",
		"USER":  iu.User,
		"HOME":  iu.Dir,
		"SHELL": iu.Shell,
	}

	// Build command
	cmd := fmt.Sprintf("%s -l", iu.Shell)

	// Setup window size channel with buffering to accept initial resize
	incusWinCh := make(incus.WindowChannel, 1)
	defer close(incusWinCh)

	// Create WebSocket wrapper for I/O - handles all read/write to WebSocket
	// and demultiplexes control messages to the window channel
	wsWrapper := &wsWrapper{
		conn:      conn,
		controlCh: incusWinCh,
	}

	// Handle persistent session with tmux
	if lu.Persistent {
		usePrefix := false
		tmux, err := NewTermMux(ctx, config.TermMux, config.App.Name(), usePrefix)
		if err != nil {
			log.Errorf("web: failed to initialize terminal mux: %v", err)
			sendErrorMessage(conn, "Failed to create persistent session")
		}

		err = checkTermMux(wsWrapper, tmux, client, lu, iu, env)
		if err != nil {
			log.Errorf("web: failed to check/create tmux session: %v", err)
			sendErrorMessage(conn, fmt.Sprintf("Failed to create persistent session: %s", err))
		} else {
			cmd = tmux.Attach()
		}
	}

	log.Debugf("web: CMD %s", cmd)
	log.Debugf("web: ENV %v", env)

	// Setup pipes for WebSocket ↔ Incus bridging
	// Use io.Copy to bridge WebSocket directly to stdin/stdout without extra goroutines
	stdin, stdinWrite := io.Pipe()
	stdoutRead, stdout := io.Pipe()

	// Send welcome message if configured
	if welcome := welcomeHandler(iu); config.Welcome && welcome != "" {
		welcomeMsg := fmt.Sprintf("\r\n%s\r\n\n", welcome)
		wsWrapper.Write([]byte(welcomeMsg))
	}

	// Bridge WebSocket input to stdin
	go io.Copy(stdinWrite, wsWrapper)

	// Bridge stdout to WebSocket output
	go io.Copy(wsWrapper, stdoutRead)

	// Wait briefly for initial window size from client
	// This allows the pending resize to arrive before we start the shell
	initialWindow := incus.Window{Width: 80, Height: 24}
	select {
	case resizeMsg := <-incusWinCh:
		initialWindow = resizeMsg
		log.Debugf("web: using initial window size from client: %dx%d", initialWindow.Width, initialWindow.Height)
	case <-time.After(200 * time.Millisecond):
		// Timeout - use default size
		log.Debugf("web: timeout waiting for initial resize, using default 80x24")
	}

	// Execute command in instance
	ie := client.NewInstanceExec(incus.InstanceExec{
		Instance: lu.Instance,
		Cmd:      cmd,
		Env:      env,
		IsPty:    true,
		Window:   initialWindow,
		WinCh:    incusWinCh,
		Stdin:    stdin,
		Stdout:   stdout,
		Stderr:   stdout, // Send both stdout and stderr to same stream
		User:     iu.Uid,
		Group:    iu.Gid,
		Cwd:      iu.Dir,
	})

	ret, err := ie.Exec()
	if err != nil && err != io.EOF {
		log.Errorf("web: exec failed: %v", err)
	}

	log.Debugf("web: exit %d", ret)

	// Send a control message to indicate normal shell exit before closing
	// This allows the client to distinguish between normal exit and connection loss
	exitMsg := map[string]interface{}{
		"type": "exit",
		"data": map[string]int{
			"code": ret,
		},
	}
	if msgBytes, err := json.Marshal(exitMsg); err == nil {
		conn.WriteMessage(websocket.TextMessage, msgBytes)
	}

	return nil
}

// wsWrapper implements ReadWriteCloser on top of a websocket connection.
// Handles demultiplexing of binary (I/O) and text (control) messages.
type wsWrapper struct {
	conn      *websocket.Conn
	reader    io.Reader
	mur       sync.Mutex
	muw       sync.Mutex
	controlCh chan<- incus.Window // For sending resize events
}

func (w *wsWrapper) Read(p []byte) (n int, err error) {
	w.mur.Lock()
	defer w.mur.Unlock()

	// Keep reading until we get a binary message (skip control messages)
	for {
		// Get new message if no active one.
		if w.reader == nil {
			var mt int

			mt, w.reader, err = w.conn.NextReader()
			if err != nil {
				if websocket.IsCloseError(err, websocket.CloseNormalClosure) {
					return 0, io.EOF
				}
				return 0, err
			}

			// Handle control messages (text) and skip them
			if mt == websocket.TextMessage {
				// Read the entire text message
				var msg WebSocketMessage
				buf := new(bytes.Buffer)
				if _, err := io.Copy(buf, w.reader); err != nil {
					w.reader = nil
					continue
				}
				w.reader = nil

				// Parse and handle control message
				if err := json.Unmarshal(buf.Bytes(), &msg); err != nil {
					continue
				}

				switch msg.Type {
				case "resize":
					if w.controlCh != nil {
						var resize ResizeMessage
						if err := json.Unmarshal(msg.Data, &resize); err == nil {
							select {
							case w.controlCh <- incus.Window{Width: resize.Width, Height: resize.Height}:
							default:
								log.Warnf("web: controlCh full, resize dropped")
							}
						}
					}
				}
				// Continue to next message
				continue
			}

			if mt == websocket.CloseMessage {
				w.reader = nil
				return 0, io.EOF
			}

			// mt == websocket.BinaryMessage, fall through to read
		}

		// Perform the read from current binary message
		n, err = w.reader.Read(p)
		if err != nil {
			w.reader = nil // At the end of the message, reset reader.

			if err == io.EOF {
				// End of current binary message, try next one
				continue
			}

			return n, err
		}

		return n, nil
	}
}

func (w *wsWrapper) Write(p []byte) (int, error) {
	w.muw.Lock()
	defer w.muw.Unlock()

	// Send the data as a binary message for terminal data.
	err := w.conn.WriteMessage(websocket.BinaryMessage, p)
	if err != nil {
		return 0, err
	}

	return len(p), nil
}

func (w *wsWrapper) Close() error {
	// Close the WebSocket connection
	return w.conn.Close()
}

// sendErrorMessage sends an error message to the WebSocket client
func sendErrorMessage(conn *websocket.Conn, message string) {
	msg := WebSocketMessage{
		Type: "error",
		Data: json.RawMessage(fmt.Sprintf(`{"message":%q}`, message)),
	}
	data, _ := json.Marshal(msg)
	conn.WriteMessage(websocket.TextMessage, data)
}

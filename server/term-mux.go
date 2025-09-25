package server

import (
	"fmt"
	"strings"

	"ssh2incus/pkg/ssh"
)

// termMux contains the command templates for each supported terminal multiplexer.
var termMux = map[string]map[string]string{
	"tmux": {
		"list":   "ls",
		"new":    "new -s %s -d",
		"attach": "a -t %s",
	},
	"screen": {
		"list":   "-ls",
		"new":    "-dmS %s",
		"attach": "-r %s",
	},
}

// TermMux holds the selected mux name and its corresponding command map.
type TermMux struct {
	mux           string
	session       string
	cmds          map[string]string
	binWithPrefix bool
	ctx           ssh.Context
}

// NewTermMux creates a new TermMux for the given mux name.
// It returns an error if the mux name is unknown.
func NewTermMux(ctx ssh.Context, mux, session string, binWithPrefix bool) (*TermMux, error) {
	cmds, ok := termMux[mux]
	if !ok {
		return nil, fmt.Errorf("unsupported mux: %s", mux)
	}
	return &TermMux{mux: mux, session: session, cmds: cmds, ctx: ctx, binWithPrefix: binWithPrefix}, nil
}

// Name returns mux name.
func (t *TermMux) Name() string {
	if t.binWithPrefix {
		return fmt.Sprintf("%s-%s", t.session, t.mux)
	}
	return t.mux
}

// List returns the command used to list sessions.
func (t *TermMux) List() string {
	return t.Name() + " " + t.cmds["list"]
}

// New returns the command to create a new session, with the session name
// substituted into the format string.
func (t *TermMux) New() string {
	return t.Name() + " " + fmt.Sprintf(t.cmds["new"], t.session)
}

// Attach returns the command to attach to an existing session.
func (t *TermMux) Attach() string {
	return t.Name() + " " + fmt.Sprintf(t.cmds["attach"], t.session)
}

// SessionExists checks whether a session with the given name exists in the
// output returned by the mux's list command.
func (t *TermMux) SessionExists(lines []string) bool {
	for _, l := range lines {
		if strings.Contains(l, t.session+":") || strings.Contains(l, "."+t.session) {
			return true
		}
	}
	return false
}

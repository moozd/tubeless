// Package applet is the protocol between `tubeless exec` (running in a
// pane) and the tubeless window that hosts the applet's page.
//
// The open request travels in-band through the pty as OSC 7777, so it
// reaches the right window through tmux with no lookup. It names a Unix
// socket that `exec` listens on; the window dials it, and from then on
// the two exchange newline-delimited JSON Messages over it.
package applet

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// Request asks the window to open an applet in the pane `exec` runs in.
type Request struct {
	ID     int      `json:"id"`
	Socket string   `json:"socket"`
	Name   string   `json:"name"`
	Args   []string `json:"args"`
	Cwd    string   `json:"cwd"`
	Cols   int      `json:"cols"`
	Rows   int      `json:"rows"`
}

// MessageType names what a Message carries.
type MessageType string

const (
	// MsgReady: window to exec, the page is open.
	MsgReady MessageType = "ready"
	// MsgError: window to exec, the applet could not open.
	MsgError MessageType = "error"
	// MsgExit: window to exec, the applet ended with Code and Output.
	MsgExit MessageType = "exit"
	// MsgResize: exec to window, the pane is now Cols x Rows.
	MsgResize MessageType = "resize"
	// MsgFocus: exec to window, the pane gained or lost focus.
	MsgFocus MessageType = "focus"
	// MsgClose: exec to window, the user quit; close the page.
	MsgClose MessageType = "close"
)

// Message is one line on the applet socket.
type Message struct {
	Type    MessageType `json:"type"`
	Code    int         `json:"code,omitempty"`
	Output  string      `json:"output,omitempty"`
	Error   string      `json:"error,omitempty"`
	Cols    int         `json:"cols,omitempty"`
	Rows    int         `json:"rows,omitempty"`
	Focused bool        `json:"focused,omitempty"`
}

// EncodeRequest returns the OSC 7777 sequence for r. Inside tmux it is
// wrapped in tmux's DCS passthrough (needs `allow-passthrough on`).
func EncodeRequest(r Request, isTmux bool) (string, error) {
	data, err := json.Marshal(r)
	if err != nil {
		return "", fmt.Errorf("encode applet request: %w", err)
	}
	osc := "\x1b]7777;" + base64.StdEncoding.EncodeToString(data) + "\x1b\\"
	if !isTmux {
		return osc, nil
	}
	return "\x1bPtmux;" + strings.ReplaceAll(osc, "\x1b", "\x1b\x1b") + "\x1b\\", nil
}

// DecodeRequest parses an OSC 7777 payload (already base64-decoded).
func DecodeRequest(payload []byte) (Request, error) {
	var r Request
	if err := json.Unmarshal(payload, &r); err != nil {
		return r, fmt.Errorf("decode applet request: %w", err)
	}
	return r, nil
}

// WriteMessage sends one Message as a JSON line.
func WriteMessage(w io.Writer, m Message) error {
	data, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("encode applet message: %w", err)
	}
	if _, err := w.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("send applet message: %w", err)
	}
	return nil
}

// ReadMessages decodes JSON lines from r into out until r ends, then
// closes out.
func ReadMessages(r io.Reader, out chan<- Message) error {
	defer close(out)
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for sc.Scan() {
		var m Message
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			return fmt.Errorf("decode applet message: %w", err)
		}
		out <- m
	}
	return sc.Err()
}

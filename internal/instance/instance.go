// Package instance keeps one Skiff window per configuration folder. The
// running window writes where it listens in the folder; a second start
// (a link clicked in a browser, a shortcut) hands it its session URL and
// exits, and the running window opens the URL and comes to the front.
//
// The key of the file only lets one ask the window to open a URL (which
// it confirms when it comes from a link): it is not the token of the UI.
package instance

import (
	"bytes"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// FileName is the file of the running window, in the configuration folder.
const FileName = "instance.json"

type file struct {
	PID  int    `json:"pid"`
	Port int    `json:"port"`
	Key  string `json:"key"`
}

// Request is what a second start asks the running window.
type Request struct {
	URL string `json:"url,omitempty"`
	// Link: the URL comes from a link clicked elsewhere (confirmed first)
	Link bool `json:"link,omitempty"`
}

// handoffBody is a Request with the key of the running window.
type handoffBody struct {
	Key string `json:"key"`
	Request
}

// NewKey makes the key a running window writes in its file.
func NewKey() string {
	b := make([]byte, 24)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// Handoff gives req to the window running with this configuration folder.
// It reports whether one took it; false means none is running (or its file
// is stale), and this start goes on.
func Handoff(dir string, req Request) bool {
	b, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		return false
	}
	var f file
	if json.Unmarshal(b, &f) != nil || f.Port <= 0 || f.Key == "" || f.PID == os.Getpid() {
		return false
	}
	body, _ := json.Marshal(handoffBody{Key: f.Key, Request: req})
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Post(fmt.Sprintf("http://127.0.0.1:%d/handoff", f.Port), "application/json", bytes.NewReader(body))
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusNoContent
}

// Register writes the file of the window running here; the returned
// function removes it, unless another window has written its own since.
func Register(dir string, port int, key string) (func(), error) {
	p := filepath.Join(dir, FileName)
	b, _ := json.Marshal(file{PID: os.Getpid(), Port: port, Key: key})
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return func() {}, err
	}
	if err := os.WriteFile(p, b, 0o600); err != nil {
		return func() {}, err
	}
	return func() {
		cur, err := os.ReadFile(p)
		if err == nil && bytes.Equal(cur, b) {
			os.Remove(p)
		}
	}, nil
}

// Check reports whether body is a valid request for key, and returns it.
func Check(key string, body []byte) (Request, bool) {
	var h handoffBody
	if key == "" || json.Unmarshal(body, &h) != nil || subtle.ConstantTimeCompare([]byte(h.Key), []byte(key)) != 1 {
		return Request{}, false
	}
	return h.Request, true
}

// Package output types transcribed text via wtype, or pastes via the
// clipboard for apps where wtype is too slow or doesn't take focus correctly.
package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
)

// pasteWtypeArgs maps a config-level paste-method label to the wtype argv
// that performs that key chord.
var pasteWtypeArgs = map[string][]string{
	"C-v":   {"-M", "ctrl", "v", "-m", "ctrl"},
	"C-S-v": {"-M", "ctrl", "-M", "shift", "v", "-m", "shift", "-m", "ctrl"},
}

// Type sends text to the focused Wayland window. If the focused app's sway
// app_id has an entry in pasteMethods, the clipboard paste flow is used;
// otherwise wtype types the text directly.
func Type(text string, pasteMethods map[string]string) error {
	appID, _ := focusedAppID()
	method, ok := pasteMethods[appID]
	if ok {
		args, known := pasteWtypeArgs[method]
		if !known {
			return fmt.Errorf("unknown paste method %q for app_id %q", method, appID)
		}
		return paste(text, args)
	}
	return exec.Command("wtype", "--", text).Run()
}

// paste saves the clipboard, sets it to text, sends the paste chord, restores.
func paste(text string, chord []string) error {
	saved, _ := exec.Command("wl-paste", "--no-newline").Output()
	if err := exec.Command("wl-copy", "--", text).Run(); err != nil {
		return fmt.Errorf("wl-copy: %w", err)
	}
	_ = exec.Command("wtype", chord...).Run()
	restore := exec.Command("wl-copy", "--")
	restore.Stdin = bytes.NewReader(saved)
	return restore.Run()
}

// focusedAppID returns the sway app_id of the focused window.
func focusedAppID() (string, error) {
	out, err := exec.Command("swaymsg", "-t", "get_tree").Output()
	if err != nil {
		return "", err
	}
	var tree json.RawMessage
	if err := json.Unmarshal(out, &tree); err != nil {
		return "", err
	}
	return findFocused(tree), nil
}

func findFocused(raw json.RawMessage) string {
	var node struct {
		Focused       bool              `json:"focused"`
		AppID         *string           `json:"app_id"`
		Nodes         []json.RawMessage `json:"nodes"`
		FloatingNodes []json.RawMessage `json:"floating_nodes"`
	}
	if err := json.Unmarshal(raw, &node); err != nil {
		return ""
	}
	if node.Focused && node.AppID != nil {
		return *node.AppID
	}
	for _, child := range node.Nodes {
		if id := findFocused(child); id != "" {
			return id
		}
	}
	for _, child := range node.FloatingNodes {
		if id := findFocused(child); id != "" {
			return id
		}
	}
	return ""
}

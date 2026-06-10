package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// renderTranscript returns a plain-text view of the session for the preview pane.
func renderTranscript(path string, maxBytes int) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	var b strings.Builder
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	for sc.Scan() {
		var m rawMsg
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			continue
		}
		if m.Type != "user" && m.Type != "assistant" {
			continue
		}
		if m.IsMeta {
			continue
		}
		text := firstText(m.Message)
		if text == "" {
			continue
		}
		role := strings.ToUpper(m.Type)
		fmt.Fprintf(&b, "── %s ──\n%s\n\n", role, text)
		if maxBytes > 0 && b.Len() > maxBytes {
			b.WriteString("…(truncated)\n")
			break
		}
	}
	return b.String(), nil
}

//go:build windows

package main

import (
	"os"

	"golang.org/x/sys/windows"
)

func openTTY() (*os.File, *os.File, error) {
	// O_RDWR on both handles:
	//   - CONIN$:  bubbletea calls SetConsoleMode to enable raw input — requires write access.
	//   - CONOUT$: bubbletea calls GetConsoleMode via term.IsTerminal to detect tty and
	//     fetch terminal size; on Windows that needs read access. With O_WRONLY,
	//     IsTerminal returns false, no WindowSizeMsg is sent, and the UI hangs on "loading…".
	out, err := os.OpenFile("CONOUT$", os.O_RDWR, 0)
	if err != nil {
		return nil, nil, err
	}
	in, err := os.OpenFile("CONIN$", os.O_RDWR, 0)
	if err != nil {
		out.Close()
		return nil, nil, err
	}

	// Force the console output code page to UTF-8 so multibyte glyphs (e.g. `…`,
	// box-drawing) render correctly instead of as cp1252/cp437 mojibake.
	// Ignore errors — falling back to the existing CP is acceptable.
	_ = windows.SetConsoleOutputCP(65001) // CP_UTF8

	return in, out, nil
}

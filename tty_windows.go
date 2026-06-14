//go:build windows

package main

import "os"

func openTTY() (*os.File, *os.File, error) {
	out, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0)
	if err != nil {
		return nil, nil, err
	}
	// O_RDWR is required: bubbletea calls SetConsoleMode on this handle to
	// enable raw input mode, which on Windows needs write access. With O_RDONLY
	// the call fails with ERROR_ACCESS_DENIED ("error making raw: Access is denied").
	in, err := os.OpenFile("CONIN$", os.O_RDWR, 0)
	if err != nil {
		out.Close()
		return nil, nil, err
	}
	return in, out, nil
}

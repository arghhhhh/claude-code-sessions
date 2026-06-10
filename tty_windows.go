//go:build windows

package main

import "os"

func openTTY() (*os.File, *os.File, error) {
	out, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0)
	if err != nil {
		return nil, nil, err
	}
	in, err := os.OpenFile("CONIN$", os.O_RDONLY, 0)
	if err != nil {
		out.Close()
		return nil, nil, err
	}
	return in, out, nil
}

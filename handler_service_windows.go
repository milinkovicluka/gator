//go:build windows

package main

import "errors"

// The background service uses Unix process features, so on native Windows
// we point people to WSL or a plain `gator agg` instead.
func handlerService(s *state, cmd command) error {
	return errors.New("gator service isn't supported on Windows: use WSL, or run `gator agg 1m` in a terminal")
}

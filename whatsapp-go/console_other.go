//go:build !windows

package main

// detachConsole is a no-op outside Windows.
func detachConsole() {}

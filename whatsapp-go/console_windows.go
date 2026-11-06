package main

import "syscall"

// detachConsole closes the console window Windows opens for the scheduled
// task: the daemon logs to a file and needs no window.
func detachConsole() {
	_, _, _ = syscall.NewLazyDLL("kernel32.dll").NewProc("FreeConsole").Call()
}

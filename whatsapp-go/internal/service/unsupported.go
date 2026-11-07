//go:build !darwin && !linux && !windows

package service

import "whatsapp-mcp/internal/config"

func platformName() string    { return "background service" }
func platformSupported() bool { return false }
func platformDefinition([]string, config.Paths) (string, string, error) {
	return "", "", ErrUnsupported
}
func platformInstall([]string, config.Paths) (Installed, error) { return Installed{}, ErrUnsupported }
func platformUninstall() error                                  { return ErrUnsupported }
func platformStatus() State                                     { return State{} }
func platformRestart() error                                    { return ErrUnsupported }

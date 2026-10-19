//go:build !windows

package core

// Windows Setup integration has no registration or maintenance mutex on Unix.
func GuardInstallation() error    { return nil }
func SyncInstalledVersion() error { return nil }

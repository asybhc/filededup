//go:build desktop && !windows

package main

func everythingWindowsExtraDirs() []string { return nil }

func everythingScannedES() string { return "" }

func everythingProcessRunning() bool { return false }

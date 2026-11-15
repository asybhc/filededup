//go:build windows

package auth

import (
	"strings"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const (
	trialRegistryPath  = `Software\AI大叔\FileDedupTool.Trial`
	trialRegistryValue = "Trial"
)

func readTrialRegistry() (string, bool) {
	key, err := registry.OpenKey(registry.CURRENT_USER, trialRegistryPath, registry.QUERY_VALUE)
	if err != nil {
		return "", false
	}
	defer key.Close()
	value, _, err := key.GetStringValue(trialRegistryValue)
	if err != nil || strings.TrimSpace(value) == "" {
		return "", false
	}
	return value, true
}

func writeTrialRegistry(text string) error {
	key, _, err := registry.CreateKey(registry.CURRENT_USER, trialRegistryPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()
	return key.SetStringValue(trialRegistryValue, text)
}

// hideTrialFile 给试用记录加上隐藏属性，与旧版一致。
func hideTrialFile(path string) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return
	}
	_ = windows.SetFileAttributes(p, windows.FILE_ATTRIBUTE_HIDDEN)
}

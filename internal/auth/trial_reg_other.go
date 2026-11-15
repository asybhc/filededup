//go:build !windows

package auth

// 非 Windows 平台没有注册表与隐藏属性，试用记录只落在用户配置目录。
func readTrialRegistry() (string, bool) { return "", false }

func writeTrialRegistry(string) error { return nil }

func hideTrialFile(string) {}

//go:build windows

package scan

import "io/fs"

// platformHidden 是未注入 HiddenFile 时的默认判定。
// Windows 上隐藏信息存放在文件属性里，引擎不做系统调用，故默认不做隐藏过滤；
// 外壳（cmd/fdedup）会注入基于属性的实现。
func platformHidden(string, fs.FileInfo) bool { return false }

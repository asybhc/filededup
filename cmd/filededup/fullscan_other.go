//go:build desktop && !windows

package main

import "errors"

// listFixedVolumes 在非 Windows 平台不可用：全盘查重只针对本地固定盘符。
func listFixedVolumes() ([]string, error) {
	return nil, errors.New("全盘查重目前仅支持 Windows")
}

//go:build desktop

package filededup

import "embed"

// Assets 是 web/dist 的构建产物。只有 -tags desktop 构建时才需要它存在：
//
//	cd web && npm run build
//
// 资源放在模块根嵌入，因为 go:embed 不能引用上级目录。
//
//go:embed all:web/dist
var Assets embed.FS

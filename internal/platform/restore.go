package platform

// RestoreTarget 描述要恢复的文件及其用于唯一匹配回收站项的指纹。
//
// 同名文件可能被多次删除后仍同时留在回收站里，只按“文件名 + 原目录”匹配
// 会取到错误版本。Size 与 DeletedAt 来自撤销记录：
//   - Size <= 0 表示大小未知；已知大小时优先按大小过滤；
//   - DeletedAt 为 UTC RFC3339，空字符串表示未知；有值时按删除时间挑选最接近的项。
//
// 两项都无法唯一定位时不猜测，返回“无法确定要恢复的版本”。
type RestoreTarget struct {
	Path      string `json:"path"`
	Size      int64  `json:"size,omitempty"`
	DeletedAt string `json:"deletedAt,omitempty"`
}

// RestoreResult 是单个原路径的回收站恢复结果。
type RestoreResult struct {
	Path string `json:"path"`
	OK   bool   `json:"ok"`
	Err  string `json:"err,omitempty"`
}

// RestoreReport 是一次批量恢复的结果。Results 与输入路径一一对应。
type RestoreReport struct {
	Results []RestoreResult `json:"results"`
	// BinItems 是执行时回收站里的项目数，用于区分“文件不在回收站”和“回收站已被清空”。
	// -1 表示未能读取回收站（平台不支持或脚本未跑完）。
	BinItems int `json:"binItems"`
}

// failedRestore 为一批目标生成统一的失败结果（平台不支持、脚本不可用等）。
func failedRestore(targets []RestoreTarget, reason string) RestoreReport {
	rep := RestoreReport{Results: make([]RestoreResult, 0, len(targets)), BinItems: -1}
	for _, t := range targets {
		rep.Results = append(rep.Results, RestoreResult{Path: t.Path, Err: reason})
	}
	return rep
}

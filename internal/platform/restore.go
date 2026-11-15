package platform

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

// failedRestore 为一批路径生成统一的失败结果（平台不支持、脚本不可用等）。
func failedRestore(paths []string, reason string) RestoreReport {
	rep := RestoreReport{Results: make([]RestoreResult, 0, len(paths)), BinItems: -1}
	for _, p := range paths {
		rep.Results = append(rep.Results, RestoreResult{Path: p, Err: reason})
	}
	return rep
}

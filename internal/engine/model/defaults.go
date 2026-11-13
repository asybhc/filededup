package model

// DefaultRequest 返回与 C# 基线一致的扫描默认值。
// 注意：IncludeHidden=false 对应原版"跳过 Hidden/System"；FollowLinks=false 对应"跳过 ReparsePoint"。
func DefaultRequest() Request {
	return Request{
		SchemaVersion:   SchemaVersion,
		Roots:           []string{},
		Excludes:        []string{},
		Exact:           true,
		KeepPolicy:      KeepPathShortest,
		ImageKeepPolicy: KeepLargest,
		Workers:         0,
		UseCache:        true,
	}
}

// EnabledImageOpts 返回启用状态的默认图片判定参数（阈值 8）。
func EnabledImageOpts() *ImageOpts {
	return &ImageOpts{Enabled: true, Threshold: 8}
}

// EnabledAudioOpts 返回启用状态的默认音频判定参数（容差 2 秒）。
func EnabledAudioOpts() *AudioOpts {
	return &AudioOpts{Enabled: true, Tolerance: 2}
}

// DefaultStepToggles 返回原版默认开关：
// 前缀改名、后缀改名、去只读、文件整理默认开启；其余默认关闭（b/D.cs#L107-L191）。
func DefaultStepToggles() StepToggles {
	return StepToggles{
		RenamePrefix:  true,
		RenameSuffix:  true,
		SpecialDelete: false,
		Readonly:      true,
		ContentDedup:  false,
		ImageDedup:    false,
		AudioDedup:    false,
		Organize:      true,
		EmptyDirs:     false,
	}
}

// DefaultOrganizeRequest 返回整理器默认值。
//
// 与原版的两处显式差异（均需在差异清单登记）：
//  1. ConflictPolicy 默认 ConflictSkip，而原版默认 ConflictOverwrite（会删除源文件，高危）。
//     对拍时显式传入 ConflictOverwrite 以复现原版行为。
//  2. 保留策略沿用原版默认 pathShortest；图片保留沿用 C-15 修复后的 largest。
func DefaultOrganizeRequest(root, targetDir string) OrganizeRequest {
	return OrganizeRequest{
		SchemaVersion:       SchemaVersion,
		Root:                root,
		TargetDir:           targetDir,
		Excludes:            []string{},
		WhitelistExtensions: []string{},
		ConfirmedSuspects:   []string{},
		ConflictPolicy:      ConflictSkip,
		DeleteMode:          DeleteTrash,
		RecognizeStrength:   1,
		KeepPolicy:          KeepPathShortest,
		ImageKeepPolicy:     KeepLargest,
		ImageThreshold:      8,
		AudioToleranceSec:   2,
		Steps:               DefaultStepToggles(),
		Organize:            OrganizeModeOptions{Mode: OrganizeByCategory},
		Workers:             0,
	}
}

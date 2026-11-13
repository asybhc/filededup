package model_test

import (
	"strings"
	"testing"

	"filededup/internal/engine/model"
)

func TestDefaultRequestValid(t *testing.T) {
	r := model.DefaultRequest()
	r.Roots = []string{`C:\corpus`}
	if err := r.Validate(); err != nil {
		t.Fatalf("默认扫描请求应合法: %v", err)
	}
	if r.KeepPolicy != model.KeepPathShortest {
		t.Fatalf("默认保留策略应为 pathShortest，实际 %s", r.KeepPolicy)
	}
	if r.ImageKeepPolicy != model.KeepLargest {
		t.Fatalf("默认图片保留策略应为 largest，实际 %s", r.ImageKeepPolicy)
	}
}

func TestRequestValidation(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*model.Request)
		want   string
	}{
		{"无根目录", func(r *model.Request) { r.Roots = nil }, "roots 不能为空"},
		{"相对根目录", func(r *model.Request) { r.Roots = []string{"corpus"} }, "必须是绝对路径"},
		{"负最小大小", func(r *model.Request) { r.MinSize = -1 }, "minSize"},
		{"非法保留策略", func(r *model.Request) { r.KeepPolicy = "largest" }, "keepPolicy 不受支持"},
		{"无查重方式", func(r *model.Request) { r.Exact = false }, "至少启用一种查重方式"},
		{"阈值越界", func(r *model.Request) { r.SimilarImage = &model.ImageOpts{Enabled: true, Threshold: 65} }, "threshold"},
		{"快筛与图片冲突", func(r *model.Request) {
			r.QuickHash = true
			r.SimilarImage = model.EnabledImageOpts()
		}, "不能与 quickHash 同时启用"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := model.DefaultRequest()
			r.Roots = []string{`C:\corpus`}
			tc.mutate(&r)
			err := r.Validate()
			if err == nil {
				t.Fatalf("应校验失败")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("错误信息应包含 %q，实际: %v", tc.want, err)
			}
		})
	}
}

func TestDefaultOrganizeRequestValid(t *testing.T) {
	r := model.DefaultOrganizeRequest(`C:\corpus`, `C:\corpus\out`)
	if err := r.Validate(); err != nil {
		t.Fatalf("默认整理请求应合法: %v", err)
	}
	if r.ConflictPolicy != model.ConflictSkip {
		t.Fatalf("产品默认冲突策略应为 skip（安全侧），实际 %s", r.ConflictPolicy)
	}
	if !r.Steps.RenamePrefix || !r.Steps.RenameSuffix || !r.Steps.Readonly || !r.Steps.Organize {
		t.Fatal("原版默认开启的四个步骤未开启")
	}
	if r.Steps.SpecialDelete || r.Steps.ContentDedup || r.Steps.ImageDedup || r.Steps.AudioDedup || r.Steps.EmptyDirs {
		t.Fatal("原版默认关闭的步骤被开启")
	}
}

func TestOrganizeValidation(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*model.OrganizeRequest)
		want   string
	}{
		{"缺目标目录", func(r *model.OrganizeRequest) { r.TargetDir = "" }, "必须提供 targetDir"},
		{"非法冲突策略", func(r *model.OrganizeRequest) { r.ConflictPolicy = "x" }, "conflictPolicy"},
		{"非法删除方式", func(r *model.OrganizeRequest) { r.DeleteMode = "x" }, "deleteMode"},
		{"强度越界", func(r *model.OrganizeRequest) { r.RecognizeStrength = 3 }, "recognizeStrength"},
		{"关键字为空", func(r *model.OrganizeRequest) {
			r.Organize = model.OrganizeModeOptions{Mode: model.OrganizeByNameKeyword}
		}, "keyword 不能为空"},
		{"正则非法", func(r *model.OrganizeRequest) {
			r.Organize = model.OrganizeModeOptions{Mode: model.OrganizeByNameRegex, Regex: "("}
		}, "regex 非法"},
		{"全部步骤关闭", func(r *model.OrganizeRequest) { r.Steps = model.StepToggles{} }, "至少开启一个整理步骤"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := model.DefaultOrganizeRequest(`C:\corpus`, `C:\corpus\out`)
			tc.mutate(&r)
			err := r.Validate()
			if err == nil {
				t.Fatal("应校验失败")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("错误信息应包含 %q，实际: %v", tc.want, err)
			}
		})
	}
}

func TestOrderFor(t *testing.T) {
	standalone := model.OrderFor(false)
	preview := model.OrderFor(true)
	if len(standalone) != 9 || len(preview) != 9 {
		t.Fatalf("两套顺序都必须是 9 步，实际 %d/%d", len(standalone), len(preview))
	}
	if standalone[0] != model.StepRenamePrefix {
		t.Fatalf("自由整理第一步应为 rename_prefix，实际 %s", standalone[0])
	}
	if preview[0] != model.StepContentDedup {
		t.Fatalf("有预览计划第一步应为 content_dedup，实际 %s", preview[0])
	}
	if standalone[8] != model.StepEmptyDirs || preview[8] != model.StepEmptyDirs {
		t.Fatal("两套顺序最后一步都应是 empty_dirs")
	}
	seen := map[model.StepID]bool{}
	for _, id := range preview {
		if seen[id] {
			t.Fatalf("预览顺序出现重复步骤 %s", id)
		}
		seen[id] = true
	}
}

func TestPlanSummarize(t *testing.T) {
	p := model.Plan{Actions: []model.Action{
		{Kind: model.ActionDeleteTrash},
		{Kind: model.ActionDeletePermanent, Irreversible: true},
		{Kind: model.ActionMove},
		{Kind: model.ActionRename},
		{Kind: model.ActionHardlink},
		{Kind: model.ActionClearReadonly},
		{Kind: model.ActionRemoveDir},
	}}
	p.Summarize()
	if p.Summary.Actions != 7 || p.Summary.Deletes != 2 || p.Summary.Trash != 1 || p.Summary.Permanent != 1 {
		t.Fatalf("汇总计数错误: %+v", p.Summary)
	}
	if p.Summary.Moves != 1 || p.Summary.Renames != 1 || p.Summary.Hardlinks != 1 || p.Summary.ClearReadonly != 1 || p.Summary.RemoveDirs != 1 {
		t.Fatalf("汇总计数错误: %+v", p.Summary)
	}
	if p.Summary.Irreversible != 1 {
		t.Fatalf("不可逆项计数错误: %d", p.Summary.Irreversible)
	}
}

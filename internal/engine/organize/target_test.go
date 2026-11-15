package organize

import (
	"path/filepath"
	"testing"
)

// TestResolveTarget 覆盖目标解析：空、相对、根内绝对、根目录本身、根外绝对、相对逃逸。
func TestResolveTarget(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "elsewhere")

	cases := []struct {
		name     string
		raw      string
		wantRel  string
		wantAbs  string
		external bool
		wantErr  bool
	}{
		{name: "空值", raw: ""},
		{name: "相对路径", raw: "整理结果", wantRel: "整理结果"},
		{name: "相对路径规整", raw: "./a/b", wantRel: "a/b"},
		{name: "根内绝对", raw: filepath.Join(root, "out"), wantRel: "out"},
		{name: "根目录本身", raw: root, wantErr: true},
		{name: "根外绝对", raw: outside, wantRel: ExternalTargetAlias, wantAbs: filepath.ToSlash(filepath.Clean(outside)), external: true},
		{name: "相对逃逸", raw: "../x", wantErr: true},
		{name: "当前目录", raw: ".", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolveTarget(root, tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("期望报错，得到 %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("解析失败：%v", err)
			}
			if got.Rel != tc.wantRel || got.Abs != tc.wantAbs || got.External != tc.external {
				t.Fatalf("解析结果 = %+v, 期望 rel=%q abs=%q external=%v", got, tc.wantRel, tc.wantAbs, tc.external)
			}
		})
	}
}

// TestRewriteExternalTarget 只改写以别名开头（或等于别名）的目标，From 与形似路径不动。
func TestRewriteExternalTarget(t *testing.T) {
	base := "D:/整理输出"
	res := &Result{Steps: []StepPlan{
		{ID: string(StepOrganize), Actions: []Action{
			{Kind: ActionEnsureDir, To: ExternalTargetAlias},
			{Kind: ActionMove, From: "a/x.jpg", To: ExternalTargetAlias + "/图片/x.jpg"},
			{Kind: ActionMove, From: "b/y.txt", To: "整理结果/y.txt"},
			{Kind: ActionMove, From: "c/z.txt", To: ExternalTargetAlias + "x/z.txt"},
			{Kind: ActionMove, From: "d/w.txt", To: "其它/w.txt"},
		}},
	}}
	RewriteExternalTarget(res, base)
	acts := res.Steps[0].Actions
	if acts[0].To != base {
		t.Fatalf("ensure_dir 目标 = %q, 期望 %q", acts[0].To, base)
	}
	if acts[1].To != base+"/图片/x.jpg" {
		t.Fatalf("子路径目标 = %q, 期望 %q", acts[1].To, base+"/图片/x.jpg")
	}
	if acts[1].From != "a/x.jpg" {
		t.Fatalf("From 不应被改写：%q", acts[1].From)
	}
	if acts[2].To != "整理结果/y.txt" {
		t.Fatalf("根内目标不应被改写：%q", acts[2].To)
	}
	if acts[3].To != ExternalTargetAlias+"x/z.txt" {
		t.Fatalf("形似别名但非子路径不应被改写：%q", acts[3].To)
	}
	if acts[4].To != "其它/w.txt" {
		t.Fatalf("无关目标不应被改写：%q", acts[4].To)
	}
}

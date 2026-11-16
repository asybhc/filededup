//go:build desktop

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"filededup/internal/engine/organize"
)

// TestStartOrganizeExternalTargetPlan 目标在根目录之外时，预览计划能正常生成，
// 状态标注外部目标与移动/复制方式，动作目标被改写为真实绝对路径。
func TestStartOrganizeExternalTargetPlan(t *testing.T) {
	root := t.TempDir()
	out := filepath.Join(t.TempDir(), "根外目标")
	if err := os.WriteFile(filepath.Join(root, "a.jpg"), []byte("image-bytes"), 0o644); err != nil {
		t.Fatalf("写入测试文件失败：%v", err)
	}

	app := &App{}
	id, err := app.StartOrganize(OrganizeRequest{
		Root:            root,
		Target:          out,
		PlanOnly:        true,
		Steps:           OrganizeSteps{Organize: true},
		Mode:            "category",
		Conflict:        "overwrite",
		KeepPolicy:      "shortestPath",
		ImageKeepPolicy: "largest",
		ImageThreshold:  8,
		AudioTolerance:  2,
		Strength:        1,
	})
	if err != nil {
		t.Fatalf("启动整理失败：%v", err)
	}

	deadline := time.Now().Add(10 * time.Second)
	var st OrganizeState
	for {
		st, err = app.GetOrganizeState(id)
		if err != nil {
			t.Fatalf("读取状态失败：%v", err)
		}
		if st.Phase != "running" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("等待整理计划超时，最后状态：%+v", st)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if st.Phase != "done" {
		t.Fatalf("计划阶段失败：phase=%s error=%s", st.Phase, st.Error)
	}
	if !st.TargetExternal {
		t.Fatalf("targetExternal = false, 期望 true（targetDir=%q）", st.TargetDir)
	}
	wantDir := filepath.ToSlash(filepath.Clean(out))
	if st.TargetDir != wantDir {
		t.Fatalf("targetDir = %q, 期望 %q", st.TargetDir, wantDir)
	}
	switch st.MoveMode {
	case "move", "copy", "unknown":
	default:
		t.Fatalf("moveMode = %q, 期望 move/copy/unknown", st.MoveMode)
	}

	foundMove := false
	for _, act := range st.Actions {
		if act.Kind != string(organize.ActionMove) {
			continue
		}
		if !strings.HasPrefix(act.To, st.TargetDir+"/") {
			continue
		}
		foundMove = true
		switch act.Mode {
		case "move", "copy", "unknown":
		default:
			t.Fatalf("move 动作未标注移动/复制方式：%+v", act)
		}
	}
	if !foundMove {
		t.Fatalf("未找到指向根外目标的 move 动作：%+v", st.Actions)
	}
}

// TestOrganizeOpenDir 覆盖「整理完成后打开目标文件夹」的路径解析。
func TestOrganizeOpenDir(t *testing.T) {
	root := `C:\data`
	cases := []struct {
		name string
		spec organize.TargetSpec
		on   bool
		want string
	}{
		{"步骤关闭", organize.TargetSpec{Rel: "out"}, false, ""},
		{"未设目标", organize.TargetSpec{}, true, ""},
		{"根内相对路径", organize.TargetSpec{Rel: "out/sub"}, true, filepath.Join(root, "out", "sub")},
		{"根外绝对路径", organize.TargetSpec{Abs: "/tmp/out", External: true}, true, filepath.FromSlash("/tmp/out")},
	}
	for _, c := range cases {
		if got := organizeOpenDir(root, c.spec, c.on); got != c.want {
			t.Fatalf("%s: organizeOpenDir = %q, want %q", c.name, got, c.want)
		}
	}
}

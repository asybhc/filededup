//go:build desktop

package main

import (
	"strings"
	"testing"
)

func testerRules() OrganizeSettings {
	r := DefaultSettings().Organize
	r.Target = "整理"
	return r
}

// TestOrganizeTesterRename 覆盖前/后缀序列号剥离与保护词排除。
func TestOrganizeTesterRename(t *testing.T) {
	rules := testerRules()
	got, err := testOrganizeRules(OrganizeTestRequest{
		Rules:   rules,
		Samples: []string{"001-photo.jpg", "report (1).png"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Rows) != 2 {
		t.Fatalf("行数异常: %d", len(got.Rows))
	}
	if got.Rows[0].FinalName != "photo.jpg" || !got.Rows[0].Renamed {
		t.Fatalf("前缀剥离异常: %+v", got.Rows[0])
	}
	if got.Rows[1].FinalName != "report.png" || !got.Rows[1].Renamed {
		t.Fatalf("后缀剥离异常: %+v", got.Rows[1])
	}

	rules.ProtectWords = "photo"
	got, err = testOrganizeRules(OrganizeTestRequest{Rules: rules, Samples: []string{"001-photo.jpg"}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Rows[0].SpecialAction != "excluded" || got.Rows[0].FinalName != "001-photo.jpg" {
		t.Fatalf("保护词应跳过改名并标 excluded: %+v", got.Rows[0])
	}
	if got.Excluded != 1 {
		t.Fatalf("保护词计数异常: %+v", got)
	}
}

// TestOrganizeTesterSpecial 覆盖特殊文件判定与删除后不再整理。
func TestOrganizeTesterSpecial(t *testing.T) {
	rules := testerRules()
	rules.Steps.SpecialDelete = true
	rules.Steps.RenamePrefix = false
	rules.Steps.RenameSuffix = false

	normal, err := testOrganizeRules(OrganizeTestRequest{Rules: rules, Samples: []string{"旅行照片.jpg"}})
	if err != nil {
		t.Fatal(err)
	}
	if normal.Rows[0].SpecialAction != "keep" {
		t.Fatalf("中文标题应为 keep: %+v", normal.Rows[0])
	}
	if normal.Rows[0].SpecialReason == "" {
		t.Fatalf("keep 应带判定原因: %+v", normal.Rows[0])
	}

	garbage, err := testOrganizeRules(OrganizeTestRequest{Rules: rules, Samples: []string{"12345.jpg"}})
	if err != nil {
		t.Fatal(err)
	}
	if garbage.Rows[0].SpecialAction != "garbage" {
		t.Fatalf("全数字名应为 garbage: %+v", garbage.Rows[0])
	}
	if garbage.Rows[0].OrganizeSkip != "deleted" {
		t.Fatalf("删除后应跳过整理: %+v", garbage.Rows[0])
	}
	if garbage.Garbage != 1 || garbage.Skipped != 1 {
		t.Fatalf("汇总计数异常: %+v", garbage)
	}
}

// TestOrganizeTesterDest 覆盖各整理方式的目标目录与跳过原因。
func TestOrganizeTesterDest(t *testing.T) {
	rules := testerRules()
	rules.Mode = "extension"
	got, err := testOrganizeRules(OrganizeTestRequest{Rules: rules, Samples: []string{"photo.jpg"}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Rows[0].OrganizeDir != "整理/JPG" || got.Rows[0].DestPath != "整理/JPG/photo.jpg" {
		t.Fatalf("后缀名整理目标异常: %+v", got.Rows[0])
	}
	if got.Organized != 1 {
		t.Fatalf("整理计数异常: %+v", got)
	}

	rules.Target = ""
	got, _ = testOrganizeRules(OrganizeTestRequest{Rules: rules, Samples: []string{"photo.jpg"}})
	if got.Rows[0].OrganizeSkip != "no-target" {
		t.Fatalf("空目标应跳过: %+v", got.Rows[0])
	}

	rules.Target = "整理"
	rules.Mode = "keyword"
	rules.Keyword = ""
	got, _ = testOrganizeRules(OrganizeTestRequest{Rules: rules, Samples: []string{"photo.jpg"}})
	if got.Rows[0].OrganizeSkip != "keyword-empty" {
		t.Fatalf("空关键字应跳过: %+v", got.Rows[0])
	}

	rules.Keyword = "报告"
	got, _ = testOrganizeRules(OrganizeTestRequest{Rules: rules, Samples: []string{"photo.jpg"}})
	if got.Rows[0].OrganizeSkip != "keyword-miss" || got.Rows[0].DestPath != "" {
		t.Fatalf("未命中关键字应跳过: %+v", got.Rows[0])
	}

	rules.Mode = "fixedfolder"
	rules.FixedDir = "分类"
	rules.Keyword = ""
	got, _ = testOrganizeRules(OrganizeTestRequest{Rules: rules, Samples: []string{"photo.jpg"}})
	if got.Rows[0].OrganizeDir != "分类" || got.Rows[0].DestPath != "分类/photo.jpg" {
		t.Fatalf("指定文件夹目标异常: %+v", got.Rows[0])
	}

	rules.Mode = "namefeature"
	rules.Regex = "("
	got, _ = testOrganizeRules(OrganizeTestRequest{Rules: rules, Samples: []string{"photo.jpg"}})
	if got.Rows[0].OrganizeSkip != "regex-invalid" {
		t.Fatalf("非法正则应跳过: %+v", got.Rows[0])
	}
}

// TestOrganizeTesterInputLimits 覆盖空输入、上限与非法整理方式。
func TestOrganizeTesterInputLimits(t *testing.T) {
	rules := testerRules()
	if _, err := testOrganizeRules(OrganizeTestRequest{Rules: rules, Samples: []string{" ", ""}}); err == nil {
		t.Fatal("空样例应报错")
	}
	if _, err := testOrganizeRules(OrganizeTestRequest{Rules: rules, Samples: strings.Split(strings.Repeat("a\n", organizeTestMaxSamples+1), "\n")}); err == nil {
		t.Fatal("超过上限应报错")
	}
	bad := rules
	bad.Mode = "unknown"
	if _, err := testOrganizeRules(OrganizeTestRequest{Rules: bad, Samples: []string{"a.jpg"}}); err == nil {
		t.Fatal("非法整理方式应报错")
	}
}

// TestOrganizeTesterStepsOff 覆盖步骤关闭时的判定键。
func TestOrganizeTesterStepsOff(t *testing.T) {
	rules := testerRules()
	rules.Steps.SpecialDelete = false
	rules.Steps.Organize = false
	got, err := testOrganizeRules(OrganizeTestRequest{Rules: rules, Samples: []string{"photo.jpg"}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Rows[0].SpecialAction != "disabled" || got.Rows[0].OrganizeSkip != "disabled" {
		t.Fatalf("步骤关闭应标 disabled: %+v", got.Rows[0])
	}
}

package organize

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"
)

// specialGoldenItem 与 SpecialDump 输出的 JSON 字段一一对应。
type specialGoldenItem struct {
	Stem         string `json:"stem"`
	Strength     int    `json:"strength"`
	ProtectWords string `json:"protectWords"`
	Verdict      string `json:"verdict"`
	Suggestion   string `json:"suggestion"`
	Reason       string `json:"reason"`
	Score        int    `json:"score"`
	HardRule     bool   `json:"hardRule"`
}

// readSpecialCases 读取 cases.tsv（每行 stem\tstrength\tprotectWords），与 SpecialDump 的解析规则一致。
func readSpecialCases(t *testing.T) []specialGoldenItem {
	t.Helper()
	data, err := os.ReadFile("testdata/special_cases.tsv")
	if err != nil {
		t.Fatalf("读取语料失败: %v", err)
	}
	var cases []specialGoldenItem
	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSuffix(line, "\r")
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		item := specialGoldenItem{Stem: parts[0]}
		if len(parts) > 1 && strings.TrimSpace(parts[1]) != "" {
			strength, err := strconv.Atoi(strings.TrimSpace(parts[1]))
			if err != nil {
				t.Fatalf("第 %d 行 strength 非法: %v", i+1, err)
			}
			item.Strength = strength
		}
		if len(parts) > 2 {
			item.ProtectWords = parts[2]
		}
		cases = append(cases, item)
	}
	return cases
}

// TestClassifySpecialGolden 用真实 C# 程序产出的 golden 逐条对拍，要求 100% 一致。
func TestClassifySpecialGolden(t *testing.T) {
	rawGolden, err := os.ReadFile("testdata/special_golden.json")
	if err != nil {
		t.Fatalf("读取 golden 失败: %v", err)
	}
	var golden []specialGoldenItem
	if err := json.Unmarshal(rawGolden, &golden); err != nil {
		t.Fatalf("解析 golden 失败: %v", err)
	}
	cases := readSpecialCases(t)
	if len(cases) != len(golden) {
		t.Fatalf("语料行数 %d 与 golden 条数 %d 不一致", len(cases), len(golden))
	}
	mismatches := 0
	for i := range golden {
		item := cases[i]
		if item.Stem != golden[i].Stem || item.Strength != golden[i].Strength || item.ProtectWords != golden[i].ProtectWords {
			t.Fatalf("第 %d 条语料与 golden 输入不一致: %+v vs %+v", i+1, item, golden[i])
		}
		got := ClassifySpecial(item.Stem, SpecialOptions{
			ProtectWords:   item.ProtectWords,
			Strength:       item.Strength,
			LocalWhitelist: true,
		})
		want := golden[i]
		if string(got.Verdict) != want.Verdict || got.Suggestion != want.Suggestion ||
			got.Reason != want.Reason || got.Score != want.Score || got.HardRule != want.HardRule {
			mismatches++
			t.Errorf("第 %d 条不一致 stem=%q strength=%d protectWords=%q\n  got : verdict=%s suggestion=%s reason=%s score=%d hardRule=%v\n  want: verdict=%s suggestion=%s reason=%s score=%d hardRule=%v",
				i+1, item.Stem, item.Strength, item.ProtectWords,
				got.Verdict, got.Suggestion, got.Reason, got.Score, got.HardRule,
				want.Verdict, want.Suggestion, want.Reason, want.Score, want.HardRule)
		}
	}
	if mismatches == 0 {
		t.Logf("对拍通过：%d 条语料，一致率 100%%", len(golden))
	}
}

// TestClassifySpecialBoundary 覆盖名称归一的边界输入。
func TestClassifySpecialBoundary(t *testing.T) {
	emptyCases := []struct {
		name string
		stem string
	}{
		{"空串", ""},
		{"纯空格", "   "},
		{"全角空格", "　　"},
	}
	for _, tc := range emptyCases {
		got := ClassifySpecial(tc.stem, SpecialOptions{Strength: 1, LocalWhitelist: true})
		if got.Verdict != SpecialNormal || got.Suggestion != "PROTECT_EMPTY" || got.Normalized != "" {
			t.Errorf("%s: 期望 PROTECT_EMPTY/normal，实际 %+v", tc.name, got)
		}
	}
	for _, n := range []int{64, 65} {
		stem := strings.Repeat("x", n)
		got := ClassifySpecial(stem, SpecialOptions{Strength: 1, LocalWhitelist: true})
		if got.Normalized != stem {
			t.Errorf("%d 字符：归一结果长度错误，got %d", n, len(got.Normalized))
		}
		if got.Score < 0 || got.Score > 10 {
			t.Errorf("%d 字符：打分越界 %d", n, got.Score)
		}
		if got.Verdict != SpecialNormal && got.Verdict != SpecialSuspect && got.Verdict != SpecialGarbage {
			t.Errorf("%d 字符：非法判定 %q", n, got.Verdict)
		}
	}
}

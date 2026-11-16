//go:build desktop

package main

// 整理规则测试器：对样例文件名做纯计算，展示改名、特殊文件判定与整理去向。
//
// 只调用 internal/engine/organize 的导出纯函数与当前页面规则，
// 不扫盘、不读盘、不写盘，也不创建任何任务；返回值里的判定键供界面翻译。

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"filededup/internal/engine/organize"
)

const organizeTestMaxSamples = 200

// OrganizeTestRequest 是测试输入；Rules 来自整理页当前配置。
type OrganizeTestRequest struct {
	Rules   OrganizeSettings `json:"rules"`
	Samples []string         `json:"samples"`
}

// OrganizeTestRow 是单个样例的计算结果。
// SpecialAction / OrganizeSkip 是稳定键（界面负责翻译），不做中文硬编码。
type OrganizeTestRow struct {
	Original          string `json:"original"`
	FinalName         string `json:"finalName"`
	Renamed           bool   `json:"renamed"`
	SpecialAction     string `json:"specialAction"` // disabled | garbage | suspect | keep | excluded
	SpecialReason     string `json:"specialReason,omitempty"`
	SpecialSuggestion string `json:"specialSuggestion,omitempty"`
	SpecialScore      int    `json:"specialScore,omitempty"`
	OrganizeDir       string `json:"organizeDir,omitempty"`
	OrganizeSkip      string `json:"organizeSkip,omitempty"` // deleted | disabled | no-target | keyword-empty | keyword-miss | regex-empty | regex-invalid | regex-miss
	DestPath          string `json:"destPath,omitempty"`
}

// OrganizeTestResult 是测试汇总。
type OrganizeTestResult struct {
	Rows      []OrganizeTestRow `json:"rows"`
	Renamed   int               `json:"renamed"`
	Garbage   int               `json:"garbage"`
	Suspects  int               `json:"suspects"`
	Kept      int               `json:"kept"`
	Excluded  int               `json:"excluded"`
	Organized int               `json:"organized"`
	Skipped   int               `json:"skipped"`
}

// testOrganizeRules 是纯函数实现，便于单测直接调用。
func testOrganizeRules(req OrganizeTestRequest) (OrganizeTestResult, error) {
	mode := strings.ToLower(strings.TrimSpace(req.Rules.Mode))
	if !validOrganizeModes[mode] {
		return OrganizeTestResult{}, fmt.Errorf("未知整理方式：%s", req.Rules.Mode)
	}
	var samples []string
	for _, s := range req.Samples {
		if s = strings.TrimSpace(s); s != "" {
			samples = append(samples, s)
		}
	}
	if len(samples) == 0 {
		return OrganizeTestResult{}, errors.New("请至少输入一个样例文件名")
	}
	if len(samples) > organizeTestMaxSamples {
		return OrganizeTestResult{}, fmt.Errorf("样例不能超过 %d 行，请分批测试", organizeTestMaxSamples)
	}

	res := OrganizeTestResult{Rows: make([]OrganizeTestRow, 0, len(samples))}
	words := organize.SplitKeywords(req.Rules.ProtectWords)
	strength := req.Rules.Strength
	if strength < 0 {
		strength = 0
	}
	if strength > 2 {
		strength = 2
	}
	target := strings.TrimSpace(req.Rules.Target)
	keywords := organize.SplitKeywords(req.Rules.Keyword)
	modeEnum := organize.ParseOrganizeMode(mode)

	for _, sample := range samples {
		row := OrganizeTestRow{Original: sample, SpecialAction: "disabled"}
		name := organize.WinBase(sample)
		stem := organize.WinStem(name)
		ext := organize.WinExt(name)
		working := stem

		if organize.ContainsAnyFold(stem, words) {
			row.SpecialAction = "excluded"
			row.FinalName = name
			res.Excluded++
			res.Rows = append(res.Rows, row)
			continue
		}
		if req.Rules.Steps.RenamePrefix {
			working = organize.CleanPrefix(working)
		}
		if req.Rules.Steps.RenameSuffix {
			working = organize.CleanSuffix(working)
		}
		row.FinalName = working + ext
		row.Renamed = working != stem
		if row.Renamed {
			res.Renamed++
		}

		if req.Rules.Steps.SpecialDelete {
			verdict := organize.ClassifySpecial(working, organize.SpecialOptions{
				ProtectWords:   req.Rules.ProtectWords,
				Strength:       strength,
				LocalWhitelist: true,
			})
			row.SpecialReason = verdict.Reason
			row.SpecialSuggestion = verdict.Suggestion
			row.SpecialScore = verdict.Score
			switch verdict.Verdict {
			case organize.SpecialGarbage:
				row.SpecialAction = "garbage"
				res.Garbage++
			case organize.SpecialSuspect:
				row.SpecialAction = "suspect"
				res.Suspects++
			default:
				row.SpecialAction = "keep"
				res.Kept++
			}
		}

		switch row.SpecialAction {
		case "garbage":
			// 真实执行里该文件在第三步已删除，后面的整理不会再处理它。
			row.OrganizeSkip = "deleted"
			res.Skipped++
		case "disabled", "suspect", "keep":
			if !req.Rules.Steps.Organize {
				row.OrganizeSkip = "disabled"
				res.Skipped++
			} else if target == "" {
				row.OrganizeSkip = "no-target"
				res.Skipped++
			} else {
				destDir, skip := testOrganizeDest(modeEnum, req.Rules, target, keywords, row.FinalName)
				if skip == "" {
					row.OrganizeDir = destDir
					row.DestPath = organize.JoinRel(destDir, organize.SanitizeTargetName(row.FinalName))
					res.Organized++
				} else {
					row.OrganizeSkip = skip
					res.Skipped++
				}
			}
		}
		res.Rows = append(res.Rows, row)
	}
	return res, nil
}

// testOrganizeDest 复刻 stepOrganize 的目标目录与跳过条件（不复制动作落盘逻辑）。
func testOrganizeDest(mode organize.OrganizeMode, rules OrganizeSettings, target string, keywords []string, name string) (string, string) {
	switch mode {
	case organize.ModeCategory:
		return organize.JoinRel(target, organize.Category(name)), ""
	case organize.ModeExtension:
		return organize.JoinRel(target, organize.ExtensionUpper(name)), ""
	case organize.ModeFixedFolder:
		folder := strings.TrimSpace(rules.FixedDir)
		if folder == "" {
			return organize.JoinRel(target, "整理结果"), ""
		}
		return folder, ""
	case organize.ModeKeyword:
		if len(keywords) == 0 {
			return "", "keyword-empty"
		}
		if !organize.ContainsAnyFold(name, keywords) {
			return "", "keyword-miss"
		}
		return target, ""
	case organize.ModeNameFeature:
		if strings.TrimSpace(rules.Regex) == "" {
			return "", "regex-empty"
		}
		compiled, err := regexp.Compile("(?i)" + rules.Regex)
		if err != nil {
			return "", "regex-invalid"
		}
		if !compiled.MatchString(name) {
			return "", "regex-miss"
		}
		return target, ""
	}
	return "", "disabled"
}

// TestOrganizeRules 是界面绑定：纯计算，不需要应用初始化，也不触碰磁盘。
func (a *App) TestOrganizeRules(req OrganizeTestRequest) (OrganizeTestResult, error) {
	req.Rules = normalizeImportedRules(req.Rules)
	req.Rules.Root = ""
	return testOrganizeRules(req)
}

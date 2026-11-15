package auth

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// 授权状态取值，界面按这些值展示。
const (
	StatusLicensed     = "licensed"
	StatusTrial        = "trial"
	StatusTrialExpired = "trial-expired"
	StatusUnlicensed   = "unlicensed"
)

// FeatureInfo 是授权面板里的一行功能状态。未解锁的功能也会列出，避免静默隐藏。
type FeatureInfo struct {
	Key      string `json:"key"`
	Name     string `json:"name"`
	Unlocked bool   `json:"unlocked"`
	Edition  string `json:"edition"`          // 解锁所需版本名（标准版 / 专业版）
	Reason   string `json:"reason,omitempty"` // 未解锁时的原因
}

// State 是界面需要的完整授权快照。
type State struct {
	Status        string        `json:"status"`
	Licensed      bool          `json:"licensed"`
	Source        string        `json:"source"` // ed25519 | legacy-ecdsa | trial | none
	Edition       string        `json:"edition,omitempty"`
	EditionCode   string        `json:"editionCode,omitempty"` // standard | pro | trial
	FeatureList   []FeatureInfo `json:"featureList"`
	Expiry        string        `json:"expiry,omitempty"` // YYYY-MM-DD
	Forever       bool          `json:"forever"`
	MachineCode   string        `json:"machineCode"`
	Features      []string      `json:"features"`
	CanApply      bool          `json:"canApply"`
	CanOrganize   bool          `json:"canOrganize"`
	TrialActive   bool          `json:"trialActive"`
	TrialStart    string        `json:"trialStart,omitempty"`
	TrialEnd      string        `json:"trialEnd,omitempty"`
	TrialDaysLeft int           `json:"trialDaysLeft"`
	LicensePath   string        `json:"licensePath,omitempty"`
	Limits        string        `json:"limits"`
	Warning       string        `json:"warning,omitempty"`
}

// Allows 判断当前授权是否包含某个功能。
func (s State) Allows(feature string) bool {
	return hasFeature(s.Features, feature)
}

// LoadState 读取许可证与试用期，给出统一状态。此函数不写盘。
func LoadState() State {
	now := time.Now()
	raw, rawErr := MachineCodeRaw()

	st := State{
		Status:      StatusUnlicensed,
		Source:      "none",
		Features:    []string{FeatureScan},
		LicensePath: LicensePath(),
	}
	if rawErr != nil {
		st.Warning = "无法生成机器码：" + rawErr.Error()
	} else {
		st.MachineCode = groupBy4(raw)
	}

	var warnings []string
	licensed := false
	if rawErr == nil {
		if text, err := readStoredLicense(); err == nil {
			lic, verr := verifyLicenseWithMachine(text, crockfordFingerprint(raw), now)
			switch {
			case verr != nil:
				warnings = append(warnings, "许可证无效："+verr.Error())
			case lic.ExpiredOn(now):
				warnings = append(warnings, "许可证已于 "+lic.Expiry.Format("2006-01-02")+" 到期")
			default:
				licensed = true
				st.Licensed = true
				st.Status = StatusLicensed
				st.Source = lic.Source
				st.Edition = EditionName(lic.Edition)
				st.EditionCode = lic.Edition
				st.Forever = lic.Forever
				if !lic.Forever {
					st.Expiry = lic.Expiry.Format("2006-01-02")
				}
				st.Features = lic.Features
			}
		} else if !os.IsNotExist(err) {
			warnings = append(warnings, "读取许可证失败："+err.Error())
		}
	}

	if !licensed && rawErr == nil {
		trial, _, _ := evaluateTrial(crockfordFingerprint(raw), now, false)
		st.TrialActive = trial.Active
		st.TrialStart = trial.Start.Format("2006-01-02")
		st.TrialEnd = trial.End.Format("2006-01-02")
		st.TrialDaysLeft = trial.DaysLeft
		if trial.Active {
			st.Status = StatusTrial
			st.Source = "trial"
			st.Edition = "试用版"
			st.EditionCode = "trial"
			st.Features = append([]string{}, allFeatures...)
		} else if trial.Present {
			st.Status = StatusTrialExpired
		}
		if trial.Warning != "" {
			warnings = append(warnings, trial.Warning)
		}
	}

	st.CanApply = st.Allows(FeatureApply)
	st.CanOrganize = st.Allows(FeatureOrganize)
	st.FeatureList = buildFeatureList(st.Features)
	st.Limits = limitsText(st)
	if len(warnings) > 0 {
		st.Warning = strings.Join(warnings, "；")
	}
	return st
}

// crockfordFingerprint 把机器码字符串还原成 10 字节指纹。
func crockfordFingerprint(raw string) [10]byte {
	var fp [10]byte
	data, ok := crockfordDecode(raw)
	if !ok {
		return fp
	}
	copy(fp[:], data)
	return fp
}

// buildFeatureList 把已解锁功能位映射成界面列表；未解锁项同样返回，供界面明示原因。
func buildFeatureList(unlocked []string) []FeatureInfo {
	out := make([]FeatureInfo, 0, len(featureDefs))
	for _, d := range featureDefs {
		info := FeatureInfo{
			Key:      d.Key,
			Name:     d.Name,
			Unlocked: hasFeature(unlocked, d.Key),
			Edition:  EditionName(d.Edition),
		}
		if !info.Unlocked {
			info.Reason = fmt.Sprintf("需要%s许可证", EditionName(d.Edition))
		}
		out = append(out, info)
	}
	return out
}

// RequireFeature 在破坏性/需授权操作前调用；未授权时返回可直接展示给用户的错误。
// 错误里带功能名、所需版本与本机机器码，界面原样展示，不做静默降级。
func RequireFeature(feature string) error {
	st := LoadState()
	if st.Allows(feature) {
		return nil
	}
	action := FeatureName(feature)
	if action == "" || action == feature {
		action = "该操作"
	}
	need := EditionName(FeatureEdition(feature))
	if need == "" {
		need = "有效"
	}
	switch st.Status {
	case StatusLicensed:
		return fmt.Errorf("当前为%s：%s未解锁。请导入%s许可证（本机机器码 %s）。", st.Edition, action, need, st.MachineCode)
	case StatusTrialExpired:
		return fmt.Errorf("试用已结束：%s已禁用。请导入%s许可证（本机机器码 %s）。", action, need, st.MachineCode)
	default:
		return fmt.Errorf("未授权：%s已禁用。请导入%s许可证（本机机器码 %s）。", action, need, st.MachineCode)
	}
}

// unlockedSummary 把已解锁功能拼成短句，界面limits 行用。
func unlockedSummary(features []string) string {
	names := make([]string, 0, len(features))
	for _, d := range featureDefs {
		if hasFeature(features, d.Key) {
			names = append(names, d.Name)
		}
	}
	if len(names) == 0 {
		return "暂无"
	}
	return strings.Join(names, "、")
}

func limitsText(st State) string {
	switch {
	case st.Licensed:
		ver := st.Edition
		if ver == "" {
			ver = "标准版"
		}
		if st.Forever {
			return fmt.Sprintf("已授权（%s，永久）：已解锁 %s。", ver, unlockedSummary(st.Features))
		}
		return fmt.Sprintf("已授权（%s）：有效期至 %s，已解锁 %s。", ver, st.Expiry, unlockedSummary(st.Features))
	case st.TrialActive:
		return fmt.Sprintf("试用中：剩余 %d 天，专业版功能全部可用。", st.TrialDaysLeft)
	case st.Status == StatusTrialExpired:
		return "试用已结束：只能扫描与预览，删除执行、整理执行等已禁用。导入许可证后按版本解锁。"
	default:
		return "未授权：只能扫描与预览，删除执行、整理执行等已禁用。"
	}
}

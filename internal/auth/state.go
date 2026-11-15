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

// State 是界面需要的完整授权快照。
type State struct {
	Status        string   `json:"status"`
	Licensed      bool     `json:"licensed"`
	Source        string   `json:"source"` // ed25519 | legacy-ecdsa | trial | none
	Edition       string   `json:"edition,omitempty"`
	Expiry        string   `json:"expiry,omitempty"` // YYYY-MM-DD
	Forever       bool     `json:"forever"`
	MachineCode   string   `json:"machineCode"`
	Features      []string `json:"features"`
	CanApply      bool     `json:"canApply"`
	CanOrganize   bool     `json:"canOrganize"`
	TrialActive   bool     `json:"trialActive"`
	TrialStart    string   `json:"trialStart,omitempty"`
	TrialEnd      string   `json:"trialEnd,omitempty"`
	TrialDaysLeft int      `json:"trialDaysLeft"`
	LicensePath   string   `json:"licensePath,omitempty"`
	Limits        string   `json:"limits"`
	Warning       string   `json:"warning,omitempty"`
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
				st.Edition = editionLabel(lic.Edition)
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

// RequireFeature 在破坏性操作前调用；未授权时返回可直接展示给用户的错误。
func RequireFeature(feature string) error {
	st := LoadState()
	if st.Allows(feature) {
		return nil
	}
	action := "该操作"
	switch feature {
	case FeatureApply:
		action = "删除执行"
	case FeatureOrganize:
		action = "整理执行"
	}
	if st.Status == StatusTrialExpired {
		return fmt.Errorf("试用已结束：%s已禁用。请先在“授权”中导入许可证（本机机器码 %s）。", action, st.MachineCode)
	}
	return fmt.Errorf("未授权：%s已禁用。请先在“授权”中导入许可证（本机机器码 %s）。", action, st.MachineCode)
}

func editionLabel(edition string) string {
	switch edition {
	case "pro":
		return "专业版"
	case "legacy-ecdsa", "legacy":
		return "旧版兼容"
	case "standard":
		return "标准版"
	}
	if edition == "" {
		return "标准版"
	}
	return edition
}

func limitsText(st State) string {
	switch {
	case st.Licensed:
		if st.Forever {
			return "已授权（永久）：可使用扫描、删除执行与整理执行。"
		}
		return fmt.Sprintf("已授权：有效期至 %s，可使用扫描、删除执行与整理执行。", st.Expiry)
	case st.TrialActive:
		return fmt.Sprintf("试用中：剩余 %d 天，可使用全部功能。", st.TrialDaysLeft)
	case st.Status == StatusTrialExpired:
		return "试用已结束：只能扫描与预览，删除执行和整理执行已禁用。导入许可证后解锁。"
	default:
		return "未授权：只能扫描与预览，删除执行和整理执行已禁用。"
	}
}

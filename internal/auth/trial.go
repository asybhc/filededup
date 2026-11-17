package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"filededup/internal/brand"
)

const (
	trialPrefix         = "FDTR1"
	trialDays           = 3
	trialClockTolerance = 2 * time.Minute
	trialFileName       = "trial.dat"

	// 与旧版 C# 完全一致的密钥材料与盐，保证两边能互相识别试用记录。
	trialKeyPrefix = "FileDedupTool.Trial|"
	trialKeySuffix = "|7a3c9e5b"
)

var trialSalt = []byte{70, 68, 84, 82, 105, 97, 108, 45, 83, 97, 108, 116, 45, 118, 49, 0}

// TrialState 是试用期的只读视图。
type TrialState struct {
	Present  bool
	Active   bool
	Expired  bool
	Poisoned bool
	Start    time.Time
	End      time.Time
	DaysLeft int
	Warning  string
}

type trialRecord struct {
	start    time.Time
	last     time.Time
	started  bool
	rollback bool
}

type trialSource struct {
	name   string
	text   string
	exists bool
}

// trialDir 返回试用记录的落盘目录；迁移会把旧 FileDedupTool 里的 trial.dat 复制过来。
func trialDir() (string, error) {
	dir, err := brand.DataDir()
	if err != nil {
		return "", fmt.Errorf("无法定位用户配置目录：%w", err)
	}
	return dir, nil
}

// trialPassword 复刻旧版的密钥来源：固定前缀 + 机器码原始字节的
// 大写十六进制（带连字符）+ 固定后缀。
func trialPassword(fp [10]byte) string {
	return trialKeyPrefix + hexDashed(fp) + trialKeySuffix
}

func hexDashed(fp [10]byte) string {
	parts := make([]string, len(fp))
	for i, b := range fp {
		parts[i] = strings.ToUpper(hex.EncodeToString([]byte{b}))
	}
	return strings.Join(parts, "-")
}

func trialKey(fp [10]byte, legacy bool) []byte {
	password := []byte(trialPassword(fp))
	if legacy {
		sum := sha256.Sum256(password)
		return sum[:]
	}
	return pbkdf2SHA256(password, trialSalt, 10000, 32)
}

// pbkdf2SHA256 是 RFC 8018 的 PBKDF2-HMAC-SHA256，只覆盖本场景所需参数。
func pbkdf2SHA256(password, salt []byte, iterations, keyLen int) []byte {
	prf := hmac.New(sha256.New, password)
	hashLen := prf.Size()
	blocks := (keyLen + hashLen - 1) / hashLen
	out := make([]byte, 0, blocks*hashLen)
	buf := make([]byte, 4)
	for block := 1; block <= blocks; block++ {
		binary.BigEndian.PutUint32(buf, uint32(block))
		prf.Reset()
		prf.Write(salt)
		prf.Write(buf)
		u := prf.Sum(nil)
		t := make([]byte, hashLen)
		copy(t, u)
		for i := 1; i < iterations; i++ {
			prf.Reset()
			prf.Write(u)
			u = prf.Sum(u[:0])
			for j := range t {
				t[j] ^= u[j]
			}
		}
		out = append(out, t...)
	}
	return out[:keyLen]
}

func trialHMAC(key []byte, text string) string {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(text))
	return strings.ToUpper(hex.EncodeToString(mac.Sum(nil)))
}

func trialRecordText(rec trialRecord, key []byte) string {
	body := fmt.Sprintf("%s|%s|%s|%s|%s",
		trialPrefix,
		rec.start.Format(time.RFC3339Nano),
		rec.last.Format(time.RFC3339Nano),
		boolText(rec.started),
		boolText(rec.rollback),
	)
	return body + "|" + trialHMAC(key, body)
}

func boolText(v bool) string {
	if v {
		return "1"
	}
	return "0"
}

func parseTrialRecord(text string, key, legacyKey []byte) (trialRecord, bool, bool) {
	text = strings.TrimSpace(stripBOM(text))
	parts := strings.Split(text, "|")
	if len(parts) < 6 || parts[0] != trialPrefix {
		return trialRecord{}, false, false
	}
	body := strings.Join(parts[:len(parts)-1], "|")
	mac := parts[len(parts)-1]
	legacy := false
	if !strings.EqualFold(trialHMAC(key, body), mac) {
		if !strings.EqualFold(trialHMAC(legacyKey, body), mac) {
			return trialRecord{}, false, false
		}
		legacy = true
	}
	start, err := time.Parse(time.RFC3339Nano, parts[1])
	if err != nil {
		return trialRecord{}, false, false
	}
	last, err := time.Parse(time.RFC3339Nano, parts[2])
	if err != nil {
		return trialRecord{}, false, false
	}
	return trialRecord{
		start:    start,
		last:     last,
		started:  parts[3] == "1",
		rollback: parts[4] == "1",
	}, true, legacy
}

// readTrialSources 读取三处存储：主文件、备份文件、注册表。
func readTrialSources() []trialSource {
	sources := make([]trialSource, 0, 3)
	if dir, err := trialDir(); err == nil {
		for _, item := range []struct {
			name string
			path string
		}{
			{"trial.dat", filepath.Join(dir, trialFileName)},
			{"trial.dat.bak", filepath.Join(dir, trialFileName+".bak")},
		} {
			raw, err := os.ReadFile(item.path)
			if err != nil {
				continue
			}
			sources = append(sources, trialSource{name: item.name, text: string(raw), exists: true})
		}
	}
	if text, ok := readTrialRegistry(); ok {
		sources = append(sources, trialSource{name: "registry", text: text, exists: true})
	}
	return sources
}

// evaluateTrial 汇总三处记录并套用旧版规则：
// 取最早开始、最晚见到；任一来源标记回拨即失效；时间回拨超过 2 分钟也失效。
func evaluateTrial(fp [10]byte, now time.Time, create bool) (TrialState, trialRecord, bool) {
	return evaluateTrialFrom(readTrialSources(), fp, now, create)
}

func evaluateTrialFrom(sources []trialSource, fp [10]byte, now time.Time, create bool) (TrialState, trialRecord, bool) {
	key := trialKey(fp, false)
	legacyKey := trialKey(fp, true)

	var recs []trialRecord
	var invalidSources []string
	legacySeen := false
	for _, src := range sources {
		if !src.exists || strings.TrimSpace(src.text) == "" {
			continue
		}
		rec, ok, legacy := parseTrialRecord(src.text, key, legacyKey)
		if !ok {
			invalidSources = append(invalidSources, src.name)
			continue
		}
		legacySeen = legacySeen || legacy
		recs = append(recs, rec)
	}

	if len(recs) == 0 {
		if len(invalidSources) > 0 {
			return TrialState{
				Present:  true,
				Poisoned: true,
				Warning:  "试用记录无效（可能被篡改），试用已失效。",
			}, trialRecord{}, false
		}
		// 首次运行：未落盘时也先按试用中展示，由 TouchTrial 负责写入。
		rec := trialRecord{start: now, last: now, started: true}
		return TrialState{
			Present:  create,
			Active:   true,
			Start:    now,
			End:      now.AddDate(0, 0, trialDays),
			DaysLeft: trialDays,
		}, rec, create
	}

	merged := recs[0]
	for _, r := range recs[1:] {
		if r.start.Before(merged.start) {
			merged.start = r.start
		}
		if r.last.After(merged.last) {
			merged.last = r.last
		}
		merged.started = merged.started || r.started
		merged.rollback = merged.rollback || r.rollback
	}
	merged.started = true

	end := merged.start.AddDate(0, 0, trialDays)
	st := TrialState{Present: true, Start: merged.start, End: end}

	if merged.rollback {
		st.Poisoned = true
		st.Warning = "检测到试用记录被标记为时间回拨，试用已失效。"
		return st, merged, false
	}
	effective := merged.last
	if merged.start.After(effective) {
		effective = merged.start
	}
	if now.Before(effective.Add(-trialClockTolerance)) {
		merged.rollback = true
		st.Poisoned = true
		st.Warning = "检测到系统时间回拨，试用已失效。"
		return st, merged, true
	}

	needsSave := legacySeen
	if merged.last.Before(now) {
		merged.last = now
		needsSave = true
	}
	if now.Before(end) {
		st.Active = true
		left := int(math.Ceil(end.Sub(now).Hours() / 24))
		if left < 1 {
			left = 1
		}
		if left > trialDays {
			left = trialDays
		}
		st.DaysLeft = left
	} else {
		st.Expired = true
	}
	return st, merged, needsSave
}

// TouchTrial 首次运行时创建试用记录，并在每次启动时更新“最后见到”时间。
// 失败不阻断启动，由界面上的授权状态给出提示。
func TouchTrial() error {
	fp, err := machineFingerprint()
	if err != nil {
		return err
	}
	_, rec, needsSave := evaluateTrial(fp, time.Now(), true)
	if !needsSave {
		return nil
	}
	return writeTrialRecord(rec, fp)
}

func writeTrialRecord(rec trialRecord, fp [10]byte) error {
	dir, err := trialDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("创建试用记录目录失败：%w", err)
	}
	text := trialRecordText(rec, trialKey(fp, false))
	primary := filepath.Join(dir, trialFileName)
	backup := primary + ".bak"
	if err := writeFileAtomic(primary, text); err != nil {
		return err
	}
	if err := writeFileAtomic(backup, text); err != nil {
		return err
	}
	hideTrialFile(primary)
	hideTrialFile(backup)
	if err := writeTrialRegistry(text); err != nil {
		return err
	}
	return nil
}

func writeFileAtomic(path, text string) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(text), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

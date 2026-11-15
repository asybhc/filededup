package auth

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"strings"
	"testing"
	"time"
)

func testFingerprint(b byte) [10]byte {
	var fp [10]byte
	for i := range fp {
		fp[i] = b + byte(i)
	}
	return fp
}

func TestCrockfordRoundTrip(t *testing.T) {
	fp := testFingerprint(1)
	encoded := crockfordEncode(fp[:])
	if len(encoded) != 16 {
		t.Fatalf("编码长度应为 16，实际 %d：%s", len(encoded), encoded)
	}
	if strings.ContainsAny(encoded, "ILOU") {
		t.Fatalf("机器码不应包含易混字符：%s", encoded)
	}
	decoded, ok := crockfordDecode(encoded)
	if !ok || len(decoded) != 10 {
		t.Fatalf("解码失败：%v %d", ok, len(decoded))
	}
	for i := range fp {
		if decoded[i] != fp[i] {
			t.Fatalf("解码结果不一致：%x != %x", decoded, fp)
		}
	}
	// 大小写、连字符与易混字符应被归一化。
	if got := normalizeMachineText(groupBy4(encoded)); got != encoded {
		t.Fatalf("归一化结果不一致：%s != %s", got, encoded)
	}
}

func TestPBKDF2KnownVector(t *testing.T) {
	// RFC 7914 / RFC 8018 常见的 PBKDF2-HMAC-SHA256 测试向量。
	got := pbkdf2SHA256([]byte("password"), []byte("salt"), 2, 32)
	want, _ := hex.DecodeString("ae4d0c95af6b46d32d0adff928f06dd02a303f8ef3c251dfd6e2d85a95474c43")
	if !strings.EqualFold(hex.EncodeToString(got), hex.EncodeToString(want)) {
		t.Fatalf("PBKDF2 结果不正确：%x", got)
	}
}

func TestEd25519License(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	oldKey := embeddedEd25519PublicKeyB64
	embeddedEd25519PublicKeyB64 = base64.StdEncoding.EncodeToString(pub)
	defer func() { embeddedEd25519PublicKeyB64 = oldKey }()

	fp := testFingerprint(2)
	raw := crockfordEncode(fp[:])
	text, err := IssueLicense(Payload{
		Machine: groupBy4(raw),
		Expiry:  "2030-01-01",
		Edition: "pro",
		Issued:  "2026-11-15",
		ID:      "TEST01",
	}, priv)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Date(2029, 6, 1, 10, 0, 0, 0, time.Local)
	lic, err := verifyLicenseWithMachine(text, fp, now)
	if err != nil {
		t.Fatalf("验签应通过：%v", err)
	}
	if lic.Source != "ed25519" || lic.Edition != "pro" || lic.Forever {
		t.Fatalf("许可证字段不正确：%+v", lic)
	}
	if lic.ExpiredOn(now) {
		t.Fatal("到期日之前不应判定为过期")
	}
	if !lic.ExpiredOn(time.Date(2030, 1, 2, 0, 0, 0, 0, time.Local)) {
		t.Fatal("到期日之后应判定为过期")
	}

	if _, err := verifyLicenseWithMachine(text, testFingerprint(9), now); err == nil {
		t.Fatal("机器码不匹配时应拒绝")
	}

	// 翻转签名原文的一个比特再编码：直接改 base64 末位可能只动到填充位，
	// 解码结果不变，会导致测试偶发失败。
	parts := strings.Split(text, ":")
	sigRaw, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		t.Fatal(err)
	}
	sigRaw[0] ^= 0x01
	tampered := parts[0] + ":" + parts[1] + ":" + base64.RawURLEncoding.EncodeToString(sigRaw)
	if _, err := verifyLicenseWithMachine(tampered, fp, now); err == nil {
		t.Fatal("篡改后的许可证应被拒绝")
	}

	wrongApp, err := IssueLicense(Payload{Machine: "*", Expiry: "forever"}, priv)
	if err != nil {
		t.Fatal(err)
	}
	licAny, err := verifyLicenseWithMachine(wrongApp, testFingerprint(3), now)
	if err != nil {
		t.Fatalf("不绑定机器的许可证应可用：%v", err)
	}
	if !licAny.Forever {
		t.Fatal("永久许可证应标记为 Forever")
	}
}

func TestLegacyECDSALicense(t *testing.T) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	oldKey := legacyPublicKeySPKI
	legacyPublicKeySPKI = base64.StdEncoding.EncodeToString(der)
	defer func() { legacyPublicKeySPKI = oldKey }()

	fp := testFingerprint(4)
	build := func(days uint16, edition byte) string {
		payload := make([]byte, 18)
		payload[0] = 2
		binary.LittleEndian.PutUint32(payload[1:5], legacyAppID)
		copy(payload[5:15], fp[:])
		binary.LittleEndian.PutUint16(payload[15:17], days)
		payload[17] = edition
		digest := sha256.Sum256(payload)
		r, s, err := ecdsa.Sign(rand.Reader, priv, digest[:])
		if err != nil {
			t.Fatal(err)
		}
		sig := make([]byte, 64)
		r.FillBytes(sig[:32])
		s.FillBytes(sig[32:])
		return groupBy4(crockfordEncode(append(payload, sig...)))
	}

	now := time.Date(2026, 11, 15, 0, 0, 0, 0, time.Local)
	lic, err := verifyLegacyLicense(build(30, 1), fp, now)
	if err != nil {
		t.Fatalf("旧版许可证验签应通过：%v", err)
	}
	if lic.Source != "legacy-ecdsa" || lic.Edition != "pro" || lic.Forever {
		t.Fatalf("旧版许可证字段不正确：%+v", lic)
	}
	wantExpiry := time.Date(2024, 1, 31, 0, 0, 0, 0, time.Local)
	if !lic.Expiry.Equal(wantExpiry) {
		t.Fatalf("到期日不正确：%v", lic.Expiry)
	}

	licForever, err := verifyLegacyLicense(build(0xFFFF, 0), fp, now)
	if err != nil || !licForever.Forever {
		t.Fatalf("永久旧版许可证应可用：%v %+v", err, licForever)
	}
	if licForever.Edition != "standard" {
		t.Fatalf("edition=0 应为标准版：%s", licForever.Edition)
	}

	if _, err := verifyLegacyLicense(build(30, 1), testFingerprint(7), now); err == nil {
		t.Fatal("旧版许可证机器码不匹配时应拒绝")
	}

	// 直接翻转签名里的一个比特：只改文本末位可能只动到 Crockford 的填充位，
	// 解码结果不变，会导致测试偶发失败。
	valid := build(30, 1)
	data, ok := crockfordDecode(normalizeMachineText(valid))
	if !ok {
		t.Fatal("旧版许可证应能解码")
	}
	data[len(data)-1] ^= 0x01
	if _, err := verifyLegacyLicense(crockfordEncode(data), fp, now); err == nil {
		t.Fatal("旧版许可证签名损坏时应拒绝")
	}
}

func TestTrialRecordRoundTrip(t *testing.T) {
	fp := testFingerprint(5)
	now := time.Date(2026, 11, 15, 9, 30, 0, 1234567, time.Local)
	rec := trialRecord{start: now.Add(-time.Hour), last: now, started: true}
	text := trialRecordText(rec, trialKey(fp, false))

	parsed, ok, legacy := parseTrialRecord(text, trialKey(fp, false), trialKey(fp, true))
	if !ok || legacy {
		t.Fatalf("记录应能解析：ok=%v legacy=%v", ok, legacy)
	}
	if !parsed.start.Equal(rec.start) || !parsed.last.Equal(rec.last) || !parsed.started || parsed.rollback {
		t.Fatalf("解析结果不一致：%+v", parsed)
	}

	legacyText := trialRecordText(rec, trialKey(fp, true))
	if _, ok, isLegacy := parseTrialRecord(legacyText, trialKey(fp, false), trialKey(fp, true)); !ok || !isLegacy {
		t.Fatal("旧版密钥签名的记录应被识别并允许迁移")
	}

	poisoned := text[:len(text)-1] + "0"
	if poisoned == text {
		poisoned = text[:len(text)-1] + "1"
	}
	if _, ok, _ := parseTrialRecord(poisoned, trialKey(fp, false), trialKey(fp, true)); ok {
		t.Fatal("HMAC 不匹配的记录应被拒绝")
	}
}

func TestTrialFirstRunAndExpiry(t *testing.T) {
	fp := testFingerprint(6)
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.Local)

	st, _, _ := evaluateTrialFrom(nil, fp, now, false)
	if !st.Active || st.DaysLeft != 3 || st.Present {
		t.Fatalf("首次运行应显示试用中：%+v", st)
	}

	start := now.AddDate(0, 0, -2)
	rec := trialRecord{start: start, last: now.Add(-time.Hour), started: true}
	st, merged, needsSave := evaluateTrialFrom([]trialSource{{name: "file", text: trialRecordText(rec, trialKey(fp, false)), exists: true}}, fp, now, false)
	if !st.Active || st.DaysLeft != 1 {
		t.Fatalf("剩余天数应向上取整为 1：%+v", st)
	}
	if !merged.last.Equal(now) || !needsSave {
		t.Fatalf("应更新最后见到时间：%+v", merged)
	}

	expired := trialRecord{start: now.AddDate(0, 0, -4), last: now.Add(-time.Hour), started: true}
	st, _, _ = evaluateTrialFrom([]trialSource{{name: "file", text: trialRecordText(expired, trialKey(fp, false)), exists: true}}, fp, now, false)
	if st.Active || !st.Expired {
		t.Fatalf("超过 3 天应过期：%+v", st)
	}
}

func TestTrialStrictestMergeAndRollback(t *testing.T) {
	fp := testFingerprint(7)
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.Local)
	key := trialKey(fp, false)

	older := trialRecord{start: now.AddDate(0, 0, -1), last: now.Add(-time.Hour), started: true}
	newer := trialRecord{start: now.Add(-time.Hour), last: now.Add(-30 * time.Minute), started: true}
	st, merged, _ := evaluateTrialFrom([]trialSource{
		{name: "file", text: trialRecordText(older, key), exists: true},
		{name: "registry", text: trialRecordText(newer, key), exists: true},
	}, fp, now, false)
	if !merged.start.Equal(older.start) {
		t.Fatalf("应取最早的开始时间：%v", merged.start)
	}
	if !st.Active || st.DaysLeft != 2 {
		t.Fatalf("双存储合并结果不正确：%+v", st)
	}

	flagged := trialRecord{start: now.Add(-time.Hour), last: now, started: true, rollback: true}
	st, _, _ = evaluateTrialFrom([]trialSource{{name: "file", text: trialRecordText(flagged, key), exists: true}}, fp, now, false)
	if !st.Poisoned {
		t.Fatalf("带回拨标记的记录应失效：%+v", st)
	}

	future := trialRecord{start: now.Add(-time.Hour), last: now.Add(10 * time.Minute), started: true}
	st, merged, needsSave := evaluateTrialFrom([]trialSource{{name: "file", text: trialRecordText(future, key), exists: true}}, fp, now, false)
	if !st.Poisoned || !needsSave || !merged.rollback {
		t.Fatalf("系统时间早于最后见到时间超过阈值时应失效并落盘：%+v save=%v", st, needsSave)
	}
}

func TestTrialInvalidSource(t *testing.T) {
	fp := testFingerprint(8)
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.Local)
	st, _, _ := evaluateTrialFrom([]trialSource{{name: "file", text: "FDTR1|bad", exists: true}}, fp, now, false)
	if !st.Poisoned {
		t.Fatalf("唯一来源损坏时应判定试用失效，而不是重新开始：%+v", st)
	}
}

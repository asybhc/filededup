//go:build desktop

package main

// 蓝奏云（老版）上传门面：登录态、目录浏览、上传前预检、上传执行与报告。
//
// 与 C# 版对等的范围（一期）：
//   - 账号密码登录为主，粘贴 Cookie 为降级；DPAPI(CurrentUser) 加密存到
//     %AppData%\FileDedup\lanzou\lanzou.bin，界面只显后四位，日志不落明文；
//   - 上传并发 1–5（默认 3）、请求间隔 1000–2000ms（默认 1500）、>100 MiB 跳过、
//     失败重试 ≤3、可取消；
//   - 云端去重：同名（Trim + 忽略大小写）且大小在 max(2048, 本地/100) 容差内即重复，
//     不上传，本地文件移动到上传目录旁边的「网盘重复文件」；
//   - 错误一律透传服务端原文，不伪造成功；
//   - 结束后写 upload-report-*.json + .csv。
//
// 未联调声明：登录成功后的真实列表、上传与去重命中都需要真实账号验证。本文件只
// 保证编译、单测（httptest）与本地可验证的部分；界面与文档必须如实标注「未联调，需账号」。
//
// 分享创建不做：C# 版没有该功能，服务端 /proved/share/url 目前也不可用。

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"filededup/internal/auth"
	"filededup/internal/cloud/lanzou"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// duplicateDirName 是云端重复文件的本地收纳目录，与 C# 版常量一致。
const duplicateDirName = "网盘重复文件"

// LanzouStatus 是登录态快照。永远不返回完整 Cookie。
type LanzouStatus struct {
	LoggedIn       bool   `json:"loggedIn"`
	Account        string `json:"account"` // 形如 ****1234
	Platform       string `json:"platform"`
	Source         string `json:"source"` // login | cookie | import | local | none
	CredentialPath string `json:"credentialPath"`
	SupportsDPAPI  bool   `json:"supportsDpapi"`
	Message        string `json:"message,omitempty"`
	SavedAt        string `json:"savedAt,omitempty"`
}

// LanzouOptions 是一次预检的参数。
type LanzouOptions struct {
	LocalDir      string `json:"localDir"`
	FolderID      string `json:"folderId"`
	FolderName    string `json:"folderName"`
	Workers       int    `json:"workers"`
	IntervalMS    int    `json:"intervalMs"`
	ForceReupload bool   `json:"forceReupload"`
}

// LanzouPrepareResult 是预检结果；此时还没有动任何本地文件。
type LanzouPrepareResult struct {
	BatchID      string        `json:"batchId"`
	FolderID     string        `json:"folderId"`
	FolderName   string        `json:"folderName"`
	LocalDir     string        `json:"localDir"`
	Total        int           `json:"total"`
	Uploadable   int           `json:"uploadable"`
	Duplicates   int           `json:"duplicates"`
	SkippedLarge int           `json:"skippedLarge"`
	Cached       int           `json:"cached"`
	Items        []lanzou.Item `json:"items"`
	Message      string        `json:"message,omitempty"`
}

// LanzouUploadState 是上传进度与最终结果；界面按 400ms 轮询。
type LanzouUploadState struct {
	BatchID    string          `json:"batchId"`
	Phase      string          `json:"phase"` // prepared | running | cancelling | done | failed
	FolderID   string          `json:"folderId"`
	FolderName string          `json:"folderName"`
	LocalDir   string          `json:"localDir"`
	Workers    int             `json:"workers"`
	IntervalMS int             `json:"intervalMs"`
	Total      int             `json:"total"`
	Done       int             `json:"done"`
	BytesSent  int64           `json:"bytesSent"`
	BytesTotal int64           `json:"bytesTotal"`
	Active     []string        `json:"active"`
	Summary    lanzou.Summary  `json:"summary"`
	Items      []lanzou.Item   `json:"items"`
	ReportJSON string          `json:"reportJson,omitempty"`
	ReportCSV  string          `json:"reportCsv,omitempty"`
	Error      string          `json:"error,omitempty"`
	StartedAt  string          `json:"startedAt,omitempty"`
	FinishedAt string          `json:"finishedAt,omitempty"`
}

type lanzouPrepare struct {
	mu         sync.Mutex
	id         string
	folderID   string
	folderName string
	localDir   string
	workers    int
	intervalMS int
	cancel     context.CancelFunc
	running    bool

	state lanzouUploadInternal
}

// lanzouUploadInternal 在 LanzouUploadState 之外多存几个只用于进度累加的字段。
type lanzouUploadInternal struct {
	LanzouUploadState
	// sent 记录每个文件已发送字节，用于增量累加 BytesSent。
	sent map[int]int64
}

func (p *lanzouPrepare) snapshot() LanzouUploadState {
	p.mu.Lock()
	defer p.mu.Unlock()
	st := p.state.LanzouUploadState
	st.Items = append([]lanzou.Item(nil), p.state.Items...)
	st.Active = append([]string(nil), p.state.Active...)
	return st
}

// LanzouStatus 返回当前登录态。
func (a *App) LanzouStatus() LanzouStatus {
	st := LanzouStatus{
		Platform:      lanzou.PlatformLanzou,
		Source:        "none",
		SupportsDPAPI: lanzou.SupportsProtection(),
	}
	if path, err := lanzou.CredentialPath(); err == nil {
		st.CredentialPath = path
	}
	a.lanzouMu.Lock()
	client := a.lanzouClient
	source := a.lanzouSource
	a.lanzouMu.Unlock()
	if client != nil {
		creds := client.Credentials()
		st.LoggedIn = creds.Valid()
		st.Account = creds.MaskedAccount()
		if source != "" {
			st.Source = source
		}
		if !creds.SavedAt.IsZero() {
			st.SavedAt = creds.SavedAt.Format(time.RFC3339)
		}
		return st
	}
	creds, err := lanzou.LoadCredentials()
	if err != nil {
		st.Message = "读取本机登录态失败：" + err.Error()
		return st
	}
	if creds == nil {
		return st
	}
	st.LoggedIn = true
	st.Account = creds.MaskedAccount()
	st.Source = "local"
	if !creds.SavedAt.IsZero() {
		st.SavedAt = creds.SavedAt.Format(time.RFC3339)
	}
	return st
}

// LanzouLogin 用账号密码登录，成功后加密保存 Cookie。
//
// 密码只用于这一次请求，不落盘、不写日志。
func (a *App) LanzouLogin(username, password string) (LanzouStatus, error) {
	interval := time.Duration(a.lanzouIntervalMS()) * time.Millisecond
	client := lanzou.NewClient(lanzou.Credentials{}, interval)
	creds, err := client.Login(a.context(), username, password)
	if err != nil {
		return a.LanzouStatus(), err
	}
	client.SetCredentials(creds)
	if err := lanzou.SaveCredentials(creds); err != nil {
		return a.LanzouStatus(), fmt.Errorf("登录成功，但保存登录态失败：%w", err)
	}
	a.setLanzouClient(client, "login")

	st := a.LanzouStatus()
	if err := client.Validate(a.context()); err != nil {
		st.Message = "登录成功，但校验目录失败（Cookie 可能仍可用于上传）：" + err.Error()
	}
	return st, nil
}

// LanzouUseCookie 用粘贴的 Cookie 登录（降级路径）。
func (a *App) LanzouUseCookie(raw string) (LanzouStatus, error) {
	creds, err := lanzou.ParseCookieHeader(raw)
	if err != nil {
		return a.LanzouStatus(), err
	}
	client := lanzou.NewClient(creds, time.Duration(a.lanzouIntervalMS())*time.Millisecond)
	if err := client.Validate(a.context()); err != nil {
		return a.LanzouStatus(), err
	}
	if err := lanzou.SaveCredentials(creds); err != nil {
		return a.LanzouStatus(), fmt.Errorf("Cookie 有效，但保存登录态失败：%w", err)
	}
	a.setLanzouClient(client, "cookie")
	return a.LanzouStatus(), nil
}

// LanzouImportCSharp 从 C# 版（%AppData%\FileDedupTool）导入登录态。
func (a *App) LanzouImportCSharp() (LanzouStatus, error) {
	creds, src, err := lanzou.ImportFromCSharp()
	if err != nil {
		return a.LanzouStatus(), err
	}
	if creds == nil {
		return a.LanzouStatus(), errors.New("未找到可导入的 C# 版登录态（%AppData%\\FileDedupTool）")
	}
	client := lanzou.NewClient(*creds, time.Duration(a.lanzouIntervalMS())*time.Millisecond)
	if err := lanzou.SaveCredentials(*creds); err != nil {
		return a.LanzouStatus(), fmt.Errorf("已从 %s 读到登录态，但保存失败：%w", src, err)
	}
	a.setLanzouClient(client, "import")
	st := a.LanzouStatus()
	st.Message = "已从 C# 版导入登录态：" + src
	return st, nil
}

// LanzouLogout 清除内存与本机保存的登录态。
func (a *App) LanzouLogout() LanzouStatus {
	a.lanzouMu.Lock()
	a.lanzouClient = nil
	a.lanzouSource = ""
	a.lanzouMu.Unlock()
	if err := lanzou.DeleteCredentials(); err != nil {
		st := a.LanzouStatus()
		st.Message = "已退出，但删除本机凭据失败：" + err.Error()
		return st
	}
	return a.LanzouStatus()
}

// LanzouBrowse 浏览云端目录；folderID 传 "0" 表示根目录。
func (a *App) LanzouBrowse(folderID string) (lanzou.Listing, error) {
	client, err := a.requireLanzouClient()
	if err != nil {
		return lanzou.Listing{}, err
	}
	return client.Listing(a.context(), folderID)
}

// LanzouPrepare 读本地目录与云端目录，做上传前预检。不会移动或上传任何文件。
func (a *App) LanzouPrepare(opts LanzouOptions) (LanzouPrepareResult, error) {
	localDir := strings.TrimSpace(opts.LocalDir)
	if localDir == "" {
		return LanzouPrepareResult{}, errors.New("请先选择本地目录")
	}
	info, err := os.Stat(localDir)
	if err != nil {
		return LanzouPrepareResult{}, fmt.Errorf("无法访问本地目录：%w", err)
	}
	if !info.IsDir() {
		return LanzouPrepareResult{}, fmt.Errorf("不是目录：%s", localDir)
	}
	workers, intervalMS := normalizeLanzouOptions(opts.Workers, opts.IntervalMS)

	client, err := a.requireLanzouClient()
	if err != nil {
		return LanzouPrepareResult{}, err
	}
	client.SetInterval(time.Duration(intervalMS) * time.Millisecond)

	folderID := strings.TrimSpace(opts.FolderID)
	if folderID == "" {
		folderID = "0"
	}
	listing, err := client.Listing(a.context(), folderID)
	if err != nil {
		return LanzouPrepareResult{}, err
	}

	entries, err := os.ReadDir(localDir)
	if err != nil {
		return LanzouPrepareResult{}, fmt.Errorf("读取本地目录失败：%w", err)
	}

	cache := lanzou.LoadUploadCache()
	items := make([]lanzou.Item, 0, len(entries))
	var sum lanzou.Summary
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		path := filepath.Join(localDir, e.Name())
		fi, err := e.Info()
		if err != nil {
			items = append(items, lanzou.Item{
				LocalPath: path, Name: e.Name(), Size: -1,
				Kind: lanzou.KindFailed, Error: "读取文件信息失败：" + err.Error(),
			})
			sum.Add(lanzou.KindFailed)
			continue
		}
		item := lanzou.Item{LocalPath: path, Name: e.Name(), Size: fi.Size(), Kind: lanzou.KindPending}

		switch {
		case fi.Size() > lanzou.MaxUploadSize:
			item.Kind = lanzou.KindSkippedLarge
		case !opts.ForceReupload && cacheHit(cache, folderID, path, fi.Size(), fi.ModTime()):
			// 缓存优先于云端判重：已经传过的文件不再移动，避免把本地文件挪走。
			item.Kind = lanzou.KindCached
		default:
			if dup, ok := lanzou.FindDuplicate(item.Name, item.Size, listing.Files); ok {
				item.Kind = lanzou.KindDuplicate
				item.DuplicateOf = dup.Name
				item.RemoteID = dup.ID
			}
		}
		items = append(items, item)
		if item.Kind != lanzou.KindPending {
			sum.Add(item.Kind)
		}
	}

	id := fmt.Sprintf("lanzou-%d", time.Now().UnixNano())
	p := &lanzouPrepare{
		id:         id,
		folderID:   folderID,
		folderName: strings.TrimSpace(opts.FolderName),
		localDir:   localDir,
		workers:    workers,
		intervalMS: intervalMS,
	}
	p.state.LanzouUploadState = LanzouUploadState{
		BatchID:    id,
		Phase:      "prepared",
		FolderID:   folderID,
		FolderName: p.folderName,
		LocalDir:   localDir,
		Workers:    workers,
		IntervalMS: intervalMS,
		Total:      len(items),
		Done:       sum.Total,
		Summary:    sum,
		Items:      append([]lanzou.Item(nil), items...),
	}
	p.state.sent = map[int]int64{}

	a.lanzouMu.Lock()
	a.lanzouPreps[id] = p
	a.lanzouMu.Unlock()

	result := LanzouPrepareResult{
		BatchID:    id,
		FolderID:   folderID,
		FolderName: p.folderName,
		LocalDir:   localDir,
		Total:      len(items),
		Items:      items,
	}
	for _, it := range items {
		switch it.Kind {
		case lanzou.KindPending:
			result.Uploadable++
		case lanzou.KindDuplicate:
			result.Duplicates++
		case lanzou.KindSkippedLarge:
			result.SkippedLarge++
		case lanzou.KindCached:
			result.Cached++
		}
	}
	result.Message = fmt.Sprintf(
		"预检完成：待上传 %d，云端重复 %d，超 100 MiB %d，本机缓存 %d。此步骤未改动任何本地文件。",
		result.Uploadable, result.Duplicates, result.SkippedLarge, result.Cached)
	return result, nil
}

// LanzouUploadState 返回预检或上传的进度快照。
func (a *App) LanzouUploadState(batchID string) (LanzouUploadState, error) {
	p, err := a.lanzouPrep(batchID)
	if err != nil {
		return LanzouUploadState{}, err
	}
	return p.snapshot(), nil
}

// LanzouStartUpload 开始上传。confirmed 必须为 true，且已同意免责声明。
func (a *App) LanzouStartUpload(batchID string, confirmed bool) error {
	if !confirmed {
		return errors.New("未确认，拒绝上传")
	}
	if err := requireLicense(auth.FeatureApply); err != nil {
		return err
	}
	settings, _ := LoadSettings()
	if !settings.Lanzou.DisclaimerOK {
		return errors.New("请先阅读并同意免责声明")
	}
	p, err := a.lanzouPrep(batchID)
	if err != nil {
		return err
	}
	client, err := a.requireLanzouClient()
	if err != nil {
		return err
	}

	p.mu.Lock()
	if p.running {
		p.mu.Unlock()
		return errors.New("该批次正在上传")
	}
	if p.state.Phase == "done" {
		p.mu.Unlock()
		return errors.New("该批次已结束，请重新预检")
	}
	ctx, cancel := context.WithCancel(context.Background())
	p.running = true
	p.cancel = cancel
	p.state.Phase = "running"
	p.state.Error = ""
	p.state.StartedAt = time.Now().Format(time.RFC3339)
	p.state.BytesSent = 0
	p.state.Active = nil
	p.state.sent = map[int]int64{}
	var bytesTotal int64
	for _, it := range p.state.Items {
		if it.Kind == lanzou.KindPending {
			bytesTotal += it.Size
		}
	}
	p.state.BytesTotal = bytesTotal
	folderID := p.folderID
	localDir := p.localDir
	folderName := p.folderName
	workers := p.workers
	p.mu.Unlock()

	client.SetInterval(time.Duration(p.intervalMS) * time.Millisecond)

	go func() {
		defer cancel()
		a.runLanzouUpload(ctx, p, client, folderID, folderName, localDir, workers)
	}()
	return nil
}

func (a *App) runLanzouUpload(ctx context.Context, p *lanzouPrepare, client *lanzou.Client, folderID, folderName, localDir string, workers int) {
	cache := lanzou.LoadUploadCache()

	// 第一步：处理云端重复项（只做本地移动，不联网）。
	duplicates := make([]int, 0)
	p.mu.Lock()
	for i, it := range p.state.Items {
		if it.Kind == lanzou.KindDuplicate {
			duplicates = append(duplicates, i)
		}
	}
	p.mu.Unlock()
	for _, i := range duplicates {
		p.mu.Lock()
		item := p.state.Items[i]
		p.mu.Unlock()
		moved, err := moveToDuplicateDir(localDir, item.LocalPath)
		if err != nil {
			p.finishItem(i, lanzou.Item{
				LocalPath: item.LocalPath, Name: item.Name, Size: item.Size,
				Kind: lanzou.KindFailed, DuplicateOf: item.DuplicateOf, RemoteID: item.RemoteID,
				Error: err.Error(),
			})
			continue
		}
		p.finishItem(i, lanzou.Item{
			LocalPath: item.LocalPath, Name: item.Name, Size: item.Size,
			Kind: lanzou.KindDuplicate, DuplicateOf: item.DuplicateOf, RemoteID: item.RemoteID,
			MovedTo: moved,
		})
	}

	// 第二步：并发上传待传项。
	jobs := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				p.uploadOne(ctx, client, folderID, cache, i)
			}
		}()
	}
dispatch:
	for i := range p.state.Items {
		p.mu.Lock()
		pending := p.state.Items[i].Kind == lanzou.KindPending
		p.mu.Unlock()
		if !pending {
			continue
		}
		select {
		case jobs <- i:
		case <-ctx.Done():
			break dispatch
		}
	}
	close(jobs)
	wg.Wait()

	// 第三步：没轮到的待传项记为已取消，然后落报告。
	p.mu.Lock()
	for i, it := range p.state.Items {
		if it.Kind != lanzou.KindPending {
			continue
		}
		p.state.Items[i].Kind = lanzou.KindCanceled
		p.state.Summary.Add(lanzou.KindCanceled)
		p.state.Done++
	}
	items := append([]lanzou.Item(nil), p.state.Items...)
	sum := p.state.Summary
	p.state.Phase = "done"
	p.state.FinishedAt = time.Now().Format(time.RFC3339)
	p.state.Active = nil
	p.running = false
	p.mu.Unlock()

	if err := cache.Save(); err != nil {
		log.Printf("蓝奏云上传缓存保存失败：%v", err)
	}

	report := lanzou.BuildReport(folderID, folderName, localDir, items, sum)
	jsonPath, csvPath, err := lanzou.WriteReport(localDir, report, time.Now())
	p.mu.Lock()
	if err != nil {
		p.state.Error = "上传已结束，但写报告失败：" + err.Error()
	} else {
		p.state.ReportJSON = jsonPath
		p.state.ReportCSV = csvPath
	}
	p.mu.Unlock()
}

func (p *lanzouPrepare) uploadOne(ctx context.Context, client *lanzou.Client, folderID string, cache *lanzou.UploadCache, i int) {
	p.mu.Lock()
	item := p.state.Items[i]
	size := item.Size
	modTime := int64(0)
	if info, err := os.Stat(item.LocalPath); err == nil {
		modTime = info.ModTime().UnixNano()
	}
	if p.state.Active == nil {
		p.state.Active = []string{}
	}
	p.state.Active = append(p.state.Active, item.Name)
	p.mu.Unlock()

	progress := func(sent, total int64) {
		p.mu.Lock()
		prev := p.state.sent[i]
		p.state.sent[i] = sent
		p.state.BytesSent += sent - prev
		p.mu.Unlock()
	}

	res, err := client.Upload(ctx, item.LocalPath, folderID, progress)

	p.mu.Lock()
	for idx, name := range p.state.Active {
		if name == item.Name {
			p.state.Active = append(p.state.Active[:idx], p.state.Active[idx+1:]...)
			break
		}
	}
	p.mu.Unlock()

	switch {
	case err == nil:
		kind := lanzou.KindSuccess
		if res.Instant {
			kind = lanzou.KindInstant
		}
		p.finishItem(i, lanzou.Item{
			LocalPath: item.LocalPath, Name: item.Name, Size: item.Size,
			Kind: kind, RemoteID: res.FileID,
		})
		if modTime > 0 {
			key := cache.Key(folderID, item.LocalPath, size, modTime)
			cache.Put(key, lanzou.CacheEntry{
				Path: item.LocalPath, FolderID: folderID, Size: size, ModTime: modTime,
				RemoteID: res.FileID, UploadedAt: time.Now(),
			})
		}
	case errors.Is(err, lanzou.ErrTooLarge):
		p.finishItem(i, lanzou.Item{
			LocalPath: item.LocalPath, Name: item.Name, Size: item.Size,
			Kind: lanzou.KindSkippedLarge, Error: err.Error(),
		})
	case ctx.Err() != nil:
		p.finishItem(i, lanzou.Item{
			LocalPath: item.LocalPath, Name: item.Name, Size: item.Size,
			Kind: lanzou.KindCanceled,
		})
	default:
		// 错误原文透传：HTTP 状态与 zt/info 都已包含在 err.Error() 里。
		p.finishItem(i, lanzou.Item{
			LocalPath: item.LocalPath, Name: item.Name, Size: item.Size,
			Kind: lanzou.KindFailed, Error: err.Error(),
		})
	}
}

func (p *lanzouPrepare) finishItem(i int, result lanzou.Item) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if i < 0 || i >= len(p.state.Items) {
		return
	}
	p.state.Items[i] = result
	p.state.Summary.Add(result.Kind)
	p.state.Done++
}

// LanzouCancelUpload 请求取消上传；未开始的待传项会记为已取消。
func (a *App) LanzouCancelUpload(batchID string) error {
	p, err := a.lanzouPrep(batchID)
	if err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cancel != nil {
		p.cancel()
	}
	if p.state.Phase == "running" {
		p.state.Phase = "cancelling"
	}
	return nil
}

// LanzouOpenWeb 在浏览器打开蓝奏云网页端；分享等操作请在网页端手动完成。
func (a *App) LanzouOpenWeb() error {
	if a.ctx == nil {
		return errors.New("应用尚未完成初始化")
	}
	wruntime.BrowserOpenURL(a.ctx, "https://pc.woozooo.com/mydisk.php")
	return nil
}

// LanzouOpenReport 打开本批次生成的报告文件。
func (a *App) LanzouOpenReport(batchID string) error {
	p, err := a.lanzouPrep(batchID)
	if err != nil {
		return err
	}
	p.mu.Lock()
	path := p.state.ReportJSON
	p.mu.Unlock()
	if path == "" {
		return errors.New("本批次还没有生成报告")
	}
	if a.ctx == nil {
		return errors.New("应用尚未完成初始化")
	}
	wruntime.BrowserOpenURL(a.ctx, path)
	return nil
}

// LanzouPickDirectory 选择待上传的本地目录。
func (a *App) LanzouPickDirectory() (string, error) {
	if a.ctx == nil {
		return "", errors.New("应用尚未完成初始化")
	}
	return wruntime.OpenDirectoryDialog(a.ctx, wruntime.OpenDialogOptions{
		Title: "选择要上传到蓝奏云的目录（只传该目录下的文件，不含子目录）",
	})
}

// LanzouAcceptDisclaimer 记录用户已阅读并同意免责声明。
func (a *App) LanzouAcceptDisclaimer() error {
	settings, _ := LoadSettings()
	settings.Lanzou.DisclaimerOK = true
	return SaveSettings(settings)
}

func (a *App) lanzouPrep(batchID string) (*lanzouPrepare, error) {
	a.lanzouMu.Lock()
	defer a.lanzouMu.Unlock()
	p, ok := a.lanzouPreps[batchID]
	if !ok {
		return nil, fmt.Errorf("找不到上传批次：%s", batchID)
	}
	return p, nil
}

func (a *App) lanzouIntervalMS() int {
	settings, _ := LoadSettings()
	return settings.Lanzou.IntervalMS
}

func (a *App) setLanzouClient(client *lanzou.Client, source string) {
	a.lanzouMu.Lock()
	a.lanzouClient = client
	a.lanzouSource = source
	a.lanzouMu.Unlock()
}

func (a *App) requireLanzouClient() (*lanzou.Client, error) {
	settings, _ := LoadSettings()
	interval := time.Duration(settings.Lanzou.IntervalMS) * time.Millisecond

	a.lanzouMu.Lock()
	client := a.lanzouClient
	a.lanzouMu.Unlock()
	if client != nil {
		return client, nil
	}

	creds, err := lanzou.LoadCredentials()
	if err != nil {
		return nil, fmt.Errorf("读取本机登录态失败：%w", err)
	}
	if creds == nil {
		return nil, errors.New("尚未登录蓝奏云，请先登录或粘贴 Cookie")
	}
	client = lanzou.NewClient(*creds, interval)

	a.lanzouMu.Lock()
	if a.lanzouClient == nil {
		a.lanzouClient = client
		a.lanzouSource = "local"
	}
	client = a.lanzouClient
	a.lanzouMu.Unlock()
	return client, nil
}

func (a *App) context() context.Context {
	if a.ctx == nil {
		return context.Background()
	}
	return a.ctx
}

func normalizeLanzouOptions(workers, intervalMS int) (int, int) {
	if workers <= 0 {
		workers = 3
	}
	if workers > 5 {
		workers = 5
	}
	if intervalMS <= 0 {
		intervalMS = 1500
	}
	if intervalMS < 1000 {
		intervalMS = 1000
	}
	if intervalMS > 2000 {
		intervalMS = 2000
	}
	return workers, intervalMS
}

func cacheHit(cache *lanzou.UploadCache, folderID, path string, size int64, mod time.Time) bool {
	if cache == nil {
		return false
	}
	_, ok := cache.Hit(cache.Key(folderID, path, size, mod.UnixNano()))
	return ok
}

// moveToDuplicateDir 把本地重复文件移动到上传目录旁边的「网盘重复文件」。
// 目标已存在时追加序号，绝不覆盖。
func moveToDuplicateDir(localDir, path string) (string, error) {
	cleanLocal := filepath.Clean(localDir)
	dupDir := filepath.Join(filepath.Dir(cleanLocal), duplicateDirName)
	if err := os.MkdirAll(dupDir, 0o755); err != nil {
		return "", err
	}
	target := uniquePath(filepath.Join(dupDir, filepath.Base(path)))
	if err := os.Rename(path, target); err != nil {
		return "", err
	}
	return target, nil
}

func uniquePath(path string) string {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return path
	}
	ext := filepath.Ext(path)
	base := strings.TrimSuffix(path, ext)
	for i := 1; i < 10000; i++ {
		candidate := fmt.Sprintf("%s (%d)%s", base, i, ext)
		if _, err := os.Stat(candidate); errors.Is(err, os.ErrNotExist) {
			return candidate
		}
	}
	return path
}

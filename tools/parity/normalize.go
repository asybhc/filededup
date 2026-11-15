package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"filededup/internal/engine/model"
	"golang.org/x/text/unicode/norm"
)

func loadScan(path string) (*model.Result, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var r model.Result
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if r.SchemaVersion != model.SchemaVersion {
		return nil, fmt.Errorf("%s: schemaVersion=%d，期望 %d", path, r.SchemaVersion, model.SchemaVersion)
	}
	return &r, nil
}

func loadPlan(path string) (*model.Plan, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var p model.Plan
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if p.SchemaVersion != model.SchemaVersion {
		return nil, fmt.Errorf("%s: schemaVersion=%d，期望 %d", path, p.SchemaVersion, model.SchemaVersion)
	}
	return &p, nil
}

// relPath 把绝对路径转为相对语料根的 "/" 分隔 NFC 路径；无法相对化时原样返回。
// 统一先转成 "/" 形式再比较：对拍夹具里会出现 F:\corpus 这类 Windows 路径，
// 在 Linux/macOS 上 filepath.Rel 不认反斜杠，会把两侧都当普通文件名而无法相对化。
func relPath(p, root string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	slash := func(s string) string {
		return strings.ReplaceAll(filepath.ToSlash(s), `\`, "/")
	}
	p = slash(p)
	if root != "" {
		r := strings.TrimRight(slash(strings.TrimSpace(root)), "/")
		if r != "" && len(p) > len(r) && strings.HasPrefix(p, r+"/") {
			p = p[len(r)+1:]
		}
	}
	return norm.NFC.String(p)
}

type normMember struct {
	Path        string
	Size        int64
	ModTimeNS   int64
	ModTimeRaw  string
	Hash        string
	Fingerprint string
	DurationMS  int64
	Keep        bool
	Deletable   bool
}

type normGroup struct {
	Kind        model.GroupKind
	Key         string
	Keep        string
	Reclaimable int64
	Members     []normMember
}

type normScan struct {
	Version int
	Stats   model.Stats
	Groups  []normGroup
	Skipped []model.Skip
}

func normTime(s string) (int64, string) {
	if s == "" {
		return 0, ""
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return 0, s
	}
	return t.UTC().UnixNano(), ""
}

func normalizeScan(r *model.Result, root string) normScan {
	n := normScan{Version: r.SchemaVersion, Stats: r.Stats}
	for _, g := range r.Groups {
		ng := normGroup{
			Kind:        g.Kind,
			Key:         g.Key,
			Keep:        relPath(g.Keep, root),
			Reclaimable: g.Reclaimable,
		}
		for _, m := range g.Members {
			ns, raw := normTime(m.ModTime)
			ng.Members = append(ng.Members, normMember{
				Path:        relPath(m.Path, root),
				Size:        m.Size,
				ModTimeNS:   ns,
				ModTimeRaw:  raw,
				Hash:        m.Hash,
				Fingerprint: m.Fingerprint,
				DurationMS:  m.DurationMS,
				Keep:        m.Keep,
				Deletable:   m.Deletable,
			})
		}
		sort.Slice(ng.Members, func(i, j int) bool { return ng.Members[i].Path < ng.Members[j].Path })
		n.Groups = append(n.Groups, ng)
	}
	sort.SliceStable(n.Groups, func(i, j int) bool {
		if n.Groups[i].Kind != n.Groups[j].Kind {
			return n.Groups[i].Kind < n.Groups[j].Kind
		}
		return n.Groups[i].Key < n.Groups[j].Key
	})
	for _, s := range r.Skipped {
		n.Skipped = append(n.Skipped, model.Skip{Path: relPath(s.Path, root), Code: s.Code, Message: s.Message})
	}
	sort.SliceStable(n.Skipped, func(i, j int) bool { return n.Skipped[i].Path < n.Skipped[j].Path })
	return n
}

type normPlan struct {
	Version   int
	RequestID string
	Source    model.PlanSource
	Summary   model.PlanSummary
	Actions   []model.Action
}

func normalizePlan(p *model.Plan, root string) normPlan {
	n := normPlan{
		Version:   p.SchemaVersion,
		RequestID: p.RequestID,
		Source:    p.Source,
		Summary:   p.Summary,
	}
	for _, a := range p.Actions {
		a.Src = relPath(a.Src, root)
		if a.Dst != "" {
			a.Dst = relPath(a.Dst, root)
		}
		n.Actions = append(n.Actions, a)
	}
	return n
}

// commonDirOf 在未显式指定 root 时，用所有动作路径的最长公共目录近似根。
func commonDirOf(actions []model.Action) string {
	var dirs []string
	for _, a := range actions {
		p := a.Src
		if a.Dst != "" {
			p = filepath.Dir(a.Dst)
		}
		dirs = append(dirs, filepath.Dir(p))
	}
	if len(dirs) == 0 {
		return ""
	}
	common := dirs[0]
	for _, d := range dirs[1:] {
		for !strings.HasPrefix(strings.ToLower(d), strings.ToLower(common)+string(filepath.Separator)) && d != common {
			parent := filepath.Dir(common)
			if parent == common {
				return common
			}
			common = parent
		}
	}
	return common
}

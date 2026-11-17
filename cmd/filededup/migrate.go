//go:build desktop

package main

// 品牌改名后的数据迁移：把旧 %AppData% 目录复制到新目录，旧目录保留以便回滚。
//
// 约定：
//   - 仅当新目录不存在时执行；新目录已存在则完全不覆盖。
//   - 复制而非移动，旧目录原样保留。
//   - 任何失败只记录日志，不阻塞启动。
//   - 迁移来源、文件/目录条目数与失败明细写入 <新目录>/logs/migration.log。

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"filededup/internal/brand"
)

// migrationResult 是单个旧目录的迁移结果。
type migrationResult struct {
	Source  string
	Files   int
	Dirs    int
	Skipped int
	Failed  []string
}

// migrateLegacyData 在启动早期调用；失败时只写日志，不影响后续启动流程。
func migrateLegacyData() {
	newDir, err := brand.DataDir()
	if err != nil {
		logMigrationFailure("", newDir, fmt.Sprintf("无法定位新数据目录：%v", err))
		return
	}
	configBase, err := os.UserConfigDir()
	if err != nil {
		logMigrationFailure("", newDir, fmt.Sprintf("无法定位用户配置目录：%v", err))
		return
	}

	results := migrateDataDirs(configBase, newDir)
	if len(results) == 0 {
		return
	}
	writeMigrationLog(newDir, results)
}

// migrateDataDirs 是迁移的可测试入口：新目录已存在时直接返回空结果，不做任何覆盖。
func migrateDataDirs(configBase, newDir string) []migrationResult {
	if info, err := os.Stat(newDir); err == nil {
		if info.IsDir() {
			return nil
		}
		logMigrationFailure("", newDir, "新数据目录路径已被同名文件占用，跳过迁移")
		return nil
	}
	sources := []string{
		filepath.Join(configBase, brand.LegacyDataDirName),
		filepath.Join(configBase, brand.LegacyToolDirName),
	}
	var results []migrationResult
	for _, src := range sources {
		info, err := os.Stat(src)
		if err != nil || !info.IsDir() {
			continue
		}
		results = append(results, copyLegacyDir(src, newDir))
	}
	return results
}

// copyLegacyDir 把 src 的内容递归复制进 dst；目标已存在的条目跳过，不覆盖。
func copyLegacyDir(src, dst string) migrationResult {
	res := migrationResult{Source: src}
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			res.Failed = append(res.Failed, fmt.Sprintf("%s：%v", path, walkErr))
			return nil
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			res.Failed = append(res.Failed, fmt.Sprintf("%s：%v", path, err))
			return nil
		}
		target := filepath.Join(dst, rel)

		if d.IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				res.Failed = append(res.Failed, fmt.Sprintf("%s：%v", rel, err))
				return nil
			}
			if rel != "." {
				res.Dirs++
				if err := os.Chmod(target, 0o755); err != nil {
					// 目录权限失败不影响文件复制，但如实记一条。
					res.Failed = append(res.Failed, fmt.Sprintf("%s：%v", rel, err))
				}
			}
			return nil
		}

		if _, err := os.Lstat(target); err == nil {
			res.Skipped++
			return nil
		}
		if !d.Type().IsRegular() {
			res.Failed = append(res.Failed, fmt.Sprintf("%s：不支持的文件类型", rel))
			return nil
		}
		if err := copyFile(path, target); err != nil {
			res.Failed = append(res.Failed, fmt.Sprintf("%s：%v", rel, err))
			return nil
		}
		res.Files++
		return nil
	})
	if err != nil {
		res.Failed = append(res.Failed, fmt.Sprintf("%s：%v", src, err))
	}
	return res
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		_ = os.Remove(dst)
		return err
	}
	return out.Close()
}

// writeMigrationLog 把每个来源的结果追加到 <新目录>/logs/migration.log。
func writeMigrationLog(newDir string, results []migrationResult) {
	logDir := filepath.Join(newDir, "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		logMigrationFailure("", newDir, fmt.Sprintf("无法创建日志目录：%v", err))
		return
	}
	path := filepath.Join(logDir, "migration.log")
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		logMigrationFailure("", newDir, fmt.Sprintf("无法写入迁移日志：%v", err))
		return
	}
	defer f.Close()
	ts := time.Now().Format(time.RFC3339)
	for _, r := range results {
		line := fmt.Sprintf("%s 数据迁移：来源 %s → 目标 %s；复制文件 %d、目录 %d；跳过已存在 %d；失败 %d",
			ts, r.Source, newDir, r.Files, r.Dirs, r.Skipped, len(r.Failed))
		if len(r.Failed) > 0 {
			line += "（" + strings.Join(r.Failed, "；") + "）"
		}
		_, _ = fmt.Fprintln(f, line)
	}
}

// logMigrationFailure 在无法写入新目录日志时，退回到标准日志，保证原因可见。
func logMigrationFailure(source, newDir, reason string) {
	fmt.Fprintf(os.Stderr, "%s 数据迁移失败：来源 %s → 目标 %s；%s\n",
		time.Now().Format(time.RFC3339), source, newDir, reason)
}

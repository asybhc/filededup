package main

import (
	"encoding/binary"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
)

// GenerateScale 生成规模压测语料：n 个文件铺到 dNNN/sNN 两级目录。
// 内容按索引确定性生成；每 10 个文件里有一对完全重复，便于验证分组与去重统计。
// 只用于性能/内存验收，不产出 manifest，也不参与 golden 哈希。
func GenerateScale(outDir string, n int, seed int64) error {
	if n <= 0 {
		return fmt.Errorf("scale 必须为正数：%d", n)
	}
	if err := prepareDir(outDir); err != nil {
		return err
	}
	const perDir = 1000
	var prev []byte
	for i := 0; i < n; i++ {
		rel := filepath.Join(
			fmt.Sprintf("d%03d", i/perDir),
			fmt.Sprintf("s%02d", (i%perDir)/100),
			fmt.Sprintf("f%06d.bin", i),
		)
		var data []byte
		if i%10 == 1 && prev != nil {
			data = prev
		} else {
			data = scaleData(seed, i, 256+(i%8)*128)
		}
		prev = data
		full := filepath.Join(outDir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(full, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// scaleData 前 8 字节写索引，其余用索引派生的伪随机字节填充。
func scaleData(seed int64, i, size int) []byte {
	r := rand.New(rand.NewSource(seed + int64(i)))
	b := make([]byte, size)
	binary.LittleEndian.PutUint64(b[:8], uint64(i))
	r.Read(b[8:])
	return b
}

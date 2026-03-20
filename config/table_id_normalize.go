package config

import "strings"

// NormalizeAutoGenerateIDFromStrategy 根据 id_strategy 同步 auto_generate_id，二者语义必须一致。
//
// 规则：
//   - id_strategy = user_provided → auto_generate_id = false（必须由客户端提供 ID）
//   - id_strategy 为 uuid / snowflake / custom / 空字符串 / 其它扩展名 → auto_generate_id = true
//     （未提供 ID 时由服务端生成；与 WriteData 中系统分配类策略一致）
//
// 应在持久化前、以及从存储读出后调用，以修复历史错误配置。
func NormalizeAutoGenerateIDFromStrategy(tc *TableConfig) {
	if tc == nil {
		return
	}
	switch strings.ToLower(strings.TrimSpace(tc.IDStrategy)) {
	case "user_provided":
		tc.AutoGenerateID = false
	default:
		tc.AutoGenerateID = true
	}
}

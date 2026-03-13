// Package version 提供版本号管理功能
// 版本号通过 -ldflags 在编译时注入
package version

import (
	"fmt"
	"runtime"
)

var (
	// version 由 -ldflags 注入，格式: -X 'minIODB/pkg/version.version=v1.0.0'
	version   = "dev"
	gitCommit = "unknown"
	buildTime = "unknown"
)

// Info 包含版本相关信息
type Info struct {
	Version   string `json:"version"`
	GitCommit string `json:"git_commit"`
	BuildTime string `json:"build_time"`
	GoVersion string `json:"go_version"`
	Platform  string `json:"platform"`
}

// Get 返回当前版本号
func Get() string {
	return version
}

// GetInfo 返回完整的版本信息
func GetInfo() Info {
	return Info{
		Version:   version,
		GitCommit: gitCommit,
		BuildTime: buildTime,
		GoVersion: runtime.Version(),
		Platform:  fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH),
	}
}

// String 返回版本信息的字符串表示
func String() string {
	return fmt.Sprintf("MinIODB %s (commit: %s, built: %s, %s)",
		version, gitCommit, buildTime, runtime.Version())
}

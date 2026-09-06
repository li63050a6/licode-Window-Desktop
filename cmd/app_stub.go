//go:build !windows

package cmd

import (
	"errors"

	"github.com/spf13/cobra"
)

// NewAppCommand 桌面窗口模式依赖 WebView2，仅 Windows 提供，其余平台给出提示。
func NewAppCommand() *cobra.Command {
	return &cobra.Command{
		Use:    "app",
		Short:  "桌面窗口模式（仅 Windows 可用）",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return errors.New("桌面窗口模式仅支持 Windows（依赖 WebView2），其他平台请使用 web 命令")
		},
	}
}

// MaybeDefaultApp 仅 Windows 需要（GUI 子系统无控制台时改跑 app）。
func MaybeDefaultApp() {}

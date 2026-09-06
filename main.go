package main

import (
	"os"

	"github.com/spf13/cobra"

	"licode/cmd"
)

func main() {
	// 允许从资源管理器双击启动：cobra 默认的 mousetrap 会在检测到 Explorer
	// 启动时打印提示并退出，导致桌面窗口版（-H windowsgui）双击无任何反应。
	cobra.MousetrapHelpText = ""
	// licode 直接运行即启动 TUI 终端界面，加 web 参数启动 Web 服务器，加 app 参数以桌面窗口运行 Web 界面。
	// 双击 exe（无控制台、无参数）时自动改跑桌面窗口模式。
	cmd.MaybeDefaultApp()
	root := cmd.NewTUICommand()
	root.AddCommand(cmd.NewServeCommand())
	root.AddCommand(cmd.NewAppCommand())
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}
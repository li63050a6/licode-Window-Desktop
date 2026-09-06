//go:build windows

package cmd

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"
	"unsafe"

	"github.com/jchv/go-webview2"
	"github.com/spf13/cobra"
	"golang.org/x/sys/windows"

	"licode/internal/settings"
)

var procGetConsoleWindow = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetConsoleWindow")

var (
	procSetProcessDpiAwarenessContext = windows.NewLazySystemDLL("user32.dll").NewProc("SetProcessDpiAwarenessContext")
	procSetProcessDPIAware            = windows.NewLazySystemDLL("user32.dll").NewProc("SetProcessDPIAware")
	procSystemParametersInfoW         = windows.NewLazySystemDLL("user32.dll").NewProc("SystemParametersInfoW")
)

// DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 = (HANDLE)-4
const dpiAwarenessContextPerMonitorAwareV2 = ^uintptr(3)

// SPI_GETWORKAREA：获取主屏工作区（去除任务栏）。
const spIGetWorkArea = 0x0030

type winRect struct {
	Left, Top, Right, Bottom int32
}

// MaybeDefaultApp 处理双击启动：无参数且无控制台（-H windowsgui 构建）时，
// cobra 默认执行根命令 TUI，而 TUI 在无控制台进程内无法运行，故改跑桌面窗口模式。
func MaybeDefaultApp() {
	if len(os.Args) > 1 {
		return
	}
	h, _, _ := procGetConsoleWindow.Call()
	if h != 0 {
		return
	}
	os.Args = append(os.Args, "app")
}

// NewAppCommand 返回桌面窗口命令：本地服务器 + WebView2 原生窗口，无需浏览器。
func NewAppCommand() *cobra.Command {
	opts := &ServeOptions{}
	c := &cobra.Command{
		Use:   "app",
		Short: "AI 编程助手（桌面窗口，无需浏览器）",
		Long: `licode app —— 以桌面应用窗口方式运行 Web 界面。

内部启动仅监听本机的服务器，并用系统自带的 WebView2 渲染原生窗口。
双击 exe 即可使用，无需打开浏览器；关闭窗口即优雅退出。

参数与 web 命令一致；端口默认自动选择空闲端口，避免冲突。`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runApp(cmd, opts)
		},
	}
	f := c.Flags()
	f.StringVar(&opts.Host, "host", "127.0.0.1", "监听主机（桌面模式仅限本机，保持默认）")
	f.IntVar(&opts.Port, "port", 0, "监听端口（0 = 自动选择空闲端口）")
	f.BoolVar(&opts.NoSubAgents, "no-subagents", false, "禁用子代理编排")
	f.StringVarP(&opts.ConfigPath, "config", "c", "", "配置文件路径（默认 ~/.licode/config.toml）")
	return c
}

func runApp(cmd *cobra.Command, opts *ServeOptions) error {
	if err := loadServeConfig(cmd, opts); err != nil {
		return err
	}
	// 桌面模式仅监听本机，避免意外暴露到局域网
	opts.Host = "127.0.0.1"
	// 自动选择空闲端口，避免与其他程序冲突
	if opts.Port == 0 {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return fmt.Errorf("选择端口失败: %w", err)
		}
		opts.Port = l.Addr().(*net.TCPAddr).Port
		_ = l.Close()
	}
	url := fmt.Sprintf("http://127.0.0.1:%d/", opts.Port)

	serveDone := make(chan struct{})
	go func() {
		defer close(serveDone)
		if err := runServe(opts); err != nil {
			fmt.Fprintln(os.Stderr, "服务器退出:", err)
		}
	}()

	// 等待服务器就绪后再打开窗口
	if err := waitServerReady(fmt.Sprintf("http://127.0.0.1:%d/health", opts.Port), 20*time.Second); err != nil {
		TriggerAppShutdown()
		<-serveDone
		return err
	}

	// 声明 Per-Monitor V2 DPI 感知：高 DPI 屏上渲染清晰，窗口坐标按真实像素计算。
	// 未声明时进程被 DPI 虚拟化（位图拉伸），小窗口也会被放大到超出屏幕。
	if ok, _, _ := procSetProcessDpiAwarenessContext.Call(dpiAwarenessContextPerMonitorAwareV2); ok == 0 {
		_, _, _ = procSetProcessDPIAware.Call() // 老系统回退到系统级 DPI 感知
	}

	// 窗口尺寸自适应屏幕工作区（不超过 90%）：默认 1360x880 在小屏/缩放屏上
	// 可能超出屏幕，居中后四条缩放边框都落在屏幕外，导致无法拖拽调整大小。
	winW, winH := 1360, 880
	var wa winRect
	if ok, _, _ := procSystemParametersInfoW.Call(spIGetWorkArea, 0, uintptr(unsafe.Pointer(&wa)), 0); ok != 0 {
		if mw := int(wa.Right-wa.Left) * 9 / 10; winW > mw {
			winW = mw
		}
		if mh := int(wa.Bottom-wa.Top) * 9 / 10; winH > mh {
			winH = mh
		}
	}

	w := webview2.NewWithOptions(webview2.WebViewOptions{
		Debug:     false,
		AutoFocus: true,
		DataPath:  filepath.Join(settings.CacheDir(), "webview"),
		WindowOptions: webview2.WindowOptions{
			Title:  "licode —— AI 编程助手",
			Width:  uint(winW),
			Height: uint(winH),
			Center: true,
		},
	})
	if w == nil {
		TriggerAppShutdown()
		<-serveDone
		return fmt.Errorf("未检测到 WebView2 运行时，请安装后重试: https://developer.microsoft.com/microsoft-edge/webview2/")
	}
	defer w.Destroy()
	// 最小尺寸 320x240（HintMin 仅记录下限，不改变当前大小，并保留缩放边框样式）
	w.SetSize(320, 240, webview2.HintMin)
	w.Navigate(url)
	log.Printf("桌面窗口已打开: %s", url)
	w.Run() // 阻塞直至窗口关闭

	// 窗口已关闭：优雅关停服务器后退出
	TriggerAppShutdown()
	select {
	case <-serveDone:
	case <-time.After(3 * time.Second):
	}
	return nil
}

// waitServerReady 轮询健康检查端点直至本地服务器就绪。
func waitServerReady(url string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 2 * time.Second}
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			_ = resp.Body.Close()
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("等待本地服务器就绪超时")
}

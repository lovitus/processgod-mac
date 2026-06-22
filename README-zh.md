# ProcessGodMac

这是从 `lovitus/processgod` 重构而来的 macOS 原生进程守护工具。

`v0.4.0-dev` 使用 Swift 6 + SwiftUI/AppKit 实现菜单栏和管理界面，Go 继续负责进程守护、Cron、内存日志、配置和 CLI。

## 系统要求

- macOS 15 或更高版本
- 仅支持 Apple Silicon `arm64`
- 不再包含网页 Dashboard，也不监听 `51089/51090`

## 使用方式

1. 打开 `processgod-mac-0.4.0-dev.dmg`。
2. 将 `ProcessGodMac.app` 拖入 Applications。
3. 启动后点击菜单栏图标。

正式公证的 DMG 不需要执行 `xattr`。首次启动会自动注册用户级服务，登录后自动运行。退出 Swift 菜单栏 App 不会停止 Go 守护进程。

需要登录前启动时，在“设置”中切换到“系统级”。此操作仅允许管理员账户，macOS 可能要求在“系统设置 > 通用 > 登录项与扩展”中批准。目标系统 daemon 完成配置导入和健康检查之前，用户 daemon 不会被卸载。

## 原生界面

- 菜单栏 Popover：查看状态、启停单项、立即重启、日志速览、暂停/恢复全部任务
- 管理窗口：左侧进程列表，右侧新增/编辑器
- 编辑器：命令选择、参数、工作目录、环境变量、运行模式和 Cron 校验
- 日志窗口：错误/警告与标准/其他分开显示，并显示当前行数和容量
- 设置：用户级/系统级服务、PATH 修改/保存/取消、English/简体中文

## 内存日志上限

每个已启用任务只有两个真实内存环形缓冲区：

- 错误/警告：最近 100 行
- 标准/其他：最近 20 行
- 每行最多 4096 字节

因此每个任务保留的日志文本最多 491,520 字节，外加固定的 Go 对象和字符串开销。界面和 CLI 读取的就是这两个缓冲区，不存在另一个隐藏的大缓存，也不会写任务日志文件。

日志序号使用 64 位有符号整数。达到理论最大值 `9,223,372,036,854,775,807` 后从 1 重新开始；不会增加内存占用，也不会改变环形容量。

## 构建和测试

```bash
make test
make build VERSION=0.4.0 CHANNEL=dev
```

Xcode 工程：`macos/ProcessGodMac.xcodeproj`。

## CLI

```bash
/Applications/ProcessGodMac.app/Contents/MacOS/processgod-mac status
/Applications/ProcessGodMac.app/Contents/MacOS/processgod-mac logs <任务ID>
/Applications/ProcessGodMac.app/Contents/MacOS/processgod-mac restart <任务ID>
/Applications/ProcessGodMac.app/Contents/MacOS/processgod-mac pause
/Applications/ProcessGodMac.app/Contents/MacOS/processgod-mac resume
```

连接系统 daemon 时添加 `--system`。

更多文档见 [README](README.md) 中的 Documentation 列表。

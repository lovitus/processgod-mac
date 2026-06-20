# ProcessGod macOS 版本

这是 `lovitus/processgod` 的 macOS 重写版（Go 实现）。

主要能力：

- launchd 服务模式（LaunchAgent / LaunchDaemon）
- `--system` 模式支持开机启动（登录前）
- 进程守护与自动拉起
- cron 定时触发重启/执行
- 进程输出内存环形缓存（不落盘）
- CLI 查看状态、拉取日志、热重载配置

## 构建

```bash
mkdir -p /tmp/gocache /tmp/gomodcache
GOCACHE=/tmp/gocache GOMODCACHE=/tmp/gomodcache go build -o dist/processgod-mac ./cmd/processgod
```

## 运行守护进程

```bash
./dist/processgod-mac daemon
```

从 Finder 双击 `ProcessGodMac.app` 时：

- 会创建菜单栏托盘图标（`PG`）
- 自动启动守护进程
- 启动后只驻留菜单栏，不再自动弹出浏览器
- 每个进程直接显示在托盘菜单中，可查看状态/PID、启停守护、立即重启、查看日志、编辑和删除
- 托盘内可切换用户级启动（登录后）和系统级启动（登录前，需要管理员授权）
- 只有点击“新增进程”或“管理进程”时才打开管理页面
- 管理页面采用类似原版 ProcessGuard 的左侧进程列表 + 右侧单一编辑器，不再堆叠多个表单
- 网页支持 English / 简体中文，首次访问自动读取浏览器语言，手动选择后会保持
- 支持直接填写命令名（例如 `ping`、`node`、`java`），无需绝对路径
- 运行模式可直接选择：持续守护、仅运行一次、Cron 启动、Cron 重启

日志保留策略：

- 任务 stdout/stderr 仅保存在内存
- 每个任务分为 2 个内存环形缓冲：
- `error_warning` 保留最近 `100` 行
- `standard_other` 保留最近 `20` 行
- 每行最多缓存 `4096` 字节
- 日志输出带行号（`E#<n>` / `S#<n>`）和缓冲区统计
- 超出后按环形覆盖，不写入磁盘

## 服务模式

用户级（登录后启动）：

```bash
./dist/processgod-mac service install
```

系统级（开机启动，需要 sudo）：

```bash
sudo ./dist/processgod-mac service install --system
```

`system` 模式才是登录前启动。

## 配置文件

```bash
./dist/processgod-mac config path
./dist/processgod-mac config sample
./dist/processgod-mac config validate
./dist/processgod-mac dashboard
```

补充文档：

- [User Guide](docs/USER_GUIDE.md)
- [Operations](docs/OPERATIONS.md)

默认路径：`~/Library/Application Support/ProcessGodMac/config.json`

如在沙箱/受限环境运行，可设置：

```bash
export PROCESSGOD_HOME=/path/to/runtime-dir
```

## DMG 打包

```bash
./scripts/package-dmg.sh 0.1.0 dev
```

输出示例：`processgod-mac-0.1.0-dev.dmg`

DMG 内包含已自签名的应用和 `Applications` 快捷入口，可直接拖入安装。

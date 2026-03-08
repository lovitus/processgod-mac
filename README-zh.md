# ProcessGod macOS 版本

这是 `lovitus/processgod` 的 macOS 重写版（Go 实现）。

主要能力：

- launchd 服务模式（LaunchAgent / LaunchDaemon）
- `--system` 模式支持开机启动
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
- 可在托盘菜单执行 `Start/Stop/Reload/Show Status/Open Dashboard/Open Config/Quit`
- 会自动打开 Dashboard，可完整管理配置（新增/编辑/删除/启停单项/查看日志）

## 服务模式

用户级（登录后启动）：

```bash
./dist/processgod-mac service install
```

系统级（开机启动，需要 sudo）：

```bash
sudo ./dist/processgod-mac service install --system
```

## 配置文件

```bash
./dist/processgod-mac config path
./dist/processgod-mac config sample
./dist/processgod-mac config validate
./dist/processgod-mac dashboard
```

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

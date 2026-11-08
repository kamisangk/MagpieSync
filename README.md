<p align="center"><img src="internal/httpapi/web/brand-logo.png" alt="MagpieSync" width="128"></p>

<h1 align="center">MagpieSync</h1>

<p align="center"><strong>把视频收藏同步到自己的资料库，并按需保存为本地媒体。</strong></p>

<p align="center">
  <img src="https://img.shields.io/badge/Go-1.26.1%2B-00ADD8?logo=go&logoColor=white" alt="Go">
  <img src="https://img.shields.io/badge/License-Apache--2.0-blue.svg" alt="License">
  <img src="https://img.shields.io/badge/Storage-SQLite-003B57?logo=sqlite&logoColor=white" alt="Storage">
</p>

MagpieSync 是一个自托管、单机运行的视频收藏备份工具。它将支持站点的收藏来源只读同步到本地 SQLite，提供搜索、同步记录、数据导出和下载队列，并内置 Web 界面。

## 功能

- 通过浏览器访问本机 Web UI；首次运行时设置本地访问密码。
- 在同一资料库中查看来源、视频、同步状态和下载状态，支持搜索与筛选。
- 支持多账户管理、账户切换及每账户代理设置。
- 支持后台同步、同步进度、同步历史和跨账户 JSON / CSV 导出。
- 提供下载队列、进度、暂停、继续、重试和取消；文件命名与媒体目录可在设置中调整。
- 登录凭据与代理配置使用 AES-256-GCM 加密后保存在本地；站点插件和共享同步层分离。

## 快速开始

### 从源码运行

需要 Go 1.26.1 或更新版本。

```bash
go build -trimpath -o bin/magpiesync ./cmd/magpiesync
./bin/magpiesync
```

Windows PowerShell：

```powershell
go build -trimpath -o bin/magpiesync.exe ./cmd/magpiesync
.\bin\magpiesync.exe
```

打开 <http://127.0.0.1:8787>，按页面提示设置本地密码，然后在“账户与站点”中连接哔哩哔哩账户。扫码登录使用手机客户端确认；也可以粘贴已登录浏览器中的 Cookie。

首次启动会在 `./data` 创建 SQLite 数据库和加密密钥。下载媒体默认写入 `./media`，可以在 Web UI 的设置中更改。

### Docker

```bash
docker build -t magpiesync .
docker run -d --name magpiesync \
  -p 127.0.0.1:8787:8787 \
  -v magpiesync-data:/data \
  magpiesync
```

浏览 <http://127.0.0.1:8787> 完成首次密码设置。容器以非 root 用户运行，镜像包含 FFmpeg、CA 证书和时区数据。持久化 `/data` 卷以保留数据库和加密密钥。

### GitHub Releases

将 `vMAJOR.MINOR.PATCH` 格式的 tag 推送到 GitHub 后，Actions 会运行测试并创建 Release，附上 Linux amd64/arm64、Windows amd64 和 fnOS x86/ARM FPK。比如：

```bash
git tag v0.10.3
git push origin v0.10.3
```

## 配置

命令行参数优先于对应环境变量。

| 参数 | 环境变量 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `-listen` | `MAGPIESYNC_LISTEN` | `127.0.0.1:8787` | Web UI 和 API 监听地址 |
| `-data-dir` | `MAGPIESYNC_DATA_DIR` | `./data` | 数据库和密钥目录 |
| `-rate` | `MAGPIESYNC_RATE` | `1.5` | 每个站点客户端的最大请求速率（次/秒） |
| `-schedule` | `MAGPIESYNC_SCHEDULE` | 空 | 首次初始化同步计划的 cron 表达式，例如 `0 4 * * *` |
| `-reset-password` | 无 | 关闭 | 清除本地 Web UI 密码后退出，不删除站点账户和本地资料 |
| 无 | `MAGPIESYNC_SECRET` | 空 | 可选的 base64 编码 32 字节密钥；未设置时使用 `data/secret.key` |

默认只监听本机回环地址。若需要从其他设备访问，请先自行配置可信的内网和访问控制；不要直接将服务暴露到公网。

## 数据与备份

```text
data/
  magpiesync.db    # SQLite 数据库
  secret.key       # 本地凭据加密密钥
media/             # 已下载的媒体文件
```

备份前请停止程序，再复制 `data/`；如需备份已下载媒体，也复制媒体目录。数据库和 `secret.key` 必须一起保留，否则已有加密凭据将无法解密。当前版本不会自动迁移旧数据库结构；升级前请先备份，遇到不兼容数据库时使用新的数据目录并重新连接账户。

忘记本地访问密码时，停止程序后运行：

```bash
./bin/magpiesync -reset-password
```

然后重新启动并设置新密码。此操作保留本地数据库和账户数据。

## 开发

```bash
go generate ./internal/sites/all
go test ./... -count=1
go vet ./...
go build ./...
```

前端是嵌入式 HTML、CSS 和 JavaScript，无需 Node 构建。站点适配器位于 `internal/sites/plugins/<site>`；生成清单位于 `internal/sites/all`，由 `go generate ./internal/sites/all` 更新。构建标签可通过 Makefile 选择站点，例如：

```bash
make build SITES="bilibili"
```

## 许可证

[Apache-2.0](LICENSE)

定时同步在「设置 → 常规」中配置，使用五段 Cron 表达式，留空关闭，保存后立即生效。计划按服务运行时区执行，适用于所有活跃账户，不会自动下载媒体。`-schedule` / `MAGPIESYNC_SCHEDULE` 仅在数据库尚无同步计划时初始化一次；已保存的值（包括空值）始终优先。

# 飞牛 FPK 打包

本目录按飞牛 fnOS 官方 Native 应用规范准备 MagpieSync 的应用包目录。`.fpk` 必须由官方 `fnpack build` 生成，普通 `tar.gz` 或改名后的压缩包都不是可安装的 FPK。

## 构建

需要在 Linux、WSL、CI 或飞牛设备上安装与当前系统匹配的 `fnpack`。脚本会交叉编译 Linux Go 二进制，并准备完整的 Native 包目录。也可以用 `FNPACK_BIN` 指定本地 `fnpack` 路径：

```bash
./packaging/fpk/build.sh x86
./packaging/fpk/build.sh arm

FNPACK_BIN=/path/to/fnpack ./packaging/fpk/build.sh x86
```

参数含义：

- `x86` 生成 `platform=x86`、`linux/amd64` 包。
- `arm` 生成 `platform=arm`、`linux/arm64` 包。

staging 目录分别为：

```text
dist/fpk/staging/magpiesync-x86/
dist/fpk/staging/magpiesync-arm/
```

脚本检测到 `fnpack` 后会执行：

```bash
cd dist/fpk/staging/magpiesync-x86 && fnpack build
cd dist/fpk/staging/magpiesync-arm && fnpack build
```

`.fpk` 分别输出到对应的 staging 目录中。若系统没有 `fnpack`，脚本只生成 staging 目录并以失败状态退出，不会生成伪 `.fpk`。

## FFmpeg

默认使用飞牛系统 PATH 中的 `ffmpeg`。如需集成到包内，传入与目标架构匹配的 Linux 可执行文件：

```bash
FFMPEG_BIN=/path/to/linux-amd64/ffmpeg ./packaging/fpk/build.sh x86
FFMPEG_BIN=/path/to/linux-arm64/ffmpeg ./packaging/fpk/build.sh arm
```

脚本会使用 `file` 校验架构，并将文件放到 `app/bin/ffmpeg/ffmpeg`。Windows 版 FFmpeg 不能放入 Linux FPK；动态链接版本还必须同时提供所需的共享库，静态构建更适合随包分发。

## 包目录

生成的目录包含官方要求的文件：

```text
manifest
app/bin/magpiesync
app/bin/ffmpeg/ffmpeg       # 可选
app/ui/config
app/ui/images/icon_64.png
app/ui/images/icon_256.png
cmd/main
cmd/install_init
cmd/install_callback
cmd/upgrade_init
cmd/upgrade_callback
cmd/uninstall_init
cmd/uninstall_callback
config/privilege
config/resource
wizard/
ICON.PNG
ICON_256.PNG
```

`cmd/main` 使用 `TRIM_APPDEST`、`TRIM_PKGVAR` 和 `TRIM_SERVICE_PORT`，支持 `start`、`stop`、`status`。数据和 PID、日志保存在飞牛为应用准备的持久化运行目录中。

## 安装验证

在目标设备安装或覆盖安装生成的 `.fpk` 后，启动应用并刷新飞牛桌面。桌面和应用中心的 MagpieSync 入口使用 `type=iframe`，点击后会在当前飞牛桌面内打开应用窗口，加载该 NAS 的 HTTP 8787 端口。应用仍使用自己的本地密码登录。

该端口入口适用于 HTTP NAS 桌面。HTTPS 桌面嵌入 HTTP 服务可能被浏览器按混合内容拦截；当前包尚未接入同源统一网关。IP 加端口的访问方式仍可使用。

设备验收：

- 点击 MagpieSync 图标，确认在桌面窗口内显示页面，且没有打开新的浏览器标签。
- 在窗口中登录，确认页面、API 和设置页正常；退出登录后受保护接口应拒绝访问。
- 关闭窗口再打开，并停止、启动应用，确认入口和状态正常。
- 8787 被其他服务占用时，启动应报告冲突；服务停止时，入口不应显示为仍可访问。
- 下载任务需要音视频合并时，确认设置页显示 FFmpeg 可用。

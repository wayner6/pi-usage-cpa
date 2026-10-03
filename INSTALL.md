# 发布与维护说明（维护者）

普通用户安装、验收和回滚请直接阅读 [README.md](README.md)。本文件不要求普通用户编译插件；发布资产以 GitHub Releases 为准。v0.1.0 仅支持 Antigravity；v0.2.0 配合 Pi Usage GitHub 0.6.0 提供七类额度，仍待真实 CPA v8 验证。

## CPA v8 商店约定

参考 [CPA 官方插件商店格式](https://github.com/router-for-me/CLIProxyAPI-Plugins-Store) 和 CPA v8 的 `internal/pluginstore`：仓库根目录 `registry.json`（schema 1 / `github-release`）列出本插件；CPA 用户需在 `plugins.store-sources` 添加此 URL 才能看见本项目。它不是 CPA 官方收录，官方商店收录需要维护者另行提交 PR 并获得批准。

Git 标签 `vX.Y.Z` 触发 `.github/workflows/release.yml`：先运行 Go 测试和 `vet`，再使用 GitHub 托管的 Linux amd64/arm64 原生 runner 构建 c-shared 库并打包：

```text
pi-usage-cpa_X.Y.Z_linux_amd64.zip
pi-usage-cpa_X.Y.Z_linux_arm64.zip
checksums.txt
```

ZIP 根目录只有 `pi-usage-cpa.so`，校验文件采用 sha256sum 格式；CPA 商店从 GitHub 最新 Release 下载并验证。发布前必须安排人工审查、核对目标 CPA v8 镜像架构与 libc、在匹配的隔离实例进行插件加载和脱敏上游额度集成验证。**创建 tag 会自动对外发布 Release；未得到明确发布授权不要创建或推送 tag。** 流水线不读取生产密钥或原始额度响应。

## 生产前检查

- 当前本地只核对了 CPA v8 的 C ABI 1、旧 RPC schema 兼容、资源路由、商店包格式及管理接口。未在实际 v8 Docker Compose 实例验收；**不要把静态核对当作生产兼容认证**。
- CPA 插件进程必须有服务端可用的明文管理凭据。`PI_USAGE_CPA_MANAGEMENT_KEY` 优先；没有时复用已有 `MANAGEMENT_PASSWORD`。管理入口必须在容器进程的数字 loopback 上；普通 Pi API Key 无法代替管理密钥。只依赖配置中的 bcrypt 哈希时无法自动反推明文。
- 宿主的请求日志可能与插件输出脱敏无关；确认管理请求及上游响应不被额外记录。
- 真实 `groups[].displayName` 与 `buckets[].window` 需要在服务器本地核对，缺失时用精确 `PI_USAGE_CPA_GROUP_MAP` 映射；没有窗口的降级数据不得冒充 5h/7d。
- Pi Usage GitHub 0.6.0 配合服务端 v0.2.0 解析七类额度且不回退旧插件；旧版 GitHub 0.5.0 只解析 Antigravity，npm 0.3.0 不认识新路由。安装时须明确客户端来源和版本。

## 手动构建（仅调试）

```sh
go test -race ./...
go vet ./...
CGO_ENABLED=1 go build -buildmode=c-shared -o pi-usage-cpa.so .
```

不要覆盖运行中的旧插件或 `pi-bridge`。生产安装首选商店；商店会写入插件启用配置，容器新增环境变量仍需由管理员以现有部署流程安排容器重建。

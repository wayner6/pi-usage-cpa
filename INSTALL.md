# 发布与维护说明（维护者）

用户安装、验收和回滚请阅读 [README.md](README.md)。本文件面向维护者；发布资产以 GitHub Releases 为准，当前推荐服务端 v0.2.4 和客户端 GitHub 0.6.2。

## CPA v8 商店约定

参考 [CPA 官方插件商店格式](https://github.com/router-for-me/CLIProxyAPI-Plugins-Store) 和 CPA v8 的 `internal/pluginstore`：仓库根目录 `registry.json`（schema 1 / `github-release`）列出本插件；CPA 用户需在 `plugins.store-sources` 添加此 URL 才能看见本项目。它不是 CPA 官方收录，官方商店收录需要维护者另行提交 PR 并获得批准。

Git 标签 `vX.Y.Z` 触发 `.github/workflows/release.yml`：先运行 Go race 测试、`vet` 和 CPA v8.0.8 实际宿主集成测试，再使用 GitHub 托管的 Linux amd64/arm64 原生 runner 构建 c-shared 库并打包：

```text
pi-usage-cpa_X.Y.Z_linux_amd64.zip
pi-usage-cpa_X.Y.Z_linux_arm64.zip
checksums.txt
```

ZIP 根目录只有 `pi-usage-cpa.so`，校验文件采用 sha256sum 格式；CPA 商店从 GitHub 最新 Release 下载并验证。发布前必须安排人工审查、核对目标 CPA v8 镜像架构与 libc、在匹配的隔离实例进行插件加载和脱敏上游额度集成验证。**创建 tag 会自动对外发布 Release；未得到明确发布授权不要创建或推送 tag。** 流水线不读取生产密钥或原始额度响应。

## 生产前检查

- CPA v8.0.8 实际宿主集成测试已覆盖加载、注册、重新配置和资源路由。一套实际部署已验收 Antigravity、CPA Codex 额度及客户端模型切换；其他服务商和目标镜像仍需各自验收，不能由此推断全部兼容。
- CPA 插件进程必须有服务端可用的明文管理凭据。`PI_USAGE_CPA_MANAGEMENT_KEY` 优先；没有时复用已有 `MANAGEMENT_PASSWORD`。管理入口必须在容器进程的数字 loopback 上；普通 Pi API Key 无法代替管理密钥。只依赖配置中的 bcrypt 哈希时无法自动反推明文。
- 宿主的请求日志可能与插件输出脱敏无关；确认管理请求及上游响应不被额外记录。
- 在服务器本地核对真实 `groups[].displayName` 与 `buckets[].window`。组名不匹配时可用 `PI_USAGE_CPA_GROUP_MAP` 精确映射；该配置不能补出缺失的窗口，降级数据不得冒充 5h/7d。
- 推荐 Pi Usage GitHub 0.6.2 配合服务端 v0.2.4：前者区分 GPT 与 GPT-OSS，后者包含注册和 Antigravity 组名映射修复。客户端不回退旧插件。

## 手动构建（仅调试）

```sh
go test -race ./...
go vet ./...
bash integration/test-cpa-v8.sh
CGO_ENABLED=1 go build -buildmode=c-shared -o pi-usage-cpa.so .
```

不要覆盖运行中的旧插件或 `pi-bridge`。生产安装首选商店；商店会写入插件启用配置，容器新增环境变量仍需由管理员以现有部署流程安排容器重建。

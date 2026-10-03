# Pi Usage CPA · Antigravity 额度插件

一个运行在 [CLIProxyAPI（CPA）](https://github.com/router-for-me/CLIProxyAPI) 服务端的只读插件，为 [Pi Usage](https://github.com/wayner6/pi-usage) 提供 Antigravity 账户的额度窗口。它按**账户和模型组**读取上游明确返回的 5 小时、周额度和重置时间，供 Pi 状态栏展示。

> **兼容性提示：** 尚未针对真实生产账户响应或实际 CPA v8 Docker Compose 实例完成端到端验证。Pi Usage **GitHub 0.5.0** 已只请求本插件的新路由；npm `@wayner6/pi-usage` 的 `latest` 仍为 **0.3.0**，不支持该路由。请先核对要安装的客户端来源和版本。

## 能做什么

- 使用账户自己的认证选择项和 project 调用固定的 Antigravity `retrieveUserQuotaSummary`，只认明确的模型组及 `window` 字段；Gemini 的额度与 Claude/GPT 共享组的额度不会混淆。
- 当前模型为 Gemini 时仅展示 Gemini 组；当前模型为 Claude 或 GPT 时仅展示 Claude/GPT 组。未知组名、未知窗口和缺失周窗口显示不可用，**不会**凭百分比或重置时间猜测窗口。
- 汇总请求失败时可降级为 `fetchAvailableModels` 的**未标窗口**额度；不会把降级数据写成 5h/7d。多个可用账户而 CPA 路由选择未知时，不把某个账户的额度冒充整个代理池。
- 只读 GET `/usage`、`/capabilities`、`/well-known`；使用普通 CPA API Key 鉴权。120 秒额度缓存、强制刷新限流、超时、脱敏。Pi 不需要管理密钥或上游 token。
- 插件 ID 为 `pi-usage-cpa`，与旧 `pi-bridge` 路由不同，可以并存。Pi Usage GitHub 0.5.0 **仅访问新路由**，不会回退旧插件；npm 0.3.0 不会自动发现本插件。

模拟显示（**纯格式示例，不是真实账户数据**）：

```text
Claude Opus · 5h 84% (resets in 3h 39m) · 7d 61% (resets in 3d 0h)
Claude Opus · 5h 84% · 7d unavailable
2 accounts · routing account unknown
```

## 适用条件

- 支持原生插件商店的 CPA v8（参考 [CPA v8 插件商店](https://github.com/router-for-me/CLIProxyAPI-Plugins-Store) 发布格式）。代码使用 C ABI 1 / RPC schema 1；v8 宿主兼容旧 schema 不代表每个 v8 派生版已通过集成测试。
- GitHub Release 提供 Linux `amd64` 和 `arm64` 安装包。请核对 CPA **容器**的架构与 libc；GitHub 构建使用 Ubuntu/glibc，不能直接假定能在 Alpine/musl 容器中加载。其他平台暂不声明支持。
- CPA 使用配置中的普通 `api-keys` 鉴权。本插件从**本机**管理接口验证这些 key；自定义插件鉴权的 key 未支持。
- CPA 进程已有 `MANAGEMENT_PASSWORD` 环境变量，或额外提供 `PI_USAGE_CPA_MANAGEMENT_KEY`（CPA Management Key 明文）。插件优先使用专用变量，缺省时复用进程已有的 `MANAGEMENT_PASSWORD`；若只有 CPA 配置里的哈希管理密钥，两者都没设置，则无法自动取得明文，必须额外在服务端安全注入。**不要**为了此插件新设 `MANAGEMENT_PASSWORD` 而意外开启 CPA 远程管理；这种情况使用专用变量。
- CPA 进程本身能访问 `http://127.0.0.1:8317` 管理接口，或用 `PI_USAGE_CPA_MANAGEMENT_ORIGIN` 指向进程网络空间内的另一数字 loopback HTTP origin。容器里的 `127.0.0.1` **不是宿主机**。不接受任意管理 URL、公网地址或客户端传入的上游 URL。
- 上游真实组名若与默认精确映射 `Gemini`、`Claude / GPT`、`Claude/GPT` 不同，管理员须先在服务器本地核对响应后设置 `PI_USAGE_CPA_GROUP_MAP`（JSON：上游组名 → `gemini` 或 `claude-gpt`）。本插件不会猜组名；无需在聊天提供响应。

## 最省事的安装方式：CPA 插件商店

安装插件本身不需要在服务器安装 Go，也不需要手工放 `.so`。目前通过下方自有插件源安装；尚未获 CPA 官方商店收录。

1. 确认 CPA 已启用插件功能，插件目录可写且会在容器重建后保留；备份 CPA 配置，保留已有 `pi-bridge`。
2. 在 CPA 管理界面 **配置管理 → 高级 → 第三方插件源** 中加入（该项由 CPA 管理界面版本提供，也可在 `plugins.store-sources` 中配置）：

   ```text
   https://raw.githubusercontent.com/wayner6/pi-usage-cpa/main/registry.json
   ```

   官方商店默认源始终保留。本项目**未获官方收录**，需添加一次第三方源；仅把源码仓库公开不会自动出现在官方列表。
3. **点击安装前**确认 CPA 进程已有正确的 `MANAGEMENT_PASSWORD`、管理接口在容器本机可达；满足条件则**无需新增密钥配置**。否则先按下节为 CPA 进程注入专用变量。插件注册缺少服务端密钥会失败，Pi 的普通 API Key 不能替代管理密钥。
4. 在 **插件 → 商店** 中找到 `Pi Usage · Antigravity Quota`，检查支持的容器平台并点击安装。CPA v8 商店会下载安装包、核对 SHA-256、写入启用配置；遇到重载/重启提示按 CPA 管理界面操作。不要覆盖或卸载旧 `pi-bridge`。
5. 使用 Pi Usage **GitHub 0.5.0**（`pi install github:wayner6/pi-usage`；pi-web 用 `git:https://github.com/wayner6/pi-usage`）再验收。已发布 npm `0.3.0` 不具备该能力，暂不要用 `npm:@wayner6/pi-usage` 验收此插件。

### Docker Compose：只有缺少环境变量时才改动

如果已有 `MANAGEMENT_PASSWORD` 且管理端口是容器内默认端口，通常不需要为**本插件**编辑 Compose。否则，把**已存在的服务端管理明文密钥**通过你的密钥管理/受保护环境文件传入 CPA 服务。下面仅展示变量引用，不包含密钥值，也不是完整 Compose 文件：

```yaml
services:
  cpa: # 换成实际服务名，只把 environment 字段合并进原服务
    environment:
      PI_USAGE_CPA_MANAGEMENT_KEY: ${PI_USAGE_CPA_MANAGEMENT_KEY:?set_in_a_private_server_env}
      # 仅当 CPA 容器内管理监听端口并非 8317 时设置：
      # PI_USAGE_CPA_MANAGEMENT_ORIGIN: http://127.0.0.1:实际端口
```

不要把明文写在提交到 Git 的 Compose 或 `.env` 中，不要把 Compose 的完整展开结果、密钥或原始额度响应发给任何人。`PI_USAGE_CPA_MANAGEMENT_KEY` 只属于 CPA 容器，不属于 Pi 客户端。Compose 新增环境变量通常需要安排一次**容器重建**（影响在途请求）；商店点击安装无法凭空把新环境注入已运行容器。也可采用现有安全密钥注入方式代替 `${...}`。不要把管理接口暴露到公网。

### 验收与回滚

- 新接口：`GET /v0/resource/plugins/pi-usage-cpa/capabilities` 和 `GET /v0/resource/plugins/pi-usage-cpa/usage`，请求头用**普通** CPA API Key，`X-Pi-Contract: 2`。无 key 应返回 401。`usage` 会向上游发起真实额度查询；先确定可接受此请求，再在服务器本地验收，避免将原始响应存到日志或 Git。
- 检查 `accounts[].groups[]` 的 `modelGroup`、`window`、`source`，以及 `missingWindows`。只有 `source: summary` 且窗口明确时才可认定读到了双窗口；`fallback` 不是 5h/7d。请只报告校验结果，不粘贴账户标识、请求头或额度原始响应。
- HTTP 401：检查普通 key 是否属于 CPA `api-keys`，以及服务端本机管理鉴权是否正常；错误时 fail closed。404：检查新插件是否正确启用。403/429：在服务器本地排查 project、上游状态和频率，不通过重置时间推断窗口。
- 回滚只需在 CPA 插件管理界面**禁用新增 `pi-usage-cpa`**，按提示重载；保留旧插件。Pi Usage GitHub 0.5.0 **不会回退旧插件**，禁用后状态栏可能显示 `Bridge Not Found`；如需旧额度展示须另外切回旧客户端。插件不改账户文件或模型调度。

## 自行构建（开发者可选）

Go 1.26、C 编译器、与目标 CPA 容器兼容的构建环境：

```sh
go test -race ./...
go vet ./...
CGO_ENABLED=1 go build -buildmode=c-shared -o pi-usage-cpa.so .
```

标签 `vX.Y.Z` 的 GitHub Actions 会为 Linux amd64/arm64 构建 `pi-usage-cpa_X.Y.Z_linux_<arch>.zip` 与 `checksums.txt`；ZIP 根目录为 `pi-usage-cpa.so`。`registry.json` 使用 CPA 商店 `github-release` 安装类型。实际发布以 GitHub Releases 页面列出的标签、资产和校验文件为准；不要使用仅有源码、缺少安装包的提交。

## 安全与许可

管理接口只访问 CPA 进程本机；客户端不接触管理密钥、OAuth token、账户 ID 或邮箱。服务端只对输出账户做进程内假名化，使用固定上游接口；不提供任意 URL 代理。CPA **宿主日志**独立于本插件，生产部署前请核对不要记录 `management/api-call` 请求/响应 body。

未复制无明确许可的 `abix5/pi-cliproxyapi-bridge` 源码。CPA SDK ABI 接口与管理界面数据结构来自 MIT 项目；见 [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)。如需复用旧桥接的完整功能，应先取得作者授权。

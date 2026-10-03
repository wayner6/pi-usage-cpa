# Pi Usage CPA · 服务端额度插件

运行在 [CLIProxyAPI（CPA）](https://github.com/router-for-me/CLIProxyAPI) 服务端的只读插件，为 [Pi Usage](https://github.com/wayner6/pi-usage) 提供逐账户的真实额度窗口。服务端当前源码覆盖 Antigravity、Claude、Codex、Kimi、xAI、Devin、Meta；具体值只来自各服务商的额度接口，不用 CPA 请求统计推算。

> **版本配套：** 本插件 GitHub Release **v0.2.1** 配合 Pi Usage **GitHub 0.6.0** 才支持下述七类；旧版服务端 v0.1.0 和客户端 0.5.0 仅支持 Antigravity。npm `@wayner6/pi-usage` 的 latest 仍为 0.3.0，不支持本插件。测试均使用模拟数据；尚未在真实 CPA v8 Docker Compose 和生产账户上完成端到端验收。

## 能做什么

- 使用账户自己的认证选择项和 project 调用固定的 Antigravity `retrieveUserQuotaSummary`，只认明确的模型组及 `window` 字段；Gemini 的额度与 Claude/GPT 共享组的额度不会混淆。
- Claude/Codex/Kimi/xAI/Devin/Meta 各自使用固定额度查询与模型匹配：Claude 的 5h/7d 和模型专属周额度、Codex 主额度明确时长的窗口、Kimi 显式周期与月额度、xAI 免费档可查询的账单额度、Devin 日/周额度、Meta 明确窗口与周额度。缺失、未知窗口不冒称 5h/7d。xAI 付费档若只有聊天健康探测而无可查额度，显示不可用；不额外发送聊天请求。
- 汇总请求失败时可降级为 `fetchAvailableModels` 的**未标窗口**额度；不会把降级数据写成 5h/7d。多个可用账户而 CPA 路由选择未知时，不把某个账户的额度冒充整个代理池。
- 只读 GET `/usage`、`/capabilities`、`/well-known`；使用普通 CPA API Key 鉴权。120 秒额度缓存、强制刷新限流、超时、脱敏。Pi 不需要管理密钥或上游 token。
- 插件 ID 为 `pi-usage-cpa`，与旧 `pi-bridge` 路由不同，可以并存。Pi Usage GitHub 0.6.0 **仅访问新路由**，不会回退旧插件；配合本插件 v0.2.1 展示七类额度。

| 服务商 | 读取内容（仅在上游实际返回时） | 不显示为额度的情况 |
|---|---|---|
| Antigravity | Gemini 与 Claude/GPT 两组的明确 5h、周窗口 | 周窗口缺失或单模型降级查询 |
| Claude | 5h、7d、Opus/Sonnet 等专属周窗口 | 不属于当前模型的专属额度 |
| Codex | 带时长字段的主额度；API 也返回 Code Review 窗口 | 状态栏不把 Code Review 当作当前模型主额度；未提供时长的窗口只标「window unknown」 |
| Kimi | 显式时长的限额与月用量比例 | 没有 `limit` 与 `used/remaining` 的行 |
| xAI | 可读取的周期积分与月度含量额度 | 只有付费健康探测、预存余额时不换算额度 |
| Devin | 日/周剩余百分比 | 只有套餐名称而无额度百分比 |
| Meta | DCA 凭据可读取的窗口与周额度 | 缺失 DCA、没有百分比的返回 |

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

## 安装

通过 **CPA Web 管理界面**安装。本插件尚未收录到官方商店，需要先添加第三方插件源。

1. 打开 **配置面板**，搜索 `store-sources`，找到 **第三方插件源**。
2. 点击 **添加**，在新的一行填写：

   ```text
   https://raw.githubusercontent.com/wayner6/pi-usage-cpa/main/registry.json
   ```

3. 保留已有插件源，保存配置，并确认 **启用插件系统**已开启。
4. 打开左侧 **插件商店**，刷新列表，搜索 `Pi Usage`。
5. 找到 **Pi Usage · CPA Quotas**，确认版本 **v0.2.1**，点击安装，再按界面提示启用或重载。

安装前，CPA 进程须已有 `MANAGEMENT_PASSWORD` 或 `PI_USAGE_CPA_MANAGEMENT_KEY`，且本机管理接口可达；缺少时需在服务端配置，Web 安装不会自动注入环境变量。安装包支持 Linux amd64/arm64 的 glibc 环境。已有 `pi-bridge` 请保留到新插件验收完成。

### 配套客户端

使用 Pi Usage **GitHub 0.6.0**；npm latest `0.3.0` 不支持本插件。客户端安装见 [Pi Usage README](https://github.com/wayner6/pi-usage#安装)。

### 验收与回滚

- 新接口：`GET /v0/resource/plugins/pi-usage-cpa/capabilities` 和 `GET /v0/resource/plugins/pi-usage-cpa/usage`，请求头用**普通** CPA API Key，`X-Pi-Contract: 2`。无 key 应返回 401。`usage` 会向上游发起真实额度查询；先确定可接受此请求，再在服务器本地验收，避免将原始响应存到日志或 Git。
- 检查 `accounts[].provider`、`groups[]` 的 `modelGroup`、`window`、`source`，以及 Antigravity 的 `missingWindows`。只有明确的窗口字段才可以标 5h/7d；未知窗口与 `fallback` 不可冒称双窗口。请只报告校验结果，不粘贴账户标识、请求头或额度原始响应。
- HTTP 401：检查普通 key 是否属于 CPA `api-keys`，以及服务端本机管理鉴权是否正常；错误时 fail closed。404：检查新插件是否正确启用。403/429：在服务器本地排查 project、上游状态和频率，不通过重置时间推断窗口。
- 回滚只需在 CPA 插件管理界面**禁用新增 `pi-usage-cpa`**，按提示重载；保留旧插件。Pi Usage GitHub 0.6.0 **不会回退旧插件**，禁用后状态栏可能显示 `Bridge Not Found`；如需旧额度展示须另外切回旧客户端。插件不改账户文件或模型调度。

## 安全与许可

管理接口只访问 CPA 进程本机；客户端不接触管理密钥、OAuth token、账户 ID 或邮箱。服务端只对输出账户做进程内假名化，使用固定上游接口；不提供任意 URL 代理。CPA **宿主日志**独立于本插件，生产部署前请核对不要记录 `management/api-call` 请求/响应 body。

未复制无明确许可的 `abix5/pi-cliproxyapi-bridge` 源码。CPA SDK ABI 接口与管理界面数据结构来自 MIT 项目；见 [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)。如需复用旧桥接的完整功能，应先取得作者授权。

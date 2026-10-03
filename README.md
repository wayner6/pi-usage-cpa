# Pi Usage CPA · 服务端额度插件

运行在 [CLIProxyAPI（CPA）](https://github.com/router-for-me/CLIProxyAPI) 服务端的只读插件，为 [Pi Usage](https://github.com/wayner6/pi-usage) 提供逐账户的真实额度窗口。服务端当前源码覆盖 Antigravity、Claude、Codex、Kimi、xAI、Devin、Meta；具体值只来自各服务商的额度接口，不用 CPA 请求统计推算。

> **版本配套：** **v0.2.3** 包含 CPA v8 注册修复及 Antigravity 新组名映射；v0.2.2 仅包含注册修复。客户端需要 Pi Usage **GitHub 0.6.0**，npm latest `0.3.0` 不支持本插件。宿主注册测试使用 CPA v8.0.8 源码和本地构建的 `.so`；生产环境已确认 v0.2.2 注册生效，完整真实额度端到端验收仍未完成。

## 能做什么

- 使用账户自己的认证选择项和 project 调用固定的 Antigravity `retrieveUserQuotaSummary`，只认明确的模型组及 `window` 字段；Gemini 的额度与 Claude/GPT 共享组的额度不会混淆。
- Claude/Codex/Kimi/xAI/Devin/Meta 各自使用固定额度查询与模型匹配：Claude 的 5h/7d 和模型专属周额度、Codex 主额度明确时长的窗口、Kimi 显式周期与月额度、xAI 免费档可查询的账单额度、Devin 日/周额度、Meta 明确窗口与周额度。缺失、未知窗口不冒称 5h/7d。xAI 付费档若只有聊天健康探测而无可查额度，显示不可用；不额外发送聊天请求。
- 汇总请求失败时可降级为 `fetchAvailableModels` 的**未标窗口**额度；不会把降级数据写成 5h/7d。多个可用账户而 CPA 路由选择未知时，不把某个账户的额度冒充整个代理池。
- 只读 GET `/usage`、`/capabilities`、`/well-known`；使用普通 CPA API Key 鉴权。120 秒额度缓存、强制刷新限流、超时、脱敏。Pi 不需要管理密钥或上游 token。
- 插件 ID 为 `pi-usage-cpa`，与旧 `pi-bridge` 路由不同，可以并存。Pi Usage GitHub 0.6.0 **仅访问新路由**，不会回退旧插件。

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

## 安装前提

- 支持原生插件商店的 CPA v8（参考 [CPA v8 插件商店](https://github.com/router-for-me/CLIProxyAPI-Plugins-Store) 发布格式）。代码使用 C ABI 1 / RPC schema 1；v8 宿主兼容旧 schema 不代表每个 v8 派生版已通过集成测试。
- GitHub Release 提供 Linux `amd64` 和 `arm64` 安装包。请核对 CPA **容器**的架构与 libc；GitHub 构建使用 Ubuntu/glibc，不能直接假定能在 Alpine/musl 容器中加载。其他平台暂不声明支持。
- CPA 使用配置中的普通 `api-keys` 鉴权。本插件从**本机**管理接口验证这些 key；自定义插件鉴权的 key 未支持。
- CPA 进程已有 `MANAGEMENT_PASSWORD` 环境变量，或额外提供 `PI_USAGE_CPA_MANAGEMENT_KEY`（CPA Management Key 明文）。插件优先使用专用变量，缺省时复用进程已有的 `MANAGEMENT_PASSWORD`；若只有 CPA 配置里的哈希管理密钥，两者都没设置，则无法自动取得明文，必须额外在服务端安全注入。**不要**为了此插件新设 `MANAGEMENT_PASSWORD` 而意外开启 CPA 远程管理；这种情况使用专用变量。
- CPA 进程本身能访问 `http://127.0.0.1:8317` 管理接口，或用 `PI_USAGE_CPA_MANAGEMENT_ORIGIN` 指向进程网络空间内的另一数字 loopback HTTP origin。容器里的 `127.0.0.1` **不是宿主机**。不接受任意管理 URL、公网地址或客户端传入的上游 URL。
- 当前源码按精确组名映射：`Gemini`、`Gemini Models` → `gemini`；`Claude / GPT`、`Claude/GPT`、`Claude and GPT models` → `claude-gpt`。新增两个组名需要 v0.2.3 或更新版本。其他组名须先在服务器本地确认归属，再通过 `PI_USAGE_CPA_GROUP_MAP`（JSON：上游组名 → 模型族）配置；未知名称不会按关键词或窗口猜测归属，无需分享原始响应。

## 安装

Web 添加插件源和安装插件**不会自动注入环境变量**。先满足上述前提，再安装 v0.2.3 或更新版本；v0.2.1 无法在 CPA v8.0.8 注册。

### 1. 准备服务端管理凭据

CPA 进程已有正确的 `MANAGEMENT_PASSWORD` 或 `PI_USAGE_CPA_MANAGEMENT_KEY` 时跳过此步。

Docker Compose 可从已有管理密钥文件读取明文。将以下字段合并到原 CPA 服务中；保留原镜像、端口、配置挂载和启动参数。`cpa` 和宿主文件路径换成实际值，已有同路径只读挂载无需重复添加。

```yaml
services:
  cpa:
    volumes:
      - /absolute/private/cpa-management-key:/run/secrets/cpa-management-key:ro
    command: ["/CLIProxyAPI/CLIProxyAPI"]
    entrypoint:
      - /bin/sh
      - -ec
      - |
        key="$$(cat /run/secrets/cpa-management-key)"
        test -n "$$key"
        export PI_USAGE_CPA_MANAGEMENT_KEY="$$key"
        unset key
        exec "$$@"
      - --
```

示例要求镜像有 `/bin/sh`。覆盖 entrypoint 后必须显式配置 `command`：示例使用官方镜像的可执行路径，请按实际镜像调整，并保留原有启动参数；原来有自定义 entrypoint 时需保留其初始化逻辑。`$$` 防止 Compose 提前展开容器变量。密钥文件应只包含现有管理密钥明文，限制文件权限，不写入 Git；不要开启 `set -x`、打印密钥或截图分享。配置中的 bcrypt 哈希不能代替明文。

安排短暂维护窗口，重建 CPA 容器以加载新环境（仅重启旧容器不够）：

```bash
docker compose up -d --no-deps --force-recreate cpa
```

### 2. 在 Web 商店安装

1. 打开 **配置面板**，搜索 `store-sources`，找到 **第三方插件源**。
2. 点击 **添加**，在新的一行填写：

   ```text
   https://raw.githubusercontent.com/wayner6/pi-usage-cpa/main/registry.json
   ```

3. 保留已有插件源，保存配置，并确认 **启用插件系统**已开启。
4. 打开左侧 **插件商店**，刷新列表，搜索 `Pi Usage`。
5. 找到 **Pi Usage · CPA Quotas**，确认版本为 **v0.2.3** 或更新版本，点击安装，再按界面提示启用或重载。

已有 `pi-bridge` 请保留到新插件验收完成。插件配置弹窗显示“没有声明可视化配置字段”是正常的，管理凭据来自服务端环境。

### 3. 验收

在 **插件管理**中应看到 **已注册、已生效**，只有“已发现、已配置”不代表安装成功。

在服务器本机执行不带 API Key 的请求（端口按实际配置调整）：

```bash
curl -sS -o /dev/null -w '%{http_code}\n' \
  http://127.0.0.1:8317/v0/resource/plugins/pi-usage-cpa/capabilities
```

预期为 **401**，不是 404。该检查仅验证路由和拒绝未认证请求；不能证明普通 Key 鉴权或上游额度查询成功。

已认证验收在本机使用普通 CPA API Key，请求头带 `X-Pi-Contract: 2`，先检查 `/capabilities`，再检查 `/usage`。`usage` 会向上游查询真实额度。不要分享 Key、请求头或原始账户响应，只报告状态码与脱敏校验结果。

### 配套客户端

使用 Pi Usage **GitHub 0.6.0**；npm latest `0.3.0` 不支持本插件。客户端安装见 [Pi Usage README](https://github.com/wayner6/pi-usage#安装)。

## 故障排查

| 现象 | 检查与处理 |
| --- | --- |
| 文件未发现 | 检查商店安装结果、插件目录、持久化挂载和读取权限。 |
| 已加载但未注册，日志为 `invalid metadata or no capabilities` | 检查此前的注册错误；若仅此错误，核对必需元数据和能力。v0.2.1 缺少 `GitHubRepository`，需安装修复版。 |
| 日志为 `server-only loopback management configuration required` | 检查 CPA 进程中的管理环境变量、本机 origin 和组映射；注入变量后需重建容器。 |
| 资源路由返回 404 | 检查插件是否已注册、生效以及资源路由是否注册；不能只检查文件存在。 |
| 带普通 Key 仍返回 401 | 检查 Key 是否属于 CPA `api-keys`，以及本机管理接口鉴权。 |
| `.so` 加载失败 | 检查容器架构、glibc 和宿主 ABI；Ubuntu 构建包不保证适用 Alpine/musl。 |

## 回滚

在 Web 插件管理中禁用 `pi-usage-cpa`，按提示重载。若需要恢复原启动方式，还原 Compose 的 entrypoint 和新增挂载，再重建 CPA 容器；重建会短暂中断服务。保留配置备份和旧 `pi-bridge`。

Pi Usage GitHub 0.6.0 不会回退旧插件，禁用后可能显示 `Bridge Not Found`；需要旧额度展示时须切回旧客户端。插件不改账户文件或模型调度。

## 开发

```bash
go test -race ./...
go vet ./...
bash integration/test-cpa-v8.sh
```

宿主集成测试固定 CPA v8.0.8 提交 `fd48ea6`，在临时目录构建并加载 `.so`，验证宿主注册、重新配置、管理能力和资源路由的未认证 401。使用合成凭据，不访问生产服务或上游账户。发布流程也执行此测试；通过不等于真实额度端到端验收。

## 安全与许可

管理接口只访问 CPA 进程本机；客户端不接触管理密钥、OAuth token、账户 ID 或邮箱。服务端只对输出账户做进程内假名化，使用固定上游接口；不提供任意 URL 代理。CPA **宿主日志**独立于本插件，生产部署前请核对不要记录 `management/api-call` 请求/响应 body。

未复制无明确许可的 `abix5/pi-cliproxyapi-bridge` 源码。CPA SDK ABI 接口与管理界面数据结构来自 MIT 项目；见 [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)。如需复用旧桥接的完整功能，应先取得作者授权。

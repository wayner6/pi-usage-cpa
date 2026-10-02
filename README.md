# pi-usage-cpa

独立实现的 CPA 原生只读 Antigravity 额度插件。插件 ID 为 `pi-usage-cpa`，不覆盖 `pi-bridge`。开发候选版本；尚未接入生产 CPA 验证。

**另一台服务器的构建、配置、验证与回滚：见 [INSTALL.md](INSTALL.md)。** 本仓库不包含 Pi Usage 客户端补丁；已发布 npm 0.3.0 尚不支持新路由。

## 许可与来源

本次检查：`abix5/pi-cliproxyapi-bridge` 提交 `039c28b23abcc2e475252a92cd115a7eb943a251` 的递归文件树没有 LICENSE，GitHub `license` 为 null。没有复制其源码；公开可读不是再发布授权。如需直接复用其其他功能，需要作者授权。

ABI 按 MIT 许可的 `router-for-me/CLIProxyAPI` **v7.2.93** 的 `sdk/pluginabi`、`sdk/pluginapi` 与原生示例接口独立实现，无 Go 第三方依赖。许可正文见 `THIRD_PARTY_NOTICES.md`。字段结构按 MIT 管理界面仓库 `router-for-me/Cli-Proxy-API-Management-Center` 提交 `752e0ee772220ce49aae1221a3f39f23236590d7` 的 Antigravity 数据层核对：

- 请求 `POST https://daily-cloudcode-pa.googleapis.com/v1internal:retrieveUserQuotaSummary`；`data` 是 `{"project": <该凭据项目>}`。
- CPA 管理 `api-call` 使用该凭据 `authIndex`，服务端替换 `Bearer $TOKEN$`。
- 响应 `groups[].displayName / display_name`，`buckets[].window`、`remainingFraction / remaining_fraction`、`resetTime / reset_time`。
- 明确窗口：`5h/five-hour/five_hour` → `5h`，`weekly/week/7d` → `7d`。不按 reset 时间、桶标签、数值猜窗口。

这确认的是公开界面实现的结构，不是生产响应的实测。部署前必须在服务器本地核对实际组名、窗口和请求 User-Agent；不要把原始响应发到聊天或存入 Git。默认精确组名白名单 `Gemini`、`Claude / GPT`、`Claude/GPT`；其他组名只通过服务器配置精确映射，不模糊推断。未知组/窗口会体现在 `missingWindows`；若无已知窗口则降级。重复组窗口视为歧义，降级。

## 安全与接口

资源接口均要求普通 CPA API Key：

```
GET /v0/resource/plugins/pi-usage-cpa/usage
GET /v0/resource/plugins/pi-usage-cpa/usage?refresh=1
GET /v0/resource/plugins/pi-usage-cpa/capabilities
GET /v0/resource/plugins/pi-usage-cpa/well-known
```

仅 GET；仅 `refresh` 查询参数。无管理代理、任意 URL、客户端账户选择、凭据下载或写操作。普通 key 通过服务器本机 `/v0/management/api-keys` 校验，不缓存以避免撤销延迟。为实现本机管理桥接，CPA 进程环境必须有管理密钥；Pi 不接触此密钥或上游凭据。

- 管理 origin 限定 HTTP 数字 loopback 地址，禁用环境 HTTP proxy 和重定向。
- 内部管理只读 GET `api-keys`、GET `auth-files`、固定 POST `api-call` 两种额度端点。认证项的 project 来自本项顶层、metadata 或 attributes；缺失则显式失败，不跨账户借用，不自动下载 OAuth 文件。
- 自定义普通 key 鉴权插件不出现在 `api-keys` 的情况暂不支持，fail closed。
- 每个管理请求超时 10 秒，一次额度刷新总截止 20 秒，管理响应大小上限 4 MiB。
- 120 秒服务端缓存；强制刷新至少相隔 30 秒，全局锁合并普通并发请求，失败也退避。首次全局失败返回 503；已有缓存全局失败标 `stale`。逐账户 summary 失败使用未标窗口的 `fetchAvailableModels` 降级，不把不同模型因数值相同折叠成共享池。
- 输出只含白名单字段。authIndex 是进程内随机盐 HMAC 化的假名，重启改变；标签只为 `Antigravity N`。不输出真实 ID、邮箱、project、token、原始上游错误或请求头。插件无日志调用。
- **CPA 自身日志独立于插件**：生产必须核对关闭请求/调试日志及对 management/api-call 的外部访问日志 body 记录；不能声称插件能关闭宿主所有日志。管理凭据环境文件应限制读取权限，不用命令行参数传密钥。
- 多账户不预测 CPA 路由；新版客户端显示 `2 accounts · routing account unknown`，不将某一账户伪装成池额度。

## 构建与测试

需要 Go 1.26 和 C 编译器（CPA 同架构 Linux/macOS；当前已在 Linux amd64 构建）。

```sh
go test -race ./...
go vet ./...
mkdir -p dist
go build -buildmode=c-shared -o dist/pi-usage-cpa.so .
```

测试全部是人工构造数据，不含生产响应。客户端改动在 `../pi-usage`；运行 `npm run verify`。新客户端优先新路由，仅 404 回退旧路由；401/403/429/503 不切换插件。旧客户端仍访问旧插件，两个插件并行时无路由冲突。本插件输出 `schemaVersion:1` 的原有 accounts/groups 外形，但新增显式 `modelGroup/window/source/missingWindows`；旧客户端不自动发现新插件，不建议手动把新路由改名给旧客户端使用。

## 示例（完全模拟，不是任何真实账户额度）

```json
{
  "schemaVersion": 1,
  "generatedAt": "2030-01-01T00:00:00Z",
  "cache": {"updatedAt":"2030-01-01T00:00:00Z","ttlMs":120000,"stale":false},
  "accounts": [{
    "provider":"antigravity","authIndex":"synthetic-opaque","label":"Antigravity 1",
    "groups":[
      {"id":"gemini-5h","label":"5h","modelGroup":"gemini","window":"5h","remainingFraction":0.42,"source":"summary"},
      {"id":"gemini-7d","label":"7d","modelGroup":"gemini","window":"7d","remainingFraction":0.31,"source":"summary"},
      {"id":"claude-gpt-5h","label":"5h","modelGroup":"claude-gpt","window":"5h","remainingFraction":0.23,"source":"summary"},
      {"id":"claude-gpt-7d","label":"7d","modelGroup":"claude-gpt","window":"7d","remainingFraction":0.17,"source":"summary"}
    ]
  }]
}
```

模拟状态栏：`Gemini Pro · 5h 42% · 7d 31%` 或 `Claude Opus · 5h 23% · 7d 17%`。真实响应带有效 resetTime 时，客户端增加 `(resets in …)`；此示例没有 resetTime，不生成重置倒计时。仅有 5h 时：`Claude Opus · 5h 23% · 7d unavailable`。降级时：`Claude Opus 60% · window unknown · 5h/7d unavailable`（60% 也是测试数据）。

## 待批准的部署步骤（没有执行）

1. 核对目标 CPA 版本/ABI、插件配置方式、loopback 管理监听端口以及宿主日志设置。先备份 CPA 配置和旧插件，保持 `pi-bridge` 启用。
2. 在服务器本地受保护的进程环境配置：
   - `PI_USAGE_CPA_MANAGEMENT_ORIGIN`，默认 `http://127.0.0.1:8317`；
   - `PI_USAGE_CPA_MANAGEMENT_KEY`，真实值仅服务器密钥管理/受限环境文件，不进源码、聊天、shell history；
   - 可选 `PI_USAGE_CPA_GROUP_MAP`，JSON 精确组名映射，例如 `{"Gemini":"gemini","Claude / GPT":"claude-gpt"}`，必须按实际响应核对。
3. 在匹配目标 OS/架构的环境构建，将产物作为**新增** ID `pi-usage-cpa` 安装，不能覆盖旧 `.so`。实际 CPA 配置/重载命令须核对目标版本后单独批准，本文不提供未经验证的配置片段。
4. 经批准，只在服务器本地用测试请求核对该凭据 summary 200、真实四窗口或明确缺失、modelGroup、缓存/限流，以及 401 无密钥。此步骤会产生实际额度查询请求，未获准不执行。仅记录校验结论，不保存原始 body。
5. 使用本地修改后的 Pi Usage 验收模型切换；npm 0.3.0 已发布包本身**未更改、尚不支持本补丁**。发布/安装客户端需要另外批准。

回滚：恢复客户端旧版本或禁用新增 `pi-usage-cpa`（产生 404，新客户端回退旧路由），保留原 `pi-bridge`；撤销新增的环境/配置并按 CPA 正式方式重载。仅禁用新插件可能影响新客户端的请求但不影响代理模型请求；不要卸载旧插件。插件不写账户文件，不改路由调度。

## 当前边界

无配置说明页面、其他服务商和模型目录；不是旧项目的全功能原位替换。真正生产组名与字段、CPA 宿主加载与日志行为尚未集成验证，不能宣称真实账户显示已经修复。若后续需要生产验证，先列明具体请求取得同意，在服务器本地执行，不索取聊天中的密钥或原始响应。

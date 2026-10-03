# Pi Usage CPA

为 [Pi Usage](https://github.com/wayner6/pi-usage) 提供 CPA 账户额度的服务端插件。安装在 [CLIProxyAPI（CPA）](https://github.com/router-for-me/CLIProxyAPI) 服务端，客户端使用普通 CPA API Key 查询。

[安装](#安装) · [支持的服务商](#支持的服务商) · [故障排查](#故障排查) · [反馈问题](https://github.com/wayner6/pi-usage-cpa/issues)

## 功能

- 查询各账户的剩余额度、明确的额度窗口和重置时间。
- 分开返回账户与模型组，区分 Antigravity 的 Gemini、Claude / GPT-OSS 额度。
- 对客户端隐藏管理密钥、上游 Token、账户 ID 和邮箱。
- 缓存额度约 120 秒，并限制强制刷新频率。刷新整体超时或取消时保留旧数据并标为 stale，不将未完成的查询当作新结果缓存。

两个项目的安装位置：

| 项目 | 安装位置 | 作用 |
| --- | --- | --- |
| [pi-usage](https://github.com/wayner6/pi-usage) | Pi / pi-web | 额度展示、模型匹配和本地 Skill 统计 |
| 本项目 | CPA 服务端 | 查询账户额度并返回脱敏数据 |

推荐配套版本：服务端 **v0.2.4 或更新版本**，客户端 **GitHub 0.6.2 或更新版本**。

本插件只提供额度查询，不是旧 `pi-bridge` 全部功能的替代品。

## 安装

通过 **CPA Web 管理界面的插件商店**安装，无需手动编译或复制 `.so`。本项目尚未收录到官方默认源，需先添加第三方插件源。

### 1. 检查服务端环境

| 项目 | 要求 |
| --- | --- |
| CPA | 支持原生插件商店的 v8；宿主集成测试固定 v8.0.8 |
| 运行环境 | Linux amd64 / arm64，glibc；发布包不保证适用 Alpine / musl |
| 插件目录 | 可写，并在容器重建后保留 |
| 管理凭据 | CPA 进程中有 `PI_USAGE_CPA_MANAGEMENT_KEY`，或已有 `MANAGEMENT_PASSWORD` |
| 本机管理接口 | CPA 进程可访问 `http://127.0.0.1:8317`；容器内地址不是宿主机地址 |

管理凭据是 **CPA 管理密钥的明文**，不是普通 API Key，也不是配置中的 bcrypt 哈希。专用变量 `PI_USAGE_CPA_MANAGEMENT_KEY` 优先；没有时复用已有的 `MANAGEMENT_PASSWORD`。

**Web 安装不会自动注入环境变量。** 两个变量都没有时，先在服务端安全注入凭据，再安装插件。不要为此新设 `MANAGEMENT_PASSWORD` 而意外开启远程管理。

<details>
<summary>Docker Compose：从已有只读密钥文件注入凭据</summary>

备份原配置，将以下字段合并到 CPA 服务。替换服务名 `cpa` 和宿主文件路径，保留原镜像、端口、配置挂载和启动参数。已有同路径挂载无需重复添加。

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

- 镜像须有 `/bin/sh`。覆盖 entrypoint 后必须显式配置 `command`；示例使用官方镜像路径，按实际镜像调整并保留启动参数。原来有自定义 entrypoint 时，还需保留其初始化逻辑。
- `$$` 防止 Compose 提前展开容器变量。密钥文件只存已有管理密钥明文，限制文件权限，不提交到 Git。
- 不开启 `set -x`，不打印密钥或分享展开后的配置、环境变量及截图。

新增环境变量需**重建容器**，仅重启旧容器不够。安排短暂维护窗口后执行：

```bash
docker compose up -d --no-deps --force-recreate cpa
```

</details>

### 2. 添加插件源并安装

1. 打开 CPA Web 的 **配置面板**，搜索 `store-sources`，找到 **第三方插件源**。
2. 点击 **添加**，在新的一行填写：

   ```text
   https://raw.githubusercontent.com/wayner6/pi-usage-cpa/main/registry.json
   ```

3. 保留已有源，保存配置，确认 **启用插件系统**已开启。
4. 打开左侧 **插件商店**，刷新并搜索 `Pi Usage`。
5. 找到 **Pi Usage · CPA Quotas**，选择 **v0.2.4 或更新版本**安装，按提示启用或重载。

CPA v8.0.8 会缓存商店最新版本号一小时。如果仍显示旧版，可从历史版本中明确选择 `v0.2.4`，无需为刷新商店重启服务。

已有 `pi-bridge` 先保留到验收完成。配置弹窗显示“没有声明可视化配置字段”是正常的，管理凭据通过服务端环境传入。

### 3. 确认安装成功

在 **插件管理**中确认版本，并看到 **已注册、生效中**。仅有“已发现、已配置”表示文件存在，尚不能证明插件可用。

在服务器本机发起不带 API Key 的请求，端口按实际配置调整：

```bash
curl -sS -o /dev/null -w '%{http_code}\n' \
  http://127.0.0.1:8317/v0/resource/plugins/pi-usage-cpa/capabilities
```

预期 **401**。404 表示路由尚不可用。401 只验证路由已注册且拒绝未认证请求，不代表上游额度查询成功。

然后在 Pi / pi-web 中安装或更新 [Pi Usage GitHub 版](https://github.com/wayner6/pi-usage#安装)，选择 CPA 模型并执行：

```text
/usage doctor
/usage current
```

适配器应为 `pi-usage-cpa`，额度显示应对应当前模型。首次 `/usage` 查询会向上游请求额度；请只分享脱敏状态，不发送 Key、请求头或原始账户响应。

## 支持的服务商

| 服务商 | 可查询的数据 |
| --- | --- |
| Antigravity | Gemini 与 Claude / GPT-OSS 两组明确的 5h、7d 窗口 |
| Claude | 5h、7d 及 Opus / Sonnet 等模型专属周额度 |
| Codex | 明确时长的主额度窗口；接口返回的 Code Review 数据也会保留，但不作为当前模型主额度 |
| Kimi | 显式周期额度和月用量比例 |
| xAI | 免费档可查询的周期积分和月度含量额度；付费档可能没有可查询额度 |
| Devin | 日、周剩余百分比 |
| Meta | DCA 凭据可读取的窗口及周额度 |

只返回上游实际提供的数据。缺少可用比例或凭据时显示不可用；窗口不明确时标为未知。余额、重置时间和 CPA 请求统计不用于推算 5h / 7d。xAI 查询不发送聊天请求。

Antigravity 汇总查询失败时，可以降级为单模型额度；这种数据保留 `window unknown`，不会显示成双窗口。客户端遇到多个匹配账户且路由账户未知时，显示 `N accounts · routing account unknown`。

已在一套 CPA v8.0.8 部署中验收插件注册、Antigravity 的明确窗口、CPA Codex 额度，以及客户端 Claude、Gemini、GPT 模型切换。其他服务商目前主要由模拟数据测试覆盖；不能据此保证所有账户和 v8 派生部署均可用。

## 配置与接口

### 服务端环境变量

| 变量 | 用途 |
| --- | --- |
| `PI_USAGE_CPA_MANAGEMENT_KEY` | 专用管理密钥明文；未设置时复用已有 `MANAGEMENT_PASSWORD` |
| `PI_USAGE_CPA_MANAGEMENT_ORIGIN` | 本机管理 origin，默认 `http://127.0.0.1:8317`；仅接受数字 loopback 的 HTTP origin |
| `PI_USAGE_CPA_GROUP_MAP` | 额外精确组名映射，JSON 格式：上游组名 → `gemini` 或 `claude-gpt` |

默认识别 `Gemini`、`Gemini Models` → `gemini`；`Claude / GPT`、`Claude/GPT`、`Claude and GPT models` → `claude-gpt`。其他名称先在服务端确认归属，再配置映射，未知名称不会按关键词猜测。

### 客户端接口

以下接口均使用普通 CPA `api-keys` 鉴权；自定义插件鉴权的 Key 暂不支持。

```text
GET /v0/resource/plugins/pi-usage-cpa/usage
GET /v0/resource/plugins/pi-usage-cpa/capabilities
GET /v0/resource/plugins/pi-usage-cpa/well-known
```

配套客户端请求携带 `X-Pi-Contract: 2`。手动已认证验收应在本机进行：先检查 `capabilities`，再检查 `usage`，不要把凭据或原始响应写入日志、截图或 Git。

## 故障排查

| 现象 | 检查与处理 |
| --- | --- |
| 商店搜不到 | 确认已添加本项目的 `registry.json` 源并保存、刷新 |
| 商店版本落后 | 对照 [GitHub Release](https://github.com/wayner6/pi-usage-cpa/releases/latest)；缓存到期后刷新，或选择明确的历史版本 |
| 文件未发现 | 检查安装结果、插件目录、持久化挂载和读取权限 |
| 已加载但未注册，日志为 `invalid metadata or no capabilities` | 查看此前的注册错误；v0.2.1 缺少必需元数据，需升级到 v0.2.3 或更新版本 |
| `server-only loopback management configuration required` | 检查 CPA 进程的管理变量、loopback origin 及组映射；新增变量后需重建容器 |
| 路由返回 404 | 检查插件是否已注册、生效及资源路由是否注册 |
| 带普通 Key 仍返回 401 | 检查 Key 是否属于 CPA `api-keys`、管理凭据是否正确及本机管理接口是否可达 |
| `summary unavailable` | 检查上游状态、响应字段和精确组名；v0.2.3 已补两个实际组名 |
| `.so` 加载失败 | 核对容器架构、glibc 和宿主 ABI，不能直接使用于 Alpine / musl |

## 更新与回滚

在 CPA 插件商店选择新版本更新，按提示重载。再确认版本、注册状态和客户端额度。

回滚时，在 **插件管理**禁用 `pi-usage-cpa`，按提示重载。若要恢复原启动方式，还原 Compose 的 entrypoint、command 和新增挂载，再重建容器；此操作会短暂中断服务。

保留原配置备份和旧插件。当前 Pi Usage 不会回退 `pi-bridge`；禁用新插件后可能显示 `Bridge Not Found`，如需旧展示还需切回旧客户端。本插件不修改账户文件或模型调度。

## 安全

管理请求限定 CPA 进程内的数字 loopback，上游请求使用固定额度接口。每个账户使用自己的认证上下文，Meta DCA 只在单次刷新中通过本机管理接口读取，不进入额度响应。

管理密钥和上游凭据仅留服务端。插件输出脱敏与 CPA 宿主日志是两件事，请确认宿主不会记录 `management/api-call` 的请求、响应 body；不要将管理接口暴露到公网。

## 开发与维护

```bash
go test -race ./...
go vet ./...
bash integration/test-cpa-v8.sh
```

宿主集成测试固定 CPA v8.0.8 提交 `fd48ea6`，实际构建并加载 `.so`，检查注册、重新配置、管理能力和未认证资源路由。使用合成凭据，不访问生产账户；发布流程也执行此测试。

打包、发布和本地调试见 [INSTALL.md](./INSTALL.md)。CPA ABI 使用第三方 MIT 接口，见 [THIRD_PARTY_NOTICES.md](./THIRD_PARTY_NOTICES.md)。本项目未复制旧 `pi-bridge` 源码。

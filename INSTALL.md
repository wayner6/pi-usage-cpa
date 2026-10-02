# 另一台服务器安装指南（Linux）

这是候选版本，不是已在你的生产 CPA 验证的发行版。请先在测试实例验证，再安排生产变更。安装插件与重启 CPA 会影响正在进行的请求；下列命令由管理员在确认后执行，本项目没有自动操作服务器。

## 1. 确认部署条件

- CPA 支持原生插件，接口参考版本为 CLIProxyAPI v7.2.93，C ABI 1 / schema 1。
- 先记录目标 CPA 版本与部署方式。旧版本、派生版及 v8 不能只凭 ABI 数字相同就保证兼容。
- 检查现有插件目录与 `plugins.configs`，备份实际 CPA 配置。保留原 `pi-bridge` 文件和配置。
- 使用与 CPA 相同的 OS/CPU 架构、兼容 libc 的构建环境。Docker 中运行 CPA 时，尽量在匹配容器的环境构建；glibc 产物不能直接假定可在 Alpine/musl 使用。
- 本插件只支持 CPA 配置 `api-keys` 中的普通 key；自定义鉴权插件提供的 key 暂不支持。
- CPA 从自身进程网络空间能够访问 loopback 管理端口。Docker 里的 `127.0.0.1` 指容器本身；不用宿主机或公网管理地址替代。
- 管理接口须允许本机访问。只需给 CPA 进程提供原有管理密钥的**明文值**，不能使用配置文件中可能已经哈希化的 secret-key；不要求另开公网管理访问。
- 在验收前检查关闭宿主请求/调试日志及外部 management/api-call body 记录，避免 OAuth、project 或原始额度响应被宿主日志保存。

## 2. 拉取并构建

安装 Git、C 编译器和 Go 1.26.x。不要直接运行本机上传的不同平台二进制；仓库不提供预编译发布资产。

```sh
git clone https://github.com/wayner6/pi-usage-cpa.git
cd pi-usage-cpa
go version
go test -race ./...
go vet ./...
mkdir -p dist
CGO_ENABLED=1 go build -buildmode=c-shared -o dist/pi-usage-cpa.so .
```

通过测试之后，记录 `git rev-parse HEAD` 和 `sha256sum dist/pi-usage-cpa.so`，便于回滚定位。

## 3. 配置服务器密钥（不可提交或发送）

CPA **进程环境**需要：

| 环境变量 | 含义 |
|---|---|
| `PI_USAGE_CPA_MANAGEMENT_ORIGIN` | 默认 `http://127.0.0.1:8317`；改成 CPA 进程内实际 loopback HTTP 管理端口 |
| `PI_USAGE_CPA_MANAGEMENT_KEY` | 原有 CPA Management Key 明文，服务端独占 |
| `PI_USAGE_CPA_GROUP_MAP` | 可选，实际上游组名到 `gemini` / `claude-gpt` 的精确映射 |

使用现有密钥管理方式；不要把密钥写在命令参数、Git、聊天或 shell history。不要在聊天发送你的环境文件。

如果通过 systemd 启动，可以在**先确认文件不存在或不含其他数据**后创建一个新文件，例如 `/etc/pi-usage-cpa.env`，设置 root 所有、模式 `0600`，用服务器本地编辑器填写：

```ini
PI_USAGE_CPA_MANAGEMENT_ORIGIN=http://127.0.0.1:8317
PI_USAGE_CPA_MANAGEMENT_KEY=<仅在服务器本地填写真实明文>
```

然后为实际 CPA unit 增加 `EnvironmentFile=/etc/pi-usage-cpa.env`。**文件名和 unit 名是示例，不能假定实际路径或服务名。** 插件注册时读取环境，给当前 SSH shell export 不会自动改变已经运行的 CPA 进程。

Docker Compose：通过受保护的 `env_file` 或现有秘密注入机制将变量传入 CPA 容器。不要写在提交到 Git 的 Compose 配置里，也不要贴 `docker inspect` 或 `docker compose config` 完整输出。单纯容器内 export 同样不会改变运行中的 CPA 环境。注入的新环境通常需要重建该容器，而非仅在宿主 shell 设置变量。

默认组名映射：`Gemini` → `gemini`，`Claude / GPT`、`Claude/GPT` → `claude-gpt`。这些默认名字尚未对你的实际响应验证。如果真实名字不同，在服务器本地确认后配置，例如：

```ini
PI_USAGE_CPA_GROUP_MAP='{"实际Gemini组名":"gemini","实际Claude-GPT组名":"claude-gpt"}'
```

这里是格式说明，不要原样采用“实际…组名”；引用语法以你的环境注入方式为准。传给进程的变量内容必须为有效 JSON，而不是带外层引号的字符串。

## 4. 新增插件，不覆盖旧插件

按照实际 `plugins.dir`，把产物放入该目录，名字必须为 **`pi-usage-cpa.so`**。CPA 参考实现从文件名决定 plugin ID。只需 `.so`，不用安装生成的 `.h`。

```sh
# /ACTUAL/CPA/plugins 是占位路径，先换成真实目录。
# -n 确保已有同名文件时拒绝覆盖。
cp -n dist/pi-usage-cpa.so /ACTUAL/CPA/plugins/pi-usage-cpa.so
```

如果之前就有同名插件，不要靠这一行默默覆盖；先备份、核对版本并安排升级。

下面配置结构已经按 CPA v7.2.93 示例核对。**将新条目合并到现有配置，不要替换整段，不要重复写 `plugins:` 键**；保留已有 `pi-bridge` 和其他插件条目、目录与设置：

```yaml
plugins:
  enabled: true
  dir: "plugins" # 保留你原有的实际目录
  configs:
    pi-usage-cpa:
      enabled: true
      priority: 0
```

插件初始注册需环境变量已就绪。管理员确认变更时间、备份和在途请求影响后，按现有部署方式重载/重启 CPA；这里不假定服务名，也不提供会自动重启生产的脚本。

## 5. 安全验证接口

新路由：

```
/v0/resource/plugins/pi-usage-cpa/capabilities
/v0/resource/plugins/pi-usage-cpa/usage
```

建议先无密钥验证本机 capabilities 返回 401，然后用普通 API Key 验证 capabilities。capabilities 不调用上游额度接口；usage 会按可用 Antigravity 账户发起实际额度查询，首次或缓存到期可能进行 summary 与降级调用。先批准查询再执行。

下面使用 Python 标准库，API Key 用隐藏输入，不放进 curl 参数或 history。它仅输出固定的脱敏字段；不保存原始响应，也不输出账户 ID、邮箱、请求头或未知字符串。**如果它报错，请只报告 HTTP 状态/固定错误，不要贴原始 body。**

```sh
python3 - <<'PY'
import getpass, json, sys, urllib.request, urllib.error
try:
    sys.stdin = open('/dev/tty')
except OSError:
    raise SystemExit('需要交互终端；不要把密钥硬编码到脚本')
origin = input('CPA 本机 origin（默认 http://127.0.0.1:8317）：').strip() or 'http://127.0.0.1:8317'
from urllib.parse import urlsplit
u = urlsplit(origin)
if u.scheme != 'http' or u.hostname not in ('127.0.0.1', '::1') or u.username or u.query or u.fragment or u.path not in ('', '/'):
    raise SystemExit('仅接受本机 HTTP origin')
path = input('查询 capabilities 或 usage（默认 capabilities）：').strip() or 'capabilities'
if path not in ('capabilities', 'usage'):
    raise SystemExit('不支持该路径')
if path == 'usage' and input('usage 会查询上游额度，输入 YES 同意：') != 'YES':
    raise SystemExit('未发起请求')
key = getpass.getpass('普通 CPA API Key（隐藏输入）：')
req = urllib.request.Request(origin.rstrip('/') + '/v0/resource/plugins/pi-usage-cpa/' + path,
    headers={'Authorization': 'Bearer ' + key, 'X-Pi-Contract': '2'})
class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None
opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())
try:
    with opener.open(req, timeout=30) as r:
        raw = r.read(4 * 1024 * 1024 + 1)
        if len(raw) > 4 * 1024 * 1024:
            raise SystemExit('响应过大')
        data = json.loads(raw)
except urllib.error.HTTPError as e:
    raise SystemExit('HTTP ' + str(e.code))
except Exception:
    raise SystemExit('连接失败或响应无效；未输出原始内容')
print('schemaVersion:', data.get('schemaVersion') == 1)
if path == 'capabilities':
    print('pi-usage-cpa:', data.get('pluginId') == 'pi-usage-cpa')
else:
    accounts = data.get('accounts', [])
    print('accounts:', len(accounts))
    print('stale:', data.get('cache', {}).get('stale') is True)
    for index, account in enumerate(accounts, 1):
        print('account', index, 'hasError:', bool(account.get('error')),
              'missingWindows:', len(account.get('missingWindows', [])))
        for group in account.get('groups', []):
            family, window, source = (group.get(k) for k in ('modelGroup', 'window', 'source'))
            remaining = group.get('remainingFraction')
            if family not in ('gemini', 'claude-gpt') or window not in (None, '', '5h', '7d') or source not in ('summary', 'fallback'):
                print('unknown group (details withheld)'); continue
            if not isinstance(remaining, (int, float)) or not 0 <= remaining <= 1:
                print('invalid fraction'); continue
            print(family, window or 'window unknown', 'remaining:', remaining,
                  'source:', source, 'hasResetTime:', bool(group.get('resetTime')))
PY
```

该验证脚本需要交互终端（`/dev/tty`），不要在无终端的 CI 中运行，也不要把密钥硬编码到脚本。

- 404：先查插件是否成功加载、plugin ID 与文件名；新客户端可能回退旧插件。
- 401：普通 key 校验失败，或服务器管理凭据/loopback 管理连接失败；插件 fail closed。先在服务器本地检查配置，不发送密钥。
- summary 403：不能仅凭此断定缺少 project；按成功的后台请求在服务器本地比较项目上下文和必要请求参数。
- summary 429：保留失败信息与未标窗口的降级，不声称已读取双窗口。
- 只有 fallback / 有 missingWindows：还不能认定双窗口成功。需本地核对原始响应的结构及组名，不上传原始响应。

## 6. 客户端仍需更新

已发布的 `@wayner6/pi-usage@0.3.0` 不访问新路由。此次上传只有服务端仓库，Pi Usage 客户端补丁仍在开发机 `/home/piagent/github/pi-usage`，未推送、未发布。

开发机可以沿用现有本地 Pi Usage 项目安装方式使用修改后的文件；其他机器暂不能通过 npm 安装本补丁。若需要可复制的客户端 Git 安装命令，必须先另行批准将客户端补丁提交/推送到 `wayner6/pi-usage`；本服务端仓库不假装包含客户端更新。

## 7. 回滚

保留旧 `pi-bridge`；在备份确认后只禁用 `plugins.configs.pi-usage-cpa.enabled`，按现有运维方式重载，或恢复本次变更前配置和环境。新客户端在新路由 404 时回退旧路由；如果禁用后路由返回其他状态，则不会回退，需要使用旧客户端。

不要删除全部插件目录、修改其他 provider 或卸载旧插件。回滚同样涉及服务重载与在途请求，请安排变更窗口。插件不改账户文件、不改模型调度。

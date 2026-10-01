# 自部署

## 系统配置

复制 `.env.example` 为 `.env`。实际 `.env` 不应进入版本控制；建议执行 `chmod 600 .env`。本项目不提供生产密码。

| 配置 | 用途 |
| --- | --- |
| `POSTGRES_PASSWORD` | Compose 必填。推荐 `openssl rand -hex 32`；如使用 URL 保留字符，请在 `DATABASE_URL` 中填写经过 URL 编码的密码 |
| `POSTGRES_USER` / `POSTGRES_DB` | Compose 数据库账号和数据库，默认 `river` |
| `POSTGRES_PORT` | PostgreSQL 本机端口，默认 `5432`，仅绑定 `127.0.0.1` |
| `BASE_URL` | 用户访问的完整外部地址；生产使用 `https://域名`，同时决定 OAuth callback 与会话 cookie 的 Secure 标志 |
| `DOMAIN` | 可选 Caddy HTTPS profile 的域名，不含 `https://` 或路径 |
| `APP_BIND` / `APP_PORT` | Go 服务在宿主机的绑定，默认 `127.0.0.1:8080` |
| `TRUST_PROXY` | 默认 `false`；仅在受信任代理后且后端端口不向公网开放时开启，用于登录限流中的客户端地址 |
| `JOIN_PASSWORD` | 全站统一加入密码，留空表示不要求；不是房间密码，也不是账号密码 |
| `GUEST_ENABLED` | 是否允许访客身份，默认 `true` |
| `VOICE_ENABLED` / `CHAT_ENABLED` / `REACTIONS_ENABLED` | 系统功能开关，默认 `true`；房间设置可进一步关闭 |
| `DEFAULT_VOICE_MODE` | 新房间默认语音模式：`free` 自由发言或 `push-to-talk` 按住说话，默认 `free` |
| `SPECTATOR_VOICE_ENABLED` | 默认 `false`；允许旁观者发言的系统上限，房间设置可进一步关闭 |
| `GITHUB_CLIENT_ID` / `GITHUB_CLIENT_SECRET` | GitHub OAuth App 的真实凭据，两个均填写才启用；只填写一个会拒绝启动 |
| `ICE_SERVERS_JSON` | WebRTC ICE 服务数组，默认 `[]`；会提供给浏览器，因此其中的 TURN 凭据对参与者可见 |
| `DATABASE_URL` | PostgreSQL URL；留空时 Compose 和开发脚本分别构造容器内/本机 URL，直接运行 Go 留空则无持久化 |
| `LISTEN_ADDR` | 直接运行 Go 的监听地址，默认 `:8080`；Compose 固定为 `:8080` |
| `STATIC_DIR` | 直接运行 Go 的前端构建目录；Compose 固定为 `/app/web/dist` |

未提供 `DATABASE_URL` 的直接运行仅使用开发内存存储，重启不会保留数据。正式部署应始终连接 PostgreSQL。Compose 使用容器内部数据库连接，数据库、用户头像和房间数据位于 `postgres_data` 持久卷；不存在需要单独备份的头像上传目录。

Compose 自定义 `DATABASE_URL` 时，内置 PostgreSQL 主机为 `db:5432`；开发脚本则使用 `127.0.0.1:POSTGRES_PORT`。数据库用户和库名建议使用简单字母数字名称。默认构造的 URL 使用未编码的配置值，因此推荐使用示例的十六进制随机密码。

`.env` 同时可由开发脚本作为受信任的 shell 文件读取，请保持示例中的格式，含空格或 JSON 的值使用单引号。避免把不受信任的文本当作 `.env` 执行。

## HTTPS 与手机访问

将域名的 DNS 指向服务器，开放 TCP 80/443；如需 HTTP/3，可同时开放 UDP 443。在 `.env` 填写你的真实域名：

```dotenv
BASE_URL=https://你的域名
DOMAIN=你的域名
APP_BIND=127.0.0.1
TRUST_PROXY=true
```

启动自带的可选 Caddy 反向代理：

```bash
docker compose --profile https up -d --build
docker compose --profile https ps
```

Caddy 自动申请和续期证书，反向代理 HTTP 与 WebSocket，证书状态保存在 `caddy_data`。域名、DNS 和公网端口必须实际可用；本机体验不需要该 profile。

如果使用已有 Nginx、Caddy 或其他代理，将所有路径转发到 `127.0.0.1:8080`，保留 WebSocket Upgrade，使用足够长的 WebSocket 空闲超时，并把 `BASE_URL` 设置为用户实际访问的 HTTPS 地址。Go 根据这个配置设置 Secure cookie，不能仅靠反向代理头来替代正确配置。

手机和平板使用同一个 HTTPS 域名访问。局域网临时查看界面可设置 `APP_BIND=0.0.0.0`，把 `BASE_URL` 改为局域网实际地址，然后重建容器；浏览器通常不会在普通局域网 HTTP 地址允许麦克风，完整语音体验应使用 HTTPS。

## GitHub OAuth

在自己的 GitHub Settings → Developer settings → OAuth Apps 新建 OAuth App：

- Homepage URL：你配置的 `BASE_URL`。
- Authorization callback URL：`BASE_URL` 加 `/api/auth/github/callback`。
- 将生成的 Client ID 和 Client Secret 分别填入 `.env` 对应配置。

例如本机 callback 是 `http://localhost:8080/api/auth/github/callback`。本机和正式域名通常使用各自的 OAuth App。重新创建 `app` 容器后，登录页才会启用 GitHub 登录。全站加入密码也适用于 OAuth 登录。

## 语音与 TURN

语音使用 WebRTC，房间中的其他参与者通过信令交换 ICE 候选。麦克风授权只在 HTTPS 或 localhost 等安全上下文可用。系统 `VOICE_ENABLED=false` 会关闭语音；房主也可以在房间设置中关闭语音，或选择自由发言/按住说话。旁观者发言需要系统和房间同时允许。

ICE 数组可配置 STUN 和 TURN。下面的主机、用户名和密码是格式占位内容，必须替换为你实际管理的服务：

```dotenv
ICE_SERVERS_JSON='[{"urls":["stun:你的TURN域名:3478"]},{"urls":["turn:你的TURN域名:3478?transport=udp","turn:你的TURN域名:3478?transport=tcp"],"username":"你的TURN用户","credential":"你的TURN密码"}]'
```

只使用 STUN 无法覆盖对称 NAT 等网络。自建 TURN 可以使用 coturn，参考 [coturn 官方文档](https://github.com/coturn/coturn)。一个独立服务器的配置骨架如下，填入实际地址与强随机密码后再运行：

```ini
listening-port=3478
fingerprint
lt-cred-mech
realm=你的TURN域名
user=你的TURN用户:你的TURN强随机密码
external-ip=服务器公网IPv4
min-port=49160
max-port=49260
no-cli
no-multicast-peers
denied-peer-ip=0.0.0.0-0.255.255.255
denied-peer-ip=10.0.0.0-10.255.255.255
denied-peer-ip=127.0.0.0-127.255.255.255
denied-peer-ip=169.254.0.0-169.254.255.255
denied-peer-ip=172.16.0.0-172.31.255.255
denied-peer-ip=192.168.0.0-192.168.255.255
denied-peer-ip=::1
denied-peer-ip=fc00::-fdff:ffff:ffff:ffff:ffff:ffff:ffff:ffff
denied-peer-ip=fe80::-febf:ffff:ffff:ffff:ffff:ffff:ffff:ffff
```

将其保存为服务器上的 `/etc/coturn/turnserver.conf` 并限制读取权限。在 Linux 服务器上可以使用 coturn 镜像和 host 网络，或部署发行版的 coturn 服务：

```bash
docker run -d --name river-turn --restart unless-stopped \
  --network host \
  --mount type=bind,src=/etc/coturn/turnserver.conf,dst=/etc/coturn/turnserver.conf,readonly \
  coturn/coturn -c /etc/coturn/turnserver.conf
```

开放 TCP/UDP 3478 以及配置的 UDP 中继端口范围。服务器位于 NAT 后时，需按 coturn 文档配置公网/内网 IP 映射和相应端口转发。需要 `turns:` 时另配置证书和 TLS 监听端口；HTTPS 网站证书不会自动成为 TURN 证书。不要将服务器管理端口放入开放范围。

当前配置支持静态 TURN 凭据，尚未实现服务器动态签发短时 TURN 凭据。应使用专用账号、限制带宽/配额、定期轮换，并避免把该账号用于其他服务。是否需要中继由实际网络决定，应至少用两台不同网络的设备验证通话。

WebRTC 使用浏览器之间的 mesh 连接，每桌最多同时连接 9 名语音参与者。旁观者语音默认关闭；开启后同样占用语音参与者名额。

## 更新、健康检查与备份

更新代码后重建镜像：

```bash
docker compose up -d --build
```

使用 HTTPS profile 时也在更新命令中保留 `--profile https`。Go 镜像的 `/healthz` 健康检查用于确认服务与数据库就绪。查看运行状态和服务日志：

```bash
docker compose ps
docker compose logs --tail=100 app
```

数据库 schema 在启动时初始化。升级前备份，涉及多人正在游戏的升级应先通知玩家并停止新牌局。

```bash
mkdir -p backups
docker compose exec -T db sh -c 'pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Fc' > backups/river.dump
```

恢复前停止 `app`，确保目标数据库与备份匹配：

```bash
docker compose stop app
docker compose exec -T db sh -c 'pg_restore -U "$POSTGRES_USER" -d "$POSTGRES_DB" --clean --if-exists' < backups/river.dump
docker compose up -d app
```

备份包含私人手牌、会话与账号数据，应限制备份访问并另存到安全位置。备份 `.env` 时使用单独的安全位置。`docker compose down` 保留数据卷；`docker compose down -v` 会删除数据库卷与代理证书卷，请勿用作普通更新命令。

## GitHub Actions 持续部署（us1）

[工作流](../.github/workflows/ci-cd.yml) 对 main 的 PR 执行部署脚本单元测试、Compose 校验、Go test/vet、前端及生产镜像构建，以及隔离 PostgreSQL 上的发布/失败回滚演练。main push 和 main 上的手动运行通过全部检查后，把完整 SHA 标记的镜像传到 `root@us1.skyli.xyz`，公开地址为 https://river.skyli.xyz 。PR 不执行上传步骤。可在 [Actions 页面](https://github.com/li-sky/river-code/actions/workflows/ci-cd.yml) 选择 **Run workflow → main**，或运行：

```bash
gh workflow run ci-cd.yml --repo li-sky/river-code --ref main
```

生产文件位于 `/opt/river`；`.env` 和 OAuth 凭据只保留在服务器。GitHub Actions 使用 `RIVER_DEPLOY_SSH_KEY` 和 `RIVER_DEPLOY_KNOWN_HOSTS` 两个 Secret，禁止用管理员现有私钥替代。专用 ed25519 公钥在 root 的 `authorized_keys` 中配置 `restrict,command="/usr/bin/python3 /opt/river/cd/cd.py receive"`，只接受 `deploy <完整 SHA>` 和 `status <完整 SHA>`；无交互 shell、PTY 或转发。known_hosts 来自通过既有可信管理员连接读取的服务器公钥，主机验证保持开启。

首次安装使用现有管理员 SSH 连接，在服务器的源码目录运行 `bash scripts/install-cd.sh`。它要求已存在当前生产布局、Python 3.12+ 和指定 Nginx upstream，安装 root 私有接收器、维护响应与 `river-deploy.timer`。生成专用密钥、添加受限公钥和设置 GitHub Secrets 属于一次性运维配置。后续修改发布器也须由管理员重新安装；Actions 上传应用版本不会自动替换 root 发布器。

接收器验证镜像 revision 标签，下载固定仓库的对应源码，持锁写入 `pending-release.json`。服务器每分钟检查全部公开及私人房间快照；有在线玩家或未结束的手牌就暂缓。空闲时临时返回 503、再次检查、停止应用、生成并校验 `pg_dump -Fc` 备份，再切换 `current` 与镜像。维护期间仅 `/healthz` 继续转发，容器就绪、本机及公网检查均通过后才开放页面与 WebSocket 并更新 `deployed-version`。只有 app 被重建，数据库容器与生产卷保留。

失败会恢复旧源码和镜像并记录失败。进程中断留下事务记录，下一次定时运行尝试恢复旧版；两版都不健康时保留维护响应供管理员处理。备份为 `/opt/river/backups/*.dump`，目录 0700、文件 0600，包含私人牌局与会话；不自动上传、清理或恢复数据库。涉及不兼容 schema 的变更仍需单独制定迁移和恢复计划。旧镜像及源码也保留，管理员按容量安排清理。

```bash
ssh root@us1.skyli.xyz 'python3 /opt/river/cd/cd.py status'
ssh root@us1.skyli.xyz 'cat /opt/river/deployed-version; systemctl status river-deploy.timer --no-pager'
ssh root@us1.skyli.xyz 'journalctl -u river-deploy.service -n 80 --no-pager'
```

Actions 最多等待发布 10 分钟；仍有玩家在线时会在运行摘要标明 **queued**，服务器继续自动等待空闲。绿色检查不代表排队版本已经上线。新的 main 可以替代旧队列；发布前 Actions 跳过已被更新 main 替代的版本。暂停队列处理使用 `systemctl stop river-deploy.timer` 并等待正在执行的 service 结束，不要直接改当前事务文件。人工回退时先暂停定时器、核对房间空闲及数据库兼容性，再切换到已验证的旧源码/镜像并重新检查健康；不要把 `docker compose down -v` 用于更新。

这是单实例短暂重启，要求房间无人在线且没有进行中手牌；玩家即使只是停留在牌桌也会阻止发布。尚未实现在线手牌边界热更新、零停机、玩家版本通知或语音无缝迁移。隔离演练入口（会创建并删除专用测试项目及其测试卷）：

```bash
python3 -m unittest discover -s scripts -p 'test_cd.py' -v
python3 -m venv /tmp/river-cd-check
/tmp/river-cd-check/bin/pip install 'websockets==15.0.1'
/tmp/river-cd-check/bin/python scripts/cd_acceptance.py --image river:local
```

## 部署验收

以下脚本会创建玩家和房间，恢复验收还会重启服务。请对使用独立数据库的验收实例运行，从项目根目录执行。需要 Python 3.11+，浏览器验收使用 Playwright Chromium；媒体单元测试需要已安装的前端 npm 依赖。

```bash
python3 -m venv /tmp/river-verify-venv
/tmp/river-verify-venv/bin/pip install websockets playwright
/tmp/river-verify-venv/bin/python -m playwright install --with-deps chromium
export RIVER_TEST_URL=http://localhost:8080
export RIVER_ARTIFACT_DIR=/tmp/river-browser-artifacts
/tmp/river-verify-venv/bin/python scripts/smoke.py
/tmp/river-verify-venv/bin/python scripts/browser_check.py
./scripts/test-media.sh
```

如果验收实例配置了全站加入密码，将同一个值提供给测试进程的 `JOIN_PASSWORD` 环境变量。浏览器验收使用模拟麦克风验证浏览器连接与交互，截图保存到 `RIVER_ARTIFACT_DIR`，实际设备与公网 TURN 通话仍需分别验收。

重启恢复验收分为准备和验证两个步骤。下面的 Compose 命令应指向验收实例，而不是正在使用的正式数据库；直接运行 Go 时替换为你自己的进程重启方式。

```bash
task_recovery_fixture="$(mktemp /tmp/river-recovery.XXXXXX)"
/tmp/river-verify-venv/bin/python scripts/recovery_check.py prepare "$task_recovery_fixture"
docker compose restart app
docker compose up -d --wait app
/tmp/river-verify-venv/bin/python scripts/recovery_check.py verify "$task_recovery_fixture"
```

恢复脚本检查会话、私人手牌、筹码、行动时间与轮次信息，成功后删除临时文件。该临时文件含测试会话与手牌，应保持私有；失败后也应手工清理。

## 运行边界

当前服务部署为单实例；同一套房间不能同时由多个独立 Go 实例处理。PostgreSQL 保存房间最新快照用于服务重启恢复，部署前应针对自己的备份、恢复和网络环境进行演练。目前没有保存每手完整历史或提供历史回放页面。规则模块与传输、存储分开，后续规则变体仍需要对应实现与测试。

音效由浏览器生成，无需外部音频 CDN。头像上传保存到数据库；Gravatar 与 GitHub 头像需要浏览器能访问相应服务。浏览器有时会限制后台标签页的音频和麦克风，移动端切换应用可能导致连接暂停。

## 受管开发环境中的镜像构建

普通自部署直接 `docker compose build`。如果开发环境注入 HTTPS 代理 CA，Dockerfile 支持可选 BuildKit secret，保持 TLS 校验启用：

```bash
docker build \
  --secret id=proxy_ca,src="$CODEX_PROXY_CERT" \
  --secret id=system_ca,src=/etc/ssl/certs/ca-certificates.crt \
  -t river:local .
docker compose up -d --no-build
```

这两个 CA 只在依赖下载步骤挂载，不写入应用镜像。生产服务器无需提供开发环境代理变量或证书。

# RIVER 德州扑克

自部署的多人 No-Limit Texas Hold’em 现金桌，使用整数娱乐筹码。Go 服务负责发牌、动作校验、计时与结算；React + TypeScript 页面适配手机竖屏、平板和桌面横屏；PostgreSQL 保存身份、头像、房间和牌局数据。

术语约定：**配置**属于系统，**设置**属于用户或房间。**host** 是房主，**guest** 是加入房间的人；未登录账号的临时身份称为**访客**。

## 文档与 SDD

需求、模块规格和页面交互集中在 [RIVER 项目文档](https://chatgpt.com/space/page_4a6ca15ed15c8191a7ab8d94079c1b56)。后端每个模块有对应规格，前端按入口、大厅和牌桌分别维护；用户设置与媒体互动单独记录。

源码与文档的对应关系见 [文档索引](docs/INDEX.md)，开发约定见 [AGENTS.md](AGENTS.md)。朋友试玩反馈、统一热重载和安全更新流程在文档中标为待实现方案。

## 启动

需要 Docker Engine 和 Docker Compose v2。

```bash
cp .env.example .env
openssl rand -hex 32
```

把生成的随机值填写到 `.env` 的 `POSTGRES_PASSWORD`，按需填写全站 `JOIN_PASSWORD`。然后：

```bash
docker compose up -d --build
docker compose ps
```

本机打开 <http://localhost:8080>。默认只在本机监听，正式部署及手机访问见 [部署说明](docs/DEPLOYMENT.md)。`POSTGRES_PASSWORD` 不能为空；GitHub OAuth 密钥需要你在自己的 GitHub 账号中创建，项目不包含共享密钥。

## 开始一桌

1. 使用访客、自建账号或已配置的 GitHub OAuth 登录。系统配置了全站加入密码时先填写密码。
2. 房主创建房间，设置小盲、大盲、买入筹码、席位数和行动时间；其他玩家加入同一房间。
3. 玩家选择空位坐下，至少两人后由房主开始一手。下注金额使用整数筹码，“加注到”是本轮累计下注额。
4. 房主在两手之间调整房间设置或玩家手上的筹码，然后开始下一手。旁观者可以先进入房间再坐下。

牌桌支持过牌、跟注、弃牌、加注、all-in、边池及分池。行动超时自动过牌或弃牌；断线后可以重连。房主权限、手牌和下注由服务器校验，客户端只显示自己的底牌及允许公开的摊牌信息。

用户设置包含声音、音量、语音静音和头像 emoji；头像可以上传或使用 Gravatar。房间设置控制语音、自由发言/按住说话、旁观者发言、聊天和定向 emoji reaction，系统配置决定这些功能是否可用。语音需要浏览器授权麦克风，公网环境需要 HTTPS，复杂网络需要 TURN。

旁观者发言由系统配置和房间设置共同授权，默认关闭；每桌最多同时连接 9 名语音参与者。

## 开发与验证

需要 Go 1.23+、Node.js 22+、Docker Compose v2。完成 `.env` 后运行：

```bash
./scripts/run-dev.sh
```

脚本启动 PostgreSQL、构建前端，再启动同源 Go 服务。可使用 `GO_BIN=/path/to/go` 指定 Go 编译器。前端热更新开发可以另开终端运行 `cd frontend && npm run dev`；Vite 会把 `/api` 和 WebSocket 转发到 `localhost:8080`，涉及 OAuth 时应同步调整 `BASE_URL` 和 GitHub callback。

```bash
cd backend
go test ./...
```

```bash
cd frontend
npm ci
npm run build
```

项目还提供真实服务验收、浏览器验收、重启恢复验收和媒体测试：`scripts/smoke.py`、`scripts/browser_check.py`、`scripts/recovery_check.py`、`scripts/test-media.sh`。执行方法见 [部署说明中的验收步骤](docs/DEPLOYMENT.md#部署验收)。

## 结构

| 路径 | 职责 |
| --- | --- |
| `backend/internal/poker` | 规则、合法动作、牌型、边池与结算；独立于 HTTP 和数据库 |
| `backend/internal/server` | 房间生命周期、连接、计时、权限与个性化牌局视图 |
| `backend/internal/identity` | 自建账号、访客、GitHub OAuth、会话与用户设置 |
| `backend/internal/store` | PostgreSQL 持久化与恢复 |
| `backend/internal/config` | 系统配置读取 |
| `frontend/src` | 牌桌 UI、聊天、动画、音效与 WebRTC 语音 |
| `docs/CONTRACT.md` | HTTP/WebSocket 数据契约 |

当前实现聚焦单实例、每桌最多 9 人的娱乐筹码现金桌。尚未提供真实资金支付、锦标赛、跨服务器房间调度或浏览器语音的服务端录音。语音使用玩家之间的 WebRTC 连接，TURN 是独立部署的网络服务。系统配置关闭的功能不能通过房间设置重新开启。

升级、备份、HTTPS、OAuth 和 TURN 配置详见 [部署说明](docs/DEPLOYMENT.md)。

已执行的规则、多人、浏览器和 Docker 检查及复现方法见 [验证记录](docs/VERIFICATION.md)。组件与素材来源见 [说明](docs/CREDITS.md)。

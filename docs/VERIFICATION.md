# 验证记录

2026-10-01 持续部署：Linux 上实际运行 16 项发布器单元测试通过，覆盖在线/手牌暂缓、到达竞态、备份失败、健康失败回滚、中断恢复及受限命令。`scripts/cd_acceptance.py` 在 us1 上使用随机名称的独立 Compose 项目和 PostgreSQL 卷通过：私人房间在线玩家阻止发布、离线进行中手牌阻止发布、空闲后真实备份与版本切换、会话及环境文件保留、故意失败的镜像切回旧应用且维护标记移除；测试项目及其测试卷已清理。生产数据未用于该演练。真实 Actions 和线上版本结果由 [CD 计划](https://github.com/li-sky/river-spec/blob/main/plans/github-cd.md) 保存。

2026-09-30，在实际 Go 服务、PostgreSQL 17 和 Chromium 浏览器上完成以下检查。浏览器验收使用独立会话及模拟麦克风，不使用假牌局或前端自结算。

| 范围 | 已通过的验证 |
| --- | --- |
| 规则 | 全部 2,598,960 种五张牌组合的类别计数；牌型大小、A2345、踢脚、单挑盲注、行动顺序、短筹码及累计加注重新开放 |
| 结算 | 边池、未跟注筹码退回、平分及零头分配；1,000 局固定随机种子的合法行动，逐动作和结算后检查筹码守恒 |
| 服务 | `go test -race ./...`、`go vet ./...`；房主权限、私牌隔离、行动令牌、过期请求、会话注销、超时和保存失败回滚 |
| 数据库 | PostgreSQL 真实连接、迁移、跨连接池的账号/会话/头像/房间存储；实际进程重启后的底牌、行动截止时间、筹码、会话和行动令牌恢复 |
| 页面 | 创建与加入、买入、all-in、房间设置、聊天、远端头像 emoji 气泡、定向 reaction、个人设置不覆盖 emoji、房主移出与语音清理 |
| 账号 | 自建账号注册、退出、重新登录、昵称及音量保存、页面刷新、头像上传及 PNG 标准化；统一加入密码和 OAuth 状态防重放测试 |
| 设备布局 | 桌面、平板、375/390 像素手机竖屏；九名玩家真实入座，无横向溢出、头像重叠或裁切；手机底牌在操作区显示 |
| 语音 | 九个独立浏览器同时加入，每端八条已连接的 WebRTC 连接及八路接收音轨；双人重复退出/重入、同时重入、连接恢复、刷新后重新加入；七项生命周期及信令测试 |
| 部署 | 实际多阶段 Docker 镜像构建，连接 PostgreSQL 启动；非 root 用户、只读根文件系统、前端文件服务和就绪探测 |

首次语音协商采用固定身份顺序选出发起方，随后保留协商冲突处理。该方式修复了实际浏览器中同时生成 offer 时 ICE 停滞的问题。九人桌的信令突发限制也已验证，不会把正常的多路协商当成过量请求。

## 复现

2026-10-01 公开／私人房间变更：Windows 本地执行 `go test ./...`、`go vet ./...` 和 `npm run build` 通过；新增服务测试覆盖必填可见性、列表过滤、认证链接加入、房主权限、手牌进行中限制、广播、保存失败回滚和内存快照恢复。隔离的实际 Go 服务使用内存仓储，Chrome 桌面 1440×1000 与手机 390×844 执行 `scripts/visibility_check.cjs` 通过，观察到私人创建、邀请链接登录、跨身份加入、公开／私人切换、5 秒大厅刷新及刷新后设置保持，无横向溢出或浏览器异常。本次没有重新验证真实 PostgreSQL、OAuth 或公网 TURN。

房间浏览器验收需要 Node.js 和 `playwright` 包，默认使用 Chrome；可用 `RIVER_BROWSER_PATH` 指定浏览器可执行文件、`RIVER_TEST_URL` 指定隔离服务、`RIVER_ARTIFACT_DIR` 保存截图，执行 `node scripts/visibility_check.cjs`。可见性必填，不兼容缺少该字段的旧请求和旧快照；旧快照会拒绝启动恢复，不自动迁移。

先连接一个隔离的测试数据库并运行服务；这些脚本会创建测试账号和房间，不应指向使用中的牌桌。Python 脚本需要 Python 3.11+、`websockets` 和 `playwright`；运行浏览器测试前执行 `playwright install chromium`。默认地址为 `http://localhost:8080`，可用 `RIVER_TEST_URL` 覆盖；配置了加入密码时以 `JOIN_PASSWORD` 提供。

```bash
cd backend
go test -race ./...
go vet ./...
```

```bash
cd frontend
npm ci
npm run build
cd ..
./scripts/test-media.sh
python3 scripts/smoke.py
python3 scripts/profile_check.py
python3 scripts/browser_check.py
RIVER_TEST_FULL_VOICE=1 python3 scripts/table_layout_check.py
```

重启验证分两步，临时文件包含测试会话 cookie，应保存在仅自己可读的位置：

```bash
python3 scripts/recovery_check.py prepare /tmp/river-recovery.json
# 重启连接同一数据库的 Go 服务，然后在 120 秒行动截止前执行：
python3 scripts/recovery_check.py verify /tmp/river-recovery.json
```

多项脚本连续运行可能触发认证限流；等待限流窗口结束再执行下一项，不要为了测试关闭正式部署的限流。浏览器截图由 `RIVER_ARTIFACT_DIR` 控制输出位置。

## 尚需部署环境验证

GitHub OAuth 使用可控的模拟提供方验证了状态、令牌交换及用户创建；实际 GitHub 登录需要自己的 Client ID/Secret。真实域名证书、不同网络设备之间的 TURN/NAT 通话及人的主观听感需要在部署环境验证。本次没有将这些外部条件报告为已完成。

当前持久化恢复的是最新房间快照，不包含每一手的完整可回放历史；服务采用单实例运行。

## 房主置顶消息验证（2026-10-01）

在独立 `feat/host-pin-message` worktree 实现，每桌一条房主置顶、替换和取消，规格计划为 [host-pin-message.md](https://github.com/li-sky/river-spec/blob/6e4eb3870a1e3a12ab76fc99b9adcbbc85fb5c01/plans/host-pin-message.md)。

- Windows 原生 Go：`go test ./...` 与 `go vet ./...` 通过；五项 `TestPin*` 测试实际覆盖当前房主、旧连接、跨桌/无效 ID、过期取消、系统/房间开关、限流、牌局中操作、房主移交、聊天裁剪、作者离开、快照恢复、旧字段缺失和保存失败回滚/无广播。
- `npm run build` 通过。`scripts/pin_check.cjs` 在隔离的 `http://localhost:8091` 服务、内存存储及两份 Chrome 会话通过：房主置顶/替换/两处取消、guest 只读、双端同步、刷新、文本转义、聊天滚动，1440×1000 桌面、390×844 手机及 390×667 手机房主布局无横向溢出且输入框可用。执行代理实际查看桌面与手机截图，确认置顶区域可读且有界滚动。
- 本次恢复证据来自内存仓储重新构造服务；未验证真实 PostgreSQL 进程重启。可选 `go test -race ./internal/server` 在当前 Windows 环境因缺少 CGO 无法运行，不登记为通过。未发布或替换既有运行服务。

## 当前可见牌型提示（2026-10-01）

独立 `feat/current-hand-rank` worktree 增加服务端 `HandPlayer.currentHand`，从可见两张底牌与当前公共牌计算最佳五张类别。测试涵盖逐轮更新、私牌隔离、摊牌、弃牌、新手清除、公共牌最佳组合及 A2345；执行入口为 `backend/internal/poker/current_hand_test.go`。

本次本地执行后端 `go test ./...`、`go vet ./...` 及前端 `npm run build`。实际浏览器使用独立内存服务 `127.0.0.1:8093` 与两名合成访客，通过真实 HTTP/WebSocket 验证翻牌前不显示、翻牌/转牌/河牌同步显示、摊牌公开对手、新手和弃牌后标签数量归零。桌面 1440×1000、手机 390×844 和 320×740 已检查；手机底牌区与座位一致，提示为 14px。320px 短屏仍使用原有 sticky 操作区和纵向滚动，滚动至牌桌下方后座位标签完整可见，底牌区同步提示始终随操作区显示。新增标签下方预留 28px 间距，无横向溢出；本次未改造原有 sticky 布局。

代码和规格使用各自独立 worktree，未修改主工作区的并行 UI；未合并或部署，未验证生产数据库、OAuth 或 TURN。最终提交后的检查证据由 [当前牌型计划](https://github.com/li-sky/river-spec/blob/2c7a2ef02aefb0be7258624133b1a75c1245678a/plans/current-hand-rank.md) 后续完成记录保存。Windows 验证使用独立工具目录中的 Go 1.24.0 和调用现有 Node/npm 的本地 npm shim，以适配 SDD 的固定命令。

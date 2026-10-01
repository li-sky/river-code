# 验证记录

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

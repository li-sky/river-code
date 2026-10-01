# RIVER 开发约定

这是使用 Go、PostgreSQL、React 和 TypeScript 的自部署多人 No-Limit Texas Hold’em 娱乐筹码现金桌。

- 配置属于系统；设置属于用户或房间。host 是房主，guest 是加入房间的人；访客账号身份与房间角色分开。
- 先读 [文档索引](docs/INDEX.md)，定位相应模块或页面规格。变更行为先写清预期与验收场景，再实现并同步规格。规格中的已实现、已验证和待实现必须区分。
- `internal/poker` 负责规则和结算，保持独立于 HTTP、身份和数据库；`server` 负责房间、授权、计时和安全视图；`identity` 负责账号和会话；`store` 负责持久化。
- 前端不能判定获胜或结算，也不能获得对手未公开底牌或牌堆。客户端按钮与输入校验不能替代服务端权限和合法动作校验。
- 前端逻辑页面包括入口、大厅和牌桌；弹窗按共享交互规格维护，不因文档拆分而强制重构为独立路由。
- 朋友反馈、统一热重载与安全更新目前为方案。不得仅凭重连、Vite HMR 或快照恢复将这些能力标为完成。
- 不提交真实环境文件、账号凭据、会话、TURN 密钥、上传文件或生成产物。GitHub 仓库访问令牌不能作为应用 OAuth Client Secret。
- 规则、权限、恢复或存储变化验证相关边界；低影响的样式和文案不增加镜像实现的测试。常规检查为后端 `go test ./...` 和前端 `npm run build`；媒体变化运行 `scripts/test-media.sh`。真实 PostgreSQL、GitHub OAuth 和公网 TURN 的证据单独记录。
- 工作流以 [river-spec/workflow.md](https://github.com/li-sky/river-spec/blob/main/workflow.md) 为准；每次改进用一份 `plans/<slug>.md` 记录问题、预期、验收、任务和结果。通过 `scripts/sdd.sh` 执行 new/check/start/run/finish，默认读取同级 river-spec，也可设置 `RIVER_SPEC_DIR`。
- 开始前补充计划并提交规格仓库；代码提交使用 `SDD-Plan: https://github.com/li-sky/river-spec/blob/<spec-commit>/plans/<slug>.md` footer，最终计划保存代码提交和当前验证证据。不得使用会漂移的 main 链接替代交付依据。
- 按风险运行必需检查；人工验收需实际确认并写清证据，不能仅凭测试文件存在或自动检查成功勾选。`finish` 的 done 只表示开发验收完毕，发布状态独立，脚本不会自动部署。
- 代理并行工作要明确文件所有权与接口，集成者负责相关规格、兼容性、差异审阅和最终检查。已有授权范围内直接推进，不把计划变成重复审批流程。
- 外部规格当前不可访问时，明确待同步项及必要假设，结合仓库现有契约继续已授权工作。提交前补齐相关规格与验证；不要把源码缺陷当成产品需求。

# 文档与代码对应索引

规格文档集中维护在 [RIVER 项目文档](https://github.com/li-sky/river-spec)。本仓库保存源码、测试和运行必需的技术说明。规格中的需求、当前实现和待实现方案分开记录；修改相关行为时同步对应规格和验收证据。

| 范围 | 规格文档 | 对应源码 |
| --- | --- | --- |
| config | [RIVER 系统配置模块规格](https://github.com/li-sky/river-spec/blob/main/modules/config.md) | [`backend/internal/config`](../backend/internal/config)、[`.env.example`](../.env.example) |
| identity | [RIVER 身份与用户设置模块规格](https://github.com/li-sky/river-spec/blob/main/modules/identity.md) | [`backend/internal/identity`](../backend/internal/identity) |
| store | [RIVER 数据存储模块规格](https://github.com/li-sky/river-spec/blob/main/modules/store.md) | [`backend/internal/store`](../backend/internal/store) |
| poker | [RIVER 德州扑克规则模块规格](https://github.com/li-sky/river-spec/blob/main/modules/poker.md) | [`backend/internal/poker`](../backend/internal/poker) |
| server | [RIVER 房间与实时服务模块规格](https://github.com/li-sky/river-spec/blob/main/modules/server.md) | [`backend/internal/server`](../backend/internal/server) |
| runtime | [RIVER 服务启动与部署模块规格](https://github.com/li-sky/river-spec/blob/main/modules/runtime.md) | [`backend/cmd/river`](../backend/cmd/river)、[`Dockerfile`](../Dockerfile)、[`compose.yaml`](../compose.yaml)、[`scripts/run-dev.sh`](../scripts/run-dev.sh) |
| workflow | [RIVER SDD 开发工作流](https://github.com/li-sky/river-spec/blob/main/workflow.md) | [开发约定](../AGENTS.md)、[执行入口](../scripts/sdd.sh) |
| shared | [RIVER 用户设置与媒体互动模块规格](https://github.com/li-sky/river-spec/blob/main/pages/04-shared-settings-media.sdd.md) | [`frontend/src/App.tsx`](../frontend/src/App.tsx)、[`frontend/src/lib/api.ts`](../frontend/src/lib/api.ts)、[`frontend/src/lib/types.ts`](../frontend/src/lib/types.ts)、[`frontend/src/lib/sound.ts`](../frontend/src/lib/sound.ts)、[`frontend/src/lib/voice.ts`](../frontend/src/lib/voice.ts)、[`frontend/src/style.css`](../frontend/src/style.css) |
| entry | [RIVER 入口与登录页规格](https://github.com/li-sky/river-spec/blob/main/pages/01-entry-login.sdd.md) | [`frontend/src/App.tsx`](../frontend/src/App.tsx) |
| lobby | [RIVER 大厅页规格](https://github.com/li-sky/river-spec/blob/main/pages/02-lobby.sdd.md) | [`frontend/src/App.tsx`](../frontend/src/App.tsx) |
| table | [RIVER 牌桌页规格](https://github.com/li-sky/river-spec/blob/main/pages/03-table.sdd.md) | [`frontend/src/App.tsx`](../frontend/src/App.tsx) |

前端目前在 `App.tsx` 中按会话与 `room` 参数显示入口、大厅和牌桌；表格中的三份页面规格对应这三个逻辑页面。用户设置、开房、买入和房间设置是页面内弹窗，归入共享交互规格。

完整表情选择器位于 [`frontend/src/components/EmojiPicker.tsx`](../frontend/src/components/EmojiPicker.tsx)，由 App 中两个表情弹窗按需加载，归入 shared 规格。

手机下注与筹码滑条触觉反馈位于 [`frontend/src/lib/haptics.ts`](../frontend/src/lib/haptics.ts)，由 App 的用户动作触发，归入 table/shared 规格；兼容性和节流边界测试见 [`haptics.test.mjs`](../frontend/src/lib/haptics.test.mjs)，通过 `npm run test:haptics` 或 [`scripts/test-media.sh`](../scripts/test-media.sh) 执行。

机器可读的对应关系保存在 [spec-map.json](spec-map.json)，供代理或工具按源码路径定位规格。规格与代码分仓维护；修改行为时同步相关规格和验收证据。

## 开发步骤

1. 阅读对应规格和相关代码，核对用户已确认的目标。
2. 写明变更后的行为、影响范围及验收场景，再拆分实现任务。
3. 完成代码和必要验证；记录仍未覆盖的外部条件。
4. 同步规格状态和测试证据，关联代码提交与发布状态。

如当前工具无法读取或更新外部文档，在变更说明中列明待同步项，结合已有需求、源码及技术契约继续工作，不把未经确认的新行为写成用户需求。

## 仓库内的技术参考

- [HTTP 与 WebSocket 契约](CONTRACT.md)
- [部署说明](DEPLOYMENT.md)
- [验证记录](VERIFICATION.md)
- [组件与素材来源](CREDITS.md)

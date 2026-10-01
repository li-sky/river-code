# 文档与代码对应索引

规格文档集中维护在 [RIVER 项目文档](https://chatgpt.com/space/page_4a6ca15ed15c8191a7ab8d94079c1b56)。本仓库保存源码、测试和运行必需的技术说明。规格中的需求、当前实现和待实现方案分开记录；修改相关行为时同步对应规格和验收证据。

| 范围 | 规格文档 | 对应源码 |
| --- | --- | --- |
| config | [RIVER 系统配置模块规格](https://chatgpt.com/space/page_bdf75f5d1ec88191b4d3ab2b19773e1e) | [`backend/internal/config`](../backend/internal/config)、[`.env.example`](../.env.example) |
| identity | [RIVER 身份与用户设置模块规格](https://chatgpt.com/space/page_282de7bc763c8191b0d71200916acca1) | [`backend/internal/identity`](../backend/internal/identity) |
| store | [RIVER 数据存储模块规格](https://chatgpt.com/space/page_c73d96c1ac808191990acd4d4869847f) | [`backend/internal/store`](../backend/internal/store) |
| poker | [RIVER 德州扑克规则模块规格](https://chatgpt.com/space/page_045de64103808191aed0be3a20c152ab) | [`backend/internal/poker`](../backend/internal/poker) |
| server | [RIVER 房间与实时服务模块规格](https://chatgpt.com/space/page_8bc1b9ca2cac8191b315c3c2248d2c78) | [`backend/internal/server`](../backend/internal/server) |
| runtime | [RIVER 服务启动与部署模块规格](https://chatgpt.com/space/page_f2c9cd8afa6c81919b3d4ccfd0a84af2) | [`backend/cmd/river`](../backend/cmd/river)、[`Dockerfile`](../Dockerfile)、[`compose.yaml`](../compose.yaml)、[`scripts/run-dev.sh`](../scripts/run-dev.sh) |
| workflow | [RIVER SDD 开发与朋友试玩迭代方案](https://chatgpt.com/space/page_f0e4fb91693c8191ab52d5f6a70ffd3f) |  |
| shared | [RIVER 用户设置与媒体互动模块规格](https://chatgpt.com/space/page_7e2fb267c25c8191bcd8c8e9ab572f62) | [`frontend/src/App.tsx`](../frontend/src/App.tsx)、[`frontend/src/lib/api.ts`](../frontend/src/lib/api.ts)、[`frontend/src/lib/types.ts`](../frontend/src/lib/types.ts)、[`frontend/src/lib/sound.ts`](../frontend/src/lib/sound.ts)、[`frontend/src/lib/voice.ts`](../frontend/src/lib/voice.ts)、[`frontend/src/style.css`](../frontend/src/style.css) |
| entry | [RIVER 入口与登录页规格](https://chatgpt.com/space/page_dec70bab8a4c81919be0413ff865c5dd) | [`frontend/src/App.tsx`](../frontend/src/App.tsx) |
| lobby | [RIVER 大厅页规格](https://chatgpt.com/space/page_752a9896c66081918d36d5384425c03f) | [`frontend/src/App.tsx`](../frontend/src/App.tsx) |
| table | [RIVER 牌桌页规格](https://chatgpt.com/space/page_5fad07bf78148191a238b6159eee5a1e) | [`frontend/src/App.tsx`](../frontend/src/App.tsx) |

前端目前在 `App.tsx` 中按会话与 `room` 参数显示入口、大厅和牌桌；表格中的三份页面规格对应这三个逻辑页面。用户设置、开房、买入和房间设置是页面内弹窗，归入共享交互规格。

机器可读的对应关系保存在 [spec-map.json](spec-map.json)，供代理或工具按源码路径定位规格。文档和私有仓库的访问权限独立；文档链接不授予代码访问权限。

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

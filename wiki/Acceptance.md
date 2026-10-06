# 2026-10-07 完整 SOP 验收

本次使用真实个人公有仓库及组织私有仓库，按 **Use this template → clone → 本地开发 → commit/push → GitHub Actions → GHCR → VPS → Cloudflare HTTPS** 完成验收。没有为新应用修改 DNS、Traefik或默认CI/CD。测试结束通过Actions删除测试容器与本地应用镜像，保留示例仓库、数据卷、加密备份和历史。

## 新建项目和本地开发

| 仓库 | 不可变repository ID | 默认模板首次部署 | 应用开发push |
|---|---|---|---|
| [RyanStanLin/sop-stateful-aspnet](https://github.com/RyanStanLin/sop-stateful-aspnet)（公有） | 1407662143 | [37500331043](https://github.com/RyanStanLin/sop-stateful-aspnet/actions/runs/37500331043) | [37502074028](https://github.com/RyanStanLin/sop-stateful-aspnet/actions/runs/37502074028)，commit `2ddd58c8a7c1303056b2630969243f50b11a1b46` |
| [Deploy-ryanl-in/sop-route-peer](https://github.com/Deploy-ryanl-in/sop-route-peer)（私有，需仓库权限） | 1407669275 | [37501073191](https://github.com/Deploy-ryanl-in/sop-route-peer/actions/runs/37501073191) | [37502162098](https://github.com/Deploy-ryanl-in/sop-route-peer/actions/runs/37502162098)，commit `1ca78276773c07525e2ff0d89b3ddba673b34b66` |

两个仓库均通过真实模板界面创建，并SSH clone到开发机；默认workflow未修改。ASP.NET本地锁定依赖还原、格式验证、5项测试通过，应用本地启动、JSON读写和WebSocket回传通过。当前Mac没有运行Docker，因此本地PostgreSQL/Redis Compose整体集成在GitHub托管Linux runner验证；不能把它称为在该Mac上运行了Docker。

最新ASP.NET模板提供完整本地Compose环境，集成测试实际启动PostgreSQL和带密码的Redis，运行生产编译的应用并验证数据库/缓存/JSON/API令牌/WebSocket。[37509788380](https://github.com/Deploy-ryanl-in/template-aspnet/actions/runs/37509788380)通过；模板commit `ffd121f8a13bf8a57511f921c736b80916d5b7d3`。Next.js更新后的CI通过[37505297269](https://github.com/Deploy-ryanl-in/template-nextjs/actions/runs/37505297269)。

## 线上数据与容器管理

实际HTTPS验证：PostgreSQL和Redis PUT/GET、JSON配置PUT/GET、带令牌认证、WebSocket回传。Redis未认证请求返回NOAUTH，数据库/缓存没有宿主机公开端口。Web的128 MiB内存、0.5 CPU及128 PID限制实际生效。

| 手动Actions操作 | 成功run | 数据结果 |
|---|---|---|
| stop | [37503064567](https://github.com/RyanStanLin/sop-stateful-aspnet/actions/runs/37503064567) | 容器保留且停止、路由404；控制器重启后仍停止 |
| start | [37503504702](https://github.com/RyanStanLin/sop-stateful-aspnet/actions/runs/37503504702) | 原容器ID重新启动，JSON原值保留 |
| restart | [37503792710](https://github.com/RyanStanLin/sop-stateful-aspnet/actions/runs/37503792710) | JSON、PostgreSQL、Redis原标记值一致 |
| delete | [37504189888](https://github.com/RyanStanLin/sop-stateful-aspnet/actions/runs/37504189888) | 容器删除，3个数据卷保留 |
| cleanup-images | [37506307727](https://github.com/RyanStanLin/sop-stateful-aspnet/actions/runs/37506307727) | 仓库本地应用镜像全部清理 |
| start（删除容器与镜像后） | [37506699662](https://github.com/RyanStanLin/sop-stateful-aspnet/actions/runs/37506699662) | 从GHCR重拉并重建，三个数据源原标记完全一致 |
| backup | [37509093006](https://github.com/RyanStanLin/sop-stateful-aspnet/actions/runs/37509093006) | PostgreSQL逻辑备份、认证Redis快照及JSON冷备成功 |
| restore | [37510008442](https://github.com/RyanStanLin/sop-stateful-aspnet/actions/runs/37510008442) | 先改写三个数据源，再恢复到新卷；均恢复备份前原值，Worker继续运行 |
| download-backup | [37510395261](https://github.com/RyanStanLin/sop-stateful-aspnet/actions/runs/37510395261) | `encrypted-backup`短期artifact生成，包含age密文 |

初次镜像清理[37504507773](https://github.com/RyanStanLin/sop-stateful-aspnet/actions/runs/37504507773)发现Docker29在RepoTags和RepoDigests重复返回同一digest，造成重复删除失败；已修复去重并增加保护在用、其他仓库和保留版本的回归测试，上表为修复后的真实成功重试。

## 配置热更新与共享 HTTP/WSS

默认仓库名域名 → `custom-sop.ryanl.in` → `endpoint.ryanl.in`，均通过`paas.json` commit/push更新，旧域名停止路由。四服务栈为Web128、PostgreSQL192、Redis64、Worker128 MiB，另一个Web128 MiB；总640 MiB，更新Web候选时最高768 MiB。数据库192 MiB为本次小型验收调整，模板默认仍256 MiB。

1. 前缀过滤验证：`/api/adduser`、`/api/addapp`及`/api/app/status`200，过滤外请求404；WSS `/ws`和`/ws/app`均通过。
2. 组织仓库改`paths`为`/api/*`、`/ws/*`：[37508645833](https://github.com/Deploy-ryanl-in/sop-route-peer/actions/runs/37508645833)成功；通配路径实际生效。
3. 两个服务都省略paths：[个人37510337338](https://github.com/RyanStanLin/sop-stateful-aspnet/actions/runs/37510337338)、[组织37509796853](https://github.com/Deploy-ryanl-in/sop-route-peer/actions/runs/37509796853)成功。每个请求接收方为2；只有一个定义的业务接口200，共同定义的`/api/hello`和`/healthz`409 `multi_service_conflict`/“多服务冲突”，都未定义的接口404。
4. 两服务同时接受`/ws`握手返回HTTP409；仅一个接受的`/ws/app`正常双向回传，连接保持65秒后仍可回传。
5. Actions summary及warning实际列出两个仓库、服务名和all paths；签名专属健康检查仍可分别验证每个服务，不被共享`/healthz`冲突掩盖。

个人最终配置commit `f694bcf6b7bbeda43729bca2c2a409519c7c7bd1`；组织最终配置commit `46ab5da06a8f9c335d1f154befe6b476b9a6aa7a`。一次较早push[37509765793](https://github.com/RyanStanLin/sop-stateful-aspnet/actions/runs/37509765793)被随后触发的恢复任务版本屏障拒绝为`stale workflow run`；等恢复结束重新push后成功，没有绕过版本保护。

## 基础设施与可复用性

平台安装版本 `d407bd338027aa8375770273220d58285cb7b3c4` 的CI[37508331556](https://github.com/Deploy-ryanl-in/personal-paas/actions/runs/37508331556)通过，安装包SHA256核验、VPS升级及doctor通过。Go race/vet、严格Schema/域名共享授权、HTTP/WS冲突、健康探针、暂停状态及镜像清理测试通过。WebSocket后端重定向不跟随，响应头及消息有上限。

实际从最新固定模板下载源码，生成另一域名、控制域名、平台仓库、个人账号及组织配置；两个模板的不可变workflow pin、域名、账号说明、Actions操作和秘密排除核验通过，actionlint通过。虚拟账号仅用于生成器测试，没有授予真实访问权限。此前隔离Debian从零安装、重装保留数据/身份和真实TLS验收见[历史验收](https://github.com/Deploy-ryanl-in/personal-paas/blob/main/docs/acceptance.md)。

本次前后Cloudflare所有DNS记录及代理状态一致。3x-ui PID179399、启动时间2026-10-02 22:07:16 CST，Xray PID179492和配置SHA256均未改变，原端口保持独立直连；没有迁移或重启这些服务。

## 最终测试资源清理

- 旧个人及组织示例首先通过Actions stop，旧测试容器与13个测试镜像清理后再进行本次验收。
- 新个人测试栈：[delete 37510999972](https://github.com/RyanStanLin/sop-stateful-aspnet/actions/runs/37510999972)、[cleanup-images 37511260773](https://github.com/RyanStanLin/sop-stateful-aspnet/actions/runs/37511260773)成功。
- 新组织测试栈：[delete 37510958092](https://github.com/Deploy-ryanl-in/sop-route-peer/actions/runs/37510958092)、[cleanup-images 37511102891](https://github.com/Deploy-ryanl-in/sop-route-peer/actions/runs/37511102891)成功。
- 检查专属rootless Docker无任何容器后，管理员清理了本次测试用的3个共用数据库/辅助镜像。最终该daemon为**0容器、0镜像**；没有清理rootfulDocker。应用workflow的cleanup-images刻意不自动删除共用基础镜像。
- 测试URL现返回404；数据卷、加密备份、GHCR远程版本、示例仓库及历史保留。需要再运行示例时，直接Actions start；首次镜像拉取稍慢，不需要SSH。

共享409表示结果冲突，不撤销服务已经执行的写入。建议生产项目使用明确路径过滤，或确保各服务只定义自己的接口；不得把本次冲突测试配置当作通用业务事务机制。

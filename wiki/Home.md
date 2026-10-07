# Personal PaaS 开发者手册

目标流程：**Use this template → clone/pull → code → commit → push → HTTPS 上线**。GitHub 是应用与部署配置的来源，应用编译由 GitHub 托管 runner 完成，VPS 只运行镜像。

模板：[ASP.NET Core](https://github.com/Deploy-ryanl-in/template-aspnet) · [Next.js](https://github.com/Deploy-ryanl-in/template-nextjs)。当前实例支持 `RyanStanLin` 个人账号和 `Deploy-ryanl-in` 组织下的公有、私有仓库。默认示例不需要应用密钥。

按顺序阅读：

1. [新建项目与本地开发](Getting-started)
2. [部署配置与环境变量](Deployment-configuration)
3. [PostgreSQL](PostgreSQL)
4. [Redis 与 Worker](Redis-and-workers)
5. [JSON 文件与持久卷](Persistent-files)
6. [自定义和共享域名](Domains-and-shared-routing)
7. [路由缓存与清除](Route-cache)
8. [WebSocket](WebSocket)
9. [Actions 操作、备份与恢复](Actions-operations)
10. [排错与容量](Troubleshooting)
11. [从零安装与账号绑定](Installation-and-account-binding)
12. [验收记录](Acceptance)

这份 Wiki 的源文件同时保存在平台仓库 `wiki/`，便于版本审查和迁移。已有 3x-ui/Xray 属于独立基础设施，平台操作不会管理它们。

# Personal PaaS 开发者手册

目标流程：**Use this template → clone/pull → code → commit → push → HTTPS 上线**。第一次使用先读快速开始，再按自己的需求看参数和数据库页。GitHub 是应用与部署配置的来源，应用编译由 GitHub 托管 runner 完成，VPS 只运行镜像。

模板：[ASP.NET Core](https://github.com/Deploy-ryanl-in/template-aspnet) · [Next.js](https://github.com/Deploy-ryanl-in/template-nextjs)。当前实例支持 `RyanStanLin` 个人账号和 `Deploy-ryanl-in` 组织下的公有、私有仓库。默认示例不需要应用密钥。

按顺序阅读：

1. [快速开始：从模板到上线](Quick-start)
2. [完整配置参数](Configuration-reference)
   [环境变量与生产秘密](Environment-and-secrets)
3. [性能对比与压测](Performance)
4. [本地开发补充](Getting-started) · [Next.js项目](Nextjs)
5. [PostgreSQL账号与密码](PostgreSQL)
6. [Redis与Worker](Redis-and-workers)
7. [JSON与持久卷](Persistent-files)
8. [自定义和共享域名](Domains-and-shared-routing)
9. [路由缓存](Route-cache)
10. [WebSocket与订阅](WebSocket)
11. [Actions备份恢复](Actions-operations)
12. [排错与容量](Troubleshooting)
13. [从零安装与账号绑定](Installation-and-account-binding)
14. [功能验收](Acceptance)

这份 Wiki 的源文件同时保存在平台仓库 `wiki/`，便于版本审查和迁移。已有 3x-ui/Xray 属于独立基础设施，平台操作不会管理它们。

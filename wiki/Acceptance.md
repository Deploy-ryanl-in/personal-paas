# 开发者 SOP 与路由缓存验收

本轮使用个人公有仓库和组织私有仓库完成 **Use this template → clone → 本地开发 → commit/push → Actions → GHCR → 自动 HTTPS** 验收。新应用不需要逐个创建 DNS 记录或手工修改 Traefik。开发配置方法见 [新建项目](Getting-started)、[PostgreSQL](PostgreSQL)、[Redis与Worker](Redis-and-workers)、[持久文件](Persistent-files)、[手动操作](Actions-operations) 和 [共享域名](Domains-and-shared-routing)。

## 模板与开发流程

- ASP.NET 模板的锁定依赖还原、格式、单元及接口测试、生产 Docker 构建、PostgreSQL/Redis 集成和 WebSocket 测试通过。[最终模板 CI](https://github.com/Deploy-ryanl-in/template-aspnet/actions/runs/37582081893)
- Next.js 模板的安装、lint、类型检查、测试、生产构建和浏览器冒烟检查通过。[最终模板 CI](https://github.com/Deploy-ryanl-in/template-nextjs/actions/runs/37582118961)
- 个人和组织应用最终的完整 CI/CD 均通过，包含独立缓存清除、镜像发布和部署。Mac 本地应用启动、测试及 JSON/WebSocket 读写通过；该 Mac 没有运行 Docker，本地 Compose 整体集成由 GitHub 托管 Linux runner 验证。
- 模板生成工具支持另一域名、控制域名、平台仓库、个人账号和组织绑定。生成后的工作流保持固定 SHA 引用和手动操作输入，语法检查通过；从零安装和重装验收见仓库中的历史验收文档。

## 数据、生命周期与声明更新

PostgreSQL、带密码 Redis、持久 JSON 的 HTTPS 读写通过，未认证访问被拒绝。stop、start、restart、delete、cleanup-images、backup、restore 和下载加密备份均通过真实 Actions 验证。删除容器与镜像后重新 start，原持久数据仍存在；备份后改写并恢复，原数据得到恢复。

通过配置 push 更换域名，旧域名停止路由，新域名自动生效。路径前缀、通配过滤及省略过滤均已验证。共享请求只有一个服务处理时正常返回；多个非404响应返回409 `multi_service_conflict`，内容为“多服务冲突”。独占 WebSocket 接口能够双向通信，共同接受握手时返回409。

409不会撤销服务已经执行的写入。生产项目应使用明确路径过滤，或保证各服务只定义自己的接口；共享转发不是业务事务机制。

## 共享路由学习缓存

[平台 CI](https://github.com/Deploy-ryanl-in/personal-paas/actions/runs/37581917683) 的竞争检测、静态检查、工作流及安装验证通过，最终控制器和模板已同步。行为说明及操作步骤见 [Route-cache](Route-cache)。

通过外部 HTTPS，配合故意延迟两秒的404服务完成以下实测：

| 验证 | 结果 |
|---|---|
| HTTP冷探测→命中 | 3.156秒→1.123秒；最终版本复验3.393秒→1.167秒 |
| 请求复制 | 命中时仍有两个接收方，另一服务计数确认收到请求 |
| 迟到冲突 | 当前命中先返回200，随后清缓存；下一次完整探测返回409 |
| WebSocket冷握手→命中 | 3.211秒→1.257秒，实际echo成功 |
| 手动清除 | Actions清除指定域名后，下一次请求为miss |
| 子域名隔离 | 清除一个域名，另一个已预热的域名仍为hit |
| 失败push | 故意提交无效声明使校验失败、构建与部署跳过；独立清除job成功，只影响已授权域名 |

耗时包含客户端网络开销，不是内部延迟承诺。故意无效声明已恢复，个人和组织仓库随后完整部署成功。缓存键隔离、容量上限、清除前请求不能重新学习、越权拒绝和错误回退有自动化回归测试。

## 测试资源清理

两个测试应用均通过 Actions delete 删除容器与路由，组织应用的 Actions cleanup-images 成功。个人镜像清理的浏览器表单未提交；管理员确认专属应用 Docker 已无容器及执行中任务后，清理剩余测试镜像。最终专属运行环境为 **0容器、0镜像**，持久数据卷、加密备份、GHCR版本和部署历史保留。需要运行示例时可以使用 Actions start。

现有独立基础设施保持原状，DNS未为本轮测试新增记录。详细诊断记录留在本地私有验收资料中。

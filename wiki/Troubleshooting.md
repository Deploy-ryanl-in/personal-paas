# 排错与容量

先看应用Actions失败job及部署summary，再用PaaS operations的status、history、logs（service填写web/postgres/redis/worker）。不要把含秘密的完整应用日志发到公开Issue。

| 现象 | 检查 |
|---|---|
| `missing selected secret` | PAAS_SECRETS名称必须准确，JSON单行字符串，覆盖所有secretRefs引用的键 |
| 数据API404 | 默认未注入API_TOKEN时数据演示接口不注册；启用postgres-redis配置和PAAS_SECRETS后才开放 |
| 数据API401 | 带X-Demo-Token，值等于API_TOKEN；配置secretRefs也必须注入API_TOKEN |
| 数据API503 | 是否复制完整postgres-redis配置；连接服务名/账号一致；看依赖日志 |
| 健康检查失败 | 应用监听0.0.0.0与声明port一致，health接口返回配置200；依赖认证可用 |
| 409多服务冲突 | 同域名多个服务实现相同路由；调整paths或应用接口，404才表示未处理 |
| 502共享参与服务不可用 | 任一匹配后端未及时返回头部，避免在不确定时选择其他服务 |
| 配置改变被拒绝 | 未知字段、路径格式、独占域名占用或稳定数据库配置变化 |
| named volume似乎空了 | 是否修改name/CONFIG_PATH；/tmp不持久化；查restore新卷映射 |
| private GHCR拉取失败 | 运行时只读GHCR身份必须能读取该包；包首次继承当前仓库访问权限 |
| 预算不足 | 停止不使用的栈，或合理减小资源；768 MiB包括同时运行的候选 |

总预算768 MiB。例如ASP.NET192 + PostgreSQL256 + Redis64 =512 MiB，更新Web增加192达到704 MiB；再运行128 MiB应用会使更新失败，旧版保留。停止容器不计运行预算；start仍会重新做准入检查。

资源必须实际通过rootless cgroup v2生效，平台doctor会检查。应用写只读文件系统失败应改用命名卷或临时目录，而非关闭安全限制。

## Cloudflare 403 / 1010

实测 Python 默认 `Python-urllib` User-Agent 会被现有 Browser Integrity Check 拒绝。模板 API 验收脚本声明 `Personal-PaaS-Smoke/1.0` 客户端标识后正常通过；没有关闭全站安全规则。先比较响应体和 `CF-Ray`，1010 属于浏览器签名阻挡；应用401是API令牌校验，二者不要混淆。参见 [Cloudflare BIC](https://developers.cloudflare.com/waf/tools/browser-integrity-check/)。

手动 stop/delete/restore 等更新操作创建版本屏障。尚未完成的较早push可能被拒绝为 `stale workflow run`，这是避免旧流水线覆盖新管理操作的保护。先等当前操作结束，再正常commit/push；不要删除状态或绕过OIDC。

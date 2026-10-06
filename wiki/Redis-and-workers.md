# Redis 和 Worker

Redis 服务使用专属 `/data` 卷、AOF（每秒fsync）及快照。例子64 MiB容器内存，Redis maxmemory设置为一半，为进程/AOF留空间；策略 `noeviction`，内存不足会明确报错。

| 配置 | 变量 | 含义 |
|---|---|---|
| redis.secretRefs | `REDIS_PASSWORD` | 秘密JSON同名键，Redis认证密码 |
| web.environment | `REDIS_HOST` | `redis` |
| web.environment | `REDIS_PORT` | `6379` |
| web.secretRefs | `REDIS_PASSWORD` | 同一个秘密JSON键 |
| worker.environment | `REDIS_HOST` | `redis` |
| worker.secretRefs | `REDIS_PASSWORD` | 同一个秘密JSON键 |

密码在容器临时文件中形成受限 Redis 配置，不出现在宿主机命令参数中。平台健康检查及备份自动使用认证。新示例均启用密码；旧无密码配置保持兼容。Redis无公网端口，Web、Worker通过本仓库网络连接。

ASP.NET 的 `/api/data/redis/{key}` GET/PUT 已包含实现，PUT body `{"value":"hello"}`，请求带 `X-Demo-Token`。键添加 `demo:` 前缀，GET没找到返回200和`found:false`。重启/删除并start后仍可回读。

`examples/multi-container.json` 额外启用 Worker，引用 `examples/worker/Dockerfile`。Worker每5秒认证并递增 `worker-counter`，日志只报告成功，不打印密码。该完整例子使用128 MiB Web、128 MiB Worker、256 MiB PostgreSQL、64 MiB Redis，共576 MiB；更新候选同时计入预算。

Worker没有域名/端口，更新先停止旧实例再启动新实例；失败恢复旧实例。凭据或稳定Redis服务配置变化应通过受控维护处理，普通push会拒绝对有状态服务的环境变化。

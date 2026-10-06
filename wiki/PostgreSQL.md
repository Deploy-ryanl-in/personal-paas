# PostgreSQL 变量、密码和 API

使用 `examples/postgres-redis.json`。PostgreSQL 镜像必须是管理员已批准的固定 digest；默认 PostgreSQL18，稳定卷挂到 `/var/lib/postgresql`。

| 配置位置 | 变量 | 示例 / 作用 |
|---|---|---|
| postgres.environment | `POSTGRES_USER` | `app`，初始化账号 |
| postgres.environment | `POSTGRES_DB` | `app`，初始化数据库 |
| postgres.secretRefs | `POSTGRES_PASSWORD` | 引用秘密JSON的同名键 |
| web.environment | `DATABASE_HOST` | `postgres`，容器网络服务名 |
| web.environment | `DATABASE_PORT` | `5432` |
| web.environment | `DATABASE_NAME` | 与 `POSTGRES_DB` 一致 |
| web.environment | `DATABASE_USER` | 与 `POSTGRES_USER` 一致 |
| web.secretRefs | `DATABASE_PASSWORD` | 引用 `POSTGRES_PASSWORD`，不能另填不一致的密码 |

`PAAS_SECRETS` 的 JSON 示例只有占位值：`{"POSTGRES_PASSWORD":"独立随机密码","REDIS_PASSWORD":"另一个随机密码","API_TOKEN":"独立API令牌"}`。用模板脚本生成真实值，保存为 Actions secret。

演示服务启动时创建 `paas_items(key,value)` 表，Npgsql 使用连接池及参数化 SQL。可用 `PUT /api/data/postgres/my-key`，body `{"value":"hello"}`；GET同路径返回 `{key,value,found}`。两者都带 `X-Demo-Token`。字段长度有限制，未找到返回200且`found:false`，方便共享路由区分“接口存在但数据不存在”和“未定义接口”的404。

默认初始化账号是该数据库所有者，适合个人演示；正式业务应通过应用自己的 SQL migration 建立最低权限角色。平台不会把数据库开放到公网，也不会执行来自 paas.json 的 SQL 或宿主机命令。

**密码轮换不能只改 PAAS_SECRETS**：现有卷中的 PostgreSQL 账号不会因容器环境变量变化而自动改密码。平台会拒绝普通部署的数据库环境/密码变化。先备份，再由经过审查的应用维护程序执行 `ALTER ROLE` 并协调应用配置；当前平台固定操作只支持批准的同大版本镜像升级，不提供任意SQL控制台或自动密码轮换。

删除容器不删除数据库卷；start重新挂载它。代码 rollback不回滚数据库数据；数据恢复用独立 restore 操作。

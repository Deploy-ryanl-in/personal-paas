# paas.json 与运行时变量

`paas.json` 必须为 `schemaVersion: 1`，未知字段、重复 JSON 键和不支持的版本都会被拒绝。编辑器通过本仓库 `schema/paas.v1.json` 提供提示；控制器另行执行严格验证。

| 字段 | 含义 |
|---|---|
| `name: "auto"` | 应用名称首次按新仓库身份生成 |
| `state: "present"` | push 部署；`absent` push 删除运行容器并保留卷 |
| `deployBranch: "main"` | 部署分支，由默认分支配置授权 |
| `services.<name>.type` | `web`、`worker`、`postgres` 或 `redis` |
| `build.context/dockerfile` | 仓库内构建路径；应用镜像由 CI 生成 |
| `port` | Web 容器监听端口，1024–65535；不会直接暴露公网 |
| `domain` | `auto` 或完整一级子域名，例如 `user-api.ryanl.in` |
| `health.path/status` | 容器及外部健康检查，例 `/healthz` /200 |
| `resources` | 必填 `memoryMiB`、`cpu`、`pids`，控制器核验实际限制 |
| `dependsOn` | 本仓库服务启动依赖；不能形成循环 |
| `environment` | 普通配置，如数据库主机名、数据库名、用户名；不要填密码 |
| `secretRefs` | 容器环境变量名 → `PAAS_SECRETS` 中的键名 |
| `volumes` | 本应用专属命名卷与容器内目标目录，禁止宿主机路径 |
| `routing` | 显式共享域名组和可选路径过滤器 |

配置不能传宿主机 shell、额外 Docker 参数、任意 bind mount、特权或代理指令。镜像来自当前不可变 repository ID 的 GHCR 路径，并固定 digest。

例如 `"secretRefs": {"DATABASE_PASSWORD":"POSTGRES_PASSWORD"}` 会把秘密 JSON 的 `POSTGRES_PASSWORD` 作为容器环境变量 `DATABASE_PASSWORD`。只传被配置引用的值；它们不会进入镜像或 Git，VPS 按版本加密保存，用于回滚。

配置随 push 同步：新 Web 镜像、域名、路径过滤、普通变量、Web/Worker 密钥和资源，在候选健康后切换。失败保留前一成功版本。数据库是稳定有状态服务；普通 push 不变更数据库镜像、资源和现有账号密码，这些需要明确的维护流程，不能把声明修改当成 SQL 密码轮换。

一个仓库最多8个服务，每个仓库对应一个生产栈。新增同仓库依赖不需要平台管理员添加服务名单。

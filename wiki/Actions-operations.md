# Actions 手动管理

在**应用仓库**进入 **Actions → PaaS operations → Run workflow**，选部署分支和操作。使用OIDC，不需要SSH私钥或root账号。操作作用于本仓库整个栈，包括依赖数据库/缓存。

| action | 行为 |
|---|---|
| `status` | 当前active/stopped/inactive、commit、release |
| `routes` | 同域名所有服务、过滤器、运行状态 |
| `logs` | 指定service最近200行，应用秘密值自动遮蔽 |
| `route-cache` | 查看本仓库相关子域名的路由缓存条数 |
| `clear-route-cache` | 立即清缓存；domain留空清本仓库所有相关域名，填完整域名仅清该域名 |
| `history` | 90天部署/操作记录 |
| `start` | 启动最后成功版本；已active则幂等；已delete则重新拉取并重建 |
| `stop` | 移除线上路由，停止容器，保留容器身份和卷 |
| `restart` | 停止并再次启动整个栈，不重新构建镜像 |
| `delete` | 删除栈容器及线上路由，保留卷、配置、秘密版本和历史 |
| `cleanup-images` | 删除VPS上本仓库未使用的应用镜像；运行和停止容器使用的镜像受保护 |
| `redeploy` | 按记录配置和镜像创建新发布候选，不构建应用 |
| `rollback` | 填release-id，恢复对应代码/配置/秘密版本，数据保持当前 |
| `backup` | 立即生成加密备份 |
| `backups` | 列出可用备份ID |
| `download-backup` | 填backup-id，下载加密文件到短期Actions artifact |
| `restore` | 填backup-id及`RESTORE <repository ID>`，恢复到新卷并切换 |
| `database-upgrade` | 填service和批准的同大版本image digest，先备份、停写入方后升级 |

清理测试：依次 `delete` → `cleanup-images`。这会保留数据及GHCR远程版本，之后还能start。要释放数据库数据卷必须走另外的明确管理员流程；本工作流不提供不可逆数据删除。清理不影响其他仓库、原3x-ui/Xray或rootfulDocker。

每天Asia/Taipei02:00加密备份，默认7天；PostgreSQL逻辑备份、Redis一致快照、普通卷冷备。age恢复密钥必须另行离线保管，不在Git里。完整服务器迁移还要保存state.db及对应state.key，详见安装文档。

普通容器更新先就绪、切路由、外部检查、观察60秒，失败回滚；旧容器有30秒退出时间。Worker先停旧再启新；数据库升级不是普通push，首版拒绝跨大版本自动升级。

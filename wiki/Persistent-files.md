# 持久 JSON 和命名卷

Docker容器根文件系统只读；`/tmp`、`/run`为受限临时目录，停止时不保留。需要跨容器保留的文件必须声明命名卷：

```json
"environment": {"CONFIG_PATH":"/data/config.json"},
"volumes": [{"name":"app-config","target":"/data"}]
```

平台把 `app-config` 绑定到本仓库ID下的卷，其他仓库即使使用同名也不会共享。应用 UID10001 可以写 `/data`；不能用宿主机绝对路径或挂载根目录。

模板 `PUT /api/data/config` 接收 JSON，写入临时文件后原子替换 `/data/config.json`；GET返回保存的内容。请求带 `X-Demo-Token`，JSON上限64 KiB。`scripts/api-smoke.py` 可验证写入和读取。

持久化验证步骤：写一个独特的proof值 → Actions stop → start → GET回读 → restart → GET回读 → delete → cleanup-images → start → GET回读。delete删除容器和路由，卷、最后成功发布配置与密钥版本保留；start重新拉取必要镜像并挂载原卷。

普通push创建新容器时同名卷保持。改变 `volumes.name` 等于选择新空卷，旧数据不会自动迁移；改变 `target` 必须同时修改应用文件路径。代码回滚不能撤销共享卷里的写入。

平台备份普通应用卷时停止写入方进行冷备，然后恢复运行；下载的是age加密备份。恢复创建新卷、验证后切换，不覆盖原卷。

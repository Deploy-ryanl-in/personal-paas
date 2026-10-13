# 完整配置参数

所有运行声明都放在项目根目录 `paas.json`。JSON Schema 位于模板的 `schema/paas.v1.json`；编辑器提示之外，CI和服务器还会严格校验。不要添加 `$schema` 到 JSON 顶层，当前严格接口不接受它。模板通过编辑器设置关联 Schema。

## 顶层

| 参数 | 必填 / 取值 | 配置方式 |
|---|---|---|
| schemaVersion | 必填，整数1 | 不能写字符串或自行升级版本 |
| name | 必填，auto或小写名称 | 显式名称1–40字符，以字母开头，字母/数字/连字符 |
| state | 必填，present / absent | present部署，absent删除容器和路由，保留数据 |
| deployBranch | 必填，例如main | 默认分支中的声明决定授权分支；其他分支不能部署 |
| services | 必填，1–8个命名服务 | 名称1–40字符，字母开头，只用小写字母/数字/连字符 |

应用身份绑定 repository ID，不绑定可变仓库名。`auto` 域名在首次部署绑定；之后改仓库名不会自动改域名。需要改域名就显式修改 `domain` 并push。

## 服务参数

| 参数 | Web | Worker | PostgreSQL / Redis | 约束 |
|---|---|---|---|---|
| type | web | worker | postgres / redis | 必填 |
| build.context | 必填 | 必填 | 禁止 | 仓库内相对路径，`.`或目录；不允许绝对路径和`..` |
| build.dockerfile | 必填 | 必填 | 禁止 | 仓库根目录相对路径，例examples/worker/Dockerfile；即使context是子目录仍按根目录写 |
| build.publicArgs | 可选 | 可选 | 禁止 | 环境名格式的键，值≤4096字符；只放公开构建配置，不能放密码 |
| image | 禁止 | 禁止 | 必填 | 管理员批准的`image@sha256:<64位digest>`；应用镜像由CI生成 |
| port | 必填 | 禁止 | 禁止 | 整数1024–65535；应用必须监听0.0.0.0，不是127.0.0.1 |
| domain | 必填 | 禁止 | 禁止 | auto或允许命名空间里的一级完整域名；保留名和冲突会拒绝 |
| health.path | 必填 | 禁止 | 禁止 | `/healthz`等绝对路径，无query/fragment；无需身份验证，必须可响应 |
| health.status | 必填 | 禁止 | 禁止 | 期望的200–299整数 |
| resources.memoryMiB | 必填 | 必填 | 必填 | 32–768整数，包含进程和容器缓存 |
| resources.cpu | 必填 | 必填 | 必填 | 大于0且≤1，0.5表示最多半个CPU |
| resources.pids | 必填 | 必填 | 必填 | 16–256整数；线程也计入Linux PID预算 |
| dependsOn | 可选 | 可选 | 可选 | 同仓库服务名数组；未知服务、循环依赖拒绝 |
| environment | 可选 | 可选 | 可选 | 普通运行变量；键`[A-Z][A-Z0-9_]{0,127}`，值字符串≤4096字符，无换行 |
| secretRefs | 可选 | 可选 | 视类型必需 | 容器环境变量名→PAAS_SECRETS中的键名，两者都是环境名格式 |
| volumes | 可选，最多8个 | 可选，最多8个 | 必须恰好1个 | name/target对象，不接受宿主机路径 |
| routing.sharedGroup | 共享时必填 | 禁止 | 禁止 | 命名格式同服务名；同域名所有参与者必须显式声明同组 |
| routing.paths | 可选，最多16个 | 禁止 | 禁止 | 不重复的绝对路径前缀，允许尾部`/*`；省略或空数组为全部路径 |

全VPS应用内存预算768 MiB，更新候选也计入。默认Web/Worker128、ASP.NET192、Next.js256、PostgreSQL256、Redis64 MiB。数据库组合的更新前请计算旧Web与新Web同时存在的峰值；容量不足时保留旧版并拒绝更新。

## 最小Web完整示例

```json
{
  "schemaVersion": 1,
  "name": "auto",
  "state": "present",
  "deployBranch": "main",
  "services": {
    "web": {
      "type": "web",
      "build": {"context": ".", "dockerfile": "Dockerfile"},
      "port": 8080,
      "domain": "my-api.ryanl.in",
      "health": {"path": "/healthz", "status": 200},
      "resources": {"memoryMiB": 192, "cpu": 0.5, "pids": 128},
      "environment": {"CONFIG_PATH": "/data/config.json"},
      "secretRefs": {"API_TOKEN": "API_TOKEN"},
      "volumes": [{"name": "app-config", "target": "/data"}]
    }
  }
}
```

这个例子引用API_TOKEN，需要Actions的PAAS_SECRETS中提供同名值。无密钥的默认模板不带这些secretRefs。普通环境变量不能与secretRefs同名；不能把字符串null或数字当环境值。

## 卷与数据库

卷name是应用内逻辑名称，实际卷按仓库隔离。target必须为干净绝对路径，不得为`/`或`/proc`、`/sys`、`/dev`、`/run`、`/etc`、`/usr`、`/bin`、`/sbin`及其子目录。Web通常使用`/data`。改卷名会选择新空卷，不会迁移旧数据。

PostgreSQL恰好一个卷，target为`/var/lib/postgresql`，secretRefs必须包含POSTGRES_PASSWORD。Redis恰好一个卷，target为`/data`，建议始终声明REDIS_PASSWORD。服务名就是应用网络的DNS名字，例如postgres、redis。应用之间网络隔离，数据库不发布公网端口。

## push更新的边界

Web/Worker代码、运行变量、秘密版本、资源、域名与路径过滤随成功部署更新。共享缓存会在部署分支push的独立job清除，即使后续CI失败；只有涉及的旧/新域名受影响。

现有数据库/Redis的镜像、卷、资源、普通变量和密码不能靠普通push更改或移除。数据库升级用固定操作，同大版本且镜像在批准名单；密码轮换需要先备份并由受控维护程序协调实际数据库账号。只改Actions密码不会修改已有数据库。

代码回滚恢复旧镜像、配置、秘密版本，但不会回滚数据库和命名卷数据。数据恢复单独操作。首次版本不支持自定义更新策略、任意命令、额外Docker参数、宿主机bind mount、特权容器或任意域名。

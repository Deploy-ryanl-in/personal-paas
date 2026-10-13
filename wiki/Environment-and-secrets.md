# 环境变量与秘密配置

先区分三个位置。普通运行配置在paas.json的environment；生产密码在应用仓库Actions secret PAAS_SECRETS；本地开发密码在被忽略的.env.local。Docker build参数只给公开浏览器配置，不能给服务器秘密。

## 生产秘密一步步设置

1. 从ASP.NET模板复制examples/postgres-redis.json到paas.json。
2. 在项目根目录运行python3 scripts/create-secrets.py，生成paas.secrets.json。它包含独立随机密码，权限600，已被Git忽略。
3. 打开自己的项目仓库Settings → Secrets and variables → Actions → New repository secret。
4. Name填写PAAS_SECRETS；Value粘贴文件完整JSON；点击Add secret。
5. 查看paas.json的secretRefs，确保每个引用键都存在。例如DATABASE_PASSWORD引用POSTGRES_PASSWORD，两者不是同一个命名位置。
6. commit/push普通配置，Actions在独立部署job只传被引用的秘密值。不要把秘密文件加入commit。

只用于说明的JSON结构：

```json
{
  "POSTGRES_PASSWORD": "replace-with-generated-random-value",
  "REDIS_PASSWORD": "replace-with-another-generated-random-value",
  "API_TOKEN": "replace-with-independent-api-token"
}
```

所有值必须是单行字符串。不能写嵌套JSON、数组、null、换行PEM或数字。环境变量引用键只用大写字母、数字和下划线，以字母开头。没被引用的值不发送。默认无数据库模板无需PAAS_SECRETS。

## ASP.NET演示变量

| 变量 | 生产例子 | 本地Compose | 作用 |
|---|---|---|---|
| DATABASE_HOST | postgres | 127.0.0.1 | PostgreSQL网络地址 |
| DATABASE_PORT | 5432 | 15432 | PostgreSQL端口 |
| DATABASE_NAME | app | app | 数据库名，与POSTGRES_DB一致 |
| DATABASE_USER | app | app | 数据库账号，与POSTGRES_USER一致 |
| DATABASE_PASSWORD | secretRefs引用POSTGRES_PASSWORD | .env.local生成值 | 数据库密码 |
| REDIS_HOST | redis | 127.0.0.1 | Redis网络地址 |
| REDIS_PORT | 6379 | 16379 | Redis端口 |
| REDIS_PASSWORD | secretRefs引用REDIS_PASSWORD | .env.local生成值 | Redis认证密码 |
| API_TOKEN | secretRefs引用API_TOKEN | .env.local生成值 | 数据演示接口X-Demo-Token |
| CONFIG_PATH | /data/config.json | ./local-data/config.json | JSON文件位置；生产必须声明/data卷 |

默认未设置API_TOKEN时，受保护数据演示接口不注册。设置令牌只是演示API的认证机制，不代表正式业务已具备用户登录或细粒度权限。

PostgreSQL服务的POSTGRES_USER/POSTGRES_DB是初始化配置，POSTGRES_PASSWORD为秘密引用。Redis服务引用REDIS_PASSWORD。Web/Worker访问相同服务时使用相同密码键。容器里的localhost表示当前容器，不能用它连接另一容器。

## Next.js变量

服务器秘密写secretRefs，应用只在服务器代码使用process.env。不要使用NEXT_PUBLIC_前缀放秘密，不要返回到页面HTML或Route Handler响应。

浏览器公开配置在build.publicArgs声明，例如NEXT_PUBLIC_APP_LABEL，Dockerfile必须显式接收相应ARG才能生效；它在构建时固定，更改后push会重新构建。运行时environment不能把已经编译进浏览器bundle的值替换掉。默认REVALIDATE_TOKEN是可选的服务端失效接口令牌，启用时通过secretRefs注入，详见模板README。

## 修改和回滚

无状态Web/Worker的秘密更新需要新的push部署，或者按适用的更新操作重新发布；只保存GitHub secret不会自动替换现有容器。版本化秘密用于代码回滚，旧发布恢复对应秘密版本。

已有数据库/Redis密码不能只改GitHub secret：实际账号密码和既有数据库卷没有被修改，平台会拒绝普通部署的稳定配置变更。先备份，按维护计划协调数据库实际账号和应用引用。数据恢复与代码回滚分开。

平台凭据（GHCR只读pull、Cloudflare DNS、GitHub App私钥）仅属于安装者，不填进任何应用仓库。日志自动遮蔽已知应用秘密，但业务仍不能主动输出密码、token、连接字符串或用户资料。

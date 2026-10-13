# 快速开始：从模板到上线

先走无数据库版本，确认一次push能上线，再增加数据库。当前实例允许RyanStanLin个人账号和Deploy-ryanl-in组织的公有、私有项目；其他账号需管理员绑定。应用开发者不需要VPS SSH密钥或Cloudflare token。

## 第一步：创建自己的仓库

打开 [ASP.NET模板](https://github.com/Deploy-ryanl-in/template-aspnet) 或 [Next.js模板](https://github.com/Deploy-ryanl-in/template-nextjs)。点击Use this template → Create a new repository，选择允许的Owner，仓库名例如my-api，选择public或private；只复制默认分支。

模板生成独立仓库。创建时可能触发首轮CI；未触发也无需处理，第一次本地push会启动。默认示例没有密码，也不需要先建Actions secret。

## 第二步：clone并启动

在新仓库Code菜单复制自己的地址，替换下面的OWNER和仓库名。私有仓库需已登录GitHub CLI或已配置SSH；遇到认证失败先解决clone，不要更改平台身份策略。

```sh
git clone https://github.com/OWNER/my-api.git
cd my-api
git status
```

ASP.NET需要global.json指定的.NET SDK，Node脚本需要模板固定Node版本。使用本机已安装SDK；如果版本不匹配，按global.json安装对应SDK。

```sh
dotnet restore --locked-mode
ASPNETCORE_URLS=http://localhost:8080 dotnet run --project src/App
```

打开http://localhost:8080/healthz和/api/hello?name=Ryan。Next.js使用：

```sh
npm ci
npm run dev
```

打开http://localhost:3000和/healthz。Next.js生产是Node standalone，不是静态导出，支持服务端API与SSR。

## 第三步：修改代码并提交

ASP.NET修改src/App里的代码；Next.js修改src/app里的页面或Route Handler。不要修改默认CI/CD工作流。paas.json中domain为auto时，默认使用新仓库名的一级子域名。

```sh
git status --short
git add src tests paas.json
# Next.js按实际改动使用git add src/app等路径
git commit -m "Add first feature"
git push origin main
```

不要提交.env.local、paas.secrets.json、私钥、token或数据库备份。git pull --ff-only用于以后同步自己的仓库。

## 第四步：看Actions并访问

新仓库Actions → CI and deployment。先通过测试和独立invalidate_cache，随后release/prepare、build、deploy。首次镜像下载和证书/健康检查可能需要几分钟。

所有job成功后，在deploy的Summary读取实际URL、commit和release；默认是https://my-api.ryanl.in。访问健康接口确认200，再验证业务页面或API。不能把镜像构建绿色当作部署完成，最终deploy也必须成功。

若仓库名过长、归一化为空、碰到保留名或已有域名冲突，就在paas.json显式填写可用domain后push。默认域名首次绑定后不会随仓库重命名变化。

## 第五步：加PostgreSQL、Redis和持久文件

ASP.NET模板已提供数据API；先复制数据库声明并生成独立生产秘密：

```sh
cp examples/postgres-redis.json paas.json
python3 scripts/create-secrets.py
```

打开paas.secrets.json，只在新仓库Settings → Secrets and variables → Actions → New repository secret里保存。Name填PAAS_SECRETS，Value填整个JSON对象，点击Add secret。这是一个JSON secret，不是分别新增三个GitHub secret。

JSON键POSTGRES_PASSWORD、REDIS_PASSWORD、API_TOKEN对应数据库密码、Redis密码和示例API令牌。真实密码由脚本生成。不要把秘密填进paas.json的environment，不要传到Docker build参数。

查看 [PostgreSQL](PostgreSQL)、[Redis与Worker](Redis-and-workers) 对照账号、数据库名、主机、端口和引用。容器里数据库主机是服务名postgres/redis，不能用localhost。再commit/push配置：

```sh
git add paas.json
git commit -m "Enable PostgreSQL Redis and persistent configuration"
git push origin main
python3 scripts/api-smoke.py https://my-api.ryanl.in
node scripts/ws-smoke.mjs https://my-api.ryanl.in
```

API脚本会创建演示数据并回读。Next.js数据库例子提供运行声明，实际数据库客户端和业务访问需由项目实现，不能直接拿ASP.NET演示API脚本当Next.js接口。

## 第六步：本地开发数据库

需要Docker与Compose。开发秘密和生产秘密分开生成：

```sh
python3 scripts/create-local-env.py
docker compose --env-file .env.local -f compose.dev.yml up -d --wait
set -a
source .env.local
set +a
dotnet run --project src/App
```

本机PostgreSQL地址127.0.0.1:15432，Redis127.0.0.1:16379；账号app、数据库app，密码由.env.local提供。VPS用postgres:5432和redis:6379。不要把VPS数据库端口公开来供本地开发。

在第二个终端加载.env.local后执行scripts/api-smoke.py http://localhost:8080 --local。结束开发：

```sh
docker compose --env-file .env.local -f compose.dev.yml down
```

down保留命名卷。不要加-v，除非确实要删除开发数据；换环境变量不会自动改旧卷里的账号密码。

## 第七步：日常管理和下一步

应用仓库Actions → PaaS operations → Run workflow，先用status看版本。stop停止，start恢复，restart重启；delete删除容器保留数据，之后cleanup-images清理本仓库未用应用镜像。回滚和数据恢复是不同操作，具体输入见 [Actions操作](Actions-operations)。

自定义域名直接改domain并push。多个服务共用域名时，每个参与者填写同一个routing.sharedGroup，并配置自己的路径；示例与冲突规则见 [共享路由](Domains-and-shared-routing)。WebSocket无需额外代理参数，认证与订阅逻辑在应用内实现。

完整参数见 [配置参考](Configuration-reference)，首次失败查 [排错](Troubleshooting)，性能测试和复现命令见 [性能](Performance)。已有独立仓库不会自动收到模板文件更新，升级workflow SHA要提交可审查的改动。

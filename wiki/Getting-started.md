# 新建 ASP.NET 项目：完整 SOP

## 1. 从模板创建

打开 [template-aspnet](https://github.com/Deploy-ryanl-in/template-aspnet)，点击 **Use this template → Create a new repository**。Owner 选 `RyanStanLin` 或 `Deploy-ryanl-in`；仓库名例如 `my-api`，公有和私有都可以。不要选择复制所有分支。生成的是独立项目，之后的提交属于新仓库。

创建完成即可 clone。GitHub 创建模板仓库时可能已经触发首轮 CI/CD，默认示例无需密钥即可上线；之后每次本地 push 都按新 commit 更新。模板仓库本身不会部署。保留默认 `.github/workflows/ci.yml` 和 `operations.yml`，不需要改 CI/CD。

```sh
git clone https://github.com/RyanStanLin/my-api.git
cd my-api
dotnet --version             # 使用 global.json 指定的 SDK
dotnet restore --locked-mode
ASPNETCORE_URLS=http://localhost:8080 dotnet run --project src/App
```

访问 `http://localhost:8080/healthz`、`/api/hello?name=Ryan`。WebSocket `/ws` 已内置，可运行 `node scripts/ws-smoke.mjs http://localhost:8080`（Node.js 24）。默认应用不需要 PostgreSQL、Redis 或密钥。

## 2. 增加数据库、缓存和持久 JSON

```sh
cp examples/postgres-redis.json paas.json
python3 scripts/create-secrets.py
```

生成的 `paas.secrets.json` 为本地私密文件，权限600且被 `.gitignore` 忽略。打开新仓库 **Settings → Secrets and variables → Actions → New repository secret**，名称填写 `PAAS_SECRETS`，Value 填文件里的完整 JSON。三个独立随机值分别是 PostgreSQL 密码、Redis 密码、测试API令牌。不能使用 SSH 密码，也不能把 JSON 提交到 Git。

该配置启用本仓库专属 PostgreSQL、Redis 以及 Web 持久卷。示例代码已包含连接与读写接口，无需再改 workflow。实际应用可替换示例业务。

## 3. 开发并 push

```sh
dotnet format --verify-no-changes
dotnet test -c Release
git status --short           # 确认未包含任何密钥
git add src tests paas.json
git commit -m "Enable persistent data API"
git push origin main
```

在 **Actions → CI and deployment** 查看测试、构建和部署；部署 summary 提供 URL、commit 与 release。默认 URL 为 `https://my-api.ryanl.in`，无需修改 DNS、Traefik 或 SSH。

```sh
python3 scripts/api-smoke.py https://my-api.ryanl.in
node scripts/ws-smoke.mjs https://my-api.ryanl.in
```

API 脚本从本地文件读取令牌，不把它输出到日志。它会创建演示数据并验证回读。

以后执行 `git pull --ff-only` 获取自己的仓库更新，再正常 code、commit、push。模板更新不会自动覆盖已有独立仓库；更新平台 workflow SHA 应通过可审查的提交完成。

## 本地数据库开发

应用需要 PostgreSQL 和 Redis 时，先启动自己的开发实例，再复制 `.env.example` 为 `.env.local`，启用并填写 `DATABASE_*`、`REDIS_*`、`API_TOKEN`、`CONFIG_PATH`。本地 `DATABASE_HOST=127.0.0.1`；VPS 容器里使用服务名 `postgres`，两者不要混淆。

```sh
set -a
source .env.local
set +a
dotnet run --project src/App
```

不要把生产数据库端口对公网开放来方便本地开发。`local-data/`、`.env.local`、`paas.secrets.json` 均应保持忽略。

Next.js 流程相同，使用 `npm ci`、`npm run dev`，默认健康接口 `/healthz`。数据库 JSON 示例只声明运行环境，Next.js 的业务数据访问需在项目内自行实现；ASP.NET 模板已包含可直接使用的演示接口。

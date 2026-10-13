# Next.js项目使用

从template-nextjs创建仓库后，clone自己的仓库，在项目根目录运行npm ci和npm run dev。页面在src/app，Route Handler在src/app/api；开发端口3000。默认paas.json的port同样为3000，生产容器监听0.0.0.0。

默认首页演示SSR与客户端交互，/api/hello为服务器接口，/stream演示流式响应，/isr演示增量缓存，/healthz为健康检查。正常commit/push即生产构建和部署。CI包含lint、类型检查、Vitest、生产build和Playwright冒烟。

生产Docker使用output: standalone，包含.next/static和public，运行Node服务；不能把默认模板替换成仅静态文件托管后仍期待SSR/API可用。

## 公开配置与秘密

NEXT_PUBLIC_APP_LABEL通过build.publicArgs及Dockerfile ARG构建到客户端，修改后需要push重新构建。服务端秘密通过secretRefs和PAAS_SECRETS运行时注入，不放进NEXT_PUBLIC变量，也不能从服务端组件把它序列化到浏览器。

REVALIDATE_TOKEN用于可选/api/revalidate失效接口，默认无令牌时拒绝请求。启用时声明secretRefs并配置生产JSON secret；调用时使用Bearer认证。不要把令牌写进示例代码或命令历史。

## 只读文件系统与缓存

根文件系统只读。模板关闭Cache Components，运行缓存适配器默认最多8MiB、128条，图片磁盘缓存最多8MiB；图片缓存使用/tmp，停止或重建后可以丢弃并重新生成。不要把用户上传文件、业务配置或数据库放在/tmp；用命名卷，业务代码读取明确路径。

Cloudflare保留动态响应的Cache-Control，平台没有对全站设置Cache Everything。流式响应和ISR是否缓存由应用自己的配置决定。部署后缓存会更新，模板根目录的cache-handler.cjs和next.config.js属于生产运行的一部分，修改时要保留读写边界并跑CI。

## 数据库与WebSocket

examples/postgres-redis.json提供PostgreSQL/Redis网络、变量、秘密与卷声明，但Next.js示例没有ASP.NET的数据读写API。按自己的业务增加数据库客户端，使用服务器端连接池和参数化查询；数据库主机填服务名，具体变量见Environment-and-secrets。

平台支持WebSocket转发，但默认Next.js standalone模板没有订阅服务器，也不能仅增加App Router HTTP Route Handler就声称完成WebSocket Upgrade。需要订阅服务时，可在同一仓库增加ASP.NET或其他Web服务并配置独立/ws路径及共享组；或者使用已实现Upgrade的服务器构建。应用必须实现订阅、鉴权、重连和队列限制。性能验收使用专用订阅fixture，不代表任意Next.js业务自动达到相同吞吐。

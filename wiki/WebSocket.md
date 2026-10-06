# WebSocket

Traefik终止TLS并转发HTTP Upgrade，Cloudflare转发WebSocket，应用使用`wss://项目.ryanl.in/ws`。ASP.NET模板已启用WebSocket并提供text/binary原样回传、分片重组、64 KiB消息限制、20秒keepalive和正常关闭处理。

```sh
node scripts/ws-smoke.mjs https://my-api.ryanl.in
# 可选第三个参数指定应用实际定义的路径
node scripts/ws-smoke.mjs https://endpoint.ryanl.in /ws/app
```

示例为无凭据echo，不读取数据库或文件。正式应用的会话权限由应用处理；不要把长期密钥放在URL查询参数中。浏览器客户端可以使用正常登录cookie或短时令牌，Origin验证应按自己的业务设置。

共享域名的请求先按paths过滤，然后对所有匹配服务发起同一路径握手。404表示未定义路径；一个101连接被接受才建立客户端连接，多个101或其他非404响应返回409多服务冲突，不挑选主服务。唯一连接建立后双向传输消息，任一侧关闭/出错会关闭另一侧；共享网关单消息上限64 KiB。

不同服务建议定义 `/ws/user` 与 `/ws/app`，同步在应用与 `routing.paths` 中声明。服务更新会让已有连接在旧容器退出后断开，客户端需要重连；不会把已建立的连接迁移到新进程。

[Traefik官方WebSocket说明](https://doc.traefik.io/traefik/master/user-guides/websocket/)

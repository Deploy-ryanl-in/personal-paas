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

## 并发与订阅

订阅是应用协议，平台不创建主题或消息队列。客户端先完成WSS握手，再发送应用要求的subscribe消息，并持续读取服务器事件。ASP.NET默认/ws是echo示例，需要在业务代码实现主题、鉴权和订阅状态。

网关现在分别限制128个处理中HTTP请求和2048条WebSocket连接，长期连接不会占用HTTP名额。限额是整台网关的总量，包含所有仓库；不是每个应用2048。超出限额返回503，关闭后释放。反向代理服务的文件描述符限额为65536。应用内存、CPU、PID及自己的连接上限仍要配置。实际1000订阅与100RPS组合负载及平台开销见[性能记录](Performance)。

每条订阅至少持续读取消息，处理close/error，使用带抖动的指数退避重连并重新订阅。更新会断开旧容器连接，不能期待连接在进程切换时无缝迁移。慢客户端写入超时10秒；单消息64KiB。应用应限制主题数、每连接队列长度和广播频率，防止慢订阅者无限积压。

Cloudflare初始Upgrade仍经过WAF，已建立连接也可能因边缘升级或空闲而关闭，因此客户端要实现心跳和重订阅。参见[Cloudflare WebSocket文档](https://developers.cloudflare.com/network/websockets/)。

# 自定义子域名与共享 API

## 自定义域名随 push 更新

把 Web 的 `domain` 从 `auto` 改成完整域名 `myproject.ryanl.in`。commit/push，候选健康后路由原子更新；原域名停止指向该服务。只允许本站一级应用子域名，基础设施和控制面名字保留；不要填写URL、端口、外部域名或多级域名。

`auto`首次绑定新仓库名，仓库重命名不会改变既有域名。显式domain可以改变它。通配DNS预先配置一次，不为项目创建记录。

## 多服务共享 endpoint.ryanl.in

所有参与服务必须显式声明同一个 `routing.sharedGroup`，避免覆盖其他仓库的独占域名。可在同仓库的两个Web服务，也可以跨已经获信任的仓库。

user服务：

```json
"domain":"endpoint.ryanl.in",
"routing":{"sharedGroup":"endpoint","paths":["/api/user","/ws/user"]}
```

app服务：

```json
"domain":"endpoint.ryanl.in",
"routing":{"sharedGroup":"endpoint","paths":["/api/app","/ws/app"]}
```

`/api/user`匹配该路径及下属路径，但不匹配 `/api/username`。`/api/*`匹配 `/api/` 下所有路径。过滤器支持尾部 `/*`，不支持正则或中间星号。多个过滤器命中同一服务仍只转发一次。路径保持原样，不strip prefix。

省略 `paths`（或空数组）意味着接收该域名所有请求。例如user只定义 `/api/adduser`，app只定义 `/api/addapp`，它们均可不填过滤器，由应用自身的路由返回404表示未定义接口。

| 所有匹配服务返回结果 | 客户端结果 |
|---|---|
| 没有匹配 / 全部404 | 404 |
| 恰好一个非404响应 | 返回该响应，包括应用的400/401/500 |
| 两个或更多非404响应 | 409 `{"error":"multi_service_conflict","detail":"多服务冲突"}` |
| 任一参与服务超时或连接失败 | 502，不能确认无冲突时不挑选其他响应 |

HTTP请求（含body）复制到所有匹配服务一次；这不是负载均衡，也不自动重试。**409不会撤销已经发生的业务写入**；因此各应用必须只处理自己定义的接口，避免重复定义写接口。业务“记录没找到”建议返回200/`found:false`或明确业务错误；共享路由会把404当作未处理。

共享请求body上限1 MiB，响应选定后支持流式输出；最多32个并发入口。独占服务保持普通反向代理和流式上传能力。WebSocket依照握手101/404判断处理服务；多个接受连接返回409，只有一个接受时建立双向连接。

Actions部署/操作summary显示共享域名的所有仓库、服务、路径和active/inactive状态，并输出warning注解。`routes`操作可单独查询。该信息只对已经通过仓库OIDC验证的操作入口开放。

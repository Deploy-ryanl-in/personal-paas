# 性能对比与复现

本页记录同一订阅应用在直连、平台网关、共享转发、TLS入口与Cloudflare公网入口的结果。同机部分已经完成，公网复验在调整握手爬升后继续；不把空闲连接数当作订阅通过。

测试目标是同时保持1000条已确认的WebSocket订阅，并处理至少100RPS HTTP。每条订阅接收每秒一次的256字节主题消息，检查序号连续；HTTP返回512字节JSON负载且明确no-store。连接逐步建立后，再开始持续窗口。应用镜像和容器资源限制保持相同，比较阶段串行执行。

## 已定位的旧版瓶颈

旧控制器HTTP/WebSocket共用32个生命周期名额。真实测试1000次订阅只有32成功、968失败；这32条连接持有期间，20秒内的2000个HTTP请求全部失败。优化版将HTTP处理并发128与长期WebSocket2048分开计数，仍保留限额和超限503。连接关闭会释放名额。

另修正WebSocket公共Host转发，Origin验证保留。后端HTTP空闲连接池扩大以减少共享转发短连接开销，异步冲突观察容量、缓存隔离和复制语义不变。

## 测试工具

平台源码cmd/paas-perf提供可重复的fixture与负载客户端。使用go.mod固定的Go版本，在平台根目录执行：

```sh
go build -o paas-perf ./cmd/paas-perf
./paas-perf serve
```

在第二个终端进行本地直连测试：

```sh
./paas-perf -url http://127.0.0.1:8080 \
  -connections 1000 -rps 100 -duration 120s -output direct.json
```

应用需监听8080，客户端发送subscribe/ticks，收到确认后逐条检查广播。PaaS测试应用可将cmd/paas-perf及平台go.mod/go.sum复制到独立应用仓库，使用现有发布workflow；build.context填`.`，build.dockerfile填`cmd/paas-perf/Dockerfile`，port8080、health/healthz状态200、资源96MiB/0.5CPU/128PID，临时域名不需秘密或卷。不要把平台控制器仓库改成应用部署仓库。只有WebSocket握手成功还不算订阅成功。

| 选项 | 默认 / 用途 |
|---|---|
| -url | HTTP或HTTPS origin，不包含请求路径 |
| -connections | 1000，订阅目标数 |
| -rps | 100，按绝对时间调度请求，慢响应不会降低供给速率 |
| -duration | 120s，建立完订阅后的持续窗口 |
| -output | JSON结果文件，权限600 |
| -host | 本机网关测试时设置公共Host，HTTP和WS一致 |
| -ramp | 默认5ms；公网建议50ms（每秒20次新建连），只影响握手爬升，不降低持续订阅数或HTTP RPS |
| -dial | TLS origin测试可固定连接地址，URL仍决定Host和SNI；不关闭证书验证 |

网关内部比较由管理员读取应用实际loopback端口进行，不对外开放该端口：

```sh
./paas-perf -url http://127.0.0.1:9081 -host my-perf.ryanl.in \
  -connections 1000 -rps 100 -duration 120s -output gateway.json
./paas-perf -url https://my-perf.ryanl.in -dial 127.0.0.1:443 \
  -connections 1000 -rps 100 -duration 120s -output origin-tls.json
```

普通开发者使用自己的压测应用公网域名：

```sh
./paas-perf -url https://my-perf.ryanl.in \
  -connections 1000 -ramp 50ms -rps 110 -duration 300s -output edge.json
```

公网测试需要应用已部署对应fixture，而不是向任意示例/ws echo发送subscribe就能得到同样行为。shared fixture的第二服务设置PERF_ROLE=peer，使业务路径返回404，验证复制转发和缓存。测试部署使用专属临时域名，结束delete和cleanup-images，不删除持久数据。

## 判断标准与边界

结果passed必须同时满足：目标订阅全部建立；无意外断线；无消息序号缺口；每个订阅在120秒窗口至少收到118条事件；所有计划HTTP完成且响应有效；零HTTP错误与调度丢弃；实际完成速率不低于目标的99%。300秒测试对应每条至少298条消息。速率误差只容纳窗口尾部计时，不能把缺少请求或失败算作成功。

JSON包含建立数、握手失败数、断线数、总事件、最低每订阅事件数、序号缺口、计划/完成HTTP、错误、调度丢弃、实际RPS与p50/p95/p99。同时观察应用与平台CPU、内存、文件描述符以及cgroup限制。

公网延迟包含客户端网络、Cloudflare和TLS；不能直接减去loopback延迟就称为控制器开销。平台开销通过同机直连与同机网关比较。额外TLS/edge阶段单独说明，结果为本次应用和机器上的负载证据，不是任意数据库查询、图片处理、消息大小或广播频率的无条件保证。

## 同机对比结果（2026-10-13）

同一固定Go应用镜像，应用每实例96MiB、0.5CPU，测试主机1vCPU、约2GiB。直连阶段仍运行平台守护进程但绕过路由链；因此这是转发开销对比，不是卸载所有基础设施后的另一台机器。每轮120秒，1000条已确认订阅，120000条事件，每连接最低120条，12000个HTTP，全部零错误、零断线、零序号缺口。

| 路径 | 实际RPS | HTTP p50 / p95 / p99 (ms) | 结果 |
|---|---|---|---|
| 同容器loopback直连 | 100.00 | 1.079 / 70.354 / 111.875 | 通过 |
| 平台单服务网关 | 100.00 | 1.258 / 115.526 / 162.147 | 通过 |
| 平台共享域名（保留复制） | 100.00 | 2.948 / 132.813 / 181.530 | 通过 |
| Traefik TLS + 平台网关 | 100.00 | 2.432 / 178.775 / 245.279 | 通过 |

单服务网关p50增加0.179ms，约16.6%；p95增加45.172ms，约64.2%。共享转发p50增加1.869ms，约173.2%。仅报p50会掩盖尾延迟，因此保留全部分位数。测试使用同步主题广播与0.5CPU容器限额，会产生周期性CPU/网络突发；本轮没有额外证明p95增长的唯一原因。

| 阶段 | 控制器平均CPU（单核百分比） | 控制器峰值RSS | Traefik平均CPU | Traefik峰值RSS |
|---|---|---|---|---|
| 直连基线 | 0.09% | 24.41MiB | 0.00% | 34.12MiB |
| 单服务网关 | 9.91% | 111.53MiB | 0.05% | 36.55MiB |
| 共享网关 | 13.25% | 116.30MiB | 0.01% | 36.55MiB |
| TLS完整入口 | 9.28% | 116.62MiB | 12.06% | 118.16MiB |

CPU采样含爬升和持续窗口；RSS是进程驻留内存，不等于cgroup全部内存。应用活跃阶段峰值RSS约59–61MiB。TLS阶段Traefik cgroup触到原128MiB硬限，后续把控制器和Traefik各设256MiB，GOMEMLIMIT为192MiB，并复测新余量。应用768MiB总预算没有变。

公网第一轮快速握手只建立部分连接，已停止，不计通过；使用较慢握手爬升重新验收，公网与上述同机结果分开报告。

## 回归测试证据

| Requirement | Evidence |
|---|---|
| 长期订阅不耗尽HTTP名额 | TestLongLivedWebSocketsDoNotBlockHTTP；旧版第33条503，修复后40条均收到事件且HTTP200 |
| 拒绝超额并在关闭后释放 | TestWebSocketCapacityRefusalAndRelease；填满容量、下一条503、关闭后新订阅成功 |
| HTTP饱和不占WS名额 | TestHTTPSaturationDoesNotConsumeWebSocketCapacity |
| 公共Host与Origin验证正确 | TestWebSocketPreservesPublicHostAndOrigin |
| 订阅和定速请求都真实验证 | TestLoadValidatesSubscriptionsAndPacedRequests |
| 非200HTTP不能误报成功 | TestLoadRejectsHTTPFailures |
| 缺失订阅不能误报成功且有原因 | TestLoadRejectsFailedSubscriptions |

以上命名测试通过Go race测试；全平台go test -race ./...与go vet ./...通过。当前Controller CI通过的安装包已核验SHA256。性能fixture只用于有授权的测试应用，没有开放数据库或宿主机路径。

## 调整内存后的TLS复验

默认快速握手在一次复验中只有797条建立、203失败，该轮明确不计通过。改为50ms间隔（每秒20次新建连）后，最终256MiB配置下1000条全部建立，120秒120000条连续事件，12000个HTTP全部完成，约100RPS，零错误/断线/漏消息。HTTP p50/p95/p99为1.355/154.441/205.791ms。爬升耗时50.65秒，与持续窗口分开报告。

控制器平均CPU7.58%、峰值RSS117.30MiB；Traefik8.99%、峰值RSS163.76MiB。cgroup总内存仍会触及配置上限，含进程以外的缓存，不能将RSS当作全部内存；这轮未发生OOM，准入和应用预算继续生效。稳定连接容量与每秒新TLS握手数是不同指标，本文保证的是已确认订阅下的持续负载。

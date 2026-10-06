# 本次 SOP 验收

本页在真实模板新建仓库、clone、开发、push、Actions部署及公网HTTP/WSS验证完成后填写commit、run和证据链接。测试应用会在验收后通过Actions清理容器和本地应用镜像，示例仓库与历史保留。

已完成本地检查：共享HTTP请求分发、路径过滤、409冲突、签名健康探针、WebSocket分发/冲突、持久停止状态、域名更新与共享授权；ASP.NET默认API、WebSocket分片消息、令牌保护、JSON跨应用重建回读。

真实VPS验收尚在进行，不能把本地测试视为整套生产验收完成。此前的从零安装验收另见 [docs/acceptance.md](https://github.com/Deploy-ryanl-in/personal-paas/blob/main/docs/acceptance.md)。

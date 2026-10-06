# 从零安装和账号/组织绑定

平台可复制到另一台全新Debian12/13 amd64 VPS，支持systemd+cgroup v2；不要求保留当前服务器。80/443需空闲。无需Kubernetes，不迁移原有宿主机服务。

完整可执行流程见仓库 [docs/reinstall.md](https://github.com/Deploy-ryanl-in/personal-paas/blob/main/docs/reinstall.md)。要点：

1. 选择已审查不可变平台commit，下载其 `build-<SHA>` Release安装包及SHA256SUMS，核验后解压。
2. 填 `installation.json`：domain、controlSubdomain、originIPv4、CloudflareZoneId、平台repository/SHA、获信任个人账号及组织的不可变owner ID。不要把名字当作安全身份。
3. 凭据存单独mode600文件：GHCR只读pull、仅本域DNS编辑的Cloudflare证书token、acmeEmail证书邮箱，及选择性只读GitHub App。默认OIDC绑定的job只读token可验证已绑定账号私有仓库，不要求逐仓库安装App。GHCR读取范围须覆盖批准账号和组织的包。
4. root仅用于一次性installer，应用控制器和应用Docker运行于独立无sudo的paas-runtime；Traefik无Docker socket。安装保留现有rootfulDocker与原服务。
5. 一次性设置代理开启的通配DNS和Full(strict)，证书服务DNS01获取/续期泛域名。既有基础设施、邮件和精确DNS记录保持原样。
6. `tools/prepare-templates.py` 按新安装配置生成两个独立模板，固定workflow SHA、域名与平台仓库；发布到自己的组织/个人账号并标记Template Repository。
7. 从模板新建私有仓库，clone/push验证。之后新增应用无需SSH或单独DNS配置。

可信owner允许旗下当前及未来仓库运行代码；只绑定自己控制的账号和组织。其他账号可以用repository ID精确白名单。重复installer保留state.key、state.db、数据卷及备份身份；namespace更换应使用新VPS并显式迁移应用绑定。

本实例已绑定RyanStanLin及Deploy-ryanl-in。新用户换账号/组织必须修改安装配置中的ID并通过 preflight 核验，不需要改控制器源码。恢复备份密钥、服务器状态密钥和应用秘密不得发布到Wiki或Git。

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

## 安装输入逐项说明

installation.json不是应用的paas.json。schemaVersion为1；domain填自己的根域名，controlSubdomain填部署API的一级前缀；originIPv4填新VPS公网IPv4；cloudflareZoneId从该域名Overview取得。platformRepository填公开平台仓库owner/name，platformSha填已审查完整40位SHA，必须与下载Release一致。

owners是id/login/type数组，type只能User或Organization，id必须是不可变GitHub数字ID。工具resolve-owners可查询并核验，不能把组织名当数字ID。repositoryAllowlist为额外批准的不可变仓库ID；不需要个人外部仓库时留空。previousWorkflowShas保留仍在使用的旧workflow固定SHA；不能写main。repositoryVerification推荐job-token-or-app，不要求每个新仓库单独安装GitHub App。

credentials.json只有秘密，不入Git。ghcrUsername是可读取私有包的GitHub身份；ghcrReadToken仅read:packages且覆盖已绑定账号/组织，必要时完成SSO授权。cloudflareDnsToken只对目标zone授予DNS Edit和Zone Read，用于DNS01。acmeEmail填证书联系邮箱。自动修改通配DNS和SSL时，另填cloudflareSetupToken，其权限增加Zone Settings Edit；安装后不保留这个setup token。可选githubAppId/githubAppPrivateKey必须同时提供，App只需要Contents/Actions/Metadata只读。

一次性配置完成后，新项目只使用自己的GITHUB_TOKEN与OIDC，不分发上述VPS凭据。安装者另行保管age恢复密钥和服务器state.key；重新装系统不能仅靠Git恢复数据库数据。

## 更换VPS或账号的执行顺序

先在新域名/新VPS准备80/443、Debian和凭据，再用configure.py解析新的个人账号/组织并生成配置。核验Release安装包，按docs/reinstall.md执行installer和doctor。生成新模板，标记Template repository；从新模板创建私有示例并push完成HTTPS验收后，再迁移正式应用。迁移持久数据使用加密备份和独立restore，不能覆盖现有实例来试验。

已有应用升级只更改已审查平台SHA并保留仍使用的previousWorkflowShas；对应控制器先安装成功，再更新应用workflow pin。当前性能版本没有改变OIDC或workflow接口，原来的已批准模板workflow仍可用。

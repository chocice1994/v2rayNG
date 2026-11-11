# AnyTLS TCP 原生技术预览 · preview1 并存测试构建

本分支基于 `chocice1994/v2rayNG` 的
`4ea9bd95e8e72fe9befd0b745c450698b46a1164`，未替换 Android 的 VPN 服务、
路由管理器或用户配置。**这不是已经完成的 Android AnyTLS 产品版本。**

## 已实现的链路

```text
完整自定义 JSON 中的 AnyTLS 出站
  → 同一 libv2ray AAR 内的配置适配器
  → 随机认证的 IPv4 loopback SOCKS5
  → Xray system dialer（沿用接口绑定 / socket controllers）
  → 验证服务器证书和主机名的 TLS
  → 固定版本 sing-anytls 的 AnyTLS 会话
```

Xray 当前没有 AnyTLS 出站实现，因此没有只添加枚举或菜单后假称协议可用。
适配器保留出站 tag，使 Xray 继续负责分流、入口、统计等工作；启动失败、停止、
重启和测速实例各自释放拥有的桥接资源。正常 JSON 原样返回；JSONC 原样交给
Xray，**含 AnyTLS 的 JSONC 暂不支持**。

- Xray 源码：[官方 proxy 目录](https://github.com/XTLS/Xray-core/tree/676eaf66f8f0/proxy)。
- Android 原生包装器固定为 gitlink `b5a377087e74eaa34e86718710b04c50f4b767f5`。
- AnyTLS 使用已发布的 [sing-anytls v0.0.11](https://github.com/anytls/sing-anytls/tree/v0.0.11)，
  不是 anytls-go 的示例客户端，也不是未固定的 testing 分支。
- 压测发现该版本上游流关闭竞态。本分支保留完整依赖源码、许可证、来源校验值、
  最小补丁和复现日志，修复流状态同步与清理 worker 退出；
  见 [LOCAL_PATCH.md](third_party/sing-anytls/LOCAL_PATCH.md)。

## 明确边界

- **仅 TCP**。UDP ASSOCIATE 明确拒绝，不支持 UDP-over-TCP，不静默直连。
- 暂无专用 AnyTLS 菜单、字段编辑器、`anytls://` 导入或分享链接。
- 此版首选**完整自定义 JSON**。普通“自定义出站”会受现有自动解析端点设置影响，
  可能注入不支持的 sockopt；暂不将其作为默认使用方式。
- 不支持 TCP half-close、uTLS 指纹、ECH、证书 pin、自定义 CA、源接口绑定、
  AnyTLS 作为代理链环节、Xray mux 或非默认目标解析策略；显式不支持的选项会报错。
- AnyTLS 自身的会话复用由固定库管理；不能把 SOCKS CONNECT 成功当作远端密码或
  最终目标已验证成功，必须实际收发应用数据。
- 默认 `allowInsecure: false`。显式设 true 会关闭证书和主机名验证，不能把它作为
  证书失败时的自动修复。
- DNS/UDP 及 VPN 防泄漏尚未在安卓核验。下方示例是 TCP 代理模式模板，不是能直接
  套到手机的完整 VPN/DNS 方案，不应据此替换现用配置。
- 未推送、发布或部署到手机。已产生 debug 测试 APK，但 Android 应用启动/VPN 尚未验证。标准 `Build APK` 工作流仍使用原版核心；要构建本预览，
  使用本分支新增加的专用 debug 工作流或下方本地构建命令。

## 仅供审阅的完整自定义 JSON 模板

`example.invalid` 和两处密码均为占位值，不能直接连接。不要把实际密码提交到 Git。
本地入口也使用密码，避免默认向其他本地应用开放未认证入口。端口需与选定代理模式匹配。

```json
{
  "remarks": "AnyTLS TCP preview1",
  "log": {"loglevel": "warning"},
  "inbounds": [{
    "tag": "socks",
    "listen": "127.0.0.1",
    "port": 10808,
    "protocol": "socks",
    "settings": {
      "auth": "password",
      "udp": false,
      "accounts": [{"user": "preview-user", "pass": "REPLACE_LOCAL_PROXY_PASSWORD"}]
    }
  }],
  "outbounds": [{
    "tag": "proxy",
    "protocol": "anytls",
    "settings": {
      "address": "example.invalid",
      "port": 443,
      "password": "REPLACE_SERVER_PASSWORD"
    },
    "streamSettings": {
      "network": "tcp",
      "security": "tls",
      "tlsSettings": {"serverName": "example.invalid", "allowInsecure": false}
    }
  }, {
    "tag": "block",
    "protocol": "blackhole",
    "settings": {}
  }],
  "routing": {
    "domainStrategy": "AsIs",
    "rules": [{"type": "field", "network": "udp", "outboundTag": "block"}]
  }
}
```

## 并存测试包

仅使用 `-PANYTLS_PREVIEW=true` 构建并存测试包。包名、Provider、快捷方式与运行边界见 [ISOLATION.md](ISOLATION.md)。
原应用配置不迁移；安装并存不意味着能同时运行两个 VPN 或 root 路由。

## 构建与检查

遵循仓库 `AGENTS.md` 及现有 workflow 中声明的 Android/Java/NDK 版本，初始化子模块。
工具必须是 Go 编译器；部分桌面环境的 `/usr/bin/go` 是围棋应用，脚本会拒绝它。
`ANDROID_HOME` 与 `ANDROID_NDK_HOME` 应指向已经配置好的 SDK 和 NDK。

```sh
python3 -m unittest discover -s native/tests -v
bash native/build-anytls-aar.sh /ABSOLUTE/NEW/anytls-core
# 完成后才拷贝生成输入，不提交二进制：
mkdir -p V2rayNG/app/libs
cp /ABSOLUTE/NEW/anytls-core/libv2ray.aar V2rayNG/app/libs/
NDK_HOME="$ANDROID_NDK_HOME" bash compile-hevtun.sh
cp -r libs/* V2rayNG/app/libs/
cd V2rayNG
./gradlew -PANYTLS_PREVIEW=true :app:testPlaystoreDebugUnitTest :app:compilePlaystoreDebugKotlin :app:assemblePlaystoreDebug
./gradlew -PANYTLS_PREVIEW=true :app:testFdroidDebugUnitTest :app:assembleFdroidDebug
cd ..
python3 native/verify_apk.py V2rayNG/app/build/outputs/apk/*/debug/*universal*.apk
```

脚本在新目录从固定原生 gitlink 生成 overlay，不修改子模块、不覆盖已有输出目录。
只有单一 Go/JNI AAR；新增依赖不会成为第二套 gomobile runtime。
专用 `.github/workflows/anytls-preview.yml` 只提供手动 debug 构建和临时 artifact，
不签发布版、不创建 release、不部署。当前未执行远端工作流。

## 验证状态

已通过与未运行的具体命令及证据见 [VALIDATION.md](VALIDATION.md)。
Linux 本地链路通过不等于 Android ABI、VPN 或真实服务器验证通过。

原生构建采用逐 ABI 串行 bind，并严格核对共同 Java/资源后合并单一 AAR；
默认限制 Go 并发并使用输出目录内临时盘，避免宿主内存与 /tmp 限额问题。
Android x86_64 使用官方 `purego` tag 绕过当前 Go/x/crypto blake2b 汇编的 PIC 链接问题，
其他 ABI 不受该回退影响；保留完整正确性测试，但不保证相同密码学运算性能。

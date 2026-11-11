# AnyTLS TCP 并存测试版验证记录

这是技术预览，不是已完成 Android 运行验收的稳定版。没有推送、部署或手机操作。

## Passed

- `python3 -m unittest discover -s native/tests -v`：11 项测试函数。
  包括固定源 overlay、关闭接线、严格 AAR 合并、两个 sourceSet 的快捷方式、
  包名/Provider/链接隔离、所有支持语言的测试标签、链接入口不直接记录含凭据输入、APK 校验器失败分支。
- `bash -n native/build-anytls-aar.sh`、源码 SHA256 清单及 `git diff --check`。
- bridge race ×20、vendored 生命周期 race ×20、wrapper race ×3、vet、module verify。
  覆盖真实本地 TLS/AnyTLS 回声及 `Xray → SOCKS → TLS → AnyTLS → HTTP 204`，
  认证/TLS失败、UDP拒绝、重复启动/停止/重启、JSON/core.New/core.Start失败清理。
- Linux `purego` race ×3。
- 四 ABI 原生 AAR 和 HEV 构建成功；按 ABI 串行 bind，共同 Java/JAR/资源一致后合并为**单一 AAR**，
  各 ABI ELF 架构核验通过。
- 普通构建包名保持 `com.v2ray.ang` / `com.v2ray.ang.fdroid`。
- `-PANYTLS_PREVIEW=true` 两发行渠道 Kotlin/Java 编译、单测与 debug assemble：
  **各 146 项单测，零失败、零错误、零跳过**。最终测试使用仓库内 Mockito agent 配置，没有私有 init script。
- 共 10 个 APK 的签名和 16KB ZIP 对齐通过；实际打包的四个快捷方式逐一核对，
  两个 universal APK 的标签、Provider、权限、独立包名、URL scheme、无 shared UID 及 **4 ABI × 3 原生库**通过。
- 64 位 arm64/x86_64 ELF LOAD 对齐为 16KB；32 位 Go/MMKV 为 4KB，**不声称所有 ABI 的 16KB 设备运行通过**。

## 已处理的真实构建与安全问题

- 生成源码没有 VCS 元数据：显式 `-buildvcs=false`。
- gomobile 多 ABI 内部并发及 /tmp 容量不足：串行 ABI、限定 Go 并发、任务专用临时目录。
- x86_64 的 blake2b 汇编有非 PIC 重定位：仅该 ABI 使用官方 `purego` 回退，可能降低性能，不改变协议目标。
- 云端 Gradle daemon 曾退出：单 worker、Kotlin 进程内编译、限定 JVM 资源后成功；没有把退出直接断言为 OOM。
- Java 21 的 Mockito self-attach 失败：按
  [Mockito 官方 instrumentation 说明](https://javadoc.io/static/org.mockito/mockito-core/5.24.0/org.mockito/org/mockito/Mockito.html#0.3)
  固化同版本 test-only 启动 agent，不放宽 OS 安全策略。
- F-Droid 有独立 shortcuts.xml 覆盖：已修复其四个硬编码目标，并核对所有最终 APK，而非只检查生成资源值。

- 并存包改动涉及的链接导入入口曾直接记录完整输入/解码URL：移除两处原文日志；不声称做过全应用日志审计。

实际工具：Go 1.27.0、Temurin JDK 21.0.13、Gradle 9.8.2、SDK Platform 37.0、
NDK 30.0.16248370；AGP 实际选择 Build-Tools 36.0.0。Android SDK 许可已获确认接受；未接受额外协议。

## Not run / 未通过的 Android 运行验收

- 官方 Android14/API34 AOSP 测试镜像的软件模拟器启动尝试：
  无 KVM，限时 184.6 秒观察到内核/init 与 zygote，但**没有确认 boot_completed=1 或可用 ADB/桌面**。
  已停止测试 VM。见 [官方软件模拟限制](https://developer.android.com/studio/run/emulator-commandline)。
- APK 安装、Android 应用实际启动、界面操作、并存安装运行：**Not run**。
- Android VPN、代理/root 生命周期、网络切换、证书库、DNS泄漏、AnyTLS 实际收发与外部服务端兼容：**Not run**。
- 用户真机、当前应用配置、发布签名和生产部署：**Not run / 未操作**。

不能用 Linux 回声、编译、签名或 ZIP 检查代替上述运行测试。
Geo 数据仍沿上游 gen_assets.sh 下载 latest；未固定数据哈希，不声称整个 AAR 位级可重现。
完整结果、初始失败与修复后证据已保留；公开摘要在 `test-evidence/`。

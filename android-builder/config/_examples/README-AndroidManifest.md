把本文件另存为上一级目录的 `config/AndroidManifest.xml` 才会生效。

⚠️ 覆盖整个 Manifest 是**高风险操作**：任何 Manifest 都需要和你的 DCloud SDK 版本严格对应，
直接从别处拷贝一份，最常见的后果是「安装后白屏」「启动就闪退」「推送收不到」。

推荐做法：先跑一次不带这个文件的打包，然后从 `--keep` 留下的 `build-cache/` 里复制出
SDK 原生的 AndroidManifest.xml 作为基线，再在基线之上做增量修改（加权限、加 provider 等）。

常见增量修改示例见 README「常见问题 → 加权限 / 厂商推送」一节。

# 图标与品牌资源

## 文件

| 文件 | 用途 |
|---|---|
| `app.ico` | **exe 图标**（16/24/32/48/64/128/256 七种尺寸），嵌入到 uniapp-pack.exe |
| `icon-512.png` | 图标源图（512×512），用于生成 app.ico |
| `tray.ico` | **托盘图标**（16/24/32/48/64 五种尺寸），运行时嵌入 |
| `tray.svg` | 托盘图标源图（SVG） |
| `icon.html` | 渲染 icon-512.png 用的 HTML（Chrome 无头截图生成） |

## 来源与许可

图形本身是 **Material Design Icons** 的 `android` 图标：

- 项目：<https://pictogrammers.com/library/mdi/>（维护方 Pictogrammers）
- 许可：**Apache License 2.0**
  （[官方许可说明](https://pictogrammers.com/docs/general/license/)明确列出
  Material Design Icons 为 Apache License 2.0；可免费商用、可修改、可再分发）
- 原始 SVG 路径：`https://api.iconify.design/mdi/android.svg`

配色（蓝色渐变 `#0052D9 → #4B8BFF` + 白色图形）是本项目自己加的，
与网页端顶栏 logo 的视觉保持一致；圆角与渐变部分不来自 MDI。

> Apache-2.0 要求保留版权与许可声明。上面的项目地址与许可信息即为归属声明；
> 若要把本项目再分发，请一并保留这段说明。

## 重新生成

改了图标源图之后：

```powershell
# 以下命令都在 pack-server/ 目录下执行

# 1) 改 assets/icon.html（改颜色/形状），用 Chrome 无头渲染出 512 源图
#    输出路径用绝对路径，相对路径 Chrome 可能基于别的工作目录解析而找不到
$root = (Get-Location).Path
& "$env:LOCALAPPDATA\Google\Chrome\Application\chrome.exe" `
  --headless=new --disable-gpu --hide-scrollbars `
  --default-background-color=00000000 --window-size=512,512 `
  --screenshot="$root\assets\icon-512.png" "file:///$($root -replace '\\','/')/assets/icon.html"

# 2) 用改好的 PNG 重新生成多尺寸 ico
#    （mkico 是本项目自带的小工具，零第三方依赖）
#    注意：必须写文件路径 main.go —— 它带 //go:build ignore，
#    用包路径 `./tools/mkico` 会被构建约束排除，报 "build constraints exclude all Go files"
go run ./tools/mkico/main.go -in assets/icon-512.png -out assets/app.ico -sizes 16,24,32,48,64,128,256

# 3) 删掉旧的资源对象，让构建脚本重新生成
Remove-Item rsrc_windows_amd64.syso -ErrorAction SilentlyContinue

# 4) 重新构建（会自动调 mkres.bat 生成图标资源）
.\release.ps1
```

> `mkico` 的输出是可复现的：同样的源图会生成字节完全相同的 ico
> （已验证 MD5 一致）。所以改了图标提交时，`app.ico` 的变化就是真实变化。

托盘图标（`tray.ico`）目前是独立的一份，改它同样用 `mkico` 生成即可。
两份分开是因为托盘图标在深色任务栏上要更小更简洁，和 exe 大头标的观感不同。

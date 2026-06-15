# BUG-001: go-toast 库中文乱码

**状态**: 已修复  
**日期**: 2026-06-15  
**影响**: Toast 通知中文内容显示为菱形问号（�）

## 现象

通过 `POST /api/notify` 发送中文标题/内容时，Windows 桌面弹出的 Toast 通知中文字符全部显示为乱码（菱形问号）。英文内容正常。

## 根因

`github.com/go-toast/toast` 库使用 `text/template` 将 Go 字符串（UTF-8）直接嵌入 PowerShell 脚本，写入临时 `.ps1` 文件后由 PowerShell 执行。

编码链路：
```
Go string (UTF-8) → text/template → temp .ps1 file (UTF-8 BOM) → PowerShell → XML here-string → WinRT XmlDocument
```

尽管 go-toast 在文件头加了 UTF-8 BOM，PowerShell 在解析 here-string 中的 XML 时仍然无法正确处理非 ASCII 字符，导致 WinRT XmlDocument.LoadXml() 收到损坏的字节。

## 修复方案

移除 `go-toast` 依赖，自行实现通知逻辑：

1. **XML 生成**：非 ASCII 字符转为 XML 数字字符引用（`&#xxxxx;`），生成的 XML 全程纯 ASCII
2. **脚本传递**：通过 PowerShell `-EncodedCommand` 参数传递脚本（UTF-16LE base64 编码），彻底绕过文件编码问题

修复后编码链路：
```
Go string → xmlEscapeText() (非ASCII→&#xxxxx;) → 纯ASCII XML → PowerShell script → encodeUTF16LEBase64() → -EncodedCommand → WinRT XmlDocument.LoadXml() 正确解码引用
```

## 修复文件

- `internal/notify/toast.go` — 重写实现，移除 go-toast 依赖
- `go.mod` / `go.sum` — 移除 `github.com/go-toast/toast` 和 `github.com/nu7hatch/gouuid`

---

## 调试记录

### 2026-06-15：验证修复时再次出现乱码

修复合并到 master 后，通过 curl 发送中文通知**仍然显示菱形问号**。

**排查过程**：

1. **怀疑 XML 生成有问题** → 在 `showImpl()` 中加 debug 日志，将生成的 XML 写入 `%TEMP%\toast_debug.xml`。发现 XML 中文变成了 `&#65533;`（U+FFFD 替换字符），而非预期的 `&#20013;` 等正确引用。说明数据在到达 `xmlEscapeText()` **之前**已经损坏。

2. **怀疑 HTTP 层编码问题** → 在 `handleNotify()` 的 JSON 解码后加 hex dump 日志：
   ```
   title=中文诊断 title_bytes=e4b8ade69687e8af8ae696ad
   ```
   UTF-8 字节完全正确（`e4b8ad` = 中, `e69687` = 文）。数据在 Go 层没有问题。

3. **怀疑旧二进制未替换** → 用 PowerShell `Get-Process` 确认旧的 `serial-gateway.exe` 仍占用 8081 端口。`taskkill /F` 后重新 build 并启动，XML debug 输出立即正常：
   ```xml
   <text id="1">&#20013;&#25991;&#35786;&#26029;</text>
   <text id="2">&#26816;&#26597;XML&#32534;&#30721;</text>
   ```
   PowerShell `Invoke-RestMethod` 发送后通知中文正常显示。

**结论**：修复代码本身正确，验证失败是因为**旧二进制未被 kill 就启动了新进程**，旧进程仍绑定端口接受请求。

### 教训

- 验证修复前务必 `Get-Process` / `taskkill` 确认旧进程已终止
- Go 的 `json.Decoder` 和 `net/http` 对 UTF-8 处理没有问题，编码出问题时优先怀疑传输层（curl/bash 终端编码）或进程版本
- 加 debug 日志输出 hex dump 是诊断编码问题的有效手段

---

## 相关测试

### TC01: XML 生成 - 中文转数字引用

**文件**: `internal/notify/toast_test.go`  
**函数**: `TestTC01_XMLGeneration_ChineseToNumericRef`  
**类型**: 自动（无需桌面环境）

验证 `toastXML()` 将中文字符转换为 XML 数字字符引用，输出为纯 ASCII。

### TC02: XML 生成 - 英文保持原样

**文件**: `internal/notify/toast_test.go`  
**函数**: `TestTC02_XMLGeneration_EnglishVerbatim`  
**类型**: 自动

验证英文文本在 XML 中保持原样，不被编码为数字引用。

### TC03: UTF-16LE Base64 编码往返

**文件**: `internal/notify/toast_test.go`  
**函数**: `TestTC03_UTF16LEBase64_Roundtrip`  
**类型**: 自动

验证 `encodeUTF16LEBase64()` 对中英文混合字符串的编码-解码往返正确性。

### TC04: 全流程数字引用保留

**文件**: `internal/notify/toast_test.go`  
**函数**: `TestTC04_FullPipeline_PreservesReferences`  
**类型**: 自动

验证完整流水线（XML 生成 → PowerShell 脚本 → base64 编码 → 解码）后，XML 数字引用仍然完整保留。

### TC05: PowerShell WinRT XML 解析中文

**文件**: `internal/notify/toast_test.go`  
**函数**: `TestTC05_PowerShellXMLParse_DecodesChineseCorrectly`  
**类型**: 手动（需 `RUN_TOAST_TEST=1`，需 Windows 桌面）

调用 PowerShell 加载生成的 XML，提取 `<text>` 节点内容，验证输出为正确的中文字符。

### TC06: 端到端中文 Toast 弹出

**文件**: `internal/notify/toast_test.go`  
**函数**: `TestTC06_ActualToast_Chinese`  
**类型**: 手动（需 `RUN_TOAST_TEST=1`，需 Windows 桌面，需人工确认通知内容）

实际弹出一个中文 Toast 通知，由人工确认桌面显示内容正确。

### TC07: XML 生成 - long duration

**文件**: `internal/notify/toast_test.go`  
**函数**: `TestTC07_XMLGeneration_LongDuration`  
**类型**: 自动

验证 `toastXML(title, content, DurationLong)` 生成的 XML 包含 `duration="long"`。

### TC07b: XML 生成 - short duration

**文件**: `internal/notify/toast_test.go`  
**函数**: `TestTC07b_XMLGeneration_ShortDuration`  
**类型**: 自动

验证 `toastXML(title, content, DurationShort)` 生成的 XML 包含 `duration="short"`。

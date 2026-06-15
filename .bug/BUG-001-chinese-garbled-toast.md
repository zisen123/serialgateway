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

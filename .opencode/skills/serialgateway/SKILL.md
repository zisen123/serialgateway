---
name: serialgateway
description: Use when needing to interact with a serial device (embedded Linux, MCU, etc.) through SerialGateway HTTP API — list ports, send commands, read output
---

# SerialGateway

通过 HTTP API 与串口设备交互。

## 配置

用户需提供 SerialGateway 的 API 地址：

```
API_BASE=http://<ip>:<port>
```

如果没有提供，询问用户。

## API 参考

### 列出串口

```
GET {API_BASE}/api/ports
```

返回所有可用 COM 端口及其状态。

### 列出映射

```
GET {API_BASE}/api/mappings
```

### 发送指令

```
POST {API_BASE}/api/mappings/{device}/write
Content-Type: application/json

{"data": "command\n"}
```

注意：指令末尾必须带 `\n`（换行符），否则设备不会执行。

### 读取输出

```
GET {API_BASE}/api/mappings/{device}/tail?lines=N
```

返回最近 N 行串口输出。默认跳过等待输出，至少指定 `lines=50`。

### 读取完整历史

```
GET {API_BASE}/api/mappings/{device}/log?format=text
```

## 交互模式

标准流程：**发指令 → 等 500ms → 读 tail 输出**

```powershell
# 发送指令
Invoke-RestMethod -Uri "$API_BASE/api/mappings/COM3/write" `
  -Method POST `
  -Body '{"data":"cat /etc/hostname\n"}' `
  -ContentType "application/json"

# 等待设备响应
Start-Sleep -Milliseconds 500

# 读取输出
Invoke-RestMethod -Uri "$API_BASE/api/mappings/COM3/tail?lines=50"
```

返回的 `output` 字段包含设备输出，`entries` 为行数。

## 注意事项

- 指令末尾必须加 `\n`
- 发送后等待 500ms-1s 让设备有时间响应
- tail 默认只返回最后 200 行，可通过 `lines` 参数调整
- 如果返回 `entries: 0`，说明设备没有输出——检查波特率或设备是否在线
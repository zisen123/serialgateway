# SerialGateway

将物理 COM 串口映射为 SSH 监听端口，并提供 HTTP REST API 进行管理和历史查询。

## 技术栈

| 组件 | 库 |
|---|---|
| 语言 | Go 1.22+ |
| SSH 服务 | [gliderlabs/ssh](https://github.com/gliderlabs/ssh) |
| 串口 | [go.bug.st/serial](https://github.com/bugst/go-serial) |
| 配置 | [gopkg.in/yaml.v3](https://gopkg.in/yaml.v3) |
| HTTP | `net/http`（标准库） |

## 用法

### 构建

```powershell
go build -o serial-gateway.exe ./cmd/serial-gateway/
```

### 配置

编辑 `serial-gateway.yaml`：

```yaml
gateway:
  http_port: 18080

serial_defaults:
  baudrate: 115200

ssh:
  base_port: 2200
  auth:
    type: "password"
    password: "serial"

ports:
  - device: "COM3"
    baudrate: 115200
```

### 启动

```powershell
.\serial-gateway.exe --config serial-gateway.yaml
```

### SSH 连接设备

```powershell
ssh -p 2203 serial@localhost
```

端口映射规则：SSH 端口 = `base_port` + COM 端口号（如 COM3 → 2203，COM8 → 2208）。

### HTTP API

| 方法 | 端点 | 说明 |
|---|---|---|
| GET | `/api/ports` | 列出所有 COM 端口 |
| GET | `/api/mappings` | 列出当前映射 |
| POST | `/api/mappings` | 创建映射 `{"device": "COM3"}` |
| DELETE | `/api/mappings/{device}` | 删除映射 |
| GET | `/api/mappings/{device}/tail?lines=N` | 获取最近 N 行串口输出 |
| GET | `/api/mappings/{device}/log?format=text` | 获取完整串口历史 |
| POST | `/api/mappings/{device}/write` | 向串口发送指令 `{"data": "command\n"}` |
| GET | `/api/config` | 查看配置 |
| PUT | `/api/config` | 更新配置 |

### 发送指令示例

```powershell
Invoke-RestMethod -Uri "http://localhost:18080/api/mappings/COM3/write" `
  -Method POST `
  -Body '{"data":"cat /etc/hostname\n"}' `
  -ContentType "application/json"
```

### 查看历史输出

```powershell
curl http://localhost:8080/api/mappings/COM3/tail?lines=50
```
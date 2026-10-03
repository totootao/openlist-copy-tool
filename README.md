# OpenList 115→115 防风控复制工具

基于 Go 实现的 Web 工具：在浏览器里**可视化选择源目录与目标目录**，将文件从一个 115 网盘**秒传**复制到另一个 115 网盘，并内置针对 115 风控的防护策略。

## 特性
- 🌐 **Web 目录浏览器**：左右双栏分别浏览 OpenList 挂载，点击即可「选为源 / 选为目标」
- ⚡ **115→115 秒传**：走 OpenList `/api/fs/copy`，文件 hash 命中即秒传
- 🛡️ **防风控**：三维滑动窗口限速（QPS/QPM/QPH）、限流自动冷却、分批提交、断点续传、晚高峰错峰
- 🐳 **容器化**：多阶段 Docker 构建，GitHub Actions 自动推送 Docker Hub

## 快速开始

### 方式一：Docker（推荐）
```bash
docker run -d --name ol-copy -p 8080:8080 \
  -e OPENLIST_URL=http://你的openlist:5244 \
  -e OPENLIST_USER=admin \
  -e OPENLIST_PASSWORD=你的密码 \
  totootao/openlist-copy-tool:latest
```
浏览器打开 http://localhost:8080

### 方式二：本地运行
```bash
go build -o app .
./app -url http://127.0.0.1:5244 -user admin -pass 你的密码 -listen :8080
```

## 配置项（命令行 flag 或环境变量）
| 参数 | 环境变量 | 默认 | 说明 |
|---|---|---|---|
| `-url` | `OPENLIST_URL` | `http://127.0.0.1:5244` | OpenList 地址 |
| `-user` | `OPENLIST_USER` | `admin` | OpenList 用户名 |
| `-pass` | `OPENLIST_PASSWORD` | 空 | OpenList 密码 |
| `-listen` | `LISTEN_ADDR` | `:8080` | Web 监听地址 |
| `-qps` | — | `1` | 每秒最大请求数（对 115 后端） |
| `-qpm` | — | `40` | 每分钟最大请求数 |
| `-qph` | — | `300` | 每小时最大请求数 |
| `-batch` | — | `10` | 每批目录数 |
| `-batch-gap` | — | `60` | 批间间隔（秒） |
| `-cooldown` | — | `1800` | 限流冷却时长（秒） |
| `-checkpoint` | — | `checkpoint.json` | 断点续传文件 |

## ⚠️ 重要：双层限速
仅靠本工具限速不够。115 风控计量的是 **OpenList 对 115 后端的真实请求频率**，因此还需：
1. 在 OpenList 后台把 115 存储的「限制速率(limit_rate)」设为 **1~2**；
2. 把 OpenList 全局 `tasks.copy.workers` 设为 **1**（`data/config.json` 的 `tasks.copy.workers`）。

## 使用流程
1. 打开 Web 页面，左侧浏览并「选为源」，右侧浏览并「选为目标」；
2. 点击「开始复制」创建任务；
3. 在「任务列表」查看实时进度（已处理 / 总数、日志、状态）；
4. 中断后重启会自动跳过已完成目录（断点续传）。

## 构建 / 部署
- CI：`.github/workflows/docker.yml` 在 push 到 main/master 时自动构建并推送 Docker Hub（`totootao/openlist-copy-tool`）。
- 需在仓库 **Settings → Secrets** 配置：`DOCKERHUB_USERNAME`、`DOCKERHUB_TOKEN`。
- Go 模块代理通过 `ghproxy.totootao.top` 加速。

---
title: Zencoder2API
emoji: 🚀
colorFrom: blue
colorTo: purple
sdk: docker
pinned: false
license: mit
app_port: 7860
---

# Zencoder2API

将 Zencoder AI 服务转换为兼容 OpenAI/Anthropic/Gemini 格式的 API 代理服务。

## 功能特性

- **多格式 API 兼容**
  - OpenAI `/v1/models`、`/v1/chat/completions` 和 `/v1/responses`
  - Anthropic `/v1/messages`
  - Gemini `/v1beta/models/*`

- **动态模型同步**
  - 启动时自动从 Zencoder 上游同步模型
  - 定时刷新模型缓存
  - 未命中的模型会触发一次按需同步
  - 上游接口异常时自动回退到官方模型文档/现有缓存

- **多账号池管理**
  - 自动轮询账号
  - 积分追踪与自动重置
  - 账号冷却机制
  - 自动刷新 Token

- **流式响应**
  - 完整支持 SSE 流式输出
  - 兼容各种客户端

- **管理面板**
  - Web 界面管理账号
  - 批量操作支持
  - 实时状态监控

## 部署方式

### Huggingface Spaces (推荐)

1. Fork 本仓库
2. 在 Huggingface 创建新 Space，选择 Docker SDK
3. 连接到你的 GitHub 仓库
4. 在 Space Settings 中设置环境变量:
   - `AUTH_TOKEN`: API 访问密钥
   - `ADMIN_PASSWORD`: 管理面板密码
5. Space 会自动构建并部署

### Docker 本地部署

```bash
# 构建镜像
docker build -t zencoder2api .

# 运行容器
docker run -d \
  -p 7860:7860 \
  -v $(pwd)/data:/app/data \
  -e AUTH_TOKEN=your_token \
  -e ADMIN_PASSWORD=your_password \
  zencoder2api
```

### Docker Compose

```bash
# 复制配置文件
cp .env.example .env

# 编辑配置
vim .env

# 启动服务
docker-compose up -d
```

### 使用 PostgreSQL

```bash
# 使用预配置的 PostgreSQL compose 文件
docker-compose -f docker-compose.postgres.yml up -d
```

### 开发命令 (Makefile)

本项目提供 Makefile 封装常用命令，**无需本地安装 Go**：

```bash
make help      # 查看所有命令
make build     # 构建 Docker 镜像
make run       # 启动服务
make dev       # 开发模式 (前台运行，显示日志)
make stop      # 停止服务
make logs      # 查看日志
make tidy      # 运行 go mod tidy (通过 Docker)
make test      # 运行测试 (通过 Docker)
make shell     # 进入 Go 开发容器
make clean     # 清理容器和镜像
```

## 环境变量

| 变量 | 说明 | 默认值 |
|------|------|--------|
| `PORT` | 服务端口 | 7860 |
| `DB_TYPE` | 数据库类型 (`sqlite` / `postgres` / `mysql`) | sqlite |
| `DB_PATH` | SQLite 数据库文件路径 | data.db |
| `DATABASE_URL` | PostgreSQL/MySQL 连接字符串 | - |
| `AUTH_TOKEN` | API 访问密钥 (留空则无需验证) | - |
| `ADMIN_PASSWORD` | 管理面板密码 | - |
| `DEBUG` | 调试模式 | false |
| `SOCKS_PROXY_POOL` | 代理池配置 | - |

## 数据库配置

支持 SQLite、PostgreSQL 和 MySQL 三种数据库，通过环境变量切换。

### SQLite (默认)

无需额外配置，开箱即用。仅设置 `DB_PATH` 即可（默认 `data.db`）：

```bash
DB_PATH=./data/zencoder.db
```

### PostgreSQL

```bash
DB_TYPE=postgres
DATABASE_URL=postgres://username:password@host:5432/dbname?sslmode=disable
```

### MySQL

```bash
DB_TYPE=mysql
DATABASE_URL=username:password@tcp(host:3306)/dbname?charset=utf8mb4&parseTime=True&loc=Local
```

### Docker Compose 示例 (PostgreSQL)

```yaml
version: '3.8'
services:
  zencoder2api:
    build: .
    ports:
      - "7860:7860"
    environment:
      - AUTH_TOKEN=your_token
      - ADMIN_PASSWORD=your_password
      - DB_TYPE=postgres
      - DATABASE_URL=postgres://zencoder:zencoder@db:5432/zencoder?sslmode=disable
    depends_on:
      - db
    restart: unless-stopped

  db:
    image: postgres:16-alpine
    environment:
      POSTGRES_USER: zencoder
      POSTGRES_PASSWORD: zencoder
      POSTGRES_DB: zencoder
    volumes:
      - pgdata:/var/lib/postgresql/data
    restart: unless-stopped

volumes:
  pgdata:
```

> **兼容性说明**：如果只设置 `DB_PATH` 且未设置 `DB_TYPE`，将自动使用 SQLite，与旧版配置完全兼容。

## API 使用

### OpenAI 格式

```bash
curl https://your-space.hf.space/v1/models \
  -H "Authorization: Bearer your_token"
```

返回的模型列表可直接用于 OpenAI 兼容客户端的自动发现，无需再手工填写。

```bash
curl https://your-space.hf.space/v1/models/status \
  -H "Authorization: Bearer your_token"
```

可查看当前模型同步来源、时间、数量和最近错误。

```bash
curl -X POST https://your-space.hf.space/v1/chat/completions \
  -H "Authorization: Bearer your_token" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-5.1-codex",
    "messages": [{"role": "user", "content": "Hello!"}],
    "stream": true
  }'
```

### Anthropic 格式

```bash
curl -X POST https://your-space.hf.space/v1/messages \
  -H "x-api-key: your_token" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "claude-sonnet-4-5-20250929",
    "max_tokens": 1024,
    "messages": [{"role": "user", "content": "Hello!"}]
  }'
```

## 支持的模型

### Anthropic Claude
- claude-sonnet-4-5-20250929
- claude-opus-4-5-20251101
- claude-haiku-4-5-20251001
- (带 -thinking 后缀的思考模式版本)

### OpenAI GPT
- gpt-5.1-codex-mini
- gpt-5.1-codex
- gpt-5.1-codex-max
- gpt-5.2-codex

### Google Gemini
- gemini-3-pro-preview
- gemini-3-flash-preview

### xAI Grok
- grok-code-fast-1

## 管理面板

访问根路径 `/` 即可打开管理面板，需要使用 `ADMIN_PASSWORD` 进行认证。

功能包括:
- 添加/编辑/删除账号
- 批量导入账号
- 查看账号状态和积分
- Token 刷新管理
- 池状态监控

## GitHub Actions

本项目包含以下自动化工作流:

- **go-build.yml**: Go 编译测试，发布时自动构建多平台二进制文件
- **docker-build.yml**: 构建并推送 Docker 镜像到 GHCR

## License

MIT License

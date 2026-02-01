# Zencoder2API Makefile
# 纯 Docker 开发环境，无需本地 Go

.PHONY: help build run dev stop logs clean tidy test shell

# Docker 镜像名
IMAGE_NAME := zencoder2api
CONTAINER_NAME := zencoder2api

# 默认目标
help:
	@echo "Zencoder2API - Docker 开发命令"
	@echo ""
	@echo "构建与运行:"
	@echo "  make build    - 构建 Docker 镜像"
	@echo "  make run      - 启动服务 (docker-compose)"
	@echo "  make dev      - 开发模式启动 (带日志输出)"
	@echo "  make stop     - 停止服务"
	@echo "  make restart  - 重启服务"
	@echo "  make logs     - 查看日志"
	@echo ""
	@echo "开发工具:"
	@echo "  make tidy     - 运行 go mod tidy (通过 Docker)"
	@echo "  make test     - 运行测试 (通过 Docker)"
	@echo "  make shell    - 进入构建容器的 shell"
	@echo "  make fmt      - 格式化代码"
	@echo ""
	@echo "清理:"
	@echo "  make clean    - 清理容器和镜像"

# 构建镜像
build:
	docker build -t $(IMAGE_NAME) .

# 使用 docker-compose 启动
run:
	docker-compose up -d

# 开发模式 (前台运行，显示日志)
dev:
	docker-compose up --build

# 停止服务
stop:
	docker-compose down

# 重启服务
restart:
	docker-compose restart

# 查看日志
logs:
	docker-compose logs -f

# 通过 Docker 运行 go mod tidy
tidy:
	docker run --rm -v "$(CURDIR):/app" -w /app golang:1.21-alpine go mod tidy

# 通过 Docker 运行测试
test:
	docker run --rm -v "$(CURDIR):/app" -w /app golang:1.21-alpine go test ./...

# 格式化代码
fmt:
	docker run --rm -v "$(CURDIR):/app" -w /app golang:1.21-alpine go fmt ./...

# 进入构建容器 shell
shell:
	docker run --rm -it -v "$(CURDIR):/app" -w /app golang:1.21-alpine sh

# 清理
clean:
	docker-compose down -v --rmi local
	docker rmi $(IMAGE_NAME) 2>/dev/null || true

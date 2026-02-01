package service

import (
	"math/rand"
	"net/http"
	"time"

	"zencoder2api/internal/model"

	"github.com/google/uuid"
)

var (
	// 可变的 User-Agent 列表
	userAgents = []string{
		"zen-cli/0.9.0-SNAPSHOT_4c6ffdd-windows-x64",
		"zen-cli/0.9.0-SNAPSHOT_5d7ggee-windows-x64",
		"zen-cli/0.9.0-SNAPSHOT_6e8hhff-windows-x64",
		"zen-cli/0.8.9-SNAPSHOT_3b5eedd-windows-x64",
	}

	// 可变的 Node 版本
	nodeVersions = []string{
		"v24.3.0",
		"v24.2.0",
		"v24.1.0",
		"v23.5.0",
		"v22.11.0",
	}

	// 可变的 zencoder 版本
	zencoderVersions = []string{
		"3.24.0",
		"3.23.9",
		"3.23.8",
		"3.24.1",
	}

	// 可变的 package 版本
	packageVersions = []string{
		"6.9.1",
		"6.9.0",
		"6.8.9",
		"6.8.8",
	}

	rng = rand.New(rand.NewSource(time.Now().UnixNano()))
)

// 随机选择一个元素
func randomChoice(items []string) string {
	return items[rng.Intn(len(items))]
}

// SetZencoderHeaders 设置Zencoder自定义请求头
func SetZencoderHeaders(req *http.Request, account *model.Account, zenModel model.ZenModel) {
	// 基础请求头 - 使用随机 User-Agent
	req.Header.Set("User-Agent", "zen-cli/0.9.0-SNAPSHOT_4c6ffdd-windows-x64")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Connection", "keep-alive")

	// 认证头
	req.Header.Set("Authorization", "Bearer "+account.AccessToken)

	// x-stainless 系列
	req.Header.Set("x-stainless-arch", "x64")
	req.Header.Set("x-stainless-lang", "js")
	req.Header.Set("x-stainless-os", "Windows")
	req.Header.Set("x-stainless-package-version", "0.70.1")
	req.Header.Set("x-stainless-retry-count", "0")
	req.Header.Set("x-stainless-runtime", "node")
	req.Header.Set("x-stainless-runtime-version", "v24.3.0")

	// zen/zencoder 系列 - 使用随机版本和唯一 ID
	req.Header.Set("zen-model-id", zenModel.ID)
	req.Header.Set("zencoder-arch", "x64")
	req.Header.Set("zencoder-auto-model", "false")
	req.Header.Set("zencoder-client-type", "vscode")
	req.Header.Set("zencoder-operation-id", uuid.New().String())
	req.Header.Set("zencoder-operation-type", "agent_call")
	req.Header.Set("zencoder-os", "windows")
	req.Header.Set("zencoder-version", "3.24.0")
}

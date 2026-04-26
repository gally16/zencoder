package service

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"zencoder2api/internal/model"
)

const (
	ZencoderModelsURL     = "https://api.zencoder.ai/v1/models"
	ZencoderModelsDocsURL = "https://docs.zencoder.ai/features/models"
)

type upstreamModelListResponse struct {
	Object string `json:"object"`
	Data   []struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Created int64  `json:"created"`
		OwnedBy string `json:"owned_by"`
	} `json:"data"`
}

type modelDescriptor struct {
	ID      string
	OwnedBy string
}

type ModelSyncService struct {
	mu           sync.RWMutex
	source       string
	lastError    string
	usingDefault bool
	modelCount   int
}

var (
	modelSyncOnce sync.Once
	modelSyncSvc  *ModelSyncService
)

func GetModelSyncService() *ModelSyncService {
	modelSyncOnce.Do(func() {
		modelSyncSvc = &ModelSyncService{
			source:       "default",
			usingDefault: true,
			modelCount:   len(model.DefaultZenModels()),
		}
	})
	return modelSyncSvc
}

func InitModelSyncService() {
	svc := GetModelSyncService()
	if err := svc.Sync(); err != nil {
		log.Printf("[ModelSync] 初始同步失败，继续使用默认模型集: %v", err)
	}
	go svc.refreshLoop()
}

func (s *ModelSyncService) refreshLoop() {
	ticker := time.NewTicker(30 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		if err := s.Sync(); err != nil {
			log.Printf("[ModelSync] 定时同步失败: %v", err)
		}
	}
}

func (s *ModelSyncService) Sync() error {
	models, source, err := s.fetchModels()
	if err != nil {
		current := model.ListZenModels()
		s.setStatus(s.currentSourceOrDefault(), err.Error(), len(current) == len(model.DefaultZenModels()), len(current))
		return err
	}

	if len(models) == 0 {
		err = fmt.Errorf("上游返回空模型列表")
		current := model.ListZenModels()
		s.setStatus(s.currentSourceOrDefault(), err.Error(), len(current) == len(model.DefaultZenModels()), len(current))
		return err
	}

	model.ReplaceZenModels(models)
	s.setStatus(source, "", false, len(models))
	log.Printf("[ModelSync] 模型同步成功，来源=%s，数量=%d", source, len(models))
	return nil
}

func (s *ModelSyncService) Status() model.ModelSyncStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()

	syncedAt := model.ZenModelsSyncedAt()
	return model.ModelSyncStatus{
		Source:       s.source,
		SyncedAt:     syncedAt.Unix(),
		ModelCount:   s.modelCount,
		LastError:    s.lastError,
		UsingDefault: s.usingDefault,
	}
}

func (s *ModelSyncService) setStatus(source, lastError string, usingDefault bool, modelCount int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.source = source
	s.lastError = lastError
	s.usingDefault = usingDefault
	s.modelCount = modelCount
}

func (s *ModelSyncService) currentSourceOrDefault() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.source == "" {
		return "default"
	}
	return s.source
}

func (s *ModelSyncService) fetchModels() (map[string]model.ZenModel, string, error) {
	if models, err := s.fetchFromAPI(); err == nil && len(models) > 0 {
		return models, "api", nil
	}

	models, err := s.fetchFromDocs()
	if err != nil {
		return nil, "", err
	}
	return models, "docs", nil
}

func (s *ModelSyncService) fetchFromAPI() (map[string]model.ZenModel, error) {
	account, err := GetNextAccount()
	if err != nil {
		return nil, fmt.Errorf("获取同步账号失败: %w", err)
	}
	defer ReleaseAccount(account)

	token, err := GetToken(account)
	if err != nil {
		return nil, fmt.Errorf("获取同步 token 失败: %w", err)
	}

	client := createHTTPClient(account.Proxy)
	req, err := http.NewRequest("GET", ZencoderModelsURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "zen-cli/0.9.0-SNAPSHOT_4c6ffdd-windows-x64")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求上游 models 接口失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("上游 models 接口返回 %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var payload upstreamModelListResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("解析上游 models 响应失败: %w", err)
	}

	items := make([]modelDescriptor, 0, len(payload.Data))
	for _, item := range payload.Data {
		items = append(items, modelDescriptor{
			ID:      item.ID,
			OwnedBy: item.OwnedBy,
		})
	}

	return buildDynamicModelMap(items), nil
}

func (s *ModelSyncService) fetchFromDocs() (map[string]model.ZenModel, error) {
	client := &http.Client{Timeout: 20 * time.Second}
	req, err := http.NewRequest("GET", ZencoderModelsDocsURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求官方 models 文档失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("官方 models 文档返回 %d", resp.StatusCode)
	}

	ids := extractModelIDsFromText(string(body))
	if len(ids) == 0 {
		return nil, fmt.Errorf("未从官方 models 文档提取到模型")
	}

	items := make([]modelDescriptor, 0, len(ids))
	for _, id := range ids {
		items = append(items, modelDescriptor{
			ID:      id,
			OwnedBy: inferProvider(id),
		})
	}

	return buildDynamicModelMap(items), nil
}

func buildDynamicModelMap(items []modelDescriptor) map[string]model.ZenModel {
	defaults := model.DefaultZenModels()
	result := make(map[string]model.ZenModel)

	for _, item := range items {
		modelID := item.ID
		if modelID == "" {
			continue
		}

		if base, ok := defaults[modelID]; ok {
			result[modelID] = base
		} else {
			providerID := normalizeProvider(item.OwnedBy, modelID)
			result[modelID] = buildFallbackModel(modelID, providerID)
		}

		if shouldAddThinkingAlias(modelID, result[modelID]) {
			aliasID := modelID + "-thinking"
			if _, exists := result[aliasID]; !exists {
				result[aliasID] = buildThinkingAlias(modelID, defaults, result[modelID])
			}
		}
	}

	return result
}

func buildFallbackModel(modelID, providerID string) model.ZenModel {
	zenModel := model.ZenModel{
		ID:          modelID,
		DisplayName: modelID,
		Model:       modelID,
		Multiplier:  1,
		ProviderID:  providerID,
	}

	switch providerID {
	case "openai":
		zenModel.Parameters = buildDefaultOpenAIParams()
	case "gemini", "xai":
		temp := 1.0
		if providerID == "xai" {
			temp = 0.0
		}
		zenModel.Parameters = &model.ModelParameters{Temperature: &temp}
	}

	return zenModel
}

func buildThinkingAlias(modelID string, defaults map[string]model.ZenModel, base model.ZenModel) model.ZenModel {
	aliasID := modelID + "-thinking"
	if tpl, ok := defaults[aliasID]; ok {
		return tpl
	}

	thinking := buildDefaultThinkingParams()
	return model.ZenModel{
		ID:          base.ID,
		DisplayName: base.DisplayName + " Thinking",
		Model:       aliasID,
		Multiplier:  base.Multiplier,
		ProviderID:  base.ProviderID,
		Parameters:  thinking,
		PremiumOnly: base.PremiumOnly,
	}
}

func buildDefaultThinkingParams() *model.ModelParameters {
	temp := 1.0
	return &model.ModelParameters{
		Temperature: &temp,
		Thinking:    &model.ThinkingConfig{Type: "enabled", BudgetTokens: 4096},
		ExtraHeaders: map[string]string{
			"anthropic-beta": "interleaved-thinking-2025-05-14",
		},
	}
}

func buildDefaultOpenAIParams() *model.ModelParameters {
	temp := 1.0
	return &model.ModelParameters{
		Temperature: &temp,
		Reasoning:   &model.ReasoningConfig{Effort: "medium", Summary: "auto"},
		Text:        &model.TextConfig{Verbosity: "medium"},
	}
}

func shouldAddThinkingAlias(modelID string, zenModel model.ZenModel) bool {
	return zenModel.ProviderID == "anthropic" &&
		strings.HasPrefix(modelID, "claude-") &&
		!strings.HasSuffix(modelID, "-thinking")
}

func normalizeProvider(ownedBy, modelID string) string {
	if provider := inferProvider(ownedBy); provider != "" {
		return provider
	}
	return inferProvider(modelID)
}

func inferProvider(value string) string {
	v := strings.ToLower(value)
	switch {
	case strings.Contains(v, "anthropic"), strings.HasPrefix(v, "claude-"):
		return "anthropic"
	case strings.Contains(v, "gemini"), strings.HasPrefix(v, "gemini-"):
		return "gemini"
	case strings.Contains(v, "grok"), strings.Contains(v, "xai"), strings.HasPrefix(v, "grok-"):
		return "xai"
	case strings.Contains(v, "gpt"), strings.Contains(v, "openai"), strings.HasPrefix(v, "o1"), strings.HasPrefix(v, "o3"), strings.HasPrefix(v, "o4"):
		return "openai"
	default:
		return "openai"
	}
}

func EnsureModelAvailable(modelID string) bool {
	if _, ok := model.GetZenModel(modelID); ok {
		return true
	}

	if err := GetModelSyncService().Sync(); err != nil {
		log.Printf("[ModelSync] 按需同步失败，模型=%s，错误=%v", modelID, err)
		return false
	}

	_, ok := model.GetZenModel(modelID)
	return ok
}

func extractModelIDsFromText(body string) []string {
	re := regexp.MustCompile(`\b(?:claude|gpt|o1|o3|o4|gemini|grok)[a-zA-Z0-9.\-]*\b`)
	matches := re.FindAllString(body, -1)
	uniq := make(map[string]struct{})
	for _, m := range matches {
		if len(m) < 4 {
			continue
		}
		uniq[m] = struct{}{}
	}

	ids := make([]string, 0, len(uniq))
	for id := range uniq {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

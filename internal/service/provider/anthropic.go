package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/packages/ssestream"
	"zencoder2api/internal/model"
)

const DefaultAnthropicBaseURL = "https://api.anthropic.com"

type AnthropicProvider struct {
	client *anthropic.Client
	config Config
}

func NewAnthropicProvider(cfg Config) *AnthropicProvider {
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultAnthropicBaseURL
	}

	opts := []option.RequestOption{
		option.WithAPIKey(cfg.APIKey),
		option.WithBaseURL(cfg.BaseURL),
	}

	for k, v := range cfg.ExtraHeaders {
		opts = append(opts, option.WithHeader(k, v))
	}

	if cfg.Proxy != "" {
		httpClient := NewHTTPClient(cfg.Proxy, 0)
		opts = append(opts, option.WithHTTPClient(httpClient))
	}

	return &AnthropicProvider{
		client: anthropic.NewClient(opts...),
		config: cfg,
	}
}

func (p *AnthropicProvider) Name() string {
	return "anthropic"
}

func (p *AnthropicProvider) ValidateToken() error {
	_, err := p.client.Messages.New(context.Background(), anthropic.MessageNewParams{
		Model:     anthropic.F(anthropic.ModelClaude3_5SonnetLatest),
		MaxTokens: anthropic.F(int64(1)),
		Messages:  anthropic.F([]anthropic.MessageParam{{Role: anthropic.F(anthropic.MessageParamRoleUser), Content: anthropic.F([]anthropic.ContentBlockParamUnion{anthropic.NewTextBlock("hi")})}}),
	})
	return err
}

func (p *AnthropicProvider) Chat(req *model.ChatCompletionRequest) (*model.ChatCompletionResponse, error) {
	messages := p.convertMessages(req.Messages)

	maxTokens := int64(4096)
	if req.MaxTokens > 0 {
		maxTokens = int64(req.MaxTokens)
	}

	resp, err := p.client.Messages.New(context.Background(), anthropic.MessageNewParams{
		Model:     anthropic.F(req.Model),
		MaxTokens: anthropic.F(maxTokens),
		Messages:  anthropic.F(messages),
	})
	if err != nil {
		return nil, err
	}

	return p.convertResponse(resp), nil
}

func (p *AnthropicProvider) convertMessages(msgs []model.ChatMessage) []anthropic.MessageParam {
	var messages []anthropic.MessageParam
	for _, msg := range msgs {
		if msg.Role == "system" {
			continue
		}
		role := anthropic.MessageParamRoleUser
		if msg.Role == "assistant" {
			role = anthropic.MessageParamRoleAssistant
		}
		messages = append(messages, anthropic.MessageParam{
			Role:    anthropic.F(role),
			Content: anthropic.F([]anthropic.ContentBlockParamUnion{anthropic.NewTextBlock(msg.Content)}),
		})
	}
	return messages
}

func (p *AnthropicProvider) convertResponse(resp *anthropic.Message) *model.ChatCompletionResponse {
	content := ""
	for _, block := range resp.Content {
		if block.Type == anthropic.ContentBlockTypeText {
			content += block.Text
		}
	}

	return &model.ChatCompletionResponse{
		ID:      resp.ID,
		Object:  "chat.completion",
		Model:   string(resp.Model),
		Choices: []model.Choice{{
			Index:        0,
			Message:      model.ChatMessage{Role: "assistant", Content: content},
			FinishReason: string(resp.StopReason),
		}},
		Usage: model.Usage{
			PromptTokens:     int(resp.Usage.InputTokens),
			CompletionTokens: int(resp.Usage.OutputTokens),
			TotalTokens:      int(resp.Usage.InputTokens + resp.Usage.OutputTokens),
		},
	}
}

func (p *AnthropicProvider) ChatStream(req *model.ChatCompletionRequest, writer http.ResponseWriter) error {
	messages := p.convertMessages(req.Messages)

	maxTokens := int64(4096)
	if req.MaxTokens > 0 {
		maxTokens = int64(req.MaxTokens)
	}

	stream := p.client.Messages.NewStreaming(context.Background(), anthropic.MessageNewParams{
		Model:     anthropic.F(req.Model),
		MaxTokens: anthropic.F(maxTokens),
		Messages:  anthropic.F(messages),
	})

	return p.handleStream(stream, writer)
}

func (p *AnthropicProvider) handleStream(stream *ssestream.Stream[anthropic.MessageStreamEvent], writer http.ResponseWriter) error {
	flusher, ok := writer.(http.Flusher)
	if !ok {
		return fmt.Errorf("streaming not supported")
	}

	writer.Header().Set("Content-Type", "text/event-stream")
	writer.Header().Set("Cache-Control", "no-cache")
	writer.Header().Set("Connection", "keep-alive")

	for stream.Next() {
		event := stream.Current()
		data, _ := json.Marshal(event)
		fmt.Fprintf(writer, "data: %s\n\n", data)
		flusher.Flush()
	}

	if err := stream.Err(); err != nil {
		return err
	}

	fmt.Fprintf(writer, "data: [DONE]\n\n")
	flusher.Flush()
	return nil
}

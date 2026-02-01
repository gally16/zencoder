package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/packages/ssestream"
	"zencoder2api/internal/model"
)

const DefaultGrokBaseURL = "https://api.x.ai/v1"

type GrokProvider struct {
	client *openai.Client
	config Config
}

func NewGrokProvider(cfg Config) *GrokProvider {
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultGrokBaseURL
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

	return &GrokProvider{
		client: openai.NewClient(opts...),
		config: cfg,
	}
}

func (p *GrokProvider) Name() string {
	return "grok"
}

func (p *GrokProvider) ValidateToken() error {
	_, err := p.client.Models.List(context.Background())
	return err
}

func (p *GrokProvider) Chat(req *model.ChatCompletionRequest) (*model.ChatCompletionResponse, error) {
	messages := make([]openai.ChatCompletionMessageParamUnion, len(req.Messages))
	for i, msg := range req.Messages {
		switch msg.Role {
		case "system":
			messages[i] = openai.SystemMessage(msg.Content)
		case "user":
			messages[i] = openai.UserMessage(msg.Content)
		case "assistant":
			messages[i] = openai.AssistantMessage(msg.Content)
		}
	}

	resp, err := p.client.Chat.Completions.New(context.Background(), openai.ChatCompletionNewParams{
		Model:    openai.F(req.Model),
		Messages: openai.F(messages),
	})
	if err != nil {
		return nil, err
	}

	return p.convertResponse(resp), nil
}

func (p *GrokProvider) convertResponse(resp *openai.ChatCompletion) *model.ChatCompletionResponse {
	choices := make([]model.Choice, len(resp.Choices))
	for i, c := range resp.Choices {
		choices[i] = model.Choice{
			Index:        int(c.Index),
			Message:      model.ChatMessage{Role: string(c.Message.Role), Content: c.Message.Content},
			FinishReason: string(c.FinishReason),
		}
	}

	return &model.ChatCompletionResponse{
		ID:      resp.ID,
		Object:  string(resp.Object),
		Created: resp.Created,
		Model:   resp.Model,
		Choices: choices,
		Usage: model.Usage{
			PromptTokens:     int(resp.Usage.PromptTokens),
			CompletionTokens: int(resp.Usage.CompletionTokens),
			TotalTokens:      int(resp.Usage.TotalTokens),
		},
	}
}

func (p *GrokProvider) ChatStream(req *model.ChatCompletionRequest, writer http.ResponseWriter) error {
	messages := make([]openai.ChatCompletionMessageParamUnion, len(req.Messages))
	for i, msg := range req.Messages {
		switch msg.Role {
		case "system":
			messages[i] = openai.SystemMessage(msg.Content)
		case "user":
			messages[i] = openai.UserMessage(msg.Content)
		case "assistant":
			messages[i] = openai.AssistantMessage(msg.Content)
		}
	}

	stream := p.client.Chat.Completions.NewStreaming(context.Background(), openai.ChatCompletionNewParams{
		Model:    openai.F(req.Model),
		Messages: openai.F(messages),
	})

	return p.handleStream(stream, writer)
}

func (p *GrokProvider) handleStream(stream *ssestream.Stream[openai.ChatCompletionChunk], writer http.ResponseWriter) error {
	flusher, ok := writer.(http.Flusher)
	if !ok {
		return fmt.Errorf("streaming not supported")
	}

	writer.Header().Set("Content-Type", "text/event-stream")
	writer.Header().Set("Cache-Control", "no-cache")
	writer.Header().Set("Connection", "keep-alive")

	for stream.Next() {
		chunk := stream.Current()
		data, _ := json.Marshal(chunk)
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

package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/google/generative-ai-go/genai"
	"google.golang.org/api/option"
	"zencoder2api/internal/model"
)

type GeminiProvider struct {
	client *genai.Client
	config Config
}

func NewGeminiProvider(cfg Config) (*GeminiProvider, error) {
	ctx := context.Background()

	opts := []option.ClientOption{
		option.WithAPIKey(cfg.APIKey),
	}

	if cfg.BaseURL != "" {
		opts = append(opts, option.WithEndpoint(cfg.BaseURL))
	}

	client, err := genai.NewClient(ctx, opts...)
	if err != nil {
		return nil, err
	}

	return &GeminiProvider{
		client: client,
		config: cfg,
	}, nil
}

func (p *GeminiProvider) Name() string {
	return "gemini"
}

func (p *GeminiProvider) ValidateToken() error {
	model := p.client.GenerativeModel("gemini-1.5-flash")
	_, err := model.GenerateContent(context.Background(), genai.Text("hi"))
	return err
}

func (p *GeminiProvider) Chat(req *model.ChatCompletionRequest) (*model.ChatCompletionResponse, error) {
	geminiModel := p.client.GenerativeModel(req.Model)

	var parts []genai.Part
	for _, msg := range req.Messages {
		parts = append(parts, genai.Text(msg.Content))
	}

	resp, err := geminiModel.GenerateContent(context.Background(), parts...)
	if err != nil {
		return nil, err
	}

	return p.convertResponse(resp, req.Model), nil
}

func (p *GeminiProvider) convertResponse(resp *genai.GenerateContentResponse, modelName string) *model.ChatCompletionResponse {
	content := ""
	if len(resp.Candidates) > 0 && resp.Candidates[0].Content != nil {
		for _, part := range resp.Candidates[0].Content.Parts {
			if text, ok := part.(genai.Text); ok {
				content += string(text)
			}
		}
	}

	return &model.ChatCompletionResponse{
		ID:      "gemini-" + modelName,
		Object:  "chat.completion",
		Model:   modelName,
		Choices: []model.Choice{{
			Index:        0,
			Message:      model.ChatMessage{Role: "assistant", Content: content},
			FinishReason: "stop",
		}},
	}
}

func (p *GeminiProvider) ChatStream(req *model.ChatCompletionRequest, writer http.ResponseWriter) error {
	geminiModel := p.client.GenerativeModel(req.Model)

	var parts []genai.Part
	for _, msg := range req.Messages {
		parts = append(parts, genai.Text(msg.Content))
	}

	iter := geminiModel.GenerateContentStream(context.Background(), parts...)
	return p.handleStream(iter, writer)
}

func (p *GeminiProvider) handleStream(iter *genai.GenerateContentResponseIterator, writer http.ResponseWriter) error {
	flusher, ok := writer.(http.Flusher)
	if !ok {
		return fmt.Errorf("streaming not supported")
	}

	writer.Header().Set("Content-Type", "text/event-stream")
	writer.Header().Set("Cache-Control", "no-cache")
	writer.Header().Set("Connection", "keep-alive")

	for {
		resp, err := iter.Next()
		if err != nil {
			break
		}
		data, _ := json.Marshal(resp)
		fmt.Fprintf(writer, "data: %s\n\n", data)
		flusher.Flush()
	}

	fmt.Fprintf(writer, "data: [DONE]\n\n")
	flusher.Flush()
	return nil
}

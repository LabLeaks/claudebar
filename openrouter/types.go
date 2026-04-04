package openrouter

import "encoding/json"

// AnthropicMessage represents a message in Anthropic's Messages API format
type AnthropicMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"` // Can be string or array of content blocks
}

// AnthropicContentBlock represents a content block within a message
type AnthropicContentBlock struct {
	Type        string                 `json:"type"`
	Text        string                 `json:"text,omitempty"`
	ID          string                 `json:"id,omitempty"`        // tool_use block id
	Name        string                 `json:"name,omitempty"`      // tool_use block name
	Input       map[string]interface{} `json:"input,omitempty"`     // tool_use block input
	Content     json.RawMessage        `json:"content,omitempty"`   // tool_result content (string or array)
	ToolUseID   string                 `json:"tool_use_id,omitempty"` // tool_result tool_use_id
	CacheControl *AnthropicCacheControl `json:"cache_control,omitempty"`
}

// AnthropicCacheControl represents cache control for Anthropic messages
type AnthropicCacheControl struct {
	Type string `json:"type"`
	TTL  string `json:"ttl,omitempty"`
}

// AnthropicRequest represents a request to Anthropic's /v1/messages endpoint
type AnthropicRequest struct {
	Model     string            `json:"model"`
	Messages  []AnthropicMessage `json:"messages"`
	System    json.RawMessage   `json:"system,omitempty"` // string or array of content blocks
	MaxTokens int               `json:"max_tokens,omitempty"`
	Stream    bool              `json:"stream,omitempty"`
	Tools     []json.RawMessage `json:"tools,omitempty"`
}

// OpenAIRequest represents a request to OpenRouter's Chat Completions API
type OpenAIRequest struct {
	Model       string      `json:"model"`
	Messages    []Message   `json:"messages"`
	MaxTokens   int         `json:"max_tokens,omitempty"`
	Stream      bool        `json:"stream,omitempty"`
	Tools       []Tool      `json:"tools,omitempty"`
	ToolChoice  string      `json:"tool_choice,omitempty"`
}

// Message represents a message in OpenAI's Chat Completions API
type Message struct {
	Role      string         `json:"role"`
	Content   string         `json:"content,omitempty"`
	ToolCalls []OpenAIToolCall `json:"tool_calls,omitempty"`
	ToolCallID string        `json:"tool_call_id,omitempty"` // for tool role messages
	Name      string         `json:"name,omitempty"`         // for tool role messages
}

// OpenAIToolCall represents a tool call in OpenAI's response
type OpenAIToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// Tool represents a tool/function in OpenAI's format
type Tool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string                 `json:"name"`
		Description string                 `json:"description,omitempty"`
		Parameters  map[string]interface{} `json:"parameters,omitempty"`
	} `json:"function"`
}

// OpenRouterResponse represents OpenRouter's response with usage data
type OpenRouterResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   *Usage   `json:"usage,omitempty"`
}

// Choice represents a choice in OpenRouter's response
type Choice struct {
	Index        int           `json:"index"`
	Message      Message       `json:"message,omitempty"`
	Delta        DeltaMessage  `json:"delta,omitempty"`
	FinishReason *string       `json:"finish_reason,omitempty"`
}

// DeltaMessage represents a streaming delta in OpenRouter's response
type DeltaMessage struct {
	Role      string           `json:"role,omitempty"`
	Content   string           `json:"content,omitempty"`
	ToolCalls []OpenAIToolCall `json:"tool_calls,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
}

// Usage represents token usage and cost from OpenRouter
type Usage struct {
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	TotalTokens      int     `json:"total_tokens"`
	Cost             float64 `json:"cost,omitempty"`
	IsByok           bool    `json:"is_byok,omitempty"`

	PromptTokensDetails     *PromptTokensDetails     `json:"prompt_tokens_details,omitempty"`
	CostDetails             *CostDetails             `json:"cost_details,omitempty"`
	CompletionTokensDetails *CompletionTokensDetails `json:"completion_tokens_details,omitempty"`
}

// PromptTokensDetails details about cached/written prompt tokens
type PromptTokensDetails struct {
	CachedTokens     int `json:"cached_tokens,omitempty"`
	CacheWriteTokens int `json:"cache_write_tokens,omitempty"`
	AudioTokens      int `json:"audio_tokens,omitempty"`
	VideoTokens      int `json:"video_tokens,omitempty"`
}

// CostDetails details about inference costs
type CostDetails struct {
	UpstreamInferenceCost            float64 `json:"upstream_inference_cost,omitempty"`
	UpstreamInferencePromptCost      float64 `json:"upstream_inference_prompt_cost,omitempty"`
	UpstreamInferenceCompletionsCost float64 `json:"upstream_inference_completions_cost,omitempty"`
}

// CompletionTokensDetails details about completion tokens
type CompletionTokensDetails struct {
	ReasoningTokens int `json:"reasoning_tokens,omitempty"`
	ImageTokens     int `json:"image_tokens,omitempty"`
	AudioTokens     int `json:"audio_tokens,omitempty"`
}

// AnthropicResponse represents the response in Anthropic's format
type AnthropicResponse struct {
	ID           string         `json:"id"`
	Type         string         `json:"type"`
	Role         string         `json:"role"`
	Content      []ContentBlock `json:"content"`
	Model        string         `json:"model"`
	StopReason   *string        `json:"stop_reason,omitempty"`
	StopSequence *string        `json:"stop_sequence,omitempty"`
	Usage        *AnthropicUsage `json:"usage,omitempty"`
}

// ContentBlock represents a content block in Anthropic's response
type ContentBlock struct {
	Type  string                 `json:"type"`
	Text  string                 `json:"text,omitempty"`
	ID    string                 `json:"id,omitempty"`        // for tool_use
	Name  string                 `json:"name,omitempty"`      // for tool_use
	Input map[string]interface{} `json:"input,omitempty"`     // for tool_use
}

// AnthropicUsage represents usage in Anthropic's format
type AnthropicUsage struct {
	InputTokens            int `json:"input_tokens"`
	OutputTokens           int `json:"output_tokens"`
	CacheReadInputTokens   int `json:"cache_read_input_tokens,omitempty"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"`
}

// SSEEvent represents a parsed Server-Sent Event line
type SSEEvent struct {
	Event string
	Data  string
}

// UsageLogEntry represents a log entry for usage tracking
type UsageLogEntry struct {
	Timestamp    string  `json:"timestamp"`
	Session      string  `json:"session"`
	Model        string  `json:"model"`
	PromptTokens int     `json:"prompt_tokens"`
	OutputTokens int     `json:"output_tokens"`
	TotalTokens  int     `json:"total_tokens"`
	CachedTokens int     `json:"cached_tokens,omitempty"`
	CostUSD      float64 `json:"cost_usd"`
	RequestID    string  `json:"request_id,omitempty"`
}

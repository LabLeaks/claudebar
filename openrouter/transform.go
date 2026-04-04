package openrouter

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode"
)

// AnthropicToOpenAI converts an Anthropic Messages API request to OpenRouter's OpenAI format
func AnthropicToOpenAI(anthropicReq AnthropicRequest) (OpenAIRequest, error) {
	// Convert messages
	messages := make([]Message, 0, len(anthropicReq.Messages))
	for _, msg := range anthropicReq.Messages {
		m, err := convertMessage(msg)
		if err != nil {
			return OpenAIRequest{}, fmt.Errorf("converting message with role %q: %w", msg.Role, err)
		}
		messages = append(messages, m)
	}

	// Prepend system message if present
	systemText, err := extractSystemString(anthropicReq.System)
	if err != nil {
		return OpenAIRequest{}, fmt.Errorf("extracting system prompt: %w", err)
	}
	if systemText != "" {
		messages = append([]Message{{
			Role:    "system",
			Content: systemText,
		}}, messages...)
	}

	// Convert tools
	tools := make([]Tool, 0, len(anthropicReq.Tools))
	for _, toolJSON := range anthropicReq.Tools {
		var anthropicTool struct {
			Name        string                 `json:"name"`
			Description string                 `json:"description,omitempty"`
			InputSchema map[string]interface{} `json:"input_schema,omitempty"`
		}
		if err := json.Unmarshal(toolJSON, &anthropicTool); err != nil {
			return OpenAIRequest{}, fmt.Errorf("unmarshaling tool: %w", err)
		}

		tool := Tool{Type: "function"}
		tool.Function.Name = anthropicTool.Name
		tool.Function.Description = anthropicTool.Description
		// input_schema (Anthropic) -> parameters (OpenAI)
		if anthropicTool.InputSchema != nil {
			tool.Function.Parameters = anthropicTool.InputSchema
		}
		tools = append(tools, tool)
	}

	return OpenAIRequest{
		Model:     anthropicReq.Model,
		Messages:  messages,
		MaxTokens: capMaxTokens(anthropicReq.MaxTokens, DefaultMaxTokens),
		Stream:    anthropicReq.Stream,
		Tools:     tools,
	}, nil
}

// convertMessage converts a single Anthropic message to OpenAI format.
// Handles plain text, tool_use, and tool_result content blocks.
func convertMessage(msg AnthropicMessage) (Message, error) {
	// Check if content is a tool_result block (user role with tool_result content)
	if msg.Role == "user" {
		var blocks []AnthropicContentBlock
		if err := json.Unmarshal(msg.Content, &blocks); err == nil {
			for _, block := range blocks {
				if block.Type == "tool_result" && block.Content != nil {
					// This is a tool_result message -- return as tool role
					text := extractToolResultContent(block.Content)
					return Message{
						Role:       "tool",
						Content:    text,
						ToolCallID: block.ToolUseID,
					}, nil
				}
			}
		}
	}

	// Check if content contains tool_use blocks (assistant role with tool_use content)
	if msg.Role == "assistant" {
		var blocks []AnthropicContentBlock
		if err := json.Unmarshal(msg.Content, &blocks); err == nil {
			var textParts []string
			var toolCalls []OpenAIToolCall
			for _, block := range blocks {
				switch block.Type {
				case "text":
					textParts = append(textParts, block.Text)
				case "tool_use":
					argsBytes, _ := json.Marshal(block.Input)
					toolCall := OpenAIToolCall{
						ID:   block.ID,
						Type: "function",
					}
					toolCall.Function.Name = block.Name
					toolCall.Function.Arguments = string(argsBytes)
					toolCalls = append(toolCalls, toolCall)
				}
			}
			m := Message{
				Role:    "assistant",
				Content: strings.Join(textParts, ""),
			}
			if len(toolCalls) > 0 {
				m.ToolCalls = toolCalls
			}
			return m, nil
		}
	}

	// Fallback: extract as a single string
	content, err := extractContentString(msg.Content)
	if err != nil {
		return Message{}, err
	}
	return Message{
		Role:    msg.Role,
		Content: content,
	}, nil
}

// extractToolResultContent extracts readable text from a tool_result content field.
// tool_result content can be a string or an array of content blocks.
func extractToolResultContent(content json.RawMessage) string {
	// Try as string first
	var str string
	if err := json.Unmarshal(content, &str); err == nil {
		return str
	}

	// Try as array of content blocks
	var blocks []AnthropicContentBlock
	if err := json.Unmarshal(content, &blocks); err == nil {
		var result strings.Builder
		for _, block := range blocks {
			if block.Type == "text" {
				result.WriteString(block.Text)
			}
		}
		return result.String()
	}

	return string(content)
}

// extractSystemString extracts text from the system field, handling both string and
// content block array variants (including cache_control blocks).
func extractSystemString(system json.RawMessage) (string, error) {
	if len(system) == 0 {
		return "", nil
	}

	// Try as string first
	var str string
	if err := json.Unmarshal(system, &str); err == nil {
		return str, nil
	}

	// Try as array of content blocks
	var blocks []AnthropicContentBlock
	if err := json.Unmarshal(system, &blocks); err != nil {
		return "", fmt.Errorf("system is neither string nor valid content blocks array: %w", err)
	}

	// Concatenate text from all blocks
	var result strings.Builder
	for _, block := range blocks {
		if block.Type == "text" {
			result.WriteString(block.Text)
		}
	}

	return result.String(), nil
}

// OpenAIResponseToAnthropic converts an OpenRouter response to Anthropic's format.
// Handles tool_calls in the response by creating tool_use content blocks.
func OpenAIResponseToAnthropic(openaiResp OpenRouterResponse) AnthropicResponse {
	var content []ContentBlock
	var stopReason *string

	if len(openaiResp.Choices) > 0 {
		choice := openaiResp.Choices[0]

		// Build content blocks from text and tool_calls
		if choice.Message.Content != "" {
			content = append(content, ContentBlock{
				Type: "text",
				Text: choice.Message.Content,
			})
		}

		// Convert tool_calls to tool_use content blocks
		for _, tc := range choice.Message.ToolCalls {
			// Fix tool call ID (OpenRouter may return integer IDs)
			id := fixToolCallID(tc.ID)

			// Repair malformed JSON in tool arguments (enhancetool)
			args := repairToolJSON(tc.Function.Arguments)
			var input map[string]interface{}
			if err := json.Unmarshal([]byte(args), &input); err != nil {
				input = make(map[string]interface{})
			}

			content = append(content, ContentBlock{
				Type:  "tool_use",
				ID:    id,
				Name:  tc.Function.Name,
				Input: input,
			})
		}

		stopReason = choice.FinishReason
	}

	var usage *AnthropicUsage
	if openaiResp.Usage != nil {
		u := openaiResp.Usage
		cacheReadTokens := 0
		cacheWriteTokens := 0

		if u.PromptTokensDetails != nil {
			cacheReadTokens = u.PromptTokensDetails.CachedTokens
			cacheWriteTokens = u.PromptTokensDetails.CacheWriteTokens
		}

		usage = &AnthropicUsage{
			InputTokens:              u.PromptTokens,
			OutputTokens:             u.CompletionTokens,
			CacheReadInputTokens:     cacheReadTokens,
			CacheCreationInputTokens: cacheWriteTokens,
		}
	}

	if content == nil {
		content = []ContentBlock{}
	}

	return AnthropicResponse{
		ID:         openaiResp.ID,
		Type:       "message",
		Role:       "assistant",
		Content:    content,
		Model:      openaiResp.Model,
		StopReason: stopReason,
		Usage:      usage,
	}
}

// ParseSSELine parses a line from an SSE stream, returning event type and data.
func ParseSSELine(line string) (event string, data string) {
	if strings.HasPrefix(line, "event: ") {
		event = strings.TrimPrefix(line, "event: ")
	} else if strings.HasPrefix(line, "data: ") {
		data = strings.TrimPrefix(line, "data: ")
	}
	return
}

// ForwardSSEStream reads an SSE stream from the reader and forwards it to the writer,
// translating OpenAI format chunks to Anthropic SSE format. Returns the final usage
// extracted from the stream (if present) and the final finish reason.
func ForwardSSEStream(w io.Writer, resp io.Reader, reqID, model string) (*Usage, error) {
	var finalUsage *Usage
	var finalFinishReason string

	scanner := bufio.NewScanner(resp)
	// Increase bufio buffer for large chunks
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	event := ""
	for scanner.Scan() {
		line := scanner.Text()

		if line == "" {
			// Blank line = end of SSE event, process accumulated data
			if event == "" {
				continue
			}
			event = ""
			continue
		}

		if strings.HasPrefix(line, "event: ") {
			event = strings.TrimPrefix(line, "event: ")
			continue
		}

		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				continue
			}

			// Parse OpenAI streaming chunk
			var chunk OpenRouterResponse
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				// Forward raw data if we can't parse it
				fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
				continue
			}

			// Extract usage from the final chunk
			if chunk.Usage != nil {
				finalUsage = chunk.Usage
			}

			// Convert each choice to Anthropic SSE format
			for _, choice := range chunk.Choices {
				isFinal := false
				if choice.FinishReason != nil {
					isFinal = true
					finalFinishReason = *choice.FinishReason
				}

				// Stream deltas
				if choice.Delta.Content != "" {
					anthropicChunk := map[string]interface{}{
						"type":  "content_block_delta",
						"index": choice.Index,
						"delta": map[string]interface{}{
							"type": "text_delta",
							"text": choice.Delta.Content,
						},
					}
					out, _ := json.Marshal(anthropicChunk)
					fmt.Fprintf(w, "event: message\ndata: %s\n\n", string(out))
				}

				// Stream tool_calls from delta
				for _, tc := range choice.Delta.ToolCalls {
					parts := parseToolCallDelta(tc.ID, tc.Function.Name, tc.Function.Arguments)
					for _, part := range parts {
						fmt.Fprintf(w, "event: message\ndata: %s\n\n", part)
					}
				}

				// Final message_delta event
				if isFinal {
					var stopSeq *string
					usageMap := map[string]interface{}{
						"output_tokens": 0,
					}
					if chunk.Usage != nil {
						usageMap["output_tokens"] = chunk.Usage.CompletionTokens
						if chunk.Usage.PromptTokensDetails != nil {
							usageMap["cache_read_input_tokens"] = chunk.Usage.PromptTokensDetails.CachedTokens
							usageMap["cache_creation_input_tokens"] = chunk.Usage.PromptTokensDetails.CacheWriteTokens
						}
					}
					anthropicChunk := map[string]interface{}{
						"type": "message_delta",
						"delta": map[string]interface{}{
							"stop_reason":   *choice.FinishReason,
							"stop_sequence": stopSeq,
						},
						"usage": usageMap,
					}
					out, _ := json.Marshal(anthropicChunk)
					fmt.Fprintf(w, "event: message\ndata: %s\n\n", string(out))
				}
			}
		}
	}

	// Send final message event
	finalChunkData := map[string]interface{}{
		"type":         "message",
		"id":           reqID,
		"model":        model,
		"content":      []ContentBlock{},
		"role":         "assistant",
		"stop_reason":  finalFinishReason,
		"stop_sequence": nil,
		"usage":        streamUsageFromOpenRouter(finalUsage),
	}
	out, _ := json.Marshal(finalChunkData)
	fmt.Fprintf(w, "event: message\ndata: %s\n\n", string(out))
	fmt.Fprintf(w, "event: message_done\ndata: {}\n\n")

	return finalUsage, scanner.Err()
}

// parseToolCallDelta breaks an OpenAI tool_call delta into Anthropic SSE events.
// OpenAI sends progressive deltas (index, then id, then name, then arguments pieces).
// For simplicity, emit a content_block_start + content_block_delta per delta observed.
func parseToolCallDelta(toolCallID, funcName, arguments string) []string {
	var parts []string

	// Fix tool call ID (OpenRouter may return integer IDs)
	toolCallID = fixToolCallID(toolCallID)

	if funcName != "" {
		startBlock := map[string]interface{}{
			"type":  "content_block_start",
			"index": 0,
			"content_block": map[string]interface{}{
				"type": "tool_use",
				"id":   toolCallID,
				"name": funcName,
			},
		}
		data, _ := json.Marshal(startBlock)
		parts = append(parts, string(data))
	}

	if arguments != "" {
		delta := map[string]interface{}{
			"type":  "content_block_delta",
			"index": 0,
			"delta": map[string]interface{}{
				"type": "input_json_delta",
				"partial_json": arguments,
			},
		}
		data, _ := json.Marshal(delta)
		parts = append(parts, string(data))
	}

	return parts
}

// streamUsageFromOpenRouter converts OpenRouter usage to Anthropic usage format for
// the final message event in a stream. Returns nil if input is nil.
func streamUsageFromOpenRouter(u *Usage) map[string]interface{} {
	if u == nil {
		return nil
	}
	result := map[string]interface{}{
		"input_tokens":  u.PromptTokens,
		"output_tokens": u.CompletionTokens,
	}
	if u.PromptTokensDetails != nil {
		result["cache_read_input_tokens"] = u.PromptTokensDetails.CachedTokens
		result["cache_creation_input_tokens"] = u.PromptTokensDetails.CacheWriteTokens
	}
	return result
}

// -- CCR transformer ports (enhancetool, openrouter, maxtoken) --

// repairToolJSON attempts to repair malformed JSON from tool call arguments.
// Ported from CCR's enhancetool transformer: try JSON parse → basic repair → empty object.
// Non-Anthropic models (Qwen, etc.) often return malformed JSON in tool arguments.
func repairToolJSON(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "{}" {
		return "{}"
	}

	// Tier 1: valid JSON — return as-is
	if json.Valid([]byte(raw)) {
		return raw
	}

	// Tier 2: basic repairs — common issues from non-Anthropic models
	repaired := raw

	// Strip trailing commas before closing braces/brackets
	repaired = stripTrailingCommas(repaired)

	// Fix unquoted keys: {foo: "bar"} → {"foo": "bar"}
	repaired = fixUnquotedKeys(repaired)

	// Try again after basic repairs
	if json.Valid([]byte(repaired)) {
		return repaired
	}

	// Tier 3: try to extract a valid JSON object from the string
	// (handles cases where model prepends/appends text around JSON)
	if idx := strings.Index(raw, "{"); idx >= 0 {
		candidate := raw[idx:]
		// Find matching closing brace
		depth := 0
		for i, c := range candidate {
			if c == '{' {
				depth++
			} else if c == '}' {
				depth--
				if depth == 0 {
					extracted := candidate[:i+1]
					if json.Valid([]byte(extracted)) {
						return extracted
					}
					break
				}
			}
		}
	}

	// Tier 4: give up, return empty object so the tool call doesn't crash
	return "{}"
}

// stripTrailingCommas removes trailing commas before } and ]
func stripTrailingCommas(s string) string {
	var result strings.Builder
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		if runes[i] == ',' {
			// Look ahead past whitespace for } or ]
			j := i + 1
			for j < len(runes) && unicode.IsSpace(runes[j]) {
				j++
			}
			if j < len(runes) && (runes[j] == '}' || runes[j] == ']') {
				continue // skip the comma
			}
		}
		result.WriteRune(runes[i])
	}
	return result.String()
}

// fixUnquotedKeys wraps unquoted object keys in double quotes.
// Handles simple cases like {foo: "bar"} → {"foo": "bar"}
func fixUnquotedKeys(s string) string {
	var result strings.Builder
	runes := []rune(s)
	inString := false
	for i := 0; i < len(runes); i++ {
		if runes[i] == '"' && (i == 0 || runes[i-1] != '\\') {
			inString = !inString
			result.WriteRune(runes[i])
			continue
		}
		if inString {
			result.WriteRune(runes[i])
			continue
		}
		// After { or , look for unquoted key followed by :
		if (runes[i] == '{' || runes[i] == ',') && i+1 < len(runes) {
			result.WriteRune(runes[i])
			// Skip whitespace
			j := i + 1
			for j < len(runes) && unicode.IsSpace(runes[j]) {
				result.WriteRune(runes[j])
				j++
			}
			// Check if next non-space char is a letter (unquoted key)
			if j < len(runes) && (unicode.IsLetter(runes[j]) || runes[j] == '_') {
				// Find end of key
				k := j
				for k < len(runes) && (unicode.IsLetter(runes[k]) || unicode.IsDigit(runes[k]) || runes[k] == '_') {
					k++
				}
				// Check if followed by :
				m := k
				for m < len(runes) && unicode.IsSpace(runes[m]) {
					m++
				}
				if m < len(runes) && runes[m] == ':' {
					result.WriteRune('"')
					result.WriteString(string(runes[j:k]))
					result.WriteRune('"')
					i = k - 1
					continue
				}
			}
			i = j - 1
			continue
		}
		result.WriteRune(runes[i])
	}
	return result.String()
}

// fixToolCallID ensures tool call IDs are string-prefixed.
// OpenRouter sometimes returns integer IDs; Anthropic expects "call_" prefix.
// Ported from CCR's openrouter transformer.
func fixToolCallID(id string) string {
	if id == "" {
		return generateCallID()
	}
	// If it's a pure integer, replace with a proper call ID
	if _, err := strconv.Atoi(id); err == nil {
		return generateCallID()
	}
	// If it doesn't start with "call_", prefix it
	if !strings.HasPrefix(id, "call_") {
		return "call_" + id
	}
	return id
}

// generateCallID creates a random call ID in the format call_<hex>
func generateCallID() string {
	b := make([]byte, 12)
	rand.Read(b)
	return "call_" + hex.EncodeToString(b)
}

// DefaultMaxTokens is the default max_tokens cap for models that don't specify one.
const DefaultMaxTokens = 16384

// capMaxTokens ensures max_tokens doesn't exceed the model's limit.
// Ported from CCR's maxtoken transformer.
func capMaxTokens(requested int, limit int) int {
	if limit <= 0 {
		limit = DefaultMaxTokens
	}
	if requested <= 0 || requested > limit {
		return limit
	}
	return requested
}

// extractContentString extracts text content from Anthropic's content field
// which can be either a string or an array of content blocks.
// Returns empty string for null content (not an error).
func extractContentString(content json.RawMessage) (string, error) {
	if len(content) == 0 || bytes.Equal(content, []byte("null")) {
		return "", nil
	}

	// Try as string first
	var str string
	if err := json.Unmarshal(content, &str); err == nil {
		return str, nil
	}

	// Try as array of content blocks
	var blocks []AnthropicContentBlock
	if err := json.Unmarshal(content, &blocks); err != nil {
		return "", fmt.Errorf("content is neither string nor valid content blocks array: %w", err)
	}

	// Concatenate text from all blocks
	var result strings.Builder
	for _, block := range blocks {
		if block.Type == "text" {
			result.WriteString(block.Text)
		}
	}

	return result.String(), nil
}

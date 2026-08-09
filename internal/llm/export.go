package llm

import (
	"context"
	"net/http"

	"github.com/xEsk/shellia/internal/core"
)

// BuildPrompts builds the system and user prompts for one planning request.
type (
	ChatCompletionRequest = chatCompletionRequest
	ChatMessage           = chatMessage
	ResponseFormat        = responseFormat
	HTTPStatusError       = llmHTTPStatusError
)

// DoRequest performs one non-streaming LLM request.
func DoRequest(ctx context.Context, client *http.Client, options ClientOptions, req ChatCompletionRequest) (string, error) {
	return doLLMRequest(ctx, client, options, req)
}

// BuildPrompts builds the system and user prompts for one planning request.
func BuildPrompts(request PromptRequest) (string, string) {
	return buildLLMPrompts(request)
}

// CallPlanningPrompt sends one planning prompt to the configured model.
func CallPlanningPrompt(ctx context.Context, client *http.Client, options ClientOptions, systemPrompt string, userPrompt string) (string, error) {
	return callPlanningPrompt(ctx, client, options, systemPrompt, userPrompt)
}

// ParseResponse parses one raw model response using the selected mode.
func ParseResponse(raw string, mode ResponseMode) (Response, error) {
	return parseResponse(raw, mode)
}

// NormalizePlan converts a parsed LLM response into a local plan.
func NormalizePlan(response Response) (string, []core.CommandPlan, error) {
	return normalizePlan(response)
}

// TrimForSummary truncates long text for prompts or compact user-facing previews.
func TrimForSummary(text string, maxChars int, strategy core.TruncationStrategy) string {
	return trimForSummary(text, maxChars, strategy)
}

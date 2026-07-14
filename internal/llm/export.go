package llm

import (
	"context"
	"io"
	"net/http"

	"shellia/internal/core"
	tracepkg "shellia/internal/trace"
)

// BuildPrompts builds the system and user prompts for one planning request.
type ChatCompletionRequest = chatCompletionRequest
type ChatMessage = chatMessage
type ResponseFormat = responseFormat
type HTTPStatusError = llmHTTPStatusError

// DoRequest performs one non-streaming LLM request.
func DoRequest(ctx context.Context, client *http.Client, cfg config, req ChatCompletionRequest) (string, error) {
	return doLLMRequest(ctx, client, cfg, req)
}

// DoStream performs one streaming LLM request.
func DoStream(ctx context.Context, client *http.Client, cfg config, req ChatCompletionRequest, w io.Writer) (string, error) {
	return doLLMStream(ctx, client, cfg, req, w)
}

// BuildPrompts builds the system and user prompts for one planning request.
func BuildPrompts(request PromptRequest) (string, string) {
	return buildLLMPrompts(request)
}

// BuildDiscoveryRepairPrompts builds the prompts for discovery repair.
func BuildDiscoveryRepairPrompts(request DiscoveryPromptRequest) (string, string) {
	return buildDiscoveryRepairLLMPrompts(request)
}

// CallPlanningPrompt sends one planning prompt to the configured model.
func CallPlanningPrompt(ctx context.Context, client *http.Client, cfg config, systemPrompt string, userPrompt string) (string, error) {
	return callPlanningPrompt(ctx, client, cfg, systemPrompt, userPrompt)
}

// ParseResponse parses one raw model response.
func ParseResponse(raw string) (Response, error) {
	return parseResponse(raw)
}

// NormalizePlan converts a parsed LLM response into a local plan.
func NormalizePlan(response Response) (string, []core.CommandPlan, error) {
	return normalizePlan(response)
}

// ShouldRetryWithDiscoveryRepair reports whether an empty plan should be retried with a discovery-only repair prompt.
func ShouldRetryWithDiscoveryRepair(response Response, round int, executions []core.CommandExecution) bool {
	return shouldRetryWithDiscoveryRepair(response, round, executions)
}

// StreamSummarizeExecutions streams the final answer from command observations.
func StreamSummarizeExecutions(
	ctx context.Context,
	client *http.Client,
	cfg config,
	instruction string,
	executions []core.CommandExecution,
	skipped []core.SkippedCommand,
	w io.Writer,
	trace *tracepkg.Logger,
	turnID string,
) (string, error) {
	return streamSummarizeExecutions(ctx, client, cfg, instruction, executions, skipped, w, trace, turnID)
}

// TrimForSummary truncates long text for prompts or compact user-facing previews.
func TrimForSummary(text string, maxChars int, strategy core.TruncationStrategy) string {
	return trimForSummary(text, maxChars, strategy)
}

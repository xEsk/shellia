package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	maxRetries                   = 3
	retryBaseDelay               = 500 * time.Millisecond
	streamChunkErrorPreviewChars = 160
	httpErrorBodyPreviewChars    = 1200
	historyEntryPreviewChars     = 240
)

type chatCompletionRequest struct {
	Model          string          `json:"model"`
	Temperature    float64         `json:"temperature"`
	Stream         bool            `json:"stream,omitempty"`
	ResponseFormat *responseFormat `json:"response_format,omitempty"`
	Messages       []chatMessage   `json:"messages"`
}

type responseFormat struct {
	Type string `json:"type"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatCompletionEnvelope struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// llmPromptRequest groups the context needed to build one planning prompt.
type llmPromptRequest struct {
	Config              config
	ContextInfo         contextInfo
	Instruction         string
	ResolvedInstruction string
	History             []historyEntry
	State               sessionState
	Observations        []commandExecution
}

// discoveryPromptRequest adds the failed response that triggered discovery repair.
type discoveryPromptRequest struct {
	Prompt   llmPromptRequest
	Previous llmResponse
}

// streamChunk is a single SSE delta from a streaming completion response.
type streamChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
}

type llmCommand struct {
	Command              string `json:"command"`
	Purpose              string `json:"purpose"`
	Risk                 string `json:"risk"`
	RequiresConfirmation bool   `json:"requires_confirmation"`
	Interactive          bool   `json:"interactive"`
	InteractiveReason    string `json:"interactive_reason"`
}

type llmResponse struct {
	Summary             string       `json:"summary"`
	Commands            []llmCommand `json:"commands"`
	RequiresObservation bool         `json:"requires_observation"`
	ObservationReason   string       `json:"observation_reason"`
	RequiresInput       bool         `json:"requires_input"`
	InputReason         string       `json:"input_reason"`
}

// llmHTTPStatusError carries a non-successful provider status and a compact body preview.
type llmHTTPStatusError struct {
	StatusCode int
	Body       string
	Err        error
}

// Error returns the provider status failure without exposing an error chain.
func (err *llmHTTPStatusError) Error() string {
	if err == nil {
		return ""
	}
	if strings.TrimSpace(err.Body) == "" {
		return fmt.Sprintf("llm request failed with status %d", err.StatusCode)
	}
	return fmt.Sprintf("llm request failed with status %d: %s", err.StatusCode, err.Body)
}

// Unwrap returns the lower-level read error, if the response body could not be read.
func (err *llmHTTPStatusError) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.Err
}

// isRetryable reports whether an HTTP status code is worth retrying.
func isRetryable(statusCode int) bool {
	return statusCode == 429 || (statusCode >= 500 && statusCode <= 504)
}

// doLLMRequest is the single non-streaming HTTP entry point for all model calls.
// It retries up to maxRetries times on transient errors (429, 5xx) with exponential backoff.
func doLLMRequest(ctx context.Context, client *http.Client, cfg config, req chatCompletionRequest) (string, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("cannot encode llm request: %w", err)
	}
	if client == nil {
		client = http.DefaultClient
	}

	url := strings.TrimRight(cfg.BaseURL, "/") + "/chat/completions"

	var (
		responseBody []byte
		statusCode   int
		lastErr      error
	)

	for attempt := range maxRetries {
		if attempt > 0 {
			wait := retryBaseDelay * (1 << (attempt - 1)) // 500ms, 1s, 2s
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(wait):
			}
		}

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return "", fmt.Errorf("cannot create llm request: %w", err)
		}
		applyLLMRequestHeaders(httpReq, cfg)

		resp, err := client.Do(httpReq)
		if err != nil {
			lastErr = fmt.Errorf("llm request failed: %w", err)
			continue
		}

		responseBody, err = io.ReadAll(resp.Body)
		resp.Body.Close()
		statusCode = resp.StatusCode

		if err != nil {
			lastErr = fmt.Errorf("cannot read llm response: %w", err)
			continue
		}
		if isRetryable(statusCode) {
			lastErr = newLLMHTTPStatusError(statusCode, string(responseBody), nil)
			continue
		}

		lastErr = nil
		break
	}

	if lastErr != nil {
		return "", lastErr
	}
	if statusCode < 200 || statusCode >= 300 {
		return "", newLLMHTTPStatusError(statusCode, string(responseBody), nil)
	}

	var envelope chatCompletionEnvelope
	if err := json.Unmarshal(responseBody, &envelope); err != nil {
		return "", fmt.Errorf("invalid llm envelope: %w", err)
	}
	if len(envelope.Choices) == 0 || strings.TrimSpace(envelope.Choices[0].Message.Content) == "" {
		return "", fmt.Errorf("invalid llm response: missing message content")
	}

	return envelope.Choices[0].Message.Content, nil
}

// doLLMStream performs a streaming LLM request.
// Delta tokens are written to w as they arrive; the full accumulated string is returned.
// The initial HTTP response is retried on transient errors before the stream is consumed.
func doLLMStream(ctx context.Context, client *http.Client, cfg config, req chatCompletionRequest, w io.Writer) (string, error) {
	req.Stream = true
	body, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("cannot encode llm request: %w", err)
	}
	if client == nil {
		client = http.DefaultClient
	}

	url := strings.TrimRight(cfg.BaseURL, "/") + "/chat/completions"

	var resp *http.Response
	var lastErr error

	for attempt := range maxRetries {
		if attempt > 0 {
			wait := retryBaseDelay * (1 << (attempt - 1))
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(wait):
			}
		}

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return "", fmt.Errorf("cannot create llm request: %w", err)
		}
		applyLLMRequestHeaders(httpReq, cfg)

		resp, err = client.Do(httpReq)
		if err != nil {
			lastErr = fmt.Errorf("llm stream request failed: %w", err)
			continue
		}
		if isRetryable(resp.StatusCode) {
			errorBody, readErr := readHTTPErrorBody(resp.Body)
			resp.Body.Close()
			lastErr = newLLMHTTPStatusError(resp.StatusCode, errorBody, readErr)
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			errorBody, readErr := readHTTPErrorBody(resp.Body)
			resp.Body.Close()
			return "", newLLMHTTPStatusError(resp.StatusCode, errorBody, readErr)
		}
		lastErr = nil
		break
	}

	if lastErr != nil {
		return "", lastErr
	}
	defer resp.Body.Close()

	var full strings.Builder
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		if payload == "[DONE]" {
			break
		}

		var chunk streamChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			return full.String(), fmt.Errorf("invalid llm stream chunk: %w: %s", err, trimForSummary(payload, streamChunkErrorPreviewChars, truncationStart))
		}
		if len(chunk.Choices) == 0 {
			continue
		}

		token := chunk.Choices[0].Delta.Content
		if token == "" {
			continue
		}

		full.WriteString(token)
		fmt.Fprint(w, token)
	}

	if err := scanner.Err(); err != nil {
		return full.String(), fmt.Errorf("stream read error: %w", err)
	}

	return full.String(), nil
}

// applyLLMRequestHeaders attaches the shared headers for OpenAI-compatible requests.
func applyLLMRequestHeaders(req *http.Request, cfg config) {
	if strings.TrimSpace(cfg.APIKey) != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	}
	req.Header.Set("Content-Type", "application/json")
}

// readHTTPErrorBody returns a compact diagnostic for failed HTTP responses.
func readHTTPErrorBody(body io.Reader) (string, error) {
	data, err := io.ReadAll(body)
	if err != nil {
		wrapped := fmt.Errorf("cannot read error response body: %w", err)
		return wrapped.Error(), wrapped
	}

	text := trimForSummary(string(data), httpErrorBodyPreviewChars, truncationStart)
	if text == "" {
		return "(empty error response body)", nil
	}
	return text, nil
}

// newLLMHTTPStatusError builds a compact, typed provider status error.
func newLLMHTTPStatusError(statusCode int, body string, err error) error {
	return &llmHTTPStatusError{
		StatusCode: statusCode,
		Body:       trimForSummary(body, httpErrorBodyPreviewChars, truncationStart),
		Err:        err,
	}
}

// callLLM sends the instruction and context to the model to obtain an execution plan.
func callLLM(ctx context.Context, client *http.Client, request llmPromptRequest) (string, error) {
	reqCtx, cancel := context.WithTimeout(ctx, request.Config.RequestTimeout)
	defer cancel()

	systemPrompt, userPrompt := buildLLMPrompts(request)
	return callPlanningPrompt(reqCtx, client, request.Config, systemPrompt, userPrompt)
}

// buildLLMPrompts builds the initial planning prompt pair.
func buildLLMPrompts(request llmPromptRequest) (string, string) {
	resolvedInstruction := resolveInstructionForPlanning(request.Instruction, request.State)
	if !request.Config.IncludeSessionMemory {
		resolvedInstruction = request.Instruction
	}

	request.ResolvedInstruction = resolvedInstruction
	if request.Config.PlanOnly {
		return buildPlanOnlySystemPrompt(), buildUserPrompt(request)
	}

	return buildSystemPrompt(), buildUserPrompt(request)
}

// callDiscoveryRepairLLM retries an empty planning response with a discovery-only repair prompt.
func callDiscoveryRepairLLM(ctx context.Context, client *http.Client, request discoveryPromptRequest) (string, error) {
	reqCtx, cancel := context.WithTimeout(ctx, request.Prompt.Config.RequestTimeout)
	defer cancel()

	systemPrompt, userPrompt := buildDiscoveryRepairLLMPrompts(request)
	return callPlanningPrompt(reqCtx, client, request.Prompt.Config, systemPrompt, userPrompt)
}

// buildDiscoveryRepairLLMPrompts builds the discovery repair prompt pair.
func buildDiscoveryRepairLLMPrompts(request discoveryPromptRequest) (string, string) {
	resolvedInstruction := resolveInstructionForPlanning(request.Prompt.Instruction, request.Prompt.State)
	if !request.Prompt.Config.IncludeSessionMemory {
		resolvedInstruction = request.Prompt.Instruction
	}

	request.Prompt.ResolvedInstruction = resolvedInstruction
	return buildSystemPrompt(), buildDiscoveryRepairPrompt(request)
}

// callPlanningPrompt sends a planning prompt pair to the model and returns the raw JSON response.
func callPlanningPrompt(ctx context.Context, client *http.Client, cfg config, systemPrompt string, userPrompt string) (string, error) {
	return doLLMRequest(ctx, client, cfg, chatCompletionRequest{
		Model:          cfg.Model,
		Temperature:    0,
		ResponseFormat: planningResponseFormat(cfg),
		Messages: []chatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
	})
}

// planningResponseFormat keeps strict JSON mode where providers advertise it.
func planningResponseFormat(cfg config) *responseFormat {
	if !cfg.SupportsResponseFormat {
		return nil
	}
	return &responseFormat{Type: "json_object"}
}

// streamSummarizeExecutions streams a short final answer based on real command output.
// Tokens are written to w as they arrive; the full string is returned for history.
func streamSummarizeExecutions(ctx context.Context, client *http.Client, cfg config, instruction string, executions []commandExecution, w io.Writer) (string, error) {
	var transcript strings.Builder
	for index, execution := range executions {
		fmt.Fprintf(&transcript, "Step %d\n", index+1)
		fmt.Fprintf(&transcript, "Purpose: %s\n", execution.Purpose)
		fmt.Fprintf(&transcript, "Command: %s\n", execution.Command)
		fmt.Fprintf(&transcript, "Exit code: %d\n", execution.ExitCode)
		fmt.Fprintf(&transcript, "%s\n\n", execution.PromptTranscript(cfg.SummaryOutputChars, cfg.TruncationStrategy))
	}

	reqCtx, cancel := context.WithTimeout(ctx, cfg.RequestTimeout)
	defer cancel()

	return doLLMStream(reqCtx, client, cfg, chatCompletionRequest{
		Model:       cfg.Model,
		Temperature: 0,
		Messages: []chatMessage{
			{
				Role: "system",
				Content: "You are the final response layer of a shell assistant. " +
					"Write only a short final answer for the user based on the real command outputs. " +
					"Do not mention JSON, plans, steps, risks, or confirmations. " +
					"If the user asked a question, answer it directly. " +
					"If the user asked to perform an action and it succeeded, say it is done in a natural way. " +
					"GROUNDING RULES: Read each step's output carefully before drawing any conclusion. " +
					"When output contains a table or list, read every row — do not skip any. " +
					"Do NOT claim something is absent unless you have read the full output and it is genuinely missing. " +
					"Different commands answer different sub-questions: do not merge their answers incorrectly (e.g. 'not running' does not mean 'not installed'). " +
					"Never claim an action was completed unless the executed commands clearly performed it or the output explicitly confirms it. " +
					"If there are concrete results, include them. " +
					"Keep it concise.",
			},
			{
				Role:    "user",
				Content: fmt.Sprintf("Original request:\n%s\n\nExecuted commands and outputs:\n%s", instruction, transcript.String()),
			},
		},
	}, w)
}

// parseResponse validates the JSON response returned by the model.
func parseResponse(raw string) (llmResponse, error) {
	jsonObject, ok := firstJSONObject(raw)
	if !ok {
		return llmResponse{}, fmt.Errorf("invalid llm response: no json object found")
	}

	var parsed llmResponse
	if err := json.Unmarshal([]byte(jsonObject), &parsed); err != nil {
		return llmResponse{}, fmt.Errorf("invalid llm response: %w", err)
	}
	if strings.TrimSpace(parsed.Summary) == "" {
		return llmResponse{}, fmt.Errorf("invalid llm response: missing summary")
	}
	for _, cmd := range parsed.Commands {
		if strings.TrimSpace(cmd.Command) == "" {
			return llmResponse{}, fmt.Errorf("invalid llm response: empty command")
		}
		if strings.TrimSpace(cmd.Purpose) == "" {
			return llmResponse{}, fmt.Errorf("invalid llm response: missing purpose")
		}
	}
	if parsed.RequiresInput && len(parsed.Commands) > 0 {
		return llmResponse{}, fmt.Errorf("invalid llm response: requires_input with commands")
	}

	return parsed, nil
}

// firstJSONObject extracts the first complete JSON object from model text.
func firstJSONObject(raw string) (string, bool) {
	start := strings.Index(raw, "{")
	if start < 0 {
		return "", false
	}

	depth := 0
	inString := false
	escaped := false
	for index := start; index < len(raw); index++ {
		char := raw[index]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			switch char {
			case '\\':
				escaped = true
			case '"':
				inString = false
			}
			continue
		}

		switch char {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return raw[start : index+1], true
			}
			if depth < 0 {
				return "", false
			}
		}
	}

	return "", false
}

// normalizePlan merges the model-reported risk with the local classification.
func normalizePlan(response llmResponse) (string, []commandPlan, error) {
	summary := strings.TrimSpace(response.Summary)
	plans := make([]commandPlan, 0, len(response.Commands))
	for _, item := range response.Commands {
		command := strings.TrimSpace(item.Command)
		local := classifyCommand(command)
		plans = append(plans, commandPlan{
			Command:              command,
			Purpose:              strings.TrimSpace(item.Purpose),
			Risk:                 higherRisk(strings.TrimSpace(strings.ToLower(item.Risk)), local.Risk),
			RequiresConfirmation: item.RequiresConfirmation || local.RequiresConfirmation,
			Classification:       local.Classification,
			LocalSafe:            local.Classification == classificationSafe && !local.RequiresConfirmation,
			Interactive:          item.Interactive,
			InteractiveReason:    strings.TrimSpace(item.InteractiveReason),
		})
	}
	return summary, plans, nil
}

// buildSystemPrompt defines the strict contract the model must follow.
func buildSystemPrompt() string {
	return strings.Join(buildSystemPromptSentences(), " ")
}

// buildSystemPromptSentences returns the stable system prompt contract.
func buildSystemPromptSentences() []string {
	return []string{
		"You are a shell planning assistant.",
		"You convert natural language instructions into shell commands for the user's current machine.",
		"You must be conservative, accurate, and avoid hallucinating tools or paths.",
		"Only use commands that are standard or clearly available from the provided context.",
		"Never propose interactive editors like nano, vim, less, top, or man.",
		"Do not use placeholders.",
		"Return pure shell commands only.",
		"Do not include explanatory echo, printf, comments, labels, banners, or formatting commands inside the command field.",
		"Do not chain commands with ';', '&&', '||', or pipes unless the user explicitly asked for a pipeline and it is strictly necessary.",
		"Prefer one atomic command per step.",
		"Use session memory to resolve follow-up references such as 'before', 'that', 'do it now', or 'the docker thing'.",
		"If the user is clearly continuing an earlier task, continue that task instead of treating the request as unrelated.",
		"Before setting requires_observation=true, first decide whether a command can directly produce the requested answer or value; if it can, return that command instead of a broader inspection command.",
		"If a later action depends on information that must be discovered from command output first, return only the information-gathering commands for this round and set requires_observation=true.",
		"Never include a command whose arguments or options would change based on the output of another command in the same response; describe it in observation_reason instead.",
		"When requires_observation=true, also set observation_reason to a short explanation of what still needs to be learned from the real output.",
		"After the shell provides that observed output in a later prompt, use it to produce the next commands.",
		"If a command cannot be built yet because a mandatory user-provided detail is still missing, return no commands and set requires_input=true.",
		"When requires_input=true, also set input_reason to a short explanation of which detail is missing.",
		"If the observed outputs already answer the user's question, return no commands and put the answer in summary instead of asking to run more commands.",
		"However, if the current user instruction explicitly asks to repeat, rerun, retry, or execute an earlier action again, treat it as a fresh execution request.",
		"You may briefly mention that the recent observation probably made the repeat unnecessary, but still propose the command again when it is concrete and safe enough to run through Shellia's normal confirmation flow.",
		"Do not repeat an inspection command that was already executed and already provided the needed information, unless the user explicitly asks to rerun it.",
		"When only a small detail is missing, prefer a short safe inspection or verification command over returning no commands.",
		"When investigating how a local tool or dependency is installed or managed, do not stop after a single unsuccessful ownership check if other plausible local discovery paths still exist.",
		"Do not refuse only because a referenced file has an unusual extension; if needed, inspect it safely first.",
		"If the task is ambiguous, choose the safest minimal plan.",
		"Return only strict JSON with this exact schema:",
		`{"summary":"short explanation","requires_observation":false,"observation_reason":"","requires_input":false,"input_reason":"","commands":[{"command":"string","purpose":"string","risk":"safe|medium|high","requires_confirmation":true,"interactive":false,"interactive_reason":""}]}.`,
		"The commands array may contain multiple commands in execution order.",
		"Any command that changes the filesystem, uses sudo, changes system users, permissions, services, packages, or network state must have requires_confirmation=true.",
		"Because Shellia already asks the user to confirm risky commands before execution, prefer a known non-interactive confirmation flag only when you are confident it is correct instead of making the tool ask for confirmation again.",
		"If a command launches a prompt, REPL, TUI, password prompt, interactive installer, fuzzy finder, or anything that needs a real terminal session, set interactive=true and explain why in interactive_reason.",
		"If observed output shows a confirmation prompt or another terminal question, do not repeat the same non-interactive command; choose a known non-interactive variant with high confidence or set interactive=true.",
		"If the request cannot be fulfilled safely with confidence, return an empty commands array.",
	}
}

// buildPlanOnlySystemPrompt defines the non-executing plan contract.
func buildPlanOnlySystemPrompt() string {
	return strings.Join(buildPlanOnlySystemPromptSentences(), " ")
}

// buildPlanOnlySystemPromptSentences returns the stable plan-only prompt contract.
func buildPlanOnlySystemPromptSentences() []string {
	return []string{
		"You are a shell planning assistant in non-executing plan mode.",
		"You produce an operational plan for a human to review and run manually; Shellia will not execute commands.",
		"You must be conservative, accurate, and avoid hallucinating tools or paths.",
		"Only use commands that are standard or clearly available from the provided context.",
		"Never propose interactive editors like nano, vim, less, top, or man.",
		"Do not use placeholders in the command field.",
		"Return pure shell commands only in command fields.",
		"Do not include explanatory echo, printf, comments, labels, banners, or formatting commands inside command fields unless the user's task is specifically to create file content.",
		"Split the plan into useful stages through command purposes: preparation, inspection, decision, and manual execution.",
		"Include only commands that can be written with certainty using currently known information.",
		"Avoid redundant inspection steps; prefer one command that returns the fields needed for a decision.",
		"If later work depends on command output, return only the inspection and preparation commands that are exact now, set requires_observation=true, and write observation_reason as explicit branches.",
		"Never include a command in the commands array whose arguments or options would only be known after seeing inspection output; put those commands in observation_reason instead.",
		"The observation_reason must say what to do if the output shows a usable value, and what to do if it does not.",
		"If a later command cannot be exact until the user chooses a value from output, describe the command shape in observation_reason using the value by name, not as a placeholder command.",
		"If exact planning is impossible because a mandatory user-provided detail is missing, return no commands and set requires_input=true with a short input_reason.",
		"Return only strict JSON with this exact schema:",
		`{"summary":"short operational plan summary","requires_observation":false,"observation_reason":"","requires_input":false,"input_reason":"","commands":[{"command":"string","purpose":"string","risk":"safe|medium|high","requires_confirmation":true,"interactive":false,"interactive_reason":""}]}.`,
		"The commands array may contain multiple commands in manual execution order.",
		"Any command that changes the filesystem, uses sudo, changes system users, permissions, services, packages, or network state must have requires_confirmation=true.",
		"If a command launches a prompt, REPL, TUI, password prompt, interactive installer, fuzzy finder, or anything that needs a real terminal session, set interactive=true and explain why in interactive_reason.",
		"If the request cannot be planned safely with confidence, return an empty commands array and explain it in summary.",
	}
}

// buildUserPrompt attaches the detected local context to the model prompt.
func buildUserPrompt(request llmPromptRequest) string {
	cfg := request.Config
	instruction := request.Instruction
	resolvedInstruction := request.ResolvedInstruction
	ctxInfo := request.ContextInfo
	history := request.History
	state := request.State
	observations := request.Observations

	historyBlock := ""
	if cfg.IncludeSessionMemory && len(history) > 0 {
		var b strings.Builder
		b.WriteString("\nRecent session context:\n")
		for i, entry := range history {
			fmt.Fprintf(&b, "%d. User: %s\n", i+1, entry.Instruction)
			fmt.Fprintf(&b, "   Result: %s\n", trimForSummary(entry.Result, historyEntryPreviewChars, truncationStart))
		}
		historyBlock = b.String()
	}

	memoryLines := make([]string, 0, 5)
	if cfg.IncludeSessionMemory && strings.TrimSpace(state.PendingIntent) != "" {
		memoryLines = append(memoryLines, "- pending_intent: "+state.PendingIntent)
	}
	if cfg.IncludeSessionMemory && strings.TrimSpace(state.LastSuggestedCommand) != "" {
		memoryLines = append(memoryLines, "- last_suggested_command: "+state.LastSuggestedCommand)
	}
	if cfg.IncludeSessionMemory && strings.TrimSpace(state.LastRuntimeHint) != "" {
		memoryLines = append(memoryLines, "- last_runtime_hint: "+state.LastRuntimeHint)
	}
	if cfg.IncludeSessionMemory && len(state.LastCreatedFiles) > 0 {
		memoryLines = append(memoryLines, "- last_created_files: "+strings.Join(state.LastCreatedFiles, ", "))
	}
	if cfg.IncludeSessionMemory && strings.TrimSpace(state.LastReferencedFile) != "" {
		memoryLines = append(memoryLines, "- last_referenced_file: "+state.LastReferencedFile)
	}

	memoryBlock := ""
	if len(memoryLines) > 0 {
		memoryBlock = "\nSession memory:\n" + strings.Join(memoryLines, "\n") + "\n"
	}

	resolutionBlock := ""
	if cfg.IncludeSessionMemory && strings.TrimSpace(resolvedInstruction) != "" && strings.TrimSpace(resolvedInstruction) != strings.TrimSpace(instruction) {
		resolutionBlock = "\nResolved planning context:\n" + resolvedInstruction + "\n"
	}

	reusableObservationBlock := ""
	if cfg.IncludeRecentObservations && len(observations) == 0 && len(state.LastObservations) > 0 && looksLikeReferenceFollowUp(instruction) {
		var b strings.Builder
		b.WriteString("\nRecent reusable observations:\n")
		for index, observation := range state.LastObservations {
			fmt.Fprintf(&b, "%d. Purpose: %s\n", index+1, observation.Purpose)
			fmt.Fprintf(&b, "   Command: %s\n", observation.Command)
			fmt.Fprintf(&b, "%s\n", indentLines(observation.Transcript, "   "))
		}
		reusableObservationBlock = b.String()
	}

	observationBlock := ""
	if cfg.IncludeRecentObservations && len(observations) > 0 {
		var b strings.Builder
		b.WriteString("\nObserved outputs from the current task:\n")
		for index, execution := range observations {
			fmt.Fprintf(&b, "%d. Purpose: %s\n", index+1, execution.Purpose)
			fmt.Fprintf(&b, "   Command: %s\n", execution.Command)
			fmt.Fprintf(&b, "%s\n", indentLines(execution.PromptTranscript(cfg.ObservationOutputChars, cfg.TruncationStrategy), "   "))
		}
		observationBlock = b.String()
	}

	planOnlyRules := ""
	if cfg.PlanOnly {
		planOnlyRules = "\nPlan-only mode:\n" +
			"- This is not an execution round and there is no automatic discovery repair.\n" +
			"- Include exact preparation commands that are useful before inspection.\n" +
			"- Include the smallest useful inspection command when information must be discovered.\n" +
			"- Explain manual decision branches in observation_reason when a later command depends on output.\n"
	}

	contextBlock := buildPromptContextBlock(cfg, ctxInfo)

	var prompt strings.Builder
	prompt.WriteString("User instruction:\n")
	prompt.WriteString(instruction)
	prompt.WriteString(resolutionBlock)
	prompt.WriteString(memoryBlock)
	prompt.WriteString(reusableObservationBlock)
	prompt.WriteString(observationBlock)
	prompt.WriteString("\nCurrent context:\n")
	prompt.WriteString(contextBlock)
	prompt.WriteString(historyBlock)
	prompt.WriteString("\n\nRules:\n")
	prompt.WriteString(strings.Join(buildUserPromptRules(), "\n"))
	prompt.WriteString("\n")
	prompt.WriteString(planOnlyRules)
	return prompt.String()
}

// buildUserPromptRules returns the stable planning rules included with every prompt.
func buildUserPromptRules() []string {
	return []string{
		"- Output exactly one JSON object. After the final closing brace, stop immediately. Do not repeat the JSON object. Do not append markdown, prose, or a second JSON object.",
		"- Commands run in the current Shellia session directory unless a command explicitly operates elsewhere.",
		"- Do not invent files, branches, remotes, package managers, or paths.",
		"- Prefer simple commands.",
		"- Return pure commands only, without echo/printf or shell decorations.",
		"- Split independent actions into separate commands instead of chaining them.",
		"- If a follow-up refers to an earlier task, use the resolved planning context, recent reusable observations, and session memory to continue it.",
		"- Recent reusable observations are provided for task continuity only — to resolve cross-turn references like \"that file\" or \"the docker thing\". They are NOT a reason to skip a fresh execution request.",
		"- If observed outputs from the CURRENT task round already answer the question, return no commands and answer directly in summary. Never skip commands based solely on reusable observations from prior turns.",
		"- Do not repeat an inspection command that already produced the needed information unless the user explicitly asks to rerun it.",
		"- If observed outputs from this task are provided, use them to decide the next commands instead of guessing.",
		"- If observed output shows a confirmation prompt or terminal question, do not repeat the same non-interactive command; choose a known non-interactive variant with high confidence or set interactive=true.",
		"- If Shellia already asks the user to confirm a risky command, avoid a second in-command confirmation prompt when a known non-interactive flag is available with high confidence.",
		"- When the user asks for a specific value from data that a command can output, prefer a command that extracts only that value instead of printing the full source data.",
		"- If a mandatory user-provided detail is still missing, return no commands and explain the missing detail in summary and input_reason.",
		"- If a command needs a real terminal session, set interactive=true and explain why in interactive_reason.",
		"- If a request is still somewhat underspecified but can be advanced safely, propose a short inspection or verification command instead of immediately returning no commands.",
		"- If a referenced file might contain executable code, inspect or verify it before refusing based only on its extension.",
		"- If the request cannot be fulfilled safely with confidence, return an empty commands array and explain it in summary.",
	}
}

// buildPromptContextBlock renders the local context fields enabled in config.
func buildPromptContextBlock(cfg config, ctxInfo contextInfo) string {
	lines := make([]string, 0, 7)
	if cfg.IncludeCWD {
		lines = append(lines, "- cwd: "+ctxInfo.CWD)
	}
	if cfg.IncludeUser {
		lines = append(lines, "- user: "+ctxInfo.User)
	}
	if cfg.IncludeOS {
		lines = append(lines, "- os: "+ctxInfo.OS)
	}
	if cfg.IncludeShell {
		lines = append(lines, "- shell: "+ctxInfo.Shell)
	}
	if cfg.IncludeGit {
		gitStatus := ctxInfo.Git.StatusShort
		if strings.TrimSpace(gitStatus) == "" {
			gitStatus = "(clean or empty)"
		}
		lines = append(lines,
			fmt.Sprintf("- git.is_repo: %t", ctxInfo.Git.IsRepo),
			"- git.branch: "+ctxInfo.Git.Branch,
			"- git.status_short:\n"+gitStatus,
		)
	}
	if len(lines) == 0 {
		return "(not shared by configuration)"
	}
	return strings.Join(lines, "\n")
}

// buildDiscoveryRepairPrompt adds focused discovery guidance on top of the normal planning context.
func buildDiscoveryRepairPrompt(request discoveryPromptRequest) string {
	basePrompt := buildUserPrompt(request.Prompt)
	previous := request.Previous

	var b strings.Builder
	b.WriteString(basePrompt)
	b.WriteString("\nDiscovery repair mode:\n")
	b.WriteString("- The previous planning response returned no commands.\n")
	b.WriteString("- Before asking the user for more detail, decide whether the missing information can be discovered locally from this machine.\n")
	b.WriteString("- Facts such as installed version, binary path, package manager ownership, installation method, config files, repo state, and runtime environment are discoverable local facts.\n")
	b.WriteString("- If those facts can be discovered safely, return only short discovery or inspection commands for this round and set requires_observation=true.\n")
	b.WriteString("- Do not stop after one unsuccessful ownership or installation check if other plausible local discovery paths still exist.\n")
	b.WriteString("- In your summary, briefly tell the user that the first verification was not conclusive and that you are continuing with another short investigation.\n")
	b.WriteString("- In this retry, do not return update, install, uninstall, or destructive action commands yet; discovery only.\n")
	b.WriteString("- If the missing detail truly depends on user preference, credentials, secrets, remote access, or another system that cannot be inspected from this machine, you may still return no commands.\n")
	b.WriteString("\nPrevious empty planning response:\n")
	fmt.Fprintf(&b, "- summary: %s\n", fallbackValue(strings.TrimSpace(previous.Summary), "(empty)"))
	fmt.Fprintf(&b, "- requires_input: %t\n", previous.RequiresInput)
	fmt.Fprintf(&b, "- input_reason: %s\n", fallbackValue(strings.TrimSpace(previous.InputReason), "(empty)"))
	fmt.Fprintf(&b, "- requires_observation: %t\n", previous.RequiresObservation)
	fmt.Fprintf(&b, "- observation_reason: %s\n", fallbackValue(strings.TrimSpace(previous.ObservationReason), "(empty)"))

	return b.String()
}

// shouldRetryWithDiscoveryRepair reports whether an empty first planning response deserves one discovery-only retry.
func shouldRetryWithDiscoveryRepair(response llmResponse, round int, executions []commandExecution) bool {
	if round != 0 || len(executions) > 0 || len(response.Commands) > 0 {
		return false
	}
	return response.RequiresInput || response.RequiresObservation
}

// trimForSummary trims long output by rune count to avoid splitting multi-byte UTF-8 characters.
// strategy controls which part of the text is kept when truncation is needed.
func trimForSummary(text string, maxChars int, strategy truncationStrategy) string {
	if maxChars <= 0 {
		return ""
	}

	runes := []rune(strings.TrimSpace(text))
	if len(runes) <= maxChars {
		return string(runes)
	}
	switch strategy {
	case truncationEnd:
		return "[truncated...]\n" + string(runes[len(runes)-maxChars:])
	case truncationMixed:
		head := maxChars / 3
		tail := maxChars - head
		omitted := len(runes) - head - tail
		return string(runes[:head]) +
			fmt.Sprintf("\n...[%d chars omitted]...\n", omitted) +
			string(runes[len(runes)-tail:])
	default: // truncationStart
		return string(runes[:maxChars]) + "\n...[truncated]"
	}
}

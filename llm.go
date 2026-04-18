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

// httpClient is shared across all LLM requests for connection pooling.
// Timeouts are enforced via per-request context, not on the client itself.
var httpClient = &http.Client{}

const (
	maxRetries     = 3
	retryBaseDelay = 500 * time.Millisecond
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

// isRetryable reports whether an HTTP status code is worth retrying.
func isRetryable(statusCode int) bool {
	return statusCode == 429 || (statusCode >= 500 && statusCode <= 504)
}

// doLLMRequest is the single non-streaming HTTP entry point for all model calls.
// It retries up to maxRetries times on transient errors (429, 5xx) with exponential backoff.
func doLLMRequest(ctx context.Context, cfg config, req chatCompletionRequest) (string, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("cannot encode LLM request: %w", err)
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
			return "", fmt.Errorf("cannot create LLM request: %w", err)
		}
		httpReq.Header.Set("Authorization", "Bearer "+cfg.APIKey)
		httpReq.Header.Set("Content-Type", "application/json")

		resp, err := httpClient.Do(httpReq)
		if err != nil {
			lastErr = fmt.Errorf("LLM request failed: %w", err)
			continue
		}

		responseBody, err = io.ReadAll(resp.Body)
		resp.Body.Close()
		statusCode = resp.StatusCode

		if err != nil {
			lastErr = fmt.Errorf("cannot read LLM response: %w", err)
			continue
		}
		if isRetryable(statusCode) {
			lastErr = fmt.Errorf("LLM request failed with status %d: %s", statusCode, strings.TrimSpace(string(responseBody)))
			continue
		}

		lastErr = nil
		break
	}

	if lastErr != nil {
		return "", lastErr
	}
	if statusCode < 200 || statusCode >= 300 {
		return "", fmt.Errorf("LLM request failed with status %d: %s", statusCode, strings.TrimSpace(string(responseBody)))
	}

	var envelope chatCompletionEnvelope
	if err := json.Unmarshal(responseBody, &envelope); err != nil {
		return "", fmt.Errorf("invalid LLM envelope: %w", err)
	}
	if len(envelope.Choices) == 0 || strings.TrimSpace(envelope.Choices[0].Message.Content) == "" {
		return "", fmt.Errorf("invalid LLM response: missing message content")
	}

	return envelope.Choices[0].Message.Content, nil
}

// doLLMStream performs a streaming LLM request.
// Delta tokens are written to w as they arrive; the full accumulated string is returned.
// The initial HTTP response is retried on transient errors before the stream is consumed.
func doLLMStream(ctx context.Context, cfg config, req chatCompletionRequest, w io.Writer) (string, error) {
	req.Stream = true
	body, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("cannot encode LLM request: %w", err)
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
			return "", fmt.Errorf("cannot create LLM request: %w", err)
		}
		httpReq.Header.Set("Authorization", "Bearer "+cfg.APIKey)
		httpReq.Header.Set("Content-Type", "application/json")

		resp, err = httpClient.Do(httpReq)
		if err != nil {
			lastErr = fmt.Errorf("LLM stream request failed: %w", err)
			continue
		}
		if isRetryable(resp.StatusCode) {
			body2, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			lastErr = fmt.Errorf("LLM request failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body2)))
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			body2, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return "", fmt.Errorf("LLM request failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body2)))
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
			continue // malformed chunk, skip
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

// callLLM sends the instruction and context to the model to obtain an execution plan.
func callLLM(ctx context.Context, cfg config, ctxInfo contextInfo, instruction string, history []historyEntry, state sessionState, observations []commandExecution) (string, error) {
	reqCtx, cancel := context.WithTimeout(ctx, cfg.RequestTimeout)
	defer cancel()

	resolvedInstruction := resolveInstructionForPlanning(instruction, state)

	return callPlanningPrompt(reqCtx, cfg, buildSystemPrompt(), buildUserPrompt(cfg, instruction, resolvedInstruction, ctxInfo, history, state, observations))
}

// callDiscoveryRepairLLM retries an empty planning response with a discovery-only repair prompt.
func callDiscoveryRepairLLM(ctx context.Context, cfg config, ctxInfo contextInfo, instruction string, history []historyEntry, state sessionState, observations []commandExecution, previous llmResponse) (string, error) {
	reqCtx, cancel := context.WithTimeout(ctx, cfg.RequestTimeout)
	defer cancel()

	resolvedInstruction := resolveInstructionForPlanning(instruction, state)

	return callPlanningPrompt(reqCtx, cfg, buildSystemPrompt(), buildDiscoveryRepairPrompt(cfg, instruction, resolvedInstruction, ctxInfo, history, state, observations, previous))
}

// callPlanningPrompt sends a planning prompt pair to the model and returns the raw JSON response.
func callPlanningPrompt(ctx context.Context, cfg config, systemPrompt string, userPrompt string) (string, error) {
	return doLLMRequest(ctx, cfg, chatCompletionRequest{
		Model:          cfg.Model,
		Temperature:    0,
		ResponseFormat: &responseFormat{Type: "json_object"},
		Messages: []chatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
	})
}

// streamSummarizeExecutions streams a short final answer based on real command output.
// Tokens are written to w as they arrive; the full string is returned for history.
func streamSummarizeExecutions(ctx context.Context, cfg config, instruction string, executions []commandExecution, w io.Writer) (string, error) {
	var transcript strings.Builder
	for index, execution := range executions {
		fmt.Fprintf(&transcript, "Step %d\n", index+1)
		fmt.Fprintf(&transcript, "Purpose: %s\n", execution.Purpose)
		fmt.Fprintf(&transcript, "Command: %s\n", execution.Command)
		fmt.Fprintf(&transcript, "Exit code: %d\n", execution.ExitCode)
		fmt.Fprintf(&transcript, "%s\n\n", execution.PromptTranscript(cfg.SummaryOutputChars))
	}

	reqCtx, cancel := context.WithTimeout(ctx, cfg.RequestTimeout)
	defer cancel()

	return doLLMStream(reqCtx, cfg, chatCompletionRequest{
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
	raw = strings.TrimSpace(raw)
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end < 0 || end < start {
		return llmResponse{}, fmt.Errorf("invalid LLM response: no JSON object found")
	}

	var parsed llmResponse
	if err := json.Unmarshal([]byte(raw[start:end+1]), &parsed); err != nil {
		return llmResponse{}, fmt.Errorf("invalid LLM response: %w", err)
	}
	if strings.TrimSpace(parsed.Summary) == "" {
		return llmResponse{}, fmt.Errorf("invalid LLM response: missing summary")
	}
	for _, cmd := range parsed.Commands {
		if strings.TrimSpace(cmd.Command) == "" {
			return llmResponse{}, fmt.Errorf("invalid LLM response: empty command")
		}
		if strings.TrimSpace(cmd.Purpose) == "" {
			return llmResponse{}, fmt.Errorf("invalid LLM response: missing purpose")
		}
	}
	if parsed.RequiresObservation && len(parsed.Commands) == 0 {
		return llmResponse{}, fmt.Errorf("invalid LLM response: requires_observation without commands")
	}
	if parsed.RequiresInput && len(parsed.Commands) > 0 {
		return llmResponse{}, fmt.Errorf("invalid LLM response: requires_input with commands")
	}

	return parsed, nil
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
	return "You are a shell planning assistant. " +
		"You convert natural language instructions into shell commands for the user's current machine. " +
		"You must be conservative, accurate, and avoid hallucinating tools or paths. " +
		"Only use commands that are standard or clearly available from the provided context. " +
		"Never propose interactive editors like nano, vim, less, top, or man. " +
		"Do not use placeholders. " +
		"Return pure shell commands only. " +
		"Do not include explanatory echo, printf, comments, labels, banners, or formatting commands inside the command field. " +
		"Do not chain commands with ';', '&&', '||', or pipes unless the user explicitly asked for a pipeline and it is strictly necessary. " +
		"Prefer one atomic command per step. " +
		"Use session memory to resolve follow-up references such as 'before', 'that', 'do it now', or 'the docker thing'. " +
		"If the user is clearly continuing an earlier task, continue that task instead of treating the request as unrelated. " +
		"If a later action depends on information that must be discovered from command output first, return only the information-gathering commands for this round and set requires_observation=true. " +
		"When requires_observation=true, also set observation_reason to a short explanation of what still needs to be learned from the real output. " +
		"After the shell provides that observed output in a later prompt, use it to produce the next commands. " +
		"If a command cannot be built yet because a mandatory user-provided detail is still missing, return no commands and set requires_input=true. " +
		"When requires_input=true, also set input_reason to a short explanation of which detail is missing. " +
		"If the observed outputs already answer the user's question, return no commands and put the answer in summary instead of asking to run more commands. " +
		"Do not repeat an inspection command that was already executed and already provided the needed information, unless the user explicitly asks to rerun it. " +
		"When only a small detail is missing, prefer a short safe inspection or verification command over returning no commands. " +
		"Do not refuse only because a referenced file has an unusual extension; if needed, inspect it safely first. " +
		"If the task is ambiguous, choose the safest minimal plan. " +
		"Return only strict JSON with this exact schema: " +
		`{"summary":"short explanation","requires_observation":false,"observation_reason":"","requires_input":false,"input_reason":"","commands":[{"command":"string","purpose":"string","risk":"safe|medium|high","requires_confirmation":true,"interactive":false,"interactive_reason":""}]}. ` +
		"The commands array may contain multiple commands in execution order. " +
		"Any command that changes the filesystem, uses sudo, changes system users, permissions, services, packages, or network state must have requires_confirmation=true. " +
		"If a command launches a prompt, REPL, TUI, password prompt, interactive installer, fuzzy finder, or anything that needs a real terminal session, set interactive=true and explain why in interactive_reason. " +
		"If the request cannot be fulfilled safely with confidence, return an empty commands array."
}

// buildUserPrompt attaches the detected local context to the model prompt.
func buildUserPrompt(cfg config, instruction string, resolvedInstruction string, ctxInfo contextInfo, history []historyEntry, state sessionState, observations []commandExecution) string {
	gitStatus := ctxInfo.Git.StatusShort
	if strings.TrimSpace(gitStatus) == "" {
		gitStatus = "(clean or empty)"
	}

	historyBlock := ""
	if len(history) > 0 {
		var b strings.Builder
		b.WriteString("\nRecent session context:\n")
		for i, entry := range history {
			fmt.Fprintf(&b, "%d. User: %s\n", i+1, entry.Instruction)
			fmt.Fprintf(&b, "   Result: %s\n", trimForSummary(entry.Result, 240))
		}
		historyBlock = b.String()
	}

	memoryLines := make([]string, 0, 5)
	if strings.TrimSpace(state.PendingIntent) != "" {
		memoryLines = append(memoryLines, "- pending_intent: "+state.PendingIntent)
	}
	if strings.TrimSpace(state.LastSuggestedCommand) != "" {
		memoryLines = append(memoryLines, "- last_suggested_command: "+state.LastSuggestedCommand)
	}
	if strings.TrimSpace(state.LastRuntimeHint) != "" {
		memoryLines = append(memoryLines, "- last_runtime_hint: "+state.LastRuntimeHint)
	}
	if len(state.LastCreatedFiles) > 0 {
		memoryLines = append(memoryLines, "- last_created_files: "+strings.Join(state.LastCreatedFiles, ", "))
	}
	if strings.TrimSpace(state.LastReferencedFile) != "" {
		memoryLines = append(memoryLines, "- last_referenced_file: "+state.LastReferencedFile)
	}

	memoryBlock := ""
	if len(memoryLines) > 0 {
		memoryBlock = "\nSession memory:\n" + strings.Join(memoryLines, "\n") + "\n"
	}

	resolutionBlock := ""
	if strings.TrimSpace(resolvedInstruction) != "" && strings.TrimSpace(resolvedInstruction) != strings.TrimSpace(instruction) {
		resolutionBlock = "\nResolved planning context:\n" + resolvedInstruction + "\n"
	}

	reusableObservationBlock := ""
	if len(observations) == 0 && len(state.LastObservations) > 0 {
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
	if len(observations) > 0 {
		var b strings.Builder
		b.WriteString("\nObserved outputs from the current task:\n")
		for index, execution := range observations {
			fmt.Fprintf(&b, "%d. Purpose: %s\n", index+1, execution.Purpose)
			fmt.Fprintf(&b, "   Command: %s\n", execution.Command)
			fmt.Fprintf(&b, "%s\n", indentLines(execution.PromptTranscript(cfg.ObservationOutputChars), "   "))
		}
		observationBlock = b.String()
	}

	return fmt.Sprintf(
		"User instruction:\n%s%s%s%s%s\nCurrent context:\n- cwd: %s\n- user: %s\n- os: %s\n- shell: %s\n- git.is_repo: %t\n- git.branch: %s\n- git.status_short:\n%s%s\n\nRules:\n- Commands must run in the current working directory unless a command explicitly operates elsewhere.\n- Do not invent files, branches, remotes, package managers, or paths.\n- Prefer simple commands.\n- Return pure commands only, without echo/printf or shell decorations.\n- Split independent actions into separate commands instead of chaining them.\n- If a follow-up refers to an earlier task, use the resolved planning context, recent reusable observations, and session memory to continue it.\n- If observed outputs from this task or recent reusable observations already answer the question, return no commands and answer directly in summary.\n- Do not repeat an inspection command that already produced the needed information unless the user explicitly asks to rerun it.\n- If observed outputs from this task are provided, use them to decide the next commands instead of guessing.\n- If a mandatory user-provided detail is still missing, return no commands and explain the missing detail in summary and input_reason.\n- If a command needs a real terminal session, set interactive=true and explain why in interactive_reason.\n- If a request is still somewhat underspecified but can be advanced safely, propose a short inspection or verification command instead of immediately returning no commands.\n- If a referenced file might contain executable code, inspect or verify it before refusing based only on its extension.\n- If the request cannot be fulfilled safely with confidence, return an empty commands array and explain it in summary.\n",
		instruction,
		resolutionBlock,
		memoryBlock,
		reusableObservationBlock,
		observationBlock,
		ctxInfo.CWD,
		ctxInfo.User,
		ctxInfo.OS,
		ctxInfo.Shell,
		ctxInfo.Git.IsRepo,
		ctxInfo.Git.Branch,
		gitStatus,
		historyBlock,
	)
}

// buildDiscoveryRepairPrompt adds focused discovery guidance on top of the normal planning context.
func buildDiscoveryRepairPrompt(cfg config, instruction string, resolvedInstruction string, ctxInfo contextInfo, history []historyEntry, state sessionState, observations []commandExecution, previous llmResponse) string {
	basePrompt := buildUserPrompt(cfg, instruction, resolvedInstruction, ctxInfo, history, state, observations)

	var b strings.Builder
	b.WriteString(basePrompt)
	b.WriteString("\nDiscovery repair mode:\n")
	b.WriteString("- The previous planning response returned no commands.\n")
	b.WriteString("- Before asking the user for more detail, decide whether the missing information can be discovered locally from this machine.\n")
	b.WriteString("- Facts such as installed version, binary path, package manager ownership, installation method, config files, repo state, and runtime environment are discoverable local facts.\n")
	b.WriteString("- If those facts can be discovered safely, return only short discovery or inspection commands for this round and set requires_observation=true.\n")
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
	return response.RequiresInput
}

// trimForSummary trims long output by rune count to avoid splitting multi-byte UTF-8 characters.
func trimForSummary(text string, max int) string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) <= max {
		return string(runes)
	}
	return string(runes[:max]) + "\n...[truncated]"
}

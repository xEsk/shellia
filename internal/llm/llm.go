package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/xEsk/shellia/internal/safety"
)

const (
	maxRetries                = 3
	retryBaseDelay            = 500 * time.Millisecond
	maxLLMResponseBytes       = 8 << 20
	httpErrorBodyPreviewChars = 1200
	historyEntryPreviewChars  = 240
)

var (
	errLLMRedirect         = errors.New("llm endpoint redirects are not allowed")
	errLLMResponseTooLarge = errors.New("llm response too large")
)

// ClientOptions contains the LLM transport and provider capability settings.
type ClientOptions struct {
	BaseURL                string
	APIKey                 string
	Model                  string
	RequestTimeout         time.Duration
	SupportsResponseFormat bool
	SupportsJSONSchema     bool
	RequestParams          map[string]any
}

// PromptOptions contains the configuration that controls prompt content.
type PromptOptions struct {
	PlanOnly                  bool
	IncludeCWD                bool
	IncludeOS                 bool
	IncludeShell              bool
	IncludeUser               bool
	IncludeSessionMemory      bool
	IncludeRecentObservations bool
	MaxObservationEntries     int
	ObservationOutputChars    int
	TruncationStrategy        truncationStrategy
}

type chatCompletionRequest struct {
	Model          string          `json:"model"`
	ResponseFormat *responseFormat `json:"response_format,omitempty"`
	Messages       []chatMessage   `json:"messages"`
}

type responseFormat struct {
	Type       string              `json:"type"`
	JSONSchema *jsonSchemaResponse `json:"json_schema,omitempty"`
}

type jsonSchemaResponse struct {
	Name   string         `json:"name"`
	Strict bool           `json:"strict"`
	Schema map[string]any `json:"schema"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatCompletionEnvelope struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
			Refusal string `json:"refusal"`
		} `json:"message"`
	} `json:"choices"`
}

// PromptRequest groups the context needed to build one planning prompt.
type PromptRequest struct {
	Config                    PromptOptions
	ContextInfo               contextInfo
	Instruction               string
	ResolvedInstruction       string
	History                   []historyEntry
	ContextRevision           int
	RetrievedContext          []historyEntry
	State                     sessionState
	Observations              []commandExecution
	Skipped                   []skippedCommand
	LatestBatchExecutionStart int
	LatestBatchSkippedStart   int
	PlanningRoundsRemaining   int
	Operation                 string
	SuccessCriteria           string
	DecisionError             string
	RetryObservationAvailable bool
	PreviousDecision          *Response
	Attempts                  []workflowAttempt
}

type Command struct {
	Command              string `json:"command"`
	Purpose              string `json:"purpose"`
	Risk                 string `json:"risk"`
	RequiresConfirmation bool   `json:"requires_confirmation"`
	IndependentOnFailure bool   `json:"independent_on_failure"`
	RepeatReason         string `json:"repeat_reason"`
	Interactive          bool   `json:"interactive"`
	InteractiveReason    string `json:"interactive_reason"`
}

// Offer describes an optional typed objective proposed for a later turn.
type Offer struct {
	Mode      string `json:"mode"`
	Objective string `json:"objective"`
	Summary   string `json:"summary"`
}

type Response struct {
	Action          string    `json:"action"`
	Operation       string    `json:"operation"`
	SuccessCriteria string    `json:"success_criteria"`
	Summary         string    `json:"summary"`
	ContextRefs     []string  `json:"context_refs"`
	Offer           Offer     `json:"offer"`
	BlockerKind     string    `json:"blocker_kind"`
	BlockerReason   string    `json:"blocker_reason"`
	Commands        []Command `json:"commands"`
}

// ModelRefusalError carries a provider refusal returned outside a structured schema.
type ModelRefusalError struct {
	Reason string
}

// Error returns the explicit refusal reason.
func (err *ModelRefusalError) Error() string {
	if err == nil || strings.TrimSpace(err.Reason) == "" {
		return "model refused request"
	}
	return "model refused request: " + strings.TrimSpace(err.Reason)
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
func doLLMRequest(ctx context.Context, client *http.Client, options ClientOptions, req chatCompletionRequest) (string, error) {
	if err := validateBaseURL(options.BaseURL); err != nil {
		return "", err
	}
	if err := validateRequestParams(options.RequestParams); err != nil {
		return "", err
	}

	payload := make(map[string]any, len(options.RequestParams)+3)
	for key, value := range options.RequestParams {
		payload[key] = value
	}
	payload["model"] = req.Model
	payload["messages"] = req.Messages
	if req.ResponseFormat != nil {
		payload["response_format"] = req.ResponseFormat
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("cannot encode llm request: %w", err)
	}
	if client == nil {
		client = http.DefaultClient
	}
	requestClient := clientWithoutRedirects(client)

	endpoint := strings.TrimRight(options.BaseURL, "/") + "/chat/completions"

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

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			return "", fmt.Errorf("cannot create llm request: %w", err)
		}
		applyLLMRequestHeaders(httpReq, options)

		resp, err := requestClient.Do(httpReq)
		if err != nil {
			if errors.Is(err, errLLMRedirect) {
				return "", fmt.Errorf("llm request rejected: %w", errLLMRedirect)
			}
			lastErr = fmt.Errorf("llm request failed: %w", err)
			continue
		}

		responseBody, err = readLLMResponseBody(resp.Body)
		resp.Body.Close()
		statusCode = resp.StatusCode

		if err != nil {
			if errors.Is(err, errLLMResponseTooLarge) {
				return "", err
			}
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
	if len(envelope.Choices) == 0 {
		return "", fmt.Errorf("invalid llm response: missing message content")
	}
	message := envelope.Choices[0].Message
	if refusal := strings.TrimSpace(message.Refusal); refusal != "" {
		return "", &ModelRefusalError{Reason: refusal}
	}
	if strings.TrimSpace(message.Content) == "" {
		return "", fmt.Errorf("invalid llm response: missing message content")
	}

	return message.Content, nil
}

// validateBaseURL rejects malformed endpoints and cleartext transport outside loopback.
func validateBaseURL(rawURL string) error {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return fmt.Errorf("invalid llm base URL: %w", err)
	}
	if parsed.Hostname() == "" {
		return fmt.Errorf("invalid llm base URL: missing host")
	}

	switch strings.ToLower(parsed.Scheme) {
	case "https":
		return nil
	case "http":
		if isLoopbackHostname(parsed.Hostname()) {
			return nil
		}
		return fmt.Errorf("invalid llm base URL: remote endpoints must use HTTPS")
	default:
		return fmt.Errorf("invalid llm base URL: scheme must be HTTP or HTTPS")
	}
}

// isLoopbackHostname reports whether a URL hostname is local-only.
func isLoopbackHostname(hostname string) bool {
	hostname = strings.ToLower(strings.TrimSpace(hostname))
	if hostname == "localhost" {
		return true
	}
	ip := net.ParseIP(hostname)
	return ip != nil && ip.IsLoopback()
}

// clientWithoutRedirects clones the caller's client and prevents credential forwarding.
func clientWithoutRedirects(client *http.Client) *http.Client {
	clone := *client
	clone.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return errLLMRedirect
	}
	return &clone
}

// readLLMResponseBody reads one bounded provider response.
func readLLMResponseBody(body io.Reader) ([]byte, error) {
	responseBody, err := io.ReadAll(io.LimitReader(body, maxLLMResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if len(responseBody) > maxLLMResponseBytes {
		return nil, fmt.Errorf("%w: limit is %d bytes", errLLMResponseTooLarge, maxLLMResponseBytes)
	}
	return responseBody, nil
}

// applyLLMRequestHeaders attaches the shared headers for OpenAI-compatible requests.
func applyLLMRequestHeaders(req *http.Request, options ClientOptions) {
	if strings.TrimSpace(options.APIKey) != "" {
		req.Header.Set("Authorization", "Bearer "+options.APIKey)
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

// callPlanningPrompt sends a planning prompt pair to the model and returns the raw JSON response.
func callPlanningPrompt(ctx context.Context, client *http.Client, options ClientOptions, systemPrompt string, userPrompt string) (string, error) {
	requestCtx, cancel := context.WithTimeout(ctx, options.RequestTimeout)
	defer cancel()

	return doLLMRequest(requestCtx, client, options, chatCompletionRequest{
		Model:          options.Model,
		ResponseFormat: planningResponseFormat(options),
		Messages: []chatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
	})
}

// planningResponseFormat selects the strongest configured structured response mode.
func planningResponseFormat(options ClientOptions) *responseFormat {
	if options.SupportsJSONSchema {
		return &responseFormat{
			Type: "json_schema",
			JSONSchema: &jsonSchemaResponse{
				Name:   "shellia_planning_decision",
				Strict: true,
				Schema: planningResponseSchema(),
			},
		}
	}
	if !options.SupportsResponseFormat {
		return nil
	}
	return &responseFormat{Type: "json_object"}
}

// planningResponseSchema defines the typed planning decision accepted by Shellia.
func planningResponseSchema() map[string]any {
	object := func(properties map[string]any, required ...string) map[string]any {
		return map[string]any{
			"type":                 "object",
			"properties":           properties,
			"required":             required,
			"additionalProperties": false,
		}
	}
	stringValues := func(values ...string) map[string]any {
		return map[string]any{"type": "string", "enum": values}
	}

	offer := object(map[string]any{
		"mode":      stringValues("", "plan", "execute"),
		"objective": map[string]any{"type": "string"},
		"summary":   map[string]any{"type": "string"},
	}, "mode", "objective", "summary")

	command := object(map[string]any{
		"command":                map[string]any{"type": "string"},
		"purpose":                map[string]any{"type": "string"},
		"risk":                   map[string]any{"type": "string"},
		"requires_confirmation":  map[string]any{"type": "boolean"},
		"independent_on_failure": map[string]any{"type": "boolean"},
		"repeat_reason":          stringValues("", "user_requested", "retry", "verify_after_change", "poll_changed_state"),
		"interactive":            map[string]any{"type": "boolean"},
		"interactive_reason":     map[string]any{"type": "string"},
	}, "command", "purpose", "risk", "requires_confirmation", "independent_on_failure", "repeat_reason", "interactive", "interactive_reason")

	return object(map[string]any{
		"action":           stringValues("execute", "plan", "retrieve_context", "complete", "blocked"),
		"operation":        stringValues("answer", "observe", "act", "capability"),
		"success_criteria": map[string]any{"type": "string"},
		"summary":          map[string]any{"type": "string"},
		"context_refs": map[string]any{
			"type":  "array",
			"items": map[string]any{"type": "string"},
		},
		"offer":          offer,
		"blocker_kind":   stringValues("", "missing_input", "unavailable", "unsafe_to_continue"),
		"blocker_reason": map[string]any{"type": "string"},
		"commands": map[string]any{
			"type":  "array",
			"items": command,
		},
	}, "action", "operation", "success_criteria", "summary", "context_refs", "offer", "blocker_kind", "blocker_reason", "commands")
}

// normalizePlan merges the model-reported risk with the local classification.
func normalizePlan(response Response) (string, []commandPlan, error) {
	summary := strings.TrimSpace(response.Summary)
	plans := make([]commandPlan, 0, len(response.Commands))
	for _, item := range response.Commands {
		command := strings.TrimSpace(item.Command)
		local := safety.ClassifyCommand(command)
		plans = append(plans, commandPlan{
			Command:              command,
			Purpose:              strings.TrimSpace(item.Purpose),
			Risk:                 safety.HigherRisk(strings.TrimSpace(strings.ToLower(item.Risk)), local.Risk),
			RequiresConfirmation: item.RequiresConfirmation || local.RequiresConfirmation,
			Classification:       local.Classification,
			LocalSafe:            local.Classification == safety.ClassificationSafe && !local.RequiresConfirmation,
			IndependentOnFailure: item.IndependentOnFailure,
			RepeatReason:         repeatReason(item.RepeatReason),
			Interactive:          item.Interactive,
			InteractiveReason:    strings.TrimSpace(item.InteractiveReason),
		})
	}
	return summary, plans, nil
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

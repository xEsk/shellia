package llm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	configpkg "github.com/xEsk/shellia/internal/config"
	"github.com/xEsk/shellia/internal/core"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

// TestPlanningResponseSchemaClosesAndRequiresAllObjects checks the strict
// schema follows the provider subset that requires closed, fully required objects.
func TestPlanningResponseSchemaClosesAndRequiresAllObjects(t *testing.T) {
	format := planningResponseFormat(ClientOptions{SupportsJSONSchema: true})
	if format == nil || format.JSONSchema == nil {
		t.Fatalf("planningResponseFormat() = %#v, want JSON schema", format)
	}

	var checkSchema func(string, map[string]any)
	checkSchema = func(path string, schema map[string]any) {
		t.Helper()
		switch schema["type"] {
		case "object":
			properties, ok := schema["properties"].(map[string]any)
			if !ok {
				t.Fatalf("%s.properties = %#v, want object", path, schema["properties"])
			}
			required, ok := schema["required"].([]string)
			if !ok {
				t.Fatalf("%s.required = %#v, want string list", path, schema["required"])
			}
			if schema["additionalProperties"] != false {
				t.Fatalf("%s.additionalProperties = %#v, want false", path, schema["additionalProperties"])
			}
			if len(required) != len(properties) {
				t.Fatalf("%s.required = %#v, want all %d properties", path, required, len(properties))
			}
			for name, property := range properties {
				if !containsString(required, name) {
					t.Fatalf("%s.required = %#v, missing %q", path, required, name)
				}
				propertySchema, ok := property.(map[string]any)
				if !ok {
					t.Fatalf("%s.%s = %#v, want schema object", path, name, property)
				}
				checkSchema(path+"."+name, propertySchema)
			}
		case "array":
			items, ok := schema["items"].(map[string]any)
			if !ok {
				t.Fatalf("%s.items = %#v, want schema object", path, schema["items"])
			}
			checkSchema(path+"[]", items)
		}
	}

	checkSchema("schema", format.JSONSchema.Schema)
}

// TestPlanningResponseSchemaLeavesRuntimeEvidenceOutOfTheWireContract checks
// provider schemas do not ask the model to reproduce Shellia-owned metadata.
func TestPlanningResponseSchemaLeavesRuntimeEvidenceOutOfTheWireContract(t *testing.T) {
	schema := planningResponseSchema()
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema.properties = %#v, want object", schema["properties"])
	}
	for _, field := range []string{"evidence_source", "freshness", "completion_basis"} {
		if _, exists := properties[field]; exists {
			t.Fatalf("schema.properties contains runtime-owned field %q", field)
		}
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

type fixedByteReader struct {
	remaining int
	read      int
}

func (reader *fixedByteReader) Read(target []byte) (int, error) {
	if reader.remaining == 0 {
		return 0, io.EOF
	}
	n := min(len(target), reader.remaining)
	for index := range n {
		target[index] = 'x'
	}
	reader.remaining -= n
	reader.read += n
	return n, nil
}

func validLLMHTTPResponse() *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"ok"}}]}`)),
	}
}

// TestCallPlanningPromptOmitsUnconfiguredRequestParams checks absent provider fields use provider defaults.
func TestCallPlanningPromptOmitsUnconfiguredRequestParams(t *testing.T) {
	var body map[string]any
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatalf("Decode(request body) error = %v", err)
		}
		return validLLMHTTPResponse(), nil
	})}

	_, err := callPlanningPrompt(t.Context(), client, ClientOptions{
		BaseURL:        "http://127.0.0.1:11434/v1",
		Model:          "provider-default",
		RequestTimeout: time.Second,
	}, "system", "user")
	if err != nil {
		t.Fatalf("callPlanningPrompt() error = %v", err)
	}
	if _, ok := body["temperature"]; ok {
		t.Fatalf("request body includes temperature: %#v", body)
	}
}

// TestCallPlanningPromptIncludesConfiguredRequestParams checks provider body fields are merged without mutating the profile.
func TestCallPlanningPromptIncludesConfiguredRequestParams(t *testing.T) {
	params := map[string]any{
		"temperature":      int64(1),
		"reasoning_effort": "medium",
		"stop":             []any{"END", "STOP"},
		"thinking":         map[string]any{"type": "enabled"},
	}
	wantParams := map[string]any{
		"temperature":      int64(1),
		"reasoning_effort": "medium",
		"stop":             []any{"END", "STOP"},
		"thinking":         map[string]any{"type": "enabled"},
	}
	var body map[string]any
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatalf("Decode(request body) error = %v", err)
		}
		return validLLMHTTPResponse(), nil
	})}

	_, err := callPlanningPrompt(t.Context(), client, ClientOptions{
		BaseURL:        "http://127.0.0.1:11434/v1",
		Model:          "custom-model",
		RequestTimeout: time.Second,
		RequestParams:  params,
	}, "system", "user")
	if err != nil {
		t.Fatalf("callPlanningPrompt() error = %v", err)
	}
	wantBodyFields := map[string]any{
		"temperature":      float64(1),
		"reasoning_effort": "medium",
		"stop":             []any{"END", "STOP"},
		"thinking":         map[string]any{"type": "enabled"},
	}
	for key, want := range wantBodyFields {
		if got := body[key]; !reflect.DeepEqual(got, want) {
			t.Fatalf("request body %s = %#v, want %#v", key, got, want)
		}
	}
	if body["model"] != "custom-model" {
		t.Fatalf("request body model = %#v, want custom-model", body["model"])
	}
	if !reflect.DeepEqual(params, wantParams) {
		t.Fatalf("RequestParams mutated to %#v, want %#v", params, wantParams)
	}
}

// TestValidateRequestParamsRejectsProtectedFields checks provider parameters cannot change Shellia's wire contract.
func TestValidateRequestParamsRejectsProtectedFields(t *testing.T) {
	protected := []string{
		"model",
		"messages",
		"response_format",
		"stream",
		"stream_options",
		"n",
		"tools",
		"tool_choice",
		"parallel_tool_calls",
		"functions",
		"function_call",
		"modalities",
		"audio",
		"web_search_options",
	}
	for _, key := range protected {
		t.Run(key, func(t *testing.T) {
			err := validateRequestParams(map[string]any{key: true})
			if err == nil {
				t.Fatalf("validateRequestParams(%q) error = nil, want protected-field error", key)
			}
			if !strings.Contains(err.Error(), "request_params."+key) || strings.Contains(err.Error(), "true") {
				t.Fatalf("validateRequestParams(%q) error = %q, want path without value", key, err.Error())
			}
		})
	}

	allowed := []struct {
		name   string
		params map[string]any
	}{
		{name: "case-sensitive key", params: map[string]any{"Model": "provider-extension"}},
		{name: "nested protected name", params: map[string]any{"thinking": map[string]any{"model": "provider-extension"}}},
	}
	for _, tt := range allowed {
		t.Run(tt.name, func(t *testing.T) {
			if err := validateRequestParams(tt.params); err != nil {
				t.Fatalf("validateRequestParams() error = %v, want allowed", err)
			}
		})
	}
}

// TestValidateRequestParamsRejectsNonJSONValues checks invalid TOML-to-JSON shapes fail with their path.
func TestValidateRequestParamsRejectsNonJSONValues(t *testing.T) {
	tests := []struct {
		name     string
		params   map[string]any
		wantPath string
	}{
		{
			name:     "datetime",
			params:   map[string]any{"created_at": time.Date(2026, time.August, 11, 8, 0, 0, 0, time.UTC)},
			wantPath: "request_params.created_at",
		},
		{
			name:     "nested nan",
			params:   map[string]any{"sampling": map[string]any{"temperature": math.NaN()}},
			wantPath: "request_params.sampling.temperature",
		},
		{
			name:     "infinity in array",
			params:   map[string]any{"levels": []any{0.5, math.Inf(1)}},
			wantPath: "request_params.levels[1]",
		},
		{
			name:     "null",
			params:   map[string]any{"optional": nil},
			wantPath: "request_params.optional",
		},
		{
			name:     "typed nil array",
			params:   map[string]any{"stop": []any(nil)},
			wantPath: "request_params.stop",
		},
		{
			name:     "typed nil table array",
			params:   map[string]any{"strategies": []map[string]any(nil)},
			wantPath: "request_params.strategies",
		},
		{
			name:     "typed nil table",
			params:   map[string]any{"thinking": map[string]any(nil)},
			wantPath: "request_params.thinking",
		},
		{
			name:     "unsupported type",
			params:   map[string]any{"events": make(chan int)},
			wantPath: "request_params.events",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRequestParams(tt.params)
			if err == nil {
				t.Fatal("validateRequestParams() error = nil, want incompatible-value error")
			}
			if !strings.Contains(err.Error(), tt.wantPath) || !strings.Contains(err.Error(), "not JSON-compatible") {
				t.Fatalf("validateRequestParams() error = %q, want path %q", err.Error(), tt.wantPath)
			}
		})
	}
}

// TestDoLLMRequestRejectsInvalidRequestParamsBeforeSending checks internal callers cannot bypass the wire guard.
func TestDoLLMRequestRejectsInvalidRequestParamsBeforeSending(t *testing.T) {
	called := false
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		called = true
		return validLLMHTTPResponse(), nil
	})}

	_, err := doLLMRequest(t.Context(), client, ClientOptions{
		BaseURL:       "http://127.0.0.1:11434/v1",
		RequestParams: map[string]any{"model": "override"},
	}, chatCompletionRequest{Model: "shellia-model"})
	if err == nil || !strings.Contains(err.Error(), "request_params.model") {
		t.Fatalf("doLLMRequest() error = %v, want protected model path", err)
	}
	if called {
		t.Fatal("doLLMRequest() sent HTTP before rejecting request_params.model")
	}
}

// TestDoLLMRequestReusesRequestParamsBodyAcrossRetries checks retries cannot observe a mutated provider payload.
func TestDoLLMRequestReusesRequestParamsBodyAcrossRetries(t *testing.T) {
	var bodies []string
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatalf("ReadAll(request body) error = %v", err)
		}
		bodies = append(bodies, string(body))
		if len(bodies) == 1 {
			return &http.Response{
				StatusCode: http.StatusTooManyRequests,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader("retry")),
			}, nil
		}
		return validLLMHTTPResponse(), nil
	})}

	_, err := doLLMRequest(t.Context(), client, ClientOptions{
		BaseURL:       "http://127.0.0.1:11434/v1",
		RequestParams: map[string]any{"temperature": int64(1)},
	}, chatCompletionRequest{Model: "custom-model", Messages: []chatMessage{{Role: "user", Content: "hello"}}})
	if err != nil {
		t.Fatalf("doLLMRequest() error = %v", err)
	}
	if len(bodies) != 2 || bodies[0] != bodies[1] {
		t.Fatalf("retry bodies = %#v, want two identical requests", bodies)
	}
}

// TestDoLLMRequestRejectsRemoteHTTPBeforeSendingCredentials checks bearer credentials never cross cleartext transport.
func TestDoLLMRequestRejectsRemoteHTTPBeforeSendingCredentials(t *testing.T) {
	called := false
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		called = true
		return validLLMHTTPResponse(), nil
	})}

	_, err := doLLMRequest(t.Context(), client, ClientOptions{
		BaseURL: "http://api.example.invalid/v1",
		APIKey:  "audit-secret",
	}, chatCompletionRequest{})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "https") {
		t.Fatalf("doLLMRequest() error = %v, want remote HTTPS requirement", err)
	}
	if called {
		t.Fatal("doLLMRequest() sent a request before rejecting remote HTTP")
	}
}

// TestDoLLMRequestAllowsLoopbackHTTP checks local OpenAI-compatible endpoints remain supported.
func TestDoLLMRequestAllowsLoopbackHTTP(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return validLLMHTTPResponse(), nil
	})}

	content, err := doLLMRequest(t.Context(), client, ClientOptions{
		BaseURL: "http://127.0.0.1:11434/v1",
	}, chatCompletionRequest{})
	if err != nil {
		t.Fatalf("doLLMRequest() error = %v", err)
	}
	if content != "ok" {
		t.Fatalf("doLLMRequest() content = %q, want ok", content)
	}
}

// TestDoLLMRequestRejectsRedirects checks credentials cannot be forwarded to a redirected endpoint.
func TestDoLLMRequestRejectsRedirects(t *testing.T) {
	requests := 0
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		if requests == 1 {
			response := &http.Response{
				StatusCode: http.StatusFound,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader("redirect")),
			}
			response.Header.Set("Location", "https://redirected.example.invalid/v1/chat/completions")
			return response, nil
		}
		return validLLMHTTPResponse(), nil
	})}

	_, err := doLLMRequest(t.Context(), client, ClientOptions{
		BaseURL: "https://api.example.invalid/v1",
		APIKey:  "audit-secret",
	}, chatCompletionRequest{})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "redirect") {
		t.Fatalf("doLLMRequest() error = %v, want redirect rejection", err)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want only the original endpoint request", requests)
	}
}

// TestDoLLMRequestRejectsOversizedResponse checks provider bodies are bounded before decoding.
func TestDoLLMRequestRejectsOversizedResponse(t *testing.T) {
	const (
		maximumResponseBytes = 8 << 20
		providedBytes        = 16 << 20
	)
	body := &fixedByteReader{remaining: providedBytes}
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(body),
		}, nil
	})}

	_, err := doLLMRequest(t.Context(), client, ClientOptions{
		BaseURL: "https://api.example.invalid/v1",
	}, chatCompletionRequest{})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "too large") {
		t.Fatalf("doLLMRequest() error = %v, want response-too-large error", err)
	}
	if body.read > maximumResponseBytes+1 {
		t.Fatalf("response bytes read = %d, want at most %d", body.read, maximumResponseBytes+1)
	}
}

// TestBuildUserPromptGoldenCharacterizes the complete prompt contract before
// extracting its sections into prompt.go.
func TestBuildUserPromptGoldenCharacterizes(t *testing.T) {
	request := representativePromptRequest()
	want, err := os.ReadFile("testdata/build_user_prompt.golden")
	if err != nil {
		t.Logf("buildUserPrompt() current output:\n%s", buildUserPrompt(request))
		t.Fatalf("read golden prompt: %v", err)
	}

	got := buildUserPrompt(request)
	if got != string(want) {
		t.Fatalf("buildUserPrompt() changed\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}

	request.Config.PlanOnly = false
	if buildUserPrompt(request) == string(want) {
		t.Fatal("golden prompt did not detect execution-authority drift")
	}
}

func TestBuildUserPromptSessionResultCatalog(t *testing.T) {
	request := PromptRequest{
		Config:      PromptOptions{IncludeSessionMemory: true, ObservationOutputChars: 1200},
		Instruction: "Reformat the earlier answer",
		History: []historyEntry{{
			ID:             "result-4",
			Instruction:    "List ports",
			Outcome:        core.TurnOutcomeCompleted,
			Result:         strings.Repeat("x", 300) + "SECRET_TAIL",
			CharacterCount: 311,
		}},
	}

	_, prompt := buildLLMPrompts(request)
	for _, required := range []string{
		"Session result catalog:",
		"id: result-4",
		"instruction: List ports",
		"outcome: completed",
		"character_count: 311",
		"preview: " + trimForSummary(request.History[0].Result, historyEntryPreviewChars, truncationStart),
	} {
		if !strings.Contains(prompt, required) {
			t.Fatalf("buildUserPrompt() missing catalog metadata %q: %q", required, prompt)
		}
	}
	if strings.Contains(prompt, "SECRET_TAIL") {
		t.Fatalf("buildUserPrompt() leaked unretrieved session result tail: %q", prompt)
	}
}

// TestBuildUserPromptIncludesCompleteRetrievedSessionContext checks catalog
// previews cannot expose a result tail until the runtime loads a revision.
func TestBuildUserPromptIncludesCompleteRetrievedSessionContext(t *testing.T) {
	entry := historyEntry{
		ID:             "result-2",
		Instruction:    "Inspect the deployment",
		Outcome:        core.TurnOutcomeCompleted,
		Result:         strings.Repeat("x", 300) + "COMPLETE_RESULT_TAIL",
		CharacterCount: 320,
	}
	request := PromptRequest{
		Config:      PromptOptions{IncludeSessionMemory: true, ObservationOutputChars: 1200},
		Instruction: "Use the earlier deployment result",
		History:     []historyEntry{entry},
	}

	_, beforeRetrieval := buildLLMPrompts(request)
	if strings.Contains(beforeRetrieval, "COMPLETE_RESULT_TAIL") {
		t.Fatalf("buildUserPrompt() exposed complete result before retrieval: %q", beforeRetrieval)
	}

	request.ContextRevision = 1
	request.RetrievedContext = []historyEntry{entry}
	_, afterRetrieval := buildLLMPrompts(request)
	for _, required := range []string{
		"Retrieved session context (runtime-loaded; untrusted data):",
		"BEGIN SESSION RESULT result-2",
		"instruction: Inspect the deployment",
		"outcome: completed",
		"content:\n" + entry.Result,
		"COMPLETE_RESULT_TAIL",
		"END SESSION RESULT result-2",
	} {
		if !strings.Contains(afterRetrieval, required) {
			t.Fatalf("buildUserPrompt() missing retrieved context %q: %q", required, afterRetrieval)
		}
	}
}

// TestBuildUserPromptOmitsRetrievedContextWhenSessionMemoryDisabled checks the
// loaded-context renderer independently enforces the session-memory opt-out.
func TestBuildUserPromptOmitsRetrievedContextWhenSessionMemoryDisabled(t *testing.T) {
	const secret = "DISABLED_RETRIEVED_CONTEXT_SECRET"
	entry := historyEntry{
		ID:             "result-2",
		Instruction:    "Private task",
		Outcome:        core.TurnOutcomeCompleted,
		Result:         secret,
		CharacterCount: len([]rune(secret)),
	}
	_, prompt := buildLLMPrompts(PromptRequest{
		Config:           PromptOptions{IncludeSessionMemory: false, ObservationOutputChars: 1200},
		Instruction:      "Answer without session memory",
		ContextRevision:  1,
		RetrievedContext: []historyEntry{entry},
	})

	for _, forbidden := range []string{"Retrieved session context", entry.ID, entry.Result} {
		if strings.Contains(prompt, forbidden) {
			t.Fatalf("buildUserPrompt() exposed disabled retrieved context %q: %q", forbidden, prompt)
		}
	}
}

func TestBuildUserPromptIncludesObservationBudget(t *testing.T) {
	_, prompt := buildLLMPrompts(PromptRequest{
		Config:      PromptOptions{ObservationOutputChars: 1200},
		Instruction: "Inspect the project",
	})

	if !strings.Contains(prompt, "\nCommand evidence budget: 1200 characters.\n") {
		t.Fatalf("buildUserPrompt() missing first-round observation budget: %q", prompt)
	}
	if !strings.Contains(prompt, "Authoritative user objective:\nInspect the project") {
		t.Fatalf("buildUserPrompt() does not identify the exact user objective as authoritative: %q", prompt)
	}
}

// TestBuildUserPromptKeepsCurrentObservationEligibleForCompletion checks
// retry-session eligibility cannot invalidate evidence gathered in this workflow.
func TestBuildUserPromptKeepsCurrentObservationEligibleForCompletion(t *testing.T) {
	_, prompt := buildLLMPrompts(PromptRequest{
		Config:      PromptOptions{ObservationOutputChars: 1200},
		Instruction: "List open ports",
		Operation:   "observe",
		Observations: []commandExecution{{
			Command:  "netstat -an",
			Purpose:  "Inspect open ports",
			ExitCode: 0,
			Stdout:   capturedStream{Text: "TCP 8080"},
		}},
	})

	for _, required := range []string{
		"Current workflow observations are eligible for completion when they resolve the objective.",
		"Prior session retry observation: not eligible for completion.",
	} {
		if !strings.Contains(prompt, required) {
			t.Fatalf("buildUserPrompt() missing current-observation completion guidance %q: %q", required, prompt)
		}
	}
	if strings.Contains(prompt, "Retry observation: not eligible for completion; refresh mutable state when needed.") {
		t.Fatalf("buildUserPrompt() still invalidates current workflow evidence: %q", prompt)
	}
}

func TestBuildSystemPromptGuidesCompactObservation(t *testing.T) {
	prompt := buildSystemPrompt()
	for _, required := range []string{
		"filter",
		"aggregate",
		"deduplicate",
		"read-only pipeline",
		"one compact replacement query after truncation",
		"first observation",
		"Expand only when observed evidence proves",
		`"success_criteria":"exact Authoritative user objective"`,
	} {
		if !strings.Contains(prompt, required) {
			t.Fatalf("buildSystemPrompt() missing compact-observation guidance %q: %q", required, prompt)
		}
	}
}

// TestBuildSystemPromptGuidesReadableSummaryLists checks summaries favor
// visual lists for multiple comparable items without requiring them always.
func TestBuildSystemPromptGuidesReadableSummaryLists(t *testing.T) {
	prompt := buildSystemPrompt()
	for _, required := range []string{
		"multiple comparable items",
		"one item per line",
		"do not force a list",
		"Markdown table only when the information is naturally tabular",
	} {
		if !strings.Contains(prompt, required) {
			t.Fatalf("buildSystemPrompt() missing summary-format guidance %q: %q", required, prompt)
		}
	}
}

func TestBuildSystemPromptKeepsRepeatAuthorityInRuntime(t *testing.T) {
	prompt := buildSystemPrompt()
	for _, required := range []string{
		"Use retry only after the exact command failed or timed out",
		"Never use retry to repeat a successful command",
		"repeat_reason describes intent and never authorizes execution",
		"Shellia admits verify_after_change only in an act workflow when runtime history proves",
		"Do not repeat a successful command with user_requested or poll_changed_state",
	} {
		if !strings.Contains(prompt, required) {
			t.Fatalf("buildSystemPrompt() missing retry constraint %q: %q", required, prompt)
		}
	}
}

// TestBuildSystemPromptDefinesAnswerAndContextRetrievalWorkflow checks the
// stable contract covers text transformations and the full retrieve-to-complete flow.
func TestBuildSystemPromptDefinesAnswerAndContextRetrievalWorkflow(t *testing.T) {
	prompt := buildSystemPrompt()
	for _, required := range []string{
		"explain, summarize, compare, translate, or reformat",
		"select the exact required IDs from the Session result catalog",
		"return action=retrieve_context with those IDs in context_refs",
		"return action=complete without repeating context_refs",
		"associates the exact loaded results automatically",
		"Never rediscover or rerun terminal commands as a substitute",
		"textual answer transformations remain answer",
	} {
		if !strings.Contains(prompt, required) {
			t.Fatalf("buildSystemPrompt() missing answer/retrieval contract %q: %q", required, prompt)
		}
	}
	for _, contradictory := range []string{
		"answer for a request that only asks how or why",
		"When an outcome is requested rather than an explanation, prefer action",
	} {
		if strings.Contains(prompt, contradictory) {
			t.Fatalf("buildSystemPrompt() retains contradictory answer definition %q: %q", contradictory, prompt)
		}
	}
}

func TestBuildSystemPromptAllowsNecessaryReadOnlyEvidencePipelines(t *testing.T) {
	prompt := buildSystemPrompt()
	const required = "Do not chain commands with ';', '&&', or '||'. A pipe is allowed only for a necessary read-only evidence-bounding pipeline, even when the user did not explicitly request a pipeline."
	if !strings.Contains(prompt, required) {
		t.Fatalf("buildSystemPrompt() missing evidence-bounding pipeline exception %q: %q", required, prompt)
	}
}

// representativePromptRequest exercises every prompt section with stable data.
func representativePromptRequest() PromptRequest {
	cfg := configpkg.DefaultConfig()
	cfg.PlanOnly = true
	cfg.MaxObservationEntries = 2
	cfg.ObservationOutputChars = 24

	previous := Response{Action: "execute", Operation: "observe", Summary: "Inspect the deployment state."}
	return PromptRequest{
		Config:              promptOptionsForTest(cfg),
		Instruction:         "complete the maintenance task",
		ResolvedInstruction: "complete the maintenance task for /srv/demo",
		ContextInfo:         contextInfo{CWD: "/srv/demo", User: "demo-user", OS: "demo-os", Shell: "/bin/zsh"},
		History:             []historyEntry{{ID: "result-1", Instruction: "inspect the old deployment", Outcome: core.TurnOutcomeCompleted, Result: "The old deployment needs maintenance.", CharacterCount: 37}},
		State: sessionState{
			PendingIntent:        "finish maintenance",
			LastRetryInstruction: "complete the maintenance task",
			PendingProposal:      pendingProposal{Objective: "restart demo", Summary: "Restart the demo service"},
			LastRuntimeHint:      "service is managed by launchd",
			LastCreatedFiles:     []string{"/srv/demo/report.txt"},
			LastReferencedFile:   "/srv/demo/config.toml",
			LastBlockerKind:      "missing_input",
			LastBlockerReason:    "service name was not provided",
			LastObservations:     []core.ObservationMemory{{Purpose: "Old observation", Command: "launchctl list", Transcript: "old service output"}},
		},
		Observations: []commandExecution{
			{Command: "launchctl print system/demo", Purpose: "Inspect the demo service", ExitCode: 1, Stderr: capturedStream{Text: "service-marker failed"}},
			{Command: "cat /srv/demo/config.toml", Purpose: "Read the demo configuration", ExitCode: 0, Stdout: capturedStream{Text: "config-marker enabled=true"}},
		},
		Skipped:                   []skippedCommand{{Command: "launchctl kickstart system/demo", Purpose: "Restart the demo service", Reason: "awaiting confirmation"}},
		LatestBatchExecutionStart: 0,
		LatestBatchSkippedStart:   0,
		PlanningRoundsRemaining:   2,
		Operation:                 "observe",
		SuccessCriteria:           "The current demo service state is reported.",
		DecisionError:             "current observation completion requires observed workflow evidence",
		RetryObservationAvailable: true,
		PreviousDecision:          &previous,
		Attempts: []workflowAttempt{{
			ID:               7,
			Round:            1,
			PlannedCommand:   "launchctl print system/demo",
			EffectiveCommand: "launchctl print system/demo",
			Outcome:          "failed",
			ExitCode:         1,
			EvidenceBefore:   3,
			EvidenceAfter:    4,
		}},
	}
}

// TestParseResponseAcceptsSimplifiedDecisionContract checks each model-owned
// action remains expressible without runtime evidence metadata.
func TestParseResponseAcceptsSimplifiedDecisionContract(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{
			name: "answer from model knowledge",
			raw:  `{"action":"complete","operation":"answer","success_criteria":"Explain the concept","summary":"Explanation.","offer":{"objective":"","summary":""},"blocker_kind":"","blocker_reason":"","context_refs":[],"commands":[]}`,
		},
		{
			name: "retrieve session result",
			raw:  `{"action":"retrieve_context","operation":"answer","success_criteria":"Reformat the earlier result","summary":"Retrieve the selected result.","offer":{"objective":"","summary":""},"blocker_kind":"","blocker_reason":"","context_refs":["result-2"],"commands":[]}`,
		},
		{
			name: "current observation",
			raw:  `{"action":"complete","operation":"observe","success_criteria":"Current ports listed","summary":"Ports listed.","offer":{"objective":"","summary":""},"blocker_kind":"","blocker_reason":"","context_refs":[],"commands":[]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := parseResponse(tt.raw, ResponseModeStrict); err != nil {
				t.Fatalf("parseResponse() error = %v", err)
			}
		})
	}
}

// TestParseResponseAcceptsTypedConversationalPlan checks a plan is a typed,
// non-authorizing provider decision with a later execute offer.
func TestParseResponseAcceptsTypedConversationalPlan(t *testing.T) {
	raw := `{"action":"plan","operation":"act","success_criteria":"Marker can be created","summary":"Create the marker after approval.","context_refs":[],"offer":{"mode":"execute","objective":"create marker","summary":"Create the marker"},"blocker_kind":"","blocker_reason":"","commands":[{"command":"touch marker","purpose":"Create marker","risk":"medium","requires_confirmation":true}]}`

	response, err := parseResponse(raw, ResponseModeStrict)
	if err != nil {
		t.Fatalf("parseResponse() error = %v", err)
	}
	if response.Action != "plan" || response.Offer.Mode != "execute" || len(response.Commands) != 1 {
		t.Fatalf("response = %#v, want typed plan with execute offer", response)
	}
}

// TestParseResponseRejectsInvalidOfferMatrix checks offers cannot silently
// cross from explanation or planning into the wrong authority mode.
func TestParseResponseRejectsInvalidOfferMatrix(t *testing.T) {
	tests := []string{
		`{"action":"complete","operation":"answer","success_criteria":"Explain","summary":"Explanation.","context_refs":[],"offer":{"mode":"execute","objective":"run checks","summary":"Run checks"},"blocker_kind":"","blocker_reason":"","commands":[]}`,
		`{"action":"complete","operation":"capability","success_criteria":"Explain capability","summary":"Possible.","context_refs":[],"offer":{"mode":"plan","objective":"run checks","summary":"Run checks"},"blocker_kind":"","blocker_reason":"","commands":[]}`,
		`{"action":"plan","operation":"act","success_criteria":"Change planned","summary":"Plan it.","context_refs":[],"offer":{"mode":"plan","objective":"change it","summary":"Change it"},"blocker_kind":"","blocker_reason":"","commands":[{"command":"touch marker","purpose":"Create marker","risk":"medium","requires_confirmation":true}]}`,
		`{"action":"plan","operation":"act","success_criteria":"Change planned","summary":"Plan it.","context_refs":[],"offer":{"mode":"","objective":"","summary":""},"blocker_kind":"","blocker_reason":"","commands":[{"command":"touch marker","purpose":"Create marker","risk":"medium","requires_confirmation":true}]}`,
		`{"action":"complete","operation":"observe","success_criteria":"Observation complete","summary":"Done.","context_refs":[],"offer":{"mode":"","objective":"hidden objective","summary":"Hidden offer"},"blocker_kind":"","blocker_reason":"","commands":[]}`,
	}

	for _, raw := range tests {
		if _, err := parseResponse(raw, ResponseModeStrict); err == nil {
			t.Fatalf("parseResponse(%s) error = nil, want invalid offer rejection", raw)
		}
	}
}

// TestParseResponseAcceptsRuntimeOwnedCompletionEvidence checks a complete
// decision needs only the model-owned operation, criterion, and answer.
func TestParseResponseAcceptsRuntimeOwnedCompletionEvidence(t *testing.T) {
	raw := `{"action":"complete","operation":"observe","success_criteria":"Current ports listed","summary":"Ports listed.","context_refs":[],"offer":{"objective":"","summary":""},"blocker_kind":"","blocker_reason":"","commands":[]}`

	response, err := parseResponse(raw, ResponseModeStrict)
	if err != nil {
		t.Fatalf("parseResponse() error = %v", err)
	}
	if response.Action != "complete" || response.Operation != "observe" || response.Summary != "Ports listed." {
		t.Fatalf("response = %#v, want minimal complete decision", response)
	}
}

// TestParseResponseRejectsInvalidDecisionContract checks structural
// wire-contract violations stop before orchestration.
func TestParseResponseRejectsInvalidDecisionContract(t *testing.T) {
	valid := `{"action":"complete","operation":"answer","success_criteria":"Explain the concept","summary":"Explanation.","offer":{"objective":"","summary":""},"blocker_kind":"","blocker_reason":"","context_refs":[],"commands":[]}`
	tests := []struct {
		name string
		raw  string
	}{
		{name: "unknown operation", raw: strings.Replace(valid, `"operation":"answer"`, `"operation":"unknown"`, 1)},
		{name: "retrieve context without references", raw: strings.Replace(valid, `"action":"complete"`, `"action":"retrieve_context"`, 1)},
		{name: "retrieve context is answer only", raw: strings.Replace(strings.Replace(strings.Replace(valid, `"action":"complete"`, `"action":"retrieve_context"`, 1), `"operation":"answer"`, `"operation":"observe"`, 1), `"context_refs":[]`, `"context_refs":["result-1"]`, 1)},
		{name: "retrieve context with commands", raw: strings.Replace(strings.Replace(strings.Replace(valid, `"action":"complete"`, `"action":"retrieve_context"`, 1), `"context_refs":[]`, `"context_refs":["result-1"]`, 1), `"commands":[]`, `"commands":[{"command":"pwd","purpose":"Inspect","risk":"safe"}]`, 1)},
		{name: "complete cannot repeat context references", raw: strings.Replace(valid, `"context_refs":[]`, `"context_refs":["result-1"]`, 1)},
		{name: "answer cannot execute", raw: strings.Replace(strings.Replace(valid, `"action":"complete"`, `"action":"execute"`, 1), `"commands":[]`, `"commands":[{"command":"pwd","purpose":"Inspect","risk":"safe"}]`, 1)},
		{name: "capability cannot execute", raw: strings.Replace(strings.Replace(strings.Replace(valid, `"action":"complete"`, `"action":"execute"`, 1), `"operation":"answer"`, `"operation":"capability"`, 1), `"commands":[]`, `"commands":[{"command":"pwd","purpose":"Inspect","risk":"safe"}]`, 1)},
		{name: "complete with commands", raw: strings.Replace(valid, `"commands":[]`, `"commands":[{"command":"pwd","purpose":"Inspect","risk":"safe"}]`, 1)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := parseResponse(tt.raw, ResponseModeStrict); err == nil {
				t.Fatal("parseResponse() error = nil, want structural contract rejection")
			}
		})
	}
}

// TestParseResponseNormalizesContextReferences checks retrieval references are
// normalized before the runtime resolves them.
func TestParseResponseNormalizesContextReferences(t *testing.T) {
	raw := `{"action":"retrieve_context","operation":"answer","success_criteria":"Reformat result","summary":"Retrieve it.","context_refs":[" Result-2 "],"offer":{"objective":"","summary":""},"blocker_kind":"","blocker_reason":"","commands":[]}`
	response, err := parseResponse(raw, ResponseModeStrict)
	if err != nil {
		t.Fatalf("parseResponse() error = %v", err)
	}
	if got := response.ContextRefs; len(got) != 1 || got[0] != "result-2" {
		t.Fatalf("ContextRefs = %#v, want []string{\"result-2\"}", got)
	}
}

// TestParseResponseRejectsInvalidContextReferences checks retrieval requests
// cannot contain empty or duplicate references.
func TestParseResponseRejectsInvalidContextReferences(t *testing.T) {
	valid := `{"action":"retrieve_context","operation":"answer","success_criteria":"Reformat result","summary":"Retrieve it.","context_refs":["result-2"],"offer":{"objective":"","summary":""},"blocker_kind":"","blocker_reason":"","commands":[]}`
	for _, raw := range []string{
		strings.Replace(valid, `"result-2"`, `" "`, 1),
		strings.Replace(valid, `"context_refs":["result-2"]`, `"context_refs":["result-2"," RESULT-2 "]`, 1),
	} {
		if _, err := parseResponse(raw, ResponseModeStrict); err == nil {
			t.Fatal("parseResponse() error = nil, want invalid context reference rejection")
		}
	}
}

// TestParseResponseAcceptsExtraTrailingBrace checks local models that append
// one stray brace still yield the first complete JSON object.
func TestParseResponseAcceptsExtraTrailingBrace(t *testing.T) {
	raw := `{"action":"execute","operation":"observe","success_criteria":"Files listed","summary":"Showing files.","commands":[{"command":"ls","purpose":"List files","risk":"safe","requires_confirmation":false}]}}`

	response, err := parseResponse(raw, ResponseModeCompatible)
	if err != nil {
		t.Fatalf("parseResponse() error = %v", err)
	}
	if len(response.Commands) != 1 || response.Commands[0].Command != "ls" {
		t.Fatalf("Commands = %#v, want ls command", response.Commands)
	}
}

// TestParseResponseKeepsBracesInsideStrings checks JSON extraction respects
// quoted brace characters.
func TestParseResponseKeepsBracesInsideStrings(t *testing.T) {
	raw := `prefix {"action":"complete","operation":"answer","success_criteria":"Braces explained","summary":"Use {literal} braces.","commands":[]} suffix`

	response, err := parseResponse(raw, ResponseModeCompatible)
	if err != nil {
		t.Fatalf("parseResponse() error = %v", err)
	}
	if response.Summary != "Use {literal} braces." {
		t.Fatalf("Summary = %q, want braces preserved", response.Summary)
	}
}

// TestParseResponseStrictRejectsDocumentBoundaries checks response-format
// providers cannot smuggle text or multiple documents past the decoder.
func TestParseResponseStrictRejectsDocumentBoundaries(t *testing.T) {
	valid := `{"action":"complete","operation":"answer","success_criteria":"Answer provided","summary":"Done.","commands":[]}`
	tests := []struct {
		name string
		raw  string
	}{
		{name: "prefix", raw: "prefix " + valid},
		{name: "suffix", raw: valid + " suffix"},
		{name: "extra brace", raw: valid + "}"},
		{name: "two objects", raw: valid + valid},
		{name: "non object", raw: `[]`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := parseResponse(tt.raw, ResponseModeStrict); err == nil {
				t.Fatal("parseResponse() error = nil, want strict document rejection")
			}
		})
	}
}

// TestParseResponseStrictAcceptsUnknownFields preserves provider compatibility
// while strict mode enforces exactly one response document.
func TestParseResponseStrictAcceptsUnknownFields(t *testing.T) {
	raw := `{"action":"complete","operation":"answer","success_criteria":"Answer provided","summary":"Done.","commands":[],"provider_metadata":{"request_id":"provider-123"}}`

	response, err := parseResponse(raw, ResponseModeStrict)
	if err != nil {
		t.Fatalf("parseResponse() error = %v", err)
	}
	if response.Summary != "Done." {
		t.Fatalf("Summary = %q, want Done.", response.Summary)
	}
}

// TestNormalizePlanPreservesFailureIndependence checks explicit batch
// independence survives local plan normalization.
func TestNormalizePlanPreservesFailureIndependence(t *testing.T) {
	_, plans, err := normalizePlan(Response{
		Action:  "execute",
		Summary: "Continue independent inspection.",
		Commands: []Command{{
			Command:              "pwd",
			Purpose:              "Print directory",
			Risk:                 "safe",
			IndependentOnFailure: true,
		}},
	})
	if err != nil {
		t.Fatalf("normalizePlan() error = %v", err)
	}
	if len(plans) != 1 || !plans[0].IndependentOnFailure {
		t.Fatalf("plans = %#v, want independent command", plans)
	}
}

// TestNormalizePlanPreservesRepeatReason checks repetition admission metadata reaches the controller unchanged.
func TestNormalizePlanPreservesRepeatReason(t *testing.T) {
	_, plans, err := normalizePlan(Response{
		Action:  "execute",
		Summary: "Verify again.",
		Commands: []Command{{
			Command:      "df -h",
			Purpose:      "Verify changed disk space",
			Risk:         "safe",
			RepeatReason: "verify_after_change",
		}},
	})
	if err != nil {
		t.Fatalf("normalizePlan() error = %v", err)
	}
	if len(plans) != 1 || plans[0].RepeatReason != repeatReasonVerifyAfterChange {
		t.Fatalf("plans = %#v, want verify_after_change", plans)
	}
}

// TestBuildSystemPromptDefinesWorkflowDecisionContract checks the planner gets
// one outcome contract and no pre-execution continuation hints.
func TestBuildSystemPromptDefinesWorkflowDecisionContract(t *testing.T) {
	prompt := buildSystemPrompt()
	for _, required := range []string{
		"action=complete",
		"action=execute",
		"action=blocked",
		"blocker_kind",
		"blocker_reason",
		"local command policy is final",
		"untrusted evidence",
		"independent_on_failure",
		"repeat_reason",
		"operation",
		"success_criteria",
		"capability",
		"explicit capability question takes precedence",
		"do not ask conversational permission",
		"When a requested outcome requires observing mutable state or changing the system",
		"Shellia associates runtime-owned evidence automatically",
	} {
		if !strings.Contains(prompt, required) {
			t.Fatalf("buildSystemPrompt() missing %q", required)
		}
	}
	for _, removed := range []string{"requires_" + "observation", "observation_" + "reason", "requires_" + "input", "input_" + "reason", "completion_basis", "evidence_source", "freshness", "evidence_revision"} {
		if strings.Contains(prompt, removed) {
			t.Fatalf("buildSystemPrompt() still contains obsolete field %q", removed)
		}
	}
}

// TestBuildSystemPromptRequiresTargetDisambiguation checks the planner cannot
// select one plausible target without evidence that distinguishes it.
func TestBuildSystemPromptRequiresTargetDisambiguation(t *testing.T) {
	prompt := buildSystemPrompt()
	for _, required := range []string{
		"multiple plausible targets",
		"minimal read-only command",
		"ordering, version, recency, or preference",
		"blocker_kind=missing_input",
	} {
		if !strings.Contains(prompt, required) {
			t.Fatalf("buildSystemPrompt() missing target disambiguation rule %q", required)
		}
	}
}

// TestBuildUserPromptDeclaresImmutableExecutionAuthority checks plan-only and
// normal turns use the same prompt contract with different local authority.
func TestBuildUserPromptDeclaresImmutableExecutionAuthority(t *testing.T) {
	tests := []struct {
		name     string
		planOnly bool
		want     string
	}{
		{name: "normal", want: "Execution authority: allowed"},
		{name: "plan only", planOnly: true, want: "Execution authority: plan_only"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := configpkg.DefaultConfig()
			cfg.PlanOnly = tt.planOnly
			prompt := buildUserPrompt(PromptRequest{
				Config:                  promptOptionsForTest(cfg),
				Instruction:             "inspect disk",
				ContextInfo:             contextInfo{CWD: "/tmp"},
				PlanningRoundsRemaining: 2,
			})
			if !strings.Contains(prompt, tt.want) {
				t.Fatalf("buildUserPrompt() missing %q in %q", tt.want, prompt)
			}
			if !strings.Contains(prompt, "Planning rounds remaining: 2") {
				t.Fatalf("buildUserPrompt() missing planning budget in %q", prompt)
			}
		})
	}
}

func TestBuildUserPromptIncludesPendingStructuredProposal(t *testing.T) {
	cfg := configpkg.DefaultConfig()
	prompt := buildUserPrompt(PromptRequest{
		Config:      promptOptionsForTest(cfg),
		Instruction: "potser",
		ContextInfo: contextInfo{CWD: "/tmp"},
		State: sessionState{PendingProposal: pendingProposal{
			Objective: "consulta l'espai disponible al disc",
			Summary:   "Consultar disc",
		}},
	})

	for _, required := range []string{"pending_proposal_objective: consulta l'espai disponible al disc", "pending_proposal_summary: Consultar disc"} {
		if !strings.Contains(prompt, required) {
			t.Fatalf("buildUserPrompt() missing %q in %q", required, prompt)
		}
	}
}

// TestBuildUserPromptOmitsDisabledSessionMemory checks local memory is not
// leaked when its configuration switch is off.
func TestBuildUserPromptOmitsDisabledSessionMemory(t *testing.T) {
	cfg := configpkg.DefaultConfig()
	cfg.IncludeSessionMemory = false
	prompt := buildUserPrompt(PromptRequest{
		Config:      promptOptionsForTest(cfg),
		Instruction: "new task",
		ContextInfo: contextInfo{CWD: "/tmp"},
		History:     []historyEntry{{Instruction: "old task", Result: "old result"}},
		State: sessionState{
			PendingIntent: "old pending intent",
			PendingProposal: pendingProposal{
				Objective: "old-command",
			},
		},
	})

	for _, hidden := range []string{"old task", "old result", "old pending intent", "old-command"} {
		if strings.Contains(prompt, hidden) {
			t.Fatalf("buildUserPrompt() leaked disabled memory %q", hidden)
		}
	}
}

// TestBuildUserPromptOmitsDisabledObservations checks command output respects
// the existing recent-observation configuration boundary.
func TestBuildUserPromptOmitsDisabledObservations(t *testing.T) {
	cfg := configpkg.DefaultConfig()
	cfg.IncludeRecentObservations = false
	prompt := buildUserPrompt(PromptRequest{
		Config:      promptOptionsForTest(cfg),
		Instruction: "inspect",
		ContextInfo: contextInfo{CWD: "/tmp"},
		Observations: []commandExecution{{
			Command:  "printf marker",
			Purpose:  "Inspect marker",
			Stdout:   capturedStream{Text: "secret-marker"},
			ExitCode: 0,
		}},
	})

	if strings.Contains(prompt, "secret-marker") {
		t.Fatalf("buildUserPrompt() leaked disabled observations: %q", prompt)
	}
	for _, required := range []string{"Command: printf marker", "Exit code: 0", "Output: [omitted by configuration]"} {
		if !strings.Contains(prompt, required) {
			t.Fatalf("buildUserPrompt() missing redacted current evidence %q: %q", required, prompt)
		}
	}
}

// TestBuildUserPromptBoundsEvidenceButKeepsLatestAndRecentFailure checks current evidence is a projection, not a transcript.
func TestBuildUserPromptBoundsEvidenceButKeepsLatestAndRecentFailure(t *testing.T) {
	cfg := configpkg.DefaultConfig()
	cfg.MaxObservationEntries = 2
	cfg.ObservationOutputChars = 40
	prompt := buildUserPrompt(PromptRequest{
		Config:                    promptOptionsForTest(cfg),
		Instruction:               "finish inspection",
		ContextInfo:               contextInfo{CWD: "/tmp"},
		LatestBatchExecutionStart: 4,
		Observations: []commandExecution{
			{Command: "echo old-success-1", Purpose: "Old success one", Stdout: capturedStream{Text: "old-success-1"}, ExitCode: 0},
			{Command: "false", Purpose: "Recent failure", Stderr: capturedStream{Text: "failure-marker"}, ExitCode: 1},
			{Command: "echo old-success-2", Purpose: "Old success two", Stdout: capturedStream{Text: "old-success-2"}, ExitCode: 0},
			{Command: "echo old-success-3", Purpose: "Old success three", Stdout: capturedStream{Text: "old-success-3"}, ExitCode: 0},
			{Command: "echo latest", Purpose: "Latest batch", Stdout: capturedStream{Text: "latest-marker"}, ExitCode: 0},
		},
	})

	for _, required := range []string{"failure-marker", "latest-marker", "older evidence omitted: 3 execution(s)"} {
		if !strings.Contains(prompt, required) {
			t.Fatalf("prompt missing %q: %q", required, prompt)
		}
	}
	for _, omitted := range []string{"old-success-1", "old-success-2", "old-success-3"} {
		if strings.Contains(prompt, omitted) {
			t.Fatalf("prompt retained omitted evidence %q: %q", omitted, prompt)
		}
	}
}

func TestBuildUserPromptKeepsRuntimeEvidenceMetadataOutOfRepair(t *testing.T) {
	prompt := buildUserPrompt(PromptRequest{
		Config:        promptOptionsForTest(configpkg.DefaultConfig()),
		Instruction:   "actualitza codex",
		ContextInfo:   contextInfo{CWD: "/tmp"},
		DecisionError: "current observation completion requires observed workflow evidence",
		Attempts: []workflowAttempt{
			{ID: 1, Outcome: "success", EvidenceBefore: 0, EvidenceAfter: 1},
			{ID: 2, Outcome: "success", EvidenceBefore: 1, EvidenceAfter: 2},
			{ID: 3, Outcome: "success", EvidenceBefore: 1, EvidenceAfter: 2},
			{ID: 4, Outcome: "skipped", EvidenceBefore: 2, EvidenceAfter: 2},
		},
	})

	for _, required := range []string{"Recent workflow attempts:", "current observation completion requires observed workflow evidence"} {
		if !strings.Contains(prompt, required) {
			t.Fatalf("repair prompt missing %q: %q", required, prompt)
		}
	}
	for _, removed := range []string{"Valid completion references:", "evidence_revision", "attempt_ids"} {
		if strings.Contains(prompt, removed) {
			t.Fatalf("repair prompt exposed runtime evidence metadata %q: %q", removed, prompt)
		}
	}
}

// TestBuildUserPromptKeepsEntireLatestSkippedBatch checks the entry limit never slices the current decision batch.
func TestBuildUserPromptKeepsEntireLatestSkippedBatch(t *testing.T) {
	cfg := configpkg.DefaultConfig()
	cfg.MaxObservationEntries = 1
	prompt := buildUserPrompt(PromptRequest{
		Config:                  promptOptionsForTest(cfg),
		Instruction:             "finish inspection",
		ContextInfo:             contextInfo{CWD: "/tmp"},
		LatestBatchSkippedStart: 2,
		Skipped: []skippedCommand{
			{Command: "old-one", Purpose: "Old one", Reason: "old"},
			{Command: "old-two", Purpose: "Old two", Reason: "old"},
			{Command: "latest-one", Purpose: "Latest one", Reason: "current"},
			{Command: "latest-two", Purpose: "Latest two", Reason: "current"},
		},
	})

	for _, required := range []string{"latest-one", "latest-two", "older evidence omitted: 0 execution(s), 2 skipped command(s)"} {
		if !strings.Contains(prompt, required) {
			t.Fatalf("prompt missing %q: %q", required, prompt)
		}
	}
	for _, omitted := range []string{"old-one", "old-two"} {
		if strings.Contains(prompt, omitted) {
			t.Fatalf("prompt retained omitted skipped evidence %q: %q", omitted, prompt)
		}
	}
}

// TestBuildUserPromptAppliesOneOutputBudget checks multiple observations share the configured evidence budget.
func TestBuildUserPromptAppliesOneOutputBudget(t *testing.T) {
	cfg := configpkg.DefaultConfig()
	cfg.ObservationOutputChars = 12
	prompt := buildUserPrompt(PromptRequest{
		Config:                    promptOptionsForTest(cfg),
		Instruction:               "inspect",
		ContextInfo:               contextInfo{CWD: "/tmp"},
		LatestBatchExecutionStart: 0,
		Observations: []commandExecution{
			{Command: "first", Purpose: "First", Stdout: capturedStream{Text: "AAAAAA-hidden-first-tail"}},
			{Command: "second", Purpose: "Second", Stdout: capturedStream{Text: "BBBBBB-hidden-second-tail"}},
		},
	})

	if !strings.Contains(prompt, "output evidence budget: 12 chars") {
		t.Fatalf("prompt missing global budget marker: %q", prompt)
	}
	for _, hidden := range []string{"hidden-first-tail", "hidden-second-tail"} {
		if strings.Contains(prompt, hidden) {
			t.Fatalf("prompt exceeded shared output budget with %q: %q", hidden, prompt)
		}
	}
}

// TestBuildUserPromptPrioritizesLatestBatchOutput checks obsolete verbose
// evidence cannot truncate a compact result that resolves the current round.
func TestBuildUserPromptPrioritizesLatestBatchOutput(t *testing.T) {
	cfg := configpkg.DefaultConfig()
	cfg.ObservationOutputChars = 24
	prompt := buildUserPrompt(PromptRequest{
		Config:                    promptOptionsForTest(cfg),
		Instruction:               "list current ports",
		ContextInfo:               contextInfo{CWD: "/tmp"},
		LatestBatchExecutionStart: 1,
		Observations: []commandExecution{
			{Command: "broad-query", Purpose: "Old broad output", Stdout: capturedStream{Text: strings.Repeat("older-output-", 20)}},
			{Command: "compact-query", Purpose: "Latest compact output", Stdout: capturedStream{Text: "LATEST-COMPLETE-VALUE"}},
		},
	})

	if !strings.Contains(prompt, "LATEST-COMPLETE-VALUE") {
		t.Fatalf("latest compact evidence was truncated by obsolete output: %q", prompt)
	}
}

// TestBuildUserPromptRedistributesLatestBatchBudget checks a short latest
// output cannot send unused budget to old evidence while a sibling is truncated.
func TestBuildUserPromptRedistributesLatestBatchBudget(t *testing.T) {
	cfg := configpkg.DefaultConfig()
	cfg.ObservationOutputChars = 12
	prompt := buildUserPrompt(PromptRequest{
		Config:                    promptOptionsForTest(cfg),
		Instruction:               "inspect",
		ContextInfo:               contextInfo{CWD: "/tmp"},
		LatestBatchExecutionStart: 1,
		Observations: []commandExecution{
			{Command: "old", Purpose: "Old", Stdout: capturedStream{Text: strings.Repeat("old", 20)}},
			{Command: "latest-long", Purpose: "Latest long", Stdout: capturedStream{Text: "ABCDEFGHIJ"}},
			{Command: "latest-short", Purpose: "Latest short", Stdout: capturedStream{Text: "K"}},
		},
	})

	if !strings.Contains(prompt, "ABCDEFGHIJ") || !strings.Contains(prompt, "stdout:\n   K") {
		t.Fatalf("latest batch did not receive its full budget before old evidence: %q", prompt)
	}
}

// TestBuildUserPromptProjectsDecisionAndBoundedAttempts checks causal workflow state survives across rounds without an unbounded ledger.
func TestBuildUserPromptProjectsDecisionAndBoundedAttempts(t *testing.T) {
	cfg := configpkg.DefaultConfig()
	cfg.MaxObservationEntries = 2
	previous := Response{Action: "execute", Summary: "Inspect and verify."}
	prompt := buildUserPrompt(PromptRequest{
		Config:           promptOptionsForTest(cfg),
		Instruction:      "finish inspection",
		ContextInfo:      contextInfo{CWD: "/tmp"},
		PreviousDecision: &previous,
		Attempts: []workflowAttempt{
			{ID: 1, Round: 0, PlannedCommand: "old-command", EffectiveCommand: "old-command", Outcome: "success"},
			{ID: 2, Round: 1, PlannedCommand: "planned-two", EffectiveCommand: "effective-two", Outcome: "failed", ExitCode: 1},
			{ID: 3, Round: 2, PlannedCommand: "verify", EffectiveCommand: "verify", Outcome: "success", RepeatReason: repeatReasonVerifyAfterChange, RelatedAttemptID: 1},
		},
	})

	for _, required := range []string{"Previous workflow decision:", "action: execute", "Inspect and verify.", "attempt 2", "planned-two", "effective-two", "attempt 3", "verify_after_change", "related_attempt: 1", "older attempts omitted: 1"} {
		if !strings.Contains(prompt, required) {
			t.Fatalf("buildUserPrompt() missing %q: %q", required, prompt)
		}
	}
	if strings.Contains(prompt, "old-command") {
		t.Fatalf("buildUserPrompt() retained older bounded attempt: %q", prompt)
	}
}

// TestTrimForSummaryHandlesNonPositiveLimit checks defensive callers cannot
// panic while trimming output.
func TestTrimForSummaryHandlesNonPositiveLimit(t *testing.T) {
	if got := trimForSummary("abcdef", 0, truncationStart); got != "" {
		t.Fatalf("trimForSummary(limit 0) = %q, want empty", got)
	}
	if got := trimForSummary("abcdef", -1, truncationStart); got != "" {
		t.Fatalf("trimForSummary(limit -1) = %q, want empty", got)
	}
}

// TestCallPlanningPromptAppliesRequestTimeout checks every workflow decision has the configured request deadline.
func TestCallPlanningPromptAppliesRequestTimeout(t *testing.T) {
	cfg := configpkg.DefaultConfig()
	cfg.BaseURL = "https://shellia.test"
	cfg.RequestTimeout = 10 * time.Millisecond
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		<-request.Context().Done()
		return nil, request.Context().Err()
	})}

	_, err := callPlanningPrompt(context.Background(), client, clientOptionsForTest(cfg), "system", "user")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("callPlanningPrompt() error = %v, want request deadline", err)
	}
}

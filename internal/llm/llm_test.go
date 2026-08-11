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

// representativePromptRequest exercises every prompt section with stable data.
func representativePromptRequest() PromptRequest {
	cfg := configpkg.DefaultConfig()
	cfg.PlanOnly = true
	cfg.MaxObservationEntries = 2
	cfg.ObservationOutputChars = 24

	previous := Response{Action: "execute", Operation: "observe", EvidenceSource: "current_observation", Freshness: "current", Summary: "Inspect the deployment state."}
	return PromptRequest{
		Config:              promptOptionsForTest(cfg),
		Instruction:         "complete the maintenance task",
		ResolvedInstruction: "complete the maintenance task for /srv/demo",
		ContextInfo:         contextInfo{CWD: "/srv/demo", User: "demo-user", OS: "demo-os", Shell: "/bin/zsh"},
		History:             []historyEntry{{Instruction: "inspect the old deployment", Result: "The old deployment needs maintenance."}},
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
		EvidenceRevision:          4,
		PlanningRoundsRemaining:   2,
		Operation:                 "observe",
		EvidenceSource:            "current_observation",
		Freshness:                 "current",
		SuccessCriteria:           "The current demo service state is reported.",
		DecisionError:             "completion basis references an unavailable attempt",
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

// TestParseResponseAcceptsOrthogonalDecisionContract checks the wire contract
// accepts independently selected operation, evidence, and freshness values.
func TestParseResponseAcceptsOrthogonalDecisionContract(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{
			name: "answer from model knowledge",
			raw:  `{"action":"complete","operation":"answer","evidence_source":"model_knowledge","freshness":"not_applicable","success_criteria":"Explain the concept","summary":"Explanation.","completion_basis":{"source":"model_knowledge","freshness":"not_applicable"},"offer":{"objective":"","summary":""},"blocker_kind":"","blocker_reason":"","context_refs":[],"commands":[]}`,
		},
		{
			name: "retrieve session result",
			raw:  `{"action":"retrieve_context","operation":"answer","evidence_source":"session_result","freshness":"snapshot","success_criteria":"Reformat the earlier result","summary":"Retrieve the selected result.","completion_basis":{"source":"","freshness":""},"offer":{"objective":"","summary":""},"blocker_kind":"","blocker_reason":"","context_refs":["result-2"],"commands":[]}`,
		},
		{
			name: "current observation",
			raw:  `{"action":"complete","operation":"observe","evidence_source":"current_observation","freshness":"current","success_criteria":"Current ports listed","summary":"Ports listed.","completion_basis":{"source":"current_observation","freshness":"current","evidence_revision":1,"attempt_ids":[1]},"offer":{"objective":"","summary":""},"blocker_kind":"","blocker_reason":"","context_refs":[],"commands":[]}`,
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

// TestParseResponseRejectsInvalidOrthogonalDecisionContract checks structural
// wire-contract violations stop before orchestration.
func TestParseResponseRejectsInvalidOrthogonalDecisionContract(t *testing.T) {
	valid := `{"action":"complete","operation":"answer","evidence_source":"model_knowledge","freshness":"not_applicable","success_criteria":"Explain the concept","summary":"Explanation.","completion_basis":{"source":"model_knowledge","freshness":"not_applicable"},"offer":{"objective":"","summary":""},"blocker_kind":"","blocker_reason":"","context_refs":[],"commands":[]}`
	tests := []struct {
		name string
		raw  string
	}{
		{name: "unknown operation", raw: strings.Replace(valid, `"operation":"answer"`, `"operation":"unknown"`, 1)},
		{name: "unknown evidence source", raw: strings.Replace(valid, `"evidence_source":"model_knowledge"`, `"evidence_source":"unknown"`, 1)},
		{name: "unknown freshness", raw: strings.Replace(valid, `"freshness":"not_applicable"`, `"freshness":"unknown"`, 1)},
		{name: "retrieve context without references", raw: strings.Replace(valid, `"action":"complete"`, `"action":"retrieve_context"`, 1)},
		{name: "retrieve context with commands", raw: strings.Replace(strings.Replace(strings.Replace(valid, `"action":"complete"`, `"action":"retrieve_context"`, 1), `"context_refs":[]`, `"context_refs":["result-1"]`, 1), `"commands":[]`, `"commands":[{"command":"pwd","purpose":"Inspect","risk":"safe"}]`, 1)},
		{name: "answer cannot execute", raw: strings.Replace(strings.Replace(valid, `"action":"complete"`, `"action":"execute"`, 1), `"commands":[]`, `"commands":[{"command":"pwd","purpose":"Inspect","risk":"safe"}]`, 1)},
		{name: "capability cannot execute", raw: strings.Replace(strings.Replace(strings.Replace(valid, `"action":"complete"`, `"action":"execute"`, 1), `"operation":"answer"`, `"operation":"capability"`, 1), `"commands":[]`, `"commands":[{"command":"pwd","purpose":"Inspect","risk":"safe"}]`, 1)},
		{name: "complete with commands", raw: strings.Replace(valid, `"commands":[]`, `"commands":[{"command":"pwd","purpose":"Inspect","risk":"safe"}]`, 1)},
		{name: "completion basis differs", raw: strings.Replace(valid, `"freshness":"not_applicable"}`, `"freshness":"current"}`, 1)},
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
	raw := `{"action":"retrieve_context","operation":"answer","evidence_source":"session_result","freshness":"snapshot","success_criteria":"Reformat result","summary":"Retrieve it.","completion_basis":{"source":"","freshness":""},"context_refs":[" Result-2 "],"offer":{"objective":"","summary":""},"blocker_kind":"","blocker_reason":"","commands":[]}`
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
	valid := `{"action":"retrieve_context","operation":"answer","evidence_source":"session_result","freshness":"snapshot","success_criteria":"Reformat result","summary":"Retrieve it.","completion_basis":{"source":"","freshness":""},"context_refs":["result-2"],"offer":{"objective":"","summary":""},"blocker_kind":"","blocker_reason":"","commands":[]}`
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
	raw := `{"action":"execute","operation":"observe","evidence_source":"current_observation","freshness":"current","success_criteria":"Files listed","summary":"Showing files.","commands":[{"command":"ls","purpose":"List files","risk":"safe","requires_confirmation":false}]}}`

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
	raw := `prefix {"action":"complete","operation":"answer","evidence_source":"model_knowledge","freshness":"not_applicable","success_criteria":"Braces explained","summary":"Use {literal} braces.","completion_basis":{"source":"model_knowledge","freshness":"not_applicable"},"commands":[]} suffix`

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
	valid := `{"action":"complete","operation":"answer","evidence_source":"model_knowledge","freshness":"not_applicable","success_criteria":"Answer provided","summary":"Done.","completion_basis":{"source":"model_knowledge","freshness":"not_applicable"},"commands":[]}`
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
	raw := `{"action":"complete","operation":"answer","evidence_source":"model_knowledge","freshness":"not_applicable","success_criteria":"Answer provided","summary":"Done.","completion_basis":{"source":"model_knowledge","freshness":"not_applicable"},"commands":[],"provider_metadata":{"request_id":"provider-123"}}`

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
		"completion_basis",
		"blocker_kind",
		"blocker_reason",
		"local command policy is final",
		"untrusted evidence",
		"independent_on_failure",
		"repeat_reason",
		"operation",
		"evidence_source",
		"freshness",
		"success_criteria",
		"capability",
		"explicit capability question takes precedence",
		"do not ask conversational permission",
		"prefer action when an outcome is requested",
		"retry_observation only when the prompt marks it eligible",
	} {
		if !strings.Contains(prompt, required) {
			t.Fatalf("buildSystemPrompt() missing %q", required)
		}
	}
	for _, removed := range []string{"requires_" + "observation", "observation_" + "reason", "requires_" + "input", "input_" + "reason"} {
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
		Config:           promptOptionsForTest(cfg),
		Instruction:      "inspect",
		ContextInfo:      contextInfo{CWD: "/tmp"},
		EvidenceRevision: 3,
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
	for _, required := range []string{"evidence_revision: 3", "Command: printf marker", "Exit code: 0", "Output: [omitted by configuration]"} {
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
		EvidenceRevision:          5,
		Observations: []commandExecution{
			{Command: "echo old-success-1", Purpose: "Old success one", Stdout: capturedStream{Text: "old-success-1"}, ExitCode: 0},
			{Command: "false", Purpose: "Recent failure", Stderr: capturedStream{Text: "failure-marker"}, ExitCode: 1},
			{Command: "echo old-success-2", Purpose: "Old success two", Stdout: capturedStream{Text: "old-success-2"}, ExitCode: 0},
			{Command: "echo old-success-3", Purpose: "Old success three", Stdout: capturedStream{Text: "old-success-3"}, ExitCode: 0},
			{Command: "echo latest", Purpose: "Latest batch", Stdout: capturedStream{Text: "latest-marker"}, ExitCode: 0},
		},
	})

	for _, required := range []string{"evidence_revision: 5", "failure-marker", "latest-marker", "older evidence omitted: 3 execution(s)"} {
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

func TestBuildUserPromptMapsValidCompletionReferencesDuringRepair(t *testing.T) {
	prompt := buildUserPrompt(PromptRequest{
		Config:        promptOptionsForTest(configpkg.DefaultConfig()),
		Instruction:   "actualitza codex",
		ContextInfo:   contextInfo{CWD: "/tmp"},
		DecisionError: "attempt 1 does not belong to evidence revision 2",
		Attempts: []workflowAttempt{
			{ID: 1, Outcome: "success", EvidenceBefore: 0, EvidenceAfter: 1},
			{ID: 2, Outcome: "success", EvidenceBefore: 1, EvidenceAfter: 2},
			{ID: 3, Outcome: "success", EvidenceBefore: 1, EvidenceAfter: 2},
			{ID: 4, Outcome: "skipped", EvidenceBefore: 2, EvidenceAfter: 2},
		},
	})

	for _, required := range []string{
		"Valid completion references:",
		"evidence_revision 1: attempt_ids [1]",
		"evidence_revision 2: attempt_ids [2, 3]",
		"Use one evidence_revision and only its listed attempt_ids",
	} {
		if !strings.Contains(prompt, required) {
			t.Fatalf("repair prompt missing %q: %q", required, prompt)
		}
	}
	if strings.Contains(prompt, "evidence_revision 2: attempt_ids [2, 3, 4]") {
		t.Fatalf("repair prompt offered skipped attempt as completion evidence: %q", prompt)
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

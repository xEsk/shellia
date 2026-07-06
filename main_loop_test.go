package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type loopLLMResponse struct {
	content string
	stream  bool
	raw     bool
}

type loopLLMRequest struct {
	stream        bool
	body          string
	authorization string
}

type loopLLMClient struct {
	responses []loopLLMResponse
	mu        sync.Mutex
	requests  []loopLLMRequest
}

type errorBodyTransport struct{}

type contextErrorTransport struct{}

type errorReadCloser struct {
	err error
}

// newLoopLLMClient builds an OpenAI-compatible fake transport for main loop tests.
func newLoopLLMClient(t *testing.T, responses ...loopLLMResponse) *loopLLMClient {
	t.Helper()
	return &loopLLMClient{responses: responses}
}

// RoundTrip serves fake LLM responses without opening a local network listener.
func (fake *loopLLMClient) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Path != "/chat/completions" {
		return loopHTTPResponse(r, http.StatusNotFound, "unexpected path", nil), nil
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}

	var request chatCompletionRequest
	if err := json.Unmarshal(body, &request); err != nil {
		return nil, fmt.Errorf("invalid fake llm request: %w", err)
	}

	fake.mu.Lock()
	index := len(fake.requests)
	fake.requests = append(fake.requests, loopLLMRequest{
		stream:        request.Stream,
		body:          string(body),
		authorization: r.Header.Get("Authorization"),
	})
	fake.mu.Unlock()

	if index >= len(fake.responses) {
		return loopHTTPResponse(r, http.StatusInternalServerError, "unexpected LLM request", nil), nil
	}

	response := fake.responses[index]
	if response.stream {
		if response.raw {
			return loopHTTPResponse(r, http.StatusOK, response.content, map[string]string{"Content-Type": "text/event-stream"}), nil
		}

		chunk := map[string]any{
			"choices": []map[string]any{
				{"delta": map[string]any{"content": response.content}},
			},
		}
		encoded, err := json.Marshal(chunk)
		if err != nil {
			return nil, err
		}
		body := fmt.Sprintf("data: %s\n\ndata: [DONE]\n\n", encoded)
		return loopHTTPResponse(r, http.StatusOK, body, map[string]string{"Content-Type": "text/event-stream"}), nil
	}

	payload := map[string]any{
		"choices": []map[string]any{
			{"message": map[string]any{"content": response.content}},
		},
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return loopHTTPResponse(r, http.StatusOK, string(encoded), map[string]string{"Content-Type": "application/json"}), nil
}

// HTTPClient returns an isolated client backed by the fake transport.
func (fake *loopLLMClient) HTTPClient() *http.Client {
	return &http.Client{Transport: fake}
}

// RoundTrip returns a failed response whose body cannot be read.
func (transport errorBodyTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusBadRequest,
		Header:     make(http.Header),
		Body:       errorReadCloser{err: errors.New("broken error body")},
		Request:    r,
	}, nil
}

// RoundTrip waits for request cancellation and returns the context cause.
func (transport contextErrorTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	<-r.Context().Done()
	return nil, r.Context().Err()
}

// Read always fails to simulate a provider/socket error while reading the error body.
func (body errorReadCloser) Read(p []byte) (int, error) {
	return 0, body.err
}

// Close implements io.Closer for errorReadCloser.
func (body errorReadCloser) Close() error {
	return nil
}

// loopHTTPResponse builds a minimal HTTP response for the fake LLM transport.
func loopHTTPResponse(request *http.Request, status int, body string, headers map[string]string) *http.Response {
	response := &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    request,
	}
	for key, value := range headers {
		response.Header.Set(key, value)
	}
	return response
}

// URL returns the fake LLM base URL.
func (fake *loopLLMClient) URL() string {
	return "http://shellia.test"
}

// requestCount returns how many LLM requests reached the fake transport.
func (fake *loopLLMClient) requestCount() int {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	return len(fake.requests)
}

// requestStreams returns whether each fake LLM request asked for streaming.
func (fake *loopLLMClient) requestStreams() []bool {
	fake.mu.Lock()
	defer fake.mu.Unlock()

	streams := make([]bool, 0, len(fake.requests))
	for _, request := range fake.requests {
		streams = append(streams, request.stream)
	}
	return streams
}

// requestBodies returns the JSON request bodies captured by the fake transport.
func (fake *loopLLMClient) requestBodies() []string {
	fake.mu.Lock()
	defer fake.mu.Unlock()

	bodies := make([]string, 0, len(fake.requests))
	for _, request := range fake.requests {
		bodies = append(bodies, request.body)
	}
	return bodies
}

// requestAuthorizations returns the Authorization headers captured by the fake transport.
func (fake *loopLLMClient) requestAuthorizations() []string {
	fake.mu.Lock()
	defer fake.mu.Unlock()

	headers := make([]string, 0, len(fake.requests))
	for _, request := range fake.requests {
		headers = append(headers, request.authorization)
	}
	return headers
}

// loopTestConfig returns a minimal config that points every model call at the fake transport.
func loopTestConfig(baseURL string) config {
	cfg := defaultConfig()
	cfg.BaseURL = baseURL
	cfg.APIKey = "test-key"
	cfg.Model = "test-model"
	cfg.RequestTimeout = 2 * time.Second
	cfg.CommandTimeout = 2 * time.Second
	cfg.YesSafe = true
	cfg.ShowSystemOutput = false
	cfg.ShowCommandPopup = false
	return cfg
}

// loopTestContext returns an isolated shell context for main loop tests.
func loopTestContext(t *testing.T) contextInfo {
	t.Helper()

	return contextInfo{
		CWD:   t.TempDir(),
		User:  "test-user",
		OS:    "test-os",
		Shell: "/bin/sh",
	}
}

// loopTurnRequest returns a minimal turn request for main loop tests.
func loopTurnRequest(cfg config, ctxInfo *contextInfo, instruction string) turnRequest {
	return turnRequest{
		Config:      cfg,
		ContextInfo: ctxInfo,
		Instruction: instruction,
	}
}

func openLoopTrace(t *testing.T) *traceLogger {
	t.Helper()

	cfg := defaultConfig()
	cfg.TraceEnabled = true
	cfg.TraceDir = t.TempDir()
	logger, err := openSessionTrace(cfg, contextInfo{})
	if err != nil {
		t.Fatalf("openSessionTrace() error = %v", err)
	}
	t.Cleanup(func() {
		_ = logger.Close()
	})
	return logger
}

func closeLoopTraceAndRead(t *testing.T, logger *traceLogger) []map[string]any {
	t.Helper()

	if err := logger.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	return readTraceEvents(t, logger.Path())
}

// TestSwitchInteractiveModelAppliesAndPersistsDefault checks /model changes the runtime profile and config default.
func TestSwitchInteractiveModelAppliesAndPersistsDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("default_model = \"openai\"\n\n[[models]]\nname = \"openai\"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	fake := newLoopLLMClient(t, loopLLMResponse{
		content: `{"summary":"Model switched.","commands":[]}`,
	})

	cfg := defaultConfig()
	cfg.ConfigPath = path
	cfg.ModelName = "openai"
	cfg.BaseURL = "http://localhost:8080/v1"
	cfg.Model = "openai-model"
	cfg.Models = []modelConfig{
		{Name: "openai", BaseURL: "http://localhost:8080/v1", Model: "openai-model", SupportsResponseFormat: true},
		{Name: "mlx", BaseURL: fake.URL(), Model: "mlx-model", APIKey: "test-key", SupportsResponseFormat: false},
	}

	if err := switchInteractiveModel(&cfg, "mlx"); err != nil {
		t.Fatalf("switchInteractiveModel() error = %v", err)
	}
	if cfg.ModelName != "mlx" || cfg.BaseURL != fake.URL() || cfg.Model != "mlx-model" || cfg.SupportsResponseFormat {
		t.Fatalf("cfg after switch = %#v, want mlx profile without response_format", cfg)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !strings.Contains(string(data), `default_model = "mlx"`) {
		t.Fatalf("config after switch = %q, want persisted default", string(data))
	}

	ctxInfo := loopTestContext(t)
	captureMainLoopIO(t, "", fake.HTTPClient(), func(deps runtimeDeps) {
		if _, err := runTurn(t.Context(), deps, false, loopTurnRequest(cfg, &ctxInfo, "answer")); err != nil {
			t.Fatalf("runTurn() error = %v", err)
		}
	})
	bodies := fake.requestBodies()
	if len(bodies) != 1 {
		t.Fatalf("LLM request bodies = %d, want 1", len(bodies))
	}
	var body struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal([]byte(bodies[0]), &body); err != nil {
		t.Fatalf("Unmarshal(request body) error = %v", err)
	}
	if body.Model != "mlx-model" {
		t.Fatalf("request model = %q, want mlx-model", body.Model)
	}
}

// TestRunAppTraceWritesSingleSessionFile checks the main application flow owns trace lifecycle.
func TestRunAppTraceWritesSingleSessionFile(t *testing.T) {
	traceDir := t.TempDir()
	fake := newLoopLLMClient(t, loopLLMResponse{
		content: `{"summary":"No command needed.","commands":[]}`,
	})
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("SHELLIA_BASE_URL", fake.URL())
	t.Setenv("SHELLIA_MODEL", "test-model")
	t.Setenv("SHELLIA_API_KEY", "test-key")

	captureMainLoopIO(t, "", fake.HTTPClient(), func(deps runtimeDeps) {
		code := runApp(t.Context(), []string{
			"--trace",
			"--trace-dir", traceDir,
			"answer directly",
		}, deps)
		if code != 0 {
			t.Fatalf("runApp() code = %d, want 0", code)
		}
	})

	entries, err := os.ReadDir(traceDir)
	if err != nil {
		t.Fatalf("ReadDir(%q) error = %v", traceDir, err)
	}
	if len(entries) != 1 {
		t.Fatalf("trace files = %d, want 1", len(entries))
	}
	events := readTraceEvents(t, filepath.Join(traceDir, entries[0].Name()))
	if len(traceEventsByName(events, "session_start")) != 1 {
		t.Fatalf("session_start events = %d, want 1", len(traceEventsByName(events, "session_start")))
	}
	if len(traceEventsByName(events, "session_end")) != 1 {
		t.Fatalf("session_end events = %d, want 1", len(traceEventsByName(events, "session_end")))
	}
	if len(traceEventsByName(events, "turn_start")) != 1 || len(traceEventsByName(events, "turn_end")) != 1 {
		t.Fatalf("turn events start=%d end=%d, want 1 and 1", len(traceEventsByName(events, "turn_start")), len(traceEventsByName(events, "turn_end")))
	}
}

// TestSwitchInteractiveModelMissingKeepsCurrent checks bad model names do not mutate the session.
func TestSwitchInteractiveModelMissingKeepsCurrent(t *testing.T) {
	cfg := defaultConfig()
	cfg.ModelName = "openai"
	cfg.BaseURL = "http://localhost:8080/v1"
	cfg.Model = "openai-model"
	cfg.Models = []modelConfig{
		{Name: "openai", BaseURL: "http://localhost:8080/v1", Model: "openai-model", SupportsResponseFormat: true},
	}
	before := cfg

	err := switchInteractiveModel(&cfg, "missing")
	if err == nil {
		t.Fatalf("switchInteractiveModel() error = nil, want missing profile")
	}
	if cfg.ModelName != before.ModelName || cfg.Model != before.Model || cfg.BaseURL != before.BaseURL {
		t.Fatalf("cfg changed after missing profile: %#v, want %#v", cfg, before)
	}
}

// TestPrintModelProfilesToListsActiveModel checks /model without args lists configured profiles.
func TestPrintModelProfilesToListsActiveModel(t *testing.T) {
	cfg := defaultConfig()
	cfg.ModelName = "mlx"
	cfg.Models = []modelConfig{
		{Name: "openai", Model: "gpt"},
		{Name: "mlx", Model: "qwen"},
	}

	var output strings.Builder
	printModelProfilesTo(&output, false, cfg)
	got := output.String()
	if !strings.Contains(got, "* mlx · qwen") || !strings.Contains(got, "openai · gpt") {
		t.Fatalf("printModelProfilesTo() = %q, want active model list", got)
	}
}

// captureMainLoopIO replaces stdin/stdout with temporary files for deterministic loop tests.
func captureMainLoopIO(t *testing.T, input string, client *http.Client, fn func(runtimeDeps)) string {
	t.Helper()

	dir := t.TempDir()
	stdinFile, err := os.CreateTemp(dir, "stdin")
	if err != nil {
		t.Fatalf("CreateTemp(stdin) error = %v", err)
	}
	if _, err := stdinFile.WriteString(input); err != nil {
		t.Fatalf("WriteString(stdin) error = %v", err)
	}
	if _, err := stdinFile.Seek(0, io.SeekStart); err != nil {
		t.Fatalf("Seek(stdin) error = %v", err)
	}

	stdoutFile, err := os.CreateTemp(dir, "stdout")
	if err != nil {
		t.Fatalf("CreateTemp(stdout) error = %v", err)
	}

	defer func() {
		stdinFile.Close()  //nolint:errcheck // best-effort cleanup of temporary test files.
		stdoutFile.Close() //nolint:errcheck // best-effort cleanup of temporary test files.
	}()

	deps := defaultRuntimeDeps()
	deps.Stdin = stdinFile
	deps.Stdout = stdoutFile
	deps.Stderr = stdoutFile
	deps.HTTPClient = client

	fn(deps)

	if _, err := stdoutFile.Seek(0, io.SeekStart); err != nil {
		t.Fatalf("Seek(stdout) error = %v", err)
	}
	output, err := io.ReadAll(stdoutFile)
	if err != nil {
		t.Fatalf("ReadAll(stdout) error = %v", err)
	}
	return string(output)
}

// TestDoLLMRequestOmitsAuthorizationWhenAPIKeyEmpty checks local no-key endpoints get no auth header.
func TestDoLLMRequestOmitsAuthorizationWhenAPIKeyEmpty(t *testing.T) {
	fake := newLoopLLMClient(t, loopLLMResponse{content: "ok"})
	cfg := loopTestConfig("http://localhost")
	cfg.APIKey = ""

	_, err := doLLMRequest(t.Context(), fake.HTTPClient(), cfg, chatCompletionRequest{
		Model:       cfg.Model,
		Temperature: 0,
		Messages:    []chatMessage{{Role: "user", Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("doLLMRequest() error = %v", err)
	}

	headers := fake.requestAuthorizations()
	if len(headers) != 1 || headers[0] != "" {
		t.Fatalf("Authorization headers = %#v, want empty header", headers)
	}
}

// TestDoLLMRequestSendsAuthorizationWhenAPIKeySet checks keyed endpoints keep bearer auth.
func TestDoLLMRequestSendsAuthorizationWhenAPIKeySet(t *testing.T) {
	fake := newLoopLLMClient(t, loopLLMResponse{content: "ok"})
	cfg := loopTestConfig(fake.URL())
	cfg.APIKey = "test-key"

	_, err := doLLMRequest(t.Context(), fake.HTTPClient(), cfg, chatCompletionRequest{
		Model:       cfg.Model,
		Temperature: 0,
		Messages:    []chatMessage{{Role: "user", Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("doLLMRequest() error = %v", err)
	}

	headers := fake.requestAuthorizations()
	if len(headers) != 1 || headers[0] != "Bearer test-key" {
		t.Fatalf("Authorization headers = %#v, want bearer token", headers)
	}
}

// TestDoLLMRequestPropagatesContextCancellation checks LLM calls preserve cancellation identity.
func TestDoLLMRequestPropagatesContextCancellation(t *testing.T) {
	cfg := loopTestConfig("http://shellia.test")
	client := &http.Client{Transport: contextErrorTransport{}}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := doLLMRequest(ctx, client, cfg, chatCompletionRequest{
		Model:    cfg.Model,
		Messages: []chatMessage{{Role: "user", Content: "hello"}},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("doLLMRequest() error = %v, want context.Canceled", err)
	}
}

// TestCallPlanningPromptUsesResponseFormatForLocalNoKey checks JSON mode is profile-driven.
func TestCallPlanningPromptUsesResponseFormatForLocalNoKey(t *testing.T) {
	fake := newLoopLLMClient(t, loopLLMResponse{content: "ok"})
	cfg := loopTestConfig("http://localhost")
	cfg.APIKey = ""

	if _, err := callPlanningPrompt(t.Context(), fake.HTTPClient(), cfg, "system", "user"); err != nil {
		t.Fatalf("callPlanningPrompt() error = %v", err)
	}

	bodies := fake.requestBodies()
	if len(bodies) != 1 {
		t.Fatalf("request bodies = %#v, want one body", bodies)
	}

	var body struct {
		ResponseFormat *responseFormat `json:"response_format"`
	}
	if err := json.Unmarshal([]byte(bodies[0]), &body); err != nil {
		t.Fatalf("Unmarshal(request body) error = %v", err)
	}
	if body.ResponseFormat == nil || body.ResponseFormat.Type != "json_object" {
		t.Fatalf("response_format = %#v, want json_object", body.ResponseFormat)
	}
}

// TestCallPlanningPromptOmitsResponseFormatWhenUnsupported checks profile capability disables JSON mode.
func TestCallPlanningPromptOmitsResponseFormatWhenUnsupported(t *testing.T) {
	fake := newLoopLLMClient(t, loopLLMResponse{content: "ok"})
	cfg := loopTestConfig("http://localhost")
	cfg.SupportsResponseFormat = false

	if _, err := callPlanningPrompt(t.Context(), fake.HTTPClient(), cfg, "system", "user"); err != nil {
		t.Fatalf("callPlanningPrompt() error = %v", err)
	}

	bodies := fake.requestBodies()
	if len(bodies) != 1 {
		t.Fatalf("request bodies = %#v, want one body", bodies)
	}

	var body map[string]any
	if err := json.Unmarshal([]byte(bodies[0]), &body); err != nil {
		t.Fatalf("Unmarshal(request body) error = %v", err)
	}
	if _, ok := body["response_format"]; ok {
		t.Fatalf("request body includes response_format: %s", bodies[0])
	}
}

// TestCallPlanningPromptKeepsResponseFormatWithKey checks JSON mode remains for compatible providers.
func TestCallPlanningPromptKeepsResponseFormatWithKey(t *testing.T) {
	fake := newLoopLLMClient(t, loopLLMResponse{content: "ok"})
	cfg := loopTestConfig(fake.URL())

	if _, err := callPlanningPrompt(t.Context(), fake.HTTPClient(), cfg, "system", "user"); err != nil {
		t.Fatalf("callPlanningPrompt() error = %v", err)
	}

	bodies := fake.requestBodies()
	if len(bodies) != 1 {
		t.Fatalf("request bodies = %#v, want one body", bodies)
	}

	var body struct {
		ResponseFormat *responseFormat `json:"response_format"`
	}
	if err := json.Unmarshal([]byte(bodies[0]), &body); err != nil {
		t.Fatalf("Unmarshal(request body) error = %v", err)
	}
	if body.ResponseFormat == nil || body.ResponseFormat.Type != "json_object" {
		t.Fatalf("response_format = %#v, want json_object", body.ResponseFormat)
	}
}

// TestRunTurnPrintsRawPrompt checks --raw-prompt exposes the exact model prompt pair.
func TestRunTurnPrintsRawPrompt(t *testing.T) {
	fake := newLoopLLMClient(t, loopLLMResponse{
		content: `{"summary":"No command needed.","commands":[]}`,
	})
	cfg := loopTestConfig(fake.URL())
	cfg.RawPrompt = true
	ctxInfo := loopTestContext(t)

	output := captureMainLoopIO(t, "", fake.HTTPClient(), func(deps runtimeDeps) {
		if _, err := runTurn(t.Context(), deps, false, loopTurnRequest(cfg, &ctxInfo, "answer directly")); err != nil {
			t.Fatalf("runTurn() error = %v", err)
		}
	})

	required := []string{
		"Raw LLM prompt",
		"system:",
		"You are Shellia's planning layer.",
		"Decision process:",
		"user:",
		"User instruction:\nanswer directly",
	}
	for _, snippet := range required {
		if !strings.Contains(output, snippet) {
			t.Fatalf("runTurn() output missing %q in %q", snippet, output)
		}
	}
}

// TestRunTurnReturnsFinalAnswerWithoutCommands checks the answer-only path of the main turn loop.
func TestRunTurnReturnsFinalAnswerWithoutCommands(t *testing.T) {
	fake := newLoopLLMClient(t, loopLLMResponse{
		content: `{"summary":"No command needed.","commands":[]}`,
	})
	cfg := loopTestConfig(fake.URL())
	ctxInfo := loopTestContext(t)

	var result turnResult
	output := captureMainLoopIO(t, "", fake.HTTPClient(), func(deps runtimeDeps) {
		var err error
		result, err = runTurn(t.Context(), deps, false, loopTurnRequest(cfg, &ctxInfo, "answer directly"))
		if err != nil {
			t.Fatalf("runTurn() error = %v", err)
		}
	})

	if result.Actionable {
		t.Fatalf("runTurn() Actionable = true, want false")
	}
	if result.Result != "No command needed." {
		t.Fatalf("runTurn() Result = %q, want %q", result.Result, "No command needed.")
	}
	if fake.requestCount() != 1 {
		t.Fatalf("LLM requests = %d, want 1", fake.requestCount())
	}
	if !strings.Contains(output, "No command needed.") {
		t.Fatalf("runTurn() output does not contain final answer: %q", output)
	}
}

// TestRunTurnTraceRecordsFinalAnswerWithoutCommands checks empty plans are diagnosable.
func TestRunTurnTraceRecordsFinalAnswerWithoutCommands(t *testing.T) {
	fake := newLoopLLMClient(t, loopLLMResponse{
		content: `{"summary":"No command needed.","commands":[]}`,
	})
	cfg := loopTestConfig(fake.URL())
	ctxInfo := loopTestContext(t)
	logger := openLoopTrace(t)

	captureMainLoopIO(t, "", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.Trace = logger
		if _, err := runTurn(t.Context(), deps, false, loopTurnRequest(cfg, &ctxInfo, "answer directly")); err != nil {
			t.Fatalf("runTurn() error = %v", err)
		}
	})

	events := closeLoopTraceAndRead(t, logger)
	if len(traceEventsByName(events, "turn_start")) != 1 {
		t.Fatalf("turn_start events = %d, want 1", len(traceEventsByName(events, "turn_start")))
	}
	if len(traceEventsByName(events, "llm_prompt")) != 1 {
		t.Fatalf("llm_prompt events = %d, want 1", len(traceEventsByName(events, "llm_prompt")))
	}
	plannerEvents := traceEventsByName(events, "planner_result")
	if len(plannerEvents) != 1 {
		t.Fatalf("planner_result events = %d, want 1", len(plannerEvents))
	}
	plannerData := traceEventData(t, plannerEvents[0])
	if plannerData["commands_count"] != float64(0) {
		t.Fatalf("commands_count = %#v, want 0", plannerData["commands_count"])
	}
	decisionEvents := traceEventsByName(events, "shellia_decision")
	if len(decisionEvents) != 1 {
		t.Fatalf("shellia_decision events = %d, want 1", len(decisionEvents))
	}
	decisionData := traceEventData(t, decisionEvents[0])
	if decisionData["decision"] != "final_answer_without_commands" {
		t.Fatalf("decision = %#v, want final_answer_without_commands", decisionData["decision"])
	}
}

// TestRunTurnExecutesSafePlanAndStreamsSummary checks planning, execution, and final summarization.
func TestRunTurnExecutesSafePlanAndStreamsSummary(t *testing.T) {
	fake := newLoopLLMClient(t,
		loopLLMResponse{
			content: `{"summary":"Print a marker.","commands":[{"command":"echo shellia-loop","purpose":"Print marker","risk":"safe","requires_confirmation":false,"interactive":false,"interactive_reason":""}]}`,
		},
		loopLLMResponse{content: "Printed shellia-loop.", stream: true},
	)
	cfg := loopTestConfig(fake.URL())
	ctxInfo := loopTestContext(t)

	var result turnResult
	captureMainLoopIO(t, "yes\n", fake.HTTPClient(), func(deps runtimeDeps) {
		var err error
		result, err = runTurn(t.Context(), deps, false, loopTurnRequest(cfg, &ctxInfo, "print marker"))
		if err != nil {
			t.Fatalf("runTurn() error = %v", err)
		}
	})

	if !result.Actionable {
		t.Fatalf("runTurn() Actionable = false, want true")
	}
	if len(result.Executions) != 1 {
		t.Fatalf("runTurn() executions = %d, want 1", len(result.Executions))
	}
	if result.Executions[0].ExitCode != 0 {
		t.Fatalf("execution exit code = %d, want 0", result.Executions[0].ExitCode)
	}
	if result.Executions[0].Stdout.Text != "shellia-loop" {
		t.Fatalf("execution stdout = %q, want %q", result.Executions[0].Stdout.Text, "shellia-loop")
	}
	if result.Result != "Printed shellia-loop." {
		t.Fatalf("runTurn() Result = %q, want %q", result.Result, "Printed shellia-loop.")
	}

	streams := fake.requestStreams()
	if len(streams) != 2 || streams[0] || !streams[1] {
		t.Fatalf("request streams = %#v, want []bool{false, true}", streams)
	}
}

// TestRunTurnTraceRecordsSummaryPromptAndResponse checks final responses are traced.
func TestRunTurnTraceRecordsSummaryPromptAndResponse(t *testing.T) {
	fake := newLoopLLMClient(t,
		loopLLMResponse{
			content: `{"summary":"Print a marker.","commands":[{"command":"echo shellia-loop","purpose":"Print marker","risk":"safe","requires_confirmation":false,"interactive":false,"interactive_reason":""}]}`,
		},
		loopLLMResponse{content: "Printed shellia-loop.", stream: true},
	)
	cfg := loopTestConfig(fake.URL())
	ctxInfo := loopTestContext(t)
	logger := openLoopTrace(t)

	captureMainLoopIO(t, "yes\n", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.Trace = logger
		deps.ExecuteCommands = func(_ context.Context, _ runtimeDeps, _ bool, _ config, _ *contextInfo, plans []commandPlan) ([]commandExecution, error) {
			return []commandExecution{{
				Command:  plans[0].Command,
				Purpose:  plans[0].Purpose,
				ExitCode: 0,
				Stdout:   capturedStream{Text: "shellia-loop", TotalBytes: 12, KeptBytes: 12},
			}}, nil
		}
		if _, err := runTurn(t.Context(), deps, false, loopTurnRequest(cfg, &ctxInfo, "print marker")); err != nil {
			t.Fatalf("runTurn() error = %v", err)
		}
	})

	events := closeLoopTraceAndRead(t, logger)
	var summaryPrompts int
	var summaryResponses int
	for _, event := range events {
		if event["phase"] != "summary" {
			continue
		}
		switch event["event"] {
		case "llm_prompt":
			summaryPrompts++
		case "llm_response":
			summaryResponses++
			data := traceEventData(t, event)
			if data["raw_response"] != "Printed shellia-loop." {
				t.Fatalf("summary raw_response = %#v, want streamed answer", data["raw_response"])
			}
		}
	}
	if summaryPrompts != 1 || summaryResponses != 1 {
		t.Fatalf("summary events = prompts %d responses %d, want 1 and 1", summaryPrompts, summaryResponses)
	}
}

// TestRunTurnDeclinesPlanWithoutExecuting checks a rejected plan does not run or summarize commands.
func TestRunTurnDeclinesPlanWithoutExecuting(t *testing.T) {
	fake := newLoopLLMClient(t, loopLLMResponse{
		content: `{"summary":"Print a marker.","commands":[{"command":"echo shellia-loop","purpose":"Print marker","risk":"safe","requires_confirmation":false,"interactive":false,"interactive_reason":""}]}`,
	})
	cfg := loopTestConfig(fake.URL())
	ctxInfo := loopTestContext(t)
	executed := false

	var result turnResult
	output := captureMainLoopIO(t, "no\n", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.ExecuteCommands = func(context.Context, runtimeDeps, bool, config, *contextInfo, []commandPlan) ([]commandExecution, error) {
			executed = true
			return nil, nil
		}

		var err error
		result, err = runTurn(t.Context(), deps, false, loopTurnRequest(cfg, &ctxInfo, "print marker"))
		if err != nil {
			t.Fatalf("runTurn() error = %v", err)
		}
	})

	if executed {
		t.Fatalf("ExecuteCommands was called after declining the plan")
	}
	if result.Actionable || len(result.Plans) != 1 || len(result.Executions) != 0 {
		t.Fatalf("runTurn() result = %#v, want declined plan without executions", result)
	}
	if fake.requestCount() != 1 {
		t.Fatalf("LLM requests = %d, want 1", fake.requestCount())
	}
	if !strings.Contains(output, "Plan not executed.") {
		t.Fatalf("declined plan output does not contain cancellation message: %q", output)
	}
}

// TestRunTurnPlanOnlyPrintsCommandsWithoutExecuting checks -p style turns stop after planning.
func TestRunTurnPlanOnlyPrintsCommandsWithoutExecuting(t *testing.T) {
	fake := newLoopLLMClient(t, loopLLMResponse{
		content: `{"summary":"Create a marker file.","commands":[{"command":"touch marker.txt","purpose":"Create marker","risk":"medium","requires_confirmation":true,"interactive":false,"interactive_reason":""}]}`,
	})
	cfg := loopTestConfig(fake.URL())
	cfg.PlanOnly = true
	ctxInfo := loopTestContext(t)
	executed := false

	var result turnResult
	output := captureMainLoopIO(t, "no\n", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.ExecuteCommands = func(context.Context, runtimeDeps, bool, config, *contextInfo, []commandPlan) ([]commandExecution, error) {
			executed = true
			return nil, nil
		}

		var err error
		result, err = runTurn(t.Context(), deps, false, loopTurnRequest(cfg, &ctxInfo, "create marker"))
		if err != nil {
			t.Fatalf("runTurn() error = %v", err)
		}
	})

	if executed {
		t.Fatalf("ExecuteCommands was called in plan-only mode")
	}
	if !result.Actionable || len(result.Plans) != 1 || len(result.Executions) != 0 {
		t.Fatalf("runTurn() result = %#v, want one planned command and no executions", result)
	}
	if !strings.Contains(output, "touch marker.txt") {
		t.Fatalf("plan-only output does not contain command: %q", output)
	}
	if !strings.Contains(output, "run › touch marker.txt") {
		t.Fatalf("plan-only output does not render command box: %q", output)
	}
	for _, hidden := range []string{"risk", "safety"} {
		if strings.Contains(output, hidden) {
			t.Fatalf("plan-only output contains verbose metadata %q: %q", hidden, output)
		}
	}
}

// TestRunTurnPlanOnlyExecutesAcceptedPlan checks /plan can run the exact accepted plan.
func TestRunTurnPlanOnlyExecutesAcceptedPlan(t *testing.T) {
	fake := newLoopLLMClient(t,
		loopLLMResponse{
			content: `{"summary":"Create a marker file.","commands":[{"command":"touch marker.txt","purpose":"Create marker","risk":"medium","requires_confirmation":true,"interactive":false,"interactive_reason":""}]}`,
		},
		loopLLMResponse{content: "Created marker file.", stream: true},
	)
	cfg := loopTestConfig(fake.URL())
	cfg.PlanOnly = true
	ctxInfo := loopTestContext(t)
	var gotPlans []commandPlan

	var result turnResult
	output := captureMainLoopIO(t, "y\n", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.ExecuteCommands = func(_ context.Context, _ runtimeDeps, _ bool, _ config, _ *contextInfo, plans []commandPlan) ([]commandExecution, error) {
			gotPlans = append([]commandPlan{}, plans...)
			return []commandExecution{{
				Command:  plans[0].Command,
				Purpose:  plans[0].Purpose,
				ExitCode: 0,
			}}, nil
		}

		var err error
		result, err = runTurn(t.Context(), deps, false, loopTurnRequest(cfg, &ctxInfo, "create marker"))
		if err != nil {
			t.Fatalf("runTurn() error = %v", err)
		}
	})

	if len(gotPlans) != 1 || gotPlans[0].Command != "touch marker.txt" {
		t.Fatalf("ExecuteCommands plans = %#v, want planned touch command", gotPlans)
	}
	if !result.Actionable || len(result.Executions) != 1 {
		t.Fatalf("runTurn() result = %#v, want executed plan", result)
	}
	if fake.requestCount() != 2 {
		t.Fatalf("LLM requests = %d, want 2", fake.requestCount())
	}
	if !strings.Contains(output, "Created marker file.") {
		t.Fatalf("accepted plan output does not contain streamed summary: %q", output)
	}
}

// TestRunTurnPlanOnlyExplainsObservationDependency checks unresolved follow-up plans explain the dependency.
func TestRunTurnPlanOnlyExplainsObservationDependency(t *testing.T) {
	fake := newLoopLLMClient(t, loopLLMResponse{
		content: `{"summary":"First inspect the repo.","requires_observation":true,"observation_reason":"The next command depends on which package manager appears in the output.","commands":[{"command":"ls","purpose":"Inspect files","risk":"safe","requires_confirmation":false,"interactive":false,"interactive_reason":""}]}`,
	})
	cfg := loopTestConfig(fake.URL())
	cfg.PlanOnly = true
	ctxInfo := loopTestContext(t)

	output := captureMainLoopIO(t, "no\n", fake.HTTPClient(), func(deps runtimeDeps) {
		if _, err := runTurn(t.Context(), deps, false, loopTurnRequest(cfg, &ctxInfo, "install deps")); err != nil {
			t.Fatalf("runTurn() error = %v", err)
		}
	})

	if !strings.Contains(output, "The next command depends on which package manager appears in the output.") {
		t.Fatalf("plan-only output does not explain observation dependency: %q", output)
	}
	if !strings.Contains(output, "next step") {
		t.Fatalf("plan-only output does not label observation guidance as next step: %q", output)
	}
}

// TestRunTurnPlanOnlySkipsDiscoveryRepair checks /plan does not retry with discovery prompts.
func TestRunTurnPlanOnlySkipsDiscoveryRepair(t *testing.T) {
	fake := newLoopLLMClient(t,
		loopLLMResponse{
			content: `{"summary":"Need the target container.","requires_input":true,"input_reason":"No Docker container or image was specified.","commands":[]}`,
		},
		loopLLMResponse{
			content: `{"summary":"Unexpected repair.","commands":[{"command":"docker ps","purpose":"Inspect Docker","risk":"safe","requires_confirmation":false,"interactive":false,"interactive_reason":""}]}`,
		},
	)
	cfg := loopTestConfig(fake.URL())
	cfg.PlanOnly = true
	ctxInfo := loopTestContext(t)

	output := captureMainLoopIO(t, "", fake.HTTPClient(), func(deps runtimeDeps) {
		if _, err := runTurn(t.Context(), deps, false, loopTurnRequest(cfg, &ctxInfo, "run php in docker")); err != nil {
			t.Fatalf("runTurn() error = %v", err)
		}
	})

	if fake.requestCount() != 1 {
		t.Fatalf("LLM requests = %d, want 1", fake.requestCount())
	}
	if !strings.Contains(output, "missing detail") {
		t.Fatalf("plan-only output does not show missing detail guidance: %q", output)
	}
	if strings.Contains(output, "Unexpected repair") || strings.Contains(output, "docker ps") {
		t.Fatalf("plan-only output used discovery repair response: %q", output)
	}
}

// TestRunTurnUsesDiscoveryRepairForRecoverableEmptyPlan checks empty input responses get one discovery retry.
func TestRunTurnUsesDiscoveryRepairForRecoverableEmptyPlan(t *testing.T) {
	fake := newLoopLLMClient(t,
		loopLLMResponse{
			content: `{"summary":"Need the installed tool.","requires_input":true,"input_reason":"The install source is unknown.","commands":[]}`,
		},
		loopLLMResponse{
			content: `{"summary":"Checking the local tool path.","requires_observation":false,"observation_reason":"","commands":[{"command":"command -v shellia","purpose":"Find shellia binary","risk":"safe","requires_confirmation":false,"interactive":false,"interactive_reason":""}]}`,
		},
		loopLLMResponse{content: "Found the shellia binary.", stream: true},
	)
	cfg := loopTestConfig(fake.URL())
	ctxInfo := loopTestContext(t)

	var gotPlans []commandPlan
	var result turnResult
	output := captureMainLoopIO(t, "yes\n", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.ExecuteCommands = func(_ context.Context, _ runtimeDeps, _ bool, _ config, _ *contextInfo, plans []commandPlan) ([]commandExecution, error) {
			gotPlans = append([]commandPlan{}, plans...)
			return []commandExecution{{
				Command:  plans[0].Command,
				Purpose:  plans[0].Purpose,
				ExitCode: 0,
				Stdout:   capturedStream{Text: "/usr/local/bin/shellia"},
			}}, nil
		}

		var err error
		result, err = runTurn(t.Context(), deps, false, loopTurnRequest(cfg, &ctxInfo, "update shellia"))
		if err != nil {
			t.Fatalf("runTurn() error = %v", err)
		}
	})

	if len(gotPlans) != 1 || gotPlans[0].Command != "command -v shellia" {
		t.Fatalf("ExecuteCommands plans = %#v, want discovery command", gotPlans)
	}
	if !result.Actionable || len(result.Executions) != 1 {
		t.Fatalf("runTurn() result = %#v, want executed discovery plan", result)
	}
	if fake.requestCount() != 3 {
		t.Fatalf("LLM requests = %d, want initial, repair, and summary requests", fake.requestCount())
	}
	bodies := fake.requestBodies()
	if len(bodies) < 2 || !strings.Contains(bodies[1], "Discovery repair mode") {
		t.Fatalf("repair request body = %#v, want discovery repair prompt", bodies)
	}
	if !strings.Contains(output, "Found the shellia binary.") {
		t.Fatalf("runTurn() output missing final summary: %q", output)
	}
}

// TestRunTurnTraceRecordsDiscoveryRepair checks repair prompts are tagged separately.
func TestRunTurnTraceRecordsDiscoveryRepair(t *testing.T) {
	fake := newLoopLLMClient(t,
		loopLLMResponse{
			content: `{"summary":"Need the installed tool.","requires_input":true,"input_reason":"The install source is unknown.","commands":[]}`,
		},
		loopLLMResponse{
			content: `{"summary":"Checking the local tool path.","commands":[{"command":"command -v shellia","purpose":"Find shellia binary","risk":"safe","requires_confirmation":false,"interactive":false,"interactive_reason":""}]}`,
		},
		loopLLMResponse{content: "Found the shellia binary.", stream: true},
	)
	cfg := loopTestConfig(fake.URL())
	ctxInfo := loopTestContext(t)
	logger := openLoopTrace(t)

	captureMainLoopIO(t, "yes\n", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.Trace = logger
		deps.ExecuteCommands = func(_ context.Context, _ runtimeDeps, _ bool, _ config, _ *contextInfo, plans []commandPlan) ([]commandExecution, error) {
			return []commandExecution{{
				Command:  plans[0].Command,
				Purpose:  plans[0].Purpose,
				ExitCode: 0,
				Stdout:   capturedStream{Text: "/usr/local/bin/shellia"},
			}}, nil
		}
		if _, err := runTurn(t.Context(), deps, false, loopTurnRequest(cfg, &ctxInfo, "update shellia")); err != nil {
			t.Fatalf("runTurn() error = %v", err)
		}
	})

	events := closeLoopTraceAndRead(t, logger)
	var planningPrompt bool
	var repairPrompt bool
	var repairResponse bool
	for _, event := range events {
		if event["event"] == "llm_prompt" && event["phase"] == "planning" {
			planningPrompt = true
		}
		if event["event"] == "llm_prompt" && event["phase"] == "discovery_repair" {
			repairPrompt = true
		}
		if event["event"] == "llm_response" && event["phase"] == "discovery_repair" {
			repairResponse = true
		}
	}
	if !planningPrompt || !repairPrompt || !repairResponse {
		t.Fatalf("trace phases planning=%t repair_prompt=%t repair_response=%t", planningPrompt, repairPrompt, repairResponse)
	}
}

// TestRunTurnCanContinueAfterPlanningRoundLimit checks users can approve extra planning rounds.
func TestRunTurnCanContinueAfterPlanningRoundLimit(t *testing.T) {
	fake := newLoopLLMClient(t,
		loopLLMResponse{
			content: `{"summary":"Inspect the first fact.","requires_observation":true,"commands":[{"command":"echo first","purpose":"Inspect first fact","risk":"safe","requires_confirmation":false,"interactive":false,"interactive_reason":""}]}`,
		},
		loopLLMResponse{
			content: `{"summary":"Inspect the second fact.","requires_observation":true,"commands":[{"command":"echo second","purpose":"Inspect second fact","risk":"safe","requires_confirmation":false,"interactive":false,"interactive_reason":""}]}`,
		},
		loopLLMResponse{
			content: `{"summary":"Done after extra planning.","commands":[]}`,
		},
	)
	cfg := loopTestConfig(fake.URL())
	cfg.AskConfirmPlan = false
	cfg.PlanningMaxRounds = 2
	ctxInfo := loopTestContext(t)
	var executed []string

	var result turnResult
	output := captureMainLoopIO(t, "y\n", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.ExecuteCommands = func(_ context.Context, _ runtimeDeps, _ bool, _ config, _ *contextInfo, plans []commandPlan) ([]commandExecution, error) {
			executed = append(executed, plans[0].Command)
			return []commandExecution{{
				Command:  plans[0].Command,
				Purpose:  plans[0].Purpose,
				ExitCode: 0,
				Stdout:   capturedStream{Text: strings.TrimPrefix(plans[0].Command, "echo ")},
			}}, nil
		}

		var err error
		result, err = runTurn(t.Context(), deps, false, loopTurnRequest(cfg, &ctxInfo, "inspect twice"))
		if err != nil {
			t.Fatalf("runTurn() error = %v", err)
		}
	})

	if got := strings.Join(executed, ","); got != "echo first,echo second" {
		t.Fatalf("executed commands = %q, want both discovery commands", got)
	}
	if result.Result != "Done after extra planning." {
		t.Fatalf("result = %q, want final extra-round answer", result.Result)
	}
	if fake.requestCount() != 3 {
		t.Fatalf("LLM requests = %d, want 3", fake.requestCount())
	}
	if !strings.Contains(output, "planning reached the current follow-up round limit (2)") {
		t.Fatalf("output missing planning limit warning: %q", output)
	}
	if !strings.Contains(output, "Continue planning? [y/n]: yes") {
		t.Fatalf("output missing continuation confirmation: %q", output)
	}
}

// TestRunTurnSummarizesWhenPlanningRoundLimitIsDeclined checks declining continuation ends cleanly.
func TestRunTurnSummarizesWhenPlanningRoundLimitIsDeclined(t *testing.T) {
	fake := newLoopLLMClient(t,
		loopLLMResponse{
			content: `{"summary":"Inspect one fact.","requires_observation":true,"commands":[{"command":"echo first","purpose":"Inspect first fact","risk":"safe","requires_confirmation":false,"interactive":false,"interactive_reason":""}]}`,
		},
		loopLLMResponse{content: "Stopped after the observed fact.", stream: true},
	)
	cfg := loopTestConfig(fake.URL())
	cfg.AskConfirmPlan = false
	cfg.PlanningMaxRounds = 1
	ctxInfo := loopTestContext(t)

	var result turnResult
	output := captureMainLoopIO(t, "n\n", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.ExecuteCommands = func(_ context.Context, _ runtimeDeps, _ bool, _ config, _ *contextInfo, plans []commandPlan) ([]commandExecution, error) {
			return []commandExecution{{
				Command:  plans[0].Command,
				Purpose:  plans[0].Purpose,
				ExitCode: 0,
				Stdout:   capturedStream{Text: "first"},
			}}, nil
		}

		var err error
		result, err = runTurn(t.Context(), deps, false, loopTurnRequest(cfg, &ctxInfo, "inspect once"))
		if err != nil {
			t.Fatalf("runTurn() error = %v", err)
		}
	})

	if result.Result != "Stopped after the observed fact." {
		t.Fatalf("result = %q, want streamed summary", result.Result)
	}
	if fake.requestCount() != 2 {
		t.Fatalf("LLM requests = %d, want planning and summary", fake.requestCount())
	}
	if !strings.Contains(output, "SHELLIA_PLANNING_MAX_ROUNDS") {
		t.Fatalf("output missing env override guidance: %q", output)
	}
	if !strings.Contains(output, "Continue planning? [y/n]: no") {
		t.Fatalf("output missing declined continuation: %q", output)
	}
}

// TestRunTurnPlanOnlyUsesDedicatedSystemPrompt checks /plan sends the plan-only prompt contract.
func TestRunTurnPlanOnlyUsesDedicatedSystemPrompt(t *testing.T) {
	fake := newLoopLLMClient(t, loopLLMResponse{
		content: `{"summary":"Create a marker file.","commands":[{"command":"touch marker.txt","purpose":"Preparation: create marker","risk":"medium","requires_confirmation":true,"interactive":false,"interactive_reason":""}]}`,
	})
	cfg := loopTestConfig(fake.URL())
	cfg.PlanOnly = true
	ctxInfo := loopTestContext(t)

	captureMainLoopIO(t, "no\n", fake.HTTPClient(), func(deps runtimeDeps) {
		if _, err := runTurn(t.Context(), deps, false, loopTurnRequest(cfg, &ctxInfo, "create marker")); err != nil {
			t.Fatalf("runTurn() error = %v", err)
		}
	})

	bodies := fake.requestBodies()
	if len(bodies) != 1 {
		t.Fatalf("LLM request bodies = %d, want 1", len(bodies))
	}
	if !strings.Contains(bodies[0], "non-executing plan mode") {
		t.Fatalf("plan-only request did not use dedicated system prompt: %q", bodies[0])
	}
}

// TestRunInteractiveProcessesPromptThenExit checks that the interactive loop runs one AI turn and exits cleanly.
func TestRunInteractiveProcessesPromptThenExit(t *testing.T) {
	fake := newLoopLLMClient(t, loopLLMResponse{
		content: `{"summary":"Interactive answer.","commands":[]}`,
	})
	cfg := loopTestConfig(fake.URL())
	ctxInfo := loopTestContext(t)

	output := captureMainLoopIO(t, "answer something\n/exit\n", fake.HTTPClient(), func(deps runtimeDeps) {
		runInteractive(t.Context(), deps, false, cfg, &ctxInfo)
	})

	if fake.requestCount() != 1 {
		t.Fatalf("LLM requests = %d, want 1", fake.requestCount())
	}
	if !strings.Contains(output, "Interactive answer.") {
		t.Fatalf("interactive output does not contain AI answer: %q", output)
	}
	if !strings.Contains(output, "Session closed.") {
		t.Fatalf("interactive output does not contain close message: %q", output)
	}
}

// TestRunInteractiveModelCommandSwitchesWithoutLLM checks /model is handled locally.
func TestRunInteractiveModelCommandSwitchesWithoutLLM(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("default_model = \"openai\"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	fake := newLoopLLMClient(t)
	cfg := loopTestConfig(fake.URL())
	cfg.ConfigPath = path
	cfg.ModelName = "openai"
	cfg.Models = []modelConfig{
		{Name: "openai", BaseURL: fake.URL(), Model: "openai-model", APIKey: "test-key", SupportsResponseFormat: true},
		{Name: "mlx", BaseURL: fake.URL(), Model: "mlx-model", APIKey: "test-key", SupportsResponseFormat: false},
	}
	ctxInfo := loopTestContext(t)

	output := captureMainLoopIO(t, "/model mlx\n/exit\n", fake.HTTPClient(), func(deps runtimeDeps) {
		runInteractive(t.Context(), deps, false, cfg, &ctxInfo)
	})

	if fake.requestCount() != 0 {
		t.Fatalf("LLM requests = %d, want 0", fake.requestCount())
	}
	if !strings.Contains(output, "Model switched to mlx") {
		t.Fatalf("output = %q, want model switch message", output)
	}
	if !strings.Contains(output, " · mlx-model") {
		t.Fatalf("output = %q, want selected model detail", output)
	}
	if !strings.Contains(output, "\nShellia Model switched to mlx.") {
		t.Fatalf("output = %q, want blank line before model switch message", output)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !strings.Contains(string(data), `default_model = "mlx"`) {
		t.Fatalf("config after /model = %q, want persisted default", string(data))
	}
}

// TestRunInteractivePlanCommandPlansWithoutExecuting checks /plan is scoped to one prompt.
func TestRunInteractivePlanCommandPlansWithoutExecuting(t *testing.T) {
	fake := newLoopLLMClient(t, loopLLMResponse{
		content: `{"summary":"Create a marker file.","commands":[{"command":"touch marker.txt","purpose":"Create marker","risk":"medium","requires_confirmation":true,"interactive":false,"interactive_reason":""}]}`,
	})
	cfg := loopTestConfig(fake.URL())
	ctxInfo := loopTestContext(t)
	executed := false

	output := captureMainLoopIO(t, "/plan create marker\nno\n/exit\n", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.ExecuteCommands = func(context.Context, runtimeDeps, bool, config, *contextInfo, []commandPlan) ([]commandExecution, error) {
			executed = true
			return nil, nil
		}
		runInteractive(t.Context(), deps, false, cfg, &ctxInfo)
	})

	if executed {
		t.Fatalf("ExecuteCommands was called for /plan")
	}
	if !strings.Contains(output, "touch marker.txt") {
		t.Fatalf("/plan output does not contain command: %q", output)
	}
	if !strings.Contains(output, "Session closed.") {
		t.Fatalf("interactive output does not contain close message: %q", output)
	}
}

// TestRunInteractiveRetryCommandRepeatsLastFailedInstruction checks /retry is a local command.
func TestRunInteractiveRetryCommandRepeatsLastFailedInstruction(t *testing.T) {
	fake := newLoopLLMClient(t,
		loopLLMResponse{content: `not json`},
		loopLLMResponse{content: `{"summary":"No command needed.","commands":[]}`},
	)
	cfg := loopTestConfig(fake.URL())
	ctxInfo := loopTestContext(t)

	output := captureMainLoopIO(t, "build it\n/retry\n/exit\n", fake.HTTPClient(), func(deps runtimeDeps) {
		runInteractive(t.Context(), deps, false, cfg, &ctxInfo)
	})

	if fake.requestCount() != 2 {
		t.Fatalf("LLM requests = %d, want 2", fake.requestCount())
	}
	for index, body := range fake.requestBodies() {
		if !strings.Contains(body, "User instruction:\\nbuild it") {
			t.Fatalf("request %d body = %q, want retried instruction", index+1, body)
		}
		if strings.Contains(body, "User instruction:\\n/retry") {
			t.Fatalf("request %d body = %q, want no /retry natural-language prompt", index+1, body)
		}
	}
	if !strings.Contains(output, "Retrying: build it") {
		t.Fatalf("output = %q, want retry status", output)
	}
}

// TestRunInteractiveNewCommandClearsPromptContext checks /new resets conversational memory.
func TestRunInteractiveNewCommandClearsPromptContext(t *testing.T) {
	fake := newLoopLLMClient(t,
		loopLLMResponse{content: `{"summary":"First answer.","commands":[]}`},
		loopLLMResponse{content: `{"summary":"Second answer.","commands":[]}`},
	)
	cfg := loopTestConfig(fake.URL())
	ctxInfo := loopTestContext(t)

	output := captureMainLoopIO(t, "first task\n/new\nsecond task\n/exit\n", fake.HTTPClient(), func(deps runtimeDeps) {
		runInteractive(t.Context(), deps, false, cfg, &ctxInfo)
	})

	bodies := fake.requestBodies()
	if len(bodies) != 2 {
		t.Fatalf("LLM requests = %d, want 2", len(bodies))
	}
	if !strings.Contains(bodies[0], "User instruction:\\nfirst task") {
		t.Fatalf("first request body = %q, want first task", bodies[0])
	}
	if !strings.Contains(bodies[1], "User instruction:\\nsecond task") {
		t.Fatalf("second request body = %q, want second task", bodies[1])
	}
	for _, snippet := range []string{"Recent session context:", "Session memory:", "first task", "First answer."} {
		if strings.Contains(bodies[1], snippet) {
			t.Fatalf("second request body contains cleared context %q: %q", snippet, bodies[1])
		}
	}
	if !strings.Contains(output, "─ new session · context cleared ") {
		t.Fatalf("output = %q, want new session separator", output)
	}
}

// TestRunInteractiveIgnoresEmptyPrompt checks Enter on an empty prompt does not start a turn.
func TestRunInteractiveIgnoresEmptyPrompt(t *testing.T) {
	fake := newLoopLLMClient(t)
	cfg := loopTestConfig(fake.URL())
	ctxInfo := loopTestContext(t)

	captureMainLoopIO(t, "\n/exit\n", fake.HTTPClient(), func(deps runtimeDeps) {
		runInteractive(t.Context(), deps, false, cfg, &ctxInfo)
	})

	if fake.requestCount() != 0 {
		t.Fatalf("LLM requests = %d, want 0", fake.requestCount())
	}
}

// TestDoLLMStreamReturnsMalformedChunkError checks corrupt SSE payloads are not skipped silently.
func TestDoLLMStreamReturnsMalformedChunkError(t *testing.T) {
	fake := newLoopLLMClient(t, loopLLMResponse{
		content: "data: {bad json}\n\n",
		stream:  true,
		raw:     true,
	})
	cfg := loopTestConfig(fake.URL())

	var output strings.Builder
	result, err := doLLMStream(t.Context(), fake.HTTPClient(), cfg, chatCompletionRequest{Model: cfg.Model}, &output)
	if err == nil {
		t.Fatalf("doLLMStream() error = nil, want malformed chunk error")
	}
	if !strings.Contains(err.Error(), "invalid llm stream chunk") {
		t.Fatalf("doLLMStream() error = %q, want invalid chunk message", err.Error())
	}
	if result != "" || output.String() != "" {
		t.Fatalf("doLLMStream() result/output = %q/%q, want both empty", result, output.String())
	}
}

// TestDoLLMStreamPropagatesContextCancellation checks streaming calls preserve cancellation identity.
func TestDoLLMStreamPropagatesContextCancellation(t *testing.T) {
	cfg := loopTestConfig("http://shellia.test")
	client := &http.Client{Transport: contextErrorTransport{}}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	var output strings.Builder
	_, err := doLLMStream(ctx, client, cfg, chatCompletionRequest{Model: cfg.Model}, &output)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("doLLMStream() error = %v, want context.Canceled", err)
	}
	if output.String() != "" {
		t.Fatalf("doLLMStream() output = %q, want empty output", output.String())
	}
}

// TestDoLLMStreamReportsErrorBodyReadFailure checks failed stream diagnostics include body read errors.
func TestDoLLMStreamReportsErrorBodyReadFailure(t *testing.T) {
	cfg := loopTestConfig("http://shellia.test")
	var output strings.Builder
	client := &http.Client{Transport: errorBodyTransport{}}
	_, err := doLLMStream(t.Context(), client, cfg, chatCompletionRequest{Model: cfg.Model}, &output)
	if err == nil {
		t.Fatalf("doLLMStream() error = nil, want HTTP error")
	}

	message := err.Error()
	if !strings.Contains(message, "llm request failed with status 400") {
		t.Fatalf("doLLMStream() error = %q, want status", message)
	}
	if !strings.Contains(message, "cannot read error response body") || !strings.Contains(message, "broken error body") {
		t.Fatalf("doLLMStream() error = %q, want body read failure", message)
	}
	var statusErr *llmHTTPStatusError
	if !errors.As(err, &statusErr) || statusErr.StatusCode != http.StatusBadRequest {
		t.Fatalf("doLLMStream() error = %T %[1]v, want llmHTTPStatusError status 400", err)
	}
}

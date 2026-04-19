package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
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
	stream bool
	body   string
}

type loopLLMClient struct {
	responses []loopLLMResponse
	mu        sync.Mutex
	requests  []loopLLMRequest
}

type errorBodyTransport struct{}

type errorReadCloser struct {
	err error
}

// newLoopLLMClient installs an OpenAI-compatible fake transport for main loop tests.
func newLoopLLMClient(t *testing.T, responses ...loopLLMResponse) *loopLLMClient {
	t.Helper()

	fake := &loopLLMClient{responses: responses}
	previousClient := httpClient
	httpClient = &http.Client{Transport: fake}
	t.Cleanup(func() {
		httpClient = previousClient
	})

	return fake
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
		return loopHTTPResponse(r, http.StatusBadRequest, "invalid request", nil), nil
	}

	fake.mu.Lock()
	index := len(fake.requests)
	fake.requests = append(fake.requests, loopLLMRequest{stream: request.Stream, body: string(body)})
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

// RoundTrip returns a failed response whose body cannot be read.
func (transport errorBodyTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusBadRequest,
		Header:     make(http.Header),
		Body:       errorReadCloser{err: errors.New("broken error body")},
		Request:    r,
	}, nil
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

// captureMainLoopIO replaces stdin/stdout with temporary files for deterministic loop tests.
func captureMainLoopIO(t *testing.T, input string, fn func()) string {
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

	previousStdin := os.Stdin
	previousStdout := os.Stdout
	os.Stdin = stdinFile
	os.Stdout = stdoutFile
	defer func() {
		os.Stdin = previousStdin
		os.Stdout = previousStdout
		stdinFile.Close()  //nolint:errcheck
		stdoutFile.Close() //nolint:errcheck
	}()

	fn()

	if _, err := stdoutFile.Seek(0, io.SeekStart); err != nil {
		t.Fatalf("Seek(stdout) error = %v", err)
	}
	output, err := io.ReadAll(stdoutFile)
	if err != nil {
		t.Fatalf("ReadAll(stdout) error = %v", err)
	}
	return string(output)
}

// TestRunTurnReturnsFinalAnswerWithoutCommands checks the answer-only path of the main turn loop.
func TestRunTurnReturnsFinalAnswerWithoutCommands(t *testing.T) {
	fake := newLoopLLMClient(t, loopLLMResponse{
		content: `{"summary":"No command needed.","commands":[]}`,
	})
	cfg := loopTestConfig(fake.URL())
	ctxInfo := loopTestContext(t)

	var result turnResult
	output := captureMainLoopIO(t, "", func() {
		var err error
		result, err = runTurn(context.Background(), false, cfg, &ctxInfo, "answer directly", nil, sessionState{})
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
	captureMainLoopIO(t, "", func() {
		var err error
		result, err = runTurn(context.Background(), false, cfg, &ctxInfo, "print marker", nil, sessionState{})
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

// TestRunInteractiveProcessesPromptThenExit checks that the interactive loop runs one AI turn and exits cleanly.
func TestRunInteractiveProcessesPromptThenExit(t *testing.T) {
	fake := newLoopLLMClient(t, loopLLMResponse{
		content: `{"summary":"Interactive answer.","commands":[]}`,
	})
	cfg := loopTestConfig(fake.URL())
	ctxInfo := loopTestContext(t)

	output := captureMainLoopIO(t, "answer something\n/exit\n", func() {
		runInteractive(context.Background(), false, cfg, &ctxInfo)
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

// TestRunInteractiveIgnoresEmptyPrompt checks Enter on an empty prompt does not start a turn.
func TestRunInteractiveIgnoresEmptyPrompt(t *testing.T) {
	fake := newLoopLLMClient(t)
	cfg := loopTestConfig(fake.URL())
	ctxInfo := loopTestContext(t)

	captureMainLoopIO(t, "\n/exit\n", func() {
		runInteractive(context.Background(), false, cfg, &ctxInfo)
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
	result, err := doLLMStream(context.Background(), cfg, chatCompletionRequest{Model: cfg.Model}, &output)
	if err == nil {
		t.Fatalf("doLLMStream() error = nil, want malformed chunk error")
	}
	if !strings.Contains(err.Error(), "invalid LLM stream chunk") {
		t.Fatalf("doLLMStream() error = %q, want invalid chunk message", err.Error())
	}
	if result != "" || output.String() != "" {
		t.Fatalf("doLLMStream() result/output = %q/%q, want both empty", result, output.String())
	}
}

// TestDoLLMStreamReportsErrorBodyReadFailure checks failed stream diagnostics include body read errors.
func TestDoLLMStreamReportsErrorBodyReadFailure(t *testing.T) {
	previousClient := httpClient
	httpClient = &http.Client{Transport: errorBodyTransport{}}
	t.Cleanup(func() {
		httpClient = previousClient
	})

	cfg := loopTestConfig("http://shellia.test")
	var output strings.Builder
	_, err := doLLMStream(context.Background(), cfg, chatCompletionRequest{Model: cfg.Model}, &output)
	if err == nil {
		t.Fatalf("doLLMStream() error = nil, want HTTP error")
	}

	message := err.Error()
	if !strings.Contains(message, "LLM request failed with status 400") {
		t.Fatalf("doLLMStream() error = %q, want status", message)
	}
	if !strings.Contains(message, "cannot read error response body") || !strings.Contains(message, "broken error body") {
		t.Fatalf("doLLMStream() error = %q, want body read failure", message)
	}
}

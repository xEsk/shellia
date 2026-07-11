package llm

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
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

func newLoopLLMClient(t *testing.T, responses ...loopLLMResponse) *loopLLMClient {
	t.Helper()
	return &loopLLMClient{responses: responses}
}

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

func (fake *loopLLMClient) HTTPClient() *http.Client {
	return &http.Client{Transport: fake}
}

func (transport errorBodyTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusBadRequest,
		Header:     make(http.Header),
		Body:       errorReadCloser{err: errors.New("broken error body")},
		Request:    r,
	}, nil
}

func (transport contextErrorTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	<-r.Context().Done()
	return nil, r.Context().Err()
}

func (body errorReadCloser) Read(p []byte) (int, error) {
	return 0, body.err
}

func (body errorReadCloser) Close() error {
	return nil
}

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

func (fake *loopLLMClient) URL() string {
	return "http://shellia.test"
}

func (fake *loopLLMClient) requestCount() int {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	return len(fake.requests)
}

func (fake *loopLLMClient) requestStreams() []bool {
	fake.mu.Lock()
	defer fake.mu.Unlock()

	streams := make([]bool, 0, len(fake.requests))
	for _, request := range fake.requests {
		streams = append(streams, request.stream)
	}
	return streams
}

func (fake *loopLLMClient) requestBodies() []string {
	fake.mu.Lock()
	defer fake.mu.Unlock()

	bodies := make([]string, 0, len(fake.requests))
	for _, request := range fake.requests {
		bodies = append(bodies, request.body)
	}
	return bodies
}

func (fake *loopLLMClient) requestAuthorizations() []string {
	fake.mu.Lock()
	defer fake.mu.Unlock()

	headers := make([]string, 0, len(fake.requests))
	for _, request := range fake.requests {
		headers = append(headers, request.authorization)
	}
	return headers
}

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

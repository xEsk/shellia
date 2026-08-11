package llm

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// ResponseMode controls how the model response document is decoded.
type ResponseMode string

const (
	// ResponseModeStrict requires exactly one JSON object and no surrounding text.
	ResponseModeStrict ResponseMode = "strict"
	// ResponseModeCompatible extracts the first complete JSON object from local-model output.
	ResponseModeCompatible ResponseMode = "compatible"
)

// parseResponse validates one JSON response returned by the model.
func parseResponse(raw string, mode ResponseMode) (Response, error) {
	var (
		parsed Response
		err    error
	)
	switch mode {
	case ResponseModeStrict:
		parsed, err = decodeStrictResponse(raw)
	case ResponseModeCompatible:
		parsed, err = decodeCompatibleResponse(raw)
	default:
		return Response{}, fmt.Errorf("invalid llm response mode %q", mode)
	}
	if err != nil {
		return Response{}, err
	}
	return validateResponse(parsed)
}

// decodeStrictResponse requires a single JSON object with no surrounding content.
func decodeStrictResponse(raw string) (Response, error) {
	if firstNonWhitespaceByte(raw) != '{' {
		return Response{}, fmt.Errorf("invalid llm response: expected one JSON object")
	}

	decoder := json.NewDecoder(strings.NewReader(raw))
	var parsed Response
	if err := decoder.Decode(&parsed); err != nil {
		return Response{}, fmt.Errorf("invalid llm response: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return Response{}, fmt.Errorf("invalid llm response: expected exactly one JSON object")
		}
		return Response{}, fmt.Errorf("invalid llm response: %w", err)
	}
	return parsed, nil
}

// decodeCompatibleResponse preserves the local-model JSON-object extraction behavior.
func decodeCompatibleResponse(raw string) (Response, error) {
	jsonObject, ok := firstJSONObject(raw)
	if !ok {
		return Response{}, fmt.Errorf("invalid llm response: no json object found")
	}

	var parsed Response
	if err := json.Unmarshal([]byte(jsonObject), &parsed); err != nil {
		return Response{}, fmt.Errorf("invalid llm response: %w", err)
	}
	return parsed, nil
}

// firstNonWhitespaceByte returns the first non-whitespace byte, if any.
func firstNonWhitespaceByte(raw string) byte {
	for index := range raw {
		switch raw[index] {
		case ' ', '\t', '\n', '\r':
			continue
		default:
			return raw[index]
		}
	}
	return 0
}

// validateResponse normalizes and validates the decoded model decision.
func validateResponse(parsed Response) (Response, error) {
	parsed.Action = strings.TrimSpace(strings.ToLower(parsed.Action))
	parsed.Operation = strings.TrimSpace(strings.ToLower(parsed.Operation))
	parsed.EvidenceSource = strings.TrimSpace(strings.ToLower(parsed.EvidenceSource))
	parsed.Freshness = strings.TrimSpace(strings.ToLower(parsed.Freshness))
	parsed.SuccessCriteria = strings.TrimSpace(parsed.SuccessCriteria)
	parsed.Summary = strings.TrimSpace(parsed.Summary)
	parsed.CompletionBasis.Source = strings.TrimSpace(strings.ToLower(parsed.CompletionBasis.Source))
	parsed.CompletionBasis.Freshness = strings.TrimSpace(strings.ToLower(parsed.CompletionBasis.Freshness))
	parsed.Offer.Objective = strings.TrimSpace(parsed.Offer.Objective)
	parsed.Offer.Summary = strings.TrimSpace(parsed.Offer.Summary)
	parsed.BlockerKind = strings.TrimSpace(strings.ToLower(parsed.BlockerKind))
	parsed.BlockerReason = strings.TrimSpace(parsed.BlockerReason)
	switch parsed.Operation {
	case "answer", "observe", "act", "capability":
	default:
		return Response{}, fmt.Errorf("invalid llm response: unknown operation %q", parsed.Operation)
	}
	switch parsed.EvidenceSource {
	case "model_knowledge", "session_result", "retry_observation", "current_observation", "current_execution":
	default:
		return Response{}, fmt.Errorf("invalid llm response: unknown evidence_source %q", parsed.EvidenceSource)
	}
	switch parsed.Freshness {
	case "not_applicable", "snapshot", "current":
	default:
		return Response{}, fmt.Errorf("invalid llm response: unknown freshness %q", parsed.Freshness)
	}
	if parsed.SuccessCriteria == "" {
		return Response{}, fmt.Errorf("invalid llm response: missing success_criteria")
	}
	if parsed.Operation != "capability" && (parsed.Offer.Objective != "" || parsed.Offer.Summary != "") {
		return Response{}, fmt.Errorf("invalid llm response: offer is only valid for capability")
	}
	if parsed.Offer.Objective == "" && parsed.Offer.Summary != "" {
		return Response{}, fmt.Errorf("invalid llm response: offer summary requires an objective")
	}
	for index := range parsed.Commands {
		cmd := &parsed.Commands[index]
		cmd.RepeatReason = strings.TrimSpace(strings.ToLower(cmd.RepeatReason))
		if strings.TrimSpace(cmd.Command) == "" {
			return Response{}, fmt.Errorf("invalid llm response: empty command")
		}
		if strings.TrimSpace(cmd.Purpose) == "" {
			return Response{}, fmt.Errorf("invalid llm response: missing purpose")
		}
		switch repeatReason(cmd.RepeatReason) {
		case "", repeatReasonUserRequested, repeatReasonRetry, repeatReasonVerifyAfterChange, repeatReasonPollChangedState:
		default:
			return Response{}, fmt.Errorf("invalid llm response: unknown repeat_reason %q", cmd.RepeatReason)
		}
	}
	refs := make(map[string]struct{}, len(parsed.ContextRefs))
	for index := range parsed.ContextRefs {
		ref := strings.TrimSpace(strings.ToLower(parsed.ContextRefs[index]))
		if ref == "" {
			return Response{}, fmt.Errorf("invalid llm response: empty context reference")
		}
		if _, exists := refs[ref]; exists {
			return Response{}, fmt.Errorf("invalid llm response: duplicate context reference %q", ref)
		}
		refs[ref] = struct{}{}
		parsed.ContextRefs[index] = ref
	}
	switch parsed.Action {
	case "execute":
		if parsed.Operation == "capability" || parsed.Operation == "answer" {
			return Response{}, fmt.Errorf("invalid llm response: operation %q cannot execute", parsed.Operation)
		}
		if parsed.Summary == "" {
			return Response{}, fmt.Errorf("invalid llm response: execute decision missing summary")
		}
		if len(parsed.Commands) == 0 {
			return Response{}, fmt.Errorf("invalid llm response: execute decision missing commands")
		}
	case "retrieve_context":
		if len(parsed.ContextRefs) == 0 {
			return Response{}, fmt.Errorf("invalid llm response: retrieve_context decision missing context_refs")
		}
		if len(parsed.Commands) > 0 {
			return Response{}, fmt.Errorf("invalid llm response: retrieve_context decision with commands")
		}
	case "complete":
		if parsed.Summary == "" {
			return Response{}, fmt.Errorf("invalid llm response: complete decision missing final answer")
		}
		if parsed.CompletionBasis.Source == "" || parsed.CompletionBasis.Freshness == "" {
			return Response{}, fmt.Errorf("invalid llm response: complete decision missing completion basis")
		}
		if parsed.CompletionBasis.Source != parsed.EvidenceSource || parsed.CompletionBasis.Freshness != parsed.Freshness {
			return Response{}, fmt.Errorf("invalid llm response: completion basis must match evidence_source and freshness")
		}
		if len(parsed.Commands) > 0 {
			return Response{}, fmt.Errorf("invalid llm response: complete decision with commands")
		}
		if len(parsed.ContextRefs) > 0 && parsed.EvidenceSource != "session_result" {
			return Response{}, fmt.Errorf("invalid llm response: context_refs require session_result evidence")
		}
	case "blocked":
		if len(parsed.Commands) > 0 {
			return Response{}, fmt.Errorf("invalid llm response: blocked decision with commands")
		}
		if parsed.BlockerKind == "" || parsed.BlockerReason == "" {
			return Response{}, fmt.Errorf("invalid llm response: blocked decision missing blocker")
		}
		switch parsed.BlockerKind {
		case "missing_input", "unavailable", "unsafe_to_continue":
		default:
			return Response{}, fmt.Errorf("invalid llm response: unknown blocker_kind %q", parsed.BlockerKind)
		}
	default:
		return Response{}, fmt.Errorf("invalid llm response: unknown action %q", parsed.Action)
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

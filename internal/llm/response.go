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

// ResponseValidationError reports a decoded response that violated Shellia's
// structural decision contract while preserving its recognized operation.
type ResponseValidationError struct {
	Response Response
	Err      error
}

// Error returns the underlying decision-contract validation failure.
func (err *ResponseValidationError) Error() string {
	if err == nil || err.Err == nil {
		return ""
	}
	return err.Err.Error()
}

// Unwrap exposes the underlying decision-contract validation failure.
func (err *ResponseValidationError) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.Err
}

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
	decoded := parsed
	validated, err := validateResponse(parsed)
	if err != nil {
		decoded.Operation = strings.TrimSpace(strings.ToLower(decoded.Operation))
		return decoded, &ResponseValidationError{Response: decoded, Err: err}
	}
	return validated, nil
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
	parsed.SuccessCriteria = strings.TrimSpace(parsed.SuccessCriteria)
	parsed.Summary = strings.TrimSpace(parsed.Summary)
	parsed.Offer.Mode = strings.TrimSpace(strings.ToLower(parsed.Offer.Mode))
	parsed.Offer.Objective = strings.TrimSpace(parsed.Offer.Objective)
	parsed.Offer.Summary = strings.TrimSpace(parsed.Offer.Summary)
	parsed.BlockerKind = strings.TrimSpace(strings.ToLower(parsed.BlockerKind))
	parsed.BlockerReason = strings.TrimSpace(parsed.BlockerReason)
	switch parsed.Operation {
	case "answer", "observe", "act", "capability":
	default:
		return Response{}, fmt.Errorf("invalid llm response: unknown operation %q", parsed.Operation)
	}
	if parsed.SuccessCriteria == "" {
		return Response{}, fmt.Errorf("invalid llm response: missing success_criteria")
	}
	if err := validateOffer(parsed); err != nil {
		return Response{}, err
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
	if parsed.Action != "retrieve_context" && len(parsed.ContextRefs) > 0 {
		return Response{}, fmt.Errorf("invalid llm response: context_refs are only valid for retrieve_context")
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
	case "plan":
		if parsed.Operation != "act" && parsed.Operation != "observe" {
			return Response{}, fmt.Errorf("invalid llm response: plan requires operation act or observe")
		}
		if parsed.Summary == "" || len(parsed.Commands) == 0 {
			return Response{}, fmt.Errorf("invalid llm response: plan decision missing summary or commands")
		}
		if parsed.BlockerKind != "" || parsed.BlockerReason != "" {
			return Response{}, fmt.Errorf("invalid llm response: plan decision with blocker")
		}
	case "retrieve_context":
		if parsed.Operation != "answer" {
			return Response{}, fmt.Errorf("invalid llm response: retrieve_context requires operation answer")
		}
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
		if len(parsed.Commands) > 0 {
			return Response{}, fmt.Errorf("invalid llm response: complete decision with commands")
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

// validateOffer enforces the closed action, operation, and proposal-mode matrix.
func validateOffer(parsed Response) error {
	hasOffer := parsed.Offer.Mode != "" || parsed.Offer.Objective != "" || parsed.Offer.Summary != ""
	if !hasOffer {
		if parsed.Action == "plan" {
			return fmt.Errorf("invalid llm response: plan decision requires an execute offer")
		}
		return nil
	}
	if parsed.Offer.Mode == "" || parsed.Offer.Objective == "" || parsed.Offer.Summary == "" {
		return fmt.Errorf("invalid llm response: offer requires mode, objective, and summary")
	}

	wantMode := ""
	switch {
	case parsed.Action == "complete" && parsed.Operation == "answer":
		wantMode = "plan"
	case parsed.Action == "complete" && parsed.Operation == "capability":
		wantMode = "execute"
	case parsed.Action == "plan" && (parsed.Operation == "act" || parsed.Operation == "observe"):
		wantMode = "execute"
	}
	if parsed.Offer.Mode != wantMode {
		return fmt.Errorf("invalid llm response: offer mode %q is not allowed for operation=%q action=%q", parsed.Offer.Mode, parsed.Operation, parsed.Action)
	}
	return nil
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

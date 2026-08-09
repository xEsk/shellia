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
	parsed.ObjectiveMode = strings.TrimSpace(strings.ToLower(parsed.ObjectiveMode))
	parsed.SuccessCriteria = strings.TrimSpace(parsed.SuccessCriteria)
	parsed.Summary = strings.TrimSpace(parsed.Summary)
	parsed.CompletionBasis.Type = strings.TrimSpace(strings.ToLower(parsed.CompletionBasis.Type))
	parsed.Offer.Objective = strings.TrimSpace(parsed.Offer.Objective)
	parsed.Offer.Summary = strings.TrimSpace(parsed.Offer.Summary)
	parsed.BlockerKind = strings.TrimSpace(strings.ToLower(parsed.BlockerKind))
	parsed.BlockerReason = strings.TrimSpace(parsed.BlockerReason)
	switch parsed.ObjectiveMode {
	case "act", "observe", "capability", "explain":
	default:
		return Response{}, fmt.Errorf("invalid llm response: unknown objective_mode %q", parsed.ObjectiveMode)
	}
	if parsed.SuccessCriteria == "" {
		return Response{}, fmt.Errorf("invalid llm response: missing success_criteria")
	}
	if parsed.ObjectiveMode != "capability" && (parsed.Offer.Objective != "" || parsed.Offer.Summary != "") {
		return Response{}, fmt.Errorf("invalid llm response: offer is only valid for capability")
	}
	if parsed.ObjectiveMode == "capability" && parsed.Action != "complete" {
		return Response{}, fmt.Errorf("invalid llm response: capability decision must complete the capability question")
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
	switch parsed.Action {
	case "execute":
		if parsed.ObjectiveMode == "capability" || parsed.ObjectiveMode == "explain" {
			return Response{}, fmt.Errorf("invalid llm response: objective_mode %q cannot execute", parsed.ObjectiveMode)
		}
		if parsed.Summary == "" {
			return Response{}, fmt.Errorf("invalid llm response: execute decision missing summary")
		}
		if len(parsed.Commands) == 0 {
			return Response{}, fmt.Errorf("invalid llm response: execute decision missing commands")
		}
	case "complete":
		if parsed.Summary == "" {
			return Response{}, fmt.Errorf("invalid llm response: complete decision missing final answer")
		}
		if parsed.CompletionBasis.Type == "" {
			return Response{}, fmt.Errorf("invalid llm response: complete decision missing completion basis")
		}
		switch parsed.CompletionBasis.Type {
		case "model_knowledge", "current_observation", "current_execution", "prior_session_evidence":
		default:
			return Response{}, fmt.Errorf("invalid llm response: unknown completion basis %q", parsed.CompletionBasis.Type)
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

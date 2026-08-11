package llm

import (
	"fmt"
	"math"
	"sort"
)

var protectedRequestParams = map[string]struct{}{
	"model":               {},
	"messages":            {},
	"response_format":     {},
	"stream":              {},
	"stream_options":      {},
	"n":                   {},
	"tools":               {},
	"tool_choice":         {},
	"parallel_tool_calls": {},
	"functions":           {},
	"function_call":       {},
	"modalities":          {},
	"audio":               {},
	"web_search_options":  {},
}

// validateRequestParams checks provider body fields do not override Shellia's wire contract.
func validateRequestParams(params map[string]any) error {
	for _, key := range sortedRequestParamKeys(params) {
		if _, protected := protectedRequestParams[key]; protected {
			return fmt.Errorf("request_params.%s is reserved by Shellia", key)
		}
		if err := validateRequestParamValue("request_params."+key, params[key]); err != nil {
			return err
		}
	}
	return nil
}

// validateRequestParamValue checks one provider field can be represented by the supported JSON value domain.
func validateRequestParamValue(path string, value any) error {
	if value == nil {
		return fmt.Errorf("%s is not JSON-compatible", path)
	}

	switch typed := value.(type) {
	case string, bool,
		int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64:
		return nil
	case float32:
		if math.IsNaN(float64(typed)) || math.IsInf(float64(typed), 0) {
			return fmt.Errorf("%s is not JSON-compatible", path)
		}
		return nil
	case float64:
		if math.IsNaN(typed) || math.IsInf(typed, 0) {
			return fmt.Errorf("%s is not JSON-compatible", path)
		}
		return nil
	case []any:
		if typed == nil {
			return fmt.Errorf("%s is not JSON-compatible", path)
		}
		for index, item := range typed {
			if err := validateRequestParamValue(fmt.Sprintf("%s[%d]", path, index), item); err != nil {
				return err
			}
		}
		return nil
	case []map[string]any:
		if typed == nil {
			return fmt.Errorf("%s is not JSON-compatible", path)
		}
		for index, item := range typed {
			if err := validateRequestParamValue(fmt.Sprintf("%s[%d]", path, index), item); err != nil {
				return err
			}
		}
		return nil
	case map[string]any:
		if typed == nil {
			return fmt.Errorf("%s is not JSON-compatible", path)
		}
		for _, key := range sortedRequestParamKeys(typed) {
			if err := validateRequestParamValue(path+"."+key, typed[key]); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("%s is not JSON-compatible", path)
	}
}

// sortedRequestParamKeys returns deterministic validation order for provider fields.
func sortedRequestParamKeys(params map[string]any) []string {
	keys := make([]string, 0, len(params))
	for key := range params {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

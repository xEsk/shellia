package config

import "strings"

// PromptIdentity selects the label used for the person interacting with Shellia.
type PromptIdentity string

const (
	PromptIdentityUser PromptIdentity = "user"
	PromptIdentityYou  PromptIdentity = "you"
)

func normalizePromptIdentity(value string, fallback PromptIdentity) PromptIdentity {
	switch PromptIdentity(strings.ToLower(strings.TrimSpace(value))) {
	case PromptIdentityUser:
		return PromptIdentityUser
	case PromptIdentityYou:
		return PromptIdentityYou
	default:
		return fallback
	}
}

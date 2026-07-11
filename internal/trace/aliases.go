package trace

import (
	configpkg "shellia/internal/config"
	"shellia/internal/core"
)

type (
	config           = configpkg.Config
	contextInfo      = core.ContextInfo
	capturedStream   = core.CapturedStream
	commandExecution = core.CommandExecution
)

var defaultConfig = configpkg.DefaultConfig

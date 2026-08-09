package trace

import (
	configpkg "github.com/xEsk/shellia/internal/config"
	"github.com/xEsk/shellia/internal/core"
)

type (
	config           = configpkg.Config
	contextInfo      = core.ContextInfo
	capturedStream   = core.CapturedStream
	commandExecution = core.CommandExecution
)

var defaultConfig = configpkg.DefaultConfig

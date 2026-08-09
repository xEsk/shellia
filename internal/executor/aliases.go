package executor

import (
	configpkg "github.com/xEsk/shellia/internal/config"
	"github.com/xEsk/shellia/internal/core"
)

type (
	config           = configpkg.Config
	contextInfo      = core.ContextInfo
	commandPlan      = core.CommandPlan
	commandExecution = core.CommandExecution
)

package agent

import (
	"github.com/zhubert/plural-core/config"

	"github.com/zhubert/plural-agent/internal/agentconfig"
)

// Compile-time interface satisfaction check.
var _ agentconfig.Config = (*config.Config)(nil)

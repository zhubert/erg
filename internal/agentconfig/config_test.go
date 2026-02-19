package agentconfig

import "github.com/zhubert/plural-core/config"

// Compile-time interface satisfaction check.
var _ Config = (*config.Config)(nil)

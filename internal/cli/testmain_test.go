package cli

import (
	"os"
	"testing"

	"github.com/zhubert/erg/internal/logger"
)

func TestMain(m *testing.M) {
	// Disable logging during tests to avoid polluting /tmp/erg-debug.log
	logger.Reset()
	logger.Init(os.DevNull)

	code := m.Run()

	logger.Reset()
	os.Exit(code)
}

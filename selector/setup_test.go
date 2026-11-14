package selector

import (
	"os"
	"testing"

	corelogger "github.com/dolfly/core/logger"
	xlogger "github.com/dolfly/x/logger"
)

func TestMain(m *testing.M) {
	corelogger.SetDefault(xlogger.Nop())
	os.Exit(m.Run())
}

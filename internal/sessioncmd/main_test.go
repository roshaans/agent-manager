package sessioncmd

import (
	"os"
	"testing"

	"github.com/YoanWai/agent-manager/internal/testenv"
)

func TestMain(m *testing.M) {
	testenv.UnsignCommits()
	os.Exit(m.Run())
}

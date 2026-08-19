package git

import (
	"github.com/YoanWai/agent-manager/internal/testenv"
	"os"
	"testing"
)

// TestMain stops the machine's own git config from signing the commits these
// tests make. A signing agent that has locked asks for a passphrase, and a
// prompt nobody is there to answer hangs the run instead of failing it.
func TestMain(m *testing.M) {
	testenv.UnsignCommits()
	os.Exit(m.Run())
}

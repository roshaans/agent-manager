package mcpserver

import (
	"os"
	"testing"
)

// TestMain stops the machine's own git config from signing the commits these
// tests make. A signing agent that has locked asks for a passphrase, and a
// prompt nobody is there to answer hangs the run rather than failing it.
func TestMain(m *testing.M) {
	os.Setenv("GIT_CONFIG_COUNT", "1")
	os.Setenv("GIT_CONFIG_KEY_0", "commit.gpgsign")
	os.Setenv("GIT_CONFIG_VALUE_0", "false")
	os.Exit(m.Run())
}

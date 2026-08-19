// Package testenv holds the environment a test run needs before it touches
// the tools this project shells out to.
package testenv

import "os"

// UnsignCommits stops the machine's own git config from signing the commits a
// test makes, for every git the run reaches — the helpers in the test and the
// ones the code under test spawns.
//
// A developer who signs their commits usually does it through an agent, and a
// locked agent asks for a passphrase. A test run has no terminal to ask at, so
// the commit does not fail: it waits, one minute per commit, until the whole
// suite times out. Call this from TestMain in any package whose tests commit.
func UnsignCommits() {
	os.Setenv("GIT_CONFIG_COUNT", "1")
	os.Setenv("GIT_CONFIG_KEY_0", "commit.gpgsign")
	os.Setenv("GIT_CONFIG_VALUE_0", "false")
}

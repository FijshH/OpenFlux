package avito

import (
	"os"
	"testing"
)

// The tests must not read, or write, the pause a real process on this machine shares.
func TestMain(m *testing.M) {
	os.Setenv("OPENFLUX_AVITO_GATE_FILE", "off")
	os.Exit(m.Run())
}

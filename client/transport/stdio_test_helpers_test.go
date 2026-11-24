package transport

import (
	"os"
	"testing"
)

func TestTransportCommandHelperProcess(t *testing.T) {}

func testTransportCommand(t *testing.T) (string, []string) {
	t.Helper()

	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return executable, []string{"-test.run=^TestTransportCommandHelperProcess$"}
}

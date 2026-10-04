package procutil

import (
	"os"
	"testing"
)

func TestResidentMemoryReadsRunningProcess(t *testing.T) {
	if got := ResidentMemory(os.Getpid()); got == 0 {
		t.Fatal("resident memory = 0 for the running test process")
	}
}

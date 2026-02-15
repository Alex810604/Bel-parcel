package main

import (
	"os"
	"testing"
)

func TestMainSkipEnv(t *testing.T) {
	os.Setenv("BP_CMD_TEST", "1")
	defer os.Unsetenv("BP_CMD_TEST")
	main()
}

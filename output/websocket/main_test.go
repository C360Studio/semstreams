package websocket

import (
	"fmt"
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	if os.Getenv("INTEGRATION_TESTS") == "" {
		fmt.Println("Skipping integration tests. Set INTEGRATION_TESTS=1 to run.")
		return
	}
	os.Exit(m.Run())
}

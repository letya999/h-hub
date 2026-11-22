package broken

import (
	"testing"
	"time"
)

func TestLazy(t *testing.T) {
	t.Skip("not today")
	time.Sleep(time.Second)
}

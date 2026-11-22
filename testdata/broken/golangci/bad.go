package broken

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"time"
)

func violations() {
	_ = os.Getenv("HOME")
	_, _ = os.LookupEnv("USER")
	_ = exec.Command("ls", "-la")
	_, _ = http.Get("http://example.com")
	_ = http.DefaultClient
	fmt.Println("direct print")
	time.Sleep(time.Second)
	panic("boom")
}

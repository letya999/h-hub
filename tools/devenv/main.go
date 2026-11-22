// Command devenv checks the Docker disk budget of the dev machine
// (v0 pain: unbounded build cache eating the WSL disk). It is a dev
// tool, not a product component and not a second check system.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// Build-cache ceiling; other sections are reported for visibility.
const buildCacheLimit = 10 << 30 // 10 GiB

type row struct {
	Type  string `json:"Type"`
	Total string `json:"TotalCount"`
	Size  string `json:"Size"`
}

func main() {
	out, err := exec.Command("docker", "system", "df", "--format", "{{json .}}").Output()
	if err != nil {
		fmt.Fprintln(os.Stderr, "devenv: docker system df failed:", err)
		os.Exit(1)
	}
	code := 0
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		var r row
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			continue
		}
		size := parseSize(r.Size)
		fmt.Fprintf(os.Stdout, "%-12s %-6s %s\n", r.Type, r.Total, r.Size) //nolint:errcheck // report is best effort
		if r.Type == "Build Cache" && size > buildCacheLimit {
			fmt.Fprintf(os.Stderr, "devenv: build cache %s exceeds 10 GiB; run: docker builder prune\n", r.Size)
			code = 1
		}
	}
	os.Exit(code)
}

var units = []struct {
	suffix string
	mult   float64
}{
	{"TB", 1 << 40}, {"GB", 1 << 30}, {"MB", 1 << 20}, {"kB", 1 << 10}, {"B", 1},
}

func parseSize(s string) float64 {
	s = strings.TrimSpace(s)
	for _, u := range units {
		if strings.HasSuffix(s, u.suffix) {
			f, err := strconv.ParseFloat(strings.TrimSuffix(s, u.suffix), 64)
			if err != nil {
				return 0
			}
			return f * u.mult
		}
	}
	return 0
}

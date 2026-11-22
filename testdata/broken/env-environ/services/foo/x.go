package foo

import "os"

func read() []string { return os.Environ() }

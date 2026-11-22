package foo

import "os"

var dsn = os.Getenv("HHUB_UNDECLARED")

# Third-party software

h-hub is licensed under AGPL-3.0-only. The Go modules below retain their
own licenses and copyright notices. Versions are pinned by `go.mod` and
`go.sum`; the upstream license files are in the downloaded module source.

| Go module | License |
| --- | --- |
| `github.com/jackc/pgx/v5` | MIT |
| `github.com/jackc/pgpassfile` | MIT |
| `github.com/jackc/pgservicefile` | MIT |
| `github.com/jackc/puddle/v2` | MIT |
| `github.com/kr/text` | MIT |
| `github.com/rogpeppe/go-internal` | BSD-3-Clause |
| `golang.org/x/sync` | BSD-3-Clause |
| `golang.org/x/text` | BSD-3-Clause |
| `gopkg.in/yaml.v3` | MIT and Apache-2.0 (different files) |

The following modules are present in the dependency graph for tests or
their tooling:

| Go module | License |
| --- | --- |
| `github.com/creack/pty` | MIT |
| `github.com/davecgh/go-spew` | ISC |
| `github.com/kr/pretty` | MIT |
| `github.com/kr/pty` | MIT |
| `github.com/pmezard/go-difflib` | BSD-3-Clause |
| `github.com/stretchr/objx` | MIT |
| `github.com/stretchr/testify` | MIT |
| `golang.org/x/mod` | BSD-3-Clause |
| `golang.org/x/tools` | BSD-3-Clause |
| `gopkg.in/check.v1` | BSD-3-Clause |
| `gopkg.in/errgo.v2` | BSD-3-Clause |

The container base images, PostgreSQL, and build/check tools are obtained
from their upstream projects by the pinned build and CI definitions. Their
licenses remain with those projects; they are not copied into this source
tree.

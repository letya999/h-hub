// Package secret implements the operator's thin credential writer:
// `hubctl secret set <name>` reads the value from stdin or
// --from-file, never argv or env, and posts it to credential-broker.
package secret

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/letya999/h-hub/cmd/hubctl/operator"
	credv1 "github.com/letya999/h-hub/contracts/cred/v1"
	"github.com/letya999/h-hub/shared/httpapi"
)

const brokerURL = "http://credential-broker:9000"
const operatorKey = "/etc/hhub/keys/operator.key"

// Run dispatches the secret subcommands. in/out are injectable for
// tests; stdin is the real stream in production.
func Run(ctx context.Context, args []string, in io.Reader, out io.Writer) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: hubctl secret <set|list|snapshot>")
	}
	switch args[0] {
	case "set":
		if len(args) < 2 {
			return fmt.Errorf("usage: hubctl secret set <name> [--from-file PATH]")
		}
		return set(ctx, args[1:], in, out)
	case "list":
		return list(ctx, out)
	case "snapshot":
		return snapshot(ctx, out)
	default:
		return fmt.Errorf("usage: hubctl secret <set|list|snapshot>")
	}
}

// list prints the registered connections — name, type, ref, newest
// version — never any value. Backup stores this listing so a restore
// drill can diff it against what the snapshot answers.
func list(ctx context.Context, out io.Writer) error {
	tok, err := operator.Token("", "", "ListConnections", "credential-broker", operatorKey)
	if err != nil {
		return fmt.Errorf("secret token: %w", err)
	}
	c := &credv1.Client{T: credv1.HTTP{C: httpapi.NewClient(brokerURL, tok)}}
	res, err := c.ListConnections(ctx, credv1.ListConnectionsReq{})
	if err != nil {
		return fmt.Errorf("broker list: %w", err)
	}
	for _, conn := range res.Connections {
		fmt.Fprintf(out, "%s %s %s %d\n", conn.Name, conn.Type, conn.Ref, conn.Version) //nolint:errcheck // report is best effort
	}
	return nil
}

// snapshot asks the broker to VACUUM INTO a consistent encrypted copy
// inside its own volume; the backup job copies the sealed file out.
func snapshot(ctx context.Context, out io.Writer) error {
	tok, err := operator.Token("", "", "Snapshot", "credential-broker", operatorKey)
	if err != nil {
		return fmt.Errorf("secret token: %w", err)
	}
	c := &credv1.Client{T: credv1.HTTP{C: httpapi.NewClient(brokerURL, tok)}}
	res, err := c.Snapshot(ctx, credv1.SnapshotReq{})
	if err != nil {
		return fmt.Errorf("broker snapshot: %w", err)
	}
	fmt.Fprintf(out, "%s\n", res.Path) //nolint:errcheck // report is best effort
	return nil
}

func set(ctx context.Context, args []string, in io.Reader, out io.Writer) error {
	name := args[0]
	var value []byte
	var err error
	if len(args) == 1 {
		value, err = io.ReadAll(io.LimitReader(in, httpapi.MaxBody))
	} else if len(args) == 3 && args[1] == "--from-file" {
		value, err = readFile(args[2])
	} else {
		return fmt.Errorf("usage: hubctl secret set <name> [--from-file PATH]")
	}
	if err != nil {
		return fmt.Errorf("read value: %w", err)
	}
	v := strings.TrimRight(string(value), "\r\n")
	if v == "" {
		return fmt.Errorf("empty secret value")
	}
	tok, err := operator.Token("", "", "SetSecret", "credential-broker", operatorKey)
	if err != nil {
		return fmt.Errorf("secret token: %w", err)
	}
	c := &credv1.Client{T: credv1.HTTP{C: httpapi.NewClient(brokerURL, tok)}}
	res, err := c.SetSecret(ctx, credv1.SetSecretReq{Name: name, Value: v})
	if err != nil {
		return fmt.Errorf("broker set: %w", err)
	}
	fmt.Fprintf(out, "%s version %d\n", res.Ref, res.Version) //nolint:errcheck // report is best effort
	return nil
}

func readFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close() //nolint:errcheck // best-effort close
	b, err := io.ReadAll(io.LimitReader(f, httpapi.MaxBody))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return b, nil
}

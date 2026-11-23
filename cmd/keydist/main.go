// keydist distributes install-time public keys and service keys into
// per-service volumes: every service gets public keys, most get service.key,
// key under pubs/, and the descriptor edge table rendered once for
// all. Run once after `hubctl init` and before services start.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/letya999/h-hub/internal/descriptor"
	"github.com/letya999/h-hub/shared/edges"
)

func main() {
	conf := flag.String("conf", "", "install config dir")
	keyRoot := flag.String("keys", "", "key volume root")
	desc := flag.String("descriptor", "", "descriptor.yaml path")
	flag.Parse()
	if *conf == "" || *keyRoot == "" || *desc == "" {
		fmt.Fprintln(os.Stderr, "usage: keydist --conf DIR --keys DIR --descriptor FILE")
		os.Exit(2)
	}
	if err := run(*conf, *keyRoot, *desc); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(conf, keyRoot, descPath string) error {
	f, err := descriptor.Load(descPath)
	if err != nil {
		return fmt.Errorf("keydist: %w", err)
	}
	rendered := renderEdges(f)
	for _, s := range f.Services {
		dst := keyVolume(keyRoot, s)
		if dst == "" {
			continue
		}
		if err := distribute(conf, dst, s.Name, f, rendered); err != nil {
			return err
		}
	}
	return nil
}

// keyVolume resolves a service's keys-* mount target name.
func keyVolume(root string, s descriptor.Service) string {
	for _, m := range s.Volumes {
		if strings.HasPrefix(m.Name, "keys-") {
			return filepath.Join(root, m.Name)
		}
	}
	return ""
}

// distribute writes one service's material: service.key, pubs/*, edges.
func distribute(conf, dst, name string, f *descriptor.File, rendered string) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return fmt.Errorf("keydist: destination: %w", err)
	}
	if err := distributePrivateKey(conf, dst, name); err != nil {
		return err
	}
	pubs := filepath.Join(dst, "pubs")
	if err := os.MkdirAll(pubs, 0o755); err != nil {
		return fmt.Errorf("keydist: pubs: %w", err)
	}
	if err := distributePublicKeys(conf, pubs, f); err != nil {
		return err
	}
	if operatorPubRecipients[name] {
		if err := copyFile(filepath.Join(conf, "keys", "operator.pub"),
			filepath.Join(pubs, "operator.pub"), 0o644); err != nil {
			return err
		}
	}
	if err := os.WriteFile(filepath.Join(dst, "edges"), []byte(rendered), 0o644); err != nil {
		return fmt.Errorf("keydist: edges: %w", err)
	}
	return nil
}

func distributePrivateKey(conf, dst, name string) error {
	if name == "identity-hub" {
		err := os.Remove(filepath.Join(dst, "service.key"))
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("keydist: remove issuer key: %w", err)
		}
		return nil
	}
	return copyFile(filepath.Join(conf, "services", name, "service.key"), filepath.Join(dst, "service.key"), 0o600)
}

func distributePublicKeys(conf, pubs string, f *descriptor.File) error {
	for _, s := range f.Services {
		pub := filepath.Join(conf, "services", s.Name, "service.pub")
		if _, err := os.Stat(pub); errors.Is(err, fs.ErrNotExist) {
			continue // services without keys (space zone) contribute none
		}
		if err := copyFile(pub, filepath.Join(pubs, s.Name+".pub"), 0o644); err != nil {
			return err
		}
	}
	return nil
}

var operatorPubRecipients = map[string]bool{"identity-hub": true, "channel-hub": true}

// renderEdges flattens all declared pairs to the canonical env form.
func renderEdges(f *descriptor.File) string {
	t := edges.Table{}
	for _, p := range f.EdgePairs() {
		if t[p.To] == nil {
			t[p.To] = map[string]map[string]bool{}
		}
		if t[p.To][p.From] == nil {
			t[p.To][p.From] = map[string]bool{}
		}
		t[p.To][p.From][p.Op] = true
	}
	return t.Render()
}

func copyFile(src, dst string, perm fs.FileMode) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("keydist: %w", err)
	}
	if err := os.WriteFile(dst, data, perm); err != nil {
		return fmt.Errorf("keydist: %w", err)
	}
	return nil
}

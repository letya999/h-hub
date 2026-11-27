package v1

import (
	"fmt"
	"os"
	"path"
	"strconv"
)

const (
	SocketRoot   = "/run/hhub-worker-slots"
	WorkerSocket = "/run/hhub-worker/connect"
	SocketName   = "connect"
)

func SlotDirectory(slot int) string { return strconv.Itoa(slot) }

func SocketPath(slot int) string {
	return path.Join(SocketRoot, SlotDirectory(slot), SocketName)
}

func SocketVolume(installation string) string { return "hhub_" + installation + "-worker-sockets" }

// PrepareSlots creates the private mount directories before tool-hub admits work.
func PrepareSlots(maxTotal int) error {
	root, err := os.OpenRoot(SocketRoot)
	if err != nil {
		return fmt.Errorf("open worker socket volume: %w", err)
	}
	defer root.Close() //nolint:errcheck // best-effort close
	for slot := 1; slot <= maxTotal; slot++ {
		dir := SlotDirectory(slot)
		if err := root.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("prepare worker socket slot %d: %w", slot, err)
		}
		if err := root.Chmod(dir, 0o777); err != nil {
			return fmt.Errorf("set worker socket slot %d permissions: %w", slot, err)
		}
	}
	return nil
}

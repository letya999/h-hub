//go:build linux

package v1

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net"
	"os"
	"syscall"
)

// ListenWorkerSocket creates the worker's private descriptor handoff socket.
func ListenWorkerSocket(path string) (*net.UnixListener, error) {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("clear worker socket: %w", err)
	}
	listener, err := net.ListenUnix("unixpacket", &net.UnixAddr{Name: path, Net: "unixpacket"})
	if err != nil {
		return nil, fmt.Errorf("open worker socket: %w", err)
	}
	if err := os.Chmod(path, 0o666); err != nil {
		return nil, errors.Join(fmt.Errorf("set worker socket permissions: %w", err), listener.Close())
	}
	return listener, nil
}

// ServeWorkerSocket accepts connection challenges until the worker shuts down.
func ServeWorkerSocket(ctx context.Context, listener *net.UnixListener, token string, report func(error)) error {
	stop := context.AfterFunc(ctx, func() {
		listener.Close() //nolint:errcheck // unblock accept on shutdown
	})
	defer stop()
	for {
		conn, err := listener.AcceptUnix()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("accept worker socket: %w", err)
		}
		if err := sendWorkerConn(ctx, conn, token); err != nil {
			report(err)
		}
	}
}

func sendWorkerConn(ctx context.Context, unix *net.UnixConn, token string) error {
	defer unix.Close() //nolint:errcheck // best-effort close
	ctx, cancel := context.WithTimeout(ctx, connectionTimeout)
	defer cancel()
	at, _ := ctx.Deadline()
	if err := unix.SetDeadline(at); err != nil {
		return fmt.Errorf("bound worker socket write: %w", err)
	}
	var challenge [sha256.Size]byte
	n, err := unix.Read(challenge[:])
	if err != nil || n != len(challenge) {
		return errors.Join(errors.New("invalid worker socket challenge"), err)
	}
	tcp, err := (&net.Dialer{}).DialContext(ctx, "tcp4", Loopback)
	if err != nil {
		return fmt.Errorf("open worker loopback: %w", err)
	}
	defer tcp.Close() //nolint:errcheck // best-effort close
	return transferWorkerConn(unix, tcp, connectionProof(token, challenge[:]))
}

func transferWorkerConn(unix *net.UnixConn, tcp net.Conn, proof []byte) error {
	peer, ok := tcp.(*net.TCPConn)
	if !ok {
		return errors.New("worker loopback did not create a TCP connection")
	}
	file, err := peer.File()
	if err != nil {
		return fmt.Errorf("duplicate worker loopback: %w", err)
	}
	defer file.Close() //nolint:errcheck // best-effort close
	if _, _, err := unix.WriteMsgUnix(proof, syscall.UnixRights(int(file.Fd())), nil); err != nil {
		return fmt.Errorf("send worker TCP connection: %w", err)
	}
	return nil
}

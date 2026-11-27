//go:build linux

package v1

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"syscall"
	"time"

	"github.com/letya999/h-hub/shared/httpapi"
)

const connectionTimeout = time.Second

// NewHTTPClient applies the worker contract over a transferred TCP connection.
func NewHTTPClient(path, token string, timeout time.Duration) *httpapi.Client {
	transport := &http.Transport{
		DisableKeepAlives: true,
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return DialWorker(ctx, path, token)
		},
	}
	return httpapi.NewClientTransport("http://"+Loopback, token, timeout, transport)
}

// DialWorker receives a connected TCP socket from the worker's own network
// namespace. It authenticates the handoff before sending any HTTP credentials.
func DialWorker(ctx context.Context, path, token string) (net.Conn, error) {
	if token == "" {
		return nil, errors.New("missing worker connection token")
	}
	ctx, cancel := context.WithTimeout(ctx, connectionTimeout)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(ctx, "unixpacket", path)
	if err != nil {
		return nil, fmt.Errorf("dial worker socket: %w", err)
	}
	defer conn.Close() //nolint:errcheck // best-effort close
	unix, ok := conn.(*net.UnixConn)
	if !ok {
		return nil, errors.New("worker listen path is not a Unix socket")
	}
	at, _ := ctx.Deadline()
	if err := unix.SetDeadline(at); err != nil {
		return nil, fmt.Errorf("bound worker socket read: %w", err)
	}
	var challenge [sha256.Size]byte
	if _, err := rand.Read(challenge[:]); err != nil {
		return nil, fmt.Errorf("worker socket challenge: %w", err)
	}
	if _, err := unix.Write(challenge[:]); err != nil {
		return nil, fmt.Errorf("send worker socket challenge: %w", err)
	}
	return receiveWorkerConn(unix, connectionProof(token, challenge[:]))
}

func connectionProof(token string, challenge []byte) []byte {
	tag := hmac.New(sha256.New, []byte(token))
	tag.Write([]byte("hhub.worker.connected-socket\x00"))
	tag.Write(challenge)
	return tag.Sum(nil)
}

func receiveWorkerConn(unix *net.UnixConn, expected []byte) (net.Conn, error) {
	var proof [sha256.Size]byte
	control := make([]byte, syscall.CmsgSpace(16*4))
	n, oob, flags, _, err := unix.ReadMsgUnix(proof[:], control)
	if err != nil {
		return nil, fmt.Errorf("receive worker connection: %w", err)
	}
	fds, err := socketRights(control[:oob])
	defer func() { closeRights(fds) }()
	if err != nil {
		return nil, err
	}
	if flags&(syscall.MSG_CTRUNC|syscall.MSG_TRUNC) != 0 || len(fds) != 1 ||
		n != len(proof) || !hmac.Equal(proof[:], expected) {
		return nil, errors.New("invalid worker connection proof")
	}
	file := os.NewFile(uintptr(fds[0]), "worker-loopback")
	fds = nil
	defer file.Close() //nolint:errcheck // best-effort close
	conn, err := net.FileConn(file)
	if err != nil {
		return nil, fmt.Errorf("wrap worker TCP connection: %w", err)
	}
	if _, ok := conn.(*net.TCPConn); !ok || conn.RemoteAddr().String() != Loopback {
		return nil, errors.Join(errors.New("invalid worker TCP peer"), conn.Close())
	}
	return conn, nil
}

func socketRights(raw []byte) ([]int, error) {
	messages, err := syscall.ParseSocketControlMessage(raw)
	if err != nil {
		return nil, fmt.Errorf("decode worker socket control: %w", err)
	}
	var fds []int
	for _, message := range messages {
		rights, err := syscall.ParseUnixRights(&message)
		if err != nil {
			return fds, fmt.Errorf("decode worker socket descriptors: %w", err)
		}
		fds = append(fds, rights...)
	}
	return fds, nil
}

func closeRights(fds []int) {
	for _, fd := range fds {
		syscall.Close(fd) //nolint:errcheck // close after rejecting a handoff
	}
}

// Package chat is the human operator's private mock-channel client.
package chat

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/letya999/h-hub/cmd/hubctl/operator"
	cv1 "github.com/letya999/h-hub/contracts/channel/v1"
	"github.com/letya999/h-hub/shared/httpapi"
	"github.com/letya999/h-hub/shared/locale"
)

const channelURL = "http://channel-hub:9000"
const operatorKey = "/etc/hhub/keys/operator.key"

func Run(ctx context.Context, args []string, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: hubctl chat <external> <message> | chat read <external> [after]")
	}
	if args[0] == "read" {
		return read(ctx, args[1:], out)
	}
	if len(args) < 2 {
		return fmt.Errorf("usage: hubctl chat <external> <message>")
	}
	return send(ctx, args[0], strings.Join(args[1:], " "), out)
}

func client(external, target string) (*cv1.Client, error) {
	tok, err := operator.Token(external, "", target, "channel-hub", operatorKey)
	if err != nil {
		return nil, fmt.Errorf("chat token: %w", err)
	}
	return &cv1.Client{T: cv1.HTTP{C: httpapi.NewClient(channelURL, tok)}}, nil
}

func read(ctx context.Context, args []string, out io.Writer) error {
	if len(args) < 1 || len(args) > 2 {
		return fmt.Errorf("usage: hubctl chat read <external> [after]")
	}
	after, err := parseAfter(args)
	if err != nil {
		return err
	}
	c, err := client(args[0], "ReadOutbox")
	if err != nil {
		return err
	}
	res, err := c.ReadOutbox(ctx, cv1.ReadOutboxReq{External: args[0], After: after})
	if err != nil {
		return fmt.Errorf("read: %w", err)
	}
	return printMessages(out, res.Messages)
}

func parseAfter(args []string) (int64, error) {
	if len(args) < 2 {
		return 0, nil
	}
	v, err := strconv.ParseInt(args[1], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("after: %w", err)
	}
	return v, nil
}

func send(ctx context.Context, external, text string, out io.Writer) error {
	reader, err := client(external, "ReadOutbox")
	if err != nil {
		return err
	}
	prev, err := reader.ReadOutbox(ctx, cv1.ReadOutboxReq{External: external})
	if err != nil {
		return fmt.Errorf("outbox cursor: %w", err)
	}
	var after int64
	for _, m := range prev.Messages {
		after = m.Seq
	}
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return fmt.Errorf("update id: %w", err)
	}
	writer, err := client(external, "Inject")
	if err != nil {
		return err
	}
	if _, err := writer.Inject(ctx, cv1.InjectReq{External: external, UpdateID: hex.EncodeToString(raw[:]), Text: text}); err != nil {
		return fmt.Errorf("inject: %w", err)
	}
	return await(ctx, external, after, out)
}

func await(ctx context.Context, external string, after int64, out io.Writer) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	last := time.Time{}
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("chat response: %w", ctx.Err())
		case <-tick.C:
			next, err := readAfter(ctx, external, after, out)
			if err != nil {
				return err
			}
			if next > after {
				after = next
				last = time.Now()
			}
			if !last.IsZero() && time.Since(last) > time.Second {
				return nil
			}
		}
	}
}

func readAfter(ctx context.Context, external string, after int64, out io.Writer) (int64, error) {
	c, err := client(external, "ReadOutbox")
	if err != nil {
		return after, err
	}
	res, err := c.ReadOutbox(ctx, cv1.ReadOutboxReq{External: external, After: after})
	if err != nil {
		return after, fmt.Errorf("read: %w", err)
	}
	if err := printMessages(out, res.Messages); err != nil {
		return after, err
	}
	if len(res.Messages) > 0 {
		return res.Messages[len(res.Messages)-1].Seq, nil
	}
	return after, nil
}

func printMessages(out io.Writer, messages []cv1.OutMessage) error {
	for _, m := range messages {
		if m.Kind == "file" {
			if _, err := fmt.Fprintln(out, locale.T(locale.EN, "channel.file")); err != nil {
				return fmt.Errorf("output: %w", err)
			}
		}
		if _, err := fmt.Fprintln(out, m.Body); err != nil {
			return fmt.Errorf("output: %w", err)
		}
	}
	return nil
}

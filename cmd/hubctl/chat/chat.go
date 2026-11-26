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
		return fmt.Errorf("usage: hubctl chat <external> <message> | chat read <external> [after] | chat reply <external> <receipt> <message>")
	}
	if args[0] == "read" {
		return read(ctx, args[1:], out)
	}
	if args[0] == "reply" {
		return reply(ctx, args[1:], out)
	}
	if len(args) < 2 {
		return fmt.Errorf("usage: hubctl chat <external> <message>")
	}
	return send(ctx, cv1.InjectReq{External: args[0], Text: strings.Join(args[1:], " ")}, out)
}

func reply(ctx context.Context, args []string, out io.Writer) error {
	if len(args) < 3 {
		return fmt.Errorf("usage: hubctl chat reply <external> <receipt> <message>")
	}
	receipt, err := strconv.ParseInt(args[1], 10, 64)
	if err != nil || receipt <= 0 {
		return fmt.Errorf("invalid receipt %q", args[1])
	}
	return send(ctx, cv1.InjectReq{External: args[0], Text: strings.Join(args[2:], " "), ReplyTo: receipt}, out)
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

func send(ctx context.Context, req cv1.InjectReq, out io.Writer) error {
	reader, err := client(req.External, "ReadOutbox")
	if err != nil {
		return err
	}
	prev, err := reader.ReadOutbox(ctx, cv1.ReadOutboxReq{External: req.External})
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
	writer, err := client(req.External, "Inject")
	if err != nil {
		return err
	}
	req.UpdateID = hex.EncodeToString(raw[:])
	if _, err := writer.Inject(ctx, req); err != nil {
		return fmt.Errorf("inject: %w", err)
	}
	return await(ctx, req.External, after, out)
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
		if prefix := messagePrefix(m); prefix != "" {
			if _, err := fmt.Fprintln(out, prefix); err != nil {
				return fmt.Errorf("output: %w", err)
			}
		}
		if _, err := fmt.Fprintln(out, m.Body); err != nil {
			return fmt.Errorf("output: %w", err)
		}
	}
	return nil
}

func messagePrefix(m cv1.OutMessage) string {
	switch m.Kind {
	case "request":
		return locale.T(locale.EN, "channel.request-receipt", "receipt", m.Seq)
	case "file":
		return locale.T(locale.EN, "channel.file")
	}
	return ""
}

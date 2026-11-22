// Package job implements `hubctl job`: thin task contract calls for
// operators. No domain logic — arguments map to the contract one to
// one.
package job

import (
	"context"
	"flag"
	"fmt"
	"io"

	tv1 "github.com/letya999/h-hub/contracts/task/v1"
	"github.com/letya999/h-hub/shared/httpapi"
)

const defaultURL = "http://task-engine:9000"

// Run dispatches `hubctl job <op> [flags]`.
func Run(ctx context.Context, args []string, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: hubctl job " + usage)
	}
	fs := flag.NewFlagSet("job", flag.ContinueOnError)
	url := fs.String("url", defaultURL, "task-engine base URL")
	// -url goes before the op: hubctl job [-url U] <op> [op flags].
	if len(args) >= 2 && args[0] == "-url" {
		if err := fs.Parse(args[:2]); err != nil {
			return fmt.Errorf("job flags: %w", err)
		}
		args = args[2:]
	}
	if len(args) == 0 {
		return fmt.Errorf("usage: hubctl job " + usage)
	}
	c := &tv1.Client{T: tv1.HTTP{C: httpapi.NewClient(*url, "")}}
	switch args[0] {
	case "create":
		return create(ctx, c, args[1:], out)
	case "get":
		return one(ctx, c, args[1:], out, "get")
	case "list":
		return list(ctx, c, args[1:], out)
	case "cancel", "retry", "unquarantine":
		return one(ctx, c, args[1:], out, args[0])
	case "hold":
		return hold(ctx, c, args[1:], out)
	}
	return fmt.Errorf("usage: hubctl job " + usage)
}

const usage = "[-url U] create|get|list|cancel|retry|unquarantine|hold [flags]"

func create(ctx context.Context, c *tv1.Client, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("create", flag.ContinueOnError)
	owner := fs.String("owner", "", "owner")
	space := fs.String("space", "", "space")
	conv := fs.String("conversation", "", "conversation")
	input := fs.String("input", "", "input")
	idem := fs.String("idem", "", "idempotency key")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("create flags: %w", err)
	}
	res, err := c.CreateJob(ctx, tv1.CreateJobReq{
		Owner: *owner, Space: *space, Conversation: *conv,
		Input: *input, Idem: *idem})
	if err != nil {
		return fmt.Errorf("create job: %w", err)
	}
	return emit(out, "%s %s\n", res.ID, res.State)
}

func list(ctx context.Context, c *tv1.Client, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	owner := fs.String("owner", "", "owner")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("list flags: %w", err)
	}
	res, err := c.ListJobs(ctx, tv1.ListJobsReq{Owner: *owner})
	if err != nil {
		return fmt.Errorf("list jobs: %w", err)
	}
	for _, j := range res.Jobs {
		if err := emit(out, "%s %s %s\n", j.ID, j.State, j.Result); err != nil {
			return err
		}
	}
	return nil
}

// one handles the single-job ops: get, cancel, retry, unquarantine.
func one(ctx context.Context, c *tv1.Client, args []string, out io.Writer, op string) error {
	fs := flag.NewFlagSet(op, flag.ContinueOnError)
	owner := fs.String("owner", "", "owner")
	id := fs.String("id", "", "job id")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%s flags: %w", op, err)
	}
	j, err := callOne(ctx, c, op, tv1.JobReq{Owner: *owner, ID: *id})
	if err != nil {
		return err
	}
	return emit(out, "%s %s %s\n", j.ID, j.State, j.Result)
}

func callOne(ctx context.Context, c *tv1.Client, op string, r tv1.JobReq) (tv1.JobInfo, error) {
	var j tv1.JobInfo
	var err error
	switch op {
	case "get":
		var res tv1.GetJobResp
		res, err = c.GetJob(ctx, tv1.GetJobReq(r))
		j = res.Job
	case "cancel":
		j, err = c.CancelJob(ctx, r)
	case "retry":
		j, err = c.RetryJob(ctx, r)
	case "unquarantine":
		j, err = c.UnquarantineJob(ctx, r)
	}
	if err != nil {
		return j, fmt.Errorf("%s job: %w", op, err)
	}
	return j, nil
}

func hold(ctx context.Context, c *tv1.Client, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("hold", flag.ContinueOnError)
	space := fs.String("space", "", "space")
	active := fs.Bool("active", true, "hold active")
	note := fs.String("note", "", "note")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("hold flags: %w", err)
	}
	res, err := c.SetHold(ctx, tv1.SetHoldReq{
		Space: *space, Active: *active, Note: *note})
	if err != nil {
		return fmt.Errorf("set hold: %w", err)
	}
	return emit(out, "epoch %d\n", res.Epoch)
}

func emit(out io.Writer, format string, a ...any) error {
	if _, err := fmt.Fprintf(out, format, a...); err != nil {
		return fmt.Errorf("write output: %w", err)
	}
	return nil
}

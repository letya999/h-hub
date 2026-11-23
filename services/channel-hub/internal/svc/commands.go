package svc

import (
	"context"
	"fmt"
	"strings"

	idv1 "github.com/letya999/h-hub/contracts/identity/v1"
	tv1 "github.com/letya999/h-hub/contracts/task/v1"
	herrors "github.com/letya999/h-hub/shared/errors"
	"github.com/letya999/h-hub/shared/locale"
)

type commandOps struct{ *Server }

func (s *commandOps) command(ctx context.Context, r inputRow) error {
	switch {
	case strings.HasPrefix(r.body, "/start "):
		return s.startCommand(ctx, r)
	case r.body == "/new":
		return s.newCommand(ctx, r)
	case r.body == "/status":
		return s.statusCommand(ctx, r)
	case r.body == "/stop":
		return s.stopCommand(ctx, r)
	default:
		return s.completeText(ctx, r.id, r.external, locale.T(locale.EN, "channel.help"))
	}
}

func (s *commandOps) startCommand(ctx context.Context, r inputRow) error {
	code := strings.TrimSpace(strings.TrimPrefix(r.body, "/start "))
	id, err := s.identityClient(ctx)
	if err != nil {
		return err
	}
	if _, err := id.LinkConfirm(ctx, idv1.LinkConfirmReq{Channel: "mock", External: r.external, Code: code}); err != nil {
		return fmt.Errorf("link confirm: %w", err)
	}
	return s.completeText(ctx, r.id, r.external, locale.T(locale.EN, "channel.linked"))
}

func (s *commandOps) newCommand(ctx context.Context, r inputRow) error {
	if _, _, err := s.taskClient(ctx, r.external); err != nil {
		return s.linkError(ctx, r, err)
	}
	if err := s.newConversation(ctx, r.external, r.id); err != nil {
		return err
	}
	return s.completeText(ctx, r.id, r.external, locale.T(locale.EN, "channel.new"))
}

func (s *commandOps) statusCommand(ctx context.Context, r inputRow) error {
	client, _, err := s.taskClient(ctx, r.external)
	if err != nil {
		return s.linkError(ctx, r, err)
	}
	jobs, err := client.ListJobs(ctx, tv1.ListJobsReq{})
	if err != nil {
		return fmt.Errorf("list jobs: %w", err)
	}
	if len(jobs.Jobs) == 0 {
		return s.completeText(ctx, r.id, r.external, locale.T(locale.EN, "channel.no-jobs"))
	}
	last := jobs.Jobs[len(jobs.Jobs)-1]
	return s.completeText(ctx, r.id, r.external, locale.T(locale.EN, "channel.status", "state", last.State))
}

func (s *commandOps) stopCommand(ctx context.Context, r inputRow) error {
	client, _, err := s.taskClient(ctx, r.external)
	if err != nil {
		return s.linkError(ctx, r, err)
	}
	jobs, err := client.ListJobs(ctx, tv1.ListJobsReq{})
	if err != nil {
		return fmt.Errorf("list jobs: %w", err)
	}
	for i := len(jobs.Jobs) - 1; i >= 0; i-- {
		if jobs.Jobs[i].Done {
			continue
		}
		if _, err := client.CancelJob(ctx, tv1.JobReq{ID: jobs.Jobs[i].ID}); err != nil {
			return fmt.Errorf("cancel job: %w", err)
		}
		return s.completeText(ctx, r.id, r.external, locale.T(locale.EN, "channel.stopped"))
	}
	return s.completeText(ctx, r.id, r.external, locale.T(locale.EN, "channel.no-active"))
}

func (s *commandOps) linkError(ctx context.Context, r inputRow, err error) error {
	if herrors.CodeOf(err) == herrors.CodeNotFound {
		return s.completeText(ctx, r.id, r.external, locale.T(locale.EN, "channel.link-required"))
	}
	return err
}

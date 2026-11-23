package svc

import (
	"context"
	"fmt"
	"strings"

	idv1 "github.com/letya999/h-hub/contracts/identity/v1"
	mv1 "github.com/letya999/h-hub/contracts/model/v1"
	sv1 "github.com/letya999/h-hub/contracts/spawn/v1"
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
	case r.body == "/usage":
		return s.usageCommand(ctx, r)
	case r.body == "/model" || strings.HasPrefix(r.body, "/model "):
		return s.modelCommand(ctx, r)
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

// usageCommand reports today's spend against the passport ceilings
// through the gateway's Usage op — the owner scope comes from the
// link token, so the caller reads only its own counters.
func (s *commandOps) usageCommand(ctx context.Context, r inputRow) error {
	mc, err := s.modelClient(ctx, r.external)
	if err != nil {
		return s.linkError(ctx, r, err)
	}
	u, err := mc.Usage(ctx, mv1.UsageReq{})
	if err != nil {
		return fmt.Errorf("usage: %w", err)
	}
	return s.completeText(ctx, r.id, r.external, locale.T(locale.EN, "channel.usage",
		"spent", u.Spent, "reserved", u.Reserved, "daily", u.DailyLimit, "run", u.RunLimit))
}

// modelCommand lists the routes or switches models.default through
// the passport mechanism; the new route applies to the next run with
// no runtime restart because the gateway resolves spec per call.
func (s *commandOps) modelCommand(ctx context.Context, r inputRow) error {
	mc, err := s.modelClient(ctx, r.external)
	if err != nil {
		return s.linkError(ctx, r, err)
	}
	rt, err := mc.Routes(ctx, mv1.RoutesReq{})
	if err != nil {
		return fmt.Errorf("routes: %w", err)
	}
	names := make([]string, 0, len(rt.Routes))
	for _, x := range rt.Routes {
		names = append(names, x.Name)
	}
	list := strings.Join(names, ", ")
	arg := strings.TrimSpace(strings.TrimPrefix(r.body, "/model"))
	if arg == "" {
		return s.completeText(ctx, r.id, r.external,
			locale.T(locale.EN, "channel.models", "list", list, "active", rt.Default))
	}
	resolved, err := s.resolveExternal(ctx, r.external)
	if err != nil {
		return s.linkError(ctx, r, err)
	}
	sc, err := s.spawnOwnerClient(ctx, r.external)
	if err != nil {
		return s.linkError(ctx, r, err)
	}
	if _, err := sc.SetModel(ctx, sv1.SetModelReq{Space: resolved.Space, Default: arg}); err != nil {
		if herrors.CodeOf(err) == herrors.CodeInvalid {
			return s.completeText(ctx, r.id, r.external,
				locale.T(locale.EN, "channel.model-unknown", "route", arg, "list", list))
		}
		return fmt.Errorf("set model: %w", err)
	}
	return s.completeText(ctx, r.id, r.external,
		locale.T(locale.EN, "channel.model-set", "route", arg))
}

// resolveExternal maps the channel handle to its owner space through
// the identity Resolve op.
func (s *commandOps) resolveExternal(ctx context.Context, external string) (idv1.ResolveResp, error) {
	id, err := s.identityClient(ctx)
	if err != nil {
		return idv1.ResolveResp{}, err
	}
	res, err := id.Resolve(ctx, idv1.ResolveReq{Channel: "mock", External: external})
	if err != nil {
		return idv1.ResolveResp{}, fmt.Errorf("resolve: %w", err)
	}
	return res, nil
}

func (s *commandOps) linkError(ctx context.Context, r inputRow, err error) error {
	if herrors.CodeOf(err) == herrors.CodeNotFound {
		return s.completeText(ctx, r.id, r.external, locale.T(locale.EN, "channel.link-required"))
	}
	return err
}

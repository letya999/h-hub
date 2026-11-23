// Package user implements the operator's thin user lifecycle client.
package user

import (
	"context"
	"fmt"
	"io"

	"github.com/letya999/h-hub/cmd/hubctl/operator"
	idv1 "github.com/letya999/h-hub/contracts/identity/v1"
	sv1 "github.com/letya999/h-hub/contracts/spawn/v1"
	"github.com/letya999/h-hub/shared/httpapi"
)

const identityURL = "http://identity-hub:9000"
const spawnURL = "http://spawn-controller:9000"
const operatorKey = "/etc/hhub/keys/operator.key"

func Run(ctx context.Context, args []string, out io.Writer) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: hubctl user add|disable|runtime-drain")
	}
	switch args[0] {
	case "add":
		template := "default"
		if len(args) == 4 && args[2] == "--template" {
			template = args[3]
		} else if len(args) != 2 {
			return fmt.Errorf("usage: hubctl user add <id> [--template default]")
		}
		return add(ctx, args[1], template, out)
	case "disable":
		if len(args) != 2 {
			return fmt.Errorf("usage: hubctl user disable <id>")
		}
		return disable(ctx, args[1], out)
	case "runtime-drain":
		return runRuntimeDrain(ctx, args[1:], out)
	}
	return fmt.Errorf("usage: hubctl user add|disable|runtime-drain")
}

func runRuntimeDrain(ctx context.Context, args []string, out io.Writer) error {
	if len(args) != 2 {
		return fmt.Errorf("usage: hubctl user runtime-drain <owner> <space>")
	}
	return runtimeDrain(ctx, args[0], args[1], out)
}

func runtimeDrain(ctx context.Context, owner, space string, out io.Writer) error {
	if owner == "" || space == "" {
		return fmt.Errorf("owner and space are required")
	}
	id, err := identityClient(owner, space, "spawn-controller")
	if err != nil {
		return err
	}
	delegated, err := id.ActAs(ctx, idv1.ActAsReq{Owner: owner, Space: space, Aud: "spawn-controller"})
	if err != nil {
		return fmt.Errorf("act as: %w", err)
	}
	sp := &sv1.Client{T: sv1.HTTP{C: httpapi.NewClient(spawnURL, delegated.Token)}}
	res, err := sp.DrainRuntime(ctx, sv1.DrainRuntimeReq{Space: space})
	if err != nil {
		return fmt.Errorf("drain runtime: %w", err)
	}
	if _, err := fmt.Fprintf(out, "%s %s\n", space, res.State); err != nil {
		return fmt.Errorf("output: %w", err)
	}
	return nil
}

func identityClient(owner, space, target string) (*idv1.Client, error) {
	tok, err := operator.Token(owner, space, target, "identity-hub", operatorKey)
	if err != nil {
		return nil, fmt.Errorf("operator token: %w", err)
	}
	return &idv1.Client{T: idv1.HTTP{C: httpapi.NewClient(identityURL, tok)}}, nil
}

func add(ctx context.Context, id, template string, out io.Writer) error {
	if id == "" {
		return fmt.Errorf("id is required")
	}
	space, err := createSpace(ctx, id, template)
	if err != nil {
		return err
	}
	code, err := register(ctx, id, space.ID)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "%s %s %s\n", id, space.ID, code.Code); err != nil {
		return fmt.Errorf("output: %w", err)
	}
	return nil
}

func createSpace(ctx context.Context, id, template string) (sv1.CreateSpaceResp, error) {
	idc, err := identityClient(id, "", "spawn-controller")
	if err != nil {
		return sv1.CreateSpaceResp{}, err
	}
	delegated, err := idc.ActAs(ctx, idv1.ActAsReq{Owner: id, Aud: "spawn-controller"})
	if err != nil {
		return sv1.CreateSpaceResp{}, fmt.Errorf("act as: %w", err)
	}
	sp := &sv1.Client{T: sv1.HTTP{C: httpapi.NewClient(spawnURL, delegated.Token)}}
	space, err := sp.CreateSpace(ctx, sv1.CreateSpaceReq{Name: "home", Template: template})
	if err != nil {
		return sv1.CreateSpaceResp{}, fmt.Errorf("create space: %w", err)
	}
	return space, nil
}

func register(ctx context.Context, id, space string) (idv1.LinkBeginResp, error) {
	idc, err := identityClient(id, space, "CreatePrincipal")
	if err != nil {
		return idv1.LinkBeginResp{}, err
	}
	if _, err := idc.CreatePrincipal(ctx, idv1.CreatePrincipalReq{ID: id, Space: space}); err != nil {
		return idv1.LinkBeginResp{}, fmt.Errorf("create principal: %w", err)
	}
	idc, err = identityClient(id, "", "LinkBegin")
	if err != nil {
		return idv1.LinkBeginResp{}, err
	}
	code, err := idc.LinkBegin(ctx, idv1.LinkBeginReq{Owner: id})
	if err != nil {
		return idv1.LinkBeginResp{}, fmt.Errorf("link begin: %w", err)
	}
	return code, nil
}

func disable(ctx context.Context, id string, out io.Writer) error {
	if id == "" {
		return fmt.Errorf("id is required")
	}
	idc, err := identityClient(id, "", "DeactivateHuman")
	if err != nil {
		return err
	}
	if _, err := idc.DeactivateHuman(ctx, idv1.DeactivateHumanReq{ID: id}); err != nil {
		return fmt.Errorf("disable: %w", err)
	}
	if _, err := fmt.Fprintf(out, "%s disabled\n", id); err != nil {
		return fmt.Errorf("output: %w", err)
	}
	return nil
}

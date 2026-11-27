package backend

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"net/url"
)

const (
	workerKeyLabel        = "hhub.worker"
	workerGenerationLabel = "hhub.generation"
	workerInstallLabel    = "hhub.installation"
)

// WorkerRef identifies one generation of a worker key within this installation.
type WorkerRef struct {
	Key, Generation string
	Container       string
}

func workerName(installation, key string) string {
	sum := sha256.Sum256([]byte(installation + "|" + key))
	return fmt.Sprintf("hhub-worker-%x", sum[:16])
}

func workerLabels(installation string, ref WorkerRef) map[string]string {
	return map[string]string{
		workerKeyLabel: ref.Key, workerGenerationLabel: ref.Generation, workerInstallLabel: installation,
	}
}

func (d *Docker) workerInspect(ctx context.Context, ref WorkerRef) (containerInspect, error) {
	var c containerInspect
	if d.network == "" || ref.Key == "" || ref.Generation == "" {
		return c, errors.New("incomplete Docker worker identity")
	}
	target := ref.Container
	if target == "" {
		target = workerName(d.network, ref.Key)
	}
	err := d.req(ctx, http.MethodGet, "/containers/"+url.PathEscape(target)+"/json", nil, &c)
	if err != nil {
		return c, err
	}
	for name, value := range workerLabels(d.network, ref) {
		if c.ID == "" || c.Config.Labels[name] != value {
			return c, fmt.Errorf("Docker worker %s differs from registered generation", ref.Key)
		}
	}
	return c, nil
}

// Workers lists labelled worker generations from this Docker installation.
func (d *Docker) Workers(ctx context.Context) ([]WorkerRef, error) {
	var rows []struct {
		ID     string `json:"Id"`
		Labels map[string]string
	}
	filter := url.QueryEscape(fmt.Sprintf(`{"label":[%q]}`, workerInstallLabel+"="+d.network))
	if err := d.req(ctx, http.MethodGet, "/containers/json?all=1&filters="+filter, nil, &rows); err != nil {
		return nil, fmt.Errorf("list workers: %w", err)
	}
	var out []WorkerRef
	for _, row := range rows {
		ref := WorkerRef{Key: row.Labels[workerKeyLabel], Generation: row.Labels[workerGenerationLabel], Container: row.ID}
		if d.network != "" && row.Labels[workerInstallLabel] == d.network && ref.Key != "" &&
			ref.Generation != "" && ref.Container != "" {
			out = append(out, ref)
		}
	}
	return out, nil
}

// StopWorker attempts removal and confirms absence with Inspect.
func (d *Docker) StopWorker(ctx context.Context, ref WorkerRef) (bool, error) {
	c, err := d.workerInspect(ctx, ref)
	if err != nil {
		if isNotFound(err) {
			return true, nil
		}
		return false, err
	}
	removeErr := d.req(ctx, http.MethodDelete, "/containers/"+c.ID+"?force=true", nil, nil)
	if isNotFound(removeErr) {
		removeErr = nil
	}
	_, inspectErr := d.workerInspect(ctx, ref)
	if isNotFound(inspectErr) {
		return true, removeErr
	}
	return false, errors.Join(removeErr, inspectErr)
}

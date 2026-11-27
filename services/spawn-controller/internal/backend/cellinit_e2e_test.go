//go:build hhube2e

package backend

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"testing"
	"time"

	wv1 "github.com/letya999/h-hub/contracts/worker/v1"
)

func TestCellinitBootInDocker(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	d := New("/var/run/docker.sock", "hhub-cellinit-probe-"+rand.Text())
	ref := WorkerRef{Key: rand.Text(), Generation: rand.Text()}
	cl := wv1.Profile{Image: "hhub-terminal-worker:local", Entrypoint: "/cellinit", Memory: 128 << 20, CPU: 1e9, PIDs: 64}
	active, err := d.ActivateWorkerProfiles(ctx, wv1.Policy{Profiles: map[string]wv1.Profile{"terminal": cl}})
	if err != nil {
		t.Fatal(err)
	}
	prepareProbeSlot(t, ctx, d)
	launch := WorkerLaunch{Ref: ref, Profile: active.Profiles["terminal"], RenderVol: "hhub_hhub-render", Slot: 1}
	began := time.Now()
	id, err := d.CreateAndStartWorker(ctx, launch)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if absent, err := d.StopWorker(cleanup, ref); !absent || err != nil {
			t.Errorf("cleanup worker: absent=%t err=%v", absent, err)
		}
	})
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		t.Fatal(err)
	}
	token := hex.EncodeToString(raw[:])
	if err := d.InstallWorkerToken(ctx, ref, token); err != nil {
		t.Fatal(err)
	}
	if err := d.ProbeWorker(ctx, ref, token); err != nil {
		t.Fatal(err)
	}
	t.Logf("local cellinit readiness: %.3f s", time.Since(began).Seconds())
	if recovered, err := d.CreateAndStartWorker(ctx, launch); err != nil || recovered != id {
		t.Fatalf("same launch intent: first=%q recovered=%q err=%v", id, recovered, err)
	}
}

func prepareProbeSlot(t *testing.T, ctx context.Context, d *Docker) {
	t.Helper()
	volume := wv1.SocketVolume(d.network)
	var c struct{ ID string }
	args := map[string]any{
		"Image": "hhub-terminal-worker:local", "User": "0",
		"Entrypoint": []string{"install", "-d", "-m", "0777", "/slots/1"},
		"HostConfig": hostConfig{NetworkMode: "none", Mounts: []mount{{Type: "volume", Source: volume, Target: "/slots"}}},
	}
	if err := d.req(ctx, http.MethodPost, "/containers/create", args, &c); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := d.req(ctx, http.MethodDelete, "/containers/"+c.ID+"?force=true", nil, nil); err != nil {
			t.Error(err)
		}
		if err := d.req(ctx, http.MethodDelete, "/volumes/"+volume, nil, nil); err != nil {
			t.Error(err)
		}
	})
	if err := d.req(ctx, http.MethodPost, "/containers/"+c.ID+"/start", nil, nil); err != nil {
		t.Fatal(err)
	}
	var status struct{ StatusCode int }
	if err := d.req(ctx, http.MethodPost, "/containers/"+c.ID+"/wait", nil, &status); err != nil || status.StatusCode != 0 {
		t.Fatalf("prepare socket slot: exit=%d, err=%v", status.StatusCode, err)
	}
}

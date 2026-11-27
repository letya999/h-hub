package backend

import (
	"context"
	"fmt"
	"path"
	"regexp"

	wv1 "github.com/letya999/h-hub/contracts/worker/v1"
)

// WorkerLaunch contains controller-derived paths and a locally approved image.
type WorkerLaunch struct {
	Ref       WorkerRef
	Profile   wv1.Profile
	RenderVol string
	Workspace []WorkspaceMount
	Slot      int
}

var imageID = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// ActivateWorkerProfiles resolves installed images once when the controller starts.
func (d *Docker) ActivateWorkerProfiles(ctx context.Context, policy wv1.Policy) (wv1.Policy, error) {
	active := wv1.Policy{Limits: policy.Limits, Profiles: make(map[string]wv1.Profile, len(policy.Profiles))}
	for name, p := range policy.Profiles {
		id, err := d.ImageID(ctx, p.Image)
		if err != nil {
			return wv1.Policy{}, fmt.Errorf("activate worker profile %s: %w", name, err)
		}
		if !imageID.MatchString(id) {
			return wv1.Policy{}, fmt.Errorf("worker profile %s has no installed image ID", name)
		}
		p.Image = id
		active.Profiles[name] = p
	}
	return active, nil
}

// CreateAndStartWorker launches the registered generation under its stable name.
func (d *Docker) CreateAndStartWorker(ctx context.Context, l WorkerLaunch) (string, error) {
	if d.network == "" || l.Ref.Key == "" || l.Ref.Generation == "" || !validWorkerResources(l) {
		return "", fmt.Errorf("invalid worker launch specification")
	}
	spec := workerSpec(d.network, l)
	return d.ensureStarted(ctx, workerName(d.network, l.Ref.Key), spec)
}

func validWorkerResources(l WorkerLaunch) bool {
	p := l.Profile
	return imageID.MatchString(p.Image) && p.Entrypoint != "" && l.RenderVol != "" &&
		l.Slot > 0 && p.Memory > 0 && p.CPU > 0 && p.PIDs > 0
}

func workerSpec(installation string, l WorkerLaunch) createReq {
	return createReq{
		Image:       l.Profile.Image,
		User:        "65534:65534",
		Env:         []string{"PATH=/usr/local/bin:/usr/bin:/bin"},
		Entrypoint:  []string{l.Profile.Entrypoint},
		Labels:      workerLabels(installation, l.Ref),
		Healthcheck: healthCheck{Test: []string{"NONE"}},
		HostConfig: hostConfig{
			NetworkMode:    "none",
			ReadonlyRootfs: true,
			CapDrop:        []string{"ALL"},
			SecurityOpt:    []string{"no-new-privileges"},
			Memory:         l.Profile.Memory,
			NanoCPUs:       l.Profile.CPU,
			PidsLimit:      l.Profile.PIDs,
			Tmpfs: map[string]string{
				"/tmp":      "rw,noexec,nosuid,nodev,uid=65534,gid=65534,mode=0700,size=32m",
				"/run/hhub": "rw,noexec,nosuid,nodev,uid=65534,gid=65534,mode=0700,size=1m",
			},
			Mounts: append(workspaceMounts(l.RenderVol, l.Workspace), mount{Type: "volume",
				Source: wv1.SocketVolume(installation), Target: path.Dir(wv1.WorkerSocket),
				VolumeOptions: &mountPath{Subpath: wv1.SlotDirectory(l.Slot)}}),
		},
	}
}

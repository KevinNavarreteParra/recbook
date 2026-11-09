package nightly

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// GPU is a reading of the graphics card's load.
type GPU struct {
	UtilPct int // how busy it is, 0-100
	UsedMiB int // memory in use by everything, ours included
}

// ReadGPU asks nvidia-smi for the first GPU's load.
func ReadGPU(ctx context.Context) (GPU, error) {
	out, err := exec.CommandContext(ctx, "nvidia-smi",
		"--query-gpu=utilization.gpu,memory.used", "--format=csv,noheader,nounits").Output()
	if err != nil {
		return GPU{}, fmt.Errorf("reading the GPU with nvidia-smi: %w", err)
	}
	line, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
	util, used, ok := strings.Cut(line, ",")
	if !ok {
		return GPU{}, fmt.Errorf("unexpected nvidia-smi output %q", line)
	}
	var g GPU
	if g.UtilPct, err = strconv.Atoi(strings.TrimSpace(util)); err != nil {
		return GPU{}, fmt.Errorf("unexpected nvidia-smi output %q", line)
	}
	if g.UsedMiB, err = strconv.Atoi(strings.TrimSpace(used)); err != nil {
		return GPU{}, fmt.Errorf("unexpected nvidia-smi output %q", line)
	}
	return g, nil
}

// Thresholds for calling the GPU busy. An idle desktop uses a few hundred
// MiB and about 1% of the GPU; a game uses gigabytes and most of it.
const (
	maxStartUtilPct = 20   // busier than this at the start: someone is using it
	maxStartUsedMiB = 2560 // more memory in use than this at the start: same
	maxGrowthMiB    = 1536 // others' memory growing this much mid-run: a game started
)

// Guard decides whether the job may use the GPU. Make one with Claim before
// starting GPU work, then call Check between batches.
type Guard struct {
	read        func(context.Context) (GPU, error)
	baselineMiB int // memory others were using when the job started
}

// Claim checks that the GPU is idle enough to start, and remembers how much
// memory other programs were using at that moment.
func Claim(ctx context.Context) (*Guard, error) {
	return claim(ctx, ReadGPU)
}

func claim(ctx context.Context, read func(context.Context) (GPU, error)) (*Guard, error) {
	g, err := read(ctx)
	if err != nil {
		return nil, err
	}
	if g.UtilPct > maxStartUtilPct || g.UsedMiB > maxStartUsedMiB {
		return nil, fmt.Errorf("GPU in use (%d%% busy, %d MiB used); skipping tonight", g.UtilPct, g.UsedMiB)
	}
	return &Guard{read: read, baselineMiB: g.UsedMiB}, nil
}

// Check returns an error once other programs' GPU memory has grown well
// past what it was at the start, which means someone started using the
// GPU. ownMiB is what the job itself holds (the loaded model).
func (gd *Guard) Check(ctx context.Context, ownMiB int) error {
	g, err := gd.read(ctx)
	if err != nil {
		return err
	}
	others := g.UsedMiB - ownMiB
	if others-gd.baselineMiB > maxGrowthMiB {
		return fmt.Errorf("something else started using the GPU (%d MiB, up from %d); stopping for tonight",
			others, gd.baselineMiB)
	}
	return nil
}

package kry

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"

	psutil "github.com/shirou/gopsutil/v3/process"
)

type processOutputLimit struct {
	mu       sync.Mutex
	limit    int64
	written  int64
	exceeded bool
	cancel   context.CancelFunc
}

func (w *processOutputLimit) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.exceeded {
		return len(p), nil
	}
	if int64(len(p)) > w.limit-w.written {
		w.exceeded = true
		w.cancel()
		return len(p), nil
	}
	w.written += int64(len(p))
	return len(p), nil
}

func (w *processOutputLimit) exceededOutput() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.exceeded
}

func processContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func processDetails(ctx context.Context, p *psutil.Process) map[string]any {
	ctx = processContext(ctx)
	value := map[string]any{
		"pid":            p.Pid,
		"name":           "",
		"exe":            "",
		"username":       "",
		"create_time_ms": int64(0),
		"status":         []string{},
	}
	if name, err := p.NameWithContext(ctx); err == nil {
		value["name"] = name
	}
	if exe, err := p.ExeWithContext(ctx); err == nil {
		value["exe"] = exe
	}
	if username, err := p.UsernameWithContext(ctx); err == nil {
		value["username"] = username
	}
	if created, err := p.CreateTimeWithContext(ctx); err == nil {
		value["create_time_ms"] = created
	}
	if status, err := p.StatusWithContext(ctx); err == nil {
		value["status"] = status
	}
	return value
}

func processInfoJSON(ctx context.Context, pid int64) (string, error) {
	ctx = processContext(ctx)
	if pid <= 0 || pid > int64(^uint32(0)>>1) {
		return "", fmt.Errorf("pid must be a positive 32-bit integer")
	}
	p, err := psutil.NewProcess(int32(pid))
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(processDetails(ctx, p))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func processListJSON(ctx context.Context, max int) (string, error) {
	ctx = processContext(ctx)
	if max < 1 {
		return "", fmt.Errorf("process list limit must be positive")
	}
	processes, err := psutil.ProcessesWithContext(ctx)
	if err != nil {
		return "", err
	}
	sort.Slice(processes, func(i, j int) bool { return processes[i].Pid < processes[j].Pid })
	if len(processes) > max {
		return "", fmt.Errorf("process list exceeds configured limit of %d", max)
	}
	values := make([]map[string]any, len(processes))
	for i, p := range processes {
		values[i] = processDetails(ctx, p)
	}
	data, err := json.Marshal(values)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

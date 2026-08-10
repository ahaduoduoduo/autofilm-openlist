package _115

import (
	"context"
	"sort"
	"sync"
)

type uploadWaiter struct {
	taskID  string
	weight  int
	ready   chan struct{}
	granted bool
}

// weightedUploadGate caps provider uploads while distributing newly available
// slots with smooth weighted round-robin. Existing uploads are never
// interrupted; weights affect the next free slot.
type weightedUploadGate struct {
	mu       sync.Mutex
	capacity int
	active   int
	queues   map[string][]*uploadWaiter
	current  map[string]int
}

func newWeightedUploadGate(capacity int) *weightedUploadGate {
	return &weightedUploadGate{
		capacity: capacity,
		queues:   make(map[string][]*uploadWaiter),
		current:  make(map[string]int),
	}
}

func (g *weightedUploadGate) acquire(ctx context.Context, taskID string, weight int) (func(), error) {
	if taskID == "" {
		taskID = "_default"
	}
	if weight <= 0 {
		weight = 1
	}
	waiter := &uploadWaiter{taskID: taskID, weight: weight, ready: make(chan struct{})}
	g.mu.Lock()
	g.queues[taskID] = append(g.queues[taskID], waiter)
	g.dispatchLocked()
	g.mu.Unlock()

	select {
	case <-waiter.ready:
		return g.releaseFunc(), nil
	case <-ctx.Done():
		g.mu.Lock()
		if waiter.granted {
			g.active--
			g.dispatchLocked()
			g.mu.Unlock()
			return nil, ctx.Err()
		}
		g.removeWaiterLocked(waiter)
		g.mu.Unlock()
		return nil, ctx.Err()
	}
}

func (g *weightedUploadGate) releaseFunc() func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			g.mu.Lock()
			g.active--
			g.dispatchLocked()
			g.mu.Unlock()
		})
	}
}

func (g *weightedUploadGate) dispatchLocked() {
	for g.active < g.capacity {
		taskIDs := make([]string, 0, len(g.queues))
		totalWeight := 0
		for taskID, queue := range g.queues {
			if len(queue) == 0 {
				continue
			}
			taskIDs = append(taskIDs, taskID)
			totalWeight += queue[0].weight
		}
		if len(taskIDs) == 0 {
			return
		}
		sort.Strings(taskIDs)
		selected := taskIDs[0]
		for _, taskID := range taskIDs {
			g.current[taskID] += g.queues[taskID][0].weight
			if g.current[taskID] > g.current[selected] {
				selected = taskID
			}
		}
		g.current[selected] -= totalWeight
		waiter := g.queues[selected][0]
		g.queues[selected] = g.queues[selected][1:]
		if len(g.queues[selected]) == 0 {
			delete(g.queues, selected)
		}
		waiter.granted = true
		g.active++
		close(waiter.ready)
	}
}

func (g *weightedUploadGate) removeWaiterLocked(target *uploadWaiter) {
	queue := g.queues[target.taskID]
	for idx, waiter := range queue {
		if waiter != target {
			continue
		}
		queue = append(queue[:idx], queue[idx+1:]...)
		break
	}
	if len(queue) == 0 {
		delete(g.queues, target.taskID)
		return
	}
	g.queues[target.taskID] = queue
}

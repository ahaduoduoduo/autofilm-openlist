package _115

import "testing"

func TestWeightedUploadGateUsesSmoothWeightedOrder(t *testing.T) {
	gate := newWeightedUploadGate(1)
	makeWaiters := func(task string, weight, count int) []*uploadWaiter {
		waiters := make([]*uploadWaiter, 0, count)
		for range count {
			waiters = append(waiters, &uploadWaiter{
				taskID: task,
				weight: weight,
				ready:  make(chan struct{}),
			})
		}
		return waiters
	}
	nasWaiters := makeWaiters("nas-config", 2, 4)
	timeMachineWaiters := makeWaiters("time-machine", 1, 2)
	gate.queues["nas-config"] = append([]*uploadWaiter(nil), nasWaiters...)
	gate.queues["time-machine"] = append([]*uploadWaiter(nil), timeMachineWaiters...)

	var order []string
	granted := map[string]int{}
	for range 6 {
		gate.dispatchLocked()
		for _, set := range [][]*uploadWaiter{nasWaiters, timeMachineWaiters} {
			count := 0
			for _, waiter := range set {
				if waiter.granted {
					count++
				}
			}
			task := set[0].taskID
			if count > granted[task] {
				order = append(order, task)
				granted[task] = count
				break
			}
		}
		gate.active--
	}

	want := []string{
		"nas-config", "time-machine", "nas-config",
		"nas-config", "time-machine", "nas-config",
	}
	if len(order) != len(want) {
		t.Fatalf("dispatch order length = %d, want %d (%v)", len(order), len(want), order)
	}
	for idx := range want {
		if order[idx] != want[idx] {
			t.Fatalf("dispatch order = %v, want %v", order, want)
		}
	}
}

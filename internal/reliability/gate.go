package reliability

import (
	"fmt"
	"net/http"
	"sync"
	"time"
)

type Kind string

const (
	KindPlan  Kind = "Plan"
	KindApply Kind = "Apply"
)

type WorkItem struct {
	ID                   string
	Kind                 Kind
	Stack                string
	ConcurrencyGroup     string
	MaxConcurrentPlans   int
	MaxConcurrentApplies int
}

type Metrics struct {
	Submitted        int
	Completed        int
	MaxActivePlans   int
	MaxActiveApplies int
	MaxStackApplies  int
	DuplicateIDs     int
	GateReady        bool
	OverCapacity     bool
	SafetyViolation  bool
}

type Snapshot struct {
	Active []WorkItem
}

type Gate struct {
	mu                 sync.Mutex
	maxPlans           int
	maxApplies         int
	activePlans        int
	activeApplies      int
	activeByStack      map[string]int
	activeByPlanGroup  map[string]int
	activeByApplyGroup map[string]int
	startedIDs         map[string]bool
	activeItems        map[string]WorkItem
	metrics            Metrics
	condition          *sync.Cond
	gateReady          bool
	overCapacity       bool
	safetyViolation    bool
}

func NewGate(maxPlans, maxApplies int) *Gate {
	gate := &Gate{maxPlans: maxPlans, maxApplies: maxApplies, activeByStack: map[string]int{}, activeByPlanGroup: map[string]int{}, activeByApplyGroup: map[string]int{}, startedIDs: map[string]bool{}, activeItems: map[string]WorkItem{}, gateReady: true}
	gate.condition = sync.NewCond(&gate.mu)
	return gate
}

func (g *Gate) Acquire(item WorkItem) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := validateItem(item); err != nil {
		return err
	}
	if g.startedIDs[item.ID] {
		g.metrics.DuplicateIDs++
		return fmt.Errorf("work item %q was already started", item.ID)
	}
	for !g.gateReady || !g.available(item) || g.overCapacity || g.safetyViolation {
		g.condition.Wait()
	}
	g.reserveLocked(item)
	return nil
}

// TryAcquire reserves a slot without blocking. It is used by reconcilers so a
// run with no capacity remains Pending instead of tying up a worker.
func (g *Gate) TryAcquire(item WorkItem) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if validateItem(item) != nil {
		return false
	}
	if current, ok := g.activeItems[item.ID]; ok {
		return current.Kind == item.Kind && current.Stack == item.Stack
	}
	if !g.gateReady || g.overCapacity || g.safetyViolation || g.startedIDs[item.ID] || !g.available(item) {
		return false
	}
	g.reserveLocked(item)
	return true
}

// EnsureActive reconstructs a reservation for an already-running Job after a
// controller restart. It is idempotent for the same active item.
func (g *Gate) EnsureActive(item WorkItem) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if validateItem(item) != nil {
		return false
	}
	if current, ok := g.activeItems[item.ID]; ok {
		return current.Kind == item.Kind && current.Stack == item.Stack
	}
	if !g.gateReady || g.startedIDs[item.ID] || !g.available(item) {
		return false
	}
	g.reserveLocked(item)
	return true
}

func validateItem(item WorkItem) error {
	if item.ID == "" {
		return fmt.Errorf("work item ID is required")
	}
	if item.Kind != KindPlan && item.Kind != KindApply {
		return fmt.Errorf("unsupported work item kind %q", item.Kind)
	}
	return nil
}

func (g *Gate) reserveLocked(item WorkItem) {
	g.startedIDs[item.ID] = true
	g.activeItems[item.ID] = item
	if item.Kind == KindPlan {
		g.activePlans++
		if item.ConcurrencyGroup != "" {
			g.activeByPlanGroup[item.ConcurrencyGroup]++
		}
		if g.activePlans > g.metrics.MaxActivePlans {
			g.metrics.MaxActivePlans = g.activePlans
		}
		return
	}
	g.activeApplies++
	g.activeByStack[item.Stack]++
	if item.ConcurrencyGroup != "" {
		g.activeByApplyGroup[item.ConcurrencyGroup]++
	}
	if g.activeApplies > g.metrics.MaxActiveApplies {
		g.metrics.MaxActiveApplies = g.activeApplies
	}
	if g.activeByStack[item.Stack] > g.metrics.MaxStackApplies {
		g.metrics.MaxStackApplies = g.activeByStack[item.Stack]
	}
}

func (g *Gate) Release(item WorkItem) {
	g.mu.Lock()
	defer g.mu.Unlock()
	active, ok := g.activeItems[item.ID]
	if !ok {
		return
	}
	if active.Kind == KindPlan {
		g.activePlans--
		if active.ConcurrencyGroup != "" {
			g.activeByPlanGroup[active.ConcurrencyGroup]--
		}
	} else {
		g.activeApplies--
		g.activeByStack[active.Stack]--
		if active.ConcurrencyGroup != "" {
			g.activeByApplyGroup[active.ConcurrencyGroup]--
		}
	}
	delete(g.activeItems, item.ID)
	g.metrics.Completed++
	if g.activePlans <= g.maxPlans && g.activeApplies <= g.maxApplies {
		g.overCapacity = false
	}
	g.condition.Broadcast()
}

func (g *Gate) available(item WorkItem) bool {
	if item.Kind == KindPlan {
		if g.activePlans >= g.maxPlans {
			return false
		}
		return item.ConcurrencyGroup == "" || item.MaxConcurrentPlans <= 0 || g.activeByPlanGroup[item.ConcurrencyGroup] < item.MaxConcurrentPlans
	}
	if g.activeApplies >= g.maxApplies || g.activeByStack[item.Stack] != 0 {
		return false
	}
	return item.ConcurrencyGroup == "" || item.MaxConcurrentApplies <= 0 || g.activeByApplyGroup[item.ConcurrencyGroup] < item.MaxConcurrentApplies
}

func (g *Gate) Metrics() Metrics {
	g.mu.Lock()
	defer g.mu.Unlock()
	metrics := g.metrics
	metrics.GateReady = g.gateReady
	metrics.OverCapacity = g.overCapacity
	metrics.SafetyViolation = g.safetyViolation
	return metrics
}

type ReconstructionResult struct {
	GateReady       bool
	OverCapacity    bool
	SafetyViolation bool
	Active          []WorkItem
	Duration        time.Duration
}

// Reconstruct is called after a manager restart. It observes every active
// execution, even when configured capacity is exceeded; only new admission is
// blocked until the active set drains.
func (g *Gate) Reconstruct(items []WorkItem) ReconstructionResult {
	started := time.Now()
	g.mu.Lock()
	defer g.mu.Unlock()
	g.gateReady = false
	g.overCapacity = false
	g.safetyViolation = false
	g.activePlans, g.activeApplies = 0, 0
	g.activeByStack = map[string]int{}
	g.activeByPlanGroup = map[string]int{}
	g.activeByApplyGroup = map[string]int{}
	g.activeItems = map[string]WorkItem{}
	g.startedIDs = map[string]bool{}
	result := ReconstructionResult{Active: append([]WorkItem(nil), items...)}
	for _, item := range items {
		if validateItem(item) != nil {
			g.safetyViolation = true
			continue
		}
		if g.startedIDs[item.ID] {
			g.safetyViolation = true
		}
		if item.Kind == KindApply && g.activeByStack[item.Stack] > 0 {
			g.safetyViolation = true
		}
		g.reserveObservedLocked(item)
	}
	g.overCapacity = g.activePlans > g.maxPlans || g.activeApplies > g.maxApplies
	g.gateReady = true
	result.GateReady = g.gateReady
	result.OverCapacity = g.overCapacity
	result.SafetyViolation = g.safetyViolation
	result.Duration = time.Since(started)
	g.condition.Broadcast()
	return result
}

func (g *Gate) reserveObservedLocked(item WorkItem) {
	g.startedIDs[item.ID] = true
	g.activeItems[item.ID] = item
	if item.Kind == KindPlan {
		g.activePlans++
		if item.ConcurrencyGroup != "" {
			g.activeByPlanGroup[item.ConcurrencyGroup]++
		}
		return
	}
	g.activeApplies++
	g.activeByStack[item.Stack]++
	if item.ConcurrencyGroup != "" {
		g.activeByApplyGroup[item.ConcurrencyGroup]++
	}
}

func (g *Gate) Ready() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.gateReady
}

func (g *Gate) ReadyCheck(_ *http.Request) error {
	if !g.Ready() {
		return fmt.Errorf("execution reconstruction gate is not ready")
	}
	return nil
}

func (g *Gate) Snapshot() Snapshot {
	g.mu.Lock()
	defer g.mu.Unlock()
	active := make([]WorkItem, 0, g.activePlans+g.activeApplies)
	for _, item := range g.activeItems {
		active = append(active, item)
	}
	return Snapshot{Active: active}
}

func (g *Gate) Restore(items []WorkItem) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, item := range items {
		if err := validateItem(item); err != nil {
			return err
		}
		if g.startedIDs[item.ID] || g.activeItems[item.ID].ID != "" {
			return fmt.Errorf("cannot restore duplicate active run %q", item.ID)
		}
		if !g.available(item) {
			return fmt.Errorf("cannot restore active run %q: concurrency capacity exceeded", item.ID)
		}
		if item.Kind == KindPlan {
			g.reserveLocked(item)
		} else {
			if g.activeByStack[item.Stack] > 0 {
				return fmt.Errorf("cannot restore multiple active applies for stack %q", item.Stack)
			}
			g.reserveLocked(item)
		}
	}
	return nil
}

func Execute(items []WorkItem, maxPlans, maxApplies int) Metrics {
	gate := NewGate(maxPlans, maxApplies)
	var wait sync.WaitGroup
	wait.Add(len(items))
	for _, item := range items {
		item := item
		go func() {
			defer wait.Done()
			if err := gate.Acquire(item); err == nil {
				gate.Release(item)
			}
		}()
	}
	wait.Wait()
	metrics := gate.Metrics()
	metrics.Submitted = len(items)
	return metrics
}

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
	gateReady          bool
	overCapacity       bool
	safetyViolation    bool
}

func NewGate(maxPlans, maxApplies int) *Gate {
	return &Gate{maxPlans: maxPlans, maxApplies: maxApplies, activeByStack: map[string]int{}, activeByPlanGroup: map[string]int{}, activeByApplyGroup: map[string]int{}, startedIDs: map[string]bool{}, activeItems: map[string]WorkItem{}, gateReady: true}
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
		return
	}
	g.activeApplies++
	g.activeByStack[item.Stack]++
	if item.ConcurrencyGroup != "" {
		g.activeByApplyGroup[item.ConcurrencyGroup]++
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
	if g.activePlans <= g.maxPlans && g.activeApplies <= g.maxApplies {
		g.overCapacity = false
	}
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

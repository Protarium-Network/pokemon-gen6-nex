package globals

import (
	"sync"

	"github.com/PretendoNetwork/nex-go/v2/types"
)

// SubscriptionDataTargets remembers, per player, the players they asked to be kept informed about. Safe for concurrent use.
type SubscriptionDataTargets struct {
	mu      sync.RWMutex
	targets map[types.PID]types.List[types.PID]
}

func NewSubscriptionDataTargets() *SubscriptionDataTargets {
	return &SubscriptionDataTargets{targets: make(map[types.PID]types.List[types.PID])}
}

func (dt *SubscriptionDataTargets) ClearTargets(owner types.PID) {
	dt.mu.Lock()
	defer dt.mu.Unlock()

	delete(dt.targets, owner)
}

// CreateTargets makes sure the player has a (possibly empty) list.
func (dt *SubscriptionDataTargets) CreateTargets(owner types.PID) {
	dt.mu.Lock()
	defer dt.mu.Unlock()

	if _, exists := dt.targets[owner]; !exists {
		dt.targets[owner] = types.List[types.PID]{}
	}
}

func (dt *SubscriptionDataTargets) DeleteTargets(owner types.PID, targetsToDelete types.List[types.PID]) {
	dt.mu.Lock()
	defer dt.mu.Unlock()

	current, exists := dt.targets[owner]
	if !exists {
		return
	}

	kept := make(types.List[types.PID], 0, len(current))
	for _, pid := range current {
		if !targetsToDelete.Contains(pid) {
			kept = append(kept, pid)
		}
	}

	dt.targets[owner] = kept
}

// GetTargets returns a copy, the caller may keep it.
func (dt *SubscriptionDataTargets) GetTargets(owner types.PID) types.List[types.PID] {
	dt.mu.RLock()
	defer dt.mu.RUnlock()

	current, exists := dt.targets[owner]
	if !exists {
		return types.List[types.PID]{}
	}

	return append(types.List[types.PID]{}, current...)
}

func (dt *SubscriptionDataTargets) HasData(owner types.PID) bool {
	dt.mu.RLock()
	defer dt.mu.RUnlock()

	return len(dt.targets[owner]) > 0
}

// ReplaceTargets stores the new list. It is kept even if the player has no list yet: the list of a player whose old session
// just ended must not make the call a silent no-op.
func (dt *SubscriptionDataTargets) ReplaceTargets(owner types.PID, newTargets types.List[types.PID]) {
	dt.mu.Lock()
	defer dt.mu.Unlock()

	dt.targets[owner] = append(types.List[types.PID]{}, newTargets...)
}

// Watchers lists the players who have this player as a target.
func (dt *SubscriptionDataTargets) Watchers(pid types.PID) []types.PID {
	dt.mu.RLock()
	defer dt.mu.RUnlock()

	var watchers []types.PID
	for owner, list := range dt.targets {
		if list.Contains(pid) {
			watchers = append(watchers, owner)
		}
	}

	return watchers
}

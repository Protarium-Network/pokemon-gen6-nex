package globals

import (
	"sync"
	"time"

	"github.com/PretendoNetwork/nex-go/v2"
	"github.com/PretendoNetwork/nex-go/v2/types"
	subscription_types "github.com/PretendoNetwork/nex-protocols-go/v2/subscription/types"
)

// Upper bound on the players kept in memory. When it is reached the player who has been there longest is dropped.
const TimelineLength int = 100000

// SubscriptionDataHolder is what one player published about themselves (their presence), and who published it.
type SubscriptionDataHolder struct {
	CreationTime time.Time
	UpdatedTime  time.Time
	Data         subscription_types.SubscriptionData

	// The games share this server but not their players: X/Y and OR/AS announce different group numbers and
	// ask for the active players of their own group only.
	Group uint32

	// The connection that published the data. A console that reconnects publishes again from a new connection while the
	// old one may still time out, so only the connection that owns the data may remove it.
	Connection *nex.PRUDPConnection
}

// ActivePlayer is one entry of the "players online" list.
type ActivePlayer struct {
	PID  types.PID
	Data subscription_types.SubscriptionData
}

// SubscriptionDataTimeline holds the presence of every connected player. Safe for concurrent use: every connection is
// served from its own goroutine.
type SubscriptionDataTimeline struct {
	mu      sync.RWMutex
	entries map[types.PID]SubscriptionDataHolder
}

func NewSubscriptionDataTimeline() *SubscriptionDataTimeline {
	return &SubscriptionDataTimeline{entries: make(map[types.PID]SubscriptionDataHolder)}
}

// CreateData publishes the presence of a player. A player who is already there (reconnected, or the old entry never
// timed out) is replaced, and the players watching them are told.
func (tm *SubscriptionDataTimeline) CreateData(owner types.PID, data subscription_types.SubscriptionData, group uint32, connection *nex.PRUDPConnection, targetsTable *SubscriptionDataTargets) {
	now := time.Now()

	tm.mu.Lock()
	if _, exists := tm.entries[owner]; !exists && len(tm.entries) >= TimelineLength {
		tm.evictOldestLocked()
	}
	tm.entries[owner] = SubscriptionDataHolder{
		CreationTime: now,
		UpdatedTime:  now,
		Data:         data.Copy().(subscription_types.SubscriptionData),
		Group:        group,
		Connection:   connection,
	}
	tm.mu.Unlock()

	targetsTable.CreateTargets(owner)

	HandleSubscriptionChangeNotification(owner, true)
}

func (tm *SubscriptionDataTimeline) evictOldestLocked() {
	var oldestKey types.PID
	oldest := time.Now()
	for key, value := range tm.entries {
		if value.CreationTime.Before(oldest) {
			oldest = value.CreationTime
			oldestKey = key
		}
	}
	delete(tm.entries, oldestKey)
}

func (tm *SubscriptionDataTimeline) GetData(target types.PID) (*SubscriptionDataHolder, *nex.Error) {
	tm.mu.RLock()
	holder, exists := tm.entries[target]
	tm.mu.RUnlock()

	if !exists {
		return nil, nex.NewError(nex.ResultCodes.Core.InvalidArgument, "Subscription target does not exist")
	}

	return &holder, nil
}

func (tm *SubscriptionDataTimeline) HasData(owner types.PID) bool {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	_, exists := tm.entries[owner]

	return exists
}

func (tm *SubscriptionDataTimeline) Len() int {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	return len(tm.entries)
}

func (tm *SubscriptionDataTimeline) UpdateData(owner types.PID, data subscription_types.SubscriptionData) *nex.Error {
	tm.mu.Lock()
	holder, exists := tm.entries[owner]
	if !exists {
		tm.mu.Unlock()
		return nex.NewError(nex.ResultCodes.Core.InvalidArgument, "Subscription target does not exist")
	}

	holder.UpdatedTime = time.Now()
	holder.Data = data.Copy().(subscription_types.SubscriptionData)
	tm.entries[owner] = holder
	tm.mu.Unlock()

	HandleSubscriptionChangeNotification(owner, false)

	return nil
}

// ClearData removes a player because they asked to go offline.
func (tm *SubscriptionDataTimeline) ClearData(owner types.PID, targetsTable *SubscriptionDataTargets) {
	tm.mu.Lock()
	delete(tm.entries, owner)
	tm.mu.Unlock()

	targetsTable.ClearTargets(owner)

	HandleSubscriptionClearNotification(owner, true)
}

// ClearDataIfOwned removes a player because a connection ended, but only when that connection is the one that published
// the data. The end of an old connection must not wipe the presence of the session that replaced it.
func (tm *SubscriptionDataTimeline) ClearDataIfOwned(owner types.PID, connection *nex.PRUDPConnection, targetsTable *SubscriptionDataTargets) bool {
	tm.mu.Lock()
	holder, exists := tm.entries[owner]
	if !exists || holder.Connection != connection {
		tm.mu.Unlock()
		return false
	}
	delete(tm.entries, owner)
	tm.mu.Unlock()

	targetsTable.ClearTargets(owner)

	HandleSubscriptionClearNotification(owner, true)

	return true
}

// ActivePlayers lists up to limit online players, leaving out the player who asks. The players of the asked group come first,
// the others follow to fill the list: the games can play together (trades, battles, Wonder Trade), so a player of another
// group is still worth showing. It also returns how many of the players are of the asked group.
func (tm *SubscriptionDataTimeline) ActivePlayers(group uint32, exclude types.PID, limit int) ([]ActivePlayer, int) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	var sameGroup, otherGroups []ActivePlayer
	for pid, holder := range tm.entries {
		if pid == exclude {
			continue
		}

		player := ActivePlayer{PID: pid, Data: holder.Data.Copy().(subscription_types.SubscriptionData)}
		if holder.Group == group {
			sameGroup = append(sameGroup, player)
		} else {
			otherGroups = append(otherGroups, player)
		}
	}

	if len(sameGroup) > limit {
		sameGroup = sameGroup[:limit]
	}

	players := append(sameGroup, otherGroups...)
	if len(players) > limit {
		players = players[:limit]
	}

	return players, len(sameGroup)
}

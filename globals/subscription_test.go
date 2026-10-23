package globals

import (
	"sync"
	"testing"

	"github.com/PretendoNetwork/nex-go/v2"
	"github.com/PretendoNetwork/nex-go/v2/types"
	subscription_types "github.com/PretendoNetwork/nex-protocols-go/v2/subscription/types"
)

func presence(pid uint32, blob string) subscription_types.SubscriptionData {
	data := subscription_types.NewSubscriptionData()
	data.PrincipalID = types.NewPID(uint64(pid))
	data.Unknown = types.QBuffer([]byte(blob))
	return data
}

func newTables() (*SubscriptionDataTimeline, *SubscriptionDataTargets) {
	return NewSubscriptionDataTimeline(), NewSubscriptionDataTargets()
}

// A console that reconnects publishes from a new connection while the old one is still around. Creating again must
// replace the data, and the old connection ending afterwards must not take the new session's presence away.
func TestEndOfOldSessionKeepsNewSession(t *testing.T) {
	timeline, targets := newTables()
	pid := types.NewPID(1000000001)
	oldSession, newSession := &nex.PRUDPConnection{}, &nex.PRUDPConnection{}

	timeline.CreateData(pid, presence(1000000001, "first"), 1, oldSession, targets)
	timeline.CreateData(pid, presence(1000000001, "second"), 1, newSession, targets)
	targets.ReplaceTargets(pid, types.List[types.PID]{types.NewPID(7)})

	holder, err := timeline.GetData(pid)
	if err != nil || string(holder.Data.Unknown) != "second" {
		t.Fatal("creating again must replace the presence")
	}

	if timeline.ClearDataIfOwned(pid, oldSession, targets) {
		t.Fatal("the old connection ending removed the presence of the new session")
	}
	if !timeline.HasData(pid) || len(targets.GetTargets(pid)) != 1 {
		t.Fatal("presence or targets of the new session were lost")
	}

	if !timeline.ClearDataIfOwned(pid, newSession, targets) {
		t.Fatal("the owning connection ending must remove the presence")
	}
	if timeline.HasData(pid) || len(targets.GetTargets(pid)) != 0 {
		t.Fatal("presence not removed")
	}
}

func TestExplicitClearRemovesWhoeverOwnsIt(t *testing.T) {
	timeline, targets := newTables()
	pid := types.NewPID(5)
	timeline.CreateData(pid, presence(5, "x"), 1, &nex.PRUDPConnection{}, targets)

	timeline.ClearData(pid, targets)

	if timeline.HasData(pid) {
		t.Fatal("ClearMySubscriptionData must remove the presence")
	}
}

func TestActivePlayersPreferTheSameGameAndNeverListSelf(t *testing.T) {
	timeline, targets := newTables()
	xy1, xy2, oras := types.NewPID(11), types.NewPID(12), types.NewPID(21)
	timeline.CreateData(xy1, presence(11, "a"), 1, &nex.PRUDPConnection{}, targets)
	timeline.CreateData(xy2, presence(12, "b"), 1, &nex.PRUDPConnection{}, targets)
	timeline.CreateData(oras, presence(21, "c"), 2, &nex.PRUDPConnection{}, targets)

	// the games can play together: the other game is listed too, after the players of the same game
	forXY, same := timeline.ActivePlayers(1, xy1, 100)
	if len(forXY) != 2 || same != 1 || forXY[0].PID != xy2 || forXY[1].PID != oras {
		t.Fatalf("X/Y player: want [other X/Y, OR/AS] with 1 of the same group, got %v (%d)", forXY, same)
	}

	forORAS, same := timeline.ActivePlayers(2, oras, 100)
	if len(forORAS) != 2 || same != 0 {
		t.Fatalf("OR/AS player should see both X/Y players, got %v (%d)", forORAS, same)
	}

	// the limit cuts the others first, the same group keeps its place
	limited, _ := timeline.ActivePlayers(1, xy1, 1)
	if len(limited) != 1 || limited[0].PID != xy2 {
		t.Fatalf("with a limit of 1 the player of the same group must win, got %v", limited)
	}

	for _, p := range forXY {
		if p.PID == xy1 {
			t.Fatal("a player must not see themselves")
		}
	}
}

func TestReplaceTargetsWorksWithoutAnExistingList(t *testing.T) {
	timeline, targets := newTables()
	_ = timeline
	pid := types.NewPID(30)

	targets.ReplaceTargets(pid, types.List[types.PID]{types.NewPID(31), types.NewPID(32)})

	if got := targets.GetTargets(pid); len(got) != 2 {
		t.Fatalf("targets of a player without a list were dropped: %v", got)
	}
	if w := targets.Watchers(types.NewPID(31)); len(w) != 1 || w[0] != pid {
		t.Fatalf("watchers wrong: %v", w)
	}

	// the returned list is a copy
	copyOfTargets := targets.GetTargets(pid)
	copyOfTargets[0] = types.NewPID(999)
	if targets.GetTargets(pid)[0] == types.NewPID(999) {
		t.Fatal("GetTargets leaks the internal list")
	}
}

func TestDeleteTargets(t *testing.T) {
	_, targets := newTables()
	pid := types.NewPID(40)
	targets.ReplaceTargets(pid, types.List[types.PID]{types.NewPID(1), types.NewPID(2), types.NewPID(3)})

	targets.DeleteTargets(pid, types.List[types.PID]{types.NewPID(2)})

	got := targets.GetTargets(pid)
	if len(got) != 2 || got.Contains(types.NewPID(2)) {
		t.Fatalf("DeleteTargets did not remove the target: %v", got)
	}
}

// Run with -race: every connection is served from its own goroutine.
func TestConcurrentUse(t *testing.T) {
	timeline, targets := newTables()
	var wg sync.WaitGroup

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			pid := types.NewPID(uint64(1000 + i%10))
			connection := &nex.PRUDPConnection{}
			for j := 0; j < 200; j++ {
				timeline.CreateData(pid, presence(uint32(pid), "p"), uint32(1+i%2), connection, targets)
				targets.ReplaceTargets(pid, types.List[types.PID]{types.NewPID(1000 + uint64(j%10))})
				_, _ = timeline.ActivePlayers(1, pid, 100)
				_ = targets.Watchers(pid)
				_ = timeline.HasData(pid)
				_ = timeline.UpdateData(pid, presence(uint32(pid), "u"))
				timeline.ClearDataIfOwned(pid, connection, targets)
			}
		}(i)
	}

	wg.Wait()
}

func TestPIDsExceptLeavesOutTheNewcomer(t *testing.T) {
	timeline, targets := newTables()
	for _, pid := range []uint64{1, 2, 3} {
		timeline.CreateData(types.NewPID(pid), presence(uint32(pid), "x"), 1, &nex.PRUDPConnection{}, targets)
	}

	others := timeline.PIDsExcept(types.NewPID(2))
	if len(others) != 2 {
		t.Fatalf("want the 2 other players, got %v", others)
	}
	for _, pid := range others {
		if pid == types.NewPID(2) {
			t.Fatal("the newcomer must not be told about themselves")
		}
	}
}

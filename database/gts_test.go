package database

import (
	"bytes"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	_ "github.com/lib/pq"
)

// Needs a scratch database: PN_POKEGEN6_TEST_POSTGRES_URI=postgres://user:pass@host/db?sslmode=disable go test ./database
func connectTestDB(t *testing.T) {
	t.Helper()

	uri := os.Getenv("PN_POKEGEN6_TEST_POSTGRES_URI")
	if uri == "" {
		t.Skip("PN_POKEGEN6_TEST_POSTGRES_URI not set")
	}

	db, err := sql.Open("postgres", uri)
	if err != nil {
		t.Fatal(err)
	}
	Postgres = db

	if _, err := Postgres.Exec(`DROP SCHEMA IF EXISTS gts CASCADE`); err != nil {
		t.Fatal(err)
	}
	InitGTSDatabase()
}

const (
	alice = uint64(1700000001)
	bob   = uint64(1700000002)
	carol = uint64(1700000003)
)

// list puts a Pokemon on sale for a player and returns its record
func list(t *testing.T, owner uint64, index, pokemon string) *GTSRecord {
	t.Helper()

	password := uint64(0xFFFFFFFF00000001) + owner // high values on purpose: uint64 round trip through numeric
	id, err := GTSReserveKey(owner, password)
	if err != nil {
		t.Fatal(err)
	}
	if err := GTSList(id, 7, []byte(index), []byte(pokemon), []byte("sig")); err != nil {
		t.Fatal(err)
	}
	record, err := GTSGetOwned(id, password, owner)
	if err != nil {
		t.Fatal(err)
	}
	return record
}

func TestListSearchAndOwnership(t *testing.T) {
	connectTestDB(t)

	a := list(t, alice, "idx-a", "mon-a")
	list(t, bob, "idx-b", "mon-b")

	if a.Password != uint64(0xFFFFFFFF00000001)+alice {
		t.Fatalf("password did not survive the database: %d", a.Password)
	}
	if a.State != GTSStateListed || a.Version != 2 {
		t.Fatalf("unexpected state %d version %d", a.State, a.Version)
	}

	if _, err := GTSGetOwned(a.DataID, a.Password+1, alice); !errors.Is(err, ErrGTSNotFound) {
		t.Fatal("a wrong password must not give access")
	}
	if _, err := GTSGetOwned(a.DataID, a.Password, bob); !errors.Is(err, ErrGTSNotFound) {
		t.Fatal("somebody else's key must not give access")
	}

	records, total, err := GTSSearch(GTSSearchFilter{ExcludeOwner: alice, Length: 10})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(records) != 1 || records[0].OwnerPID != bob {
		t.Fatalf("alice should only see bob's listing, got total=%d", total)
	}

	// paging and order
	list(t, carol, "idx-c", "mon-c")
	newest, _, _ := GTSSearch(GTSSearchFilter{ExcludeOwner: alice, Length: 1})
	oldest, total, _ := GTSSearch(GTSSearchFilter{ExcludeOwner: alice, Length: 1, OldestFirst: true})
	if total != 2 || newest[0].OwnerPID != carol || oldest[0].OwnerPID != bob {
		t.Fatalf("ordering is wrong: newest=%d oldest=%d total=%d", newest[0].OwnerPID, oldest[0].OwnerPID, total)
	}
	second, _, _ := GTSSearch(GTSSearchFilter{ExcludeOwner: alice, Offset: 1, Length: 5})
	if len(second) != 1 || second[0].OwnerPID != bob {
		t.Fatal("offset ignored")
	}

	// time window
	none, total, _ := GTSSearch(GTSSearchFilter{ExcludeOwner: alice, Length: 5, UploadedAfter: time.Now().Add(time.Hour)})
	if total != 0 || len(none) != 0 {
		t.Fatal("UploadedAfter ignored")
	}
}

func TestTradeFlow(t *testing.T) {
	connectTestDB(t)

	listing := list(t, alice, "idx-a", "mon-a")

	// bob prepares a record to deposit into, then reserves alice's listing
	bobKey, _ := GTSReserveKey(bob, 99)
	if err := GTSReserveForTrade(listing.DataID, listing.Version, bob, 555); err != nil {
		t.Fatal(err)
	}

	// carol cannot take what bob holds, and bob cannot take a stale version
	if err := GTSReserveForTrade(listing.DataID, listing.Version, carol, 777); !errors.Is(err, ErrGTSNotFound) {
		t.Fatal("a held listing must not be reservable by someone else")
	}
	if err := GTSReserveForTrade(listing.DataID, listing.Version+1, bob, 555); !errors.Is(err, ErrGTSNotFound) {
		t.Fatal("a stale version must not be reservable")
	}
	if _, err := GTSCompleteTrade(listing.DataID, listing.Version, carol, 777, 0, 7, []byte("i"), []byte("p"), nil); !errors.Is(err, ErrGTSNotFound) {
		t.Fatal("trading without holding the reservation must fail")
	}
	if _, err := GTSCompleteTrade(listing.DataID, listing.Version, bob, 556, bobKey, 7, []byte("i"), []byte("p"), nil); !errors.Is(err, ErrGTSNotFound) {
		t.Fatal("trading with the wrong reservation key must fail")
	}

	mine, err := GTSCompleteTrade(listing.DataID, listing.Version, bob, 555, bobKey, 7, []byte("idx-b"), []byte("mon-b"), []byte("sig-b"))
	if err != nil {
		t.Fatal(err)
	}
	if mine != bobKey {
		t.Fatalf("bob's side should reuse the record he prepared (%d), got %d", bobKey, mine)
	}

	// alice finds her listing traded, with bob's Pokemon waiting
	after, err := GTSGetOwned(listing.DataID, listing.Password, alice)
	if err != nil {
		t.Fatal(err)
	}
	if after.State != GTSStateTraded || !bytes.Equal(after.ReceivedPokemon, []byte("mon-b")) || !bytes.Equal(after.ReceivedIndex, []byte("idx-b")) {
		t.Fatalf("depositor does not get the traded Pokemon: state=%d", after.State)
	}
	if !bytes.Equal(after.PokemonData, []byte("mon-a")) {
		t.Fatal("the original Pokemon must stay on the record")
	}

	// no longer searchable, cannot be traded twice
	if _, total, _ := GTSSearch(GTSSearchFilter{ExcludeOwner: carol, Length: 5}); total != 0 {
		t.Fatal("a traded listing is still searchable")
	}
	if err := GTSReserveForTrade(listing.DataID, listing.Version, carol, 777); !errors.Is(err, ErrGTSNotFound) {
		t.Fatal("a traded listing can be reserved again")
	}

	// bob's record is history, not a listing
	history, _ := GTSGet(mine)
	if history.State != GTSStateHistory || history.OwnerPID != bob {
		t.Fatal("trader's side not recorded")
	}

	// alice collects and deletes
	if err := GTSDelete(listing.DataID, listing.Password, bob); !errors.Is(err, ErrGTSNotFound) {
		t.Fatal("only the owner may delete")
	}
	if err := GTSDelete(listing.DataID, listing.Password, alice); err != nil {
		t.Fatal(err)
	}
}

func TestExpiredReservationCanBeTaken(t *testing.T) {
	connectTestDB(t)

	listing := list(t, alice, "i", "p")
	if err := GTSReserveForTrade(listing.DataID, listing.Version, bob, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := Postgres.Exec(`UPDATE gts.records SET reserved_until = now() - interval '1 minute' WHERE data_id = $1`, int64(listing.DataID)); err != nil {
		t.Fatal(err)
	}
	if err := GTSReserveForTrade(listing.DataID, listing.Version, carol, 2); err != nil {
		t.Fatalf("an expired reservation should be free: %v", err)
	}
	// and bob's old key no longer works
	if _, err := GTSCompleteTrade(listing.DataID, listing.Version, bob, 1, 0, 7, []byte("i"), []byte("p"), nil); !errors.Is(err, ErrGTSNotFound) {
		t.Fatal("an expired reservation key must not complete a trade")
	}
}

func TestReserveKeyReplacesUnfinishedOnes(t *testing.T) {
	connectTestDB(t)

	first, _ := GTSReserveKey(alice, 1)
	second, _ := GTSReserveKey(alice, 2)
	if _, err := GTSGet(first); !errors.Is(err, ErrGTSNotFound) {
		t.Fatal("an unfinished reservation should be replaced by the next one")
	}
	if _, err := GTSGet(second); err != nil {
		t.Fatal(err)
	}
}

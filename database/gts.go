package database

import (
	"database/sql"
	"errors"
	"strconv"
	"time"

	"github.com/PretendoNetwork/pokemon-gen6/globals"
)

// GTS (Global Trade Station) storage. One row is one Pokemon deposited by a player. Its lifecycle:
//
//	reserved  - PrepareUploadPokemon handed out a key, nothing uploaded yet
//	listed    - visible in searches
//	traded    - somebody took it, the depositor has not collected the Pokemon they were given in return
//	history   - the row a trader gets for their side of a trade (kept for the "MyDataID" they are told about)
const (
	GTSStateReserved int16 = 0
	GTSStateListed   int16 = 1
	GTSStateTraded   int16 = 2
	GTSStateHistory  int16 = 3
)

// How long a listing stays reserved for a trader between PrepareTradePokemon and TradePokemon
const GTSReservationTTL = 5 * time.Minute

// GTSRecord mirrors a row of gts.records. Passwords are uint64 in the protocol but numeric(20) in Postgres.
type GTSRecord struct {
	DataID          uint64
	OwnerPID        uint64
	Password        uint64
	State           int16
	Period          uint16
	Version         uint32
	IndexData       []byte
	PokemonData     []byte
	Signature       []byte
	UpdatedAt       time.Time
	ReservedBy      uint64
	ReservationKey  uint64
	ReservedUntil   time.Time
	ReceivedIndex   []byte
	ReceivedPokemon []byte
}

var ErrGTSNotFound = errors.New("gts record not found")

func InitGTSDatabase() {
	statements := []string{
		`CREATE SCHEMA IF NOT EXISTS gts`,
		`CREATE TABLE IF NOT EXISTS gts.records (
			data_id bigserial PRIMARY KEY,
			owner_pid numeric(20) NOT NULL,
			password numeric(20) NOT NULL,
			state smallint NOT NULL DEFAULT 0,
			period integer NOT NULL DEFAULT 0,
			version integer NOT NULL DEFAULT 1,
			index_data bytea,
			pokemon_data bytea,
			signature bytea,
			updated_at timestamptz NOT NULL DEFAULT now(),
			reserved_by numeric(20) NOT NULL DEFAULT 0,
			reservation_key numeric(20) NOT NULL DEFAULT 0,
			reserved_until timestamptz,
			received_index bytea,
			received_pokemon bytea
		)`,
		`CREATE INDEX IF NOT EXISTS gts_records_listing ON gts.records (state, updated_at)`,
		`CREATE INDEX IF NOT EXISTS gts_records_owner ON gts.records (owner_pid)`,
	}

	for _, statement := range statements {
		if _, err := Postgres.Exec(statement); err != nil {
			globals.Logger.Error(err.Error())
			return
		}
	}
}

const gtsColumns = `data_id, owner_pid::text, password::text, state, period, version,
	COALESCE(index_data, ''::bytea), COALESCE(pokemon_data, ''::bytea), COALESCE(signature, ''::bytea), updated_at,
	reserved_by::text, reservation_key::text, COALESCE(reserved_until, 'epoch'::timestamptz),
	COALESCE(received_index, ''::bytea), COALESCE(received_pokemon, ''::bytea)`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanGTSRecord(row rowScanner) (*GTSRecord, error) {
	var (
		r                                       GTSRecord
		id                                      int64
		owner, password, reservedBy, reserveKey string
		period, version                         int64
	)

	err := row.Scan(&id, &owner, &password, &r.State, &period, &version,
		&r.IndexData, &r.PokemonData, &r.Signature, &r.UpdatedAt,
		&reservedBy, &reserveKey, &r.ReservedUntil,
		&r.ReceivedIndex, &r.ReceivedPokemon)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrGTSNotFound
	}
	if err != nil {
		return nil, err
	}

	r.DataID = uint64(id)
	r.Period = uint16(period)
	r.Version = uint32(version)
	r.OwnerPID, _ = strconv.ParseUint(owner, 10, 64)
	r.Password, _ = strconv.ParseUint(password, 10, 64)
	r.ReservedBy, _ = strconv.ParseUint(reservedBy, 10, 64)
	r.ReservationKey, _ = strconv.ParseUint(reserveKey, 10, 64)

	return &r, nil
}

func u64(v uint64) string { return strconv.FormatUint(v, 10) }

// GTSReserveKey creates an empty record for a player and returns it. Older unfinished reservations of the same player are dropped.
func GTSReserveKey(ownerPID, password uint64) (uint64, error) {
	if _, err := Postgres.Exec(`DELETE FROM gts.records WHERE owner_pid = $1 AND state = $2`, u64(ownerPID), GTSStateReserved); err != nil {
		return 0, err
	}

	var id int64
	err := Postgres.QueryRow(`INSERT INTO gts.records (owner_pid, password, state) VALUES ($1, $2, $3) RETURNING data_id`,
		u64(ownerPID), u64(password), GTSStateReserved).Scan(&id)

	return uint64(id), err
}

// GTSGetOwned returns the record if it belongs to the player and the password matches.
func GTSGetOwned(dataID, password, ownerPID uint64) (*GTSRecord, error) {
	return scanGTSRecord(Postgres.QueryRow(`SELECT `+gtsColumns+` FROM gts.records
		WHERE data_id = $1 AND password = $2 AND owner_pid = $3`, int64(dataID), u64(password), u64(ownerPID)))
}

// GTSGet returns a record by id, whoever owns it.
func GTSGet(dataID uint64) (*GTSRecord, error) {
	return scanGTSRecord(Postgres.QueryRow(`SELECT `+gtsColumns+` FROM gts.records WHERE data_id = $1`, int64(dataID)))
}

// GTSList stores a player's Pokemon on a record they reserved earlier and makes it visible.
func GTSList(dataID uint64, period uint16, index, pokemon, signature []byte) error {
	result, err := Postgres.Exec(`UPDATE gts.records
		SET state = $2, period = $3, index_data = $4, pokemon_data = $5, signature = $6, version = version + 1, updated_at = now()
		WHERE data_id = $1 AND state IN ($7, $2)`,
		int64(dataID), GTSStateListed, int(period), index, pokemon, signature, GTSStateReserved)
	if err != nil {
		return err
	}

	if n, _ := result.RowsAffected(); n == 0 {
		return ErrGTSNotFound
	}

	return nil
}

// GTSSearchFilter selects listed Pokemon. Zero times mean "no bound".
type GTSSearchFilter struct {
	ExcludeOwner   uint64
	UploadedAfter  time.Time
	UploadedBefore time.Time
	OldestFirst    bool
	Offset         uint32
	Length         uint32
}

// GTSSearch returns one page of listings and how many listings match in total.
func GTSSearch(filter GTSSearchFilter) ([]*GTSRecord, uint32, error) {
	where := `state = $1 AND owner_pid <> $2 AND ($3::timestamptz IS NULL OR updated_at >= $3) AND ($4::timestamptz IS NULL OR updated_at <= $4)`
	args := []any{GTSStateListed, u64(filter.ExcludeOwner), nullTime(filter.UploadedAfter), nullTime(filter.UploadedBefore)}

	var total uint32
	if err := Postgres.QueryRow(`SELECT count(*) FROM gts.records WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	order := `DESC`
	if filter.OldestFirst {
		order = `ASC`
	}

	rows, err := Postgres.Query(`SELECT `+gtsColumns+` FROM gts.records WHERE `+where+
		` ORDER BY updated_at `+order+`, data_id `+order+` OFFSET $5 LIMIT $6`,
		append(args, int64(filter.Offset), int64(filter.Length))...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var records []*GTSRecord
	for rows.Next() {
		record, err := scanGTSRecord(rows)
		if err != nil {
			return nil, 0, err
		}
		records = append(records, record)
	}

	return records, total, rows.Err()
}

func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}

// GTSReserveForTrade lets a trader hold a listing for GTSReservationTTL. It fails when somebody else holds it.
func GTSReserveForTrade(dataID uint64, version uint32, trader, key uint64) error {
	result, err := Postgres.Exec(`UPDATE gts.records
		SET reserved_by = $3, reservation_key = $4, reserved_until = now() + make_interval(secs => $5)
		WHERE data_id = $1 AND state = $6 AND version = $2 AND owner_pid <> $3
		  AND (reserved_by = 0 OR reserved_by = $3 OR reserved_until IS NULL OR reserved_until < now())`,
		int64(dataID), int64(version), u64(trader), u64(key), int(GTSReservationTTL.Seconds()), GTSStateListed)
	if err != nil {
		return err
	}

	if n, _ := result.RowsAffected(); n == 0 {
		return ErrGTSNotFound
	}

	return nil
}

// GTSCompleteTrade moves a listing to the traded state, handing the depositor the trader's Pokemon, and records the
// trader's side. It returns the id of the trader's own record. Everything happens in one transaction.
func GTSCompleteTrade(targetID uint64, version uint32, trader, key uint64, myRecordID uint64, period uint16, index, pokemon, signature []byte) (uint64, error) {
	tx, err := Postgres.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	result, err := tx.Exec(`UPDATE gts.records
		SET state = $5, received_index = $6, received_pokemon = $7, updated_at = now()
		WHERE data_id = $1 AND state = $8 AND version = $2 AND reserved_by = $3 AND reservation_key = $4 AND reserved_until >= now()`,
		int64(targetID), int64(version), u64(trader), u64(key), GTSStateTraded, index, pokemon, GTSStateListed)
	if err != nil {
		return 0, err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return 0, ErrGTSNotFound
	}

	var mine int64
	if myRecordID != 0 {
		err = tx.QueryRow(`UPDATE gts.records
			SET state = $2, period = $3, index_data = $4, pokemon_data = $5, signature = $6, updated_at = now()
			WHERE data_id = $1 AND owner_pid = $7 RETURNING data_id`,
			int64(myRecordID), GTSStateHistory, int(period), index, pokemon, signature, u64(trader)).Scan(&mine)
		if errors.Is(err, sql.ErrNoRows) {
			mine = 0
		} else if err != nil {
			return 0, err
		}
	}

	if mine == 0 {
		if err = tx.QueryRow(`INSERT INTO gts.records (owner_pid, password, state, period, index_data, pokemon_data, signature)
			VALUES ($1, 0, $2, $3, $4, $5, $6) RETURNING data_id`,
			u64(trader), GTSStateHistory, int(period), index, pokemon, signature).Scan(&mine); err != nil {
			return 0, err
		}
	}

	return uint64(mine), tx.Commit()
}

// GTSDelete removes a player's record.
func GTSDelete(dataID, password, ownerPID uint64) error {
	result, err := Postgres.Exec(`DELETE FROM gts.records WHERE data_id = $1 AND password = $2 AND owner_pid = $3`,
		int64(dataID), u64(password), u64(ownerPID))
	if err != nil {
		return err
	}

	if n, _ := result.RowsAffected(); n == 0 {
		return ErrGTSNotFound
	}

	return nil
}

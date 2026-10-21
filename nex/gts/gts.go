// Package nex_gts implements the Global Trade Station methods of the Pokemon Gen 6 DataStore protocol.
//
// Players deposit one Pokemon (PrepareUploadPokemon + UploadPokemon), others browse the deposits (SearchPokemon) and swap
// one of their own for a deposit (PrepareTradePokemon + TradePokemon). The depositor later collects what they were given
// (DownloadMyPokemon) and may withdraw (DeletePokemon).
package nex_gts

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"time"

	"github.com/PretendoNetwork/nex-go/v2"
	"github.com/PretendoNetwork/nex-go/v2/types"
	datastore_pokemon_gen6 "github.com/PretendoNetwork/nex-protocols-go/v2/datastore/pokemon-gen6"
	gts_types "github.com/PretendoNetwork/nex-protocols-go/v2/datastore/pokemon-gen6/types"
	"github.com/PretendoNetwork/pokemon-gen6/database"
	"github.com/PretendoNetwork/pokemon-gen6/globals"
)

const (
	defaultPageLength = 30
	maxPageLength     = 100
)

// Register installs the GTS handlers on a protocol instance.
func Register(protocol *datastore_pokemon_gen6.Protocol) {
	protocol.PrepareUploadPokemon = prepareUpload
	protocol.UploadPokemon = upload
	protocol.SearchPokemon = search
	protocol.PrepareTradePokemon = prepareTrade
	protocol.TradePokemon = trade
	protocol.DownloadOtherPokemon = downloadOther
	protocol.DownloadMyPokemon = downloadMine
	protocol.DeletePokemon = remove
}

// ---- helpers -----------------------------------------------------------------------------------------------------

func senderPID(packet nex.PacketInterface) uint64 {
	return uint64(packet.Sender().PID())
}

func randomUint64() uint64 {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err) // the system random source is gone, nothing sane to do
	}
	// never 0: 0 means "no key" in the database
	return binary.BigEndian.Uint64(b[:]) | 1
}

// reply builds a success response around whatever the writer puts in the body.
func reply(packet nex.PacketInterface, callID uint32, method uint32, write func(out *nex.ByteStreamOut)) *nex.RMCMessage {
	endpoint := packet.Sender().Endpoint().(*nex.PRUDPEndPoint)

	out := nex.NewByteStreamOut(endpoint.LibraryVersions(), endpoint.ByteStreamSettings())
	if write != nil {
		write(out)
	}

	message := nex.NewRMCSuccess(endpoint, out.Bytes())
	message.ProtocolID = datastore_pokemon_gen6.ProtocolID
	message.MethodID = method
	message.CallID = callID

	return message
}

// fail turns a storage error into the RMC error the game understands.
func fail(what string, err error) (*nex.RMCMessage, *nex.Error) {
	if errors.Is(err, database.ErrGTSNotFound) {
		return nil, nex.NewError(nex.ResultCodes.DataStore.NotFound, what+": record not found")
	}

	globals.Logger.Errorf("GTS %s: %v", what, err)
	return nil, nex.NewError(nex.ResultCodes.DataStore.Unknown, what+" failed")
}

func badRequest(err error) (*nex.RMCMessage, *nex.Error) {
	globals.Logger.Error(err.Error())
	return nil, nex.NewError(nex.ResultCodes.Core.InvalidArgument, err.Error())
}

func downloadResult(r *database.GTSRecord, index, pokemon []byte) gts_types.GlobalTradeStationDownloadPokemonResult {
	return gts_types.GlobalTradeStationDownloadPokemonResult{
		DataID:      types.NewUInt64(r.DataID),
		IndexData:   types.QBuffer(index),
		PokemonData: types.QBuffer(pokemon),
	}
}

// ---- upload ------------------------------------------------------------------------------------------------------

func prepareUpload(err error, packet nex.PacketInterface, callID uint32) (*nex.RMCMessage, *nex.Error) {
	if err != nil {
		return badRequest(err)
	}

	password := randomUint64()
	dataID, dbErr := database.GTSReserveKey(senderPID(packet), password)
	if dbErr != nil {
		return fail("PrepareUploadPokemon", dbErr)
	}

	key := gts_types.GlobalTradeStationRecordKey{DataID: types.NewUInt64(dataID), Password: types.NewUInt64(password)}

	return reply(packet, callID, datastore_pokemon_gen6.MethodPrepareUploadPokemon, func(out *nex.ByteStreamOut) {
		key.WriteTo(out)
	}), nil
}

func upload(err error, packet nex.PacketInterface, callID uint32, param gts_types.GlobalTradeStationUploadPokemonParam) (*nex.RMCMessage, *nex.Error) {
	if err != nil {
		return badRequest(err)
	}

	pid := senderPID(packet)
	if _, dbErr := database.GTSGetOwned(uint64(param.PrepareUploadKey.DataID), uint64(param.PrepareUploadKey.Password), pid); dbErr != nil {
		return fail("UploadPokemon", dbErr)
	}

	if dbErr := database.GTSList(uint64(param.PrepareUploadKey.DataID), uint16(param.Period), []byte(param.IndexData), []byte(param.PokemonData), []byte(param.Signature)); dbErr != nil {
		return fail("UploadPokemon", dbErr)
	}

	return reply(packet, callID, datastore_pokemon_gen6.MethodUploadPokemon, nil), nil
}

// ---- browsing ----------------------------------------------------------------------------------------------------

func search(err error, packet nex.PacketInterface, callID uint32, param gts_types.GlobalTradeStationSearchPokemonParam) (*nex.RMCMessage, *nex.Error) {
	if err != nil {
		return badRequest(err)
	}

	length := uint32(param.ResultRange.Length)
	if length == 0 {
		length = defaultPageLength
	}
	if length > maxPageLength {
		length = maxPageLength
	}

	filter := database.GTSSearchFilter{
		ExcludeOwner: senderPID(packet),
		OldestFirst:  param.ResultOrder == 1,
		Offset:       uint32(param.ResultRange.Offset),
		Length:       length,
	}
	// The search conditions are opaque numbers whose meaning depends on the index layout of the listing; they are not
	// interpreted yet, so every listing matches.
	if after := toTime(param.UploadedAfter); !after.IsZero() {
		filter.UploadedAfter = after
	}
	if before := toTime(param.UploadedBefore); !before.IsZero() {
		filter.UploadedBefore = before
	}

	records, total, dbErr := database.GTSSearch(filter)
	if dbErr != nil {
		return fail("SearchPokemon", dbErr)
	}

	result := gts_types.NewGlobalTradeStationSearchPokemonResult()
	result.TotalCount = types.NewUInt32(total)
	result.Result = make(types.List[gts_types.GlobalTradeStationData], 0, len(records))
	for _, r := range records {
		updated := types.NewDateTime(0)
		updated = updated.FromTimestamp(r.UpdatedAt)
		result.Result = append(result.Result, gts_types.GlobalTradeStationData{
			DataID:      types.NewUInt64(r.DataID),
			OwnerID:     types.NewPID(r.OwnerPID),
			UpdatedTime: updated,
			IndexData:   types.QBuffer(r.IndexData),
			Version:     types.NewUInt32(r.Version),
		})
	}

	return reply(packet, callID, datastore_pokemon_gen6.MethodSearchPokemon, func(out *nex.ByteStreamOut) {
		result.WriteTo(out)
	}), nil
}

func toTime(dt types.DateTime) time.Time {
	if uint64(dt) == 0 {
		return time.Time{}
	}
	return dt.Standard()
}

func downloadOther(err error, packet nex.PacketInterface, callID uint32, param gts_types.GlobalTradeStationDownloadOtherPokemonParam) (*nex.RMCMessage, *nex.Error) {
	if err != nil {
		return badRequest(err)
	}

	record, dbErr := database.GTSGet(uint64(param.PrepareUploadKey.DataID))
	if dbErr != nil {
		return fail("DownloadOtherPokemon", dbErr)
	}
	if record.State != database.GTSStateListed && record.State != database.GTSStateTraded {
		return fail("DownloadOtherPokemon", database.ErrGTSNotFound)
	}

	result := downloadResult(record, record.IndexData, record.PokemonData)

	return reply(packet, callID, datastore_pokemon_gen6.MethodDownloadOtherPokemon, func(out *nex.ByteStreamOut) {
		result.WriteTo(out)
	}), nil
}

// ---- trading -----------------------------------------------------------------------------------------------------

func prepareTrade(err error, packet nex.PacketInterface, callID uint32, param gts_types.GlobalTradeStationPrepareTradePokemonParam) (*nex.RMCMessage, *nex.Error) {
	if err != nil {
		return badRequest(err)
	}

	targetID := uint64(param.TradeKey.DataID)
	reservation := randomUint64()

	if dbErr := database.GTSReserveForTrade(targetID, uint32(param.TradeKey.Version), senderPID(packet), reservation); dbErr != nil {
		return fail("PrepareTradePokemon", dbErr)
	}

	target, dbErr := database.GTSGet(targetID)
	if dbErr != nil {
		return fail("PrepareTradePokemon", dbErr)
	}

	result := gts_types.GlobalTradeStationPrepareTradePokemonResult{
		Result:          downloadResult(target, target.IndexData, target.PokemonData),
		PrepareTradeKey: gts_types.GlobalTradeStationRecordKey{DataID: types.NewUInt64(targetID), Password: types.NewUInt64(reservation)},
	}

	return reply(packet, callID, datastore_pokemon_gen6.MethodPrepareTradePokemon, func(out *nex.ByteStreamOut) {
		result.WriteTo(out)
	}), nil
}

func trade(err error, packet nex.PacketInterface, callID uint32, param gts_types.GlobalTradeStationTradePokemonParam) (*nex.RMCMessage, *nex.Error) {
	if err != nil {
		return badRequest(err)
	}

	pid := senderPID(packet)
	targetID := uint64(param.TradeKey.DataID)

	myRecord, dbErr := database.GTSCompleteTrade(
		targetID, uint32(param.TradeKey.Version), pid, uint64(param.PrepareTradeKey.Password),
		uint64(param.PrepareUploadKey.DataID), uint16(param.Period),
		[]byte(param.IndexData), []byte(param.PokemonData), []byte(param.Signature),
	)
	if dbErr != nil {
		return fail("TradePokemon", dbErr)
	}

	// What the trader receives is what the depositor had listed
	target, dbErr := database.GTSGet(targetID)
	if dbErr != nil {
		return fail("TradePokemon", dbErr)
	}

	result := gts_types.GlobalTradeStationTradePokemonResult{
		Result:   downloadResult(target, target.IndexData, target.PokemonData),
		MyDataID: types.NewUInt64(myRecord),
	}

	return reply(packet, callID, datastore_pokemon_gen6.MethodTradePokemon, func(out *nex.ByteStreamOut) {
		result.WriteTo(out)
	}), nil
}

// ---- the depositor's side ----------------------------------------------------------------------------------------

func downloadMine(err error, packet nex.PacketInterface, callID uint32, param gts_types.GlobalTradeStationDownloadMyPokemonParam) (*nex.RMCMessage, *nex.Error) {
	if err != nil {
		return badRequest(err)
	}

	record, dbErr := database.GTSGetOwned(uint64(param.PrepareUploadKey.DataID), uint64(param.PrepareUploadKey.Password), senderPID(packet))
	if dbErr != nil {
		return fail("DownloadMyPokemon", dbErr)
	}

	// Once traded the depositor gets the Pokemon that was left in exchange, until then they get their own back
	result := gts_types.GlobalTradeStationDownloadMyPokemonResult{IsTraded: types.NewBool(record.State == database.GTSStateTraded)}
	if record.State == database.GTSStateTraded {
		result.Result = downloadResult(record, record.ReceivedIndex, record.ReceivedPokemon)
	} else {
		result.Result = downloadResult(record, record.IndexData, record.PokemonData)
	}

	return reply(packet, callID, datastore_pokemon_gen6.MethodDownloadMyPokemon, func(out *nex.ByteStreamOut) {
		result.WriteTo(out)
	}), nil
}

func remove(err error, packet nex.PacketInterface, callID uint32, param gts_types.GlobalTradeStationDeletePokemonParam) (*nex.RMCMessage, *nex.Error) {
	if err != nil {
		return badRequest(err)
	}

	if dbErr := database.GTSDelete(uint64(param.PrepareUploadKey.DataID), uint64(param.PrepareUploadKey.Password), senderPID(packet)); dbErr != nil {
		return fail("DeletePokemon", dbErr)
	}

	return reply(packet, callID, datastore_pokemon_gen6.MethodDeletePokemon, nil), nil
}

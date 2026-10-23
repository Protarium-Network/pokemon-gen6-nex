package nex_subscription

import (
	"github.com/PretendoNetwork/nex-go/v2"
	"github.com/PretendoNetwork/nex-go/v2/types"
	subscription "github.com/PretendoNetwork/nex-protocols-go/v2/subscription"
	subscription_types "github.com/PretendoNetwork/nex-protocols-go/v2/subscription/types"
	"github.com/PretendoNetwork/pokemon-gen6/globals"
)

func GetActivePlayerSubscriptionData(err error, packet nex.PacketInterface, callID uint32, unk1, unk2, unk3 types.UInt32) (*nex.RMCMessage, *nex.Error) {
	if err != nil {
		globals.Logger.Error(err.Error())
		return nil, nex.NewError(nex.ResultCodes.Core.InvalidArgument, err.Error())
	}

	client := packet.Sender()

	endpoint := client.Endpoint().(*nex.PRUDPEndPoint)

	rmcResponseStream := nex.NewByteStreamOut(endpoint.LibraryVersions(), endpoint.ByteStreamSettings())

	globals.Logger.Infof("GetActivePlayerSubscriptionData | unk1: %d | unk2: %d | unk3: %d", unk1, unk2, unk3)
	activePlayerSubscriptionList := types.NewList[subscription_types.ActivePlayerSubscriptionData]()

	// this seems arbitrary but it seems to make sense in the context of packet dumps
	// the presence of two numbers may have something to do with the unknown field in ActivePlayerSubscriptionData
	count := unk1 + unk2
	if count > 100 || count == 0 {
		count = 100
	}

	// Only players of the same game group (unk3 matches what they published at creation), never the player who asks
	for _, player := range globals.SubscriptionTimeline.ActivePlayers(uint32(unk3), client.PID(), int(count)) {
		activePlayerSubscription := subscription_types.NewActivePlayerSubscriptionData()

		activePlayerSubscription.Unknown = true
		activePlayerSubscription.SubscriptionData.PrincipalID = player.PID
		activePlayerSubscription.SubscriptionData.Unknown = player.Data.Unknown

		activePlayerSubscriptionList = append(activePlayerSubscriptionList, activePlayerSubscription)
	}

	globals.Logger.Infof("GetActivePlayerSubscriptionData | group %d: %d players returned (%d online in total)", unk3, len(activePlayerSubscriptionList), globals.SubscriptionTimeline.Len())

	activePlayerSubscriptionList.WriteTo(rmcResponseStream)

	rmcResponseBody := rmcResponseStream.Bytes()

	rmcResponse := nex.NewRMCSuccess(endpoint, rmcResponseBody)
	rmcResponse.ProtocolID = subscription.ProtocolID
	rmcResponse.MethodID = subscription.MethodGetActivePlayerSubscriptionData
	rmcResponse.CallID = callID

	return rmcResponse, nil
}

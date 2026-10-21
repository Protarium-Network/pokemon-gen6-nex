package nex

import (
	datastore_pokemon_gen6 "github.com/PretendoNetwork/nex-protocols-go/v2/datastore/pokemon-gen6"
	rating "github.com/PretendoNetwork/nex-protocols-go/v2/rating"
	subscription "github.com/PretendoNetwork/nex-protocols-go/v2/subscription"
	"github.com/PretendoNetwork/pokemon-gen6/globals"

	nex_gts "github.com/PretendoNetwork/pokemon-gen6/nex/gts"
	nex_rating "github.com/PretendoNetwork/pokemon-gen6/nex/rating"
	nex_subscription "github.com/PretendoNetwork/pokemon-gen6/nex/subscription"
)

func registerSecureServerNEXProtocols() {
	// The Pokemon Gen 6 flavour of DataStore wraps the regular one and adds the Global Trade Station methods
	datastoreProtocol := datastore_pokemon_gen6.NewProtocol(globals.SecureEndpoint)
	globals.SecureEndpoint.RegisterServiceProtocol(datastoreProtocol)
	nex_gts.Register(datastoreProtocol)

	subscriptionProtocol := subscription.NewProtocol()
	globals.SecureEndpoint.RegisterServiceProtocol(subscriptionProtocol)

	subscriptionProtocol.AddTarget = nex_subscription.AddTarget
	subscriptionProtocol.ClearMySubscriptionData = nex_subscription.ClearMySubscriptionData
	subscriptionProtocol.DeleteTarget = nex_subscription.DeleteTarget
	subscriptionProtocol.ClearTarget = nex_subscription.ClearTarget
	subscriptionProtocol.CreateMySubscriptionData = nex_subscription.CreateMySubscriptionData
	subscriptionProtocol.GetActivePlayerSubscriptionData = nex_subscription.GetActivePlayerSubscriptionData
	subscriptionProtocol.GetFriendSubscriptionData = nex_subscription.GetFriendSubscriptionData
	subscriptionProtocol.GetPrivacyLevel = nex_subscription.GetPrivacyLevel
	subscriptionProtocol.GetSubscriptionData = nex_subscription.GetSubscriptionData
	subscriptionProtocol.GetTargetSubscriptionData = nex_subscription.GetTargetSubscriptionData
	subscriptionProtocol.ReplaceTargetAndGetSubscriptionData = nex_subscription.ReplaceTargetAndGetSubscriptionData
	subscriptionProtocol.SetPrivacyLevel = nex_subscription.SetPrivacyLevel
	subscriptionProtocol.UpdateMySubscriptionData = nex_subscription.UpdateMySubscriptionData

	ratingProtocol := rating.NewProtocol()
	globals.SecureEndpoint.RegisterServiceProtocol(ratingProtocol)

	ratingProtocol.Unk1 = nex_rating.Unk1
	ratingProtocol.Unk2 = nex_rating.Unk2
}

package globals

import (
	"github.com/PretendoNetwork/nex-go/v2/types"
	common_globals "github.com/PretendoNetwork/nex-protocols-common-go/v2/globals"
	notification_types "github.com/PretendoNetwork/nex-protocols-go/v2/notifications/types"
)

func HandleSubscriptionClearNotification(source types.PID, notifyTargets bool) {
	handleSubscriptionNotification(source, 112000, notifyTargets)
}

func HandleSubscriptionChangeNotification(source types.PID, notifyTargets bool) {
	handleSubscriptionNotification(source, 112001, notifyTargets)
}

// 112002 seems to trigger a GetActivePlayerSubscriptionData request from the client, notification sent from the secure account pid (2)
// 112003-112009 are either rare or do not exist

func handleSubscriptionNotification(source types.PID, notificationType uint32, notifyTargets bool) {
	notificationEvent := notification_types.NewNotificationEvent()
	notificationEvent.PIDSource = source
	notificationEvent.Type = types.UInt32(notificationType)

	if SecureEndpoint == nil {
		return // not serving yet (unit tests)
	}

	targets := GetOnlineFriendPIDs(uint32(source))

	if notifyTargets {
		// if source is targetted by any other user, send the other user a notification
		for _, watcher := range SubscriptionTargets.Watchers(source) {
			targets = append(targets, uint64(watcher))
		}
	}

	common_globals.SendNotificationEvent(SecureEndpoint, notificationEvent, targets)
}

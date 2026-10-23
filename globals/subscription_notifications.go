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

// 112002 triggers a GetActivePlayerSubscriptionData request from the client, notification sent from the secure account pid (2)
const refreshActivePlayersNotification = 112002

// HandleSubscriptionRefreshNotification tells players to ask for the list of active players again. A player who was online
// before somebody else arrived would otherwise never learn about the newcomer.
func HandleSubscriptionRefreshNotification(players []types.PID) {
	if SecureEndpoint == nil || len(players) == 0 {
		return // not serving yet (unit tests), or nobody to tell
	}

	notificationEvent := notification_types.NewNotificationEvent()
	notificationEvent.PIDSource = types.NewPID(2)
	notificationEvent.Type = types.UInt32(refreshActivePlayersNotification)

	targets := make([]uint64, 0, len(players))
	for _, pid := range players {
		targets = append(targets, uint64(pid))
	}

	common_globals.SendNotificationEvent(SecureEndpoint, notificationEvent, targets)
}
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

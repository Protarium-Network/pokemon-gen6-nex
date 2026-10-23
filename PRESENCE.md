# Presence (subscription) fixes

The players-online lists of Pokemon X/Y and OR/AS use the Subscription protocol. Changes on top of upstream:

- The presence tables are guarded by locks: every connection is served from its own goroutine.
- A presence entry remembers the connection that published it. When an old connection of a player ends after the player has
  reconnected, it no longer removes the presence (and the targets) of the new session.
- `CreateMySubscriptionData` replaces an existing entry instead of keeping the old one, and tells the players watching.
- `ReplaceTargetAndGetSubscriptionData` keeps the new targets even if the player had no list yet.
- `DeleteTarget` actually removes the targets.
- The active player list only contains players of the same game group (the number sent at creation and asked again by the
  client: X/Y and OR/AS differ) and never the player who asks.

Not covered: friend presence (`GetFriendSubscriptionData`) asks the friends server over gRPC for the friend list of the
player. It only knows the friendships stored in that friends server; friendships kept by another friends server (for instance
Pretendo's) are not visible to it.

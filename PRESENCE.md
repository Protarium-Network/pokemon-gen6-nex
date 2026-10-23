# Presence (subscription) fixes

The players-online lists of Pokemon X/Y and OR/AS use the Subscription protocol. Changes on top of upstream:

- The presence tables are guarded by locks: every connection is served from its own goroutine.
- A presence entry remembers the connection that published it. When an old connection of a player ends after the player has
  reconnected, it no longer removes the presence (and the targets) of the new session.
- `CreateMySubscriptionData` replaces an existing entry instead of keeping the old one, and tells the players watching.
- `ReplaceTargetAndGetSubscriptionData` keeps the new targets even if the player had no list yet.
- `DeleteTarget` actually removes the targets.
- When a player publishes their presence, every other player present gets notification 112002 (sent from the secure account, pid 2),
  which makes the game ask for the active players again. Without it a player who was online first never learns about the next one.
- The active player list puts the players of the same game group first (the number sent at creation and asked again by the
  client: X/Y and OR/AS use different values), then the others, and never lists the player who asks. X/Y and OR/AS can trade and
  battle together, so players of the other group stay visible.

Not covered: friend presence (`GetFriendSubscriptionData`) asks the friends server over gRPC for the friend list of the
player. It only knows the friendships stored in that friends server; friendships kept by another friends server (for instance
Pretendo's) are not visible to it.

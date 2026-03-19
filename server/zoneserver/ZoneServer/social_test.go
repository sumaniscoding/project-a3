package main

import (
	"testing"
)

func resetSocialStateForTests() {
	partyMu.Lock()
	parties = map[string]*Party{}
	partyByMember = map[string]string{}
	partyInvites = map[string]PartyInvite{}
	partySeq = 0
	partyMu.Unlock()

	guildMu.Lock()
	guilds = map[string]*Guild{}
	guildInvites = map[string]GuildInvite{}
	guildMu.Unlock()

	friendMu.Lock()
	friendInvites = map[string]FriendInvite{}
	friendMu.Unlock()

	// Keep test isolation deterministic for helpers that consult online-session state.
	resetSessionStateForTests()
}

func resetSessionStateForTests() {
	sessionsMu.Lock()
	sessions = map[*ClientSession]bool{}
	sessionsByName = map[string]*ClientSession{}
	sessionsMu.Unlock()
}

func TestPartyInviteAcceptLeave(t *testing.T) {
	resetSocialStateForTests()

	if _, ok, reason := partyInvite("Alice", "Bob"); !ok {
		t.Fatalf("partyInvite failed: %s", reason)
	}
	accept, ok, reason := partyAccept("Bob", "Alice")
	if !ok {
		t.Fatalf("partyAccept failed: %s", reason)
	}
	partyMap := toMap(accept["party"])
	if toInt(partyMap, "size") != 2 {
		t.Fatalf("expected party size 2, got %v", partyMap["size"])
	}

	leave, ok, reason := partyLeave("Bob")
	if !ok {
		t.Fatalf("partyLeave failed: %s", reason)
	}
	if dissolved, _ := leave["dissolved"].(bool); !dissolved {
		t.Fatalf("expected party dissolved after second member left")
	}
}

func TestGuildCreateJoinLeave(t *testing.T) {
	resetSocialStateForTests()

	result, ok, reason := guildCreate("Alice", "Knights")
	if !ok {
		t.Fatalf("guildCreate failed: %s", reason)
	}
	if toString(result, "role") != "leader" {
		t.Fatalf("expected creator role leader")
	}
	if _, ok, reason := guildJoin("Bob", "Knights"); !ok {
		t.Fatalf("guildJoin failed: %s", reason)
	}

	list := guildListPayload()
	if toInt(list, "count") != 1 {
		t.Fatalf("expected 1 guild, got %v", list["count"])
	}

	leaveLeader, ok, reason := guildLeave("Alice", "Knights")
	if !ok {
		t.Fatalf("guildLeave owner failed: %s", reason)
	}
	if disbanded, _ := leaveLeader["disbanded"].(bool); disbanded {
		t.Fatalf("expected guild to persist with Bob as leader after leader leaves first")
	}
	if toString(leaveLeader, "leader") != "Bob" {
		t.Fatalf("expected leadership transfer to Bob")
	}
	if _, ok, reason := guildLeave("Bob", "Knights"); !ok {
		t.Fatalf("guildLeave final member failed: %s", reason)
	}
	list = guildListPayload()
	if toInt(list, "count") != 0 {
		t.Fatalf("expected 0 guilds after last leave, got %v", list["count"])
	}
}

func TestGuildDisbandRequiresLeader(t *testing.T) {
	resetSocialStateForTests()

	if _, ok, reason := guildCreate("Alice", "Knights"); !ok {
		t.Fatalf("guildCreate failed: %s", reason)
	}
	if _, ok, reason := guildJoin("Bob", "Knights"); !ok {
		t.Fatalf("guildJoin failed: %s", reason)
	}
	if _, ok, reason := guildJoin("Cara", "Knights"); !ok {
		t.Fatalf("guildJoin failed: %s", reason)
	}
	if _, ok, reason := guildDisband("Bob", "Knights"); ok || reason != "NOT_GUILD_LEADER" {
		t.Fatalf("expected NOT_GUILD_LEADER, got ok=%v reason=%s", ok, reason)
	}
	result, ok, reason := guildDisband("Alice", "Knights")
	if !ok {
		t.Fatalf("guildDisband failed: %s", reason)
	}
	if disbanded, _ := result["disbanded"].(bool); !disbanded {
		t.Fatalf("expected disbanded=true")
	}
	if members, ok := result["members"].([]string); !ok || len(members) != 3 {
		t.Fatalf("expected 3 members in disband result, got %#v", result["members"])
	}
	if role := guildRoleOfMember("Bob", "Knights"); role != "" {
		t.Fatalf("expected empty role after disband, got %q", role)
	}
	list := guildListPayload()
	if toInt(list, "count") != 0 {
		t.Fatalf("expected 0 guilds after disband, got %v", list["count"])
	}
}

func TestGuildInviteAndAccept(t *testing.T) {
	resetSocialStateForTests()

	if _, ok, reason := guildCreate("Alice", "Knights"); !ok {
		t.Fatalf("guildCreate failed: %s", reason)
	}
	if _, ok, reason := guildInvite("Alice", "Bob", "Knights"); !ok {
		t.Fatalf("guildInvite failed: %s", reason)
	}
	if _, ok, reason := guildAccept("Bob", "Alice"); !ok {
		t.Fatalf("guildAccept failed: %s", reason)
	}
	if role := guildRoleOfMember("Bob", "Knights"); role != "member" {
		t.Fatalf("expected Bob role member, got %q", role)
	}
}

func TestGuildInviteRequiresLeader(t *testing.T) {
	resetSocialStateForTests()

	if _, ok, reason := guildCreate("Alice", "Knights"); !ok {
		t.Fatalf("guildCreate failed: %s", reason)
	}
	if _, ok, reason := guildJoin("Bob", "Knights"); !ok {
		t.Fatalf("guildJoin failed: %s", reason)
	}
	if _, ok, reason := guildInvite("Bob", "Cara", "Knights"); ok || reason != "NOT_GUILD_LEADER" {
		t.Fatalf("expected NOT_GUILD_LEADER, got ok=%v reason=%s", ok, reason)
	}
}

func TestGuildInviteBlocked(t *testing.T) {
	resetSocialStateForTests()
	resetSessionStateForTests()

	if _, ok, reason := guildCreate("Alice", "Knights"); !ok {
		t.Fatalf("guildCreate failed: %s", reason)
	}

	alice := &ClientSession{
		Authenticated: true,
		Character:     &Character{Name: "Alice", Blocks: map[string]bool{}},
		Active:        true,
	}
	bob := &ClientSession{
		Authenticated: true,
		Character:     &Character{Name: "Bob", Blocks: map[string]bool{}},
		Active:        true,
	}
	registerSession(alice)
	registerSession(bob)
	bindSessionCharacterName(alice, "Alice")
	bindSessionCharacterName(bob, "Bob")

	bob.Character.Blocks["Alice"] = true
	if _, ok, reason := guildInvite("Alice", "Bob", "Knights"); ok || reason != "BLOCKED" {
		t.Fatalf("expected BLOCKED (target blocks inviter), got ok=%v reason=%s", ok, reason)
	}

	bob.Character.Blocks = map[string]bool{}
	alice.Character.Blocks["Bob"] = true
	if _, ok, reason := guildInvite("Alice", "Bob", "Knights"); ok || reason != "BLOCKED" {
		t.Fatalf("expected BLOCKED (inviter blocks target), got ok=%v reason=%s", ok, reason)
	}
}

func TestGuildAcceptBlocked(t *testing.T) {
	resetSocialStateForTests()
	resetSessionStateForTests()

	if _, ok, reason := guildCreate("Alice", "Knights"); !ok {
		t.Fatalf("guildCreate failed: %s", reason)
	}

	alice := &ClientSession{
		Authenticated: true,
		Character:     &Character{Name: "Alice", Blocks: map[string]bool{}},
		Active:        true,
	}
	bob := &ClientSession{
		Authenticated: true,
		Character:     &Character{Name: "Bob", Blocks: map[string]bool{}},
		Active:        true,
	}
	registerSession(alice)
	registerSession(bob)
	bindSessionCharacterName(alice, "Alice")
	bindSessionCharacterName(bob, "Bob")

	if _, ok, reason := guildInvite("Alice", "Bob", "Knights"); !ok {
		t.Fatalf("guildInvite failed: %s", reason)
	}
	bob.Character.Blocks["Alice"] = true
	if _, ok, reason := guildAccept("Bob", "Alice"); ok || reason != "BLOCKED" {
		t.Fatalf("expected BLOCKED on accept (target blocks inviter), got ok=%v reason=%s", ok, reason)
	}

	bob.Character.Blocks = map[string]bool{}
	if _, ok, reason := guildInvite("Alice", "Bob", "Knights"); !ok {
		t.Fatalf("guildInvite failed: %s", reason)
	}
	alice.Character.Blocks["Bob"] = true
	if _, ok, reason := guildAccept("Bob", "Alice"); ok || reason != "BLOCKED" {
		t.Fatalf("expected BLOCKED on accept (inviter blocks target), got ok=%v reason=%s", ok, reason)
	}
}

func TestGuildInviteExpires(t *testing.T) {
	resetSocialStateForTests()

	if _, ok, reason := guildCreate("Alice", "Knights"); !ok {
		t.Fatalf("guildCreate failed: %s", reason)
	}
	if _, ok, reason := guildInvite("Alice", "Bob", "Knights"); !ok {
		t.Fatalf("guildInvite failed: %s", reason)
	}

	guildMu.Lock()
	invite := guildInvites["Bob"]
	invite.ExpiresAt = invite.ExpiresAt.Add(-2 * guildInviteTTL)
	guildInvites["Bob"] = invite
	guildMu.Unlock()

	if _, ok, reason := guildAccept("Bob", "Alice"); ok || reason != "INVITE_EXPIRED" {
		t.Fatalf("expected INVITE_EXPIRED, got ok=%v reason=%s", ok, reason)
	}
}

func TestGuildDeclineInvite(t *testing.T) {
	resetSocialStateForTests()

	if _, ok, reason := guildCreate("Alice", "Knights"); !ok {
		t.Fatalf("guildCreate failed: %s", reason)
	}
	if _, ok, reason := guildInvite("Alice", "Bob", "Knights"); !ok {
		t.Fatalf("guildInvite failed: %s", reason)
	}
	if _, ok, reason := guildDecline("Bob", "Cara"); ok || reason != "INVITE_MISMATCH" {
		t.Fatalf("expected INVITE_MISMATCH, got ok=%v reason=%s", ok, reason)
	}
	result, ok, reason := guildDecline("Bob", "Alice")
	if !ok {
		t.Fatalf("guildDecline failed: %s", reason)
	}
	if toString(result, "from") != "Alice" || toString(result, "to") != "Bob" {
		t.Fatalf("unexpected decline payload: %#v", result)
	}
	if _, ok, reason := guildDecline("Bob", "Alice"); ok || reason != "NO_INVITE" {
		t.Fatalf("expected NO_INVITE after decline, got ok=%v reason=%s", ok, reason)
	}
}

func TestGuildCancelInvite(t *testing.T) {
	resetSocialStateForTests()

	if _, ok, reason := guildCreate("Alice", "Knights"); !ok {
		t.Fatalf("guildCreate failed: %s", reason)
	}
	if _, ok, reason := guildInvite("Alice", "Bob", "Knights"); !ok {
		t.Fatalf("guildInvite failed: %s", reason)
	}
	if _, ok, reason := guildCancelInvite("Cara", "Bob", "Knights"); ok || reason != "NOT_INVITER" {
		t.Fatalf("expected NOT_INVITER, got ok=%v reason=%s", ok, reason)
	}
	result, ok, reason := guildCancelInvite("Alice", "Bob", "Knights")
	if !ok {
		t.Fatalf("guildCancelInvite failed: %s", reason)
	}
	if toString(result, "from") != "Alice" || toString(result, "to") != "Bob" {
		t.Fatalf("unexpected cancel payload: %#v", result)
	}
	if _, ok, reason := guildCancelInvite("Alice", "Bob", "Knights"); ok || reason != "NO_INVITE" {
		t.Fatalf("expected NO_INVITE after cancel, got ok=%v reason=%s", ok, reason)
	}
}

func TestGuildTransferLeader(t *testing.T) {
	resetSocialStateForTests()

	if _, ok, reason := guildCreate("Alice", "Knights"); !ok {
		t.Fatalf("guildCreate failed: %s", reason)
	}
	if _, ok, reason := guildJoin("Bob", "Knights"); !ok {
		t.Fatalf("guildJoin failed: %s", reason)
	}
	if _, ok, reason := guildTransferLeader("Alice", "Bob", "Knights"); !ok {
		t.Fatalf("guildTransferLeader failed: %s", reason)
	}
	if role := guildRoleOfMember("Alice", "Knights"); role != "member" {
		t.Fatalf("expected Alice role member after transfer, got %q", role)
	}
	if role := guildRoleOfMember("Bob", "Knights"); role != "leader" {
		t.Fatalf("expected Bob role leader after transfer, got %q", role)
	}
}

func TestGuildKickRequiresLeader(t *testing.T) {
	resetSocialStateForTests()

	if _, ok, reason := guildCreate("Alice", "Knights"); !ok {
		t.Fatalf("guildCreate failed: %s", reason)
	}
	if _, ok, reason := guildJoin("Bob", "Knights"); !ok {
		t.Fatalf("guildJoin failed: %s", reason)
	}
	if _, ok, reason := guildJoin("Cara", "Knights"); !ok {
		t.Fatalf("guildJoin failed: %s", reason)
	}
	if _, ok, reason := guildKick("Bob", "Cara", "Knights"); ok || reason != "NOT_GUILD_LEADER" {
		t.Fatalf("expected NOT_GUILD_LEADER, got ok=%v reason=%s", ok, reason)
	}
	if _, ok, reason := guildKick("Alice", "Cara", "Knights"); !ok {
		t.Fatalf("guildKick by leader failed: %s", reason)
	}
	if role := guildRoleOfMember("Cara", "Knights"); role != "" {
		t.Fatalf("expected Cara removed from guild, role=%q", role)
	}
}

func TestGuildPromoteAndDemote(t *testing.T) {
	resetSocialStateForTests()

	if _, ok, reason := guildCreate("Alice", "Knights"); !ok {
		t.Fatalf("guildCreate failed: %s", reason)
	}
	if _, ok, reason := guildJoin("Bob", "Knights"); !ok {
		t.Fatalf("guildJoin failed: %s", reason)
	}
	if _, ok, reason := guildPromote("Alice", "Bob", "Knights"); !ok {
		t.Fatalf("guildPromote failed: %s", reason)
	}
	if role := guildRoleOfMember("Bob", "Knights"); role != "officer" {
		t.Fatalf("expected Bob role officer, got %q", role)
	}
	if _, ok, reason := guildDemote("Alice", "Bob", "Knights"); !ok {
		t.Fatalf("guildDemote failed: %s", reason)
	}
	if role := guildRoleOfMember("Bob", "Knights"); role != "member" {
		t.Fatalf("expected Bob role member after demote, got %q", role)
	}
}

func TestGuildOfficerPermissions(t *testing.T) {
	resetSocialStateForTests()

	if _, ok, reason := guildCreate("Alice", "Knights"); !ok {
		t.Fatalf("guildCreate failed: %s", reason)
	}
	if _, ok, reason := guildJoin("Bob", "Knights"); !ok {
		t.Fatalf("guildJoin failed: %s", reason)
	}
	if _, ok, reason := guildJoin("Cara", "Knights"); !ok {
		t.Fatalf("guildJoin failed: %s", reason)
	}
	if _, ok, reason := guildPromote("Alice", "Bob", "Knights"); !ok {
		t.Fatalf("guildPromote failed: %s", reason)
	}
	if _, ok, reason := guildInvite("Bob", "Dane", "Knights"); !ok {
		t.Fatalf("officer guildInvite failed: %s", reason)
	}
	if _, ok, reason := guildKick("Bob", "Cara", "Knights"); !ok {
		t.Fatalf("officer guildKick on member failed: %s", reason)
	}
	if _, ok, reason := guildKick("Bob", "Alice", "Knights"); ok || reason != "INSUFFICIENT_GUILD_PERMS" {
		t.Fatalf("expected INSUFFICIENT_GUILD_PERMS, got ok=%v reason=%s", ok, reason)
	}
}

func TestSanitizeChatMessage(t *testing.T) {
	if got := sanitizeChatMessage("   "); got != "" {
		t.Fatalf("expected empty message")
	}
	in := "  hello world  "
	if got := sanitizeChatMessage(in); got != "hello world" {
		t.Fatalf("unexpected sanitized message: %q", got)
	}
}

func TestArePartyMates(t *testing.T) {
	resetSocialStateForTests()

	if _, ok, reason := partyInvite("Alice", "Bob"); !ok {
		t.Fatalf("partyInvite failed: %s", reason)
	}
	if _, ok, reason := partyAccept("Bob", "Alice"); !ok {
		t.Fatalf("partyAccept failed: %s", reason)
	}
	if !arePartyMates("Alice", "Bob") {
		t.Fatalf("expected Alice and Bob to be party mates")
	}
	if arePartyMates("Alice", "Cara") {
		t.Fatalf("did not expect Alice and Cara to be party mates")
	}
}

func TestPartyReadyAndStatus(t *testing.T) {
	resetSocialStateForTests()

	if _, ok, reason := partyInvite("Alice", "Bob"); !ok {
		t.Fatalf("partyInvite failed: %s", reason)
	}
	if _, ok, reason := partyAccept("Bob", "Alice"); !ok {
		t.Fatalf("partyAccept failed: %s", reason)
	}
	res, ok, reason := setPartyReady("Alice", true)
	if !ok {
		t.Fatalf("setPartyReady failed: %s", reason)
	}
	party := toMap(res["party"])
	ready := toMap(party["ready"])
	if toInt(ready, "ready_count") != 1 {
		t.Fatalf("expected ready_count=1, got %v", ready["ready_count"])
	}

	status, ok, reason := partyStatusForMember("Bob")
	if !ok {
		t.Fatalf("partyStatusForMember failed: %s", reason)
	}
	p := toMap(status["party"])
	r := toMap(p["ready"])
	states, ok := r["states"].(map[string]bool)
	if !ok {
		t.Fatalf("expected states map[string]bool")
	}
	if raw, ok := states["Alice"]; !ok || !raw {
		t.Fatalf("expected Alice ready=true in status")
	}
}

func TestPartyInviteBlocked(t *testing.T) {
	resetSocialStateForTests()
	resetSessionStateForTests()

	alice := &ClientSession{
		Authenticated: true,
		Character:     &Character{Name: "Alice", Blocks: map[string]bool{}},
		Active:        true,
	}
	bob := &ClientSession{
		Authenticated: true,
		Character:     &Character{Name: "Bob", Blocks: map[string]bool{}},
		Active:        true,
	}
	registerSession(alice)
	registerSession(bob)
	bindSessionCharacterName(alice, "Alice")
	bindSessionCharacterName(bob, "Bob")

	bob.Character.Blocks["Alice"] = true
	if _, ok, reason := partyInvite("Alice", "Bob"); ok || reason != "BLOCKED" {
		t.Fatalf("expected BLOCKED (target blocks inviter), got ok=%v reason=%s", ok, reason)
	}

	bob.Character.Blocks = map[string]bool{}
	alice.Character.Blocks["Bob"] = true
	if _, ok, reason := partyInvite("Alice", "Bob"); ok || reason != "BLOCKED" {
		t.Fatalf("expected BLOCKED (inviter blocks target), got ok=%v reason=%s", ok, reason)
	}
}

func TestPartyAcceptBlocked(t *testing.T) {
	resetSocialStateForTests()
	resetSessionStateForTests()

	alice := &ClientSession{
		Authenticated: true,
		Character:     &Character{Name: "Alice", Blocks: map[string]bool{}},
		Active:        true,
	}
	bob := &ClientSession{
		Authenticated: true,
		Character:     &Character{Name: "Bob", Blocks: map[string]bool{}},
		Active:        true,
	}
	registerSession(alice)
	registerSession(bob)
	bindSessionCharacterName(alice, "Alice")
	bindSessionCharacterName(bob, "Bob")

	if _, ok, reason := partyInvite("Alice", "Bob"); !ok {
		t.Fatalf("partyInvite failed: %s", reason)
	}
	bob.Character.Blocks["Alice"] = true
	if _, ok, reason := partyAccept("Bob", "Alice"); ok || reason != "BLOCKED" {
		t.Fatalf("expected BLOCKED on accept (target blocks inviter), got ok=%v reason=%s", ok, reason)
	}

	bob.Character.Blocks = map[string]bool{}
	if _, ok, reason := partyInvite("Alice", "Bob"); !ok {
		t.Fatalf("partyInvite failed: %s", reason)
	}
	alice.Character.Blocks["Bob"] = true
	if _, ok, reason := partyAccept("Bob", "Alice"); ok || reason != "BLOCKED" {
		t.Fatalf("expected BLOCKED on accept (inviter blocks target), got ok=%v reason=%s", ok, reason)
	}
}

func TestPartyKickRequiresLeader(t *testing.T) {
	resetSocialStateForTests()

	if _, ok, reason := partyInvite("Alice", "Bob"); !ok {
		t.Fatalf("partyInvite failed: %s", reason)
	}
	if _, ok, reason := partyAccept("Bob", "Alice"); !ok {
		t.Fatalf("partyAccept failed: %s", reason)
	}
	if _, ok, reason := partyKick("Bob", "Alice"); ok || reason != "NOT_PARTY_LEADER" {
		t.Fatalf("expected NOT_PARTY_LEADER, got ok=%v reason=%s", ok, reason)
	}
	result, ok, reason := partyKick("Alice", "Bob")
	if !ok {
		t.Fatalf("partyKick by leader failed: %s", reason)
	}
	if dissolved, _ := result["dissolved"].(bool); !dissolved {
		t.Fatalf("expected dissolved party after leader kicks only other member")
	}
	if remaining, ok := result["remaining"].([]string); !ok || len(remaining) != 1 || remaining[0] != "Alice" {
		t.Fatalf("expected remaining=['Alice'], got %#v", result["remaining"])
	}
}

func TestPartyTransferLeader(t *testing.T) {
	resetSocialStateForTests()

	if _, ok, reason := partyInvite("Alice", "Bob"); !ok {
		t.Fatalf("partyInvite failed: %s", reason)
	}
	if _, ok, reason := partyAccept("Bob", "Alice"); !ok {
		t.Fatalf("partyAccept failed: %s", reason)
	}
	if _, ok, reason := partyTransferLeader("Bob", "Alice"); ok || reason != "NOT_PARTY_LEADER" {
		t.Fatalf("expected NOT_PARTY_LEADER, got ok=%v reason=%s", ok, reason)
	}
	result, ok, reason := partyTransferLeader("Alice", "Bob")
	if !ok {
		t.Fatalf("partyTransferLeader failed: %s", reason)
	}
	party := toMap(result["party"])
	if toString(party, "leader") != "Bob" {
		t.Fatalf("expected leader Bob, got %v", party["leader"])
	}
	if _, ok, reason := partyTransferLeader("Alice", "Cara"); ok || reason != "NOT_PARTY_LEADER" {
		t.Fatalf("expected NOT_PARTY_LEADER after transfer, got ok=%v reason=%s", ok, reason)
	}
}

func TestPartyDisbandRequiresLeader(t *testing.T) {
	resetSocialStateForTests()

	if _, ok, reason := partyInvite("Alice", "Bob"); !ok {
		t.Fatalf("partyInvite failed: %s", reason)
	}
	if _, ok, reason := partyAccept("Bob", "Alice"); !ok {
		t.Fatalf("partyAccept failed: %s", reason)
	}
	if _, ok, reason := partyDisband("Bob"); ok || reason != "NOT_PARTY_LEADER" {
		t.Fatalf("expected NOT_PARTY_LEADER, got ok=%v reason=%s", ok, reason)
	}
	result, ok, reason := partyDisband("Alice")
	if !ok {
		t.Fatalf("partyDisband failed: %s", reason)
	}
	if disbanded, _ := result["disbanded"].(bool); !disbanded {
		t.Fatalf("expected disbanded=true")
	}
	if members, ok := result["members"].([]string); !ok || len(members) != 2 {
		t.Fatalf("expected 2 members in disband result, got %#v", result["members"])
	}
	if _, ok, reason := partyStatusForMember("Alice"); ok || reason != "NOT_IN_PARTY" {
		t.Fatalf("expected NOT_IN_PARTY after disband, got ok=%v reason=%s", ok, reason)
	}
}

func TestPartyInviteExpires(t *testing.T) {
	resetSocialStateForTests()

	if _, ok, reason := partyInvite("Alice", "Bob"); !ok {
		t.Fatalf("partyInvite failed: %s", reason)
	}
	partyMu.Lock()
	invite := partyInvites["Bob"]
	invite.ExpiresAt = invite.ExpiresAt.Add(-2 * partyInviteTTL)
	partyInvites["Bob"] = invite
	partyMu.Unlock()

	if _, ok, reason := partyAccept("Bob", "Alice"); ok || reason != "INVITE_EXPIRED" {
		t.Fatalf("expected INVITE_EXPIRED, got ok=%v reason=%s", ok, reason)
	}
}

func TestPartyDeclineInvite(t *testing.T) {
	resetSocialStateForTests()

	if _, ok, reason := partyInvite("Alice", "Bob"); !ok {
		t.Fatalf("partyInvite failed: %s", reason)
	}
	if _, ok, reason := partyDecline("Bob", "Cara"); ok || reason != "INVITE_MISMATCH" {
		t.Fatalf("expected INVITE_MISMATCH, got ok=%v reason=%s", ok, reason)
	}
	result, ok, reason := partyDecline("Bob", "Alice")
	if !ok {
		t.Fatalf("partyDecline failed: %s", reason)
	}
	if toString(result, "from") != "Alice" || toString(result, "to") != "Bob" {
		t.Fatalf("unexpected decline payload: %#v", result)
	}
	if _, ok, reason := partyDecline("Bob", "Alice"); ok || reason != "NO_INVITE" {
		t.Fatalf("expected NO_INVITE after decline, got ok=%v reason=%s", ok, reason)
	}
}

func TestPartyCancelInvite(t *testing.T) {
	resetSocialStateForTests()

	if _, ok, reason := partyInvite("Alice", "Bob"); !ok {
		t.Fatalf("partyInvite failed: %s", reason)
	}
	if _, ok, reason := partyCancelInvite("Cara", "Bob"); ok || reason != "NOT_INVITER" {
		t.Fatalf("expected NOT_INVITER, got ok=%v reason=%s", ok, reason)
	}
	result, ok, reason := partyCancelInvite("Alice", "Bob")
	if !ok {
		t.Fatalf("partyCancelInvite failed: %s", reason)
	}
	if toString(result, "from") != "Alice" || toString(result, "to") != "Bob" {
		t.Fatalf("unexpected cancel payload: %#v", result)
	}
	if _, ok, reason := partyCancelInvite("Alice", "Bob"); ok || reason != "NO_INVITE" {
		t.Fatalf("expected NO_INVITE after cancel, got ok=%v reason=%s", ok, reason)
	}
}

func TestPartyLeaveDissolveReturnsRemaining(t *testing.T) {
	resetSocialStateForTests()

	if _, ok, reason := partyInvite("Alice", "Bob"); !ok {
		t.Fatalf("partyInvite failed: %s", reason)
	}
	if _, ok, reason := partyAccept("Bob", "Alice"); !ok {
		t.Fatalf("partyAccept failed: %s", reason)
	}
	result, ok, reason := partyLeave("Bob")
	if !ok {
		t.Fatalf("partyLeave failed: %s", reason)
	}
	if dissolved, _ := result["dissolved"].(bool); !dissolved {
		t.Fatalf("expected dissolved party")
	}
	if remaining, ok := result["remaining"].([]string); !ok || len(remaining) != 1 || remaining[0] != "Alice" {
		t.Fatalf("expected remaining=['Alice'], got %#v", result["remaining"])
	}
}

func TestGuildMembersPayloadReflectsOnlineState(t *testing.T) {
	resetSocialStateForTests()
	resetSessionStateForTests()

	if _, ok, reason := guildCreate("Alice", "Knights"); !ok {
		t.Fatalf("guildCreate failed: %s", reason)
	}
	if _, ok, reason := guildJoin("Bob", "Knights"); !ok {
		t.Fatalf("guildJoin failed: %s", reason)
	}

	s := &ClientSession{
		Authenticated: true,
		Character:     &Character{Name: "Alice", Guild: "Knights"},
		World:         worlds[World1],
		Active:        true,
	}
	registerSession(s)
	bindSessionCharacterName(s, "Alice")

	payload, ok := guildMembersPayload("Knights")
	if !ok {
		t.Fatalf("expected payload")
	}
	if toInt(payload, "count") != 2 {
		t.Fatalf("expected 2 guild members, got %v", payload["count"])
	}
}

func TestFriendRequestAcceptLifecycle(t *testing.T) {
	resetSocialStateForTests()
	resetSessionStateForTests()

	alice := &ClientSession{
		Authenticated: true,
		Character:     &Character{Name: "Alice", Friends: map[string]bool{}},
		World:         worlds[World1],
		Active:        true,
	}
	bob := &ClientSession{
		Authenticated: true,
		Character:     &Character{Name: "Bob", Friends: map[string]bool{}},
		World:         worlds[World1],
		Active:        true,
	}
	registerSession(alice)
	registerSession(bob)
	bindSessionCharacterName(alice, "Alice")
	bindSessionCharacterName(bob, "Bob")

	if _, ok, reason := friendRequest("Alice", "Bob"); !ok {
		t.Fatalf("friendRequest failed: %s", reason)
	}
	if _, ok, reason := friendAccept("Bob", "Alice"); !ok {
		t.Fatalf("friendAccept failed: %s", reason)
	}
	if !alice.Character.Friends["Bob"] || !bob.Character.Friends["Alice"] {
		t.Fatalf("expected symmetric friendship after accept")
	}

	if _, ok, reason := friendRemove("Alice", "Bob"); !ok {
		t.Fatalf("friendRemove failed: %s", reason)
	}
	if alice.Character.Friends["Bob"] || bob.Character.Friends["Alice"] {
		t.Fatalf("expected friendship removed from both online sessions")
	}
}

func TestFriendRequestRejectsOfflineTarget(t *testing.T) {
	resetSocialStateForTests()
	resetSessionStateForTests()

	alice := &ClientSession{
		Authenticated: true,
		Character:     &Character{Name: "Alice", Friends: map[string]bool{}},
		World:         worlds[World1],
		Active:        true,
	}
	registerSession(alice)
	bindSessionCharacterName(alice, "Alice")

	if _, ok, reason := friendRequest("Alice", "Nobody"); ok || reason != "TARGET_OFFLINE" {
		t.Fatalf("expected TARGET_OFFLINE, got ok=%v reason=%s", ok, reason)
	}
}

func TestFriendAcceptInviteExpires(t *testing.T) {
	resetSocialStateForTests()
	resetSessionStateForTests()

	alice := &ClientSession{
		Authenticated: true,
		Character:     &Character{Name: "Alice", Friends: map[string]bool{}},
		World:         worlds[World1],
		Active:        true,
	}
	bob := &ClientSession{
		Authenticated: true,
		Character:     &Character{Name: "Bob", Friends: map[string]bool{}},
		World:         worlds[World1],
		Active:        true,
	}
	registerSession(alice)
	registerSession(bob)
	bindSessionCharacterName(alice, "Alice")
	bindSessionCharacterName(bob, "Bob")

	if _, ok, reason := friendRequest("Alice", "Bob"); !ok {
		t.Fatalf("friendRequest failed: %s", reason)
	}
	friendMu.Lock()
	invite := friendInvites["Bob"]
	invite.ExpiresAt = invite.ExpiresAt.Add(-2 * friendInviteTTL)
	friendInvites["Bob"] = invite
	friendMu.Unlock()

	if _, ok, reason := friendAccept("Bob", "Alice"); ok || reason != "INVITE_EXPIRED" {
		t.Fatalf("expected INVITE_EXPIRED, got ok=%v reason=%s", ok, reason)
	}
}

func TestFriendDeclineInvite(t *testing.T) {
	resetSocialStateForTests()
	resetSessionStateForTests()

	alice := &ClientSession{
		Authenticated: true,
		Character:     &Character{Name: "Alice", Friends: map[string]bool{}},
		World:         worlds[World1],
		Active:        true,
	}
	bob := &ClientSession{
		Authenticated: true,
		Character:     &Character{Name: "Bob", Friends: map[string]bool{}},
		World:         worlds[World1],
		Active:        true,
	}
	registerSession(alice)
	registerSession(bob)
	bindSessionCharacterName(alice, "Alice")
	bindSessionCharacterName(bob, "Bob")

	if _, ok, reason := friendRequest("Alice", "Bob"); !ok {
		t.Fatalf("friendRequest failed: %s", reason)
	}
	if _, ok, reason := friendDecline("Bob", "Cara"); ok || reason != "INVITE_MISMATCH" {
		t.Fatalf("expected INVITE_MISMATCH, got ok=%v reason=%s", ok, reason)
	}
	result, ok, reason := friendDecline("Bob", "Alice")
	if !ok {
		t.Fatalf("friendDecline failed: %s", reason)
	}
	if toString(result, "from") != "Alice" || toString(result, "to") != "Bob" {
		t.Fatalf("unexpected decline payload: %#v", result)
	}
	if _, ok, reason := friendDecline("Bob", "Alice"); ok || reason != "NO_INVITE" {
		t.Fatalf("expected NO_INVITE after decline, got ok=%v reason=%s", ok, reason)
	}
}

func TestFriendCancelInvite(t *testing.T) {
	resetSocialStateForTests()
	resetSessionStateForTests()

	alice := &ClientSession{
		Authenticated: true,
		Character:     &Character{Name: "Alice", Friends: map[string]bool{}},
		World:         worlds[World1],
		Active:        true,
	}
	bob := &ClientSession{
		Authenticated: true,
		Character:     &Character{Name: "Bob", Friends: map[string]bool{}},
		World:         worlds[World1],
		Active:        true,
	}
	registerSession(alice)
	registerSession(bob)
	bindSessionCharacterName(alice, "Alice")
	bindSessionCharacterName(bob, "Bob")

	if _, ok, reason := friendRequest("Alice", "Bob"); !ok {
		t.Fatalf("friendRequest failed: %s", reason)
	}
	if _, ok, reason := friendCancelRequest("Cara", "Bob"); ok || reason != "NOT_INVITER" {
		t.Fatalf("expected NOT_INVITER, got ok=%v reason=%s", ok, reason)
	}
	result, ok, reason := friendCancelRequest("Alice", "Bob")
	if !ok {
		t.Fatalf("friendCancelRequest failed: %s", reason)
	}
	if toString(result, "from") != "Alice" || toString(result, "to") != "Bob" {
		t.Fatalf("unexpected cancel payload: %#v", result)
	}
	if _, ok, reason := friendCancelRequest("Alice", "Bob"); ok || reason != "NO_INVITE" {
		t.Fatalf("expected NO_INVITE after cancel, got ok=%v reason=%s", ok, reason)
	}
}

func TestFriendStatusPayloadIncludesOnline(t *testing.T) {
	resetSocialStateForTests()
	resetSessionStateForTests()
	world1 := &World{ID: World1, Name: "The Known World"}
	world2 := &World{ID: World2, Name: "The Shattered World"}

	alice := &ClientSession{
		Authenticated: true,
		Character:     &Character{Name: "Alice", Friends: map[string]bool{"Bob": true, "Cara": true}},
		World:         world1,
		Active:        true,
	}
	bob := &ClientSession{
		Authenticated: true,
		Character:     &Character{Name: "Bob", Guild: "Knights", Level: 56, Presence: "afk", Friends: map[string]bool{}},
		World:         world2,
		Active:        true,
	}
	registerSession(alice)
	registerSession(bob)
	bindSessionCharacterName(alice, "Alice")
	bindSessionCharacterName(bob, "Bob")

	payload := friendStatusPayload(alice.Character)
	if toInt(payload, "count") != 2 {
		t.Fatalf("expected count=2, got %v", payload["count"])
	}
	if toInt(payload, "online_count") != 1 {
		t.Fatalf("expected online_count=1, got %v", payload["online_count"])
	}

	entries, ok := payload["entries"].([]map[string]interface{})
	if !ok || len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %#v", payload["entries"])
	}

	entryByName := map[string]map[string]interface{}{}
	for _, e := range entries {
		entryByName[toString(e, "name")] = e
	}
	bobEntry := entryByName["Bob"]
	if bobEntry == nil || bobEntry["online"] != true || toString(bobEntry, "world") != world2.Name {
		t.Fatalf("unexpected Bob entry %#v", bobEntry)
	}
	if toString(bobEntry, "status") != "afk" {
		t.Fatalf("expected Bob status afk, got %#v", bobEntry["status"])
	}
	caraEntry := entryByName["Cara"]
	if caraEntry == nil || caraEntry["online"] != false {
		t.Fatalf("unexpected Cara entry %#v", caraEntry)
	}
	if toString(caraEntry, "status") != "offline" {
		t.Fatalf("expected Cara status offline, got %#v", caraEntry["status"])
	}
}

func TestPresenceStatusParsing(t *testing.T) {
	if status, ok := parsePresenceStatus("afk"); !ok || status != "afk" {
		t.Fatalf("expected afk parse success")
	}
	if status, ok := parsePresenceStatus("DND"); !ok || status != "dnd" {
		t.Fatalf("expected dnd parse success")
	}
	if _, ok := parsePresenceStatus("busy"); ok {
		t.Fatalf("expected busy parse failure")
	}
	if got := canonicalPresenceStatus("bogus"); got != "online" {
		t.Fatalf("expected canonical fallback online, got %s", got)
	}
}

// TestBroadcastWhisper exercises the whisper flow using captureConn stubs.
// broadcastWhisper now routes through the Redis bus (or local fallback), so
// we verify the message via DrainMessages instead of a raw net.Pipe reader.
func TestBroadcastWhisper(t *testing.T) {
	resetSessionStateForTests()

	aliceConn := &captureConn{}
	bobConn := &captureConn{}

	alice := &ClientSession{
		Conn:          aliceConn,
		Authenticated: true,
		Character:     &Character{Name: "Alice"},
		Active:        true,
	}
	bob := &ClientSession{
		Conn:          bobConn,
		Authenticated: true,
		Character:     &Character{Name: "Bob"},
		Active:        true,
	}
	registerSession(alice)
	registerSession(bob)
	bindSessionCharacterName(alice, "Alice")
	bindSessionCharacterName(bob, "Bob")

	ok, reason := broadcastWhisper(alice, "Bob", "psst")
	if !ok || reason != "OK" {
		t.Fatalf("expected whisper success, got ok=%v reason=%s", ok, reason)
	}

	// The local bus delivers synchronously when Redis is not configured.
	aliceMsgs := aliceConn.DrainMessages(t)
	bobMsgs := bobConn.DrainMessages(t)

	if len(aliceMsgs) == 0 || aliceMsgs[0].Command != RespChatMessage {
		t.Fatalf("expected CHAT_MESSAGE for alice, got %#v", aliceMsgs)
	}
	if len(bobMsgs) == 0 || bobMsgs[0].Command != RespChatMessage {
		t.Fatalf("expected CHAT_MESSAGE for bob, got %#v", bobMsgs)
	}
	alicePayload := toMap(aliceMsgs[0].Payload)
	bobPayload := toMap(bobMsgs[0].Payload)
	if toString(alicePayload, "channel") != "whisper" || toString(bobPayload, "channel") != "whisper" {
		t.Fatalf("expected whisper channel payload")
	}
	if toString(alicePayload, "to") != "Bob" || toString(bobPayload, "from") != "Alice" {
		t.Fatalf("unexpected whisper payloads")
	}
}

func TestBroadcastWhisperRejectsOfflineTarget(t *testing.T) {
	resetSessionStateForTests()

	alice := &ClientSession{
		Conn:          &captureConn{},
		Authenticated: true,
		Character:     &Character{Name: "Alice"},
		Active:        true,
	}
	registerSession(alice)
	bindSessionCharacterName(alice, "Alice")

	ok, reason := broadcastWhisper(alice, "Nobody", "hello")
	if ok || reason != "TARGET_OFFLINE" {
		t.Fatalf("expected TARGET_OFFLINE, got ok=%v reason=%s", ok, reason)
	}
}

func TestBlockPlayerLifecycle(t *testing.T) {
	resetSocialStateForTests()
	resetSessionStateForTests()

	alice := &ClientSession{
		Authenticated: true,
		Character:     &Character{Name: "Alice", Friends: map[string]bool{}, Blocks: map[string]bool{}},
		World:         worlds[World1],
		Active:        true,
	}
	bob := &ClientSession{
		Authenticated: true,
		Character:     &Character{Name: "Bob", Friends: map[string]bool{}, Blocks: map[string]bool{}},
		World:         worlds[World1],
		Active:        true,
	}
	registerSession(alice)
	registerSession(bob)
	bindSessionCharacterName(alice, "Alice")
	bindSessionCharacterName(bob, "Bob")

	alice.Character.Friends["Bob"] = true
	bob.Character.Friends["Alice"] = true

	result, ok, reason := blockPlayer("Alice", "Bob")
	if !ok {
		t.Fatalf("blockPlayer failed: %s", reason)
	}
	if !alice.Character.Blocks["Bob"] {
		t.Fatalf("expected Alice to block Bob")
	}
	if alice.Character.Friends["Bob"] || bob.Character.Friends["Alice"] {
		t.Fatalf("expected friendship removed on block")
	}
	if targetUpdated, _ := result["target_updated"].(bool); !targetUpdated {
		t.Fatalf("expected target_updated true")
	}

	if _, ok, reason := unblockPlayer("Alice", "Bob"); !ok {
		t.Fatalf("unblockPlayer failed: %s", reason)
	}
	if alice.Character.Blocks["Bob"] {
		t.Fatalf("expected Alice block removed")
	}
}

func TestFriendRequestRejectedWhenBlocked(t *testing.T) {
	resetSocialStateForTests()
	resetSessionStateForTests()

	alice := &ClientSession{
		Authenticated: true,
		Character:     &Character{Name: "Alice", Friends: map[string]bool{}, Blocks: map[string]bool{}},
		World:         worlds[World1],
		Active:        true,
	}
	bob := &ClientSession{
		Authenticated: true,
		Character:     &Character{Name: "Bob", Friends: map[string]bool{}, Blocks: map[string]bool{}},
		World:         worlds[World1],
		Active:        true,
	}
	registerSession(alice)
	registerSession(bob)
	bindSessionCharacterName(alice, "Alice")
	bindSessionCharacterName(bob, "Bob")

	bob.Character.Blocks["Alice"] = true
	if _, ok, reason := friendRequest("Alice", "Bob"); ok || reason != "BLOCKED" {
		t.Fatalf("expected BLOCKED, got ok=%v reason=%s", ok, reason)
	}
}

func TestBroadcastWhisperBlocked(t *testing.T) {
	resetSessionStateForTests()

	alice := &ClientSession{
		Conn:          &captureConn{},
		Authenticated: true,
		Character:     &Character{Name: "Alice", Blocks: map[string]bool{"Bob": true}},
		Active:        true,
	}
	bob := &ClientSession{
		Conn:          &captureConn{},
		Authenticated: true,
		Character:     &Character{Name: "Bob", Blocks: map[string]bool{}},
		Active:        true,
	}
	registerSession(alice)
	registerSession(bob)
	bindSessionCharacterName(alice, "Alice")
	bindSessionCharacterName(bob, "Bob")

	ok, reason := broadcastWhisper(alice, "Bob", "psst")
	if ok || reason != "TARGET_BLOCKED_BY_YOU" {
		t.Fatalf("expected TARGET_BLOCKED_BY_YOU, got ok=%v reason=%s", ok, reason)
	}

	alice.Character.Blocks = map[string]bool{}
	bob.Character.Blocks["Alice"] = true
	ok, reason = broadcastWhisper(alice, "Bob", "psst")
	if ok || reason != "TARGET_BLOCKED_YOU" {
		t.Fatalf("expected TARGET_BLOCKED_YOU, got ok=%v reason=%s", ok, reason)
	}
}

func TestChatDeliveryAllowedByBlockList(t *testing.T) {
	resetSessionStateForTests()

	alice := &ClientSession{
		Authenticated: true,
		Character:     &Character{Name: "Alice", Blocks: map[string]bool{}},
		Active:        true,
	}
	bob := &ClientSession{
		Authenticated: true,
		Character:     &Character{Name: "Bob", Blocks: map[string]bool{}},
		Active:        true,
	}
	registerSession(alice)
	registerSession(bob)
	bindSessionCharacterName(alice, "Alice")
	bindSessionCharacterName(bob, "Bob")

	if !chatDeliveryAllowed(alice, bob) {
		t.Fatalf("expected delivery allowed when no blocks")
	}
	bob.Character.Blocks["Alice"] = true
	if chatDeliveryAllowed(alice, bob) {
		t.Fatalf("expected delivery blocked when receiver blocks sender")
	}
	bob.Character.Blocks = map[string]bool{}
	alice.Character.Blocks["Bob"] = true
	if chatDeliveryAllowed(alice, bob) {
		t.Fatalf("expected delivery blocked when sender blocks receiver")
	}
}

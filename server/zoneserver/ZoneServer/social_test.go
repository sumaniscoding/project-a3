package main

import "testing"

func resetSocialStateForTests() {
	partyMu.Lock()
	parties = map[string]*Party{}
	partyByMember = map[string]string{}
	partyInvites = map[string]string{}
	partySeq = 0
	partyMu.Unlock()

	guildMu.Lock()
	guildRoster = map[string]map[string]bool{}
	guildMu.Unlock()
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

	if _, ok, reason := guildCreate("Alice", "Knights"); !ok {
		t.Fatalf("guildCreate failed: %s", reason)
	}
	if _, ok, reason := guildJoin("Bob", "Knights"); !ok {
		t.Fatalf("guildJoin failed: %s", reason)
	}

	list := guildListPayload()
	if toInt(list, "count") != 1 {
		t.Fatalf("expected 1 guild, got %v", list["count"])
	}

	if _, ok, reason := guildLeave("Bob", "Knights"); !ok {
		t.Fatalf("guildLeave failed: %s", reason)
	}
	if _, ok, reason := guildLeave("Alice", "Knights"); !ok {
		t.Fatalf("guildLeave owner failed: %s", reason)
	}
	list = guildListPayload()
	if toInt(list, "count") != 0 {
		t.Fatalf("expected 0 guilds after last leave, got %v", list["count"])
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

package main

import "testing"

func TestHasNearbyStorageNPC(t *testing.T) {
	worlds = DefaultWorlds()
	session := &ClientSession{
		Character: MockCharacter(),
		World:     worlds[World1],
		Position:  DefaultSpawnPosition(World1),
	}
	if !hasNearbyStorageNPC(session) {
		t.Fatalf("expected storage NPC nearby at default world1 spawn")
	}

	session.Position = Position{X: 200, Y: 0, Z: 200}
	if hasNearbyStorageNPC(session) {
		t.Fatalf("expected no nearby storage NPC when far from world1 storage keeper")
	}
}

func TestStorageViewRequiresNearbyNPC(t *testing.T) {
	resetSocialStateForTests()
	resetPersistenceRuntimeStateForTests()

	worlds = DefaultWorlds()
	conn := &captureConn{}
	session := NewSession(conn)
	session.Conn = conn
	session.Character = MockCharacter()
	ensureCharacterDefaults(session.Character)
	session.World = worlds[World1]
	session.Position = Position{X: 200, Y: 0, Z: 200}

	visible := map[*ClientSession]bool{}
	bound := ""
	peer := "storage-gate-peer"
	handled, modified := handleClientCommand(conn, session, visible, peer, &bound, ReqStorageView, nil)
	if !handled || modified {
		t.Fatalf("expected handled=true modified=false, got handled=%v modified=%v", handled, modified)
	}
	msgs := conn.DrainMessages(t)
	if len(msgs) != 1 || msgs[0].Command != RespStorageRejected || msgs[0].Payload != "STORAGE_NPC_REQUIRED" {
		t.Fatalf("expected STORAGE_REJECTED/STORAGE_NPC_REQUIRED, got %#v", msgs)
	}

	session.Position = DefaultSpawnPosition(World1)
	handled, modified = handleClientCommand(conn, session, visible, peer, &bound, ReqStorageView, nil)
	if !handled || modified {
		t.Fatalf("expected handled=true modified=false near NPC, got handled=%v modified=%v", handled, modified)
	}
	msgs = conn.DrainMessages(t)
	if len(msgs) != 1 || msgs[0].Command != RespStorageState {
		t.Fatalf("expected STORAGE_STATE near NPC, got %#v", msgs)
	}
}

package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type testAddr string

func (a testAddr) Network() string { return "test" }
func (a testAddr) String() string  { return string(a) }

type captureConn struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (c *captureConn) Read(_ []byte) (int, error)  { return 0, io.EOF }
func (c *captureConn) Close() error                { return nil }

func (c *captureConn) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.Write(p)
}

// WriteJSON satisfies WSConn interface; serialises msg as a JSON line.
func (c *captureConn) WriteJSON(v interface{}) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = c.Write(data)
	return err
}

func (c *captureConn) DrainMessages(t *testing.T) []ServerMessage {
	t.Helper()
	c.mu.Lock()
	raw := c.buf.String()
	c.buf.Reset()
	c.mu.Unlock()

	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	lines := strings.Split(raw, "\n")
	msgs := make([]ServerMessage, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var msg ServerMessage
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			t.Fatalf("failed to parse server message %q: %v", line, err)
		}
		msgs = append(msgs, msg)
	}
	return msgs
}

func issueTestToken(username string) string {
	claims := tokenClaims{
		Username: username,
		Iss:      tokenIssuer,
		Ver:      tokenVersion,
		Iat:      time.Now().UTC().Unix(),
		Exp:      time.Now().UTC().Add(30 * time.Minute).Unix(),
	}
	payload, _ := json.Marshal(claims)
	payloadEnc := base64.RawURLEncoding.EncodeToString(payload)
	return payloadEnc + "." + signTokenPayload(payloadEnc, authSecret())
}

func payloadIntMap(v interface{}) map[string]int {
	src, _ := v.(map[string]interface{})
	out := map[string]int{}
	for k, raw := range src {
		switch n := raw.(type) {
		case float64:
			out[k] = int(n)
		case int:
			out[k] = n
		}
	}
	return out
}

func payloadRecipes(v interface{}) []map[string]interface{} {
	raw, _ := v.([]interface{})
	out := make([]map[string]interface{}, 0, len(raw))
	for _, entry := range raw {
		m, _ := entry.(map[string]interface{})
		if m != nil {
			out = append(out, m)
		}
	}
	return out
}

func payloadDrops(v interface{}) []map[string]interface{} {
	raw, _ := v.([]interface{})
	out := make([]map[string]interface{}, 0, len(raw))
	for _, entry := range raw {
		m, _ := entry.(map[string]interface{})
		if m != nil {
			out = append(out, m)
		}
	}
	return out
}

func withRandIntnSequence(values []int, fn func()) {
	orig := randIntn
	idx := 0
	randIntn = func(n int) int {
		if n <= 0 {
			return 0
		}
		if len(values) == 0 {
			return 0
		}
		v := values[idx]
		if idx < len(values)-1 {
			idx++
		}
		if v < 0 {
			return 0
		}
		if v >= n {
			return n - 1
		}
		return v
	}
	defer func() { randIntn = orig }()
	fn()
}

func payloadInventoryHasID(v interface{}, itemID string) bool {
	raw, _ := v.([]interface{})
	for _, entry := range raw {
		item, _ := entry.(map[string]interface{})
		if item == nil {
			continue
		}
		if toString(item, "id") == itemID {
			return true
		}
	}
	return false
}

func payloadSkillRank(v interface{}, skillID string) int {
	skills := payloadIntMap(v)
	return skills[skillID]
}

func payloadStorageHasItemID(v interface{}, itemID string) bool {
	storage := toMap(v)
	raw, _ := storage["items"].([]interface{})
	for _, entry := range raw {
		item, _ := entry.(map[string]interface{})
		if item == nil {
			continue
		}
		if toString(item, "id") == itemID {
			return true
		}
	}
	return false
}

func TestCommandFlowAuthLootCraftState(t *testing.T) {
	resetSocialStateForTests()
	resetPersistenceRuntimeStateForTests()

	t.Setenv("A3_PERSISTENCE_MODE", "json")
	t.Setenv("A3_AUTH_SECRET", "test-auth-secret")

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd failed: %v", err)
	}
	tmpWD := t.TempDir()
	if err := os.Chdir(tmpWD); err != nil {
		t.Fatalf("chdir temp dir failed: %v", err)
	}
	defer func() { _ = os.Chdir(oldWD) }()
	resetPersistenceRuntimeStateForTests()

	worlds = DefaultWorlds()
	accountSeed := newDefaultAccount("FlowHero")
	accountSeed.WalletGold = 77
	accountSeed.Storage.Materials["bandit_scrap"] = 4
	if err := persistAccount(accountSeed); err != nil {
		t.Fatalf("persistAccount seed failed: %v", err)
	}

	conn := &captureConn{}
	session := NewSession(conn)
	session.Conn = conn
	session.Character = MockCharacter()
	ensureCharacterDefaults(session.Character)
	session.World = worlds[World1]
	session.Position = DefaultSpawnPosition(World1)
	registerSession(session)
	defer unregisterSession(session)

	visible := map[*ClientSession]bool{}
	boundName := ""
	peerKey := "test-flow-peer"

	handled, _ := handleClientCommand(conn, session, visible, peerKey, &boundName, ReqAuthToken, map[string]interface{}{
		"token": issueTestToken("FlowHero"),
		"class": "Archer",
	})
	if !handled {
		t.Fatalf("expected AUTH_TOKEN to be handled")
	}
	authMsgs := conn.DrainMessages(t)
	if len(authMsgs) != 3 || authMsgs[0].Command != RespAuthOK || authMsgs[1].Command != RespEnterOK || authMsgs[2].Command != RespState {
		t.Fatalf("unexpected auth message sequence: %#v", authMsgs)
	}
	authState := toMap(authMsgs[2].Payload)
	if toInt(authState, "wallet_gold") != 77 {
		t.Fatalf("expected wallet_gold hydrated from account store, got %v", authState["wallet_gold"])
	}
	authStorage := toMap(authState["storage"])
	authStorageMaterials := payloadIntMap(authStorage["materials"])
	if authStorageMaterials["bandit_scrap"] != 4 {
		t.Fatalf("expected storage bandit_scrap=4 from account store, got %d", authStorageMaterials["bandit_scrap"])
	}

	withWorldMob(World1, "mob_wolf_01", &MobEntity{
		ID:         "mob_wolf_01",
		Name:       "Rift Wolf",
		WorldID:    World1,
		Level:      42,
		HP:         1,
		MaxHP:      1,
		Position:   session.Position,
		RespawnSec: 600,
	}, func() {
		withFixedRandIntn(0, func() {
			handleClientCommand(conn, session, visible, peerKey, &boundName, ReqAttackMob, map[string]interface{}{
				"mob_id":   "mob_wolf_01",
				"skill_id": "burst_arrow",
			})
		})
	})

	attackMsgs := conn.DrainMessages(t)
	if len(attackMsgs) != 1 || attackMsgs[0].Command != RespMobAttackResult {
		t.Fatalf("unexpected attack messages: %#v", attackMsgs)
	}
	attackPayload := toMap(attackMsgs[0].Payload)
	if defeated, _ := attackPayload["defeated"].(bool); !defeated {
		t.Fatalf("expected defeated=true, got %#v", attackPayload["defeated"])
	}
	if attackPayload["legendary"] == nil {
		t.Fatalf("expected deterministic legendary payload on fixed RNG")
	}
	drops := payloadDrops(attackPayload["drops"])
	wolfPeltDrops := 0
	for _, drop := range drops {
		if toString(drop, "kind") == lootKindMaterial && toString(drop, "item_id") == "wolf_pelt" {
			wolfPeltDrops += toInt(drop, "qty")
		}
	}
	if wolfPeltDrops < 1 {
		t.Fatalf("expected wolf_pelt material drop, got %#v", drops)
	}

	handleClientCommand(conn, session, visible, peerKey, &boundName, ReqGetState, nil)
	stateAfterLootMsgs := conn.DrainMessages(t)
	if len(stateAfterLootMsgs) != 1 || stateAfterLootMsgs[0].Command != RespState {
		t.Fatalf("unexpected state-after-loot messages: %#v", stateAfterLootMsgs)
	}
	stateAfterLoot := toMap(stateAfterLootMsgs[0].Payload)
	materialsAfterLoot := payloadIntMap(stateAfterLoot["materials"])
	if materialsAfterLoot["wolf_pelt"] < wolfPeltDrops {
		t.Fatalf("expected wolf_pelt >= %d after loot, got %d", wolfPeltDrops, materialsAfterLoot["wolf_pelt"])
	}
	inventoryAfterLoot, _ := stateAfterLoot["inventory"].([]interface{})

	handleClientCommand(conn, session, visible, peerKey, &boundName, ReqGetRecipes, nil)
	recipeMsgs := conn.DrainMessages(t)
	if len(recipeMsgs) != 1 || recipeMsgs[0].Command != RespRecipes {
		t.Fatalf("unexpected recipes messages: %#v", recipeMsgs)
	}
	recipesPayload := toMap(recipeMsgs[0].Payload)
	recipes := payloadRecipes(recipesPayload["recipes"])
	foundWolfRecipe := false
	foundGuardianShield := false
	foundHunterHelm := false
	for _, recipe := range recipes {
		if toString(recipe, "id") == "wolfhide_bow" {
			foundWolfRecipe = true
		}
		if toString(recipe, "id") == "guardian_shield" {
			foundGuardianShield = true
		}
		if toString(recipe, "id") == "hunter_helm" {
			foundHunterHelm = true
		}
	}
	if !foundWolfRecipe {
		t.Fatalf("wolfhide_bow recipe not found in catalog")
	}
	if !foundGuardianShield {
		t.Fatalf("guardian_shield recipe not found in catalog")
	}
	if !foundHunterHelm {
		t.Fatalf("hunter_helm recipe not found in catalog")
	}

	handleClientCommand(conn, session, visible, peerKey, &boundName, ReqCraftItem, map[string]interface{}{
		"recipe_id": "wolfhide_bow",
		"qty":       1,
	})
	craftMsgs := conn.DrainMessages(t)
	if len(craftMsgs) != 1 || craftMsgs[0].Command != RespCraftOK {
		t.Fatalf("unexpected craft messages: %#v", craftMsgs)
	}
	craftPayload := toMap(craftMsgs[0].Payload)
	consumed := payloadIntMap(craftPayload["consumed"])
	if consumed["wolf_pelt"] != 1 {
		t.Fatalf("expected consumed wolf_pelt=1, got %d", consumed["wolf_pelt"])
	}

	handleClientCommand(conn, session, visible, peerKey, &boundName, ReqGetState, nil)
	finalStateMsgs := conn.DrainMessages(t)
	if len(finalStateMsgs) != 1 || finalStateMsgs[0].Command != RespState {
		t.Fatalf("unexpected final state messages: %#v", finalStateMsgs)
	}
	finalState := toMap(finalStateMsgs[0].Payload)
	materialsAfterCraft := payloadIntMap(finalState["materials"])
	if got, want := materialsAfterCraft["wolf_pelt"], materialsAfterLoot["wolf_pelt"]-consumed["wolf_pelt"]; got != want {
		t.Fatalf("unexpected wolf_pelt after craft: got=%d want=%d", got, want)
	}
	inventoryAfterCraft, _ := finalState["inventory"].([]interface{})
	if len(inventoryAfterCraft) != len(inventoryAfterLoot)+1 {
		t.Fatalf("expected inventory +1 after craft, got %d -> %d", len(inventoryAfterLoot), len(inventoryAfterCraft))
	}

	banditScrapDrops := 0
	for i := 0; i < 5; i++ {
		withWorldMob(World1, "mob_bandit_01", &MobEntity{
			ID:         "mob_bandit_01",
			Name:       "Dust Bandit",
			WorldID:    World1,
			Level:      46,
			HP:         1,
			MaxHP:      1,
			Position:   session.Position,
			RespawnSec: 600,
		}, func() {
			withFixedRandIntn(0, func() {
				handleClientCommand(conn, session, visible, peerKey, &boundName, ReqAttackMob, map[string]interface{}{
					"mob_id":   "mob_bandit_01",
					"skill_id": "burst_arrow",
				})
			})
		})

		banditMsgs := conn.DrainMessages(t)
		if len(banditMsgs) != 1 || banditMsgs[0].Command != RespMobAttackResult {
			t.Fatalf("unexpected bandit attack messages: %#v", banditMsgs)
		}
		banditPayload := toMap(banditMsgs[0].Payload)
		if defeated, _ := banditPayload["defeated"].(bool); !defeated {
			t.Fatalf("expected bandit defeat, got %#v", banditPayload["defeated"])
		}
		for _, drop := range payloadDrops(banditPayload["drops"]) {
			if toString(drop, "kind") == lootKindMaterial && toString(drop, "item_id") == "bandit_scrap" {
				banditScrapDrops += toInt(drop, "qty")
			}
		}
	}
	if banditScrapDrops < 5 {
		t.Fatalf("expected at least 5 bandit_scrap from deterministic drops, got %d", banditScrapDrops)
	}

	handleClientCommand(conn, session, visible, peerKey, &boundName, ReqCraftItem, map[string]interface{}{
		"recipe_id": "bandit_mail",
		"qty":       1,
	})
	secondCraftMsgs := conn.DrainMessages(t)
	if len(secondCraftMsgs) != 1 || secondCraftMsgs[0].Command != RespCraftOK {
		t.Fatalf("unexpected second craft messages: %#v", secondCraftMsgs)
	}
	secondCraftPayload := toMap(secondCraftMsgs[0].Payload)
	secondConsumed := payloadIntMap(secondCraftPayload["consumed"])
	if secondConsumed["bandit_scrap"] != 5 {
		t.Fatalf("expected consumed bandit_scrap=5, got %d", secondConsumed["bandit_scrap"])
	}

	handleClientCommand(conn, session, visible, peerKey, &boundName, ReqGetState, nil)
	finalState2Msgs := conn.DrainMessages(t)
	if len(finalState2Msgs) != 1 || finalState2Msgs[0].Command != RespState {
		t.Fatalf("unexpected final state2 messages: %#v", finalState2Msgs)
	}
	finalState2 := toMap(finalState2Msgs[0].Payload)
	materialsAfterSecondCraft := payloadIntMap(finalState2["materials"])
	if materialsAfterSecondCraft["bandit_scrap"] != banditScrapDrops-secondConsumed["bandit_scrap"] {
		t.Fatalf(
			"unexpected bandit_scrap after second craft: got=%d want=%d",
			materialsAfterSecondCraft["bandit_scrap"],
			banditScrapDrops-secondConsumed["bandit_scrap"],
		)
	}
	inventoryAfterSecondCraft, _ := finalState2["inventory"].([]interface{})
	foundBanditMail := false
	for _, raw := range inventoryAfterSecondCraft {
		item, _ := raw.(map[string]interface{})
		if item == nil {
			continue
		}
		if strings.HasPrefix(toString(item, "id"), "crafted_bandit_mail_") {
			foundBanditMail = true
			break
		}
	}
	if !foundBanditMail {
		t.Fatalf("expected crafted_bandit_mail item in inventory, got %#v", inventoryAfterSecondCraft)
	}
}

func TestCommandFlowAuthClassBootstrapLoadouts(t *testing.T) {
	resetSocialStateForTests()
	resetPersistenceRuntimeStateForTests()

	t.Setenv("A3_PERSISTENCE_MODE", "json")
	t.Setenv("A3_AUTH_SECRET", "test-auth-secret")

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd failed: %v", err)
	}
	tmpWD := t.TempDir()
	if err := os.Chdir(tmpWD); err != nil {
		t.Fatalf("chdir temp dir failed: %v", err)
	}
	defer func() { _ = os.Chdir(oldWD) }()
	resetPersistenceRuntimeStateForTests()

	worlds = DefaultWorlds()
	visible := map[*ClientSession]bool{}

	tests := []struct {
		inputClass     string
		expectedClass  string
		expectedWeapon string
		primarySkill   string
		expectShield   bool
	}{
		{
			inputClass:     "Archer",
			expectedClass:  "Archer",
			expectedWeapon: "starter_bow",
			primarySkill:   "precise_shot",
			expectShield:   false,
		},
		{
			inputClass:     "Mage",
			expectedClass:  "Mage",
			expectedWeapon: "starter_focus",
			primarySkill:   "arc_bolt",
			expectShield:   false,
		},
		{
			inputClass:     "Warrior",
			expectedClass:  "Warrior",
			expectedWeapon: "starter_blade",
			primarySkill:   "cleave",
			expectShield:   false,
		},
		{
			inputClass:     "healing_knight",
			expectedClass:  "Healing Knight",
			expectedWeapon: "starter_mace",
			primarySkill:   "holy_slash",
			expectShield:   true,
		},
	}

	for i, tc := range tests {
		username := "BootstrapClassHero_" + strconv.Itoa(i)
		peerKey := "bootstrap-class-peer-" + strconv.Itoa(i)

		conn := &captureConn{}
		session := NewSession(conn)
		session.Conn = conn
		session.Character = MockCharacter()
		ensureCharacterDefaults(session.Character)
		session.World = worlds[World1]
		session.Position = DefaultSpawnPosition(World1)
		registerSession(session)
		boundName := ""

		handleClientCommand(conn, session, visible, peerKey, &boundName, ReqAuthToken, map[string]interface{}{
			"token": issueTestToken(username),
			"class": tc.inputClass,
		})
		msgs := conn.DrainMessages(t)
		unregisterSession(session)

		if len(msgs) != 3 || msgs[0].Command != RespAuthOK || msgs[2].Command != RespState {
			t.Fatalf("unexpected auth sequence for class %s: %#v", tc.inputClass, msgs)
		}
		if got := toString(toMap(msgs[0].Payload), "class"); got != tc.expectedClass {
			t.Fatalf("expected AUTH_OK class %q, got %q", tc.expectedClass, got)
		}

		state := toMap(msgs[2].Payload)
		if got := toString(state, "class"); got != tc.expectedClass {
			t.Fatalf("expected STATE class %q, got %q", tc.expectedClass, got)
		}
		if !payloadInventoryHasID(state["inventory"], tc.expectedWeapon) {
			t.Fatalf("expected starter weapon %s for class %s", tc.expectedWeapon, tc.expectedClass)
		}
		if rank := payloadSkillRank(state["skills"], tc.primarySkill); rank != 1 {
			t.Fatalf("expected primary starter skill %s rank=1 for class %s, got %d", tc.primarySkill, tc.expectedClass, rank)
		}
		if hasShield := payloadInventoryHasID(state["inventory"], "starter_shield"); hasShield != tc.expectShield {
			t.Fatalf("unexpected shield presence for class %s: got=%v want=%v", tc.expectedClass, hasShield, tc.expectShield)
		}
	}
}

func TestCommandFlowUpgradeGearResultFields(t *testing.T) {
	resetSocialStateForTests()
	resetPersistenceRuntimeStateForTests()

	t.Setenv("A3_PERSISTENCE_MODE", "json")
	t.Setenv("A3_AUTH_SECRET", "test-auth-secret")

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd failed: %v", err)
	}
	tmpWD := t.TempDir()
	if err := os.Chdir(tmpWD); err != nil {
		t.Fatalf("chdir temp dir failed: %v", err)
	}
	defer func() { _ = os.Chdir(oldWD) }()
	resetPersistenceRuntimeStateForTests()

	worlds = DefaultWorlds()
	visible := map[*ClientSession]bool{}

	conn := &captureConn{}
	session := NewSession(conn)
	session.Conn = conn
	session.Character = MockCharacter()
	ensureCharacterDefaults(session.Character)
	session.World = worlds[World1]
	session.Position = DefaultSpawnPosition(World1)
	registerSession(session)
	defer unregisterSession(session)
	boundName := ""
	peerKey := "upgrade-fields-peer"

	handleClientCommand(conn, session, visible, peerKey, &boundName, ReqAuthToken, map[string]interface{}{
		"token": issueTestToken("UpgradeFieldsHero"),
		"class": "Archer",
	})
	_ = conn.DrainMessages(t)

	// +1 -> +2 success path should expose old/new/cost/failure_effect fields.
	session.Character.Materials["enhance_gem_t1"] = 1
	withFixedRandIntn(0, func() {
		handleClientCommand(conn, session, visible, peerKey, &boundName, ReqUpgradeGear, map[string]interface{}{
			"item_id": "starter_bow",
		})
	})
	msgs := conn.DrainMessages(t)
	if len(msgs) != 1 || msgs[0].Command != RespGearUpgradeResult {
		t.Fatalf("unexpected upgrade success messages: %#v", msgs)
	}
	successPayload := toMap(msgs[0].Payload)
	if toInt(successPayload, "old_gear_level") != 1 || toInt(successPayload, "new_gear_level") != 2 {
		t.Fatalf("expected old/new levels 1->2, got %#v", successPayload)
	}
	if toInt(successPayload, "cost") != 1 || toString(successPayload, "gem") != "enhance_gem_t1" {
		t.Fatalf("unexpected gem cost fields: %#v", successPayload)
	}
	if toString(successPayload, "failure_effect") != "NONE" {
		t.Fatalf("expected failure_effect NONE on success, got %#v", successPayload["failure_effect"])
	}
	if success, _ := successPayload["success"].(bool); !success {
		t.Fatalf("expected success=true for deterministic success roll")
	}

	// +8 failure path can downgrade and should expose DOWNGRADED effect.
	session.Character.Inventory[0].GearLevel = 8
	session.Character.Materials["enhance_gem_t3"] = 3
	withRandIntnSequence([]int{9999, 0}, func() {
		handleClientCommand(conn, session, visible, peerKey, &boundName, ReqUpgradeGear, map[string]interface{}{
			"item_id": "starter_bow",
		})
	})
	msgs = conn.DrainMessages(t)
	if len(msgs) != 1 || msgs[0].Command != RespGearUpgradeResult {
		t.Fatalf("unexpected high-tier failure messages: %#v", msgs)
	}
	failPayload := toMap(msgs[0].Payload)
	if toInt(failPayload, "old_gear_level") != 8 || toInt(failPayload, "new_gear_level") != 7 {
		t.Fatalf("expected old/new levels 8->7 on downgrade, got %#v", failPayload)
	}
	if toInt(failPayload, "cost") != 3 || toString(failPayload, "gem") != "enhance_gem_t3" {
		t.Fatalf("unexpected high-tier gem cost fields: %#v", failPayload)
	}
	if toString(failPayload, "failure_effect") != "DOWNGRADED" {
		t.Fatalf("expected failure_effect DOWNGRADED, got %#v", failPayload["failure_effect"])
	}
	if success, _ := failPayload["success"].(bool); success {
		t.Fatalf("expected success=false for deterministic fail roll")
	}
}

func TestCommandFlowAuthKeepsExistingClassOnReauthHint(t *testing.T) {
	resetSocialStateForTests()
	resetPersistenceRuntimeStateForTests()

	t.Setenv("A3_PERSISTENCE_MODE", "json")
	t.Setenv("A3_AUTH_SECRET", "test-auth-secret")

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd failed: %v", err)
	}
	tmpWD := t.TempDir()
	if err := os.Chdir(tmpWD); err != nil {
		t.Fatalf("chdir temp dir failed: %v", err)
	}
	defer func() { _ = os.Chdir(oldWD) }()
	resetPersistenceRuntimeStateForTests()

	worlds = DefaultWorlds()
	visible := map[*ClientSession]bool{}
	username := "ClassStickyHero"

	// First auth creates character as Mage.
	conn1 := &captureConn{}
	session1 := NewSession(conn1)
	session1.Conn = conn1
	session1.Character = MockCharacter()
	ensureCharacterDefaults(session1.Character)
	session1.World = worlds[World1]
	session1.Position = DefaultSpawnPosition(World1)
	registerSession(session1)
	bound1 := ""
	handleClientCommand(conn1, session1, visible, "class-sticky-1", &bound1, ReqAuthToken, map[string]interface{}{
		"token": issueTestToken(username),
		"class": "Mage",
	})
	authMsgs1 := conn1.DrainMessages(t)
	if len(authMsgs1) != 3 || authMsgs1[0].Command != RespAuthOK || authMsgs1[2].Command != RespState {
		t.Fatalf("unexpected first auth sequence: %#v", authMsgs1)
	}
	if got := toString(toMap(authMsgs1[0].Payload), "class"); got != "Mage" {
		t.Fatalf("expected AUTH_OK class Mage, got %q", got)
	}
	state1 := toMap(authMsgs1[2].Payload)
	if got := toString(state1, "class"); got != "Mage" {
		t.Fatalf("expected STATE class Mage, got %q", got)
	}
	if !payloadInventoryHasID(state1["inventory"], "starter_focus") {
		t.Fatalf("expected Mage starter item starter_focus in first state")
	}
	if err := persistSessionState(session1); err != nil {
		t.Fatalf("persistSessionState session1 failed: %v", err)
	}
	unregisterSession(session1)

	// Second auth attempts Warrior hint, but persisted class should stay Mage.
	conn2 := &captureConn{}
	session2 := NewSession(conn2)
	session2.Conn = conn2
	session2.Character = MockCharacter()
	ensureCharacterDefaults(session2.Character)
	session2.World = worlds[World1]
	session2.Position = DefaultSpawnPosition(World1)
	registerSession(session2)
	defer unregisterSession(session2)
	bound2 := ""
	handleClientCommand(conn2, session2, visible, "class-sticky-2", &bound2, ReqAuthToken, map[string]interface{}{
		"token": issueTestToken(username),
		"class": "Warrior",
	})
	authMsgs2 := conn2.DrainMessages(t)
	if len(authMsgs2) != 3 || authMsgs2[0].Command != RespAuthOK || authMsgs2[2].Command != RespState {
		t.Fatalf("unexpected second auth sequence: %#v", authMsgs2)
	}
	if got := toString(toMap(authMsgs2[0].Payload), "class"); got != "Mage" {
		t.Fatalf("expected AUTH_OK class to remain Mage, got %q", got)
	}
	state2 := toMap(authMsgs2[2].Payload)
	if got := toString(state2, "class"); got != "Mage" {
		t.Fatalf("expected STATE class to remain Mage, got %q", got)
	}
	if !payloadInventoryHasID(state2["inventory"], "starter_focus") {
		t.Fatalf("expected Mage starter item starter_focus to be retained")
	}
	if payloadInventoryHasID(state2["inventory"], "starter_blade") {
		t.Fatalf("unexpected Warrior starter item after reauth class hint")
	}
}

func TestAccountScopedStoragePersistsAcrossSessions(t *testing.T) {
	resetSocialStateForTests()
	resetPersistenceRuntimeStateForTests()

	t.Setenv("A3_PERSISTENCE_MODE", "json")
	t.Setenv("A3_AUTH_SECRET", "test-auth-secret")

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd failed: %v", err)
	}
	tmpWD := t.TempDir()
	if err := os.Chdir(tmpWD); err != nil {
		t.Fatalf("chdir temp dir failed: %v", err)
	}
	defer func() { _ = os.Chdir(oldWD) }()
	resetPersistenceRuntimeStateForTests()

	worlds = DefaultWorlds()
	visible := map[*ClientSession]bool{}
	peerKey := "test-account-peer"
	username := "AccountPersistHero"

	// Session 1: auth, mutate wallet+storage, persist.
	conn1 := &captureConn{}
	session1 := NewSession(conn1)
	session1.Conn = conn1
	session1.Character = MockCharacter()
	ensureCharacterDefaults(session1.Character)
	session1.World = worlds[World1]
	session1.Position = DefaultSpawnPosition(World1)
	registerSession(session1)
	bound1 := ""
	handleClientCommand(conn1, session1, visible, peerKey, &bound1, ReqAuthToken, map[string]interface{}{
		"token": issueTestToken(username),
		"class": "Archer",
	})
	_ = conn1.DrainMessages(t)

	session1.Character.Materials["wolf_pelt"] = 3
	handleClientCommand(conn1, session1, visible, peerKey, &bound1, ReqStorageDepGold, map[string]interface{}{"amount": 25})
	handleClientCommand(conn1, session1, visible, peerKey, &bound1, ReqStorageDepMat, map[string]interface{}{"item_id": "wolf_pelt", "qty": 2})
	depositMsgs := conn1.DrainMessages(t)
	if len(depositMsgs) != 2 || depositMsgs[0].Command != RespStorageState || depositMsgs[1].Command != RespStorageState {
		t.Fatalf("unexpected deposit messages: %#v", depositMsgs)
	}

	if err := persistSessionState(session1); err != nil {
		t.Fatalf("persistSessionState session1 failed: %v", err)
	}
	unregisterSession(session1)

	// Session 2: same username should hydrate account-scoped wallet/storage.
	conn2 := &captureConn{}
	session2 := NewSession(conn2)
	session2.Conn = conn2
	session2.Character = MockCharacter()
	ensureCharacterDefaults(session2.Character)
	session2.World = worlds[World1]
	session2.Position = DefaultSpawnPosition(World1)
	registerSession(session2)
	defer unregisterSession(session2)

	bound2 := ""
	handleClientCommand(conn2, session2, visible, peerKey+"_2", &bound2, ReqAuthToken, map[string]interface{}{
		"token": issueTestToken(username),
		"class": "Archer",
	})
	authMsgs := conn2.DrainMessages(t)
	if len(authMsgs) != 3 || authMsgs[2].Command != RespState {
		t.Fatalf("unexpected auth sequence for session2: %#v", authMsgs)
	}

	state := toMap(authMsgs[2].Payload)
	if toInt(state, "wallet_gold") != 25 {
		t.Fatalf("expected wallet_gold=25 after reconnect, got %v", state["wallet_gold"])
	}
	storage := toMap(state["storage"])
	storageMaterials := payloadIntMap(storage["materials"])
	if storageMaterials["wolf_pelt"] != 2 {
		t.Fatalf("expected storage wolf_pelt=2 after reconnect, got %d", storageMaterials["wolf_pelt"])
	}
}

func TestCommandFlowStorageItemRestrictionsAndRoundTrip(t *testing.T) {
	resetSocialStateForTests()
	resetPersistenceRuntimeStateForTests()

	t.Setenv("A3_PERSISTENCE_MODE", "json")
	t.Setenv("A3_AUTH_SECRET", "test-auth-secret")

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd failed: %v", err)
	}
	tmpWD := t.TempDir()
	if err := os.Chdir(tmpWD); err != nil {
		t.Fatalf("chdir temp dir failed: %v", err)
	}
	defer func() { _ = os.Chdir(oldWD) }()
	resetPersistenceRuntimeStateForTests()

	worlds = DefaultWorlds()
	conn := &captureConn{}
	session := NewSession(conn)
	session.Conn = conn
	session.Character = MockCharacter()
	ensureCharacterDefaults(session.Character)
	session.World = worlds[World1]
	session.Position = DefaultSpawnPosition(World1)
	registerSession(session)
	defer unregisterSession(session)

	visible := map[*ClientSession]bool{}
	bound := ""
	peerKey := "test-storage-items-peer"

	handleClientCommand(conn, session, visible, peerKey, &bound, ReqAuthToken, map[string]interface{}{
		"token": issueTestToken("StorageItemHero"),
		"class": "Archer",
	})
	authMsgs := conn.DrainMessages(t)
	if len(authMsgs) != 3 || authMsgs[2].Command != RespState {
		t.Fatalf("unexpected auth sequence: %#v", authMsgs)
	}

	handled, modified := handleClientCommand(conn, session, visible, peerKey, &bound, ReqStorageDepItm, map[string]interface{}{
		"item_id": "starter_bow",
	})
	if !handled || modified {
		t.Fatalf("expected non-modifying handled storage reject, got handled=%v modified=%v", handled, modified)
	}
	rejectMsgs := conn.DrainMessages(t)
	if len(rejectMsgs) != 1 || rejectMsgs[0].Command != RespStorageRejected || rejectMsgs[0].Payload != "ITEM_NOT_STORABLE" {
		t.Fatalf("expected ITEM_NOT_STORABLE rejection, got %#v", rejectMsgs)
	}

	token := Item{
		ID:        "storage_token",
		Name:      "Storage Token",
		Slot:      "misc",
		GearLevel: 1,
	}
	session.Character.Inventory = append(session.Character.Inventory, token)
	ensureCharacterDefaults(session.Character)

	handled, modified = handleClientCommand(conn, session, visible, peerKey, &bound, ReqStorageDepItm, map[string]interface{}{
		"item_id": token.ID,
	})
	if !handled || !modified {
		t.Fatalf("expected successful storage deposit to modify state, got handled=%v modified=%v", handled, modified)
	}
	depositMsgs := conn.DrainMessages(t)
	if len(depositMsgs) != 1 || depositMsgs[0].Command != RespStorageState {
		t.Fatalf("expected STORAGE_STATE after storable item deposit, got %#v", depositMsgs)
	}
	depositPayload := toMap(depositMsgs[0].Payload)
	if toInt(depositPayload, "used_stacks") != 1 {
		t.Fatalf("expected used_stacks=1 after deposit, got %v", depositPayload["used_stacks"])
	}
	if !payloadStorageHasItemID(depositPayload["storage"], token.ID) {
		t.Fatalf("expected storage to include %s after deposit, got %#v", token.ID, depositPayload["storage"])
	}
	if findInventoryItemIndex(session.Character, token.ID) >= 0 {
		t.Fatalf("expected %s removed from inventory after storage deposit", token.ID)
	}

	handled, modified = handleClientCommand(conn, session, visible, peerKey, &bound, ReqStorageWdrItm, map[string]interface{}{
		"item_id": token.ID,
	})
	if !handled || !modified {
		t.Fatalf("expected successful storage withdrawal to modify state, got handled=%v modified=%v", handled, modified)
	}
	withdrawMsgs := conn.DrainMessages(t)
	if len(withdrawMsgs) != 1 || withdrawMsgs[0].Command != RespStorageState {
		t.Fatalf("expected STORAGE_STATE after storable item withdraw, got %#v", withdrawMsgs)
	}
	withdrawPayload := toMap(withdrawMsgs[0].Payload)
	if toInt(withdrawPayload, "used_stacks") != 0 {
		t.Fatalf("expected used_stacks=0 after withdrawal, got %v", withdrawPayload["used_stacks"])
	}
	if payloadStorageHasItemID(withdrawPayload["storage"], token.ID) {
		t.Fatalf("did not expect storage to contain %s after withdrawal", token.ID)
	}
	if findInventoryItemIndex(session.Character, token.ID) < 0 {
		t.Fatalf("expected %s restored to inventory after withdrawal", token.ID)
	}
}

func TestAccountBackfillsFromLegacyCharacterScopedData(t *testing.T) {
	resetSocialStateForTests()
	resetPersistenceRuntimeStateForTests()

	t.Setenv("A3_PERSISTENCE_MODE", "json")
	t.Setenv("A3_AUTH_SECRET", "test-auth-secret")

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd failed: %v", err)
	}
	tmpWD := t.TempDir()
	if err := os.Chdir(tmpWD); err != nil {
		t.Fatalf("chdir temp dir failed: %v", err)
	}
	defer func() { _ = os.Chdir(oldWD) }()
	resetPersistenceRuntimeStateForTests()

	worlds = DefaultWorlds()
	username := "LegacyAccountHero"
	seed := MockCharacter()
	seed.Name = username
	ensureCharacterDefaults(seed)
	seed.WalletGold = 91
	seed.Storage.Materials["wolf_pelt"] = 6
	if err := persistCharacter(seed); err != nil {
		t.Fatalf("persistCharacter seed failed: %v", err)
	}

	visible := map[*ClientSession]bool{}
	peerKey := "test-backfill-peer"
	conn := &captureConn{}
	session := NewSession(conn)
	session.Conn = conn
	session.Character = MockCharacter()
	ensureCharacterDefaults(session.Character)
	session.World = worlds[World1]
	session.Position = DefaultSpawnPosition(World1)
	registerSession(session)
	defer unregisterSession(session)
	bound := ""

	handleClientCommand(conn, session, visible, peerKey, &bound, ReqAuthToken, map[string]interface{}{
		"token": issueTestToken(username),
		"class": "Archer",
	})
	authMsgs := conn.DrainMessages(t)
	if len(authMsgs) != 3 || authMsgs[2].Command != RespState {
		t.Fatalf("unexpected auth messages: %#v", authMsgs)
	}
	state := toMap(authMsgs[2].Payload)
	if toInt(state, "wallet_gold") != 91 {
		t.Fatalf("expected wallet_gold=91 from legacy character backfill, got %v", state["wallet_gold"])
	}
	storage := toMap(state["storage"])
	storageMaterials := payloadIntMap(storage["materials"])
	if storageMaterials["wolf_pelt"] != 6 {
		t.Fatalf("expected storage wolf_pelt=6 from legacy character backfill, got %d", storageMaterials["wolf_pelt"])
	}

	account, err := loadAccount(username)
	if err != nil {
		t.Fatalf("loadAccount after backfill failed: %v", err)
	}
	if account.WalletGold != 91 || account.Storage.Materials["wolf_pelt"] != 6 {
		t.Fatalf("expected persisted account backfill, got wallet=%d mats=%d", account.WalletGold, account.Storage.Materials["wolf_pelt"])
	}
}

func TestPetQuestGatingFlow(t *testing.T) {
	resetSocialStateForTests()
	resetPersistenceRuntimeStateForTests()

	t.Setenv("A3_PERSISTENCE_MODE", "json")
	t.Setenv("A3_AUTH_SECRET", "test-auth-secret")

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd failed: %v", err)
	}
	tmpWD := t.TempDir()
	if err := os.Chdir(tmpWD); err != nil {
		t.Fatalf("chdir temp dir failed: %v", err)
	}
	defer func() { _ = os.Chdir(oldWD) }()
	resetPersistenceRuntimeStateForTests()

	worlds = DefaultWorlds()
	conn := &captureConn{}
	session := NewSession(conn)
	session.Conn = conn
	session.Character = MockCharacter()
	ensureCharacterDefaults(session.Character)
	session.World = worlds[World1]
	session.Position = DefaultSpawnPosition(World1)
	registerSession(session)
	defer unregisterSession(session)

	visible := map[*ClientSession]bool{}
	bound := ""
	peerKey := "test-pet-gating-peer"

	handleClientCommand(conn, session, visible, peerKey, &bound, ReqAuthToken, map[string]interface{}{
		"token": issueTestToken("PetQuestHero"),
		"class": "Archer",
	})
	authMsgs := conn.DrainMessages(t)
	if len(authMsgs) != 3 || authMsgs[2].Command != RespState {
		t.Fatalf("unexpected auth sequence: %#v", authMsgs)
	}

	handleClientCommand(conn, session, visible, peerKey, &bound, ReqSummonPet, map[string]interface{}{"pet": "Falcon"})
	summonBefore := conn.DrainMessages(t)
	if len(summonBefore) != 1 || summonBefore[0].Command != RespPetRejected || summonBefore[0].Payload != "PET_NOT_ACQUIRED" {
		t.Fatalf("expected PET_NOT_ACQUIRED before unlock, got %#v", summonBefore)
	}

	handleClientCommand(conn, session, visible, peerKey, &bound, ReqPetFeed, map[string]interface{}{"qty": 1})
	feedBefore := conn.DrainMessages(t)
	if len(feedBefore) != 1 || feedBefore[0].Command != RespPetRejected || feedBefore[0].Payload != "PET_NOT_ACQUIRED" {
		t.Fatalf("expected PET_NOT_ACQUIRED feed before unlock, got %#v", feedBefore)
	}

	handleClientCommand(conn, session, visible, peerKey, &bound, ReqAcceptQuest, map[string]interface{}{"quest_id": "unlock_world2_race"})
	acceptMsgs := conn.DrainMessages(t)
	if len(acceptMsgs) != 1 || acceptMsgs[0].Command != RespQuestAccepted {
		t.Fatalf("unexpected accept quest response: %#v", acceptMsgs)
	}
	handleClientCommand(conn, session, visible, peerKey, &bound, ReqCompleteQuest, map[string]interface{}{"quest_id": "unlock_world2_race"})
	completeMsgs := conn.DrainMessages(t)
	if len(completeMsgs) != 1 || completeMsgs[0].Command != RespQuestCompleted {
		t.Fatalf("unexpected complete quest response: %#v", completeMsgs)
	}

	handleClientCommand(conn, session, visible, peerKey, &bound, ReqSummonPet, map[string]interface{}{"pet": "Falcon"})
	summonAfter := conn.DrainMessages(t)
	if len(summonAfter) != 1 || summonAfter[0].Command != RespPetSummoned {
		t.Fatalf("expected PET_SUMMONED after unlock, got %#v", summonAfter)
	}

	session.Character.Materials["pet_treat"] = 1
	handleClientCommand(conn, session, visible, peerKey, &bound, ReqPetFeed, map[string]interface{}{"qty": 1})
	feedAfter := conn.DrainMessages(t)
	if len(feedAfter) != 1 || feedAfter[0].Command != RespPetUpdate {
		t.Fatalf("expected PET_UPDATE after unlock/feed, got %#v", feedAfter)
	}
}

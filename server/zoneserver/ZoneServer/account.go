package main

type Account struct {
	Username   string       `json:"username"`
	WalletGold int          `json:"wallet_gold"`
	Storage    StorageState `json:"storage"`
}

func cloneStorageState(src StorageState) StorageState {
	dst := StorageState{
		Materials: map[string]int{},
		Items:     make([]Item, 0, len(src.Items)),
	}
	for k, v := range src.Materials {
		dst.Materials[k] = v
	}
	dst.Items = append(dst.Items, src.Items...)
	return dst
}

func ensureAccountDefaults(a *Account) {
	if a == nil {
		return
	}
	if a.Storage.Materials == nil {
		a.Storage.Materials = map[string]int{}
	}
	if a.Storage.Items == nil {
		a.Storage.Items = []Item{}
	}
	if a.WalletGold < 0 {
		a.WalletGold = 0
	}
	for i := range a.Storage.Items {
		ensureItemDefaults(&a.Storage.Items[i])
	}
}

func newDefaultAccount(username string) *Account {
	a := &Account{
		Username:   username,
		WalletGold: 0,
		Storage: StorageState{
			Materials: map[string]int{},
			Items:     []Item{},
		},
	}
	ensureAccountDefaults(a)
	return a
}

func syncCharacterFromAccount(c *Character, a *Account) {
	if c == nil || a == nil {
		return
	}
	ensureCharacterDefaults(c)
	ensureAccountDefaults(a)
	c.WalletGold = a.WalletGold
	c.Storage = cloneStorageState(a.Storage)
}

func syncAccountFromCharacter(a *Account, c *Character) {
	if a == nil || c == nil {
		return
	}
	ensureAccountDefaults(a)
	ensureCharacterDefaults(c)
	a.WalletGold = c.WalletGold
	a.Storage = cloneStorageState(c.Storage)
}

func accountHasStoredData(a *Account) bool {
	if a == nil {
		return false
	}
	if a.WalletGold > 0 {
		return true
	}
	if len(a.Storage.Items) > 0 {
		return true
	}
	for _, qty := range a.Storage.Materials {
		if qty > 0 {
			return true
		}
	}
	return false
}

func characterHasAccountScopedData(c *Character) bool {
	if c == nil {
		return false
	}
	if c.WalletGold > 0 {
		return true
	}
	if len(c.Storage.Items) > 0 {
		return true
	}
	for _, qty := range c.Storage.Materials {
		if qty > 0 {
			return true
		}
	}
	return false
}

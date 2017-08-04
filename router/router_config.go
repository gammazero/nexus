package router

type RouterConfig struct {
	RealmConfigs  []*RealmConfig `json:"realms"`
	RealmTemplate *RealmConfig   `json:"realm_template"`
}

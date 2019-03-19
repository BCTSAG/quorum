package vault

import (
	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/log"
	"sync"
)

type hashicorpBackend struct {
	stateLock sync.RWMutex
	wallets []accounts.Wallet // A vault wallet contains all account keys stored in that particular vault and accessible with a particular auth token
	updateFeed event.Feed
	updateScope event.SubscriptionScope
	hashicorpConfigs []HashicorpConfig
}

type HashicorpConfig struct {
	ClientData ClientData `toml:"Client,omitempty"`
	Secrets    []SecretData `toml:"Secrets,omitempty"`
}

func NewHashicorpBackend(hashicorpConfigs []HashicorpConfig) *hashicorpBackend {
	hb := &hashicorpBackend{hashicorpConfigs: hashicorpConfigs}
	hb.refreshWallets()

	return hb
}

func (hb *hashicorpBackend) Wallets() []accounts.Wallet {
	// check connection to vault is still up before returning wallet
	// update list of accounts in wallets to cover the instances where secrets have been updated/deleted
	//hb.refreshWallets()
	return hb.wallets
}

func (hb *hashicorpBackend) Subscribe(sink chan<- accounts.WalletEvent) event.Subscription {
	// We need the mutex to reliably start/stop the update loop
	hb.stateLock.Lock()
	defer hb.stateLock.Unlock()

	// Subscribe the caller and track the subscriber count
	sub := hb.updateScope.Track(hb.updateFeed.Subscribe(sink))

	//hb.refreshWallets()

	return sub
}

func (hb *hashicorpBackend) refreshWallets() {
	// Check which wallets have been added/dropped and raise corresponding events
	// Update hb.wallets with refreshed wallets
	var wallets []accounts.Wallet
	var events []accounts.WalletEvent

	//TODO consider not only fetching the wallets once (i.e. like in other backend impls)
	if len(hb.wallets) == 0 {
		for _, hc := range hb.hashicorpConfigs {
			w, err := NewHashicorpWallet(hc.ClientData, hc.Secrets, hb.updateFeed)

			//TODO review how to handle
			if err != nil {
				log.Warn("Unable to create Hashicorp wallet", err)
			}

			events = append(events, accounts.WalletEvent{w, accounts.WalletArrived})
			wallets = append(wallets, w)
		}

		for _, e := range events {
			hb.updateFeed.Send(e)
		}

		hb.wallets = wallets
	}

}


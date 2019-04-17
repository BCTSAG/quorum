package vault

import (
	"crypto/ecdsa"
	"errors"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/log"
	"math/big"
	"strings"
	"sync"
)

type vaultWallet struct {
	vault             VaultService // vault can only be written to in constructor, therefore do not need mutex lock to access.  The VaultService impl is expected to contain a mutex and lock/unlock as required.
	url               accounts.URL
	updateFeed        *event.Feed
	stateLock         sync.RWMutex  // Protects read and write access to the wallet struct fields
	accounts          []accounts.Account
}

type VaultService interface {
	status() (string, error)
	open() error
	isOpen() bool
	close() error
	getAccounts() ([]accounts.Account, []error)
	getPrivateKey(account accounts.Account) (*ecdsa.PrivateKey, error)
	store(key *ecdsa.PrivateKey) (common.Address, error)
}

func NewHashicorpVaultWallet(config HashicorpWalletConfig, updateFeed *event.Feed) (*vaultWallet, error) {
	url, err := parseURL(config.Client.Url)

	if err != nil {
		return &vaultWallet{}, err
	}

	s := NewHashicorpService(config.Client, config.Secrets)

	w := &vaultWallet{
		vault: s,
		url: url,
		updateFeed: updateFeed,
	}

	return w, nil
}

func (w *vaultWallet) URL() accounts.URL {
	return w.url
}

func (w *vaultWallet) Status() (string, error) {
	return w.vault.status()
}

// Open implements accounts.Wallet, creating an authenticated Client and making it accessible to the wallet to enable vault operations.
//
// If Approle credentials have been provided these will be used to authenticate the Client with the vault, else the Token will be used.
//
// The passphrase arg is not used and this method does not retrieve any secrets from the vault.
func (w *vaultWallet) Open(passphrase string) error {
	if w.vault.isOpen() {
		return accounts.ErrWalletAlreadyOpen
	}

	if err := w.vault.open(); err != nil {
		return err
	}

	go w.updateFeed.Send(
		accounts.WalletEvent{Wallet: w, Kind: accounts.WalletOpened},
	)

	return nil
}

// Close implements accounts.Wallet, clearing the state of the wallet and removing the vault Client so vault operations can no longer be carried out.
func (w *vaultWallet) Close() error {
	w.stateLock.Lock()
	w.accounts = nil
	w.stateLock.Unlock()

	return w.vault.close()
}

// Account implements accounts.Wallet, returning the accounts specified in config that are stored in the vault.  refreshAccounts() retrieves the list of accounts from the vault and so must have been called prior to this method in order to return a non-empty slice
func (w *vaultWallet) Accounts() []accounts.Account {
	accts, errs := w.vault.getAccounts()

	for _, err := range errs {
		log.Warn("Error getting account from vault", "wallet", w.URL(), "err", err)
	}

	w.stateLock.Lock()
	w.accounts = accts
	w.stateLock.Unlock()

	w.stateLock.RLock()
	defer w.stateLock.RUnlock()

	cpy := make([]accounts.Account, len(w.accounts))
	copy(cpy, w.accounts)

	return cpy
}

// Contains implements accounts.Wallet, returning whether a particular account is managed by this wallet.
func (w *vaultWallet) Contains(account accounts.Account) bool {
	w.stateLock.RLock()
	defer w.stateLock.RUnlock()

	for _, wltAcct := range w.accounts {
		if wltAcct.Address == account.Address && (account.URL == accounts.URL{} || wltAcct.URL == account.URL) {
			return true
		}
	}

	return false
}

// Derive implements accounts.Wallet, but is a noop for Vault wallets since these have no notion of hierarchical account derivation.
func (*vaultWallet) Derive(path accounts.DerivationPath, pin bool) (accounts.Account, error) {
	return accounts.Account{}, accounts.ErrNotSupported
}

// SelfDerive implements accounts.Wallet, but is a noop for Vault wallets since these have no notion of hierarchical account derivation.
func (w *vaultWallet) SelfDerive(base accounts.DerivationPath, chain ethereum.ChainStateReader) {}

func (w *vaultWallet) SignHash(account accounts.Account, hash []byte) ([]byte, error) {
	if !w.Contains(account) {
		return nil, accounts.ErrUnknownAccount
	}

	key, err := w.vault.getPrivateKey(account)
	if err != nil {
		return nil, err
	}
	defer zeroKey(key)

	return crypto.Sign(hash, key)
}

func (w *vaultWallet) SignTx(account accounts.Account, tx *types.Transaction, chainID *big.Int, isQuorum bool) (*types.Transaction, error) {
	if !w.Contains(account) {
		return nil, accounts.ErrUnknownAccount
	}

	key, err := w.vault.getPrivateKey(account)
	if err != nil {
		return nil, err
	}
	defer zeroKey(key)

	if chainID != nil && !tx.IsPrivate() {
		return types.SignTx(tx, types.NewEIP155Signer(chainID), key)
	}
	return types.SignTx(tx, types.HomesteadSigner{}, key)
}

func (w *vaultWallet) SignHashWithPassphrase(account accounts.Account, passphrase string, hash []byte) ([]byte, error) {
	return w.SignHash(account, hash)
}

func (w *vaultWallet) SignTxWithPassphrase(account accounts.Account, passphrase string, tx *types.Transaction, chainID *big.Int) (*types.Transaction, error) {
	return w.SignTx(account, tx, chainID, true)
}

// TODO Duplicated code from url.go
// parseURL converts a user supplied URL into the accounts specific structure.
func parseURL(url string) (accounts.URL, error) {
	parts := strings.Split(url, "://")
	if len(parts) != 2 || parts[0] == "" {
		return accounts.URL{}, errors.New("protocol scheme missing")
	}
	return accounts.URL {
		Scheme: parts[0],
		Path:   parts[1],
	}, nil
}

// zeroKey zeroes a private key in memory.
//TODO duplicated code from keystore.go
func zeroKey(k *ecdsa.PrivateKey) {
	b := k.D.Bits()
	for i := range b {
		b[i] = 0
	}
}

/*
Copyright SecureKey Technologies Inc. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package kms

import (
	"fmt"
	"net/http"
	"time"

	"github.com/bluele/gcache"
	"github.com/gorilla/mux"
	"github.com/hyperledger/aries-framework-go/pkg/crypto"
	"github.com/hyperledger/aries-framework-go/pkg/kms"
	"github.com/hyperledger/aries-framework-go/pkg/kms/localkms"
	"github.com/hyperledger/aries-framework-go/pkg/secretlock"
	"github.com/hyperledger/aries-framework-go/pkg/storage"
	"github.com/trustbloc/edge-core/pkg/log"

	"github.com/trustbloc/hub-kms/pkg/keystore"
)

const (
	primaryKeyURI        = "local-lock://%s"
	keystoreIDQueryParam = "keystoreID"
)

// ServiceCreator is a function that creates KMS Service.
type ServiceCreator func(req *http.Request) (Service, error)

// Config defines configuration for ServiceCreator.
type Config struct {
	KeystoreService    keystore.Service
	CryptoService      crypto.Crypto
	KMSStorageResolver func(keystoreID string) (storage.Provider, error)
	SecretLockResolver func(keyURI string, req *http.Request) (secretlock.Service, error)
	CacheExpiration    time.Duration
}

// TODO(#134): Improve caching solution for KMS creator service.
var (
	cache  = gcache.New(0).Build()              //nolint:gochecknoglobals // todo refactor
	logger = log.New("hub-kms/service-creator") //nolint:gochecknoglobals // todo refactor
)

// NewServiceCreator returns func to create KMS Service backed by LocalKMS and passphrase-based secret lock.
func NewServiceCreator(c *Config) ServiceCreator {
	return func(req *http.Request) (Service, error) {
		keystoreID := mux.Vars(req)[keystoreIDQueryParam]
		keyURI := fmt.Sprintf(primaryKeyURI, keystoreID)

		if c.CacheExpiration != 0 {
			cachedService, err := cache.Get(keystoreID)
			if err == nil {
				logger.Infof("service for keystore %q resolved from the cache", keystoreID)

				return cachedService.(Service), nil
			}
		}

		kmsStorageProvider, err := c.KMSStorageResolver(keystoreID)
		if err != nil {
			return nil, err
		}

		secretLock, err := c.SecretLockResolver(keyURI, req)
		if err != nil {
			return nil, err
		}

		kmsProv := kmsProvider{
			storageProvider: kmsStorageProvider,
			secretLock:      secretLock,
		}

		keyManager, err := localkms.New(keyURI, kmsProv)
		if err != nil {
			return nil, err
		}

		cryptoBox, err := localkms.NewCryptoBox(keyManager)
		if err != nil {
			return nil, err
		}

		provider := kmsServiceProvider{
			keystoreService: c.KeystoreService,
			keyManager:      keyManager,
			crypto:          c.CryptoService,
			cryptoBox:       cryptoBox,
		}

		srv := NewService(provider)

		if c.CacheExpiration != 0 {
			err = cache.SetWithExpire(keystoreID, srv, c.CacheExpiration)
			if err != nil {
				logger.Errorf("failed to save into the cache: %s", err)
			}

			logger.Infof("service for keystore %q added to the cache", keystoreID)
		}

		return srv, nil
	}
}

type kmsProvider struct {
	storageProvider storage.Provider
	secretLock      secretlock.Service
}

func (k kmsProvider) StorageProvider() storage.Provider {
	return k.storageProvider
}

func (k kmsProvider) SecretLock() secretlock.Service {
	return k.secretLock
}

type kmsServiceProvider struct {
	keystoreService keystore.Service
	keyManager      kms.KeyManager
	crypto          crypto.Crypto
	cryptoBox       CryptoBox
}

func (k kmsServiceProvider) KeystoreService() keystore.Service {
	return k.keystoreService
}

func (k kmsServiceProvider) KeyManager() kms.KeyManager {
	return k.keyManager
}

func (k kmsServiceProvider) Crypto() crypto.Crypto {
	return k.crypto
}

func (k kmsServiceProvider) CryptoBox() CryptoBox {
	return k.cryptoBox
}

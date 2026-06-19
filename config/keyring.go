package config

import "github.com/zalando/go-keyring"

const keyringService = "us3ui"

// secretSet stores a connection's secret key in the OS keychain.
func secretSet(name, secret string) error {
	return keyring.Set(keyringService, name, secret)
}

// secretGet retrieves a connection's secret key from the OS keychain.
// It returns an error (e.g. keyring.ErrNotFound) when nothing is stored or no
// keychain backend is available; callers treat any error as "no secret".
func secretGet(name string) (string, error) {
	return keyring.Get(keyringService, name)
}

// secretDelete removes a connection's secret key from the OS keychain.
func secretDelete(name string) error {
	return keyring.Delete(keyringService, name)
}

// DeleteSecret removes a connection's secret key from the OS keychain.
func DeleteSecret(name string) error {
	return secretDelete(name)
}

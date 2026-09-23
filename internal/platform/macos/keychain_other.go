//go:build !darwin

package macos

import "errors"

type SubscriptionKeychain struct{ Account string }

func (SubscriptionKeychain) Get() (string, error) { return "", errors.New("仅支持 macOS Keychain") }
func (SubscriptionKeychain) Put(string) error     { return errors.New("仅支持 macOS Keychain") }
func (SubscriptionKeychain) Delete() error        { return errors.New("仅支持 macOS Keychain") }

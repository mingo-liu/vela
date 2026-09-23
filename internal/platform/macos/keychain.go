//go:build darwin

package macos

/*
#cgo LDFLAGS: -framework Security -framework CoreFoundation
#include <Security/Security.h>
#include <CoreFoundation/CoreFoundation.h>
#include <stdlib.h>
#include <string.h>

static CFMutableDictionaryRef velaQuery(const char *accountName) {
    CFMutableDictionaryRef query = CFDictionaryCreateMutable(NULL, 0, &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
    CFStringRef service = CFStringCreateWithCString(NULL, "local.vela.desktop.subscription", kCFStringEncodingUTF8);
    CFStringRef account = CFStringCreateWithCString(NULL, accountName, kCFStringEncodingUTF8);
    CFDictionarySetValue(query, kSecClass, kSecClassGenericPassword);
    CFDictionarySetValue(query, kSecAttrService, service);
    CFDictionarySetValue(query, kSecAttrAccount, account);
    CFRelease(service);
    CFRelease(account);
    return query;
}

static OSStatus velaKeychainGet(const char *account, void **bytes, int *length) {
    CFMutableDictionaryRef query = velaQuery(account);
    CFDictionarySetValue(query, kSecReturnData, kCFBooleanTrue);
    CFDictionarySetValue(query, kSecMatchLimit, kSecMatchLimitOne);
    CFTypeRef result = NULL;
    OSStatus status = SecItemCopyMatching(query, &result);
    CFRelease(query);
    if (status != errSecSuccess) return status;
    CFDataRef data = (CFDataRef)result;
    *length = (int)CFDataGetLength(data);
    *bytes = malloc(*length);
    if (*bytes == NULL) { CFRelease(data); return errSecAllocate; }
    memcpy(*bytes, CFDataGetBytePtr(data), *length);
    CFRelease(data);
    return errSecSuccess;
}

static OSStatus velaKeychainPut(const char *account, const void *bytes, int length) {
    CFMutableDictionaryRef query = velaQuery(account);
    CFDataRef data = CFDataCreate(NULL, bytes, length);
    CFMutableDictionaryRef changes = CFDictionaryCreateMutable(NULL, 0, &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
    CFDictionarySetValue(changes, kSecValueData, data);
    OSStatus status = SecItemUpdate(query, changes);
    if (status == errSecItemNotFound) {
        CFDictionarySetValue(query, kSecValueData, data);
        CFDictionarySetValue(query, kSecAttrAccessible, kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly);
        status = SecItemAdd(query, NULL);
    }
    CFRelease(changes);
    CFRelease(data);
    CFRelease(query);
    return status;
}

static OSStatus velaKeychainDelete(const char *account) {
    CFMutableDictionaryRef query = velaQuery(account);
    OSStatus status = SecItemDelete(query);
    CFRelease(query);
    return status;
}
*/
import "C"

import (
	"fmt"
	"unsafe"

	"github.com/mingo-liu/vela/internal/profile"
)

type SubscriptionKeychain struct{ Account string }

func (k SubscriptionKeychain) account() *C.char {
	if k.Account == "" {
		return C.CString("primary")
	}
	return C.CString(k.Account)
}

func (k SubscriptionKeychain) Get() (string, error) {
	var data unsafe.Pointer
	var length C.int
	account := k.account()
	defer C.free(unsafe.Pointer(account))
	status := C.velaKeychainGet(account, &data, &length)
	if status == C.errSecItemNotFound {
		return "", profile.ErrNoSubscription
	}
	if status != C.errSecSuccess {
		return "", fmt.Errorf("Keychain 读取失败 (%d)", int(status))
	}
	defer C.free(data)
	return string(C.GoBytes(data, length)), nil
}

func (k SubscriptionKeychain) Put(value string) error {
	bytes := []byte(value)
	if len(bytes) == 0 {
		return fmt.Errorf("订阅地址不能为空")
	}
	account := k.account()
	defer C.free(unsafe.Pointer(account))
	status := C.velaKeychainPut(account, unsafe.Pointer(&bytes[0]), C.int(len(bytes)))
	if status != C.errSecSuccess {
		return fmt.Errorf("Keychain 保存失败 (%d)", int(status))
	}
	return nil
}

func (k SubscriptionKeychain) Delete() error {
	account := k.account()
	defer C.free(unsafe.Pointer(account))
	status := C.velaKeychainDelete(account)
	if status != C.errSecSuccess && status != C.errSecItemNotFound {
		return fmt.Errorf("Keychain 删除失败 (%d)", int(status))
	}
	return nil
}

package desktop

import (
	"testing"

	"github.com/mingo-liu/vela/internal/profile"
)

func TestSetLanguagePersistsAndNotifies(t *testing.T) {
	dir := t.TempDir()
	store := profile.NewStore(dir)
	var notified string
	service := NewRuntimeService(nil, store, "", func(language string) { notified = language })
	settings, err := service.SetLanguage(profile.LanguageEnglish)
	if err != nil || settings.Language != profile.LanguageEnglish || notified != profile.LanguageEnglish {
		t.Fatalf("set language = %+v, %v; notified = %q", settings, err, notified)
	}
	saved, err := profile.NewStore(dir).Settings()
	if err != nil || saved.Language != profile.LanguageEnglish {
		t.Fatalf("saved language = %+v, %v", saved, err)
	}
	notified = ""
	if _, err := service.SetLanguage("fr-FR"); err == nil || notified != "" {
		t.Fatalf("invalid language error = %v; notified = %q", err, notified)
	}
}

package i18n

import (
	"strings"

	"github.com/imdario/mergo"

	"github.com/sirupsen/logrus"
)

// Localizer will translate a message into the user's language
type Localizer struct {
	Log *logrus.Entry
	S   TranslationSet
}

func NewTranslationSetFromConfig(log *logrus.Entry, configLanguage string) (*TranslationSet, error) {
	// This fork intentionally ships English only. We ignore the configured
	// language (and system locale auto-detection) so the UI never renders a
	// mix of languages when new strings have not been translated yet.
	return NewTranslationSet(log, "en"), nil
}

func NewTranslationSet(log *logrus.Entry, language string) *TranslationSet {
	log.Info("language: " + language)

	baseSet := englishSet()

	for languageCode, translationSet := range GetTranslationSets() {
		if strings.HasPrefix(language, languageCode) {
			_ = mergo.Merge(&baseSet, translationSet, mergo.WithOverride)
		}
	}

	return &baseSet
}

// GetTranslationSets gets all the translation sets, keyed by language code
func GetTranslationSets() map[string]TranslationSet {
	return map[string]TranslationSet{
		"pl": polishSet(),
		"nl": dutchSet(),
		"de": germanSet(),
		"tr": turkishSet(),
		"en": englishSet(),
		"fr": frenchSet(),
		"zh": chineseSet(),
		"es": spanishSet(),
		"pt": portugueseSet(),
	}
}

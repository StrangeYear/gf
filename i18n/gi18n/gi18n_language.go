// Copyright GoFrame Author(https://goframe.org). All Rights Reserved.
//
// This Source Code Form is subject to the terms of the MIT License.
// If a copy of the MIT was not distributed with this file,
// You can obtain one at https://github.com/gogf/gf.

package gi18n

import (
	"strings"

	"golang.org/x/text/language"
)

type languageSearchInfo struct {
	exactLanguage    string
	standardLanguage string
	shortLanguage    string
	candidates       []string // Unique language candidates in matching order.
}

func newLanguageSearchInfo(languageCode string) languageSearchInfo {
	languageCode = strings.TrimSpace(languageCode)
	info := languageSearchInfo{
		exactLanguage: languageCode,
	}
	info.candidates = appendUniqueLanguageCode(info.candidates, info.exactLanguage)

	info.standardLanguage = standardizeLanguageCode(languageCode)
	info.candidates = appendUniqueLanguageCode(info.candidates, info.standardLanguage)

	info.shortLanguage = shortLanguageCode(languageCode)
	info.candidates = appendUniqueLanguageCode(info.candidates, info.shortLanguage)
	return info
}

func standardizeLanguageCode(languageCode string) string {
	languageCode = strings.TrimSpace(languageCode)
	if languageCode == "" {
		return ""
	}
	tag, err := language.Parse(languageCode)
	if err != nil && tag.IsRoot() {
		return ""
	}
	standardLanguage := tag.String()
	if standardLanguage == "" || standardLanguage == language.Und.String() {
		return ""
	}
	return standardLanguage
}

func shortLanguageCode(languageCode string) string {
	languageCode = strings.TrimSpace(languageCode)
	if languageCode == "" {
		return ""
	}
	tag, err := language.Parse(languageCode)
	if err != nil && tag.IsRoot() {
		return ""
	}
	base, confidence := tag.Base()
	if confidence == language.No {
		return ""
	}
	shortLanguage := base.String()
	if shortLanguage == "" || shortLanguage == language.Und.String() {
		return ""
	}
	return shortLanguage
}

func appendUniqueLanguageCode(codes []string, languageCode string) []string {
	if languageCode == "" {
		return codes
	}
	for _, code := range codes {
		if code == languageCode {
			return codes
		}
	}
	return append(codes, languageCode)
}

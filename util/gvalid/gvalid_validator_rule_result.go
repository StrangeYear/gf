// Copyright GoFrame Author(https://goframe.org). All Rights Reserved.
//
// This Source Code Form is subject to the terms of the MIT License.
// If a copy of the MIT was not distributed with this file,
// You can obtain one at https://github.com/gogf/gf.

package gvalid

import (
	"sort"
	"strings"
)

func appendMissingRuleKeys(rule string, errorItem map[string]error) string {
	if len(errorItem) == 0 {
		return rule
	}
	existsRuleKeyMap := make(map[string]struct{})
	for _, ruleItem := range strings.Split(rule, "|") {
		ruleKey := strings.TrimSpace(strings.Split(ruleItem, ":")[0])
		if ruleKey != "" {
			existsRuleKeyMap[ruleKey] = struct{}{}
		}
	}
	var missingRuleKeys []string
	for ruleKey := range errorItem {
		if _, ok := internalErrKeyMap[ruleKey]; ok {
			continue
		}
		if _, ok := existsRuleKeyMap[ruleKey]; !ok {
			missingRuleKeys = append(missingRuleKeys, ruleKey)
		}
	}
	if len(missingRuleKeys) == 0 {
		return rule
	}
	sort.Strings(missingRuleKeys)
	return rule + "|" + strings.Join(missingRuleKeys, "|")
}

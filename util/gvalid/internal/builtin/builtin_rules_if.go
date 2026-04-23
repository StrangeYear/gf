// Copyright GoFrame Author(https://goframe.org). All Rights Reserved.
//
// This Source Code Form is subject to the terms of the MIT License.
// If a copy of the MIT was not distributed with this file,
// You can obtain one at https://github.com/gogf/gf.

package builtin

import (
	"strings"

	"github.com/gogf/gf/v2/errors/gcode"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/util/gconv"
	"github.com/gogf/gf/v2/util/gutil"
)

// RuleRulesIf implements `rules-if` rule:
// It runs given validation rules if any of given field and its value are equal.
//
// Format:  rules-if:field,value,rule,...
// Example: rules-if:type,email,email,type,phone,phone
//
// The rules in each branch can use `&` as the separator for multiple validation
// rules, as `|` is already the top-level rule separator.
type RuleRulesIf struct{}

func init() {
	Register(RuleRulesIf{})
}

func (r RuleRulesIf) Name() string {
	return "rules-if"
}

func (r RuleRulesIf) Message() string {
	return "The {field} value `{value}` is invalid"
}

func (r RuleRulesIf) Run(in RunInput) error {
	var (
		array   = strings.Split(in.RulePattern, ",")
		dataMap = in.Data.Map()
	)
	if len(array) == 0 || len(array)%3 != 0 {
		return gerror.NewCodef(
			gcode.CodeInvalidParameter,
			`invalid "%s" rule pattern: %s`,
			r.Name(),
			in.RulePattern,
		)
	}

	for i := 0; i < len(array); i += 3 {
		var (
			fieldName  = strings.TrimSpace(array[i])
			matchValue = strings.TrimSpace(array[i+1])
			rule       = strings.TrimSpace(array[i+2])
			matched    bool
		)
		if fieldName == "" || rule == "" {
			return gerror.NewCodef(
				gcode.CodeInvalidParameter,
				`invalid "%s" rule pattern: %s`,
				r.Name(),
				in.RulePattern,
			)
		}

		_, fieldValue := gutil.MapPossibleItemByKey(dataMap, fieldName)
		if in.Option.CaseInsensitive {
			matched = strings.EqualFold(matchValue, gconv.String(fieldValue))
		} else {
			matched = strings.Compare(matchValue, gconv.String(fieldValue)) == 0
		}
		if !matched {
			continue
		}

		rule = strings.ReplaceAll(rule, "&", "|")
		if in.Option.RunRule == nil {
			return gerror.NewCodef(
				gcode.CodeInvalidParameter,
				`missing RunRule option for "%s" rule`,
				r.Name(),
			)
		}
		ruleErrorMap, err := in.Option.RunRule(rule)
		if err != nil {
			return err
		}
		if len(ruleErrorMap) > 0 {
			return &RuleResultError{
				Rule:   rule,
				Errors: ruleErrorMap,
			}
		}
		return nil
	}
	return nil
}

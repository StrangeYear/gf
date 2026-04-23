// Copyright GoFrame Author(https://goframe.org). All Rights Reserved.
//
// This Source Code Form is subject to the terms of the MIT License.
// If a copy of the MIT was not distributed with this file,
// You can obtain one at https://github.com/gogf/gf.

// Package builtin implements built-in validation rules.
//
// Referred to Laravel validation:
// https://laravel.com/docs/master/validation#available-validation-rules
package builtin

import (
	"reflect"
	"sort"
	"strings"

	"github.com/gogf/gf/v2/container/gvar"
)

type Rule interface {
	// Name returns the builtin name of the rule.
	Name() string

	// Message returns the default error message of the rule.
	Message() string

	// Run starts running the rule, it returns nil if successful, or else an error.
	Run(in RunInput) error
}

type RunInput struct {
	RuleKey     string       // RuleKey is like the "max" in rule "max: 6"
	RulePattern string       // RulePattern is like "6" in rule:"max:6"
	Field       string       // The field name of Value.
	ValueType   reflect.Type // ValueType specifies the type of the value, which might be nil.
	Value       *gvar.Var    // Value specifies the value for this rule to validate.
	Data        *gvar.Var    // Data specifies the `data` which is passed to the Validator.
	Message     string       // Message specifies the custom error message or configured i18n message for this rule.
	Option      RunOption    // Option provides extra configuration for validation rule.
}

type RunOption struct {
	CaseInsensitive bool        // CaseInsensitive indicates that it does Case-Insensitive comparison in string.
	RunRule         RunRuleFunc // RunRule runs extra validation rules for current field value.
}

// RunRuleFunc runs extra validation rules for current field value.
type RunRuleFunc func(rule string) (map[string]error, error)

// RuleResultError is returned by rules that run extra validation rules.
type RuleResultError struct {
	Rule   string
	Errors map[string]error
}

func (e *RuleResultError) Error() string {
	if e == nil || len(e.Errors) == 0 {
		return ""
	}
	keys := make([]string, 0, len(e.Errors))
	for key := range e.Errors {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	messages := make([]string, 0, len(keys))
	for _, key := range keys {
		messages = append(messages, e.Errors[key].Error())
	}
	return strings.Join(messages, "; ")
}

var (
	// ruleMap stores all builtin validation rules.
	ruleMap = map[string]Rule{}
)

// Register registers builtin rule into manager.
func Register(rule Rule) {
	ruleMap[rule.Name()] = rule
}

// GetRule retrieves and returns rule by `name`.
func GetRule(name string) Rule {
	return ruleMap[name]
}

// Copyright GoFrame Author(https://goframe.org). All Rights Reserved.
//
// This Source Code Form is subject to the terms of the MIT License.
// If a copy of the MIT was not distributed with this file,
// You can obtain one at https://github.com/gogf/gf.

package builtin

import (
	"errors"
	"reflect"
	"strings"
	"unicode/utf8"

	"github.com/gogf/gf/v2/util/gconv"
)

// RuleContainsAny implements `contains-any` rule:
// Value should contain any of the specified characters.
//
// Format: contains-any:chars
type RuleContainsAny struct{}

func init() {
	Register(RuleContains{})
	Register(RuleContainsAny{})
	Register(RuleContainsRune{})
	Register(RuleExcludes{})
	Register(RuleExcludesAll{})
	Register(RuleStartsWith{})
	Register(RuleStartsNotWith{})
}

// RuleContains implements `contains` rule:
// Value should contain the specified pattern.
//
// Format: contains:pattern
type RuleContains struct{}

func (r RuleContains) Name() string {
	return "contains"
}

func (r RuleContains) Message() string {
	return "The {field} value `{value}` must contain {pattern}"
}

func (r RuleContains) Run(in RunInput) error {
	if containsValue(in.Value.Val(), in.RulePattern, in.Option.CaseInsensitive) {
		return nil
	}
	return errors.New(in.Message)
}

func (r RuleContainsAny) Name() string {
	return "contains-any"
}

func (r RuleContainsAny) Message() string {
	return "The {field} value `{value}` must contain any of {pattern}"
}

func (r RuleContainsAny) Run(in RunInput) error {
	if stringContainsAny(in.Value.String(), in.RulePattern, in.Option.CaseInsensitive) {
		return nil
	}
	return errors.New(in.Message)
}

// RuleContainsRune implements `contains-rune` rule:
// Value should contain the specified rune.
//
// Format: contains-rune:rune
type RuleContainsRune struct{}

func (r RuleContainsRune) Name() string {
	return "contains-rune"
}

func (r RuleContainsRune) Message() string {
	return "The {field} value `{value}` must contain rune {pattern}"
}

func (r RuleContainsRune) Run(in RunInput) error {
	if stringContainsRune(in.Value.String(), in.RulePattern, in.Option.CaseInsensitive) {
		return nil
	}
	return errors.New(in.Message)
}

// RuleExcludes implements `excludes` rule:
// Value should not contain the specified pattern.
//
// Format: excludes:pattern
type RuleExcludes struct{}

func (r RuleExcludes) Name() string {
	return "excludes"
}

func (r RuleExcludes) Message() string {
	return "The {field} value `{value}` must not contain {pattern}"
}

func (r RuleExcludes) Run(in RunInput) error {
	if !containsValue(in.Value.Val(), in.RulePattern, in.Option.CaseInsensitive) {
		return nil
	}
	return errors.New(in.Message)
}

// RuleExcludesAll implements `excludes-all` rule:
// Value should not contain any of the specified characters.
//
// Format: excludes-all:chars
type RuleExcludesAll struct{}

func (r RuleExcludesAll) Name() string {
	return "excludes-all"
}

func (r RuleExcludesAll) Message() string {
	return "The {field} value `{value}` must not contain any of {pattern}"
}

func (r RuleExcludesAll) Run(in RunInput) error {
	if !stringContainsAny(in.Value.String(), in.RulePattern, in.Option.CaseInsensitive) {
		return nil
	}
	return errors.New(in.Message)
}

// RuleStartsWith implements `starts-with` rule:
// Value should start with the specified pattern.
//
// Format: starts-with:pattern
type RuleStartsWith struct{}

func (r RuleStartsWith) Name() string {
	return "starts-with"
}

func (r RuleStartsWith) Message() string {
	return "The {field} value `{value}` must start with {pattern}"
}

func (r RuleStartsWith) Run(in RunInput) error {
	if stringHasPrefix(in.Value.String(), in.RulePattern, in.Option.CaseInsensitive) {
		return nil
	}
	return errors.New(in.Message)
}

// RuleStartsNotWith implements `starts-not-with` rule:
// Value should not start with the specified pattern.
//
// Format: starts-not-with:pattern
type RuleStartsNotWith struct{}

func (r RuleStartsNotWith) Name() string {
	return "starts-not-with"
}

func (r RuleStartsNotWith) Message() string {
	return "The {field} value `{value}` must not start with {pattern}"
}

func (r RuleStartsNotWith) Run(in RunInput) error {
	if !stringHasPrefix(in.Value.String(), in.RulePattern, in.Option.CaseInsensitive) {
		return nil
	}
	return errors.New(in.Message)
}

func normalizeStringMatch(value string, caseInsensitive bool) string {
	if caseInsensitive {
		return strings.ToLower(value)
	}
	return value
}

func containsValue(value any, pattern string, caseInsensitive bool) bool {
	reflectValue := reflect.ValueOf(value)
	for reflectValue.IsValid() && (reflectValue.Kind() == reflect.Pointer || reflectValue.Kind() == reflect.Interface) {
		if reflectValue.IsNil() {
			return false
		}
		reflectValue = reflectValue.Elem()
	}
	if reflectValue.IsValid() {
		switch reflectValue.Kind() {
		case reflect.Array, reflect.Slice:
			if isStringCollection(reflectValue) {
				return stringSliceContains(reflectValue, pattern, caseInsensitive)
			}
		}
	}
	return stringContains(gconv.String(value), pattern, caseInsensitive)
}

func stringContains(value, pattern string, caseInsensitive bool) bool {
	value = normalizeStringMatch(value, caseInsensitive)
	pattern = normalizeStringMatch(pattern, caseInsensitive)
	return strings.Contains(value, pattern)
}

func stringSliceContains(reflectValue reflect.Value, pattern string, caseInsensitive bool) bool {
	pattern = normalizeStringMatch(pattern, caseInsensitive)
	for i := 0; i < reflectValue.Len(); i++ {
		item := reflectValue.Index(i)
		for item.IsValid() && (item.Kind() == reflect.Pointer || item.Kind() == reflect.Interface) {
			if item.IsNil() {
				goto Next
			}
			item = item.Elem()
		}
		if !item.IsValid() {
			goto Next
		}
		if item.Kind() != reflect.String {
			return false
		}
		if normalizeStringMatch(item.String(), caseInsensitive) == pattern {
			return true
		}
	Next:
	}
	return false
}

func isStringCollection(reflectValue reflect.Value) bool {
	for i := 0; i < reflectValue.Len(); i++ {
		item := reflectValue.Index(i)
		for item.IsValid() && (item.Kind() == reflect.Pointer || item.Kind() == reflect.Interface) {
			if item.IsNil() {
				goto Next
			}
			item = item.Elem()
		}
		if item.IsValid() && item.Kind() != reflect.String {
			return false
		}
	Next:
	}
	return true
}

func stringContainsAny(value, pattern string, caseInsensitive bool) bool {
	value = normalizeStringMatch(value, caseInsensitive)
	pattern = normalizeStringMatch(pattern, caseInsensitive)
	return strings.ContainsAny(value, pattern)
}

func stringContainsRune(value, pattern string, caseInsensitive bool) bool {
	value = normalizeStringMatch(value, caseInsensitive)
	pattern = normalizeStringMatch(pattern, caseInsensitive)
	r, _ := utf8.DecodeRuneInString(pattern)
	if r == utf8.RuneError && pattern == "" {
		return false
	}
	return strings.ContainsRune(value, r)
}

func stringHasPrefix(value, pattern string, caseInsensitive bool) bool {
	value = normalizeStringMatch(value, caseInsensitive)
	pattern = normalizeStringMatch(pattern, caseInsensitive)
	return strings.HasPrefix(value, pattern)
}

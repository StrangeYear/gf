// Copyright GoFrame Author(https://goframe.org). All Rights Reserved.
//
// This Source Code Form is subject to the terms of the MIT License.
// If a copy of the MIT was not distributed with this file,
// You can obtain one at https://github.com/gogf/gf.

package gvalid

import (
	"context"
	"reflect"

	"github.com/gogf/gf/v2/errors/gcode"
	"github.com/gogf/gf/v2/internal/empty"
	"github.com/gogf/gf/v2/os/gstructs"
	"github.com/gogf/gf/v2/util/gconv"
	"github.com/gogf/gf/v2/util/gutil"
)

func (v *Validator) doCheckStruct(ctx context.Context, object any) Error {
	var (
		errorMaps           = make(map[string]map[string]error) // Returning error.
		fieldToAliasNameMap = make(map[string]string)           // Field names to alias name map.
		ruleToFieldNameMap  = make(map[string]string)           // Validation rule names to struct field names map.
		resultSequenceRules = make([]fieldRule, 0)
		isEmptyData         = empty.IsEmpty(v.data)
		isEmptyAssoc        = empty.IsEmpty(v.assoc)
	)
	fieldMap, err := gstructs.FieldMap(gstructs.FieldMapInput{
		Pointer:          object,
		PriorityTagArray: aliasNameTagPriority,
		RecursiveOption:  gstructs.RecursiveOptionEmbedded,
	})
	if err != nil {
		return newValidationErrorByStr(internalObjectErrRuleName, err)
	}
	cacheMeta, err := getOrBuildStructRuleCacheMeta(object)
	if err != nil {
		return newValidationErrorByStr(internalObjectErrRuleName, err)
	}

	var (
		inputParamMap  map[string]any
		checkRules     = make([]fieldRule, 0)
		nameToRuleMap  = make(map[string]string) // just for internally searching index purpose.
		customMessage  = make(CustomMsg)         // Custom rule error message map.
		checkValueData = v.assoc                 // Ready to be validated data.
	)
	// If there's no struct tag and validation rules, it does nothing and returns quickly.
	if !cacheMeta.hasTagFields && v.rules == nil && v.messages == nil && isEmptyData && isEmptyAssoc {
		return nil
	}
	if v.rules == nil && v.messages == nil {
		checkRules = cloneFieldRules(cacheMeta.checkRules)
		customMessage = cloneCustomMessages(cacheMeta.customMessage)
		fieldToAliasNameMap = cloneStringMap(cacheMeta.fieldToAliasNameMap)
		ruleToFieldNameMap = cloneStringMap(cacheMeta.ruleToFieldNameMap)
		for _, item := range checkRules {
			nameToRuleMap[item.Name] = item.Rule
		}
	} else {
		switch assertValue := v.rules.(type) {
		// Sequence tag: []sequence tag
		// Sequence has order for error results.
		case []string:
			for _, tag := range assertValue {
				name, rule, msg := ParseTagValue(tag)
				if len(name) == 0 {
					continue
				}
				if len(msg) > 0 {
					appendRuleMessages(customMessage, name, rule, msg)
				}
				nameToRuleMap[name] = rule
				checkRules = append(checkRules, fieldRule{
					Name: name,
					Rule: rule,
				})
			}

		// Map type rules does not support sequence.
		// Format: map[key]rule
		case map[string]string:
			nameToRuleMap = cloneStringMap(assertValue)
			checkRules = make([]fieldRule, 0, len(assertValue))
			for name, rule := range assertValue {
				checkRules = append(checkRules, fieldRule{
					Name: name,
					Rule: rule,
				})
			}
		}
		// If there's no struct tag and validation rules, it does nothing and returns quickly.
		if len(cacheMeta.tagFields) == 0 && v.messages == nil && len(checkRules) == 0 && isEmptyData && isEmptyAssoc {
			return nil
		}
		// Merge the custom validation rules with rules in struct tag.
		// The custom rules has the most high priority that can overwrite the struct tag rules.
		for _, cachedField := range cacheMeta.tagFields {
			var (
				fieldName = cachedField.fieldName
				name      = cachedField.name
				rule      = cachedField.rule
				msg       = cachedField.msg
			)
			if len(name) == 0 {
				if value, ok := fieldToAliasNameMap[fieldName]; ok {
					// It uses alias name of the attribute if its alias name tag exists.
					name = value
				} else {
					// It or else uses the attribute name directly.
					name = cachedField.tagPriorityName
				}
			} else {
				// It uses the alias name from validation rule.
				fieldToAliasNameMap[fieldName] = name
			}
			ruleToFieldNameMap[name] = fieldName

			if _, ok := nameToRuleMap[name]; !ok {
				if _, ok = nameToRuleMap[fieldName]; ok {
					// If there's alias name,
					// use alias name as its key and remove the field name key.
					nameToRuleMap[name] = nameToRuleMap[fieldName]
					delete(nameToRuleMap, fieldName)
					delete(ruleToFieldNameMap, fieldName)
					for index, checkRuleItem := range checkRules {
						if fieldName == checkRuleItem.Name {
							checkRuleItem.Name = name
							checkRules[index] = checkRuleItem
							break
						}
					}
				} else {
					nameToRuleMap[name] = rule
					checkRules = append(checkRules, fieldRule{
						Name:      name,
						Rule:      rule,
						IsMeta:    cachedField.isMeta,
						FieldKind: cachedField.fieldKind,
						FieldType: cachedField.fieldType,
					})
				}
			} else {
				// The input rules can overwrite the rules in struct tag.
				continue
			}

			if len(msg) > 0 {
				appendRuleMessages(customMessage, name, rule, msg)
			}
		}
	}
	if checkValueData == nil {
		checkValueData = object
	}
	// Input parameter map handling.
	if v.assoc == nil || !v.useAssocInsteadOfObjectAttributes {
		inputParamMap = make(map[string]any)
	} else {
		inputParamMap = gconv.Map(v.assoc)
	}
	// Checks and extends the parameters map with struct alias tag.
	if !v.useAssocInsteadOfObjectAttributes {
		for nameOrTag, field := range fieldMap {
			inputParamMap[nameOrTag] = field.Value.Interface()
			if nameOrTag != field.Name() {
				inputParamMap[field.Name()] = field.Value.Interface()
			}
		}
	}

	// Custom error messages,
	// which have the most priority than `rules` and struct tag.
	if msg, ok := v.messages.(CustomMsg); ok && len(msg) > 0 {
		for k, msgName := range msg {
			if aliasName, ok := fieldToAliasNameMap[k]; ok {
				// Overwrite the key of field name.
				customMessage[aliasName] = msgName
			} else {
				customMessage[k] = msgName
			}
		}
	}

	// Temporary variable for value.
	var value any

	// It checks the struct recursively if its attribute is a struct/struct slice.
	for _, field := range fieldMap {
		// No validation interface implements check.
		if _, ok := field.Value.Interface().(iNoValidation); ok {
			continue
		}
		// No validation field tag check.
		if _, ok := field.TagLookup(noValidationTagName); ok {
			continue
		}
		if field.IsEmbedded() {
			// The attributes of embedded struct are considered as direct attributes of its parent struct.
			if err = v.doCheckStruct(ctx, field.Value); err != nil {
				// It merges the errors into single error map.
				for k, m := range err.(*validationError).errors {
					errorMaps[k] = m
				}
			}
		} else {
			// The `field.TagValue` is the alias name of field.Name().
			// Eg, value from struct tag `p`.
			if field.TagValue != "" {
				fieldToAliasNameMap[field.Name()] = field.TagValue
			}
			switch field.OriginalKind() {
			case reflect.Map, reflect.Struct, reflect.Slice, reflect.Array:
				// Recursively check attribute slice/map.
				value = getPossibleValueFromMap(
					inputParamMap, field.Name(), fieldToAliasNameMap[field.Name()],
				)
				if empty.IsNil(value) {
					switch field.Kind() {
					case reflect.Map, reflect.Pointer, reflect.Slice, reflect.Array:
						// Nothing to do.
						continue
					default:
					}
				}
				v.doCheckValueRecursively(ctx, doCheckValueRecursivelyInput{
					Value:               value,
					Kind:                field.OriginalKind(),
					Type:                field.Type().Type,
					ErrorMaps:           errorMaps,
					ResultSequenceRules: &resultSequenceRules,
				})
			default:
			}
		}
		if v.bail && len(errorMaps) > 0 {
			break
		}
	}
	if v.bail && len(errorMaps) > 0 {
		return newValidationError(gcode.CodeValidationFailed, resultSequenceRules, errorMaps)
	}

	// The following logic is the same as some of CheckMap but with sequence support.
	for index, checkRuleItem := range checkRules {
		// it ignores Meta object.
		if !checkRuleItem.IsMeta {
			value = getPossibleValueFromMap(
				inputParamMap, checkRuleItem.Name, ruleToFieldNameMap[checkRuleItem.Name],
			)
		}
		// Empty json string checks according to mapping field kind.
		if value != nil {
			switch checkRuleItem.FieldKind {
			case reflect.Struct, reflect.Map:
				// empty struct or map.
				if gconv.String(value) == emptyJsonObjectStr {
					value = nil
				}
			case reflect.Slice, reflect.Array:
				// empty slice.
				if gconv.String(value) == emptyJsonArrayStr {
					value = []any{}
				}
			default:
			}
		}
		// It checks each rule and its value in loop.
		if validatedError := v.doCheckValue(ctx, doCheckValueInput{
			Name:      checkRuleItem.Name,
			Value:     value,
			ValueType: checkRuleItem.FieldType,
			Rule:      checkRuleItem.Rule,
			Messages:  customMessage[checkRuleItem.Name],
			DataRaw:   checkValueData,
			DataMap:   inputParamMap,
		}); validatedError != nil {
			_, errorItem := validatedError.FirstItem()
			checkRules[index].Rule = appendMissingRuleKeys(checkRuleItem.Rule, errorItem)
			// ============================================================
			// Only in map and struct validations:
			// If value is nil or empty string and has no required* rules,
			// it clears the error message.
			// ============================================================
			if !checkRuleItem.IsMeta && (value == nil || gconv.String(value) == "") {
				required := false
				// rule => error
				for ruleKey := range errorItem {
					// it checks whether current rule is kind of required rule.
					if required = v.checkRuleRequired(ruleKey); required {
						break
					}
				}
				if !required {
					continue
				}
			}
			if _, ok := errorMaps[checkRuleItem.Name]; !ok {
				errorMaps[checkRuleItem.Name] = make(map[string]error)
			}
			for ruleKey, errorItemMsgMap := range errorItem {
				errorMaps[checkRuleItem.Name][ruleKey] = errorItemMsgMap
			}
			// Bail feature.
			if v.bail {
				break
			}
		}
	}
	if len(errorMaps) > 0 {
		return newValidationError(
			gcode.CodeValidationFailed,
			append(checkRules, resultSequenceRules...),
			errorMaps,
		)
	}
	return nil
}

func getPossibleValueFromMap(inputParamMap map[string]any, fieldName, aliasName string) (value any) {
	_, value = gutil.MapPossibleItemByKey(inputParamMap, fieldName)
	if value == nil && aliasName != "" {
		_, value = gutil.MapPossibleItemByKey(inputParamMap, aliasName)
	}
	return
}

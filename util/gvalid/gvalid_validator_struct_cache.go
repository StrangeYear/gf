package gvalid

import (
	"reflect"
	"strings"
	"sync"

	"github.com/gogf/gf/v2/os/gstructs"
	"github.com/gogf/gf/v2/util/gmeta"
)

type structRuleCacheMeta struct {
	hasTagFields        bool
	checkRules          []fieldRule
	customMessage       CustomMsg
	fieldToAliasNameMap map[string]string
	ruleToFieldNameMap  map[string]string
	tagFields           []structRuleTagFieldMeta
}

type structRuleTagFieldMeta struct {
	fieldName       string
	tagPriorityName string
	name            string
	rule            string
	msg             string
	isMeta          bool
	fieldKind       reflect.Kind
	fieldType       reflect.Type
}

var structRuleCache sync.Map

func getOrBuildStructRuleCacheMeta(object any) (*structRuleCacheMeta, error) {
	structType, err := gstructs.StructType(object)
	if err != nil {
		return nil, err
	}
	if cachedMeta, ok := structRuleCache.Load(structType.Type); ok {
		return cachedMeta.(*structRuleCacheMeta), nil
	}
	meta, err := buildStructRuleCacheMeta(structType.Type)
	if err != nil {
		return nil, err
	}
	actualMeta, _ := structRuleCache.LoadOrStore(structType.Type, meta)
	return actualMeta.(*structRuleCacheMeta), nil
}

func buildStructRuleCacheMeta(structType reflect.Type) (*structRuleCacheMeta, error) {
	object := reflect.New(structType).Interface()
	tagFields, err := gstructs.TagFields(object, structTagPriority)
	if err != nil {
		return nil, err
	}
	meta := &structRuleCacheMeta{
		hasTagFields:        len(tagFields) > 0,
		checkRules:          make([]fieldRule, 0, len(tagFields)),
		customMessage:       make(CustomMsg),
		fieldToAliasNameMap: make(map[string]string),
		ruleToFieldNameMap:  make(map[string]string),
		tagFields:           make([]structRuleTagFieldMeta, 0, len(tagFields)),
	}
	nameToRuleMap := make(map[string]string)
	for _, field := range tagFields {
		fieldName := field.Name()
		name, rule, msg := ParseTagValue(field.TagValue)
		tagFieldMeta := structRuleTagFieldMeta{
			fieldName:       fieldName,
			tagPriorityName: field.TagPriorityName(),
			name:            name,
			rule:            rule,
			msg:             msg,
			isMeta:          field.Field.Type == reflect.TypeOf(gmeta.Meta{}),
			fieldKind:       field.OriginalKind(),
			fieldType:       field.Type().Type,
		}
		meta.tagFields = append(meta.tagFields, tagFieldMeta)
		if len(name) == 0 {
			name = tagFieldMeta.tagPriorityName
		} else {
			meta.fieldToAliasNameMap[fieldName] = name
		}
		meta.ruleToFieldNameMap[name] = fieldName
		if _, ok := nameToRuleMap[name]; !ok {
			nameToRuleMap[name] = rule
			meta.checkRules = append(meta.checkRules, fieldRule{
				Name:      name,
				Rule:      rule,
				IsMeta:    tagFieldMeta.isMeta,
				FieldKind: tagFieldMeta.fieldKind,
				FieldType: tagFieldMeta.fieldType,
			})
		}
		if len(msg) > 0 {
			msgArray := strings.Split(msg, "|")
			ruleArray := strings.Split(rule, "|")
			for index, ruleKey := range ruleArray {
				if len(msgArray) <= index || len(msgArray[index]) == 0 {
					continue
				}
				array := strings.Split(ruleKey, ":")
				if _, ok := meta.customMessage[name]; !ok {
					meta.customMessage[name] = make(map[string]string)
				}
				meta.customMessage[name].(map[string]string)[strings.TrimSpace(array[0])] = strings.TrimSpace(msgArray[index])
			}
		}
	}
	return meta, nil
}

func cloneFieldRules(rules []fieldRule) []fieldRule {
	if len(rules) == 0 {
		return nil
	}
	clonedRules := make([]fieldRule, len(rules))
	copy(clonedRules, rules)
	return clonedRules
}

func cloneCustomMessages(messages CustomMsg) CustomMsg {
	if len(messages) == 0 {
		return make(CustomMsg)
	}
	clonedMessages := make(CustomMsg, len(messages))
	for key, value := range messages {
		if messageMap, ok := value.(map[string]string); ok {
			clonedMessageMap := make(map[string]string, len(messageMap))
			for messageKey, messageValue := range messageMap {
				clonedMessageMap[messageKey] = messageValue
			}
			clonedMessages[key] = clonedMessageMap
			continue
		}
		clonedMessages[key] = value
	}
	return clonedMessages
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return make(map[string]string)
	}
	clonedValues := make(map[string]string, len(values))
	for key, value := range values {
		clonedValues[key] = value
	}
	return clonedValues
}

func appendRuleMessages(customMessage CustomMsg, name, rule, msg string) {
	var (
		msgArray  = strings.Split(msg, "|")
		ruleArray = strings.Split(rule, "|")
	)
	for index, ruleKey := range ruleArray {
		// If length of custom messages is lesser than length of rules,
		// the rest rules use the default error messages.
		if len(msgArray) <= index {
			continue
		}
		if len(msgArray[index]) == 0 {
			continue
		}
		array := strings.Split(ruleKey, ":")
		if _, ok := customMessage[name]; !ok {
			customMessage[name] = make(map[string]string)
		}
		customMessage[name].(map[string]string)[strings.TrimSpace(array[0])] = strings.TrimSpace(msgArray[index])
	}
}

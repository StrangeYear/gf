// Copyright GoFrame Author(https://goframe.org). All Rights Reserved.
//
// This Source Code Form is subject to the terms of the MIT License.
// If a copy of the MIT was not distributed with this file,
// You can obtain one at https://github.com/gogf/gf.

package ghttp

import (
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"unsafe"

	"github.com/gogf/gf/v2/internal/utils"
	"github.com/gogf/gf/v2/net/goai"
	"github.com/gogf/gf/v2/util/gmeta"
	"github.com/gogf/gf/v2/util/gtag"
)

const (
	strictFastBindSourcePath = iota
	strictFastBindSourceQuery
)

var strictFastBindMetaType = reflect.TypeOf(gmeta.Meta{})

type strictRequestFastBindMeta struct {
	fields []strictRequestFastBindField
}

type strictRequestFastBindField struct {
	keys     []string
	fuzzyKey string
	offset   uintptr
	source   int
	kind     reflect.Kind
	bits     int
}

func buildStrictRequestFastBindMeta(info *handlerFuncInfo) *strictRequestFastBindMeta {
	if !info.IsStrictRoute ||
		info.ReqStructType == nil ||
		info.ReqStructHasCustomParser ||
		info.ReqStructHasParseTag ||
		info.ReqStructNeedsValidation ||
		len(info.ReqStructDefaults) > 0 ||
		len(info.ReqStructIn) > 0 {
		return nil
	}

	var fields []strictRequestFastBindField
	for i := 0; i < info.ReqStructType.NumField(); i++ {
		field := info.ReqStructType.Field(i)
		if field.PkgPath != "" {
			continue
		}
		if field.Anonymous {
			if strictFastBindIsMetaField(field.Type) {
				continue
			}
			return nil
		}

		source, ok := strictFastBindSource(field)
		if !ok {
			return nil
		}
		kind, bits, ok := strictFastBindScalarKind(field.Type)
		if !ok {
			return nil
		}
		keys := strictFastBindFieldKeys(field)
		if len(keys) == 0 {
			return nil
		}
		fields = append(fields, strictRequestFastBindField{
			keys:     keys,
			fuzzyKey: utils.RemoveSymbols(field.Name),
			offset:   field.Offset,
			source:   source,
			kind:     kind,
			bits:     bits,
		})
	}
	return &strictRequestFastBindMeta{fields: fields}
}

func strictFastBindIsMetaField(t reflect.Type) bool {
	if t == strictFastBindMetaType {
		return true
	}
	return t.Kind() == reflect.Pointer && t.Elem() == strictFastBindMetaType
}

func strictFastBindSource(field reflect.StructField) (source int, ok bool) {
	switch field.Tag.Get(gtag.In) {
	case goai.ParameterInPath:
		return strictFastBindSourcePath, true
	case goai.ParameterInQuery:
		return strictFastBindSourceQuery, true
	default:
		return 0, false
	}
}

func strictFastBindScalarKind(t reflect.Type) (kind reflect.Kind, bits int, ok bool) {
	kind = t.Kind()
	switch kind {
	case reflect.String, reflect.Bool:
		return kind, 0, true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return kind, t.Bits(), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return kind, t.Bits(), true
	case reflect.Float32, reflect.Float64:
		return kind, t.Bits(), true
	default:
		return 0, 0, false
	}
}

func strictFastBindFieldKeys(field reflect.StructField) []string {
	keys := make([]string, 0, 2)
	for _, tag := range gtag.StructTagPriority {
		if value := strictFastBindTagName(field.Tag.Get(tag)); value != "" {
			keys = append(keys, value)
		}
	}
	return strictFastBindUniqueKeys(keys)
}

func strictFastBindUniqueKeys(keys []string) []string {
	if len(keys) < 2 {
		return keys
	}
	n := 0
	for _, key := range keys {
		exists := false
		for i := 0; i < n; i++ {
			if keys[i] == key {
				exists = true
				break
			}
		}
		if !exists {
			keys[n] = key
			n++
		}
	}
	return keys[:n]
}

func strictFastBindTagName(value string) string {
	if value == "" {
		return ""
	}
	if index := strings.IndexByte(value, ','); index >= 0 {
		value = value[:index]
	}
	value = strings.TrimSpace(value)
	if value == "-" {
		return ""
	}
	return value
}

func (r *Request) bindStrictRouteRequestFast(pointer any) (ok bool, err error) {
	if r == nil || r.serveHandler == nil || r.serveHandler.Handler == nil {
		return false, nil
	}
	meta := r.serveHandler.Handler.Info.ReqStructFastBindMeta
	if meta == nil {
		return false, nil
	}
	reflectValue := reflect.ValueOf(pointer)
	if reflectValue.Kind() != reflect.Pointer || reflectValue.IsNil() {
		return false, nil
	}
	basePointer := unsafe.Pointer(reflectValue.Pointer())
	for _, field := range meta.fields {
		value, found, fallback := r.strictFastBindLookup(field)
		if fallback {
			return false, nil
		}
		if !found {
			continue
		}
		strictFastBindSetValue(basePointer, field, value)
	}
	return true, nil
}

func (r *Request) strictFastBindLookup(field strictRequestFastBindField) (
	value string, found bool, fallback bool,
) {
	switch field.source {
	case strictFastBindSourcePath:
		if len(r.routerMap) == 0 {
			return "", false, false
		}
		for _, key := range field.keys {
			if v, ok := r.routerMap[key]; ok {
				return v, true, false
			}
		}
		for key, v := range r.routerMap {
			if utils.EqualFoldWithoutChars(key, field.fuzzyKey) {
				return v, true, false
			}
		}

	case strictFastBindSourceQuery:
		return strictFastBindLookupQuery(r.URL.RawQuery, field.keys, field.fuzzyKey)
	}
	return "", false, false
}

func strictFastBindLookupQuery(rawQuery string, keys []string, fuzzyKey string) (
	value string, found bool, fallback bool,
) {
	if rawQuery == "" || len(keys) == 0 {
		return "", false, false
	}
	for _, key := range keys {
		value, found, fallback = strictFastBindLookupQueryKey(rawQuery, key)
		if found || fallback {
			return value, found, fallback
		}
	}
	return strictFastBindLookupQueryFuzzy(rawQuery, fuzzyKey)
}

func strictFastBindLookupQueryKey(rawQuery string, key string) (
	value string, found bool, fallback bool,
) {
	for start := 0; start <= len(rawQuery); {
		end := strings.IndexByte(rawQuery[start:], '&')
		if end < 0 {
			end = len(rawQuery)
		} else {
			end += start
		}
		part := rawQuery[start:end]
		start = end + 1
		pos := strings.IndexByte(part, '=')
		if pos <= 0 {
			continue
		}
		decodedKey, ok := strictFastBindUnescapeQuery(part[:pos])
		if !ok {
			return "", false, true
		}
		if decodedKey != key {
			continue
		}
		v, ok := strictFastBindUnescapeQuery(part[pos+1:])
		if !ok {
			return "", false, true
		}
		value = v
		found = true
	}
	return value, found, false
}

func strictFastBindLookupQueryFuzzy(rawQuery string, fuzzyKey string) (
	value string, found bool, fallback bool,
) {
	if fuzzyKey == "" {
		return "", false, false
	}
	for start := 0; start <= len(rawQuery); {
		end := strings.IndexByte(rawQuery[start:], '&')
		if end < 0 {
			end = len(rawQuery)
		} else {
			end += start
		}
		part := rawQuery[start:end]
		start = end + 1
		pos := strings.IndexByte(part, '=')
		if pos <= 0 {
			continue
		}
		key, ok := strictFastBindUnescapeQuery(part[:pos])
		if !ok {
			return "", false, true
		}
		if !utils.EqualFoldWithoutChars(key, fuzzyKey) {
			continue
		}
		v, ok := strictFastBindUnescapeQuery(part[pos+1:])
		if !ok {
			return "", false, true
		}
		value = v
		found = true
	}
	return value, found, false
}

func strictFastBindUnescapeQuery(value string) (string, bool) {
	if !strings.ContainsAny(value, "%+") {
		return value, true
	}
	decoded, err := url.QueryUnescape(value)
	return decoded, err == nil
}

func strictFastBindSetValue(basePointer unsafe.Pointer, field strictRequestFastBindField, value string) {
	fieldPointer := unsafe.Add(basePointer, field.offset)
	switch field.kind {
	case reflect.String:
		*(*string)(fieldPointer) = value

	case reflect.Bool:
		*(*bool)(fieldPointer) = strictFastBindParseBool(value)

	case reflect.Int:
		*(*int)(fieldPointer) = int(strictFastBindParseInt(value, field.bits))
	case reflect.Int8:
		*(*int8)(fieldPointer) = int8(strictFastBindParseInt(value, field.bits))
	case reflect.Int16:
		*(*int16)(fieldPointer) = int16(strictFastBindParseInt(value, field.bits))
	case reflect.Int32:
		*(*int32)(fieldPointer) = int32(strictFastBindParseInt(value, field.bits))
	case reflect.Int64:
		*(*int64)(fieldPointer) = strictFastBindParseInt(value, field.bits)

	case reflect.Uint:
		*(*uint)(fieldPointer) = uint(strictFastBindParseUint(value, field.bits))
	case reflect.Uint8:
		*(*uint8)(fieldPointer) = uint8(strictFastBindParseUint(value, field.bits))
	case reflect.Uint16:
		*(*uint16)(fieldPointer) = uint16(strictFastBindParseUint(value, field.bits))
	case reflect.Uint32:
		*(*uint32)(fieldPointer) = uint32(strictFastBindParseUint(value, field.bits))
	case reflect.Uint64:
		*(*uint64)(fieldPointer) = strictFastBindParseUint(value, field.bits)

	case reflect.Float32:
		*(*float32)(fieldPointer) = float32(strictFastBindParseFloat(value, field.bits))
	case reflect.Float64:
		*(*float64)(fieldPointer) = strictFastBindParseFloat(value, field.bits)
	}
}

func strictFastBindParseBool(value string) bool {
	switch strings.ToLower(value) {
	case "", "0", "false", "off", "no":
		return false
	default:
		return true
	}
}

func strictFastBindParseInt(value string, bits int) int64 {
	v, err := strconv.ParseInt(value, 10, bits)
	if err != nil {
		return 0
	}
	return v
}

func strictFastBindParseUint(value string, bits int) uint64 {
	v, err := strconv.ParseUint(value, 10, bits)
	if err != nil {
		return 0
	}
	return v
}

func strictFastBindParseFloat(value string, bits int) float64 {
	v, err := strconv.ParseFloat(value, bits)
	if err != nil {
		return 0
	}
	return v
}

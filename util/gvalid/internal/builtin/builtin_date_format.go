// Copyright GoFrame Author(https://goframe.org). All Rights Reserved.
//
// This Source Code Form is subject to the terms of the MIT License.
// If a copy of the MIT was not distributed with this file,
// You can obtain one at https://github.com/gogf/gf.

// This file provides date-format validation rule support.

package builtin

import (
	"errors"
	"time"

	"github.com/gogf/gf/v2/os/gtime"
)

// RuleDateFormat implements `date-format` rule:
// Custom date format.
//
// Format: date-format:format
type RuleDateFormat struct{}

// Go standard layout keys supported by the date-format rule.
const (
	goTimeLayoutNameLayout      = "layout"
	goTimeLayoutNameANSIC       = "ansic"
	goTimeLayoutNameUnixDate    = "unix-date"
	goTimeLayoutNameRubyDate    = "ruby-date"
	goTimeLayoutNameRFC822      = "rfc822"
	goTimeLayoutNameRFC822Z     = "rfc822-z"
	goTimeLayoutNameRFC850      = "rfc850"
	goTimeLayoutNameRFC1123     = "rfc1123"
	goTimeLayoutNameRFC1123Z    = "rfc1123-z"
	goTimeLayoutNameRFC3339     = "rfc3339"
	goTimeLayoutNameRFC3339Nano = "rfc3339-nano"
	goTimeLayoutNameKitchen     = "kitchen"
	goTimeLayoutNameStamp       = "stamp"
	goTimeLayoutNameStampMilli  = "stamp-milli"
	goTimeLayoutNameStampMicro  = "stamp-micro"
	goTimeLayoutNameStampNano   = "stamp-nano"
	goTimeLayoutNameDateTime    = "date-time"
	goTimeLayoutNameDateOnly    = "date-only"
	goTimeLayoutNameTimeOnly    = "time-only"
)

// goTimeLayouts maps Go standard layout keys to their layout values.
var goTimeLayouts = map[string]string{
	goTimeLayoutNameLayout:      time.Layout,
	goTimeLayoutNameANSIC:       time.ANSIC,
	goTimeLayoutNameUnixDate:    time.UnixDate,
	goTimeLayoutNameRubyDate:    time.RubyDate,
	goTimeLayoutNameRFC822:      time.RFC822,
	goTimeLayoutNameRFC822Z:     time.RFC822Z,
	goTimeLayoutNameRFC850:      time.RFC850,
	goTimeLayoutNameRFC1123:     time.RFC1123,
	goTimeLayoutNameRFC1123Z:    time.RFC1123Z,
	goTimeLayoutNameRFC3339:     time.RFC3339,
	goTimeLayoutNameRFC3339Nano: time.RFC3339Nano,
	goTimeLayoutNameKitchen:     time.Kitchen,
	goTimeLayoutNameStamp:       time.Stamp,
	goTimeLayoutNameStampMilli:  time.StampMilli,
	goTimeLayoutNameStampMicro:  time.StampMicro,
	goTimeLayoutNameStampNano:   time.StampNano,
	goTimeLayoutNameDateTime:    time.DateTime,
	goTimeLayoutNameDateOnly:    time.DateOnly,
	goTimeLayoutNameTimeOnly:    time.TimeOnly,
}

func init() {
	Register(RuleDateFormat{})
}

func (r RuleDateFormat) Name() string {
	return "date-format"
}

func (r RuleDateFormat) Message() string {
	return "The {field} value `{value}` does not match the format: {pattern}"
}

func (r RuleDateFormat) Run(in RunInput) error {
	type iTime interface {
		Date() (year int, month time.Month, day int)
		IsZero() bool
	}
	// support for time value, eg: gtime.Time/*gtime.Time, time.Time/*time.Time.
	if obj, ok := in.Value.Val().(iTime); ok {
		if obj.IsZero() {
			return errors.New(in.Message)
		}
		return nil
	}
	// Use Go's default time.Time JSON layout when no rule pattern is provided.
	if in.RulePattern == "" {
		if _, err := gtime.StrToTimeLayout(in.Value.String(), time.RFC3339Nano); err != nil {
			return errors.New(in.Message)
		}
		return nil
	}
	if layout, ok := goTimeLayouts[in.RulePattern]; ok {
		if _, err := gtime.StrToTimeLayout(in.Value.String(), layout); err != nil {
			return errors.New(in.Message)
		}
		return nil
	}
	if _, err := gtime.StrToTimeFormat(in.Value.String(), in.RulePattern); err != nil {
		return errors.New(in.Message)
	}
	return nil
}

// Copyright GoFrame Author(https://goframe.org). All Rights Reserved.
//
// This Source Code Form is subject to the terms of the MIT License.
// If a copy of the MIT was not distributed with this file,
// You can obtain one at https://github.com/gogf/gf.

package gvalid_test

import (
	"context"
	"testing"

	"github.com/gogf/gf/v2/frame/g"
)

func BenchmarkInternal_CheckStructWithRulesMessages(b *testing.B) {
	type Request struct {
		Name string `valid:"name@required|length:2,20#name is required|name length must be between {min} and {max}"`
		Type int    `valid:"type@required#type is required"`
	}
	var (
		ctx      = context.TODO()
		object   = &Request{Name: "john", Type: 1}
		rules    = map[string]string{"Name": "required|length:2,20", "Type": "required"}
		messages = map[string]any{"Name": "invalid name", "Type": "invalid type"}
	)
	b.ReportAllocs()
	b.Run("messages_only", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if err := g.Validator().Data(object).Messages(messages).Run(ctx); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("rules_and_messages", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if err := g.Validator().Data(object).Rules(rules).Messages(messages).Run(ctx); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// Copyright GoFrame gf Author(https://goframe.org). All Rights Reserved.
//
// This Source Code Form is subject to the terms of the MIT License.
// If a copy of the MIT was not distributed with this file,
// You can obtain one at https://github.com/gogf/gf.

package genctrl

import (
	"fmt"
	"strings"

	"github.com/gogf/gf/v2/text/gstr"
)

type apiItem struct {
	Import     string `eg:"demo.com/api/user/v1"`
	FileName   string `eg:"user"`
	Module     string `eg:"user"`
	Version    string `eg:"v1"`
	MethodName string `eg:"GetList"`
	Comment    string `eg:"GetList get list"`
}

func (a apiItem) String() string {
	return gstr.Join([]string{
		a.Import, a.Module, a.Version, a.MethodName,
	}, ",")
}

// GetComment returns the comment of apiItem.
func (a apiItem) GetComment() string {
	if a.Comment == "" {
		return ""
	}
	return formatMethodComment(a.MethodName, a.Comment, "")
}

// GetInterfaceComment returns the comment for an interface method.
func (a apiItem) GetInterfaceComment() string {
	if a.Comment == "" {
		return ""
	}
	return formatMethodComment(a.MethodName, a.Comment, "\t")
}

func formatMethodComment(methodName, comment, indent string) string {
	lines := strings.Split(comment, "\n")
	var builder strings.Builder
	firstLine := true
	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		builder.WriteString("\n")
		builder.WriteString(indent)
		builder.WriteString("// ")
		if firstLine {
			builder.WriteString(fmt.Sprintf("%s %s", methodName, line))
			firstLine = false
		} else {
			builder.WriteString(line)
		}
	}
	return builder.String()
}

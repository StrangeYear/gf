package data

import "github.com/gogf/gf/os/gres"

func init() {
	if err := gres.Add("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"); err != nil {
		panic("add binary content to resource manager failed: " + err.Error())
	}
}

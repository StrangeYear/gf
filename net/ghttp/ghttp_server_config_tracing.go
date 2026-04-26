// Copyright GoFrame Author(https://goframe.org). All Rights Reserved.
//
// This Source Code Form is subject to the terms of the MIT License.
// If a copy of the MIT was not distributed with this file,
// You can obtain one at https://github.com/gogf/gf.

package ghttp

import "context"

// SetTracingEnabled enables/disables the built-in OpenTelemetry tracing middleware.
func (s *Server) SetTracingEnabled(enabled bool) {
	if s.config.TracingEnabled == enabled {
		return
	}
	s.config.TracingEnabled = enabled
	s.clearServeCache(context.TODO())
}

// IsTracingEnabled checks whether the built-in OpenTelemetry tracing middleware is enabled.
func (s *Server) IsTracingEnabled() bool {
	return s.config.TracingEnabled
}

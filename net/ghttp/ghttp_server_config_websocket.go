// Copyright GoFrame Author(https://goframe.org). All Rights Reserved.
//
// This Source Code Form is subject to the terms of the MIT License.
// If a copy of the MIT was not distributed with this file,
// You can obtain one at https://github.com/gogf/gf.

package ghttp

import "net/http"

// SetWebSocketCheckOrigin sets the Origin check function for WebSocket upgrades.
//
// If f is nil, ghttp keeps the default compatible behavior.
func (s *Server) SetWebSocketCheckOrigin(f func(r *http.Request) bool) {
	s.config.WebSocketCheckOrigin = f
}

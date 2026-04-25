// Copyright GoFrame Author(https://goframe.org). All Rights Reserved.
//
// This Source Code Form is subject to the terms of the MIT License.
// If a copy of the MIT was not distributed with this file,
// You can obtain one at https://github.com/gogf/gf.

package ghttp

import "context"

// SetRewrite sets rewrites for static URI for server.
func (s *Server) SetRewrite(uri string, rewrite string) {
	s.config.Rewrites[uri] = rewrite
}

// SetRewriteMap sets the rewritten map for server.
func (s *Server) SetRewriteMap(rewrites map[string]string) {
	for k, v := range rewrites {
		s.config.Rewrites[k] = v
	}
}

// SetRouteOverWrite sets the RouteOverWrite for server.
func (s *Server) SetRouteOverWrite(enabled bool) {
	s.config.RouteOverWrite = enabled
}

// SetRouteComplexEnabled sets whether complex route compatibility matching is enabled.
func (s *Server) SetRouteComplexEnabled(enabled bool) {
	s.config.RouteComplexEnabled = enabled
	s.clearServeCache(context.TODO())
}

// SetRequestStructPoolEnabled sets whether strict handler request structs are pooled.
// It should only be enabled when *Req values are not retained after request completion.
func (s *Server) SetRequestStructPoolEnabled(enabled bool) {
	s.config.RequestStructPoolEnabled = enabled
}

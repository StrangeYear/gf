// Copyright GoFrame Author(https://goframe.org). All Rights Reserved.
//
// This Source Code Form is subject to the terms of the MIT License.
// If a copy of the MIT was not distributed with this file,
// You can obtain one at https://github.com/gogf/gf.

package ghttp

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/internal/intlog"
	"github.com/gogf/gf/v2/internal/json"
	"github.com/gogf/gf/v2/util/gmeta"
)

// handlerCacheItem is an item just for internal router searching cache.
type handlerCacheItem struct {
	parsedItems []*HandlerItemParsed
	serveItem   *HandlerItemParsed
	handlers    []*HandlerItem
	serveIndex  int
	hasHook     bool
	hasServe    bool
}

// serveHandlerKey creates and returns a handler key for router.
func (s *Server) serveHandlerKey(method, path, domain string) string {
	if len(domain) > 0 {
		domain = "@" + domain
	}
	if method == "" {
		return path + strings.ToLower(domain)
	}
	return strings.ToUpper(method) + ":" + path + strings.ToLower(domain)
}

// getHandlersWithCache searches the router item with cache feature for a given request.
func (s *Server) getHandlersWithCache(r *Request) (parsedItems []*HandlerItemParsed, serveItem *HandlerItemParsed, hasHook, hasServe bool) {
	var (
		ctx    = r.Context()
		method = r.Method
		path   = r.URL.Path
		host   = r.GetHost()
	)
	// In case of, eg:
	// Case 1:
	// 		GET /net/http
	// 		r.URL.Path    : /net/http
	// 		r.URL.RawPath : (empty string)
	// Case 2:
	// 		GET /net%2Fhttp
	// 		r.URL.Path    : /net/http
	// 		r.URL.RawPath : /net%2Fhttp
	if r.URL.RawPath != "" {
		path = r.URL.RawPath
	}
	// Special http method OPTIONS handling.
	// It searches the handler with the request method instead of OPTIONS method.
	if method == http.MethodOptions {
		if v := r.Header.Get("Access-Control-Request-Method"); v != "" {
			method = v
		}
	}
	// Search and cache the router handlers.
	if xUrlPath := r.Header.Get(HeaderXUrlPath); xUrlPath != "" {
		path = xUrlPath
	}
	cacheHost := host
	if _, ok := s.serveTree[host]; !ok {
		// Without host-specific routes, every host resolves against the same default tree.
		cacheHost = ""
	}
	var handlerCacheKey = s.serveHandlerKey(method, path, cacheHost)
	value, err := s.serveCache.Get(ctx, handlerCacheKey)
	if err != nil {
		intlog.Errorf(ctx, `%+v`, err)
	}
	if value != nil {
		item := value.Val().(*handlerCacheItem)
		if item.handlers != nil {
			return item.parse(path, s.config.TracingEnabled)
		}
		return item.parsedItems, item.serveItem, item.hasHook, item.hasServe
	}
	parsedItems, serveItem, hasHook, hasServe = s.searchFastHandlers(method, path, host)
	if parsedItems == nil && s.config.RouteComplexEnabled {
		parsedItems, serveItem, hasHook, hasServe = s.searchHandlers(method, path, host)
	}
	if cacheItem := newHandlerCacheItem(parsedItems, serveItem, hasHook, hasServe); cacheItem != nil {
		if err = s.serveCache.Set(
			ctx,
			handlerCacheKey,
			cacheItem,
			routeCacheDuration,
		); err != nil {
			intlog.Errorf(ctx, `%+v`, err)
		}
	}
	return
}

func (s *Server) searchFastHandlers(method, path, domain string) (
	parsedItems []*HandlerItemParsed, serveItem *HandlerItemParsed, hasHook, hasServe bool,
) {
	if len(path) == 0 {
		return nil, nil, false, false
	}
	path = normalizeRouterSearchPath(path)
	var (
		array       []string
		arrayBuffer [16]string
	)
	array = splitRouterSearchPath(path, arrayBuffer[:0])
	defaultRoot := s.serveFastTree[DefaultDomainName]
	var domainRoot *routeFastNode
	if domain != "" && domain != DefaultDomainName {
		domainRoot = s.serveFastTree[domain]
	}
	switch {
	case defaultRoot == nil && domainRoot == nil:
		return nil, nil, false, false
	case domainRoot == nil:
		return defaultRoot.search(
			method,
			path,
			array,
			s.config.RouteComplexEnabled,
			s.config.RouteComplexEnabled,
			s.compareRouterPriority,
			s.config.TracingEnabled,
		)
	case defaultRoot == nil:
		return domainRoot.search(
			method,
			path,
			array,
			s.config.RouteComplexEnabled,
			s.config.RouteComplexEnabled,
			s.compareRouterPriority,
			s.config.TracingEnabled,
		)
	}

	if s.config.RouteComplexEnabled {
		if defaultRoot.matchesFallback(method, path, array) || domainRoot.matchesFallback(method, path, array) {
			return nil, nil, false, false
		}
	}

	var (
		middlewareCount int
		seenHandlers    routeSearchSeen
	)
	parsedItemList := make([]*HandlerItemParsed, 0, 2)
	for _, root := range [2]*routeFastNode{defaultRoot, domainRoot} {
		domainItems, _, _, _ := root.search(
			method,
			path,
			array,
			s.config.RouteComplexEnabled,
			false,
			s.compareRouterPriority,
			s.config.TracingEnabled,
		)
		for _, item := range domainItems {
			if shouldSkipHandlerItem(item.Handler, s.config.TracingEnabled) {
				continue
			}
			if seenHandlers.Has(item.Handler.Id) {
				continue
			}
			if hasServe {
				switch item.Handler.Type {
				case HandlerTypeHandler, HandlerTypeObject:
					continue
				}
			}
			switch item.Handler.Type {
			case HandlerTypeHandler, HandlerTypeObject:
				hasServe = true
				serveItem = item
				parsedItemList = append(parsedItemList, item)

			case HandlerTypeMiddleware:
				parsedItemList = append(parsedItemList, nil)
				copy(parsedItemList[middlewareCount+1:], parsedItemList[middlewareCount:])
				parsedItemList[middlewareCount] = item
				middlewareCount++

			case HandlerTypeHook:
				hasHook = true
				parsedItemList = append(parsedItemList, item)

			default:
				panic(gerror.Newf(`invalid handler type %s`, item.Handler.Type))
			}
		}
	}
	if len(parsedItemList) > 0 {
		parsedItems = parsedItemList
	}
	return
}

func (n *routeFastNode) search(
	method, path string,
	parts []string,
	complexEnabled bool,
	checkFallback bool,
	compare func(newItem *HandlerItem, oldItem *HandlerItem) bool,
	tracingEnabled bool,
) (
	parsedItems []*HandlerItemParsed, serveItem *HandlerItemParsed, hasHook, hasServe bool,
) {
	var (
		candidateBuffer [16]routeFastCandidate
		candidates      = candidateBuffer[:0]
		node            = n
		remainingPath   = path
		values          map[string]string
		valuesReliable  = true
	)
	for {
		if remainingPath == "" {
			if len(node.list) > 0 {
				candidates = appendRouteFastCandidate(candidates, node.list, values, valuesReliable)
			}
			if node.catchAll != nil && len(node.catchAll.list) > 0 {
				candidates = appendRouteFastCandidate(
					candidates,
					node.catchAll.list,
					valuesWithFastRouteCatchAll(values, node.catchAllName, remainingPath),
					valuesReliable && !node.catchAllNameConflict,
				)
			}
			break
		}
		if remainingPath == "/" {
			// The root path has no more static prefix to consume, but "/" itself is a valid route.
			if len(node.list) > 0 {
				candidates = appendRouteFastCandidate(candidates, node.list, values, valuesReliable)
			}
			if node.catchAll != nil && len(node.catchAll.list) > 0 {
				candidates = appendRouteFastCandidate(
					candidates,
					node.catchAll.list,
					valuesWithFastRouteCatchAll(values, node.catchAllName, remainingPath),
					valuesReliable && !node.catchAllNameConflict,
				)
			}
			break
		}
		if node.catchAll != nil && len(node.catchAll.list) > 0 {
			candidates = appendRouteFastCandidate(
				candidates,
				node.catchAll.list,
				valuesWithFastRouteCatchAll(values, node.catchAllName, remainingPath),
				valuesReliable && !node.catchAllNameConflict,
			)
		}
		if remainingPath[0] != '/' && complexEnabled && len(node.patterns) > 0 {
			candidates = append(candidates, collectRouteFastPatternCandidates(
				node, remainingPath, values, complexEnabled,
			)...)
		}
		if child := node.findStaticChild(remainingPath[0]); child != nil && strings.HasPrefix(remainingPath, child.path) {
			if node.param != nil && remainingPath[0] != '/' {
				candidates = append(candidates, collectRouteFastParamCandidates(
					node, remainingPath, values, valuesReliable, complexEnabled,
				)...)
			}
			remainingPath = remainingPath[len(child.path):]
			node = child
			continue
		}
		if node.param != nil && remainingPath[0] != '/' {
			paramValue, newRemainingPath := splitFastRouteParamValue(remainingPath)
			if paramValue == "" {
				break
			}
			remainingPath = newRemainingPath
			if !node.paramNameConflict {
				values = setFastRouteValue(values, node.paramName, paramValue)
			} else {
				valuesReliable = false
			}
			node = node.param
			continue
		}
		break
	}
	if len(candidates) == 0 {
		return nil, nil, false, false
	}
	if routeFastCandidatesRequirePriorityMerge(candidates) {
		parsedItems, serveItem, hasHook, hasServe = resolveFastCandidatesByPriority(
			candidates,
			method,
			path,
			parts,
			compare,
			tracingEnabled,
		)
	} else {
		parsedItems, serveItem, hasHook, hasServe = resolveFastCandidates(
			candidates,
			method,
			path,
			parts,
			tracingEnabled,
		)
	}
	if len(parsedItems) == 0 {
		return nil, nil, false, false
	}
	if checkFallback && n.matchesFallback(method, path, parts) {
		return nil, nil, false, false
	}
	return parsedItems, serveItem, hasHook, hasServe
}

func collectRouteFastParamCandidates(
	node *routeFastNode,
	remainingPath string,
	values map[string]string,
	valuesReliable bool,
	complexEnabled bool,
) []routeFastCandidate {
	if node.param == nil || remainingPath == "" || remainingPath[0] == '/' {
		return nil
	}
	paramValue, remainingPath := splitFastRouteParamValue(remainingPath)
	if paramValue == "" {
		return nil
	}
	paramValues := values
	if !node.paramNameConflict {
		paramValues = setFastRouteValue(cloneRouteValues(values), node.paramName, paramValue)
	} else {
		valuesReliable = false
	}
	return collectRouteFastBranchCandidates(node.param, remainingPath, paramValues, valuesReliable, complexEnabled)
}

func collectRouteFastBranchCandidates(
	node *routeFastNode,
	remainingPath string,
	values map[string]string,
	valuesReliable bool,
	complexEnabled bool,
) []routeFastCandidate {
	var (
		candidateBuffer [8]routeFastCandidate
		candidates      = candidateBuffer[:0]
	)
	for {
		if remainingPath == "" {
			if len(node.list) > 0 {
				candidates = appendRouteFastCandidate(candidates, node.list, values, valuesReliable)
			}
			if node.catchAll != nil && len(node.catchAll.list) > 0 {
				candidates = appendRouteFastCandidate(
					candidates,
					node.catchAll.list,
					valuesWithFastRouteCatchAll(values, node.catchAllName, remainingPath),
					valuesReliable && !node.catchAllNameConflict,
				)
			}
			return cloneRouteFastCandidates(candidates)
		}
		if remainingPath == "/" {
			if len(node.list) > 0 {
				candidates = appendRouteFastCandidate(candidates, node.list, values, valuesReliable)
			}
			if node.catchAll != nil && len(node.catchAll.list) > 0 {
				candidates = appendRouteFastCandidate(
					candidates,
					node.catchAll.list,
					valuesWithFastRouteCatchAll(values, node.catchAllName, remainingPath),
					valuesReliable && !node.catchAllNameConflict,
				)
			}
			return cloneRouteFastCandidates(candidates)
		}
		if node.catchAll != nil && len(node.catchAll.list) > 0 {
			candidates = appendRouteFastCandidate(
				candidates,
				node.catchAll.list,
				valuesWithFastRouteCatchAll(values, node.catchAllName, remainingPath),
				valuesReliable && !node.catchAllNameConflict,
			)
		}
		if remainingPath[0] != '/' && complexEnabled && len(node.patterns) > 0 {
			candidates = append(candidates, collectRouteFastPatternCandidates(
				node, remainingPath, values, complexEnabled,
			)...)
		}
		if child := node.findStaticChild(remainingPath[0]); child != nil && strings.HasPrefix(remainingPath, child.path) {
			if node.param != nil && remainingPath[0] != '/' {
				candidates = append(candidates, collectRouteFastParamCandidates(
					node, remainingPath, values, valuesReliable, complexEnabled,
				)...)
			}
			remainingPath = remainingPath[len(child.path):]
			node = child
			continue
		}
		if node.param != nil && remainingPath[0] != '/' {
			paramValue, newRemainingPath := splitFastRouteParamValue(remainingPath)
			if paramValue == "" {
				return candidates
			}
			remainingPath = newRemainingPath
			if !node.paramNameConflict {
				values = setFastRouteValue(values, node.paramName, paramValue)
			} else {
				valuesReliable = false
			}
			node = node.param
			continue
		}
		return cloneRouteFastCandidates(candidates)
	}
}

func collectRouteFastPatternCandidates(
	node *routeFastNode,
	remainingPath string,
	values map[string]string,
	complexEnabled bool,
) []routeFastCandidate {
	if !complexEnabled || len(node.patterns) == 0 || remainingPath == "" || remainingPath[0] == '/' {
		return nil
	}
	segmentValue, newRemainingPath := splitFastRouteParamValue(remainingPath)
	if segmentValue == "" {
		return nil
	}
	var candidates []routeFastCandidate
	for i := range node.patterns {
		child := &node.patterns[i]
		if child.firstStatic != "" && !strings.HasPrefix(segmentValue, child.firstStatic) {
			continue
		}
		if child.lastStatic != "" && !strings.HasSuffix(segmentValue, child.lastStatic) {
			continue
		}
		if len(segmentValue) < child.literalLength+child.segment.captureCount {
			continue
		}
		matched, newValues := matchRoutePatternSegment(
			child.segment.parts,
			segmentValue,
			cloneRouteValues(values),
			len(values)+child.segment.captureCount,
		)
		if !matched {
			continue
		}
		branchCandidates := collectRouteFastBranchCandidates(
			child.node,
			newRemainingPath,
			newValues,
			false,
			complexEnabled,
		)
		for j := range branchCandidates {
			branchCandidates[j].requiresPriorityMerge = true
		}
		candidates = append(candidates, branchCandidates...)
	}
	return candidates
}

func cloneRouteFastCandidates(candidates []routeFastCandidate) []routeFastCandidate {
	if len(candidates) == 0 {
		return nil
	}
	return append([]routeFastCandidate(nil), candidates...)
}

func appendRouteFastCandidate(
	candidates []routeFastCandidate, list []*HandlerItem, values map[string]string, valuesReliable bool,
) []routeFastCandidate {
	return append(candidates, routeFastCandidate{
		list:           list,
		values:         values,
		valuesReliable: valuesReliable,
	})
}

func routeFastCandidatesRequirePriorityMerge(candidates []routeFastCandidate) bool {
	for i := range candidates {
		if candidates[i].requiresPriorityMerge {
			return true
		}
	}
	return false
}

type routeFastMatchedItem struct {
	item   *HandlerItem
	values map[string]string
}

func newHandlerItemParsed(item *HandlerItem, values map[string]string) *HandlerItemParsed {
	if len(values) == 0 && item.parsedItem != nil {
		return item.parsedItem
	}
	return &HandlerItemParsed{Handler: item, Values: values}
}

func shouldSkipHandlerItem(item *HandlerItem, tracingEnabled bool) bool {
	return !tracingEnabled && item.Info.IsInternalTracing
}

func resolveFastCandidates(
	candidates []routeFastCandidate,
	method string,
	path string,
	parts []string,
	tracingEnabled bool,
) (parsedItems []*HandlerItemParsed, serveItem *HandlerItemParsed, hasHook, hasServe bool) {
	var (
		middlewareCount int
		parsedItemList  = make([]*HandlerItemParsed, 0, 2)
		seenHandlers    routeSearchSeen
	)
	for i := len(candidates) - 1; i >= 0; i-- {
		candidate := candidates[i]
		for _, item := range candidate.list {
			if shouldSkipHandlerItem(item, tracingEnabled) {
				continue
			}
			if seenHandlers.Has(item.Id) {
				continue
			}
			if hasServe {
				switch item.Type {
				case HandlerTypeHandler, HandlerTypeObject:
					continue
				}
			}
			if item.Router.Method != defaultMethod && item.Router.Method != method {
				continue
			}
			itemValues := candidate.values
			if !candidate.valuesReliable {
				matched, matchedValues := item.Router.match(path, parts)
				if !matched {
					continue
				}
				itemValues = matchedValues
			}
			parsedItem := newHandlerItemParsed(item, itemValues)
			switch item.Type {
			case HandlerTypeHandler, HandlerTypeObject:
				hasServe = true
				serveItem = parsedItem
				parsedItemList = append(parsedItemList, parsedItem)

			case HandlerTypeMiddleware:
				parsedItemList = append(parsedItemList, nil)
				copy(parsedItemList[middlewareCount+1:], parsedItemList[middlewareCount:])
				parsedItemList[middlewareCount] = parsedItem
				middlewareCount++

			case HandlerTypeHook:
				hasHook = true
				parsedItemList = append(parsedItemList, parsedItem)

			default:
				panic(gerror.Newf(`invalid handler type %s`, item.Type))
			}
		}
	}
	if len(parsedItemList) == 0 {
		return nil, nil, false, false
	}
	return parsedItemList, serveItem, hasHook, hasServe
}

func resolveFastCandidatesByPriority(
	candidates []routeFastCandidate,
	method string,
	path string,
	parts []string,
	compare func(newItem *HandlerItem, oldItem *HandlerItem) bool,
	tracingEnabled bool,
) (parsedItems []*HandlerItemParsed, serveItem *HandlerItemParsed, hasHook, hasServe bool) {
	var seenHandlers routeSearchSeen
	matchedItems := make([]routeFastMatchedItem, 0, len(candidates))
	for i := len(candidates) - 1; i >= 0; i-- {
		candidate := candidates[i]
		for _, item := range candidate.list {
			if shouldSkipHandlerItem(item, tracingEnabled) {
				continue
			}
			if seenHandlers.Has(item.Id) {
				continue
			}
			if item.Router.Method != defaultMethod && item.Router.Method != method {
				continue
			}
			itemValues := candidate.values
			if !candidate.valuesReliable {
				matched, matchedValues := item.Router.match(path, parts)
				if !matched {
					continue
				}
				itemValues = matchedValues
			}
			matchedItems = insertMatchedFastItemByPriority(
				matchedItems,
				routeFastMatchedItem{item: item, values: itemValues},
				compare,
			)
		}
	}
	var middlewareCount int
	parsedItemList := make([]*HandlerItemParsed, 0, len(matchedItems))
	for _, matched := range matchedItems {
		item := matched.item
		if hasServe {
			switch item.Type {
			case HandlerTypeHandler, HandlerTypeObject:
				continue
			}
		}
		parsedItem := newHandlerItemParsed(item, matched.values)
		switch item.Type {
		case HandlerTypeHandler, HandlerTypeObject:
			hasServe = true
			serveItem = parsedItem
			parsedItemList = append(parsedItemList, parsedItem)

		case HandlerTypeMiddleware:
			parsedItemList = append(parsedItemList, nil)
			copy(parsedItemList[middlewareCount+1:], parsedItemList[middlewareCount:])
			parsedItemList[middlewareCount] = parsedItem
			middlewareCount++

		case HandlerTypeHook:
			hasHook = true
			parsedItemList = append(parsedItemList, parsedItem)

		default:
			panic(gerror.Newf(`invalid handler type %s`, item.Type))
		}
	}
	if len(parsedItemList) == 0 {
		return nil, nil, false, false
	}
	return parsedItemList, serveItem, hasHook, hasServe
}

func insertMatchedFastItemByPriority(
	items []routeFastMatchedItem,
	next routeFastMatchedItem,
	compare func(newItem *HandlerItem, oldItem *HandlerItem) bool,
) []routeFastMatchedItem {
	for i, old := range items {
		if compareMatchedFastItemPriority(next.item, old.item, compare) {
			items = append(items, routeFastMatchedItem{})
			copy(items[i+1:], items[i:])
			items[i] = next
			return items
		}
	}
	return append(items, next)
}

func compareMatchedFastItemPriority(
	newItem *HandlerItem,
	oldItem *HandlerItem,
	compare func(newItem *HandlerItem, oldItem *HandlerItem) bool,
) bool {
	if newItem.Type == HandlerTypeMiddleware && oldItem.Type == HandlerTypeMiddleware {
		if newItem.Router.Priority != oldItem.Router.Priority {
			return newItem.Router.Priority > oldItem.Router.Priority
		}
		return newItem.Id < oldItem.Id
	}
	newIsServe := newItem.Type == HandlerTypeHandler || newItem.Type == HandlerTypeObject
	oldIsServe := oldItem.Type == HandlerTypeHandler || oldItem.Type == HandlerTypeObject
	if newIsServe && oldIsServe {
		newHigher := compare(newItem, oldItem)
		oldHigher := compare(oldItem, newItem)
		if newHigher && oldHigher {
			return newItem.Id > oldItem.Id
		}
		return newHigher
	}
	newHigher := compare(newItem, oldItem)
	oldHigher := compare(oldItem, newItem)
	if newHigher != oldHigher {
		return newHigher
	}
	if newItem.Router.Priority != oldItem.Router.Priority {
		return newItem.Router.Priority > oldItem.Router.Priority
	}
	return newItem.Id < oldItem.Id
}

func (n *routeFastNode) findStaticChild(firstByte byte) *routeFastNode {
	if index := strings.IndexByte(n.indices, firstByte); index >= 0 {
		return n.children[index]
	}
	return nil
}

func splitFastRouteParamValue(path string) (value, remainingPath string) {
	endIndex := strings.IndexByte(path, '/')
	if endIndex >= 0 {
		return path[:endIndex], path[endIndex:]
	}
	return path, ""
}

func setFastRouteValue(values map[string]string, name, value string) map[string]string {
	if name == "" {
		return values
	}
	if len(value) > 0 && value[0] == '/' {
		value = value[1:]
	}
	if values == nil {
		values = make(map[string]string, 1)
	}
	values[name] = decodeRouteValue(value)
	return values
}

func valuesWithFastRouteCatchAll(values map[string]string, name, value string) map[string]string {
	if name == "" {
		return values
	}
	values = cloneRouteValues(values)
	return setFastRouteValue(values, name, value)
}

func cloneRouteValues(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(values)+1)
	for k, v := range values {
		cloned[k] = v
	}
	return cloned
}

func (n *routeFastNode) matchesFallback(method, path string, parts []string) bool {
	// Complex routes keep the legacy matcher as source of truth only when they overlap the current request.
	if n.matchesFallbackList(n.fallback, method, path, parts) {
		return true
	}
	if len(parts) == 0 || n.fallbackByFirstPart == nil {
		return false
	}
	return n.matchesFallbackList(n.fallbackByFirstPart[parts[0]], method, path, parts)
}

func (n *routeFastNode) matchesFallbackList(list []*HandlerItem, method, path string, parts []string) bool {
	for _, item := range list {
		if item.Router.Method != defaultMethod && item.Router.Method != method {
			continue
		}
		if matched, _ := item.Router.match(path, parts); matched {
			return true
		}
	}
	return false
}

func newHandlerCacheItem(
	parsedItems []*HandlerItemParsed, serveItem *HandlerItemParsed, hasHook, hasServe bool,
) *handlerCacheItem {
	if len(parsedItems) == 0 {
		return nil
	}
	cacheItem := &handlerCacheItem{
		parsedItems: parsedItems,
		serveItem:   serveItem,
		serveIndex:  -1,
		hasHook:     hasHook,
		hasServe:    hasServe,
	}
	for index, item := range parsedItems {
		if len(item.Values) > 0 {
			if item == serveItem {
				// Router values for the serving handler are request-path specific.
				// Caching every concrete dynamic path pollutes the route LRU for high-cardinality paths like /user/:id.
				return nil
			}
			// Non-serving handlers can still have path values, for example global middleware.
			// Cache only the handler plan and recompute those values for the current request path.
			cacheItem.parsedItems = nil
			cacheItem.serveItem = nil
			cacheItem.handlers = make([]*HandlerItem, len(parsedItems))
			for i, parsedItem := range parsedItems {
				cacheItem.handlers[i] = parsedItem.Handler
				if parsedItem == serveItem {
					cacheItem.serveIndex = i
				}
			}
			return cacheItem
		}
		if item == serveItem {
			cacheItem.serveIndex = index
		}
	}
	return cacheItem
}

func (h *handlerCacheItem) parse(path string, tracingEnabled bool) (
	parsedItems []*HandlerItemParsed, serveItem *HandlerItemParsed, hasHook, hasServe bool,
) {
	path = normalizeRouterSearchPath(path)
	var (
		array       []string
		arrayBuffer [16]string
	)
	array = splitRouterSearchPath(path, arrayBuffer[:0])
	parsedItems = make([]*HandlerItemParsed, 0, len(h.handlers))
	for index, handler := range h.handlers {
		if shouldSkipHandlerItem(handler, tracingEnabled) {
			continue
		}
		_, values := handler.Router.match(path, array)
		parsedItem := newHandlerItemParsed(handler, values)
		parsedItems = append(parsedItems, parsedItem)
		if index == h.serveIndex {
			serveItem = parsedItem
		}
	}
	return parsedItems, serveItem, h.hasHook, h.hasServe
}

// searchHandlers retrieve and returns the routers with given parameters.
// Note that the returned routers contain serving handler, middleware handlers and hook handlers.
func (s *Server) searchHandlers(method, path, domain string) (parsedItems []*HandlerItemParsed, serveItem *HandlerItemParsed, hasHook, hasServe bool) {
	if len(path) == 0 {
		return nil, nil, false, false
	}
	// In case of double '/' URI, for example:
	// /user//index, //user/index, //user//index//
	path = normalizeRouterSearchPath(path)
	// Split the URL.path to separate parts.
	var (
		array []string
		// Most routes are shallow; keep path segments on stack and allocate only for unusually deep paths.
		arrayBuffer     [16]string
		middlewareCount = 0
		// Two slots cover the common default-middleware + serve-handler path without overallocating.
		parsedItemList = make([]*HandlerItemParsed, 0, 2)
		seenHandlers   routeSearchSeen
	)
	array = splitRouterSearchPath(path, arrayBuffer[:0])

	// The default domain has the most priority when iteration.
	// Please see doSetHandler if you want to get known about the structure of serveTree.
	searchDomainHandlers := func(domainItem string) {
		p, ok := s.serveTree[domainItem]
		if !ok {
			return
		}
		// Handler lists are collected from root to leaf and consumed in reverse to preserve existing priority rules.
		var listBuffer [16][]*HandlerItem
		lists := listBuffer[:0]
		for i, part := range array {
			// Add all lists of each node to the list array.
			if len(p.list) > 0 {
				lists = append(lists, p.list)
			}
			if v := p.children[part]; v != nil {
				// Loop to the next node by certain key name.
				p = v
				if i == len(array)-1 {
					if len(p.list) > 0 {
						lists = append(lists, p.list)
						break
					}
				}
			} else if p.fuzz != nil {
				// Loop to the next node by fuzzy node item.
				p = p.fuzz
			}
			if i == len(array)-1 {
				// It here also checks the fuzzy item,
				// for rule case like: "/user/*action" matches to "/user".
				if p.fuzz != nil {
					p = p.fuzz
				}
				// The leaf must have a list item. It adds the list to the list array.
				if len(p.list) > 0 {
					lists = append(lists, p.list)
				}
			}
		}

		// OK, let's loop the result list array, adding the handler item to the result handler result array.
		// As the tail of the list array has the most priority, it iterates the list array from its tail to head.
		for i := len(lists) - 1; i >= 0; i-- {
			for _, item := range lists[i] {
				if shouldSkipHandlerItem(item, s.config.TracingEnabled) {
					continue
				}
				// Filter repeated handler items, especially the middleware and hook handlers.
				// It is necessary, do not remove this checks logic unless you really know how it is necessary.
				//
				// routeSearchSeen is used for repeat handler filtering during handler searching.
				// As there are fuzzy nodes, and the fuzzy nodes have both sub-nodes and sub-list nodes, there
				// may be repeated handler items in both sub-nodes and sub-list nodes. It here uses handler item id to
				// identify the same handler item that registered.
				//
				// The same handler item is the one that is registered in the same function doSetHandler.
				// Note that, one handler function(middleware or hook function) may be registered multiple times as
				// different handler items using function doSetHandler, and they have different handler item id.
				//
				// Note that twice, the handler function may be registered multiple times as different handler items.
				if seenHandlers.Has(item.Id) {
					continue
				}
				// Serving handler can only be added to the handler array just once.
				// The first route item in the list has the most priority than the rest.
				// This ignoring can implement route overwritten feature.
				if hasServe {
					switch item.Type {
					case HandlerTypeHandler, HandlerTypeObject:
						continue
					}
				}
				if item.Router.Method == defaultMethod || item.Router.Method == method {
					// Note the rule having no fuzzy rules: len(match) == 1
					if matched, values := item.Router.match(path, array); matched {
						parsedItem := newHandlerItemParsed(item, values)
						switch item.Type {
						// The serving handler can be added just once.
						case HandlerTypeHandler, HandlerTypeObject:
							hasServe = true
							serveItem = parsedItem
							parsedItemList = append(parsedItemList, parsedItem)

						// The middleware is inserted before the serving handler.
						// If there are multiple middleware, they're inserted into the result list by their registering order.
						// The middleware is also executed by their registered order.
						case HandlerTypeMiddleware:
							parsedItemList = append(parsedItemList, nil)
							copy(parsedItemList[middlewareCount+1:], parsedItemList[middlewareCount:])
							parsedItemList[middlewareCount] = parsedItem
							middlewareCount++

						// HOOK handler, just push it back to the list.
						case HandlerTypeHook:
							hasHook = true
							parsedItemList = append(parsedItemList, parsedItem)

						default:
							panic(gerror.Newf(`invalid handler type %s`, item.Type))
						}
					}
				}
			}
		}
	}
	searchDomainHandlers(DefaultDomainName)
	searchDomainHandlers(domain)
	if len(parsedItemList) > 0 {
		parsedItems = parsedItemList
	}
	return
}

type routeSearchSeen struct {
	count int
	ids   [16]int
	m     map[int]struct{}
}

func (s *routeSearchSeen) Has(id int) bool {
	// The small inline set avoids a map allocation for the common case with only a few candidate handlers.
	if s.m != nil {
		if _, ok := s.m[id]; ok {
			return true
		}
		s.m[id] = struct{}{}
		return false
	}
	for i := 0; i < s.count; i++ {
		if s.ids[i] == id {
			return true
		}
	}
	if s.count < len(s.ids) {
		s.ids[s.count] = id
		s.count++
		return false
	}
	s.m = make(map[int]struct{}, len(s.ids)*2)
	for i := 0; i < len(s.ids); i++ {
		s.m[s.ids[i]] = struct{}{}
	}
	s.m[id] = struct{}{}
	return false
}

func normalizeRouterSearchPath(path string) string {
	// Fast path: return the original string when there are no duplicate slashes.
	var previousIsSep bool
	for i := 0; i < len(path); i++ {
		if path[i] == '/' {
			if previousIsSep {
				buffer := make([]byte, 0, len(path))
				buffer = append(buffer, path[:i]...)
				for ; i < len(path); i++ {
					if path[i] == '/' {
						if previousIsSep {
							continue
						}
						previousIsSep = true
					} else {
						previousIsSep = false
					}
					buffer = append(buffer, path[i])
				}
				return string(buffer)
			}
			previousIsSep = true
		} else {
			previousIsSep = false
		}
	}
	return path
}

func splitRouterSearchPath(path string, array []string) []string {
	// Split without strings.Split so callers can pass a stack-backed buffer.
	if path == "/" {
		return append(array, "/")
	}
	start := 1
	for i := 1; i <= len(path); i++ {
		if i == len(path) || path[i] == '/' {
			array = append(array, path[start:i])
			start = i + 1
		}
	}
	return array
}

// MarshalJSON implements the interface MarshalJSON for json.Marshal.
func (h *HandlerItem) MarshalJSON() ([]byte, error) {
	switch h.Type {
	case HandlerTypeHook:
		return json.Marshal(
			fmt.Sprintf(
				`%s %s:%s (%s)`,
				h.Router.Uri,
				h.Router.Domain,
				h.Router.Method,
				h.HookName,
			),
		)
	case HandlerTypeMiddleware:
		return json.Marshal(
			fmt.Sprintf(
				`%s %s:%s (MIDDLEWARE)`,
				h.Router.Uri,
				h.Router.Domain,
				h.Router.Method,
			),
		)
	default:
		return json.Marshal(
			fmt.Sprintf(
				`%s %s:%s`,
				h.Router.Uri,
				h.Router.Domain,
				h.Router.Method,
			),
		)
	}
}

// MarshalJSON implements the interface MarshalJSON for json.Marshal.
func (h *HandlerItemParsed) MarshalJSON() ([]byte, error) {
	return json.Marshal(h.Handler)
}

// GetMetaTag retrieves and returns the metadata value associated with the given key from the request struct.
// The meta value is from struct tags from g.Meta/gmeta.Meta type.
func (h *HandlerItem) GetMetaTag(key string) string {
	if h == nil {
		return ""
	}
	reqStructType := h.Info.ReqStructType
	if reqStructType == nil && h.Info.Type != nil && h.Info.Type.NumIn() == 2 {
		reqStructType = h.Info.Type.In(1)
	}
	if reqStructType == nil {
		return ""
	}
	metaValue := gmeta.Get(reqStructType, key)
	if metaValue != nil {
		return metaValue.String()
	}
	return ""
}

// GetMetaTag retrieves and returns the metadata value associated with the given key from the request struct.
// The meta value is from struct tags from g.Meta/gmeta.Meta type.
// For example:
//
//	type GetMetaTagReq struct {
//	    g.Meta `path:"/test" method:"post" summary:"meta_tag" tags:"meta"`
//	    // ...
//	}
//
// r.GetServeHandler().GetMetaTag("summary") // returns "meta_tag"
// r.GetServeHandler().GetMetaTag("method")  // returns "post"
func (h *HandlerItemParsed) GetMetaTag(key string) string {
	if h == nil || h.Handler == nil {
		return ""
	}
	return h.Handler.GetMetaTag(key)
}

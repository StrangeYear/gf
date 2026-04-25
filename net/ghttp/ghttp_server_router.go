// Copyright GoFrame Author(https://goframe.org). All Rights Reserved.
//
// This Source Code Form is subject to the terms of the MIT License.
// If a copy of the MIT was not distributed with this file,
// You can obtain one at https://github.com/gogf/gf.

package ghttp

import (
	"context"
	"fmt"
	"reflect"
	"runtime"
	"strings"

	"github.com/gogf/gf/v2/container/gtype"
	"github.com/gogf/gf/v2/debug/gdebug"
	"github.com/gogf/gf/v2/errors/gcode"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/internal/consts"
	"github.com/gogf/gf/v2/internal/intlog"
	"github.com/gogf/gf/v2/text/gregex"
	"github.com/gogf/gf/v2/text/gstr"
	"github.com/gogf/gf/v2/util/gmeta"
	"github.com/gogf/gf/v2/util/gtag"
)

var (
	// handlerIdGenerator is handler item id generator.
	handlerIdGenerator = gtype.NewInt()
)

type routeTreeNode struct {
	// Static children are keyed by exact path segments.
	children map[string]*routeTreeNode
	// fuzz stores the single fuzzy branch for :name, *any, and {field} style segments.
	fuzz *routeTreeNode
	// list keeps matched handlers in priority order for this routing node.
	list []*HandlerItem
}

type routeFastNode struct {
	// path is a compressed static prefix, following the same radix idea used by high-performance routers.
	path string
	// indices keeps the first byte of every static child so lookup avoids a map on the hot path.
	indices  string
	children []*routeFastNode
	// param stores the single :name branch for this path level.
	param             *routeFastNode
	paramName         string
	paramNameConflict bool
	// catchAll stores the tail *name branch.
	catchAll             *routeFastNode
	catchAllName         string
	catchAllNameConflict bool
	// list keeps simple REST handlers in the same priority order as the compatibility tree.
	list []*HandlerItem
	// fallback keeps complex rules out of the hot tree while preserving matcher compatibility on overlap.
	fallback []*HandlerItem
}

type routeFastCandidate struct {
	list           []*HandlerItem
	values         map[string]string
	valuesReliable bool
}

// routerMapKey creates and returns a unique router key for given parameters.
// This key is used for Server.routerMap attribute, which is mainly for checks for
// repeated router registering.
func (s *Server) routerMapKey(hook HookName, method, path, domain string) string {
	return string(hook) + "%" + s.serveHandlerKey(method, path, domain)
}

// parsePattern parses the given pattern to domain, method and path variable.
func (s *Server) parsePattern(pattern string) (domain, method, path string, err error) {
	path = strings.TrimSpace(pattern)
	domain = DefaultDomainName
	method = defaultMethod
	if array, err := gregex.MatchString(`([a-zA-Z]+):(.+)`, pattern); len(array) > 1 && err == nil {
		path = strings.TrimSpace(array[2])
		if v := strings.TrimSpace(array[1]); v != "" {
			method = v
		}
	}
	if array, err := gregex.MatchString(`(.+)@([\w\.\-]+)`, path); len(array) > 1 && err == nil {
		path = strings.TrimSpace(array[1])
		if v := strings.TrimSpace(array[2]); v != "" {
			domain = v
		}
	}
	if path == "" {
		err = gerror.NewCode(gcode.CodeInvalidParameter, "invalid pattern: URI should not be empty")
	}
	if path != "/" {
		path = strings.TrimRight(path, "/")
	}
	return
}

type setHandlerInput struct {
	Prefix      string
	Pattern     string
	HandlerItem *HandlerItem
}

// setHandler creates router item with a given handler and pattern and registers the handler to the router tree.
// The router tree can be treated as a multilayer hash table, please refer to the comment in the following codes.
// This function is called during server starts up, which cares little about the performance. What really cares
// is the well-designed router storage structure for router searching when the request is under serving.
func (s *Server) setHandler(ctx context.Context, in setHandlerInput) {
	var (
		prefix  = in.Prefix
		pattern = in.Pattern
		handler = in.HandlerItem
	)
	if handler.Name == "" {
		handler.Name = runtime.FuncForPC(handler.Info.Value.Pointer()).Name()
	}
	if handler.Source == "" {
		_, file, line := gdebug.CallerWithFilter([]string{consts.StackFilterKeyForGoFrame})
		handler.Source = fmt.Sprintf(`%s:%d`, file, line)
	}
	domain, method, uri, err := s.parsePattern(pattern)
	if err != nil {
		s.Logger().Fatalf(ctx, `invalid pattern "%s", %+v`, pattern, err)
		return
	}
	// ====================================================================================
	// Change the registered route according to meta info from its request structure.
	// It supports multiple methods that are joined using char `,`.
	// ====================================================================================
	if handler.Info.Type != nil && handler.Info.Type.NumIn() == 2 {
		var objectReq = reflect.New(handler.Info.Type.In(1))
		if v := gmeta.Get(objectReq, gtag.Path); !v.IsEmpty() {
			uri = v.String()
		}
		if v := gmeta.Get(objectReq, gtag.Domain); !v.IsEmpty() {
			domain = v.String()
		}
		if v := gmeta.Get(objectReq, gtag.Method); !v.IsEmpty() {
			method = v.String()
		}
		// Multiple methods registering, which are joined using char `,`.
		if gstr.Contains(method, ",") {
			methods := gstr.SplitAndTrim(method, ",")
			for _, v := range methods {
				// Each method has it own handler.
				clonedHandler := *handler
				s.doSetHandler(ctx, &clonedHandler, prefix, uri, pattern, v, domain)
			}
			return
		}
		// Converts `all` to `ALL`.
		if gstr.Equal(method, defaultMethod) {
			method = defaultMethod
		}
	}
	s.doSetHandler(ctx, handler, prefix, uri, pattern, method, domain)
}

func (s *Server) doSetHandler(
	ctx context.Context, handler *HandlerItem,
	prefix, uri, pattern, method, domain string,
) {
	if !s.isValidMethod(method) {
		s.Logger().Fatalf(
			ctx,
			`invalid method value "%s", should be in "%s" or "%s"`,
			method, supportedHttpMethods, defaultMethod,
		)
	}
	// Prefix for URI feature.
	if prefix != "" {
		uri = prefix + "/" + strings.TrimLeft(uri, "/")
	}
	uri = strings.TrimRight(uri, "/")
	if uri == "" {
		uri = "/"
	}

	if len(uri) == 0 || uri[0] != '/' {
		s.Logger().Fatalf(ctx, `invalid pattern "%s", URI should lead with '/'`, pattern)
	}

	// Repeated router checks, this feature can be disabled by server configuration.
	var routerKey = s.routerMapKey(handler.HookName, method, uri, domain)
	if !s.config.RouteOverWrite {
		switch handler.Type {
		case HandlerTypeHandler, HandlerTypeObject:
			if items, ok := s.routesMap[routerKey]; ok {
				var duplicatedHandler *HandlerItem
				for i, item := range items {
					switch item.Type {
					case HandlerTypeHandler, HandlerTypeObject:
						duplicatedHandler = items[i]
					}
					if duplicatedHandler != nil {
						break
					}
				}
				if duplicatedHandler != nil {
					s.Logger().Fatalf(
						ctx,
						"The duplicated route registry [%s] which is meaning [{hook}%%{method}:{path}@{domain}] at \n%s -> %s , which has already been registered at \n%s -> %s"+
							"\nYou can disable duplicate route detection by modifying the server.routeOverWrite configuration, but this will cause some routes to be overwritten",
						routerKey, handler.Source, handler.Name, duplicatedHandler.Source, duplicatedHandler.Name,
					)
				}
			}
		}
	}
	// Unique id for each handler.
	handler.Id = handlerIdGenerator.Add(1)
	// Create a new router by given parameter.
	handler.Router = &Router{
		Uri:      uri,
		Domain:   domain,
		Method:   strings.ToUpper(method),
		Priority: strings.Count(uri[1:], "/"),
	}
	handler.Router.RegRule, handler.Router.RegNames = s.patternToRegular(uri)
	handler.Router.matcher = newRouteMatcher(uri)

	if _, ok := s.serveTree[domain]; !ok {
		s.serveTree[domain] = &routeTreeNode{}
	}
	// List array, very important for router registering.
	// There may be multiple lists adding into this array when searching from root to leaf.
	var (
		array []string
		lists = make([]*[]*HandlerItem, 0)
	)
	if uri == "/" {
		array = []string{"/"}
	} else {
		array = strings.Split(uri[1:], "/")
	}
	// Multilayer hash table:
	// 1. Each node of the table is separated by URI path which is split by char '/'.
	// 2. The key "*fuzz" specifies this node is a fuzzy node, which has no certain name.
	// 3. The key "*list" is the list item of the node, MOST OF THE NODES HAVE THIS ITEM,
	//    especially the fuzzy node. NOTE THAT the fuzzy node must have the "*list" item,
	//    and the leaf node also has "*list" item. If the node is not a fuzzy node either
	//    a leaf, it neither has "*list" item.
	// 2. The "*list" item is a list containing registered router items ordered by their
	//    priorities from high to low. If it's a fuzzy node, all the sub router items
	//    from this fuzzy node will also be added to its "*list" item.
	// 3. There may be repeated router items in the router lists. The lists' priorities
	//    from root to leaf are from low to high.
	var p = s.serveTree[domain]
	for i, part := range array {
		// Ignore empty URI part, like: /user//index
		if part == "" {
			continue
		}
		// Check if it's a fuzzy node.
		if gregex.IsMatchString(`^[:\*]|\{[\w\.\-]+\}|\*`, part) {
			part = "*fuzz"
			// If it's a fuzzy node, it creates a "*list" item - which is a list - in the hash map.
			// All the sub router items from this fuzzy node will also be added to its "*list" item.
			lists = append(lists, &p.list)
		}
		// Make a new bucket for the current node.
		var next *routeTreeNode
		if part == "*fuzz" {
			if p.fuzz == nil {
				p.fuzz = &routeTreeNode{}
			}
			next = p.fuzz
		} else {
			if p.children == nil {
				p.children = make(map[string]*routeTreeNode)
			}
			if p.children[part] == nil {
				p.children[part] = &routeTreeNode{}
			}
			next = p.children[part]
		}
		// Loop to next bucket.
		p = next
		// The leaf is a hash map and must have an item named "*list", which contains the router item.
		// The leaf can be furthermore extended by adding more ket-value pairs into its map.
		// Note that the `v != "*fuzz"` comparison is required as the list might be added in the former
		// fuzzy checks.
		if i == len(array)-1 && part != "*fuzz" {
			lists = append(lists, &p.list)
		}
	}
	// It iterates the list array of `lists`, compares priorities and inserts the new router item in
	// the proper position of each list. The priority of the list is ordered from high to low.
	for _, l := range lists {
		*l = insertHandlerItemByPriority(*l, handler, s.compareRouterPriority)
	}
	// Initialize the route map item.
	if _, ok := s.routesMap[routerKey]; !ok {
		s.routesMap[routerKey] = make([]*HandlerItem, 0)
	}

	// Append the route.
	s.routesMap[routerKey] = append(s.routesMap[routerKey], handler)
	s.setFastHandler(domain, uri, handler)
	s.clearServeCache(ctx)
}

func (s *Server) setFastHandler(domain, uri string, handler *HandlerItem) {
	if s.serveFastTree == nil {
		s.serveFastTree = make(map[string]*routeFastNode)
	}
	root := s.serveFastTree[domain]
	if root == nil {
		root = &routeFastNode{}
		s.serveFastTree[domain] = root
	}
	fastPath, ok := compileFastRoutePath(uri)
	if !ok {
		root.fallback = insertHandlerItemByPriority(root.fallback, handler, s.compareRouterPriority)
		return
	}
	root.insertFastPath(fastPath, handler, s.compareRouterPriority)
}

func compileFastRoutePath(uri string) (string, bool) {
	if _, ok := compileFastRouteSegments(uri); !ok {
		return "", false
	}
	if !strings.Contains(uri, "{") {
		return uri, true
	}
	// A whole-segment {name} route is equivalent to :name for the radix fast path.
	parts := strings.Split(uri[1:], "/")
	var builder strings.Builder
	builder.Grow(len(uri))
	for _, part := range parts {
		if part == "" {
			continue
		}
		builder.WriteByte('/')
		if name, ok := parseRouteParamSegment(part); ok {
			builder.WriteByte(':')
			builder.WriteString(name)
			continue
		}
		builder.WriteString(part)
	}
	if builder.Len() == 0 {
		return "/", true
	}
	return builder.String(), true
}

func compileFastRouteSegments(uri string) ([]routeSegmentMatcher, bool) {
	if uri == "/" {
		return nil, true
	}
	parts := strings.Split(uri[1:], "/")
	segments := make([]routeSegmentMatcher, 0, len(parts))
	for index, part := range parts {
		if part == "" {
			continue
		}
		segment, ok := compileRouteSegmentMatcher(part)
		if !ok {
			return nil, false
		}
		switch segment.kind {
		case routeSegmentMatcherKindStatic, routeSegmentMatcherKindNamed:
		case routeSegmentMatcherKindCatchAll:
			if index != len(parts)-1 {
				return nil, false
			}
		default:
			return nil, false
		}
		segments = append(segments, segment)
	}
	return segments, true
}

func (n *routeFastNode) insertFastPath(
	uri string, handler *HandlerItem, compare func(newItem *HandlerItem, oldItem *HandlerItem) bool,
) {
	if uri == "/" {
		n.list = insertHandlerItemByPriority(n.list, handler, compare)
		return
	}
	for len(uri) > 0 {
		wildcardIndex, wildcardKind := findFastRouteWildcard(uri)
		switch {
		case wildcardIndex < 0:
			n = n.insertStaticPrefix(uri)
			n.list = insertHandlerItemByPriority(n.list, handler, compare)
			return

		case wildcardIndex > 0:
			staticPrefix := uri[:wildcardIndex]
			if wildcardKind == '*' && staticPrefix[len(staticPrefix)-1] == '/' {
				// A tail catch-all route like /user/*action must also match /user.
				staticPrefix = staticPrefix[:len(staticPrefix)-1]
			}
			n = n.insertStaticPrefix(staticPrefix)
			uri = uri[wildcardIndex:]

		default:
			switch wildcardKind {
			case ':':
				endIndex := strings.IndexByte(uri, '/')
				name := ""
				if endIndex < 0 {
					name = uri[1:]
					uri = ""
				} else {
					name = uri[1:endIndex]
					uri = uri[endIndex:]
				}
				if n.param == nil {
					n.param = &routeFastNode{}
					n.paramName = name
				} else if n.paramName != name {
					n.paramNameConflict = true
				}
				n = n.param

			case '*':
				name := uri[1:]
				if n.catchAll == nil {
					n.catchAll = &routeFastNode{}
					n.catchAllName = name
				} else if n.catchAllName != name {
					n.catchAllNameConflict = true
				}
				n.catchAll.list = insertHandlerItemByPriority(n.catchAll.list, handler, compare)
				return
			}
		}
	}
	n.list = insertHandlerItemByPriority(n.list, handler, compare)
}

func findFastRouteWildcard(path string) (index int, kind byte) {
	for i := 0; i < len(path); i++ {
		switch path[i] {
		case ':', '*':
			if i == 0 || path[i-1] == '/' {
				return i, path[i]
			}
		}
	}
	return -1, 0
}

func (n *routeFastNode) insertStaticPrefix(path string) *routeFastNode {
	for len(path) > 0 {
		childIndex := strings.IndexByte(n.indices, path[0])
		if childIndex < 0 {
			child := &routeFastNode{path: path}
			n.indices += string(path[0])
			n.children = append(n.children, child)
			return child
		}

		child := n.children[childIndex]
		commonLength := longestCommonPrefix(path, child.path)
		if commonLength == len(child.path) {
			n = child
			path = path[commonLength:]
			continue
		}

		oldChild := *child
		oldChild.path = child.path[commonLength:]
		*child = routeFastNode{
			path:     child.path[:commonLength],
			indices:  string(oldChild.path[0]),
			children: []*routeFastNode{&oldChild},
		}
		if commonLength == len(path) {
			return child
		}
		newChild := &routeFastNode{path: path[commonLength:]}
		child.indices += string(newChild.path[0])
		child.children = append(child.children, newChild)
		return newChild
	}
	return n
}

func longestCommonPrefix(a, b string) int {
	max := len(a)
	if len(b) < max {
		max = len(b)
	}
	for i := 0; i < max; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return max
}

func (s *Server) clearServeCache(ctx context.Context) {
	if s.serveCache == nil {
		return
	}
	// Route bindings can happen after the server has already served requests, so cached route plans must be invalidated.
	if err := s.serveCache.Clear(ctx); err != nil {
		intlog.Errorf(ctx, `%+v`, err)
	}
}

func insertHandlerItemByPriority(
	items []*HandlerItem, handler *HandlerItem, compare func(newItem *HandlerItem, oldItem *HandlerItem) bool,
) []*HandlerItem {
	for index, item := range items {
		// Checks the priority whether inserting the route item before current item,
		// which means it has higher priority.
		if compare(handler, item) {
			items = append(items, nil)
			copy(items[index+1:], items[index:])
			items[index] = handler
			return items
		}
	}
	return append(items, handler)
}

func (s *Server) isValidMethod(method string) bool {
	if gstr.Equal(method, defaultMethod) {
		return true
	}
	_, ok := methodsMap[strings.ToUpper(method)]
	return ok
}

// compareRouterPriority compares the priority between `newItem` and `oldItem`. It returns true
// if `newItem`'s priority is higher than `oldItem`, else it returns false. The higher priority
// item will be inserted into the router list before the other one.
//
// Comparison rules:
// 1. The middleware has the most high priority.
// 2. URI: The deeper, the higher (simply check the count of char '/' in the URI).
// 3. Route type: {xxx} > :xxx > *xxx.
func (s *Server) compareRouterPriority(newItem *HandlerItem, oldItem *HandlerItem) bool {
	// If they're all types of middleware, the priority is according to their registered sequence.
	if newItem.Type == HandlerTypeMiddleware && oldItem.Type == HandlerTypeMiddleware {
		return false
	}
	// The middleware has the most high priority.
	if newItem.Type == HandlerTypeMiddleware && oldItem.Type != HandlerTypeMiddleware {
		return true
	}
	// URI: The deeper, the higher (simply check the count of char '/' in the URI).
	if newItem.Router.Priority > oldItem.Router.Priority {
		return true
	}
	if newItem.Router.Priority < oldItem.Router.Priority {
		return false
	}

	// Compare the length of their URI,
	// but the fuzzy and named parts of the URI are not calculated to the result.

	// Example:
	// /admin-goods-{page} > /admin-{page}
	// /{hash}.{type}      > /{hash}
	var uriNew, uriOld string
	uriNew, _ = gregex.ReplaceString(`\{[^/]+?\}`, "", newItem.Router.Uri)
	uriOld, _ = gregex.ReplaceString(`\{[^/]+?\}`, "", oldItem.Router.Uri)
	uriNew, _ = gregex.ReplaceString(`:[^/]+?`, "", uriNew)
	uriOld, _ = gregex.ReplaceString(`:[^/]+?`, "", uriOld)
	uriNew, _ = gregex.ReplaceString(`\*[^/]*`, "", uriNew) // Replace "/*" and "/*any".
	uriOld, _ = gregex.ReplaceString(`\*[^/]*`, "", uriOld) // Replace "/*" and "/*any".
	if len(uriNew) > len(uriOld) {
		return true
	}
	if len(uriNew) < len(uriOld) {
		return false
	}

	// Route type checks: {xxx} > :xxx > *xxx.
	// Example:
	// /name/act > /{name}/:act
	var (
		fuzzyCountFieldNew int
		fuzzyCountFieldOld int
		fuzzyCountNameNew  int
		fuzzyCountNameOld  int
		fuzzyCountAnyNew   int
		fuzzyCountAnyOld   int
		fuzzyCountTotalNew int
		fuzzyCountTotalOld int
	)
	for _, v := range newItem.Router.Uri {
		switch v {
		case '{':
			fuzzyCountFieldNew++
		case ':':
			fuzzyCountNameNew++
		case '*':
			fuzzyCountAnyNew++
		}
	}
	for _, v := range oldItem.Router.Uri {
		switch v {
		case '{':
			fuzzyCountFieldOld++
		case ':':
			fuzzyCountNameOld++
		case '*':
			fuzzyCountAnyOld++
		}
	}
	fuzzyCountTotalNew = fuzzyCountFieldNew + fuzzyCountNameNew + fuzzyCountAnyNew
	fuzzyCountTotalOld = fuzzyCountFieldOld + fuzzyCountNameOld + fuzzyCountAnyOld
	if fuzzyCountTotalNew < fuzzyCountTotalOld {
		return true
	}
	if fuzzyCountTotalNew > fuzzyCountTotalOld {
		return false
	}

	// If the counts of their fuzzy rules are equal.

	// Eg: /name/{act} > /name/:act
	if fuzzyCountFieldNew > fuzzyCountFieldOld {
		return true
	}
	if fuzzyCountFieldNew < fuzzyCountFieldOld {
		return false
	}
	// Eg: /name/:act > /name/*act
	if fuzzyCountNameNew > fuzzyCountNameOld {
		return true
	}
	if fuzzyCountNameNew < fuzzyCountNameOld {
		return false
	}

	// It then compares the accuracy of their http method,
	// the more accurate the more priority.
	if newItem.Router.Method != defaultMethod {
		return true
	}
	if oldItem.Router.Method != defaultMethod {
		return true
	}

	// If they have different router type,
	// the new router item has more priority than the other one.
	if newItem.Type == HandlerTypeHandler || newItem.Type == HandlerTypeObject {
		return true
	}

	// Other situations, like HOOK items,
	// the old router item has more priority than the other one.
	return false
}

// patternToRegular converts route rule to according to regular expression.
func (s *Server) patternToRegular(rule string) (regular string, names []string) {
	if len(rule) < 2 {
		return rule, nil
	}
	regular = "^"
	var array = strings.Split(rule[1:], "/")
	for _, v := range array {
		if len(v) == 0 {
			continue
		}
		switch v[0] {
		case ':':
			if len(v) > 1 {
				regular += `/([^/]+)`
				names = append(names, v[1:])
			} else {
				regular += `/[^/]+`
			}
		case '*':
			if len(v) > 1 {
				regular += `/{0,1}(.*)`
				names = append(names, v[1:])
			} else {
				regular += `/{0,1}.*`
			}
		default:
			// Special chars replacement.
			v = gstr.ReplaceByMap(v, map[string]string{
				`.`: `\.`,
				`+`: `\+`,
				`*`: `.*`,
			})
			s, _ := gregex.ReplaceStringFunc(`\{[\w\.\-]+\}`, v, func(s string) string {
				names = append(names, s[1:len(s)-1])
				return `([^/]+)`
			})
			if strings.EqualFold(s, v) {
				regular += "/" + v
			} else {
				regular += "/" + s
			}
		}
	}
	regular += `$`
	return
}

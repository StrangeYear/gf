// Copyright GoFrame Author(https://goframe.org). All Rights Reserved.
//
// This Source Code Form is subject to the terms of the MIT License.
// If a copy of the MIT was not distributed with this file,
// You can obtain one at https://github.com/gogf/gf.

package ghttp

import (
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/gogf/gf/v2/internal/intlog"
	"github.com/gogf/gf/v2/os/gres"
	"github.com/gogf/gf/v2/os/gsession"
	"github.com/gogf/gf/v2/os/gtime"
	"github.com/gogf/gf/v2/os/gview"
	"github.com/gogf/gf/v2/text/gstr"
	"github.com/gogf/gf/v2/util/guid"
)

var requestPool = sync.Pool{
	New: func() any {
		return new(Request)
	},
}

var middlewarePool = sync.Pool{
	New: func() any {
		return new(middleware)
	},
}

// Request is the context object for a request.
type Request struct {
	*http.Request
	Server     *Server           // Server.
	Cookie     *Cookie           // Cookie.
	Session    *gsession.Session // Session.
	Response   *Response         // Corresponding Response of this request.
	Router     *Router           // Matched Router for this request. Note that it's not available in HOOK handler.
	EnterTime  *gtime.Time       // Request starting time in milliseconds.
	LeaveTime  *gtime.Time       // Request to end time in milliseconds.
	Middleware *middleware       // Middleware manager.
	StaticFile *staticFile       // Static file object for static file serving.

	// =================================================================================================================
	// Private attributes for internal usage purpose.
	// =================================================================================================================

	handlers          []*HandlerItemParsed // All matched handlers containing handler, hook and middleware for this request.
	serveHandler      *HandlerItemParsed   // Real business handler serving for this request, not hook or middleware handler.
	handlerResponse   any                  // Handler response object for Request/Response handler.
	hasHookHandler    bool                 // A bool marking whether there's hook handler in the handlers for performance purpose.
	hasServeHandler   bool                 // A bool marking whether there's serving handler in the handlers for performance purpose.
	parsedQuery       bool                 // A bool marking whether the GET parameters parsed.
	parsedBody        bool                 // A bool marking whether the request body parsed.
	parsedForm        bool                 // A bool marking whether request Form parsed for HTTP method PUT, POST, PATCH.
	paramsMap         map[string]any       // Custom parameters map.
	routerMap         map[string]string    // Router parameters map, which might be nil if there are no router parameters.
	queryMap          map[string]any       // Query parameters map, which is nil if there's no query string.
	formMap           map[string]any       // Form parameters map, which is nil if there's no form of data from the client.
	bodyMap           map[string]any       // Body parameters map, which might be nil if their nobody content.
	error             error                // Current executing error of the request.
	pooledReqStruct   any                  // Strict handler request struct borrowed from handler pool.
	pooledReqPool     *sync.Pool           // Pool used for returning pooledReqStruct after response completion.
	exitAll           bool                 // A bool marking whether current request is exited.
	parsedHost        string               // The parsed host name for current host used by GetHost function.
	clientIp          string               // The parsed client ip for current host used by GetClientIp function.
	bodyContent       []byte               // Request body content.
	isFileRequest     bool                 // A bool marking whether current request is file serving.
	viewObject        *gview.View          // Custom template view engine object for this response.
	viewParams        gview.Params         // Custom template view variables for this response.
	originUrlPath     string               // Original URL path that passed from client.
	incomingSessionId string               // Session id from the incoming request.
}

// staticFile is the file struct for static file service.
type staticFile struct {
	File  *gres.File // Resource file object.
	Path  string     // File path.
	IsDir bool       // Is directory.
}

// newRequest creates and returns a new request object.
func newRequest(s *Server, r *http.Request, w http.ResponseWriter) *Request {
	request := requestPool.Get().(*Request)
	*request = Request{
		Server:            s,
		Request:           r,
		Response:          newResponse(s, w),
		EnterTime:         gtime.Now(),
		originUrlPath:     r.URL.Path,
		incomingSessionId: getSessionIdFromRequest(r, s.GetSessionIdName()),
	}
	request.Cookie = GetCookie(request)
	request.Session = s.sessionManager.New(
		r.Context(),
		request.incomingSessionId,
	)
	request.Response.Request = request
	request.Middleware = newMiddleware(request)
	// Custom session id creating function.
	err := request.Session.SetIdFunc(func(ttl time.Duration) string {
		var (
			address = request.RemoteAddr
			header  = fmt.Sprintf("%v", request.Header)
		)
		intlog.Print(r.Context(), address, header)
		return guid.S([]byte(address), []byte(header))
	})
	if err != nil {
		panic(err)
	}
	// Remove char '/' in the tail of URI.
	if request.URL.Path != "/" {
		for len(request.URL.Path) > 0 && request.URL.Path[len(request.URL.Path)-1] == '/' {
			request.URL.Path = request.URL.Path[:len(request.URL.Path)-1]
		}
	}

	// Default URI value if it's empty.
	if request.URL.Path == "" {
		request.URL.Path = "/"
	}
	return request
}

func newMiddleware(request *Request) *middleware {
	m := middlewarePool.Get().(*middleware)
	*m = middleware{
		request: request,
	}
	return m
}

func releaseRequest(request *Request) {
	if request == nil {
		return
	}
	releasePooledRequestStruct(request)
	if request.Middleware != nil {
		// Middleware objects are request-scoped and can be safely reset after the request finishes.
		*request.Middleware = middleware{}
		middlewarePool.Put(request.Middleware)
	}
	if request.Response != nil {
		releaseResponse(request.Response)
	}
	// Request is large and hot; reset all references before returning it to the pool.
	*request = Request{}
	requestPool.Put(request)
}

func (r *Request) setPooledRequestStruct(pool *sync.Pool, object any) {
	r.pooledReqPool = pool
	r.pooledReqStruct = object
}

func releasePooledRequestStruct(request *Request) {
	if request.pooledReqPool == nil || request.pooledReqStruct == nil {
		return
	}
	value := reflect.ValueOf(request.pooledReqStruct)
	if value.Kind() == reflect.Pointer && !value.IsNil() {
		elem := value.Elem()
		elem.Set(reflect.Zero(elem.Type()))
	}
	request.pooledReqPool.Put(request.pooledReqStruct)
	request.pooledReqPool = nil
	request.pooledReqStruct = nil
}

// WebSocket upgrades current request as a websocket request.
// It returns a new WebSocket object if success, or the error if failure.
// Note that the request should be a websocket request, or it will surely fail upgrading.
//
// Deprecated: will be removed in the future, please use third-party websocket library instead.
func (r *Request) WebSocket() (*WebSocket, error) {
	upgrader := wsUpGrader
	if r.Server != nil && r.Server.config.WebSocketCheckOrigin != nil {
		upgrader.CheckOrigin = r.Server.config.WebSocketCheckOrigin
	}
	if conn, err := upgrader.Upgrade(r.Response.Writer, r.Request, nil); err == nil {
		return &WebSocket{
			conn,
		}, nil
	} else {
		return nil, err
	}
}

// Exit exits executing of current HTTP handler.
func (r *Request) Exit() {
	panic(exceptionExit)
}

// ExitAll exits executing of current and following HTTP handlers.
func (r *Request) ExitAll() {
	r.exitAll = true
	panic(exceptionExitAll)
}

// ExitHook exits executing of current and following HTTP HOOK handlers.
func (r *Request) ExitHook() {
	panic(exceptionExitHook)
}

// IsExited checks and returns whether current request is exited.
func (r *Request) IsExited() bool {
	return r.exitAll
}

// GetHeader retrieves and returns the header value with given `key`.
// It returns the optional `def` parameter if the header does not exist.
func (r *Request) GetHeader(key string, def ...string) string {
	value := r.Header.Get(key)
	if value == "" && len(def) > 0 {
		value = def[0]
	}
	return value
}

// GetHost returns current request host name, which might be a domain or an IP without port.
func (r *Request) GetHost() string {
	if len(r.parsedHost) == 0 {
		r.parsedHost = stripPortFromHost(r.Host)
	}
	return r.parsedHost
}

// IsFileRequest checks and returns whether current request is serving file.
func (r *Request) IsFileRequest() bool {
	return r.isFileRequest
}

// IsAjaxRequest checks and returns whether current request is an AJAX request.
func (r *Request) IsAjaxRequest() bool {
	return strings.EqualFold(r.Header.Get("X-Requested-With"), "XMLHttpRequest")
}

// GetClientIp returns the client ip of this request without port.
// Note that this ip address might be modified by client header.
func (r *Request) GetClientIp() string {
	if r.clientIp != "" {
		return r.clientIp
	}
	realIps := r.Header.Get("X-Forwarded-For")
	if realIps != "" && len(realIps) != 0 && !strings.EqualFold("unknown", realIps) {
		ipArray := strings.Split(realIps, ",")
		r.clientIp = ipArray[0]
	}
	if r.clientIp == "" {
		r.clientIp = r.Header.Get("Proxy-Client-IP")
	}
	if r.clientIp == "" {
		r.clientIp = r.Header.Get("WL-Proxy-Client-IP")
	}
	if r.clientIp == "" {
		r.clientIp = r.Header.Get("HTTP_CLIENT_IP")
	}
	if r.clientIp == "" {
		r.clientIp = r.Header.Get("HTTP_X_FORWARDED_FOR")
	}
	if r.clientIp == "" {
		r.clientIp = r.Header.Get("X-Real-IP")
	}
	if r.clientIp == "" {
		r.clientIp = r.GetRemoteIp()
	}
	return r.clientIp
}

// GetRemoteIp returns the ip from RemoteAddr.
func (r *Request) GetRemoteIp() string {
	return stripPortFromHost(r.RemoteAddr)
}

// GetSchema returns the schema of this request.
func (r *Request) GetSchema() string {
	var (
		scheme = "http"
		proto  = r.Header.Get("X-Forwarded-Proto")
	)
	if r.TLS != nil || gstr.Equal(proto, "https") {
		scheme = "https"
	}
	return scheme
}

// GetUrl returns current URL of this request.
func (r *Request) GetUrl() string {
	var (
		scheme = "http"
		proto  = r.Header.Get("X-Forwarded-Proto")
	)

	if r.TLS != nil || gstr.Equal(proto, "https") {
		scheme = "https"
	}
	return fmt.Sprintf(`%s://%s%s`, scheme, r.Host, r.URL.String())
}

// GetSessionId retrieves and returns session id from cookie or header.
func (r *Request) GetSessionId() string {
	if r.Cookie != nil && r.Cookie.data != nil {
		id := r.Cookie.GetSessionId()
		if id == "" {
			id = r.Header.Get(r.Server.GetSessionIdName())
		}
		return id
	}
	id := r.incomingSessionId
	if id == "" {
		id = getSessionIdFromRequest(r.Request, r.Server.GetSessionIdName())
	}
	return id
}

// GetReferer returns referer of this request.
func (r *Request) GetReferer() string {
	return r.Header.Get("Referer")
}

// GetError returns the error occurs in the procedure of the request.
// It returns nil if there's no error.
func (r *Request) GetError() error {
	return r.error
}

// SetError sets custom error for current request.
func (r *Request) SetError(err error) {
	r.error = err
}

// ReloadParam is used for modifying request parameter.
// Sometimes, we want to modify request parameters through middleware, but directly modifying Request.Body
// is invalid, so it clears the parsed* marks of Request to make the parameters reparsed.
func (r *Request) ReloadParam() {
	r.parsedBody = false
	r.parsedForm = false
	r.parsedQuery = false
	r.bodyContent = nil
}

func stripPortFromHost(value string) string {
	if value == "" {
		return ""
	}
	if value[0] == '[' {
		if endIndex := strings.LastIndexByte(value, ']'); endIndex > 0 {
			return value[1:endIndex]
		}
	}
	if lastColonIndex := strings.LastIndexByte(value, ':'); lastColonIndex > 0 {
		if strings.IndexByte(value[:lastColonIndex], ':') < 0 {
			return value[:lastColonIndex]
		}
	}
	return strings.Trim(value, "[]")
}

func getSessionIdFromRequest(r *http.Request, sessionIdName string) string {
	if r == nil || sessionIdName == "" {
		return ""
	}
	if cookie, err := r.Cookie(sessionIdName); err == nil && cookie != nil {
		return cookie.Value
	}
	return r.Header.Get(sessionIdName)
}

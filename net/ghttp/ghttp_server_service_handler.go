// Copyright GoFrame Author(https://goframe.org). All Rights Reserved.
//
// This Source Code Form is subject to the terms of the MIT License.
// If a copy of the MIT was not distributed with this file,
// You can obtain one at https://github.com/gogf/gf.

package ghttp

import (
	"bytes"
	"context"
	"reflect"
	"strings"
	"sync"

	"github.com/gogf/gf/v2/errors/gcode"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/os/gstructs"
	"github.com/gogf/gf/v2/text/gstr"
)

// StrictHandler is a typed strict route handler created by NewStrictHandler.
// Its fields are intentionally unexported so registration metadata stays controlled by ghttp.
type StrictHandler struct {
	reqStructType reflect.Type
	reqStructPool *sync.Pool
	handlerType   reflect.Type
	handlerValue  reflect.Value
	handlerFunc   HandlerFunc
	err           error
}

// NewStrictHandler creates a typed strict route handler for RouterGroup.Bind.
// Route metadata such as path, method and domain is read from Req's g.Meta tags.
func NewStrictHandler[Req any, Res any](
	handler func(context.Context, *Req) (*Res, error),
) *StrictHandler {
	reqStructType := reflect.TypeOf((*Req)(nil)).Elem()
	h := &StrictHandler{
		reqStructType: reqStructType,
		reqStructPool: &sync.Pool{
			New: func() any {
				return new(Req)
			},
		},
		handlerType:  reflect.TypeOf(handler),
		handlerValue: reflect.ValueOf(handler),
	}
	if handler == nil {
		h.err = gerror.NewCode(gcode.CodeInvalidParameter, "strict handler should not be nil")
		return h
	}
	if reqStructType.Kind() != reflect.Struct {
		h.err = gerror.NewCodef(
			gcode.CodeInvalidParameter,
			"strict handler request type should be struct, but got %s",
			reqStructType.String(),
		)
		return h
	}
	h.handlerFunc = createTypedStrictHandlerFunc(handler)
	return h
}

func createTypedStrictHandlerFunc[Req any, Res any](
	handler func(context.Context, *Req) (*Res, error),
) HandlerFunc {
	return func(r *Request) {
		var req *Req
		if r.Server.config.RequestStructPoolEnabled && r.serveHandler != nil && r.serveHandler.Handler != nil {
			if pool := r.serveHandler.Handler.Info.ReqStructPool; pool != nil {
				req = pool.Get().(*Req)
				r.setPooledRequestStruct(pool, req)
			}
		}
		if req == nil {
			req = new(Req)
		}
		if err := r.parseStrictRouteRequest(req); err != nil {
			r.error = err
			return
		}
		res, err := handler(r.Context(), req)
		if err != nil {
			r.error = err
			return
		}
		r.handlerResponse = res
	}
}

// BindHandler registers a handler function to server with a given pattern.
//
// Note that the parameter `handler` can be type of:
// 1. func(*ghttp.Request)
// 2. func(context.Context, BizRequest)(BizResponse, error)
func (s *Server) BindHandler(pattern string, handler any) {
	var ctx = context.TODO()
	funcInfo, err := s.checkAndCreateFuncInfo(handler, "", "", "")
	if err != nil {
		s.Logger().Fatalf(ctx, `%+v`, err)
	}
	s.doBindHandler(ctx, doBindHandlerInput{
		Prefix:     "",
		Pattern:    pattern,
		FuncInfo:   funcInfo,
		Middleware: nil,
		Source:     "",
	})
}

// BindStrictHandler registers a typed strict handler whose route metadata is read from its request struct.
func (s *Server) BindStrictHandler(handler *StrictHandler) {
	var ctx = context.TODO()
	funcInfo, err := s.checkAndCreateFuncInfo(handler, "", "", "")
	if err != nil {
		s.Logger().Fatalf(ctx, `%+v`, err)
	}
	s.doBindHandler(ctx, doBindHandlerInput{
		Prefix:     "",
		Pattern:    "/",
		FuncInfo:   funcInfo,
		Middleware: nil,
		Source:     "",
	})
}

type doBindHandlerInput struct {
	Prefix     string
	Pattern    string
	FuncInfo   handlerFuncInfo
	Middleware []HandlerFunc
	Source     string
}

// doBindHandler registers a handler function to server with given pattern.
//
// The parameter `pattern` is like:
// /user/list, put:/user, delete:/user, post:/user@goframe.org
func (s *Server) doBindHandler(ctx context.Context, in doBindHandlerInput) {
	s.setHandler(ctx, setHandlerInput{
		Prefix:  in.Prefix,
		Pattern: in.Pattern,
		HandlerItem: &HandlerItem{
			Type:       HandlerTypeHandler,
			Info:       in.FuncInfo,
			Middleware: in.Middleware,
			Source:     in.Source,
		},
	})
}

// bindHandlerByMap registers handlers to server using map.
func (s *Server) bindHandlerByMap(ctx context.Context, prefix string, m map[string]*HandlerItem) {
	for pattern, handler := range m {
		s.setHandler(ctx, setHandlerInput{
			Prefix:      prefix,
			Pattern:     pattern,
			HandlerItem: handler,
		})
	}
}

// mergeBuildInNameToPattern merges build-in names into the pattern according to the following
// rules, and the built-in names are named like "{.xxx}".
// Rule 1: The URI in pattern contains the {.struct} keyword, it then replaces the keyword with the struct name;
// Rule 2: The URI in pattern contains the {.method} keyword, it then replaces the keyword with the method name;
// Rule 2: If Rule 1 is not met, it then adds the method name directly to the URI in the pattern;
//
// The parameter `allowAppend` specifies whether allowing appending method name to the tail of pattern.
func (s *Server) mergeBuildInNameToPattern(pattern string, structName, methodName string, allowAppend bool) string {
	structName = s.nameToUri(structName)
	methodName = s.nameToUri(methodName)
	pattern = strings.ReplaceAll(pattern, "{.struct}", structName)
	if strings.Contains(pattern, "{.method}") {
		return strings.ReplaceAll(pattern, "{.method}", methodName)
	}
	if !allowAppend {
		return pattern
	}
	// Check domain parameter.
	var (
		array = strings.Split(pattern, "@")
		uri   = strings.TrimRight(array[0], "/") + "/" + methodName
	)
	// Append the domain parameter to URI.
	if len(array) > 1 {
		return uri + "@" + array[1]
	}
	return uri
}

// nameToUri converts the given name to the URL format using the following rules:
// Rule 0: Convert all method names to lowercase, add char '-' between words.
// Rule 1: Do not convert the method name, construct the URI with the original method name.
// Rule 2: Convert all method names to lowercase, no connecting symbols between words.
// Rule 3: Use camel case naming.
func (s *Server) nameToUri(name string) string {
	switch s.config.NameToUriType {
	case UriTypeFullName:
		return name

	case UriTypeAllLower:
		return strings.ToLower(name)

	case UriTypeCamel:
		part := bytes.NewBuffer(nil)
		if gstr.IsLetterUpper(name[0]) {
			part.WriteByte(name[0] + 32)
		} else {
			part.WriteByte(name[0])
		}
		part.WriteString(name[1:])
		return part.String()

	case UriTypeDefault:
		fallthrough

	default:
		part := bytes.NewBuffer(nil)
		for i := 0; i < len(name); i++ {
			if i > 0 && gstr.IsLetterUpper(name[i]) {
				part.WriteByte('-')
			}
			if gstr.IsLetterUpper(name[i]) {
				part.WriteByte(name[i] + 32)
			} else {
				part.WriteByte(name[i])
			}
		}
		return part.String()
	}
}

func (s *Server) checkAndCreateFuncInfo(
	f any, pkgPath, structName, methodName string,
) (funcInfo handlerFuncInfo, err error) {
	if strictHandler, ok := f.(*StrictHandler); ok {
		return s.checkAndCreateStrictHandlerFuncInfo(strictHandler)
	}
	funcInfo = handlerFuncInfo{
		Type:  reflect.TypeOf(f),
		Value: reflect.ValueOf(f),
	}
	if handlerFunc, ok := f.(HandlerFunc); ok {
		funcInfo.Func = handlerFunc
		return
	}

	var reflectType = funcInfo.Type
	if reflectType.NumIn() != 2 || reflectType.NumOut() != 2 {
		if pkgPath != "" {
			err = gerror.NewCodef(
				gcode.CodeInvalidParameter,
				`invalid handler: %s.%s.%s defined as "%s", but "func(*ghttp.Request)" or "func(context.Context, *BizReq)(*BizRes, error)" is required`,
				pkgPath, structName, methodName, reflectType.String(),
			)
		} else {
			err = gerror.NewCodef(
				gcode.CodeInvalidParameter,
				`invalid handler: defined as "%s", but "func(*ghttp.Request)" or "func(context.Context, *BizReq)(*BizRes, error)" is required`,
				reflectType.String(),
			)
		}
		return
	}

	if !reflectType.In(0).Implements(reflect.TypeOf((*context.Context)(nil)).Elem()) {
		err = gerror.NewCodef(
			gcode.CodeInvalidParameter,
			`invalid handler: defined as "%s", but the first input parameter should be type of "context.Context"`,
			reflectType.String(),
		)
		return
	}

	if !reflectType.Out(1).Implements(reflect.TypeOf((*error)(nil)).Elem()) {
		err = gerror.NewCodef(
			gcode.CodeInvalidParameter,
			`invalid handler: defined as "%s", but the last output parameter should be type of "error"`,
			reflectType.String(),
		)
		return
	}

	if reflectType.In(1).Kind() != reflect.Pointer ||
		(reflectType.In(1).Kind() == reflect.Pointer && reflectType.In(1).Elem().Kind() != reflect.Struct) {
		err = gerror.NewCodef(
			gcode.CodeInvalidParameter,
			`invalid handler: defined as "%s", but the second input parameter should be type of pointer to struct like "*BizReq"`,
			reflectType.String(),
		)
		return
	}

	// Do not enable this logic, as many users are already using none struct pointer type
	// as the first output parameter.
	/*
		if reflectType.Out(0).Kind() != reflect.Pointer ||
			(reflectType.Out(0).Kind() == reflect.Pointer && reflectType.Out(0).Elem().Kind() != reflect.Struct) {
			err = gerror.NewCodef(
				gcode.CodeInvalidParameter,
				`invalid handler: defined as "%s", but the first output parameter should be type of pointer to struct like "*BizRes"`,
				reflectType.String(),
			)
			return
		}
	*/

	funcInfo.IsStrictRoute = true
	if err = s.buildStrictRequestInfo(&funcInfo, funcInfo.Type.In(1).Elem(), nil); err != nil {
		return funcInfo, err
	}
	funcInfo.Func = createRouterFunc(funcInfo)
	return
}

func (s *Server) checkAndCreateStrictHandlerFuncInfo(
	strictHandler *StrictHandler,
) (funcInfo handlerFuncInfo, err error) {
	if strictHandler == nil {
		return funcInfo, gerror.NewCode(gcode.CodeInvalidParameter, "strict handler should not be nil")
	}
	if strictHandler.err != nil {
		return funcInfo, strictHandler.err
	}
	funcInfo = handlerFuncInfo{
		Type:          strictHandler.handlerType,
		Value:         strictHandler.handlerValue,
		Func:          strictHandler.handlerFunc,
		IsStrictRoute: true,
	}
	if err = s.buildStrictRequestInfo(&funcInfo, strictHandler.reqStructType, strictHandler.reqStructPool); err != nil {
		return funcInfo, err
	}
	return funcInfo, nil
}

func (s *Server) buildStrictRequestInfo(
	funcInfo *handlerFuncInfo, reqStructType reflect.Type, reqStructPool *sync.Pool,
) (err error) {
	funcInfo.IsStrictRoute = true
	funcInfo.ReqStructType = reqStructType
	funcInfo.ReqStructHasCustomParser = reflect.PointerTo(reqStructType).Implements(requestParserType)
	if reqStructPool != nil {
		// NewStrictHandler supplies a typed pool so request objects can be allocated with new(Req).
		funcInfo.ReqStructPool = reqStructPool
	} else {
		funcInfo.ReqStructPool = newRequestStructPool(reqStructType)
	}

	inputObject := reflect.New(reqStructType)
	inputObjectPtr := inputObject.Interface()

	// It retrieves and returns the request struct fields.
	fields, err := gstructs.Fields(gstructs.FieldsInput{
		Pointer:         inputObjectPtr,
		RecursiveOption: gstructs.RecursiveOptionEmbedded,
	})
	if err != nil {
		return err
	}
	funcInfo.ReqStructFields = fields
	funcInfo.ReqStructDefaults, funcInfo.ReqStructIn, funcInfo.ReqStructNeedsValidation = buildRequestStructTagMeta(fields)
	if funcInfo.ReqStructParseMeta, err = getOrBuildParseStructMetaByType(reqStructType); err != nil {
		return err
	}
	funcInfo.ReqStructHasParseTag = funcInfo.ReqStructParseMeta != nil && funcInfo.ReqStructParseMeta.HasParseTag
	funcInfo.ReqStructFastBindMeta = buildStrictRequestFastBindMeta(funcInfo)
	return nil
}

func newRequestStructPool(reqStructType reflect.Type) *sync.Pool {
	return &sync.Pool{
		New: func() any {
			return reflect.New(reqStructType).Interface()
		},
	}
}

func createRouterFunc(funcInfo handlerFuncInfo) func(r *Request) {
	var (
		inputNum          = funcInfo.Type.NumIn()
		reqInputIsPointer bool
		reqStructType     reflect.Type
		reqStructPool     *sync.Pool
	)
	if inputNum == 2 {
		reqInputType := funcInfo.Type.In(1)
		if reqInputType.Kind() == reflect.Pointer {
			reqInputIsPointer = true
			reqStructType = funcInfo.ReqStructType
			if reqStructType == nil {
				reqStructType = reqInputType.Elem()
			}
			reqStructPool = funcInfo.ReqStructPool
		} else {
			reqStructType = reqInputType
		}
	}
	return func(r *Request) {
		var (
			ok          bool
			err         error
			inputValues [2]reflect.Value
		)
		inputValues[0] = reflect.ValueOf(r.Context())
		if inputNum == 2 {
			var inputObject reflect.Value
			var inputObjectPtr any
			if reqInputIsPointer {
				if reqStructPool != nil && r.Server.config.RequestStructPoolEnabled {
					inputObjectPtr = reqStructPool.Get()
					inputObject = reflect.ValueOf(inputObjectPtr)
					r.setPooledRequestStruct(reqStructPool, inputObjectPtr)
				} else {
					inputObject = reflect.New(reqStructType)
					inputObjectPtr = inputObject.Interface()
				}
				r.error = r.parseStrictRouteRequest(inputObjectPtr)
			} else {
				inputObject = reflect.New(reqStructType).Elem()
				r.error = r.parseStrictRouteRequest(inputObject.Addr().Interface())
			}
			if r.error != nil {
				return
			}
			inputValues[1] = inputObject
		}
		// Call handler with dynamic created parameter values.
		results := funcInfo.Value.Call(inputValues[:inputNum])
		switch len(results) {
		case 1:
			if !results[0].IsNil() {
				if err, ok = results[0].Interface().(error); ok {
					r.error = err
				}
			}

		case 2:
			r.handlerResponse = results[0].Interface()
			if !results[1].IsNil() {
				if err, ok = results[1].Interface().(error); ok {
					r.error = err
				}
			}
		}
	}
}

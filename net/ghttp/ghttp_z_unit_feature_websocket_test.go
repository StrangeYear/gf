// Copyright GoFrame Author(https://goframe.org). All Rights Reserved.
//
// This Source Code Form is subject to the terms of the MIT License.
// If a copy of the MIT was not distributed with this file,
// You can obtain one at https://github.com/gogf/gf.

package ghttp_test

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/ghttp"
	"github.com/gogf/gf/v2/test/gtest"
	"github.com/gogf/gf/v2/util/guid"
)

func Test_WebSocket(t *testing.T) {
	s := g.Server(guid.S())
	s.BindHandler("/ws", func(r *ghttp.Request) {
		ws, err := r.WebSocket()
		if err != nil {
			r.Exit()
		}
		for {
			msgType, msg, err := ws.ReadMessage()
			if err != nil {
				return
			}
			if err = ws.WriteMessage(msgType, msg); err != nil {
				return
			}
		}
	})
	s.SetDumpRouterMap(false)
	s.Start()
	defer s.Shutdown()

	time.Sleep(100 * time.Millisecond)
	gtest.C(t, func(t *gtest.T) {
		conn, _, err := websocket.DefaultDialer.Dial(fmt.Sprintf(
			"ws://127.0.0.1:%d/ws", s.GetListenedPort(),
		), nil)
		t.AssertNil(err)
		defer conn.Close()

		msg := []byte("hello")
		err = conn.WriteMessage(websocket.TextMessage, msg)
		t.AssertNil(err)

		mt, data, err := conn.ReadMessage()
		t.AssertNil(err)
		t.Assert(mt, websocket.TextMessage)
		t.Assert(data, msg)
	})
}

func Test_WebSocket_CheckOrigin(t *testing.T) {
	s := g.Server(guid.S())
	s.SetWebSocketCheckOrigin(func(r *http.Request) bool {
		return r.Header.Get("Origin") == "https://allowed.example"
	})
	s.BindHandler("/ws-origin", func(r *ghttp.Request) {
		ws, err := r.WebSocket()
		if err != nil {
			r.Exit()
		}
		_ = ws.Close()
	})
	s.SetDumpRouterMap(false)
	s.Start()
	defer s.Shutdown()

	time.Sleep(100 * time.Millisecond)
	gtest.C(t, func(t *gtest.T) {
		url := fmt.Sprintf("ws://127.0.0.1:%d/ws-origin", s.GetListenedPort())
		_, _, err := websocket.DefaultDialer.Dial(url, http.Header{
			"Origin": []string{"https://blocked.example"},
		})
		t.AssertNE(err, nil)

		conn, _, err := websocket.DefaultDialer.Dial(url, http.Header{
			"Origin": []string{"https://allowed.example"},
		})
		t.AssertNil(err)
		_ = conn.Close()
	})
}

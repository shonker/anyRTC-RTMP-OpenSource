// Copyright 2013 The Gorilla WebSocket Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	_ "bytes"
	"xlog"

	//"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"time"
	"ucenter"

	"github.com/satori/go.uuid"

	"github.com/gorilla/websocket"
	_ "github.com/mattn/go-adodb"
)

const (
	// Time allowed to write a message to the peer.
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer.
	pongWait = 60 * time.Second

	// Send pings to peer with this period. Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10

	// Maximum message size allowed from peer.
	maxMessageSize = 512
)

var (
	newline = []byte{'\n'}
	space   = []byte{' '}
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	// 新增如下代码,解决跨域问题,即403错误
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// Client is a middleman between the websocket connection and the hub.
type Client struct {
	hub *Hub

	// The websocket connection.
	conn *websocket.Conn

	// Buffered channel of outbound messages.
	send      chan []byte
	SessionId string //未登录之前，记录socket连接时生成的uuid,如果客户端免登录进来的，客户端上报后置换为客户端保存的uuid
}

// readPump pumps messages from the websocket connection to the hub.
//
// The application runs readPump in a per-connection goroutine. The application
// ensures that there is at most one reader on a connection by executing all
// reads from this goroutine.
func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()
	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error { c.conn.SetReadDeadline(time.Now().Add(pongWait)); return nil })
	for {
		mt, message, err := c.conn.ReadMessage()
		log.Println("messageType:", mt, string(message))
		if mt != 1 {
			log.Println("messageType:", mt, " error!")
			//break
		}
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("error: %v", err)
			}
			break
		}
		m := make(map[string]interface{})
		r := make(map[string]interface{})
		//var m interface{}
		json.Unmarshal(message, &m)
		log.Println(m)
		switch m["t"] {
		case "sign up": // 注册
			account := m["account"].(string)
			password := m["password"].(string)
			email := m["email"].(string)
			cellphone := m["cellphone"].(string)
			r["t"] = "sign up"
			r["status"] = 1
			r["info"] = (account + password + email + cellphone)

			rmsg, err := json.Marshal(r)
			if err == nil {
				c.send <- rmsg
			}

		case "sign in": // 登录
			account := m["account"].(string)
			password := m["password"].(string)
			tt := sign.SignIn(account, password)
			tt.SessionId = c.SessionId
			log.Println(tt)
			sign.Sessions[c.SessionId] = tt
			r["t"] = "sign in"
			r["userinfo"] = tt
			rmsg, err := json.Marshal(r)
			if err == nil {
				c.send <- rmsg
				xlog.Println("sign in status", rmsg)
			}

		case "sign out": // 登出
		case "checkin": //免密登录
			xlog.Println("checkin 免密登录")

		}

		//message = bytes.TrimSpace(bytes.Replace(message, newline, space, -1))
		//c.hub.broadcast <- message
	}
}

// writePump pumps messages from the hub to the websocket connection.
//
// A goroutine running writePump is started for each connection. The
// application ensures that there is at most one writer to a connection by
// executing all writes from this goroutine.
func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()
	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// The hub closed the channel.
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(message)

			// Add queued chat messages to the current websocket message.
			n := len(c.send)
			//log.Println("len(c.send):", n)
			for i := 0; i < n; i++ {

				w.Write(newline)
				w.Write(<-c.send)
			}

			if err := w.Close(); err != nil {
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// serveWs handles websocket requests from the peer.
func serveWs(hub *Hub, w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		return
	}
	ok, _ := uuid.NewV4()

	client := &Client{hub: hub, conn: conn, send: make(chan []byte, 256), SessionId: ok.String()}
	log.Println(client)
	client.hub.register <- client

	// Allow collection of memory referenced by the caller by doing all work in
	// new goroutines.
	go client.writePump()
	go client.readPump()
}

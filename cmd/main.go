package main

import (
	"log"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

type Message struct {
	Event   string
	Payload string
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

type Client struct {
	hub      *hub
	wsConn   *websocket.Conn
	received chan *Message
}

type hub struct {
	// Registered clients.
	clients map[*Client]bool

	// Register requests from the clients.
	register chan *Client

	// Unregister requests from clients.
	unregister chan *Client

	broadcast chan *Message
}

var (
	newline = []byte{'\n'}
	space   = []byte{' '}
)

func (h *hub) run() {
	for {
		select {
		case cli := <-h.register:
			h.clients[cli] = true
		case cli := <-h.unregister:
			delete(h.clients, cli)
		case msg := <-h.broadcast:
			for client := range h.clients {
				select {
				case client.received <- msg:
				default:
					close(client.received)
					delete(h.clients, client)
				}
			}
		}
	}
}

func main() {
	r := gin.Default()
	err := r.SetTrustedProxies(nil)
	if err != nil {
		panic(err)
	}
	h := &hub{
		broadcast:  make(chan *Message, 1024),
		clients:    make(map[*Client]bool),
		register:   make(chan *Client),
		unregister: make(chan *Client),
	}

	go h.run()
	go generateMessage(h.broadcast)

	r.GET("/ws", func(c *gin.Context) {
		wsConn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			return
		}

		cli := &Client{hub: h, wsConn: wsConn, received: make(chan *Message, 1024)}

		h.register <- cli

		go func() {
			ticker := time.NewTicker(1 * time.Second)
			defer func() {
				ticker.Stop()
				cli.wsConn.Close()
			}()

			for {
				select {
				case msg := <-cli.received:
					cli.wsConn.SetWriteDeadline(time.Now().Add(10 * time.Second))
					w, err := cli.wsConn.NextWriter(websocket.TextMessage)
					if err != nil {
						return
					}
					w.Write([]byte(msg.Payload))

					if err := w.Close(); err != nil {
						return
					}
				case <-ticker.C:
					cli.wsConn.SetWriteDeadline(time.Now().Add(10 * time.Second))
					if err := cli.wsConn.WriteMessage(websocket.PingMessage, nil); err != nil {
						return
					}
				}
			}

		}()

		go func() {
			defer func() {
				cli.hub.unregister <- cli
				cli.wsConn.Close()
			}()
			cli.wsConn.SetReadLimit(512)
			cli.wsConn.SetReadDeadline(time.Now().Add(60 * time.Second))
			cli.wsConn.SetPongHandler(func(string) error { cli.wsConn.SetReadDeadline(time.Now().Add(60 * time.Second)); return nil })
			for {
				_, _, err := cli.wsConn.ReadMessage()
				if err != nil {
					if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
						log.Printf("error: %v", err)
					}
					break
				}
			}
		}()
	})

	r.Run() // listen and serve on 0.0.0.0:8080
}

func generateMessage(out chan<- *Message) {
	for {
		out <- &Message{
			Event:   "starting",
			Payload: time.Now().String(),
		}

		time.Sleep(time.Second)
	}
}

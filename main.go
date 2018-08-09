package main

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/gorilla/websocket"
	uuid "github.com/satori/go.uuid"
)

// ClientManager keeps track of waiting to register/connected/removed/waiting to be destroyed clients
type ClientManager struct {
	clients    map[*Client]bool
	broadcast  chan []byte
	register   chan *Client
	unregister chan *Client
}

// Client has a unique id, a socket connection and a message to send
type Client struct {
	id     string
	socket *websocket.Conn
	send   chan []byte
}

// Message has a sender, receiver and the content of the message
type Message struct {
	Sender    string `json:"sender,omitempty"`
	Recipient string `json:"recipient,omitempty"`
	Content   string `json:"content,omitempty"`
}

// global ClientManager
var manager = ClientManager{
	broadcast:  make(chan []byte),
	register:   make(chan *Client),
	unregister: make(chan *Client),
	clients:    make(map[*Client]bool),
}

func (manager *ClientManager) start() {
	for {
		select {
		// whenever the manager.register channel has data
		// add a client to the clients map of the manager
		// then, broadcast a message to all clients that a new client connected
		case conn := <-manager.register:
			manager.clients[conn] = true
			jsonMessage, _ := json.Marshal(&Message{Content: "/A new socket has connected."})
			manager.send(jsonMessage, conn)
		// whenever the manager.unregister channel has data
		// close the channel data in the disconnected client
		// remove client from manager.clients
		// broadcast message informing other clients of the disconnection
		case conn := <-manager.unregister:
			if _, ok := manager.clients[conn]; ok {
				close(conn.send)
				delete(manager.clients, conn)
				jsonMessage, _ := json.Marshal(&Message{Content: "/A socket has disconnected."})
				manager.send(jsonMessage, conn)
			}
		// whenever the manager.broadcase channel has data
		// iterate over the manager.clients map and send message to them
		// if the message can't be sent, remove the client
		case message := <-manager.broadcast:
			for conn := range manager.clients {
				select {
				case conn.send <- message:
				default:
					close(conn.send)
					delete(manager.clients, conn)
				}
			}
		}
	}
}

// read websocket data sent from clients
// and add it to manager.broadcast for its use
func (c *Client) read() {
	defer func() {
		manager.unregister <- c
		c.socket.Close()
	}()

	for {
		_, message, err := c.socket.ReadMessage()
		// if there was a problem reading message, unregister the client and close connection
		if err != nil {
			manager.unregister <- c
			c.socket.Close()
			break
		}
		jsonMessage, _ := json.Marshal(&Message{Sender: c.id, Content: string(message)})
		manager.broadcast <- jsonMessage
	}
}

// if c.send has data, try to write and send the message
func (c *Client) write() {
	defer func() {
		c.socket.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			// if something is wrong, send a disconnect message to client
			if !ok {
				c.socket.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			c.socket.WriteMessage(websocket.TextMessage, message)
		}
	}
}

// helper function to iterate over manager.clients and send messages to clients
func (manager *ClientManager) send(message []byte, ignore *Client) {
	for conn := range manager.clients {
		if conn != ignore {
			conn.send <- message
		}
	}
}

func wsPage(res http.ResponseWriter, req *http.Request) {
	// upgrade http handler to websocket as well as check origin (common check)
	conn, error := (&websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}).Upgrade(res, req, nil)
	if error != nil {
		http.NotFound(res, req)
		return
	}
	// when a connection is made, client is created
	uuid, _ := uuid.NewV4()
	client := &Client{id: uuid.String(), socket: conn, send: make(chan []byte)}

	// client is registered to server
	manager.register <- client
	// read and write goroutines triggered
	go client.read()
	go client.write()
}
func main() {
	fmt.Println("Starting application...")
	go manager.start()
	http.HandleFunc("/ws", wsPage)
	http.ListenAndServe(":12345", nil)
}

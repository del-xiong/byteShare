package service

import (
	"log"
	"time"
	"encoding/base64"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	maxMessageSize = 64 * 1024 * 1024 // 64MB max per message
)

type Client struct {
	User UserInfo
	Room string
	Conn *websocket.Conn
	Send chan []byte
	Hub  *Hub

	// File transfer tracking
	transfers sync.Map
}

type transferState struct {
	FileName string
	FileSize int64
	MimeType string
	Buffer   []byte
	Received int64
}

func NewClient(conn *websocket.Conn, hub *Hub) *Client {
	return &Client{
		Conn: conn,
		Send: make(chan []byte, 256),
		Hub:  hub,
	}
}

func (c *Client) ReadPump() {
	defer func() {
		c.Hub.LeaveRoom(c)
		c.Conn.Close()
	}()

	c.Conn.SetReadLimit(maxMessageSize)
	c.Conn.SetReadDeadline(time.Now().Add(pongWait))
	c.Conn.SetPongHandler(func(string) error {
		c.Conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, message, err := c.Conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				log.Printf("[Client] Read error: %v", err)
			}
			break
		}

		c.handleMessage(message)
	}
}

func (c *Client) WritePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.Conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.Send:
			c.Conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				c.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.Conn.WriteMessage(websocket.TextMessage, message); err != nil {
				log.Printf("[Client] Write error: %v", err)
				return
			}
		case <-ticker.C:
			c.Conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (c *Client) handleMessage(data []byte) {
	result := gjson.ParseBytes(data)
	msgType := result.Get("type").String()

	switch msgType {
	case "join":
		c.handleJoin(result)
	case "user-rename":
		c.handleRename(result)
	case "file-offer":
		c.forwardMessage(data)
	case "file-accept":
		c.forwardMessage(data)
	case "file-reject":
		c.forwardMessage(data)
	case "file-chunk":
		c.handleFileChunk(data)
	case "file-cancel":
		c.forwardMessage(data)
	case "chunk-ack":
		c.forwardMessage(data)
	case "text":
		c.forwardText(result)
	default:
		log.Printf("[Client] Unknown message type: %s", msgType)
	}
}

func (c *Client) handleJoin(result gjson.Result) {
	roomName := result.Get("room").String()
	userName := result.Get("user.name").String()
	userID := result.Get("user.id").String()
	userColor := result.Get("user.color").String()

	c.User = UserInfo{
		ID:    userID,
		Name:  userName,
		Color: userColor,
	}

	c.Hub.JoinRoom(c, roomName)
}

func (c *Client) handleRename(result gjson.Result) {
	newName := result.Get("name").String()
	if newName == "" {
		return
	}

	c.User.Name = newName

	// Broadcast the rename to everyone else in the room so they
	// can update their card without re-sending a full user list.
	msg, _ := sjson.Set(`{"type":"user-rename"}`, "user_id", c.User.ID)
	msg, _ = sjson.Set(msg, "name", newName)
	c.broadcastToRoom([]byte(msg))
}

func (c *Client) broadcastToRoom(data []byte) {
	if c.Room == "" {
		return
	}

	c.Hub.mu.RLock()
	room, ok := c.Hub.rooms[c.Room]
	c.Hub.mu.RUnlock()
	if !ok {
		return
	}

	room.mu.RLock()
	defer room.mu.RUnlock()

	for client := range room.clients {
		if client != c {
			select {
			case client.Send <- data:
			default:
				log.Printf("[Client] Dropping message for %s: buffer full", client.User.ID)
			}
		}
	}
}

func (c *Client) forwardMessage(data []byte) {
	result := gjson.ParseBytes(data)
	targetID := result.Get("to").String()
	if targetID == "" {
		return
	}

	// Add from field
	fromData, err := sjson.SetBytes(data, "from", c.User.ID)
	if err != nil {
		return
	}

	c.sendToUser(targetID, fromData)
}

func (c *Client) forwardText(result gjson.Result) {
	targetID := result.Get("to").String()
	content := result.Get("content").String()
	if targetID == "" {
		return
	}

	msg, _ := sjson.Set(`{"type":"text"}`, "from", c.User.ID)
	msg, _ = sjson.Set(msg, "content", content)

	c.sendToUser(targetID, []byte(msg))
}

func (c *Client) handleFileChunk(data []byte) {
	// File chunks are forwarded with added "from" field
	result := gjson.ParseBytes(data)
	targetID := result.Get("to").String()
	if targetID == "" {
		return
	}

	fromData, err := sjson.SetBytes(data, "from", c.User.ID)
	if err != nil {
		return
	}

	c.sendToUser(targetID, fromData)
}

func (c *Client) sendToUser(targetID string, data []byte) {
	if c.Room == "" {
		return
	}

	c.Hub.mu.RLock()
	room, ok := c.Hub.rooms[c.Room]
	c.Hub.mu.RUnlock()
	if !ok {
		return
	}

	room.mu.RLock()
	defer room.mu.RUnlock()

	for client := range room.clients {
		if client.User.ID == targetID {
			select {
			case client.Send <- data:
			default:
				log.Printf("[Client] Dropping message for %s: buffer full", targetID)
			}
			return
		}
	}
}

// Utility for base64-encoded chunk transfers
func DecodeChunkData(encoded string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(encoded)
}

func EncodeChunkData(raw []byte) string {
	return base64.StdEncoding.EncodeToString(raw)
}

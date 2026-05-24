package service

import (
	"log"
	"sync"

	"github.com/tidwall/sjson"
)

type UserInfo struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

type Room struct {
	name    string
	clients map[*Client]bool
	mu      sync.RWMutex
}

func newRoom(name string) *Room {
	return &Room{
		name:    name,
		clients: make(map[*Client]bool),
	}
}

type Hub struct {
	mu    sync.RWMutex
	rooms map[string]*Room
}

func NewHub() *Hub {
	return &Hub{
		rooms: make(map[string]*Room),
	}
}

func (h *Hub) JoinRoom(client *Client, roomName string) {
	h.mu.Lock()

	// If the client is switching from a different room, remove them first so
	// they don't linger in the old room's client list.
	var oldRecipients []*Client
	if client.Room != "" && client.Room != roomName {
		if oldRoom, ok := h.rooms[client.Room]; ok {
			oldRoom.mu.Lock()
			delete(oldRoom.clients, client)
			if len(oldRoom.clients) == 0 {
				delete(h.rooms, oldRoom.name)
			} else {
				for c := range oldRoom.clients {
					oldRecipients = append(oldRecipients, c)
				}
			}
			oldRoom.mu.Unlock()
		}
	}

	// Join (or create) the target room.
	room, ok := h.rooms[roomName]
	if !ok {
		room = newRoom(roomName)
		h.rooms[roomName] = room
	}
	room.mu.Lock()
	client.Room = roomName
	room.clients[client] = true

	// Capture the snapshot and recipient list while holding both locks.
	users := make([]map[string]interface{}, 0, len(room.clients))
	recipients := make([]*Client, 0, len(room.clients))
	for c := range room.clients {
		recipients = append(recipients, c)
		isMe := c == client
		users = append(users, map[string]interface{}{
			"id":    c.User.ID,
			"name":  c.User.Name,
			"color": c.User.Color,
			"is_me": isMe,
		})
	}
	room.mu.Unlock()
	h.mu.Unlock()

	// Notify old room that this client left.
	if oldRecipients != nil {
		leaveMsg, _ := sjson.Set(`{"type":"user-left"}`, "user_id", client.User.ID)
		for _, c := range oldRecipients {
			c.Send <- []byte(leaveMsg)
		}
	}

	// Notify new room (best-effort, informational).
	joinMsg, _ := sjson.Set(`{"type":"user-joined"}`, "user", map[string]string{
		"id":    client.User.ID,
		"name":  client.User.Name,
		"color": client.User.Color,
	})
	for _, c := range recipients {
		if c != client {
			select {
			case c.Send <- []byte(joinMsg):
			default:
			}
		}
	}

	// Send user list to the joining client (blocking send).
	usersJSON, _ := sjson.Set(`{"type":"room-users"}`, "users", users)
	client.Send <- []byte(usersJSON)

	log.Printf("[Hub] Client %s joined room %s", client.User.Name, roomName)
}

func (h *Hub) LeaveRoom(client *Client) {
	if client.Room == "" {
		return
	}

	// Hold hub + room lock atomically to check-and-remove
	h.mu.Lock()
	room, ok := h.rooms[client.Room]
	if !ok {
		h.mu.Unlock()
		client.Room = ""
		return
	}

	room.mu.Lock()
	delete(room.clients, client)
	remaining := len(room.clients)

	if remaining == 0 {
		delete(h.rooms, room.name)
	}
	room.mu.Unlock()
	h.mu.Unlock()

	log.Printf("[Hub] Client %s left room %s", client.User.Name, client.Room)
	client.Room = ""

	if remaining > 0 {
		leaveMsg, _ := sjson.Set(`{"type":"user-left"}`, "user_id", client.User.ID)
		room.mu.RLock()
		for c := range room.clients {
			// Blocking send — user-left is critical; must not be dropped.
			// The 256-entry buffer means this only blocks if the recipient's
			// WritePump is genuinely stuck, which is rare and transient.
			c.Send <- []byte(leaveMsg)
		}
		room.mu.RUnlock()
	}
}

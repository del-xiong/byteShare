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
	// Lock hub + room atomically to prevent races with LeaveRoom/cleanup
	h.mu.Lock()
	room, ok := h.rooms[roomName]
	if !ok {
		room = newRoom(roomName)
		h.rooms[roomName] = room
	}
	room.mu.Lock()
	client.Room = roomName
	room.clients[client] = true
	room.mu.Unlock()
	h.mu.Unlock()

	// Notify others (no hub lock needed, Send channel is non-blocking)
	joinMsg, _ := sjson.Set(`{"type":"user-joined"}`, "user", map[string]string{
		"id":    client.User.ID,
		"name":  client.User.Name,
		"color": client.User.Color,
	})

	room.mu.RLock()
	for c := range room.clients {
		if c != client {
			select {
			case c.Send <- []byte(joinMsg):
			default:
			}
		}
	}

	users := make([]map[string]interface{}, 0, len(room.clients))
	for c := range room.clients {
		isMe := c == client
		users = append(users, map[string]interface{}{
			"id":    c.User.ID,
			"name":  c.User.Name,
			"color": c.User.Color,
			"is_me": isMe,
		})
	}
	room.mu.RUnlock()

	usersJSON, _ := sjson.Set(`{"type":"room-users"}`, "users", users)
	select {
	case client.Send <- []byte(usersJSON):
	default:
	}

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
			select {
			case c.Send <- []byte(leaveMsg):
			default:
			}
		}
		room.mu.RUnlock()
	}
}

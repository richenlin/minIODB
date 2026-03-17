
package sse

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

type Event struct {
	Type string      `json:"type"`
	Data interface{} `json:"data"`
	Time int64       `json:"time"`
}

type subscription struct {
	topic string
	ch    chan Event
	done  chan struct{}
}

type Hub struct {
	clients    map[string]map[chan Event]struct{}
	mu         sync.RWMutex
	register   chan subscription
	unregister chan subscription
	done       chan struct{}
}

func NewHub() *Hub {
	h := &Hub{
		clients:    make(map[string]map[chan Event]struct{}),
		register:   make(chan subscription, 64),
		unregister: make(chan subscription, 256),
		done:       make(chan struct{}),
	}
	go h.run()
	return h
}

func (h *Hub) Subscribe(topic string) chan Event {
	ch := make(chan Event, 64)
	done := make(chan struct{})
	h.register <- subscription{topic: topic, ch: ch, done: done}
	<-done
	return ch
}

func (h *Hub) Unsubscribe(topic string, ch chan Event) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	select {
	case h.unregister <- subscription{topic: topic, ch: ch}:
	case <-ctx.Done():
		go func() {
			defer func() { recover() }()
			for range ch {
			}
		}()
	}
}

func (h *Hub) Publish(topic string, data interface{}) {
	h.mu.RLock()
	clients, ok := h.clients[topic]
	if !ok {
		h.mu.RUnlock()
		return
	}
	clientList := make([]chan Event, 0, len(clients))
	for ch := range clients {
		clientList = append(clientList, ch)
	}
	h.mu.RUnlock()

	event := Event{
		Type: topic,
		Data: data,
		Time: time.Now().UnixMilli(),
	}

	for _, ch := range clientList {
		h.safeSend(ch, event)
	}
}

func (h *Hub) safeSend(ch chan Event, event Event) {
	defer func() {
		if r := recover(); r != nil {
		}
	}()

	select {
	case ch <- event:
	default:
	}
}

func (h *Hub) run() {
	for {
		select {
		case sub := <-h.register:
			h.mu.Lock()
			if h.clients[sub.topic] == nil {
				h.clients[sub.topic] = make(map[chan Event]struct{})
			}
			h.clients[sub.topic][sub.ch] = struct{}{}
			h.mu.Unlock()
			close(sub.done)

		case sub := <-h.unregister:
			h.mu.Lock()
			if clients, ok := h.clients[sub.topic]; ok {
				delete(clients, sub.ch)
				if len(clients) == 0 {
					delete(h.clients, sub.topic)
				}
			}
			h.mu.Unlock()
			h.safeClose(sub.ch)

		case <-h.done:
			h.mu.Lock()
			for _, clients := range h.clients {
				for ch := range clients {
					h.safeClose(ch)
				}
			}
			h.clients = make(map[string]map[chan Event]struct{})
			h.mu.Unlock()
			return
		}
	}
}

func (h *Hub) safeClose(ch chan Event) {
	defer func() {
		if r := recover(); r != nil {
		}
	}()
	close(ch)
}

func (h *Hub) Close() {
	close(h.done)
}

func ServeSSE(c *gin.Context, hub *Hub, topics []string) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	channels := make([]chan Event, len(topics))
	for i, topic := range topics {
		channels[i] = hub.Subscribe(topic)
		defer hub.Unsubscribe(topic, channels[i])
	}

	ctx := c.Request.Context()

	cases := make([]reflect.SelectCase, len(channels)+1)
	cases[0] = reflect.SelectCase{
		Dir:  reflect.SelectRecv,
		Chan: reflect.ValueOf(ctx.Done()),
	}
	for i, ch := range channels {
		cases[i+1] = reflect.SelectCase{
			Dir:  reflect.SelectRecv,
			Chan: reflect.ValueOf(ch),
		}
	}

	flusher, ok := c.Writer.(httpFlusher)
	if !ok {
		return
	}

	for {
		chosen, value, ok := reflect.Select(cases)

		if chosen == 0 {
			return
		}

		if !ok {
			cases[chosen].Chan = reflect.ValueOf(nil)
			allClosed := true
			for i := 1; i < len(cases); i++ {
				if cases[i].Chan.IsValid() {
					allClosed = false
					break
				}
			}
			if allClosed {
				return
			}
			continue
		}

		event, ok := value.Interface().(Event)
		if !ok {
			continue
		}

		jsonData, err := json.Marshal(event)
		if err != nil {
			continue
		}

		fmt.Fprintf(c.Writer, "data: %s\n\n", jsonData)
		flusher.Flush()
	}
}

type httpFlusher interface {
	Flush()
}


package sse

import (
	"context"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestHub_Publish_Receive(t *testing.T) {
	h := NewHub()
	defer h.Close()

	ch := h.Subscribe("test")

	h.Publish("test", map[string]int{"value": 1})

	select {
	case event := <-ch:
		assert.Equal(t, "test", event.Type)
		data, ok := event.Data.(map[string]int)
		assert.True(t, ok)
		assert.Equal(t, 1, data["value"])
	case <-time.After(100 * time.Millisecond):
		t.Error("Should receive event")
	}

	h.Unsubscribe("test", ch)
}

func TestHub_Publish_RaceSafety(t *testing.T) {
	h := NewHub()
	defer h.Close()

	ch := h.Subscribe("test")

	var receivedCount int32
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		timeout := time.After(2 * time.Second)
		for {
			select {
			case <-ch:
				atomic.AddInt32(&receivedCount, 1)
			case <-timeout:
				return
			}
		}
	}()

	for i := 0; i < 100; i++ {
		h.Publish("test", map[string]int{"value": i})
	}

	time.Sleep(100 * time.Millisecond)
	wg.Wait()

	count := atomic.LoadInt32(&receivedCount)
	assert.Greater(t, count, int32(0), "Should receive at least some events")
}

func TestHub_Publish_ToClosedChannel_NoPanic(t *testing.T) {
	h := NewHub()
	defer h.Close()

	ch := h.Subscribe("test")

	h.Unsubscribe("test", ch)
	time.Sleep(50 * time.Millisecond)

	notPanicked := true
	func() {
		defer func() {
			if r := recover(); r != nil {
				notPanicked = false
			}
		}()
		h.Publish("test", "data")
	}()

	assert.True(t, notPanicked, "Publish should not panic when sending to closed channel")
}

func TestHub_Unsubscribe_WithTimeout(t *testing.T) {
	h := NewHub()
	defer h.Close()

	ch := h.Subscribe("test")

	start := time.Now()
	h.Unsubscribe("test", ch)
	elapsed := time.Since(start)

	assert.Less(t, elapsed, time.Second, "Unsubscribe should complete within timeout")
}

func TestHub_MultipleTopics(t *testing.T) {
	h := NewHub()
	defer h.Close()

	ch1 := h.Subscribe("topic1")
	ch2 := h.Subscribe("topic2")

	h.Publish("topic1", "data1")
	h.Publish("topic2", "data2")

	select {
	case e := <-ch1:
		assert.Equal(t, "topic1", e.Type)
	case <-time.After(100 * time.Millisecond):
		t.Error("Should receive event on topic1")
	}

	select {
	case e := <-ch2:
		assert.Equal(t, "topic2", e.Type)
	case <-time.After(100 * time.Millisecond):
		t.Error("Should receive event on topic2")
	}

	h.Unsubscribe("topic1", ch1)
	h.Unsubscribe("topic2", ch2)
}

func TestHub_Close_ClosesAllChannels(t *testing.T) {
	h := NewHub()

	ch1 := h.Subscribe("topic1")
	ch2 := h.Subscribe("topic2")

	h.Close()

	_, ok1 := <-ch1
	_, ok2 := <-ch2

	assert.False(t, ok1, "Channel 1 should be closed")
	assert.False(t, ok2, "Channel 2 should be closed")
}

func TestHub_ChannelBufferOverflow(t *testing.T) {
	h := NewHub()
	defer h.Close()

	ch := h.Subscribe("test")

	for i := 0; i < 200; i++ {
		h.Publish("test", map[string]int{"value": i})
	}

	time.Sleep(50 * time.Millisecond)

	count := 0
	timeout := time.After(100 * time.Millisecond)
outer:
	for {
		select {
		case <-ch:
			count++
		case <-timeout:
			break outer
		}
	}

	assert.Greater(t, count, 0, "Should receive some events")
}

func TestHub_PublishNonExistentTopic(t *testing.T) {
	h := NewHub()
	defer h.Close()

	notPanicked := true
	func() {
		defer func() {
			if r := recover(); r != nil {
				notPanicked = false
			}
		}()
		h.Publish("nonexistent", "data")
	}()

	assert.True(t, notPanicked, "Publish to non-existent topic should not panic")
}

func TestServeSSE_ReflectSelect(t *testing.T) {
	h := NewHub()
	defer h.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	ch1 := h.Subscribe("topic1")
	ch2 := h.Subscribe("topic2")

	go func() {
		time.Sleep(50 * time.Millisecond)
		h.Publish("topic1", "event1")
		time.Sleep(50 * time.Millisecond)
		h.Publish("topic2", "event2")
	}()

	received := make([]Event, 0, 2)

	cases := make([]reflect.SelectCase, 3)
	cases[0] = reflect.SelectCase{
		Dir:  reflect.SelectRecv,
		Chan: reflect.ValueOf(ctx.Done()),
	}
	cases[1] = reflect.SelectCase{
		Dir:  reflect.SelectRecv,
		Chan: reflect.ValueOf(ch1),
	}
	cases[2] = reflect.SelectCase{
		Dir:  reflect.SelectRecv,
		Chan: reflect.ValueOf(ch2),
	}

	for len(received) < 2 {
		chosen, value, ok := reflect.Select(cases)
		if chosen == 0 {
			break
		}
		if !ok {
			break
		}
		if event, ok := value.Interface().(Event); ok {
			received = append(received, event)
		}
	}

	h.Unsubscribe("topic1", ch1)
	h.Unsubscribe("topic2", ch2)

	assert.GreaterOrEqual(t, len(received), 1, "Should receive at least one event via reflect.Select")
}

func TestHub_ConcurrentPublish(t *testing.T) {
	h := NewHub()
	defer h.Close()

	ch := h.Subscribe("test")

	var receivedCount int32
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-ch:
				atomic.AddInt32(&receivedCount, 1)
			case <-time.After(500 * time.Millisecond):
				return
			}
		}
	}()

	numPublishers := 10
	numEvents := 100
	var publishWg sync.WaitGroup
	for p := 0; p < numPublishers; p++ {
		publishWg.Add(1)
		go func() {
			defer publishWg.Done()
			for i := 0; i < numEvents; i++ {
				h.Publish("test", map[string]int{"value": i})
			}
		}()
	}
	publishWg.Wait()

	time.Sleep(100 * time.Millisecond)
	wg.Wait()

	count := atomic.LoadInt32(&receivedCount)
	assert.Greater(t, count, int32(0), "Should receive events")
}

func TestHub_DoubleUnsubscribe_NoPanic(t *testing.T) {
	h := NewHub()
	defer h.Close()

	ch := h.Subscribe("test")

	h.Unsubscribe("test", ch)
	time.Sleep(10 * time.Millisecond)

	notPanicked := true
	func() {
		defer func() {
			if r := recover(); r != nil {
				notPanicked = false
			}
		}()
		h.Unsubscribe("test", ch)
	}()

	assert.True(t, notPanicked, "Double unsubscribe should not panic")
}

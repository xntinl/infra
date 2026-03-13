# 16. Pub/Sub with Channels

<!--
difficulty: advanced
concepts: [pub-sub, topic-routing, subscriber-management, broadcast]
tools: [go]
estimated_time: 35m
bloom_level: analyze
prerequisites: [channels, goroutines, sync-mutex, sync-rwmutex]
-->

## Prerequisites

- Go 1.22+ installed
- Solid understanding of channels, goroutines, and mutexes
- Familiarity with the publish-subscribe messaging pattern

## Learning Objectives

After completing this exercise, you will be able to:

- **Explain** the pub/sub pattern and how it decouples producers from consumers
- **Implement** a topic-based pub/sub system using channels
- **Analyze** subscriber management and backpressure challenges

## Why Pub/Sub with Channels

Publish-subscribe decouples message producers from consumers. Publishers send messages to topics without knowing who is listening. Subscribers register interest in topics and receive matching messages. This pattern enables loose coupling, fan-out delivery, and dynamic subscription management.

Go channels are natural building blocks for pub/sub: each subscriber gets a channel, and publishing broadcasts to all subscriber channels for a topic.

## The Problem

Build a topic-based pub/sub broker that supports subscribing, unsubscribing, and publishing messages.

## Requirements

1. `Subscribe(topic) <-chan Message` registers and returns a channel
2. `Unsubscribe(topic, ch)` removes a subscriber
3. `Publish(topic, data)` sends to all subscribers of that topic
4. Non-blocking publish: if a subscriber's channel is full, skip it (or log a warning)
5. Thread-safe for concurrent publishers and subscribers

## Hints

<details>
<summary>Hint 1: Data Structures</summary>

```go
type Broker struct {
    mu   sync.RWMutex
    subs map[string][]chan Message
}
```
</details>

<details>
<summary>Hint 2: Complete Implementation</summary>

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

type Message struct {
	Topic string
	Data  any
}

type Broker struct {
	mu   sync.RWMutex
	subs map[string][]chan Message
}

func NewBroker() *Broker {
	return &Broker{subs: make(map[string][]chan Message)}
}

func (b *Broker) Subscribe(topic string, bufSize int) <-chan Message {
	b.mu.Lock()
	defer b.mu.Unlock()
	ch := make(chan Message, bufSize)
	b.subs[topic] = append(b.subs[topic], ch)
	return ch
}

func (b *Broker) Unsubscribe(topic string, ch <-chan Message) {
	b.mu.Lock()
	defer b.mu.Unlock()
	subs := b.subs[topic]
	for i, sub := range subs {
		if sub == ch {
			b.subs[topic] = append(subs[:i], subs[i+1:]...)
			close(sub)
			return
		}
	}
}

func (b *Broker) Publish(topic string, data any) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	msg := Message{Topic: topic, Data: data}
	for _, ch := range b.subs[topic] {
		select {
		case ch <- msg:
		default:
			// Subscriber channel full, skip
		}
	}
}

func (b *Broker) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	for topic, subs := range b.subs {
		for _, ch := range subs {
			close(ch)
		}
		delete(b.subs, topic)
	}
}

func main() {
	broker := NewBroker()

	var wg sync.WaitGroup

	// Subscriber 1: orders topic
	ordersCh := broker.Subscribe("orders", 10)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for msg := range ordersCh {
			fmt.Printf("[orders-sub] %v\n", msg.Data)
		}
	}()

	// Subscriber 2: orders topic (second subscriber)
	ordersCh2 := broker.Subscribe("orders", 10)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for msg := range ordersCh2 {
			fmt.Printf("[orders-log] %v\n", msg.Data)
		}
	}()

	// Subscriber 3: payments topic
	paymentsCh := broker.Subscribe("payments", 10)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for msg := range paymentsCh {
			fmt.Printf("[payments-sub] %v\n", msg.Data)
		}
	}()

	// Publish some messages
	broker.Publish("orders", "Order #1 created")
	broker.Publish("orders", "Order #2 created")
	broker.Publish("payments", "Payment $50 received")
	broker.Publish("orders", "Order #3 created")
	broker.Publish("payments", "Payment $100 received")

	time.Sleep(50 * time.Millisecond)
	broker.Close()
	wg.Wait()
}
```
</details>

## Verification

```bash
go run -race main.go
```

Expected: Both order subscribers receive all 3 order messages. The payment subscriber receives both payment messages. No race conditions.

## What's Next

Continue to [17 - Error Group Parallel Error Handling](../17-error-group-parallel-error-handling/17-error-group-parallel-error-handling.md) to learn advanced error collection patterns.

## Summary

- Pub/sub decouples publishers from subscribers using topic-based routing
- Each subscriber gets a buffered channel; publishing broadcasts to all
- Non-blocking publish (with `select default`) prevents slow subscribers from blocking publishers
- Use `sync.RWMutex` for concurrent access: RLock for publish, Lock for subscribe/unsubscribe
- Close all subscriber channels when shutting down the broker

## Reference

- [Publish-subscribe pattern (Wikipedia)](https://en.wikipedia.org/wiki/Publish%E2%80%93subscribe_pattern)
- [Go Concurrency Patterns (talk)](https://go.dev/talks/2012/concurrency.slide)

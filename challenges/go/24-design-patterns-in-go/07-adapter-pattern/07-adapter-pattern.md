# 7. Adapter Pattern

<!--
difficulty: advanced
concepts: [adapter-pattern, interface-adaptation, third-party-integration, wrapper, anti-corruption-layer]
tools: [go]
estimated_time: 30m
bloom_level: analyze
prerequisites: [interfaces, dependency-injection, repository-pattern]
-->

## Prerequisites

- Go 1.22+ installed
- Completed [05 - Repository Pattern](../05-repository-pattern/05-repository-pattern.md)
- Understanding of interfaces and composition

## Learning Objectives

After completing this exercise, you will be able to:

- **Build** adapters that make incompatible interfaces work together
- **Wrap** third-party libraries behind your own interfaces
- **Design** anti-corruption layers that isolate external API changes

## Why Adapter Pattern

Third-party libraries and external APIs have their own interfaces. Your domain defines its own. The adapter pattern bridges the gap. When you switch from SendGrid to Mailgun for email, or from Stripe to PayPal for payments, only the adapter changes. Your domain code never knows the difference.

In Go, an adapter is a struct that holds the third-party client and implements your domain interface by translating calls. It is the most practical form of the "ports and adapters" (hexagonal) architecture.

## The Problem

Build a notification system that sends messages through multiple channels: email, SMS, and Slack. Each channel uses a different third-party API with a different interface. Write adapters that make all three conform to a single `Notifier` interface.

### Requirements

1. Define a domain `Notifier` interface: `Send(ctx, recipient, message) error`
2. Simulate three third-party APIs with incompatible signatures
3. Write an adapter for each API that implements `Notifier`
4. Build a `MultiNotifier` adapter that fans out to multiple notifiers
5. Demonstrate swapping and composing notifiers without changing domain code

### Hints

<details>
<summary>Hint 1: Third-party API simulations</summary>

```go
// Simulated third-party email SDK
type SendGridClient struct{}
func (c *SendGridClient) SendEmail(from, to, subject, htmlBody string) (*SendGridResponse, error) { ... }

// Simulated SMS API
type TwilioClient struct{}
func (c *TwilioClient) CreateMessage(accountSID, from, to, body string) error { ... }

// Simulated Slack API
type SlackWebhook struct{}
func (s *SlackWebhook) Post(webhookURL string, payload map[string]string) (int, error) { ... }
```

Each has a completely different signature, return type, and parameter set.
</details>

<details>
<summary>Hint 2: Adapter pattern</summary>

```go
type EmailAdapter struct {
    client    *SendGridClient
    fromEmail string
}

func (a *EmailAdapter) Send(ctx context.Context, recipient, message string) error {
    _, err := a.client.SendEmail(a.fromEmail, recipient, "Notification", message)
    return err
}
```

The adapter holds the client plus any configuration, and translates the call.
</details>

<details>
<summary>Hint 3: MultiNotifier composition</summary>

```go
type MultiNotifier struct {
    notifiers []Notifier
}

func (m *MultiNotifier) Send(ctx context.Context, recipient, message string) error {
    var errs []error
    for _, n := range m.notifiers {
        if err := n.Send(ctx, recipient, message); err != nil {
            errs = append(errs, err)
        }
    }
    return errors.Join(errs...)
}
```
</details>

## Verification

Your program should demonstrate:

```
--- Email Adapter ---
[SendGrid] Sending to alice@example.com: "Your order shipped"

--- SMS Adapter ---
[Twilio] SMS to +1-555-0101: "Your order shipped"

--- Slack Adapter ---
[Slack] Posting to #alerts: "Your order shipped"

--- Multi Notifier ---
[SendGrid] Sending to alice@example.com: "System alert: high CPU"
[Twilio] SMS to +1-555-0101: "System alert: high CPU"
[Slack] Posting to #alerts: "System alert: high CPU"
```

```bash
go run main.go
```

## What's Next

Continue to [08 - Middleware/Decorator Pattern](../08-middleware-decorator-pattern/08-middleware-decorator-pattern.md) to learn how to add cross-cutting behavior to any interface.

## Summary

- The adapter pattern wraps an incompatible interface to match the one your code expects
- In Go, an adapter is a struct that holds the foreign dependency and implements your interface
- Adapters form an anti-corruption layer -- external API changes are contained in the adapter
- Composition adapters (MultiNotifier, FallbackNotifier) combine multiple adapters
- When switching vendors, replace the adapter, not the domain code
- Test adapters by mocking the third-party client or using integration tests

## Reference

- [Adapter pattern](https://refactoring.guru/design-patterns/adapter)
- [Hexagonal Architecture](https://alistair.cockburn.us/hexagonal-architecture/)
- [Go interfaces in practice](https://jordanorelli.com/post/32665860244/how-to-use-interfaces-in-go)

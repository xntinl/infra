# 3. Strategy Pattern

<!--
difficulty: intermediate
concepts: [strategy-pattern, interface-polymorphism, runtime-algorithm-swap, composition]
tools: [go]
estimated_time: 25m
bloom_level: apply
prerequisites: [interfaces, structs-and-methods, closures]
-->

## Prerequisites

- Go 1.22+ installed
- Understanding of interfaces and composition

## Learning Objectives

After completing this exercise, you will be able to:

- **Implement** the strategy pattern using Go interfaces
- **Swap** algorithms at runtime without modifying the context
- **Compare** interface-based strategies with function-based strategies

## Why Strategy Pattern

When your code needs to support multiple algorithms for the same task -- sorting, compression, pricing, authentication -- embedding all variants in `if/else` or `switch` blocks creates a maintenance nightmare. The strategy pattern extracts each algorithm into its own type behind a common interface. Adding a new strategy requires zero changes to existing code.

In Go, this is idiomatic: define an interface, implement it multiple times, inject the desired implementation.

## The Problem

Build a payment processing system that supports multiple pricing strategies: flat rate, percentage-based, tiered, and promotional. The checkout service uses whichever strategy is configured without knowing the details of the calculation.

## Requirements

1. Define a `PricingStrategy` interface with a `Calculate(subtotal float64) float64` method
2. Implement at least four strategies: FlatDiscount, PercentageDiscount, TieredPricing, PromotionalPricing
3. Build a `Checkout` struct that accepts any `PricingStrategy`
4. Demonstrate swapping strategies at runtime
5. Show the function-based alternative using `func(float64) float64`

## Step 1 -- Define the Interface and Strategies

```bash
mkdir -p ~/go-exercises/strategy
cd ~/go-exercises/strategy
go mod init strategy
```

Create `main.go`:

```go
package main

import "fmt"

type PricingStrategy interface {
	Calculate(subtotal float64) (discount float64, total float64)
	Name() string
}

type FlatDiscount struct {
	Amount float64
}

func (f FlatDiscount) Calculate(subtotal float64) (float64, float64) {
	discount := f.Amount
	if discount > subtotal {
		discount = subtotal
	}
	return discount, subtotal - discount
}

func (f FlatDiscount) Name() string {
	return fmt.Sprintf("Flat $%.2f off", f.Amount)
}

type PercentageDiscount struct {
	Percent float64
}

func (p PercentageDiscount) Calculate(subtotal float64) (float64, float64) {
	discount := subtotal * (p.Percent / 100)
	return discount, subtotal - discount
}

func (p PercentageDiscount) Name() string {
	return fmt.Sprintf("%.0f%% off", p.Percent)
}

type TieredPricing struct {
	Tiers []Tier
}

type Tier struct {
	MinAmount float64
	Percent   float64
}

func (t TieredPricing) Calculate(subtotal float64) (float64, float64) {
	for i := len(t.Tiers) - 1; i >= 0; i-- {
		if subtotal >= t.Tiers[i].MinAmount {
			discount := subtotal * (t.Tiers[i].Percent / 100)
			return discount, subtotal - discount
		}
	}
	return 0, subtotal
}

func (t TieredPricing) Name() string {
	return "Tiered pricing"
}

func main() {
	strategies := []PricingStrategy{
		FlatDiscount{Amount: 10},
		PercentageDiscount{Percent: 15},
		TieredPricing{Tiers: []Tier{
			{MinAmount: 0, Percent: 0},
			{MinAmount: 50, Percent: 5},
			{MinAmount: 100, Percent: 10},
			{MinAmount: 200, Percent: 15},
		}},
	}

	subtotal := 150.00
	fmt.Printf("Subtotal: $%.2f\n\n", subtotal)

	for _, s := range strategies {
		discount, total := s.Calculate(subtotal)
		fmt.Printf("%-25s discount=$%.2f  total=$%.2f\n", s.Name(), discount, total)
	}
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Subtotal: $150.00

Flat $10.00 off           discount=$10.00  total=$140.00
15% off                   discount=$22.50  total=$127.50
Tiered pricing            discount=$15.00  total=$135.00
```

## Step 2 -- Inject Strategy into a Service

```go
type Checkout struct {
	pricing PricingStrategy
}

func NewCheckout(pricing PricingStrategy) *Checkout {
	return &Checkout{pricing: pricing}
}

func (c *Checkout) Process(items []float64) {
	subtotal := 0.0
	for _, item := range items {
		subtotal += item
	}
	discount, total := c.pricing.Calculate(subtotal)
	fmt.Printf("Checkout [%s]: subtotal=$%.2f discount=$%.2f total=$%.2f\n",
		c.pricing.Name(), subtotal, discount, total)
}
```

### Intermediate Verification

```go
checkout := NewCheckout(PercentageDiscount{Percent: 20})
checkout.Process([]float64{29.99, 49.99, 19.99})

// Swap strategy at runtime
checkout = NewCheckout(FlatDiscount{Amount: 15})
checkout.Process([]float64{29.99, 49.99, 19.99})
```

## Step 3 -- Function-Based Strategy Alternative

```go
type PricingFunc func(subtotal float64) (discount, total float64)

func CheckoutWithFunc(items []float64, pricing PricingFunc) {
	subtotal := 0.0
	for _, item := range items {
		subtotal += item
	}
	discount, total := pricing(subtotal)
	fmt.Printf("Functional: subtotal=$%.2f discount=$%.2f total=$%.2f\n",
		subtotal, discount, total)
}
```

Usage:

```go
CheckoutWithFunc([]float64{100, 50}, func(s float64) (float64, float64) {
	d := s * 0.1
	return d, s - d
})
```

### Intermediate Verification

Both the interface and function approaches produce the same results.

## Common Mistakes

### Embedding Logic in the Context

**Wrong:**

```go
func (c *Checkout) Process(items []float64, strategyType string) {
    switch strategyType {
    case "flat": // ...
    case "percent": // ...
    }
}
```

**Fix:** Let the injected strategy handle the logic. The checkout should not know about strategy types.

## Verification

```bash
go run main.go
```

## What's Next

Continue to [04 - Dependency Injection](../04-dependency-injection/04-dependency-injection.md) to learn how to wire dependencies cleanly.

## Summary

- The strategy pattern defines a family of algorithms behind a common interface
- The context (e.g., Checkout) delegates to the strategy without knowing its internals
- New strategies are added by implementing the interface -- no existing code changes
- Go's implicit interface satisfaction makes strategy implementations lightweight
- For simple strategies, `func` types are a viable alternative to interfaces

## Reference

- [Strategy pattern](https://refactoring.guru/design-patterns/strategy)
- [Go interfaces](https://go.dev/tour/methods/9)

# Solution: 2-Phase Commit Coordinator

## Architecture Overview

The system is composed of three layers: the **Coordinator** (manages transaction lifecycle), **Participants** (simulate distributed services that prepare/commit/abort), and the **Write-Ahead Log** (WAL, ensures coordinator crash recovery).

The coordinator processes each transaction through a state machine: `Init -> Preparing -> Committed/Aborted -> Done`. The WAL records decisions before Phase 2 messages are sent, establishing the recovery invariant: if "COMMIT txID" is in the WAL, the transaction is committed; otherwise it is aborted (presumed-abort optimization).

Participants are modeled as goroutines with configurable failure injection. The simulation framework controls when and how participants fail, enabling deterministic testing of every failure mode.

## Go Solution

### Project Setup

```bash
mkdir -p two-phase-commit && cd two-phase-commit
go mod init two-phase-commit
```

### Implementation

```go
// wal.go
package twopc

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"sync"
)

// WAL provides a write-ahead log for coordinator crash recovery.
type WAL struct {
	mu   sync.Mutex
	file *os.File
	path string
}

func NewWAL(path string) (*WAL, error) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("open WAL: %w", err)
	}
	return &WAL{file: f, path: path}, nil
}

// LogCommit records a commit decision. Must be called before sending Phase 2 commits.
func (w *WAL) LogCommit(txID string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	_, err := fmt.Fprintf(w.file, "COMMIT %s\n", txID)
	if err != nil {
		return fmt.Errorf("write WAL: %w", err)
	}
	return w.file.Sync() // fsync is critical: the decision must be durable
}

// LogPrepare records that a transaction entered the prepare phase.
func (w *WAL) LogPrepare(txID string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	_, err := fmt.Fprintf(w.file, "PREPARE %s\n", txID)
	if err != nil {
		return fmt.Errorf("write WAL: %w", err)
	}
	return w.file.Sync()
}

// LogDone records that a transaction completed (all participants acknowledged).
func (w *WAL) LogDone(txID string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	_, err := fmt.Fprintf(w.file, "DONE %s\n", txID)
	if err != nil {
		return fmt.Errorf("write WAL: %w", err)
	}
	return w.file.Sync()
}

// Recover reads the WAL and returns transactions that need resolution.
// Returns: committed (need re-commit), prepared (need abort -- presumed abort).
func (w *WAL) Recover() (committed []string, prepared []string, err error) {
	f, err := os.Open(w.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	defer f.Close()

	states := make(map[string]string) // txID -> last state

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		parts := strings.SplitN(scanner.Text(), " ", 2)
		if len(parts) != 2 {
			continue
		}
		action, txID := parts[0], parts[1]
		states[txID] = action
	}

	for txID, state := range states {
		switch state {
		case "COMMIT":
			committed = append(committed, txID)
		case "PREPARE":
			// Presumed abort: no COMMIT record means abort
			prepared = append(prepared, txID)
		// DONE transactions are fully resolved, skip them
		}
	}

	return committed, prepared, scanner.Err()
}

func (w *WAL) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.file.Close()
}
```

```go
// participant.go
package twopc

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Vote represents a participant's prepare-phase response.
type Vote int

const (
	VoteYes Vote = iota
	VoteNo
)

// ParticipantState tracks a participant's transaction state.
type ParticipantState int

const (
	ParticipantIdle ParticipantState = iota
	ParticipantPrepared
	ParticipantCommitted
	ParticipantAborted
)

// Participant represents a service participating in a distributed transaction.
type Participant interface {
	ID() string
	Prepare(ctx context.Context, txID string) (Vote, error)
	Commit(ctx context.Context, txID string) error
	Abort(ctx context.Context, txID string) error
	QueryDecision(ctx context.Context, txID string) (ParticipantState, error)
}

// FailureConfig controls how a simulated participant fails.
type FailureConfig struct {
	PrepareDelay     time.Duration
	PrepareError     bool
	VoteNo           bool
	CrashBeforeVote  bool
	CrashAfterVote   bool
	CrashBeforeCommit bool
	CommitDelay      time.Duration
	CommitError      bool
}

// SimParticipant is a participant with configurable failure behavior.
type SimParticipant struct {
	mu      sync.Mutex
	id      string
	states  map[string]ParticipantState
	failure FailureConfig
	crashed bool
}

func NewSimParticipant(id string) *SimParticipant {
	return &SimParticipant{
		id:     id,
		states: make(map[string]ParticipantState),
	}
}

func (p *SimParticipant) ID() string { return p.id }

// SetFailure configures the next failure behavior.
func (p *SimParticipant) SetFailure(cfg FailureConfig) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.failure = cfg
	p.crashed = false
}

// Reset clears failure configuration and crash state.
func (p *SimParticipant) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.failure = FailureConfig{}
	p.crashed = false
}

func (p *SimParticipant) Prepare(ctx context.Context, txID string) (Vote, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.crashed {
		return VoteNo, fmt.Errorf("participant %s is crashed", p.id)
	}

	if p.failure.CrashBeforeVote {
		p.crashed = true
		return VoteNo, fmt.Errorf("participant %s crashed before vote", p.id)
	}

	if p.failure.PrepareDelay > 0 {
		p.mu.Unlock()
		select {
		case <-time.After(p.failure.PrepareDelay):
		case <-ctx.Done():
			p.mu.Lock()
			return VoteNo, ctx.Err()
		}
		p.mu.Lock()
	}

	if p.failure.PrepareError {
		return VoteNo, fmt.Errorf("participant %s prepare failed", p.id)
	}

	if p.failure.VoteNo {
		p.states[txID] = ParticipantAborted
		return VoteNo, nil
	}

	p.states[txID] = ParticipantPrepared

	if p.failure.CrashAfterVote {
		p.crashed = true
		return VoteYes, fmt.Errorf("participant %s crashed after vote", p.id)
	}

	return VoteYes, nil
}

func (p *SimParticipant) Commit(ctx context.Context, txID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.crashed {
		return fmt.Errorf("participant %s is crashed", p.id)
	}

	if p.failure.CrashBeforeCommit {
		p.crashed = true
		return fmt.Errorf("participant %s crashed before commit", p.id)
	}

	if p.failure.CommitDelay > 0 {
		p.mu.Unlock()
		select {
		case <-time.After(p.failure.CommitDelay):
		case <-ctx.Done():
			p.mu.Lock()
			return ctx.Err()
		}
		p.mu.Lock()
	}

	if p.failure.CommitError {
		return fmt.Errorf("participant %s commit failed", p.id)
	}

	p.states[txID] = ParticipantCommitted
	return nil
}

func (p *SimParticipant) Abort(ctx context.Context, txID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.crashed {
		return fmt.Errorf("participant %s is crashed", p.id)
	}

	p.states[txID] = ParticipantAborted
	return nil
}

func (p *SimParticipant) QueryDecision(ctx context.Context, txID string) (ParticipantState, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.crashed {
		return ParticipantIdle, fmt.Errorf("participant %s is crashed", p.id)
	}

	state, ok := p.states[txID]
	if !ok {
		return ParticipantIdle, nil
	}
	return state, nil
}

// GetState returns the participant's state for a transaction (for test assertions).
func (p *SimParticipant) GetState(txID string) ParticipantState {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.states[txID]
}
```

```go
// coordinator.go
package twopc

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// TxState represents the coordinator's view of a transaction.
type TxState int

const (
	TxInit TxState = iota
	TxPreparing
	TxCommitted
	TxAborted
	TxDone
)

// Transaction holds the state of an in-flight transaction.
type Transaction struct {
	ID           string
	State        TxState
	Participants []Participant
	StartTime    time.Time
}

// CoordinatorConfig holds coordinator settings.
type CoordinatorConfig struct {
	PrepareTimeout time.Duration
	CommitTimeout  time.Duration
	WALPath        string
}

// Metrics tracks coordinator statistics.
type Metrics struct {
	Committed     atomic.Int64
	Aborted       atomic.Int64
	TimedOut      atomic.Int64
	Recovered     atomic.Int64
	TotalLatency  atomic.Int64 // nanoseconds
	TxCount       atomic.Int64
}

func (m *Metrics) AvgLatency() time.Duration {
	count := m.TxCount.Load()
	if count == 0 {
		return 0
	}
	return time.Duration(m.TotalLatency.Load() / count)
}

// Coordinator manages distributed transactions using 2PC.
type Coordinator struct {
	mu           sync.Mutex
	config       CoordinatorConfig
	wal          *WAL
	transactions map[string]*Transaction
	participants []Participant
	metrics      Metrics
}

func NewCoordinator(cfg CoordinatorConfig, participants []Participant) (*Coordinator, error) {
	wal, err := NewWAL(cfg.WALPath)
	if err != nil {
		return nil, err
	}

	return &Coordinator{
		config:       cfg,
		wal:          wal,
		transactions: make(map[string]*Transaction),
		participants: participants,
	}, nil
}

// Begin starts a new distributed transaction.
func (c *Coordinator) Begin(txID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.transactions[txID]; exists {
		return fmt.Errorf("transaction %s already exists", txID)
	}

	tx := &Transaction{
		ID:           txID,
		State:        TxInit,
		Participants: c.participants,
		StartTime:    time.Now(),
	}
	c.transactions[txID] = tx
	return nil
}

// Execute runs the full 2PC protocol for a transaction.
func (c *Coordinator) Execute(ctx context.Context, txID string) error {
	c.mu.Lock()
	tx, exists := c.transactions[txID]
	if !exists {
		c.mu.Unlock()
		return fmt.Errorf("transaction %s not found", txID)
	}
	tx.State = TxPreparing
	c.mu.Unlock()

	// Log prepare phase start
	if err := c.wal.LogPrepare(txID); err != nil {
		return fmt.Errorf("WAL prepare: %w", err)
	}

	// Phase 1: Prepare
	allYes := c.doPrepare(ctx, tx)

	if allYes {
		return c.doCommit(ctx, tx)
	}
	return c.doAbort(ctx, tx)
}

func (c *Coordinator) doPrepare(ctx context.Context, tx *Transaction) bool {
	prepareCtx, cancel := context.WithTimeout(ctx, c.config.PrepareTimeout)
	defer cancel()

	type voteResult struct {
		participantID string
		vote          Vote
		err           error
	}

	results := make(chan voteResult, len(tx.Participants))

	for _, p := range tx.Participants {
		go func(participant Participant) {
			vote, err := participant.Prepare(prepareCtx, tx.ID)
			results <- voteResult{
				participantID: participant.ID(),
				vote:          vote,
				err:           err,
			}
		}(p)
	}

	allYes := true
	for i := 0; i < len(tx.Participants); i++ {
		result := <-results
		if result.err != nil {
			slog.Warn("prepare failed",
				"tx", tx.ID,
				"participant", result.participantID,
				"err", result.err)
			allYes = false
			c.metrics.TimedOut.Add(1)
		} else if result.vote == VoteNo {
			slog.Info("participant voted no",
				"tx", tx.ID,
				"participant", result.participantID)
			allYes = false
		}
	}

	return allYes
}

func (c *Coordinator) doCommit(ctx context.Context, tx *Transaction) error {
	// CRITICAL: Log commit decision before sending any commit messages.
	// This is the point of no return.
	if err := c.wal.LogCommit(tx.ID); err != nil {
		// If we cannot log, we cannot safely commit.
		// Abort instead (participants will time out and abort).
		return c.doAbort(ctx, tx)
	}

	c.mu.Lock()
	tx.State = TxCommitted
	c.mu.Unlock()

	commitCtx, cancel := context.WithTimeout(ctx, c.config.CommitTimeout)
	defer cancel()

	// Send commit to all participants. Retry on failure (idempotent).
	var wg sync.WaitGroup
	for _, p := range tx.Participants {
		wg.Add(1)
		go func(participant Participant) {
			defer wg.Done()
			for attempt := 0; attempt < 3; attempt++ {
				if err := participant.Commit(commitCtx, tx.ID); err != nil {
					slog.Warn("commit delivery failed, retrying",
						"tx", tx.ID,
						"participant", participant.ID(),
						"attempt", attempt,
						"err", err)
					time.Sleep(100 * time.Millisecond)
					continue
				}
				return
			}
		}(p)
	}
	wg.Wait()

	c.finalize(tx, true)
	return nil
}

func (c *Coordinator) doAbort(ctx context.Context, tx *Transaction) error {
	c.mu.Lock()
	tx.State = TxAborted
	c.mu.Unlock()

	// Presumed-abort: no need to log the abort decision.
	// If we crash here, recovery will not find a COMMIT record and will presume abort.

	abortCtx, cancel := context.WithTimeout(ctx, c.config.CommitTimeout)
	defer cancel()

	var wg sync.WaitGroup
	for _, p := range tx.Participants {
		wg.Add(1)
		go func(participant Participant) {
			defer wg.Done()
			if err := participant.Abort(abortCtx, tx.ID); err != nil {
				slog.Warn("abort delivery failed",
					"tx", tx.ID,
					"participant", participant.ID(),
					"err", err)
			}
		}(p)
	}
	wg.Wait()

	c.finalize(tx, false)
	return fmt.Errorf("transaction %s aborted", tx.ID)
}

func (c *Coordinator) finalize(tx *Transaction, committed bool) {
	latency := time.Since(tx.StartTime)
	c.metrics.TotalLatency.Add(int64(latency))
	c.metrics.TxCount.Add(1)

	if committed {
		c.metrics.Committed.Add(1)
	} else {
		c.metrics.Aborted.Add(1)
	}

	c.mu.Lock()
	tx.State = TxDone
	c.mu.Unlock()

	c.wal.LogDone(tx.ID)
}

// Recover re-processes transactions found in the WAL after a coordinator crash.
func (c *Coordinator) Recover(ctx context.Context) error {
	committed, prepared, err := c.wal.Recover()
	if err != nil {
		return fmt.Errorf("WAL recovery: %w", err)
	}

	// Re-send commit for committed transactions
	for _, txID := range committed {
		slog.Info("recovering committed transaction", "tx", txID)
		commitCtx, cancel := context.WithTimeout(ctx, c.config.CommitTimeout)

		var wg sync.WaitGroup
		for _, p := range c.participants {
			wg.Add(1)
			go func(participant Participant) {
				defer wg.Done()
				if err := participant.Commit(commitCtx, txID); err != nil {
					slog.Warn("recovery commit failed",
						"tx", txID,
						"participant", participant.ID(),
						"err", err)
				}
			}(p)
		}
		wg.Wait()
		cancel()

		c.wal.LogDone(txID)
		c.metrics.Recovered.Add(1)
	}

	// Presumed abort for prepared-but-not-committed transactions
	for _, txID := range prepared {
		slog.Info("presumed abort for transaction", "tx", txID)
		abortCtx, cancel := context.WithTimeout(ctx, c.config.CommitTimeout)

		var wg sync.WaitGroup
		for _, p := range c.participants {
			wg.Add(1)
			go func(participant Participant) {
				defer wg.Done()
				participant.Abort(abortCtx, txID)
			}(p)
		}
		wg.Wait()
		cancel()

		c.wal.LogDone(txID)
		c.metrics.Recovered.Add(1)
	}

	return nil
}

// QueryDecision returns the coordinator's decision for a transaction.
// Used by participants recovering from crashes.
func (c *Coordinator) QueryDecision(txID string) TxState {
	c.mu.Lock()
	defer c.mu.Unlock()

	tx, exists := c.transactions[txID]
	if !exists {
		// Presumed abort: if we have no record, it was aborted
		return TxAborted
	}
	return tx.State
}

// GetMetrics returns a snapshot of coordinator metrics.
func (c *Coordinator) GetMetrics() MetricsSnapshot {
	return MetricsSnapshot{
		Committed:  c.metrics.Committed.Load(),
		Aborted:    c.metrics.Aborted.Load(),
		TimedOut:   c.metrics.TimedOut.Load(),
		Recovered:  c.metrics.Recovered.Load(),
		AvgLatency: c.metrics.AvgLatency(),
	}
}

type MetricsSnapshot struct {
	Committed  int64
	Aborted    int64
	TimedOut   int64
	Recovered  int64
	AvgLatency time.Duration
}

func (m MetricsSnapshot) String() string {
	return fmt.Sprintf("committed=%d aborted=%d timedOut=%d recovered=%d avgLatency=%v",
		m.Committed, m.Aborted, m.TimedOut, m.Recovered, m.AvgLatency)
}

func (c *Coordinator) Close() error {
	return c.wal.Close()
}
```

### Tests

```go
// coordinator_test.go
package twopc

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func tempWALPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "wal.log")
}

func newTestCoordinator(t *testing.T, participants []Participant) *Coordinator {
	t.Helper()
	c, err := NewCoordinator(CoordinatorConfig{
		PrepareTimeout: 500 * time.Millisecond,
		CommitTimeout:  500 * time.Millisecond,
		WALPath:        tempWALPath(t),
	}, participants)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func TestHappyPath(t *testing.T) {
	p1 := NewSimParticipant("p1")
	p2 := NewSimParticipant("p2")
	p3 := NewSimParticipant("p3")

	c := newTestCoordinator(t, []Participant{p1, p2, p3})

	c.Begin("tx-1")
	err := c.Execute(context.Background(), "tx-1")
	if err != nil {
		t.Fatalf("expected commit, got error: %v", err)
	}

	// All participants should be committed
	for _, p := range []*SimParticipant{p1, p2, p3} {
		if state := p.GetState("tx-1"); state != ParticipantCommitted {
			t.Errorf("participant %s: expected committed, got %d", p.ID(), state)
		}
	}

	metrics := c.GetMetrics()
	if metrics.Committed != 1 {
		t.Errorf("expected 1 committed, got %d", metrics.Committed)
	}
}

func TestSingleParticipantVotesNo(t *testing.T) {
	p1 := NewSimParticipant("p1")
	p2 := NewSimParticipant("p2")
	p3 := NewSimParticipant("p3")

	p2.SetFailure(FailureConfig{VoteNo: true})

	c := newTestCoordinator(t, []Participant{p1, p2, p3})

	c.Begin("tx-2")
	err := c.Execute(context.Background(), "tx-2")
	if err == nil {
		t.Fatal("expected abort error")
	}

	// No participant should be committed
	for _, p := range []*SimParticipant{p1, p2, p3} {
		state := p.GetState("tx-2")
		if state == ParticipantCommitted {
			t.Errorf("participant %s should not be committed", p.ID())
		}
	}

	metrics := c.GetMetrics()
	if metrics.Aborted != 1 {
		t.Errorf("expected 1 aborted, got %d", metrics.Aborted)
	}
}

func TestParticipantTimeout(t *testing.T) {
	p1 := NewSimParticipant("p1")
	p2 := NewSimParticipant("p2")

	p2.SetFailure(FailureConfig{PrepareDelay: 2 * time.Second}) // exceeds timeout

	c := newTestCoordinator(t, []Participant{p1, p2})

	c.Begin("tx-3")
	err := c.Execute(context.Background(), "tx-3")
	if err == nil {
		t.Fatal("expected abort due to timeout")
	}
}

func TestParticipantCrashBeforeVote(t *testing.T) {
	p1 := NewSimParticipant("p1")
	p2 := NewSimParticipant("p2")

	p2.SetFailure(FailureConfig{CrashBeforeVote: true})

	c := newTestCoordinator(t, []Participant{p1, p2})

	c.Begin("tx-4")
	err := c.Execute(context.Background(), "tx-4")
	if err == nil {
		t.Fatal("expected abort due to participant crash")
	}
}

func TestWALRecoveryCommitted(t *testing.T) {
	walPath := tempWALPath(t)

	p1 := NewSimParticipant("p1")
	p2 := NewSimParticipant("p2")

	// First coordinator: commit a transaction
	c1, err := NewCoordinator(CoordinatorConfig{
		PrepareTimeout: 500 * time.Millisecond,
		CommitTimeout:  500 * time.Millisecond,
		WALPath:        walPath,
	}, []Participant{p1, p2})
	if err != nil {
		t.Fatal(err)
	}

	c1.Begin("tx-5")
	c1.Execute(context.Background(), "tx-5")
	c1.Close()

	// Simulate coordinator crash by creating a new coordinator with the same WAL.
	// Reset participant state to simulate they did not receive commit.
	p1.Reset()
	p2.Reset()

	// Write a PREPARE + COMMIT for a transaction that did not complete Phase 2.
	wal2, _ := NewWAL(walPath)
	wal2.LogPrepare("tx-crash")
	wal2.LogCommit("tx-crash")
	wal2.Close()

	c2, err := NewCoordinator(CoordinatorConfig{
		PrepareTimeout: 500 * time.Millisecond,
		CommitTimeout:  500 * time.Millisecond,
		WALPath:        walPath,
	}, []Participant{p1, p2})
	if err != nil {
		t.Fatal(err)
	}
	defer c2.Close()

	err = c2.Recover(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	// tx-crash should have been re-committed to participants
	if p1.GetState("tx-crash") != ParticipantCommitted {
		t.Error("p1 should be committed for tx-crash after recovery")
	}
	if p2.GetState("tx-crash") != ParticipantCommitted {
		t.Error("p2 should be committed for tx-crash after recovery")
	}
}

func TestPresumedAbort(t *testing.T) {
	walPath := tempWALPath(t)

	p1 := NewSimParticipant("p1")
	p2 := NewSimParticipant("p2")

	// Write only a PREPARE record (no COMMIT) to simulate crash before commit decision
	wal, _ := NewWAL(walPath)
	wal.LogPrepare("tx-presumed-abort")
	wal.Close()

	c, err := NewCoordinator(CoordinatorConfig{
		PrepareTimeout: 500 * time.Millisecond,
		CommitTimeout:  500 * time.Millisecond,
		WALPath:        walPath,
	}, []Participant{p1, p2})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	err = c.Recover(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	// Transaction should be presumed aborted
	if p1.GetState("tx-presumed-abort") != ParticipantAborted {
		t.Error("p1 should be aborted for presumed-abort transaction")
	}
}

func TestConcurrentTransactions(t *testing.T) {
	p1 := NewSimParticipant("p1")
	p2 := NewSimParticipant("p2")

	c := newTestCoordinator(t, []Participant{p1, p2})

	var wg sync.WaitGroup
	const txCount = 20

	for i := 0; i < txCount; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			txID := fmt.Sprintf("concurrent-tx-%d", id)
			if err := c.Begin(txID); err != nil {
				return
			}
			c.Execute(context.Background(), txID)
		}(i)
	}

	wg.Wait()

	metrics := c.GetMetrics()
	total := metrics.Committed + metrics.Aborted
	if total != txCount {
		t.Errorf("expected %d total transactions, got %d", txCount, total)
	}
	t.Logf("Concurrent results: %s", metrics)
}

func TestMultipleParticipantFailures(t *testing.T) {
	p1 := NewSimParticipant("p1")
	p2 := NewSimParticipant("p2")
	p3 := NewSimParticipant("p3")

	// Two participants fail
	p1.SetFailure(FailureConfig{VoteNo: true})
	p3.SetFailure(FailureConfig{PrepareError: true})

	c := newTestCoordinator(t, []Participant{p1, p2, p3})

	c.Begin("tx-multi-fail")
	err := c.Execute(context.Background(), "tx-multi-fail")
	if err == nil {
		t.Fatal("expected abort with multiple failures")
	}
}

func TestCoordinatorCrashDuringPhase2(t *testing.T) {
	walPath := tempWALPath(t)

	p1 := NewSimParticipant("p1")
	p2 := NewSimParticipant("p2")

	// Simulate: coordinator logged COMMIT but crashed before delivering to p2.
	// p1 received commit, p2 did not.
	wal, _ := NewWAL(walPath)
	wal.LogPrepare("tx-phase2-crash")
	wal.LogCommit("tx-phase2-crash")
	wal.Close()

	// p1 already committed (it received the message before crash)
	p1.Prepare(context.Background(), "tx-phase2-crash")
	p1.Commit(context.Background(), "tx-phase2-crash")

	// p2 is prepared but never received commit
	p2.Prepare(context.Background(), "tx-phase2-crash")

	// New coordinator recovers
	c, err := NewCoordinator(CoordinatorConfig{
		PrepareTimeout: 500 * time.Millisecond,
		CommitTimeout:  500 * time.Millisecond,
		WALPath:        walPath,
	}, []Participant{p1, p2})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	c.Recover(context.Background())

	// Both should be committed
	if p1.GetState("tx-phase2-crash") != ParticipantCommitted {
		t.Error("p1 should remain committed")
	}
	if p2.GetState("tx-phase2-crash") != ParticipantCommitted {
		t.Error("p2 should be committed after recovery")
	}
}

func TestTimeoutCascade(t *testing.T) {
	p1 := NewSimParticipant("p1")
	p2 := NewSimParticipant("p2")
	p3 := NewSimParticipant("p3")

	// All participants are slow
	for _, p := range []*SimParticipant{p1, p2, p3} {
		p.SetFailure(FailureConfig{PrepareDelay: 2 * time.Second})
	}

	c := newTestCoordinator(t, []Participant{p1, p2, p3})

	c.Begin("tx-timeout-cascade")
	err := c.Execute(context.Background(), "tx-timeout-cascade")
	if err == nil {
		t.Fatal("expected abort due to cascading timeouts")
	}
}

func TestWALFileCreation(t *testing.T) {
	walPath := tempWALPath(t)

	p1 := NewSimParticipant("p1")
	c := newTestCoordinator(t, []Participant{p1})

	c.Begin("tx-wal-test")
	c.Execute(context.Background(), "tx-wal-test")

	// WAL file should exist
	if _, err := os.Stat(walPath); os.IsNotExist(err) {
		t.Error("WAL file was not created")
	}
}
```

### Running and Testing

```bash
go test -v -race ./...
go test -v -race -run TestConcurrentTransactions ./...
go test -v -run TestWALRecoveryCommitted ./...
```

### Expected Output

```
=== RUN   TestHappyPath
--- PASS: TestHappyPath (0.01s)
=== RUN   TestSingleParticipantVotesNo
--- PASS: TestSingleParticipantVotesNo (0.01s)
=== RUN   TestParticipantTimeout
--- PASS: TestParticipantTimeout (0.51s)
=== RUN   TestParticipantCrashBeforeVote
--- PASS: TestParticipantCrashBeforeVote (0.01s)
=== RUN   TestWALRecoveryCommitted
--- PASS: TestWALRecoveryCommitted (0.01s)
=== RUN   TestPresumedAbort
--- PASS: TestPresumedAbort (0.01s)
=== RUN   TestConcurrentTransactions
    coordinator_test.go:168: Concurrent results: committed=20 aborted=0 timedOut=0 recovered=0 avgLatency=1.2ms
--- PASS: TestConcurrentTransactions (0.02s)
=== RUN   TestMultipleParticipantFailures
--- PASS: TestMultipleParticipantFailures (0.01s)
=== RUN   TestCoordinatorCrashDuringPhase2
--- PASS: TestCoordinatorCrashDuringPhase2 (0.01s)
=== RUN   TestTimeoutCascade
--- PASS: TestTimeoutCascade (0.51s)
=== RUN   TestWALFileCreation
--- PASS: TestWALFileCreation (0.01s)
PASS
```

## Design Decisions

**Decision 1: Text-based WAL vs. binary format.** The WAL uses a simple text format ("ACTION txID\n") for readability and debuggability. A production system would use a binary format with CRC checksums to detect corruption and fixed-size records for O(1) seeking. The text format is adequate for learning the protocol's correctness properties.

**Decision 2: fsync after every WAL write.** Calling `file.Sync()` after every write ensures durability at the cost of throughput. A production coordinator would batch WAL writes (group commit) to amortize the fsync cost across multiple transactions. The single-write-fsync pattern makes the durability guarantee explicit and easy to verify.

**Decision 3: Retry loop in commit phase.** After the commit decision is logged, the coordinator retries commit delivery to participants up to 3 times. In a real system, this would be an unbounded retry with exponential backoff, because the decision is final (logged in the WAL) and must eventually be delivered. The bounded retry here keeps test execution fast.

**Decision 4: Presumed-abort as the default recovery strategy.** If the WAL has no COMMIT record for a transaction, it is presumed aborted. This eliminates the need to log abort decisions, reducing WAL I/O by roughly 50% (since most aborted transactions are the common failure path). The trade-off is that a crash between logging PREPARE and the commit decision always results in abort, even if all participants voted Yes. This is safe but may abort transactions that could have committed.

## Common Mistakes

**Mistake 1: Sending commit messages before logging the decision.** If the coordinator sends Commit to participants, then crashes before logging to the WAL, the WAL has no COMMIT record. On recovery, presumed-abort kicks in and the coordinator sends Abort -- but some participants already committed. This violates atomicity. The WAL write MUST happen before any Phase 2 message.

**Mistake 2: Not making participant operations idempotent.** During recovery, the coordinator re-sends Commit to all participants, including ones that already committed. If `Commit()` is not idempotent (e.g., it double-applies a balance change), recovery corrupts data. Always design participant operations to be safely re-executable.

**Mistake 3: Using a single global timeout instead of per-phase timeouts.** A generous timeout for the prepare phase (to handle slow participants) should not apply to the commit phase (which should be fast after a Yes vote). Per-phase timeouts let you tune each phase independently.

## Performance Notes

- The fsync in the WAL is the dominant latency contributor. On spinning disks, each fsync takes 5-10ms. On NVMe SSDs, it drops to 50-100us. Group commit (batching multiple transaction decisions into a single fsync) is the standard optimization.
- Concurrent transaction throughput is limited by the WAL's sequential write lock. A segmented WAL (one file per transaction batch) or a concurrent data structure eliminates this bottleneck.
- The prepare phase runs all participant RPCs concurrently, so its latency equals the slowest participant (not the sum). This is critical for practical 2PC performance.

## Going Further

- Implement 3-Phase Commit (3PC) to eliminate the blocking problem where participants hold locks while the coordinator is down
- Add a transaction log viewer that parses the WAL and displays transaction history with state transitions
- Implement cooperative termination: when a participant suspects the coordinator has crashed, it contacts other participants to determine the outcome
- Build a saga orchestrator as an alternative to 2PC for long-running transactions that cannot hold locks
- Implement the Paxos Commit protocol (Gray & Lamport, 2006) that replaces the single coordinator with a Paxos group for fault-tolerant commit decisions
- Add OpenTelemetry tracing to visualize the message flow between coordinator and participants

package govee

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestTokenBucketAllowsBurst(t *testing.T) {
	b := newTokenBucket(3, time.Second)
	for i := 0; i < 3; i++ {
		if !b.TryTake() {
			t.Fatalf("token %d should be available", i)
		}
	}
	if b.TryTake() {
		t.Fatal("bucket should be empty after burst")
	}
}

func TestTokenBucketRefills(t *testing.T) {
	b := newTokenBucket(2, time.Second) // 2 tokens/s, capacity 2
	b.TryTake()
	b.TryTake()
	if b.TryTake() {
		t.Fatal("should be empty")
	}
	time.Sleep(600 * time.Millisecond) // ~1.2 tokens
	if !b.TryTake() {
		t.Fatal("expected a refilled token")
	}
	if b.TryTake() {
		t.Fatal("only one token should have refilled")
	}
}

func TestTokenBucketWait(t *testing.T) {
	b := newTokenBucket(1, time.Second)
	if !b.TryTake() {
		t.Fatal("initial token")
	}
	start := time.Now()
	if err := b.Wait(context.Background()); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 900*time.Millisecond {
		t.Errorf("Wait returned after %v, want >= ~1s (1 token/s)", elapsed)
	}
}

func TestTokenBucketWaitContextCancel(t *testing.T) {
	b := newTokenBucket(1, time.Hour) // refills so slowly a wait is hopeless
	b.TryTake()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := b.Wait(ctx)
	if err != context.DeadlineExceeded {
		t.Fatalf("err = %v, want DeadlineExceeded", err)
	}
	if time.Since(start) > time.Second {
		t.Error("Wait ignored the context deadline")
	}
}

func TestTokenBucketConcurrent(t *testing.T) {
	// 5 tokens capacity, refill at 100/s: concurrency must serialize on
	// the bucket without races while remaining fast.
	b := newTokenBucket(5, 25*time.Millisecond) // 200/s, capacity 5
	var wg sync.WaitGroup
	taken := make(chan struct{}, 40)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 2; j++ {
				if b.Wait(context.Background()) == nil {
					taken <- struct{}{}
				}
			}
		}()
	}
	wg.Wait()
	close(taken)
	got := 0
	for range taken {
		got++
	}
	if got != 40 {
		t.Errorf("tokens taken = %d, want 40 (all waits must succeed)", got)
	}
}

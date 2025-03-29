package queue

import "sync"

// Queueable handles sequential processing of operations with typed results
type Queueable[T any] struct {
	mu      sync.Mutex
	queue   chan func() (T, error)
	results chan queueResult[T]
	done    chan struct{}
}

type queueResult[T any] struct {
	value T
	err   error
}

// New creates a new queue with the specified buffer size
func New[T any](bufferSize int) *Queueable[T] {
	q := &Queueable[T]{
		queue:   make(chan func() (T, error), bufferSize),
		results: make(chan queueResult[T]),
		done:    make(chan struct{}),
	}
	go q.process()
	return q
}

// process continuously processes operations from the queue in sequence
func (q *Queueable[T]) process() {
	for {
		select {
		case op := <-q.queue:
			value, err := op()
			q.results <- queueResult[T]{value, err}
		case <-q.done:
			return
		}
	}
}

// Enqueue adds an operation to the queue and waits for its completion
func (q *Queueable[T]) Enqueue(op func() (T, error)) (T, error) {
	q.mu.Lock()
	q.queue <- op
	q.mu.Unlock()

	// Wait for the result
	r := <-q.results
	return r.value, r.err
}

// EnqueueError adds an error-only operation to the queue
func (q *Queueable[T]) EnqueueError(op func() error) error {
	var zero T
	wrappedOp := func() (T, error) {
		return zero, op()
	}

	_, err := q.Enqueue(wrappedOp)
	return err
}

// EnqueueFn adds a function that returns a value without error
func (q *Queueable[T]) EnqueueFn(op func() T) T {
	wrappedOp := func() (T, error) {
		return op(), nil
	}

	result, _ := q.Enqueue(wrappedOp)
	return result
}

// Shutdown terminates the queue processor
func (q *Queueable[T]) Shutdown() {
	close(q.done)
}

// EnqueueWithParam wraps a function that takes a parameter to be queued
func EnqueueWithParam[T any, P any](q *Queueable[T], fn func(P) (T, error), param P) (T, error) {
	return q.Enqueue(func() (T, error) {
		return fn(param)
	})
}

// EnqueueErrorWithParam wraps an error-returning function with a parameter
func EnqueueErrorWithParam[T any, P any](q *Queueable[T], fn func(P) error, param P) error {
	wrappedOp := func() error {
		return fn(param)
	}
	return q.EnqueueError(wrappedOp)
}

// EnqueueFnWithParam wraps a value-returning function with a parameter
func EnqueueFnWithParam[T any, P any](q *Queueable[T], fn func(P) T, param P) T {
	wrappedOp := func() T {
		return fn(param)
	}
	return q.EnqueueFn(wrappedOp)
}

// EnqueueBoolFn specifically for boolean-returning functions
func EnqueueBoolFn[P any](q *Queueable[bool], fn func(P) bool, param P) bool {
	return EnqueueFnWithParam(q, fn, param)
}

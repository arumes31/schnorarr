package pool

// Semaphore is a simple channel-based semaphore to limit concurrency
var GlobalTransferPool = make(chan struct{}, 1)

// Acquire takes a slot in the pool
func Acquire() {
	GlobalTransferPool <- struct{}{}
}

// Release frees a slot in the pool
func Release() {
	<-GlobalTransferPool
}

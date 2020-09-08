package controllers

type limiter struct {
	bucket chan struct{}
}

func (l *limiter) acquire() {
	<-l.bucket
}

func (l *limiter) release() {
	l.bucket <- struct{}{}
}

func newLimiter(concurrency int) *limiter {
	b := make(chan struct{}, concurrency)
	// fill bucket
	for token := 0; token < concurrency; token++ {
		b <- struct{}{}
	}
	return &limiter{
		bucket: b,
	}
}

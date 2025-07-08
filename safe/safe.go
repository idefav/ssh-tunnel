package safe

import (
	"log"
	"runtime/debug"
)

func GO(fn func()) {
	go func() {
		defer func() {
			if err := recover(); err != nil {
				log.Printf("Goroutine panic recovered: %v\nStack trace:\n%s", err, debug.Stack())
			}
		}()
		fn()
	}()
}

// SafeCall 为普通函数调用提供panic恢复机制
func SafeCall(fn func()) {
	defer func() {
		if err := recover(); err != nil {
			log.Printf("Function panic recovered: %v\nStack trace:\n%s", err, debug.Stack())
		}
	}()
	fn()
}

// SafeCallWithReturn 为有返回值的函数提供panic恢复机制
func SafeCallWithReturn(fn func() error) error {
	defer func() {
		if err := recover(); err != nil {
			log.Printf("Function panic recovered: %v\nStack trace:\n%s", err, debug.Stack())
		}
	}()
	return fn()
}

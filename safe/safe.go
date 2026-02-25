package safe

import (
	"fmt"
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

// SafeCallWithReturnRecover 将panic转换为error返回，便于上层做重试/自恢复。
// 注意：与 SafeCallWithReturn 不同，此函数在panic时不会返回nil。
func SafeCallWithReturnRecover(fn func() error) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			stack := debug.Stack()
			log.Printf("Function panic recovered: %v\nStack trace:\n%s", recovered, stack)
			err = fmt.Errorf("panic recovered: %v", recovered)
		}
	}()
	return fn()
}

package safe

import (
	"fmt"
	"testing"
	"time"
)

// TestSafeGO 测试 safe.GO 的 panic recovery 机制
func TestSafeGO(t *testing.T) {
	t.Log("测试 safe.GO panic recovery")
	
	// 使用 channel 来检测 goroutine 是否完成
	done := make(chan bool, 1)
	
	GO(func() {
		defer func() {
			done <- true
		}()
		time.Sleep(10 * time.Millisecond)
		panic("测试goroutine panic")
	})
	
	// 等待 goroutine 完成
	select {
	case <-done:
		t.Log("safe.GO panic recovery 工作正常")
	case <-time.After(1 * time.Second):
		t.Error("safe.GO 测试超时")
	}
}

// TestSafeCall 测试 safe.SafeCall 的 panic recovery 机制
func TestSafeCall(t *testing.T) {
	t.Log("测试 safe.SafeCall panic recovery")
	
	// 这应该不会导致程序崩溃
	SafeCall(func() {
		panic("测试SafeCall panic")
	})
	
	t.Log("safe.SafeCall panic recovery 工作正常")
}

// TestSafeCallWithReturn 测试 safe.SafeCallWithReturn 的 panic recovery 机制
func TestSafeCallWithReturn(t *testing.T) {
	t.Log("测试 safe.SafeCallWithReturn panic recovery")
	
	err := SafeCallWithReturn(func() error {
		panic("测试SafeCallWithReturn panic")
	})
	
	// SafeCallWithReturn 应该捕获 panic 并返回 nil
	if err != nil {
		t.Errorf("SafeCallWithReturn 应该捕获 panic 并返回 nil，但返回了: %v", err)
	}
	
	t.Log("safe.SafeCallWithReturn panic recovery 工作正常")
}

// TestSafeCallWithReturnNormalCase 测试 safe.SafeCallWithReturn 的正常情况
func TestSafeCallWithReturnNormalCase(t *testing.T) {
	t.Log("测试 safe.SafeCallWithReturn 正常返回")
	
	expectedError := fmt.Errorf("测试错误")
	err := SafeCallWithReturn(func() error {
		return expectedError
	})
	
	if err != expectedError {
		t.Errorf("期望返回 %v，但得到 %v", expectedError, err)
	}
	
	t.Log("safe.SafeCallWithReturn 正常返回工作正常")
}

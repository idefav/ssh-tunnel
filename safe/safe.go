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
				log.Panic(fmt.Sprintf("Go panic:%v \n%s", err, debug.Stack()))
			}
		}()
		fn()
	}()
}

package editor

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Preserve compatibility with Microspec's queue writers without depending on
// its binary or repository. The lock is held only during a QUEUE.md save.
func lockQueue(path string) (func(), error) {
	lock := filepath.Join(filepath.Dir(path), ".microspec-queue.lock")
	deadline := time.Now().Add(5 * time.Second)
	for {
		err := os.Mkdir(lock, 0700)
		if err == nil {
			return func() { _ = os.Remove(lock) }, nil
		}
		if !os.IsExist(err) {
			return nil, err
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("queue is locked (%s); retry after the writer finishes", lock)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

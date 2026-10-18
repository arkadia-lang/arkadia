package rt

import "sync"

// Task is a computation with its own evidence. The program starts as the
// main task.
type Task struct {
	cs *cancelScope // the root cancellation scope of the task
}

// cancelScope is a region of a task that can be cancelled as a whole.
type cancelScope struct {
	mu sync.Mutex
}

// nursery is the scope of the tasks started by runAsync.
type nursery struct{}

// newTaskEv returns the evidence at the root of a new task.
func newTaskEv() *Ev {
	t := &Task{cs: &cancelScope{}}
	return &Ev{eff: -1, task: t, cs: t.cs}
}

package runtime

import (
	"fmt"
	"sync"
	"time"

	"github.com/ArubikU/polyloft-bvm/internal/value"
)

type futureHandle struct {
	mu        sync.Mutex
	done      chan struct{}
	completed bool
	cancelled bool
	result    value.Value
	err       error
}

func newFutureHandle() *futureHandle {
	return &futureHandle{done: make(chan struct{})}
}

func (f *futureHandle) resolve(result value.Value, err error, cancelled bool) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.completed {
		return false
	}
	f.result = value.DeepCopy(result)
	f.err = err
	f.cancelled = cancelled
	f.completed = true
	close(f.done)
	return true
}

func (f *futureHandle) complete(result value.Value) bool {
	return f.resolve(result, nil, false)
}

func (f *futureHandle) fail(err error) bool {
	return f.resolve(value.NilValue(), err, false)
}

func (f *futureHandle) cancel() bool {
	return f.resolve(value.NilValue(), fmt.Errorf("future cancelled"), true)
}

func (f *futureHandle) await(timeout time.Duration, hasTimeout bool) (value.Value, error) {
	if hasTimeout {
		select {
		case <-f.done:
		case <-time.After(timeout):
			return value.NilValue(), fmt.Errorf("future timed out after %dms", timeout/time.Millisecond)
		}
	} else {
		<-f.done
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return value.NilValue(), f.err
	}
	return value.DeepCopy(f.result), nil
}

func (f *futureHandle) isDone() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.completed
}

type threadHandle struct {
	mu      sync.Mutex
	task    value.Value
	future  *futureHandle
	started bool
}

func newThreadHandle(task value.Value) *threadHandle {
	return &threadHandle{task: value.DeepCopy(task), future: newFutureHandle()}
}

func (t *threadHandle) start() error {
	t.mu.Lock()
	if t.started {
		t.mu.Unlock()
		return fmt.Errorf("thread already started")
	}
	t.started = true
	task := value.DeepCopy(t.task)
	future := t.future
	t.mu.Unlock()
	if GlobalVMProxy == nil {
		future.fail(fmt.Errorf("VM context is missing for thread execution"))
		return nil
	}
	go func() {
		result, err := GlobalVMProxy.CallClosureIsolated(task, nil)
		if err != nil {
			future.fail(err)
			return
		}
		future.complete(result)
	}()
	return nil
}

func (t *threadHandle) join() (value.Value, error) {
	t.mu.Lock()
	started := t.started
	future := t.future
	t.mu.Unlock()
	if !started {
		return value.NilValue(), fmt.Errorf("thread has not been started")
	}
	return future.await(0, false)
}

func (t *threadHandle) isAlive() bool {
	t.mu.Lock()
	started := t.started
	future := t.future
	t.mu.Unlock()
	return started && !future.isDone()
}

type channelHandle struct {
	mu     sync.Mutex
	ch     chan value.Value
	closed bool
}

func newChannelHandle(capacity int) *channelHandle {
	if capacity < 0 {
		capacity = 0
	}
	return &channelHandle{ch: make(chan value.Value, capacity)}
}

func (c *channelHandle) send(candidate value.Value) error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return fmt.Errorf("channel is closed")
	}
	ch := c.ch
	c.mu.Unlock()
	ch <- value.DeepCopy(candidate)
	return nil
}

func (c *channelHandle) receive() value.Value {
	item, ok := <-c.ch
	if !ok {
		return value.NilValue()
	}
	return value.DeepCopy(item)
}

func (c *channelHandle) close() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return false
	}
	c.closed = true
	close(c.ch)
	return true
}

func (c *channelHandle) isClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

func BuildConcurrentModule() *RuntimeModule {
	builder := NewModuleBuilder("Concurrent")

	builder.AddTypedFunction("thread_new", []string{TypeAny}, TypeAny, false, func(args []value.Value) (value.Value, error) {
		return value.ObjectValue(newThreadHandle(args[0])), nil
	})
	builder.AddTypedFunction("thread_start", []string{TypeAny}, TypeVoid, false, func(args []value.Value) (value.Value, error) {
		handle, ok := args[0].Object.(*threadHandle)
		if !ok {
			return value.NilValue(), fmt.Errorf("thread_start expects thread handle")
		}
		return value.NilValue(), handle.start()
	})
	builder.AddTypedFunction("thread_join", []string{TypeAny}, TypeAny, false, func(args []value.Value) (value.Value, error) {
		handle, ok := args[0].Object.(*threadHandle)
		if !ok {
			return value.NilValue(), fmt.Errorf("thread_join expects thread handle")
		}
		return handle.join()
	})
	builder.AddTypedFunction("thread_is_alive", []string{TypeAny}, TypeBool, false, func(args []value.Value) (value.Value, error) {
		handle, ok := args[0].Object.(*threadHandle)
		if !ok {
			return value.NilValue(), fmt.Errorf("thread_is_alive expects thread handle")
		}
		return value.BoolValue(handle.isAlive()), nil
	})

	builder.AddTypedFunction("future_new", []string{}, TypeAny, false, func(args []value.Value) (value.Value, error) {
		return value.ObjectValue(newFutureHandle()), nil
	})
	builder.AddTypedFunction("future_run_async", []string{TypeAny}, TypeAny, false, func(args []value.Value) (value.Value, error) {
		future := newFutureHandle()
		if GlobalVMProxy == nil {
			future.fail(fmt.Errorf("VM context is missing for async execution"))
			return value.ObjectValue(future), nil
		}
		task := value.DeepCopy(args[0])
		go func() {
			result, err := GlobalVMProxy.CallClosureIsolated(task, nil)
			if err != nil {
				future.fail(err)
				return
			}
			future.complete(result)
		}()
		return value.ObjectValue(future), nil
	})
	builder.AddTypedFunction("future_complete", []string{TypeAny, TypeAny}, TypeBool, false, func(args []value.Value) (value.Value, error) {
		future, ok := args[0].Object.(*futureHandle)
		if !ok {
			return value.NilValue(), fmt.Errorf("future_complete expects future handle")
		}
		return value.BoolValue(future.complete(args[1])), nil
	})
	builder.AddTypedFunction("future_get", []string{TypeAny}, TypeAny, false, func(args []value.Value) (value.Value, error) {
		future, ok := args[0].Object.(*futureHandle)
		if !ok {
			return value.NilValue(), fmt.Errorf("future_get expects future handle")
		}
		return future.await(0, false)
	})
	builder.AddTypedFunction("future_get_timeout", []string{TypeAny, TypeInt}, TypeAny, false, func(args []value.Value) (value.Value, error) {
		future, ok := args[0].Object.(*futureHandle)
		if !ok {
			return value.NilValue(), fmt.Errorf("future_get_timeout expects future handle")
		}
		return future.await(time.Duration(int(args[1].Num))*time.Millisecond, true)
	})
	builder.AddTypedFunction("future_is_done", []string{TypeAny}, TypeBool, false, func(args []value.Value) (value.Value, error) {
		future, ok := args[0].Object.(*futureHandle)
		if !ok {
			return value.NilValue(), fmt.Errorf("future_is_done expects future handle")
		}
		return value.BoolValue(future.isDone()), nil
	})
	builder.AddTypedFunction("future_cancel", []string{TypeAny}, TypeBool, false, func(args []value.Value) (value.Value, error) {
		future, ok := args[0].Object.(*futureHandle)
		if !ok {
			return value.NilValue(), fmt.Errorf("future_cancel expects future handle")
		}
		return value.BoolValue(future.cancel()), nil
	})
	builder.AddTypedFunction("future_then", []string{TypeAny, TypeAny}, TypeAny, false, func(args []value.Value) (value.Value, error) {
		parent, ok := args[0].Object.(*futureHandle)
		if !ok {
			return value.NilValue(), fmt.Errorf("future_then expects future handle")
		}
		next := newFutureHandle()
		callback := value.DeepCopy(args[1])
		go func() {
			result, err := parent.await(0, false)
			if err != nil {
				next.fail(err)
				return
			}
			if GlobalVMProxy == nil {
				next.fail(fmt.Errorf("VM context is missing for future continuation"))
				return
			}
			continued, err := GlobalVMProxy.CallClosureIsolated(callback, []value.Value{result})
			if err != nil {
				next.fail(err)
				return
			}
			next.complete(continued)
		}()
		return value.ObjectValue(next), nil
	})
	builder.AddTypedFunction("future_catch", []string{TypeAny, TypeAny}, TypeAny, false, func(args []value.Value) (value.Value, error) {
		parent, ok := args[0].Object.(*futureHandle)
		if !ok {
			return value.NilValue(), fmt.Errorf("future_catch expects future handle")
		}
		next := newFutureHandle()
		handler := value.DeepCopy(args[1])
		go func() {
			result, err := parent.await(0, false)
			if err == nil {
				next.complete(result)
				return
			}
			if GlobalVMProxy == nil {
				next.fail(fmt.Errorf("VM context is missing for future catch"))
				return
			}
			recovered, callErr := GlobalVMProxy.CallClosureIsolated(handler, []value.Value{value.StringValue(err.Error())})
			if callErr != nil {
				next.fail(callErr)
				return
			}
			next.complete(recovered)
		}()
		return value.ObjectValue(next), nil
	})
	builder.AddTypedFunction("future_finally", []string{TypeAny, TypeAny}, TypeAny, false, func(args []value.Value) (value.Value, error) {
		parent, ok := args[0].Object.(*futureHandle)
		if !ok {
			return value.NilValue(), fmt.Errorf("future_finally expects future handle")
		}
		next := newFutureHandle()
		handler := value.DeepCopy(args[1])
		go func() {
			result, err := parent.await(0, false)
			if GlobalVMProxy == nil {
				next.fail(fmt.Errorf("VM context is missing for future finally"))
				return
			}
			_, callErr := GlobalVMProxy.CallClosureIsolated(handler, nil)
			if callErr != nil {
				next.fail(callErr)
				return
			}
			if err != nil {
				next.fail(err)
				return
			}
			next.complete(result)
		}()
		return value.ObjectValue(next), nil
	})

	builder.AddTypedFunction("channel_new", []string{TypeInt}, TypeAny, false, func(args []value.Value) (value.Value, error) {
		return value.ObjectValue(newChannelHandle(int(args[0].Num))), nil
	})
	builder.AddTypedFunction("channel_send", []string{TypeAny, TypeAny}, TypeVoid, false, func(args []value.Value) (value.Value, error) {
		handle, ok := args[0].Object.(*channelHandle)
		if !ok {
			return value.NilValue(), fmt.Errorf("channel_send expects channel handle")
		}
		return value.NilValue(), handle.send(args[1])
	})
	builder.AddTypedFunction("channel_receive", []string{TypeAny}, TypeAny, false, func(args []value.Value) (value.Value, error) {
		handle, ok := args[0].Object.(*channelHandle)
		if !ok {
			return value.NilValue(), fmt.Errorf("channel_receive expects channel handle")
		}
		return handle.receive(), nil
	})
	builder.AddTypedFunction("channel_close", []string{TypeAny}, TypeBool, false, func(args []value.Value) (value.Value, error) {
		handle, ok := args[0].Object.(*channelHandle)
		if !ok {
			return value.NilValue(), fmt.Errorf("channel_close expects channel handle")
		}
		return value.BoolValue(handle.close()), nil
	})
	builder.AddTypedFunction("channel_is_closed", []string{TypeAny}, TypeBool, false, func(args []value.Value) (value.Value, error) {
		handle, ok := args[0].Object.(*channelHandle)
		if !ok {
			return value.NilValue(), fmt.Errorf("channel_is_closed expects channel handle")
		}
		return value.BoolValue(handle.isClosed()), nil
	})

	return builder.Build()
}

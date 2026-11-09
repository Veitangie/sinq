package event

import (
	"context"
	"errors"
	"log/slog"
	"sync"
)

type Bus struct {
	bus      chan Event
	mcSinks  []chan<- Event
	beSinks  []chan<- Event
	logger   *slog.Logger
	sinkMut  *sync.RWMutex
	closeMut *sync.RWMutex
	signal   chan struct{}
	done     bool
}

func (e Event) logAttrs() []slog.Attr {
	res := make([]slog.Attr, 0, len(e.Attrs)+3)
	if !e.RunId.IsEmpty() {
		res = append(res, slog.Attr{
			Key:   "runId",
			Value: e.RunId.LogValue(),
		})
	}
	res = append(res, slog.Attr{
		Key:   "system",
		Value: slog.StringValue(e.Source.String()),
	})
	if e.WorkerId != 0 {
		res = append(res, slog.Attr{
			Key:   "workerId",
			Value: slog.IntValue(e.WorkerId),
		})
	}

	res = append(res, e.Attrs...)
	return res
}

func NewBus(bufsize int, logger *slog.Logger) (*Bus, error) {
	if logger == nil {
		return nil, errors.New("Unable to construct event bus: logger is nil")
	}

	return &Bus{
		bus:      make(chan Event, bufsize),
		mcSinks:  []chan<- Event{},
		beSinks:  []chan<- Event{},
		logger:   logger,
		sinkMut:  &sync.RWMutex{},
		closeMut: &sync.RWMutex{},
		signal:   make(chan struct{}),
		done:     false,
	}, nil
}

func (eb *Bus) RegisterMC(bufsize int) <-chan Event {
	eb.closeMut.RLock()
	defer eb.closeMut.RUnlock()

	newConsumer := make(chan Event, bufsize)
	if eb.done {
		close(newConsumer)
		return newConsumer
	}

	eb.sinkMut.Lock()
	eb.mcSinks = append(eb.mcSinks, newConsumer)
	id := len(eb.mcSinks)
	eb.sinkMut.Unlock()

	eb.logger.Debug("[Bus] MC sink attached", "sinkId", id, "bufsize", bufsize)
	return newConsumer
}

func (eb *Bus) RegisterBE(bufsize int) <-chan Event {
	eb.closeMut.RLock()
	defer eb.closeMut.RUnlock()

	newConsumer := make(chan Event, bufsize)
	if eb.done {
		close(newConsumer)
		return newConsumer
	}

	eb.sinkMut.Lock()
	eb.beSinks = append(eb.beSinks, newConsumer)
	id := len(eb.beSinks)
	eb.sinkMut.Unlock()

	eb.logger.Debug("[Bus] BE sink attached", "sinkId", id, "bufsize", bufsize)
	return newConsumer
}

func (eb *Bus) AttachMC[T any](bufsize int, handler func(<-chan Event) T) T {
	ch := eb.RegisterMC(bufsize)
	return handler(ch)
}

func (eb *Bus) AttachBE[T any](bufsize int, handler func(<-chan Event) T) T {
	ch := eb.RegisterBE(bufsize)
	return handler(ch)
}

func (eb *Bus) Start() {
	go func() {
		eb.logger.Debug("[Bus] Event bus muxer started")
		defer close(eb.signal)
		defer eb.logger.Debug("[Bus] Event bus muxer stopped")

		for event := range eb.bus {
			eb.sinkMut.RLock()

			if event.Kind == Log {
				eb.logger.LogAttrs(
					context.Background(),
					event.Level,
					event.Message,
					event.logAttrs()...,
				)
			}

			for _, mcSink := range eb.mcSinks {
				mcSink <- event
			}
			for id, beSink := range eb.beSinks {
				select {
				case beSink <- event:
				default:
					eb.logger.Debug("[Bus] Failed to send event to BE sink", "sinkId", id+1, "runId", event.RunId, "eventType", event.Kind)
				}
			}
			eb.sinkMut.RUnlock()
		}

	}()

	eb.logger.Debug("[Bus] Event bus started")
}

func (eb *Bus) Send(ctx context.Context, es ...Event) {
	eb.closeMut.RLock()
	defer eb.closeMut.RUnlock()

	for _, e := range es {
		if eb.done {
			eb.logger.Debug("[Bus] Failed to send events to bus, bus done", "runId", e.RunId, "eventType", e.Kind)
			continue
		}
		select {
		case eb.bus <- e:
		case <-ctx.Done():
			eb.logger.Debug("[Bus] Failed to sent event to bus, context done", "error", ctx.Err(), "runId", e.RunId, "eventType", e.Kind)
		}
	}
}

func (eb *Bus) Close() {
	eb.closeMut.Lock()
	defer eb.closeMut.Unlock()
	if eb.done {
		return
	}

	eb.done = true

	close(eb.bus)
	<-eb.signal

	eb.sinkMut.RLock()
	for _, mcSink := range eb.mcSinks {
		close(mcSink)
	}
	for _, beSink := range eb.beSinks {
		close(beSink)
	}
	eb.sinkMut.RUnlock()

	eb.logger.Debug("[Bus] Event bus stopped")
}

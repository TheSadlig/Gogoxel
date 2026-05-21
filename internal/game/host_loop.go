package game

import (
	"context"
	"time"
)

func (h *Host) Run(ctx context.Context) error {
	defer close(h.done)
	state, err := newSessionState(h.options)
	if err != nil {
		h.initErr = err
		close(h.ready)
		return err
	}
	defer state.close()
	close(h.ready)
	if state.options.Live {
		return h.runLive(ctx, state)
	}
	return h.runManual(ctx, state)
}

func (h *Host) runManual(ctx context.Context, state *sessionState) error {
	for {
		if state.stopRequested || state.game.ShouldClose() {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case request := <-h.requests:
			h.handleRequest(request, state)
		}
	}
}

func (h *Host) runLive(ctx context.Context, state *sessionState) error {
	if state.options.Headless {
		return h.runLiveHeadless(ctx, state)
	}
	return h.runLiveRenderer(ctx, state)
}

// runLiveRenderer runs the live loop for sessions that have an actual window
// and renderer. It spins as fast as possible, relying on Vulkan present-mode
// pacing (mailbox / FIFO) rather than a software timer, so the renderer is
// never artificially capped at the simulation tick rate.
func (h *Host) runLiveRenderer(ctx context.Context, state *sessionState) error {
	lastFrame := time.Now()
	for {
		if state.stopRequested || state.game.ShouldClose() {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case request := <-h.requests:
			h.handleRequest(request, state)
		default:
			now := time.Now()
			delta := now.Sub(lastFrame)
			lastFrame = now
			if err := state.advanceLiveFrame(delta); err != nil {
				return err
			}
		}
	}
}

// runLiveHeadless runs the live loop for sessions without a renderer (headless
// automation). A timer paces frames at the configured tick rate so the session
// doesn't spin at 100 % CPU when no GPU present call provides back-pressure.
func (h *Host) runLiveHeadless(ctx context.Context, state *sessionState) error {
	tickDuration := state.game.TickDuration()
	timer := time.NewTimer(tickDuration)
	defer timer.Stop()

	for {
		if state.stopRequested || state.game.ShouldClose() {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case request := <-h.requests:
			h.handleRequest(request, state)
			nextTickDuration := state.game.TickDuration()
			if nextTickDuration != tickDuration {
				tickDuration = nextTickDuration
				resetTimer(timer, tickDuration)
			}
		case <-timer.C:
			if err := state.advanceLiveFrame(tickDuration); err != nil {
				return err
			}
			tickDuration = state.game.TickDuration()
			timer.Reset(tickDuration)
		}
	}
}

func (h *Host) handleRequest(request hostRequest, state *sessionState) {
	value, err := request.run(request.ctx, state)
	request.result <- hostResult{value: value, err: err}
}

func (h *Host) invoke(ctx context.Context, fn func(context.Context, *sessionState) (any, error)) (any, error) {
	select {
	case <-h.ready:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if h.initErr != nil {
		return nil, h.initErr
	}
	result := make(chan hostResult, 1)
	request := hostRequest{ctx: ctx, run: fn, result: result}
	select {
	case h.requests <- request:
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-h.done:
		return nil, errStopped
	}
	select {
	case response := <-result:
		return response.value, response.err
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-h.done:
		select {
		case response := <-result:
			return response.value, response.err
		default:
			return nil, errStopped
		}
	}
}

func (h *Host) currentTickDuration(ctx context.Context) (time.Duration, error) {
	value, err := h.invoke(ctx, func(_ context.Context, state *sessionState) (any, error) {
		return state.game.TickDuration(), nil
	})
	if err != nil {
		return 0, err
	}
	return value.(time.Duration), nil
}

func resetTimer(timer *time.Timer, duration time.Duration) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	timer.Reset(duration)
}

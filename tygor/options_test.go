package tygor

import (
	"context"
	"testing"
	"time"
)

func TestWithUnaryInterceptorsClonesInput(t *testing.T) {
	first := func(ctx Context, req any, handler HandlerFunc) (any, error) {
		return handler(ctx, req)
	}
	second := func(ctx Context, req any, handler HandlerFunc) (any, error) {
		return nil, nil
	}
	interceptors := []UnaryInterceptor{first}
	option := WithUnaryInterceptors(interceptors...)
	interceptors[0] = second

	app := NewApp(option)
	if len(app.interceptors) != 1 {
		t.Fatalf("interceptor count = %d, want 1", len(app.interceptors))
	}

	called := false
	_, err := app.interceptors[0](nil, nil, func(context.Context, any) (any, error) {
		called = true
		return nil, nil
	})
	if err != nil {
		t.Fatalf("interceptor returned an error: %v", err)
	}
	if !called {
		t.Fatal("option retained the caller's mutable interceptor slice")
	}
}

func TestEndpointOptionsApplyLeftToRight(t *testing.T) {
	app := NewApp()
	app.Service("Feed").Stream("Subscribe",
		func(context.Context, struct{}, StreamWriter[struct{}]) error { return nil },
		WithStreamHeartbeat(time.Second),
		WithStreamHeartbeat(2*time.Second),
	)

	app.mu.RLock()
	handler, ok := app.routes["Feed.Subscribe"].(*streamHandler[struct{}, struct{}])
	app.mu.RUnlock()
	if !ok {
		t.Fatal("route did not contain the typed stream handler")
	}
	if handler.heartbeatInterval == nil || *handler.heartbeatInterval != 2*time.Second {
		t.Fatalf("heartbeat = %v, want 2s", handler.heartbeatInterval)
	}
}

func TestEndpointZeroOverridesServiceStreamTiming(t *testing.T) {
	app := NewApp()
	service := app.Service("Feed",
		WithStreamWriteTimeout(time.Second),
		WithStreamHeartbeat(time.Second),
	)
	service.Stream("Subscribe",
		func(context.Context, struct{}, StreamWriter[struct{}]) error { return nil },
		WithStreamWriteTimeout(0),
		WithStreamHeartbeat(0),
	)

	app.mu.RLock()
	handler, ok := app.routes["Feed.Subscribe"].(*streamHandler[struct{}, struct{}])
	app.mu.RUnlock()
	if !ok {
		t.Fatal("route did not contain the typed stream handler")
	}
	if handler.writeTimeout == nil || *handler.writeTimeout != 0 {
		t.Fatalf("write timeout = %v, want explicit zero", handler.writeTimeout)
	}
	if handler.heartbeatInterval == nil || *handler.heartbeatInterval != 0 {
		t.Fatalf("heartbeat = %v, want explicit zero", handler.heartbeatInterval)
	}
}

type beforeRegistrationQueryOption struct {
	app     *App
	checked *bool
}

func (o beforeRegistrationQueryOption) applyQuery(*queryConfig) {
	o.app.mu.RLock()
	_, registered := o.app.routes["Test.Query"]
	o.app.mu.RUnlock()
	*o.checked = !registered
}

func TestEndpointOptionsApplyBeforeRegistration(t *testing.T) {
	app := NewApp()
	checked := false
	app.Service("Test").Query("Query",
		func(context.Context, struct{}) (struct{}, error) { return struct{}{}, nil },
		beforeRegistrationQueryOption{app: app, checked: &checked},
	)

	if !checked {
		t.Fatal("query endpoint was published before its options were applied")
	}
}

package agent

import "context"

type turnKey struct{}

func WithTurn(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, turnKey{}, id)
}
func TurnID(ctx context.Context) string { id, _ := ctx.Value(turnKey{}).(string); return id }

type requestKey struct{}

func WithRequest(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestKey{}, id)
}
func RequestID(ctx context.Context) string { id, _ := ctx.Value(requestKey{}).(string); return id }

type connectionKey struct{}

func withConnection(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, connectionKey{}, id)
}
func connectionID(ctx context.Context) string {
	id, _ := ctx.Value(connectionKey{}).(string)
	return id
}

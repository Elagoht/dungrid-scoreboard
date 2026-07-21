package internal

import "context"

type contextKey string

const bodyKey contextKey = "parsed_body"

// submitBody holds the parsed JSON body after HMAC verification.
type submitBody struct {
	Name    string  `json:"name"`
	Metrics Metrics `json:"metrics"`
}

func contextWithBody(ctx context.Context, b *submitBody) context.Context {
	return context.WithValue(ctx, bodyKey, b)
}

func bodyFromContext(ctx context.Context) *submitBody {
	b, _ := ctx.Value(bodyKey).(*submitBody)
	return b
}
